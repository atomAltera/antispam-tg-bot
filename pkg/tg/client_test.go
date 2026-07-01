package tg

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

const fakeToken = "8022662935:AAHoQWmMT_eIs-kyDQsecretsecretsecret"

// failingRoundTripper always fails, forcing http.Client.Do to return a
// *url.Error that carries the full request URL (including the token path).
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// TestGetUpdatesRedactsTokenOnTransportError verifies that when the HTTP
// transport fails, the resulting error does not contain the bot token.
func TestGetUpdatesRedactsTokenOnTransportError(t *testing.T) {
	c := NewClient(fakeToken, &http.Client{Transport: failingRoundTripper{}})

	_, err := c.GetUpdates(context.Background(), 0, 1)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}

	msg := err.Error()
	if strings.Contains(msg, fakeToken) {
		t.Fatalf("error message leaked the token: %q", msg)
	}
	if !strings.Contains(msg, "<redacted>") {
		t.Fatalf("error message was not redacted: %q", msg)
	}
}
