package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

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
