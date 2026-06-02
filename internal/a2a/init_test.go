package a2a

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestInitEngineDelegate verifies the engine initialization.
func TestInitEngineDelegate(t *testing.T) {
	// Reset before test
	// InitEngineDelegate should not panic
	InitEngineDelegate()

	// After init, DelegateToA2AFn should be set and callable
	result, err := engine.DelegateToA2AFn("http://127.0.0.1:19899/unreachable", "test task")
	if err == nil {
		t.Error("expected error when delegating to unreachable A2A server")
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}
}
