package startup

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// slowLLM wraps engine.MockLLM, adding a fixed delay to every Generate call
// so CompanyOrchestrator.RunSprint's three sequential runTree() calls take a
// controllable, non-trivial amount of wall-clock time in tests — standing in
// for the multi-minute real LLM calls RunSprint makes in production.
type slowLLM struct {
	*engine.MockLLM
	delay time.Duration
	calls int32
}

func (m *slowLLM) Generate(prompt string) (string, error) {
	atomic.AddInt32(&m.calls, 1)
	time.Sleep(m.delay)
	return m.MockLLM.Generate(prompt)
}

func (m *slowLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	atomic.AddInt32(&m.calls, 1)
	time.Sleep(m.delay)
	return m.MockLLM.Generate(prompt)
}

// TestRunSprint_DoesNotHoldStateLockAcrossTreeExecution reproduces the bug
// where CompanyOrchestrator.RunSprint takes state.Lock() for its entire body,
// which includes three sequential o.runTree() calls. In production those
// calls can each run for up to 120s against a real LLM, so any concurrent
// state.Lock()/Unlock() — standing in for handleDefaultCompany's read of the
// shared CompanyState on every dashboard page load, or Summary() — blocks for
// the full sprint duration instead of returning quickly.
func TestRunSprint_DoesNotHoldStateLockAcrossTreeExecution(t *testing.T) {
	company := NewDefaultCompany()
	slow := &slowLLM{MockLLM: engine.NewMockLLM(), delay: 50 * time.Millisecond}
	orch := NewOrchestrator(company, slow)

	sprintDone := make(chan struct{})
	go func() {
		orch.RunSprint()
		close(sprintDone)
	}()

	// Give RunSprint a moment to start and take the lock.
	time.Sleep(20 * time.Millisecond)

	const lockBudget = 250 * time.Millisecond
	start := time.Now()
	company.Lock()
	elapsed := time.Since(start)
	company.Unlock()

	if elapsed > lockBudget {
		t.Fatalf("concurrent state.Lock() took %v, want <= %v — RunSprint is holding the lock across its o.runTree() calls instead of releasing it during tree execution", elapsed, lockBudget)
	}

	<-sprintDone
}
