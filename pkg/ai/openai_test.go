package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// TestMain shrinks the retry backoff so tests exercising the retry path don't
// spend seconds sleeping.
func TestMain(m *testing.M) {
	baseRetryDelay = time.Millisecond
	os.Exit(m.Run())
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

const unsupportedFormatBody = `{
  "error": {
    "message": "You uploaded an unsupported image. Please make sure your image has of one the following formats: ['png', 'jpeg', 'gif', 'webp'].",
    "type": "invalid_request_error",
    "param": null,
    "code": "invalid_image_format"
  }
}`

func TestGetJSONCompletionWithImage_UnsupportedFormat(t *testing.T) {
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(400, unsupportedFormatBody), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletionWithImage(context.Background(), "sys", "user", []byte("not really a webp"), "image/webp", SpamCheckFormat, &result)

	var target *UnsupportedImageError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnsupportedImageError, got %T: %v", err, err)
	}

	filename, contentType, payload := target.SentryAttachment()
	if filename != "media.webp" {
		t.Errorf("filename = %q, want media.webp", filename)
	}
	if contentType != "image/webp" {
		t.Errorf("contentType = %q, want image/webp", contentType)
	}
	if !bytes.Equal(payload, []byte("not really a webp")) {
		t.Errorf("payload = %q, want original content", payload)
	}
}

// The moderator/telegram layers wrap this error several times with %w before
// it reaches the Sentry handler; errors.As must still recover it.
func TestUnsupportedImageError_SurvivesWrapping(t *testing.T) {
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(400, unsupportedFormatBody), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletionWithImage(context.Background(), "sys", "user", []byte("x"), "image/webp", SpamCheckFormat, &result)

	wrapped := fmt.Errorf("handling message: %w",
		fmt.Errorf("getting action: %w",
			fmt.Errorf("checking spam: %w",
				fmt.Errorf("getting completion: %w", err))))

	var target *UnsupportedImageError
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As failed to recover *UnsupportedImageError through wrapping chain")
	}
}

func TestGetJSONCompletionWithImage_UnsupportedFormatTooLarge(t *testing.T) {
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(400, unsupportedFormatBody), nil
	}))

	huge := make([]byte, maxAttachmentSize+1)

	var result SpamCheck
	_, err := client.GetJSONCompletionWithImage(context.Background(), "sys", "user", huge, "image/webp", SpamCheckFormat, &result)

	var target *UnsupportedImageError
	if errors.As(err, &target) {
		t.Fatalf("expected plain error for oversized content, got *UnsupportedImageError")
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

const okBody = `{"choices":[{"finish_reason":"stop","message":{"content":"{\"is_spam\":true,\"note\":\"spam\"}"}}]}`

// A 429 or 5xx used to bubble up as an error, which the moderator turns into a
// noop - leaving spam in the chat. Transient failures must be retried instead.
func TestSend_RetriesTransientFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"rate limited", 429},
		{"server error", 500},
		{"bad gateway", 502},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls < 3 {
					return jsonResponse(tt.status, `{"error":{"message":"try later"}}`), nil
				}
				return jsonResponse(200, okBody), nil
			}))

			var result SpamCheck
			_, err := client.GetJSONCompletion(context.Background(), "sys", "user", SpamCheckFormat, &result)
			if err != nil {
				t.Fatalf("expected success after retries, got %v", err)
			}
			if calls != 3 {
				t.Errorf("calls = %d, want 3", calls)
			}
			if !result.IsSpam {
				t.Error("result not decoded from the successful attempt")
			}
		})
	}
}

func TestSend_RetriesTransportErrors(t *testing.T) {
	var calls int
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 2 {
			return nil, errors.New("connection reset by peer")
		}
		return jsonResponse(200, okBody), nil
	}))

	var result SpamCheck
	if _, err := client.GetJSONCompletion(context.Background(), "sys", "user", SpamCheckFormat, &result); err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestSend_GivesUpAfterMaxAttempts(t *testing.T) {
	var calls int
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(429, `{"error":{"message":"rate limited"}}`), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletion(context.Background(), "sys", "user", SpamCheckFormat, &result)
	if err == nil {
		t.Fatal("expected an error after exhausting attempts")
	}
	if calls != maxAttempts {
		t.Errorf("calls = %d, want %d", calls, maxAttempts)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should report the last status, got %v", err)
	}
}

// Permanent failures must fail fast: retrying a 400 or a 401 just burns time.
func TestSend_DoesNotRetryPermanentFailures(t *testing.T) {
	for _, status := range []int{400, 401, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls int
			client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return jsonResponse(status, `{"error":{"message":"nope"}}`), nil
			}))

			var result SpamCheck
			if _, err := client.GetJSONCompletion(context.Background(), "sys", "user", SpamCheckFormat, &result); err == nil {
				t.Fatal("expected an error")
			}
			if calls != 1 {
				t.Errorf("calls = %d, want 1 (no retry)", calls)
			}
		})
	}
}

// The unsupported-image path is a 400: it must keep its typed error and not be
// retried into oblivion.
func TestSend_UnsupportedImageNotRetried(t *testing.T) {
	var calls int
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(400, unsupportedFormatBody), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletionWithImage(context.Background(), "sys", "user", []byte("x"), "image/webp", SpamCheckFormat, &result)

	var target *UnsupportedImageError
	if !errors.As(err, &target) {
		t.Fatalf("expected *UnsupportedImageError, got %T", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", calls)
	}
}

func TestRetryAfter_HonoursHeader(t *testing.T) {
	res := &http.Response{Header: http.Header{"Retry-After": []string{"7"}}}
	if got := retryAfter(res, 0); got != 7*time.Second {
		t.Errorf("retryAfter = %v, want 7s", got)
	}

	// No header: exponential backoff off the base delay.
	plain := &http.Response{Header: http.Header{}}
	if got := retryAfter(plain, 2); got != baseRetryDelay*4 {
		t.Errorf("retryAfter = %v, want %v", got, baseRetryDelay*4)
	}
}

func TestSend_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel() // cancel while the first attempt is in flight
		return jsonResponse(500, `{"error":{"message":"boom"}}`), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletion(ctx, "sys", "user", SpamCheckFormat, &result)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (must not retry after cancellation)", calls)
	}
}

// captureModel runs one completion against a stub transport and returns the
// model name the request carried.
func captureModel(t *testing.T, client *OpenAI, withImage bool) string {
	t.Helper()

	var got string
	client.httpClient = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		got = parsed.Model
		return jsonResponse(200, `{"choices":[{"finish_reason":"stop","message":{"content":"{\"is_spam\":false,\"note\":\"\"}"}}]}`), nil
	})

	var result SpamCheck
	var err error
	if withImage {
		_, err = client.GetJSONCompletionWithImage(context.Background(), "sys", "user", []byte("img"), "image/png", SpamCheckFormat, &result)
	} else {
		_, err = client.GetJSONCompletion(context.Background(), "sys", "user", SpamCheckFormat, &result)
	}
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}

	return got
}

func TestModelOverride(t *testing.T) {
	tests := []struct {
		name      string
		override  string
		withImage bool
		want      string
	}{
		{"text defaults to DefaultModel", "", false, DefaultModel},
		{"image defaults to VisionModel", "", true, VisionModel},
		{"text honours override", "gpt-5.6-luna", false, "gpt-5.6-luna"},
		{"image honours override", "gpt-5.6-luna", true, "gpt-5.6-luna"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewOpenAI("key", nil)
			client.Model = tt.override

			if got := captureModel(t, client, tt.withImage); got != tt.want {
				t.Errorf("request model = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelName(t *testing.T) {
	client := NewOpenAI("key", nil)
	if got := client.ModelName(); got != DefaultModel {
		t.Errorf("ModelName() = %q, want %q", got, DefaultModel)
	}

	client.Model = "gpt-5.6-terra"
	if got := client.ModelName(); got != "gpt-5.6-terra" {
		t.Errorf("ModelName() = %q, want gpt-5.6-terra", got)
	}
}

func TestGetJSONCompletionWithImage_OtherErrorNotWrapped(t *testing.T) {
	client := NewOpenAI("key", roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(500, `{"error":{"message":"server error","type":"server_error","code":""}}`), nil
	}))

	var result SpamCheck
	_, err := client.GetJSONCompletionWithImage(context.Background(), "sys", "user", []byte("content"), "image/webp", SpamCheckFormat, &result)

	var target *UnsupportedImageError
	if errors.As(err, &target) {
		t.Fatalf("expected plain error for unrelated failure, got *UnsupportedImageError")
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
