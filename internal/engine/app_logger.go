// Package log provides structured logging for the Go BT framework.
// Uses Go's standard library slog with JSON output and file rotation.
package engine

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu            sync.Mutex
	logger        *slog.Logger
	rotWriter     io.WriteCloser // rotating writer, closed on shutdown
	extraHandlers []slog.Handler
)

// Init initializes the logger with output to ~/.go-bt-evolve/logs/bt.log
// with automatic log rotation (10MB max per file, 5 backups kept).
// Falls back to stderr if the log directory cannot be created.
func Init() {
	mu.Lock()
	defer mu.Unlock()

	if logger != nil {
		return // already initialized
	}

	buildLogger()
}

// buildLogger constructs the global logger's handler chain: a base JSON
// handler (rotating file + stderr, exactly as before) fanned out to any
// attached extra handlers (e.g. the OTLP bridge), wrapped in the
// trace-correlation handler so every record gains trace_id/span_id when its
// context carries a span. The file handler is always first in the fanout
// and always receives every record. Must be called under mu.
func buildLogger() {
	base := buildBaseHandler()

	children := append([]slog.Handler{base}, extraHandlers...)
	logger = slog.New(newTraceContextHandler(&fanoutHandler{children: children}))
}

// buildBaseHandler creates the base JSON handler writing to the rotating
// log file and stderr (or a stderr-only text handler as a fallback).
func buildBaseHandler() slog.Handler {
	home, err := os.UserHomeDir()
	if err != nil {
		return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	logDir := filepath.Join(home, ".go-bt-evolve", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}

	logPath := filepath.Join(logDir, "bt.log")
	rw, err := NewRotatingWriter(logPath)
	if err != nil {
		return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	rotWriter = rw

	// Write to both rotating file and stderr
	w := io.MultiWriter(rw, os.Stderr)
	return slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
}

// attachLogHandler adds a handler (e.g. the OTLP bridge) to the global
// logger's fanout. Must be called after Init; rebuilds the logger.
func attachLogHandler(h slog.Handler) {
	mu.Lock()
	defer mu.Unlock()
	extraHandlers = append(extraHandlers, h)
	logger = nil // next L() rebuilds via Init path
	buildLogger()
}

// L returns the global logger. Calls Init() if not initialized.
func L() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		// Can't call Init() here (deadlock on mu), return a fallback
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return logger
}

// Debug logs a debug message.
func Debug(msg string, args ...any) { L().Debug(msg, args...) }

// Info logs an info message.
func Info(msg string, args ...any) { L().Info(msg, args...) }

// Warn logs a warning message.
func Warn(msg string, args ...any) { L().Warn(msg, args...) }

// Error logs an error message.
func Error(msg string, args ...any) { L().Error(msg, args...) }

// Shutdown closes the rotating log writer if one was opened.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if rotWriter != nil {
		_ = rotWriter.Close()
		rotWriter = nil
	}
}
