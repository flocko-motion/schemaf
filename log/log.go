// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// Package log provides the framework's centralized logger.
// By default it writes structured JSON to stderr via slog.
// Replace the logger via Set() to wire telemetry or change format.
package log

import (
	"context"
	"log/slog"
	"os"
)

var logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

// Set replaces the global logger. Call this at application startup to wire
// a custom handler (e.g. OpenTelemetry, structured cloud logging, etc.).
func Set(l *slog.Logger) { logger = l }

// Logger returns the current global logger.
func Logger() *slog.Logger { return logger }

func Debug(msg string, args ...any)                        { logger.Debug(msg, args...) }
func Info(msg string, args ...any)                         { logger.Info(msg, args...) }
func Warn(msg string, args ...any)                         { logger.Warn(msg, args...) }
func Error(msg string, args ...any)                        { logger.Error(msg, args...) }
func DebugCtx(ctx context.Context, msg string, args ...any) { logger.DebugContext(ctx, msg, args...) }
func InfoCtx(ctx context.Context, msg string, args ...any)  { logger.InfoContext(ctx, msg, args...) }
func WarnCtx(ctx context.Context, msg string, args ...any)  { logger.WarnContext(ctx, msg, args...) }
func ErrorCtx(ctx context.Context, msg string, args ...any) { logger.ErrorContext(ctx, msg, args...) }
