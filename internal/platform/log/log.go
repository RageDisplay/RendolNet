// Package log настраивает slog: JSON в контейнере, текст при разработке.
package log

import (
	"log/slog"
	"os"
	"strings"
)

func Setup(service string) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(os.Getenv("RN_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if os.Getenv("RN_DEV") != "" {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	l := slog.New(h).With("service", service)
	slog.SetDefault(l)
	return l
}
