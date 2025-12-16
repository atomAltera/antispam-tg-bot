package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type OpenAI struct {
	apiKey     string
	httpClient HTTPClient
}

func NewOpenAI(apiKey string, httpClient HTTPClient) *OpenAI {
	return &OpenAI{
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

func (c *OpenAI) GetJSONCompletion(ctx context.Context, system, user string, rf ResponseFormat, result any) (*Usage, error) {
	return c.getCompletion(ctx, DefaultModel, system, user, nil, rf, result)
}

// GetJSONCompletionWithImage sends a request with both text and image to the vision model
func (c *OpenAI) GetJSONCompletionWithImage(ctx context.Context, system, user string, image []byte, mimeType string, rf ResponseFormat, result any) (*Usage, error) {
	imageData := &ImageData{
		Content:  image,
		MimeType: mimeType,
	}
	return c.getCompletion(ctx, VisionModel, system, user, imageData, rf, result)
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
		return nil, fmt.Errorf("doing request: %w", err)
	}

	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != 200 {
		resBody, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("unexpected status code: %d: %s", res.StatusCode, resBody)
	}

	body, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
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
