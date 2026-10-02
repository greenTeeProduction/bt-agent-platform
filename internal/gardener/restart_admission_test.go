package gardener

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestOwnedRestartWaitsForActualGardenerCycleAndSealsResumption(t *testing.T) {
	root := t.TempDir()
	registry := &Registry{dir: filepath.Join(root, "trees")}
	tracker, err := NewMetricsTracker(root)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGardener(Config{Registry: registry, MetricsTracker: tracker, ValidationGate: ValidationGateConfig{EvidencePath: filepath.Join(root, "empty-slo")}})
	var restartCalls atomic.Int64
	stop, err := agent.StartRestartControl(agent.RestartControlConfig{
		Home: root, Unit: "bt-gardener", Revision: strings.Repeat("a", 40), BinaryPath: "fixture-bin", Enabled: true, VerifyOwner: func(string) error { return nil }, BeginRestart: g.BeginRestart,
		VerifyArtifact: func(_, _, _ string) error { return nil }, Restart: func(string) error { restartCalls.Add(1); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	// Contend on the actual cycle's first dependency, after its admission.
	registry.mu.Lock()
	release := sync.OnceFunc(registry.mu.Unlock)
	done := make(chan error, 1)
	go func() { _, err := g.RunCycleV2(DefaultEvolveV2Config()); done <- err }()
	t.Cleanup(func() { release() })
	deadline := time.Now().Add(time.Second)
	for !g.AnyInFlight() {
		if time.Now().After(deadline) {
			t.Fatal("cycle did not acquire ownership")
		}
		time.Sleep(time.Millisecond)
	}
	if err := agent.RequestOwnedRestart(root, "bt-gardener", strings.Repeat("b", 40)); !errors.Is(err, agent.ErrRestartDeferred) || restartCalls.Load() != 0 {
		t.Fatalf("in-flight cycle restarted: %v calls=%d", err, restartCalls.Load())
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cycle did not finish")
	}
	before, err := os.ReadFile(tracker.path)
	if err != nil {
		t.Fatal(err)
	}
	// The iteration remains owned after its cycle, while analysis/tool cleanup
	// is still pending. This phase invokes no model provider.
	analysisStarted, endAnalysis := make(chan struct{}), make(chan struct{})
	releaseAnalysis := sync.OnceFunc(func() { close(endAnalysis) })
	t.Cleanup(releaseAnalysis)
	analysisDone := make(chan error, 1)
	go func() { analysisDone <- g.WithActivity(func() { close(analysisStarted); <-endAnalysis }) }()
	select {
	case <-analysisStarted:
	case <-time.After(time.Second):
		t.Fatal("analysis ownership did not start")
	}
	if err := agent.RequestOwnedRestart(root, "bt-gardener", strings.Repeat("b", 40)); !errors.Is(err, agent.ErrRestartDeferred) || restartCalls.Load() != 0 {
		t.Fatalf("completed cycle hid owned analysis: %v", err)
	}
	releaseAnalysis()
	select {
	case err := <-analysisDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("analysis did not finish")
	}
	if err := agent.RequestOwnedRestart(root, "bt-gardener", strings.Repeat("b", 40)); err != nil {
		t.Fatal(err)
	}
	if err := g.WithActivity(func() { t.Error("sealed analysis admitted") }); !errors.Is(err, reliability.ErrRestartPending) {
		t.Fatalf("sealed analysis resumed: %v", err)
	}
	if _, err := g.RunCycleV2(DefaultEvolveV2Config()); !errors.Is(err, reliability.ErrRestartPending) {
		t.Fatalf("sealed cycle admitted: %v", err)
	}
	after, err := os.ReadFile(tracker.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || restartCalls.Load() != 1 || g.cycleCount.Load() != 1 {
		t.Fatal("accepted handoff admitted/repeated cycle metadata")
	}
}
