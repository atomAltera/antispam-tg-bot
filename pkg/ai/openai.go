package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type OpenAI struct {
	apiKey     string
	httpClient HTTPClient

	// Model overrides the model used for completions. Empty (the default)
	// means DefaultModel/VisionModel are used. Exists so auxiliary tools
	// (e.g. cmd/test) can A/B another model against the one the bot ships
	// with, without touching the constants.
	Model string
}

func NewOpenAI(apiKey string, httpClient HTTPClient) *OpenAI {
	return &OpenAI{
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *OpenAI) GetJSONCompletion(ctx context.Context, system, user string, rf ResponseFormat, result any) (*Usage, error) {
	return c.getCompletion(ctx, c.modelOr(DefaultModel), system, user, nil, rf, result)
}

// ModelName reports the model text completions will use: the override when set,
// otherwise DefaultModel. Lets callers log which model a run actually used.
func (c *OpenAI) ModelName() string {
	return c.modelOr(DefaultModel)
}

// modelOr returns the configured Model override, or def when none is set.
func (c *OpenAI) modelOr(def string) string {
	if c.Model != "" {
		return c.Model
	}
	return def
}

// GetJSONCompletionWithImage sends a request with both text and image to the vision model
func (c *OpenAI) GetJSONCompletionWithImage(ctx context.Context, system, user string, image []byte, mimeType string, rf ResponseFormat, result any) (*Usage, error) {
	imageData := &ImageData{
		Content:  image,
		MimeType: mimeType,
	}
	return c.getCompletion(ctx, c.modelOr(VisionModel), system, user, imageData, rf, result)
}

type ImageData struct {
	Content  []byte
	MimeType string
}

// VisionSupportedMimeTypes are the image formats supported by OpenAI vision API
var VisionSupportedMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true, // non-animated only
}

// IsVisionSupported checks if the mime type is supported by vision API
func IsVisionSupported(mimeType string) bool {
	return VisionSupportedMimeTypes[mimeType]
}

// maxAttachmentSize is the largest media payload we'll carry on an
// UnsupportedImageError for later diagnosis (e.g. as a Sentry attachment).
const maxAttachmentSize = 5 * 1024 * 1024

// UnsupportedImageError wraps a vision API failure caused by a media file
// that doesn't actually match the declared mime type (e.g. a Telegram video
// sticker misidentified as image/webp). It carries the original content so
// callers can attach it somewhere for later analysis.
type UnsupportedImageError struct {
	err      error
	mimeType string
	content  []byte
}

func (e *UnsupportedImageError) Error() string { return e.err.Error() }
func (e *UnsupportedImageError) Unwrap() error { return e.err }

// SentryAttachment returns the offending media so it can be attached to an
// error report. Implements an attacher interface understood by pkg/logger.
func (e *UnsupportedImageError) SentryAttachment() (filename, contentType string, payload []byte) {
	ext := e.mimeType
	if i := strings.LastIndex(e.mimeType, "/"); i >= 0 {
		ext = e.mimeType[i+1:]
	}
	return "media." + ext, e.mimeType, e.content
}

// isUnsupportedImageFormat reports whether an OpenAI error response body
// indicates the uploaded media doesn't match a supported image format.
func isUnsupportedImageFormat(resBody []byte) bool {
	var apiErr struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resBody, &apiErr); err != nil {
		return false
	}
	return apiErr.Error.Code == "invalid_image_format"
}

// maxAttempts bounds how many times a single completion is tried. Transient
// failures (429 rate limits, 5xx server errors) used to surface as an error
// that the moderator turns into a noop - i.e. spam silently stayed in the chat
// whenever OpenAI hiccuped.
const maxAttempts = 4

// baseRetryDelay is the first backoff step; it doubles per attempt unless the
// response carries a Retry-After hint. A variable so tests can shrink it.
var baseRetryDelay = time.Second

// isRetryable reports whether a status code is worth another attempt: rate
// limits and server-side errors are transient, everything else (bad request,
// auth, unsupported image) will fail identically no matter how often we retry.
func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryAfter extracts the server's requested delay, falling back to exponential
// backoff. attempt is zero-based.
func retryAfter(res *http.Response, attempt int) time.Duration {
	if v := res.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return baseRetryDelay << attempt
}

// sleep waits for d, or aborts early if the context is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// send posts the request body, retrying transient failures, and returns the
// raw response body on success.
func (c *OpenAI) send(ctx context.Context, body []byte, image *ImageData) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			"https://api.openai.com/v1/chat/completions",
			bytes.NewReader(body),
		)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
		req.Header.Set("Content-Type", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			// Transport-level failures (connection reset, timeout) are
			// transient too, so they get the same treatment as a 5xx.
			lastErr = fmt.Errorf("doing request: %w", err)
			if attempt == maxAttempts-1 {
				break
			}
			if err := sleep(ctx, baseRetryDelay<<attempt); err != nil {
				return nil, err
			}
			continue
		}

		resBody, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()

		if res.StatusCode == 200 {
			if readErr != nil {
				return nil, fmt.Errorf("reading response body: %w", readErr)
			}
			return resBody, nil
		}

		statusErr := fmt.Errorf("unexpected status code: %d: %s", res.StatusCode, resBody)

		if image != nil && isUnsupportedImageFormat(resBody) && len(image.Content) <= maxAttachmentSize {
			return nil, &UnsupportedImageError{err: statusErr, mimeType: image.MimeType, content: image.Content}
		}

		if !isRetryable(res.StatusCode) {
			return nil, statusErr
		}

		lastErr = statusErr
		if attempt == maxAttempts-1 {
			break
		}
		if err := sleep(ctx, retryAfter(res, attempt)); err != nil {
			return nil, err
		}
	}

	return nil, fmt.Errorf("after %d attempts: %w", maxAttempts, lastErr)
}

func (c *OpenAI) getCompletion(ctx context.Context, model, system, user string, image *ImageData, rf ResponseFormat, result any) (*Usage, error) {
	var userContent any
	if image != nil {
		// Multi-modal content with text and image
		b64 := base64.StdEncoding.EncodeToString(image.Content)
		dataURL := fmt.Sprintf("data:%s;base64,%s", image.MimeType, b64)
		userContent = []ContentPart{
			{Type: "text", Text: user},
			{Type: "image_url", ImageURL: &ImageURL{URL: dataURL, Detail: "low"}}, // "low" saves tokens
		}
	} else {
		userContent = user
	}

	request := Request{
		Model: model,
		Messages: []Message{
			{
				Role:    RoleSystem,
				Content: system,
			},
			{
				Role:    RoleUser,
				Content: userContent,
			},
		},
		ResponseFormat: rf,
	}

	// Only add reasoning effort for non-vision models
	if image == nil {
		request.ReasoningEffort = ReasoningEffortMedium
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling body: %w", err)
	}

	body, err = c.send(ctx, body, image)
	if err != nil {
		return nil, err
	}

	var response Response
	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(response.Choices) == 0 {
		return &response.Usage, fmt.Errorf("empty choices in response")
	}

	choice := response.Choices[0]

	if choice.FinishReason != FinishReasonStop {
		return &response.Usage, fmt.Errorf("unexpected finish reason: %v", choice.FinishReason)
	}

	if err = json.Unmarshal([]byte(choice.Message.Content), result); err != nil {
		return &response.Usage, fmt.Errorf("unmarshal response content: %w", err)
	}

	return &response.Usage, nil
}

type SpamCheck struct {
	IsSpam bool   `json:"is_spam"`
	Note   string `json:"note"`
}

type ResponseFormat string

func (rf ResponseFormat) MarshalJSON() ([]byte, error) {
	return []byte(rf), nil
}

var SpamCheckFormat ResponseFormat = `{
  "type": "json_schema",
  "json_schema": {
    "name": "spam_check_response",
    "schema": {
      "type": "object",
      "properties": {
        "is_spam": {
          "type": "boolean",
		  "description": "true if the message is spam, false otherwise"
        },
		"note": {
		  "type": "string",
		  "description": "if message is spam, this field contains short description of reason why it is spam"
		}
      },
      "required": ["is_spam", "note"],
      "additionalProperties": false
    },
    "strict": true
  }
}`

const DefaultModel = "gpt-5-mini"
const VisionModel = "gpt-5-mini" // same model, supports vision/image analysis
