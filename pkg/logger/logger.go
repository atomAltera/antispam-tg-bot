package logger

import (
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
)

type Logger = *slog.Logger

func NewLogger() Logger {
	return slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
	}))
}

// NewLoggerWithSentry creates a logger that auto-reports errors to Sentry
func NewLoggerWithSentry() Logger {
	tintHandler := tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
	})
	return slog.New(NewSentryHandler(tintHandler))
}
