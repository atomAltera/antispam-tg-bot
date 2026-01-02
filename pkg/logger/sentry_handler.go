package logger

import (
	"context"
	"log/slog"

	"github.com/getsentry/sentry-go"
)

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
					sentry.CaptureException(err)
				}
			}
			return true
		})
	}
	return h.handler.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes
func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SentryHandler{handler: h.handler.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group name
func (h *SentryHandler) WithGroup(name string) slog.Handler {
	return &SentryHandler{handler: h.handler.WithGroup(name)}
}
