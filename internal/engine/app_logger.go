// Package log provides structured logging for the Go BT framework.
// Uses Go's standard library slog with JSON output and file rotation.
package engine

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu            sync.Mutex
	logger        *slog.Logger
	rotWriter     io.WriteCloser // rotating writer, closed on shutdown
	extraHandlers []slog.Handler
	isSlogDefault bool // engine logger installed as slog default; guarded by mu
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

	// Split levels: the rotating FILE keeps full DEBUG detail for forensics,
	// while STDERR (the systemd journal) uses BT_LOG_LEVEL (default INFO) so
	// the journal is readable — before this, DEBUG "llm health check OK" every
	// 30s (2/min) buried every operationally useful line.
	fileH := slog.NewJSONHandler(rw, &slog.HandlerOptions{Level: slog.LevelDebug})
	stderrH := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: envLogLevel()})
	return &fanoutHandler{children: []slog.Handler{fileH, stderrH}}
}

// envLogLevel reads BT_LOG_LEVEL (debug|info|warn|error), defaulting to INFO
// for the journal/stderr sink.
func envLogLevel() slog.Level {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BT_LOG_LEVEL"))) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// SetAsDefault installs the engine logger as slog's process default and
// keeps it current when the handler chain is rebuilt later (e.g.
// InitLogExport attaching the OTLP bridge). Binaries call this instead of
// slog.SetDefault(L()), which would freeze the pre-bridge chain.
func SetAsDefault() {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		buildLogger()
	}
	isSlogDefault = true
	slog.SetDefault(logger)
}

// attachLogHandler adds a handler (e.g. the OTLP bridge) to the global
// logger's fanout. Must be called after Init; rebuilds the logger. If the
// engine logger is the slog default, the rebuilt instance is re-installed
// so package-level slog calls keep following the current handler chain.
func attachLogHandler(h slog.Handler) {
	mu.Lock()
	defer mu.Unlock()
	extraHandlers = append(extraHandlers, h)
	logger = nil // next L() rebuilds via Init path
	buildLogger()
	if isSlogDefault {
		slog.SetDefault(logger)
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

// Shutdown closes the rotating log writer if one was opened.
func Shutdown() {
	mu.Lock()
	defer mu.Unlock()
	if rotWriter != nil {
		_ = rotWriter.Close()
		rotWriter = nil
	}
}
