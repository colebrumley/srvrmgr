// internal/logging/logger.go
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// NewLogger creates a new structured logger
func NewLogger(format string, level string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}

	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}

	return slog.New(handler)
}

// WithRule returns a logger with the rule name attached
func WithRule(logger *slog.Logger, ruleName string) *slog.Logger {
	return logger.With("rule", ruleName)
}

// WithContext returns a logger with context values attached
func WithContext(logger *slog.Logger, ctx context.Context) *slog.Logger {
	// Add any context values here if needed
	return logger
}
