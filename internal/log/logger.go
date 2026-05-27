// Package log provides structured logging for the Go BT framework.
// Uses Go's standard library slog with JSON output and file rotation.
package log

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu     sync.Mutex
	logger *slog.Logger
	rotWriter io.WriteCloser // rotating writer, closed on shutdown
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

	home, err := os.UserHomeDir()
	if err != nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return
	}

	logDir := filepath.Join(home, ".go-bt-evolve", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return
	}

	logPath := filepath.Join(logDir, "bt.log")
	rw, err := NewRotatingWriter(logPath)
	if err != nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return
	}
	rotWriter = rw

	// Write to both rotating file and stderr
	w := io.MultiWriter(rw, os.Stderr)
	logger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// Shutdown closes the rotating writer. Call before program exit.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if rotWriter != nil {
		rotWriter.Close()
		rotWriter = nil
	}
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
