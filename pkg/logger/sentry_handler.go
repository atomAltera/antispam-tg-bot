package logger

import (
	"context"
	"errors"
	"log/slog"

	"github.com/getsentry/sentry-go"
)

// sentryAttacher is implemented by errors that carry extra payload worth
// attaching to their Sentry event (e.g. the offending file for a media
// format error). Kept generic (no sentry-go dependency) so lower-level
// packages like pkg/ai don't need to import the SDK.
type sentryAttacher interface {
	SentryAttachment() (filename, contentType string, payload []byte)
}

// SentryHandler wraps an slog.Handler and reports errors to Sentry
type SentryHandler struct {
	handler slog.Handler
}

// NewSentryHandler creates a new SentryHandler wrapping the given handler
func NewSentryHandler(handler slog.Handler) *SentryHandler {
	return &SentryHandler{handler: handler}
}

// Enabled reports whether the handler handles records at the given level
func (h *SentryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle processes the record, sending errors to Sentry for Error level logs
func (h *SentryHandler) Handle(ctx context.Context, r slog.Record) error {
	// For Error level and above, extract "error" attribute and send to Sentry
	if r.Level >= slog.LevelError {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "error" {
				if err, ok := a.Value.Any().(error); ok {
					captureError(err)
				}
			}
			return true
		})
	}
	return h.handler.Handle(ctx, r)
}

// captureError sends err to Sentry, attaching any payload exposed via
// sentryAttacher (checked through the error's Unwrap chain) to that event only.
func captureError(err error) {
	var attacher sentryAttacher
	if errors.As(err, &attacher) {
		filename, contentType, payload := attacher.SentryAttachment()
		hub := sentry.CurrentHub().Clone()
		hub.Scope().AddAttachment(&sentry.Attachment{
			Filename:    filename,
			ContentType: contentType,
			Payload:     payload,
		})
		hub.CaptureException(err)
		return
	}
	sentry.CaptureException(err)
}

// WithAttrs returns a new handler with the given attributes
func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SentryHandler{handler: h.handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group name
func (h *SentryHandler) WithGroup(name string) slog.Handler {
	return &SentryHandler{handler: h.handler.WithGroup(name)}
}
