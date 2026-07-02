package engine

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// recordingHandler is a minimal slog.Handler that records every message it
// receives, used to stand in for the OTLP bridge in tests.
type recordingHandler struct {
	mu   sync.Mutex
	msgs []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, r.Message)
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) received(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.msgs {
		if m == msg {
			return true
		}
	}
	return false
}

// TestSetAsDefaultFollowsHandlerRebuild verifies that after SetAsDefault,
// the process-wide slog default logger keeps tracking the engine logger even
// when the handler chain is rebuilt later (as InitLogExport does when it
// attaches the OTLP bridge via attachLogHandler). Plain
// slog.SetDefault(engine.L()) would freeze the pre-bridge chain and this
// test would fail: the record logged through slog.Default() would never
// reach the late-attached handler.
func TestSetAsDefaultFollowsHandlerRebuild(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() {
		// Restore process state: previous default logger, no extra
		// handlers, default-installer flag cleared.
		mu.Lock()
		extraHandlers = nil
		isSlogDefault = false
		buildLogger()
		mu.Unlock()
		slog.SetDefault(prev)
	})

	SetAsDefault()

	// Simulate InitLogExport attaching the OTLP bridge AFTER the default
	// was installed.
	rec := &recordingHandler{}
	attachLogHandler(rec)

	const probe = "app_logger_test: rebuild-follow probe"
	slog.Default().Info(probe)

	if !rec.received(probe) {
		t.Fatalf("late-attached handler did not receive record logged via slog.Default(); "+
			"the slog default froze the pre-rebuild handler chain (msgs seen: %v)", rec.msgs)
	}

	// Package-level slog calls (what the migrated library packages use)
	// must reach the late-attached handler too.
	const probe2 = "app_logger_test: package-level probe"
	slog.Info(probe2)
	if !rec.received(probe2) {
		t.Fatalf("late-attached handler did not receive package-level slog.Info record (msgs seen: %v)", rec.msgs)
	}
}
