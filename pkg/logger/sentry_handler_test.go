package logger

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

type fakeTransport struct {
	events []*sentry.Event
}

func (t *fakeTransport) Flush(time.Duration) bool              { return true }
func (t *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (t *fakeTransport) Configure(sentry.ClientOptions)        {}
func (t *fakeTransport) Close()                                {}
func (t *fakeTransport) SendEvent(event *sentry.Event) {
	t.events = append(t.events, event)
}

type attachingError struct {
	msg string
}

func (e *attachingError) Error() string { return e.msg }
func (e *attachingError) SentryAttachment() (filename, contentType string, payload []byte) {
	return "media.webp", "image/webp", []byte("bytes")
}

func TestSentryHandler_AttachesPayloadForAttachingError(t *testing.T) {
	transport := &fakeTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://test@example.com/1", Transport: transport}); err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	h := NewSentryHandler(slog.NewTextHandler(discardWriter{}, nil))
	r := slog.NewRecord(time.Now(), slog.LevelError, "handling update", 0)
	r.AddAttrs(slog.Any("error", &attachingError{msg: "boom"}))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	sentry.Flush(time.Second)

	if len(transport.events) != 1 {
		t.Fatalf("got %d events, want 1", len(transport.events))
	}
	att := transport.events[0].Attachments
	if len(att) != 1 {
		t.Fatalf("got %d attachments, want 1", len(att))
	}
	if att[0].Filename != "media.webp" || att[0].ContentType != "image/webp" || string(att[0].Payload) != "bytes" {
		t.Errorf("unexpected attachment: %+v", att[0])
	}
}

func TestSentryHandler_NoAttachmentForPlainError(t *testing.T) {
	transport := &fakeTransport{}
	if err := sentry.Init(sentry.ClientOptions{Dsn: "https://test@example.com/1", Transport: transport}); err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	t.Cleanup(func() { sentry.CurrentHub().BindClient(nil) })

	h := NewSentryHandler(slog.NewTextHandler(discardWriter{}, nil))
	r := slog.NewRecord(time.Now(), slog.LevelError, "handling update", 0)
	r.AddAttrs(slog.Any("error", errors.New("plain failure")))

	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	sentry.Flush(time.Second)

	if len(transport.events) != 1 {
		t.Fatalf("got %d events, want 1", len(transport.events))
	}
	if len(transport.events[0].Attachments) != 0 {
		t.Errorf("expected no attachments, got %d", len(transport.events[0].Attachments))
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
