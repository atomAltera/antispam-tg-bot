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
