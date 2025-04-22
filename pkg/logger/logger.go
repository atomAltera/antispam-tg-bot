package logger

import (
	"github.com/lmittmann/tint"
	"log/slog"
	"os"
	"time"
)

type Logger = *slog.Logger

func NewLogger() Logger {
	return slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
	}))
}
