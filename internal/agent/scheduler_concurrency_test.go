package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func concurrencyScheduler(t *testing.T, names ...string) *Scheduler {
	t.Helper()
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(SchedulerConfig{Registry: reg, TickInterval: time.Millisecond})
	for _, name := range names {
		if _, err := reg.Create(Definition{Name: name, Tree: "domain:default", Version: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
		j, err := s.Schedule(name, "every 1h", "1h", 0)
		if err != nil {
			t.Fatal(err)
		}
		j.NextRun = time.Time{}
	}
	return s
}

func receiveAgent(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case name := <-ch:
		return name
	case <-time.After(2 * time.Second):
		t.Fatal("due agent blocked behind another run")
		return ""
	}
}

func waitSchedulerIdle(t *testing.T, s *Scheduler) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for s.AnyInFlight() {
		select {
		case <-deadline:
			t.Fatal("scheduler failed to release execution lanes")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestSchedulerBoundedOldestFirst(t *testing.T) {
	for _, limit := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			s := concurrencyScheduler(t, "a", "b", "c", "d")
			s.maxConcurrent = limit
			defer s.Stop()
			started := make(chan string, 10)
			release := make(chan struct{})
			runner := func(rc RunContext) (string, string, *RunResult, error) {
				started <- rc.AgentName
				select {
				case <-release:
				case <-rc.Context.Done():
				}
				return "success", "", nil, nil
			}
			// Make a unequivocally oldest; the others have deterministic ID tie-breaking.
			for _, j := range s.jobs {
				if j.AgentName != "a" {
					j.NextRun = time.Now().Add(-time.Minute)
				}
			}
			s.tick(runner)
			seen := map[string]bool{}
			for range limit {
				seen[receiveAgent(t, started)] = true
			}
			if !seen["a"] {
				t.Fatal("oldest job was not admitted")
			}
			for range 10 {
				s.tick(runner)
			}
			s.mu.RLock()
			count := len(s.activeAgents)
			s.mu.RUnlock()
			if count != limit {
				t.Fatalf("active=%d, limit=%d", count, limit)
			}
			for _, j := range s.ListJobs() {
				if !seen[j.AgentName] && (j.InFlight || j.RunCount != 0 || j.NextRun.After(time.Now())) {
					t.Fatalf("saturated job lost due state: %+v", j)
				}
			}
			close(release)
			waitSchedulerIdle(t, s)
			s.tick(runner)
			for i := limit; i < 4 && i < 2*limit; i++ {
				name := receiveAgent(t, started)
				if seen[name] {
					t.Fatalf("ran completed job again: %s", name)
				}
			}
		})
	}
}

func TestSchedulerRemovalDoesNotReleaseAgent(t *testing.T) {
	s := concurrencyScheduler(t, "a")
	defer s.Stop()
	started := make(chan string, 4)
	release := make(chan struct{})
	runner := func(rc RunContext) (string, string, *RunResult, error) {
		started <- rc.AgentName
		select {
		case <-release:
		case <-rc.Context.Done():
		}
		return "success", "", nil, nil
	}
	s.tick(runner)
	receiveAgent(t, started)
	if err := s.RemoveJob(s.ListJobs()[0].ID); err != nil {
		t.Fatal(err)
	}
	if !s.AnyInFlight() {
		t.Fatal("removed running job disappeared from deploy guard")
	}
	j, err := s.Schedule("a", "every 1h", "1h", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	j.NextRun = time.Time{}
	s.mu.Unlock()
	s.tick(runner)
	if s.ListJobs()[0].InFlight {
		t.Fatal("replacement overlapped old agent")
	}
	if _, _, err := s.RunNow("a", "", runner, "1h"); err == nil {
		t.Fatal("RunNow overlapped scheduled agent")
	}
	// Exercise mutable timeout/schedule reads concurrently with completion.
	for range 10 {
		if _, err := s.Schedule("a", "every 1h", "30m", 0); err != nil {
			t.Fatal(err)
		}
		s.SyncFromRegistry()
	}
	close(release)
	waitSchedulerIdle(t, s)
	s.tick(runner)
	receiveAgent(t, started)
}

func TestSchedulerRunNowSharesAdmissionAndCancellation(t *testing.T) {
	s := concurrencyScheduler(t, "a")
	started := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = s.RunNow("a", "", func(rc RunContext) (string, string, *RunResult, error) {
			started <- rc.AgentName
			<-rc.Context.Done()
			return "failure", "", nil, rc.Context.Err()
		}, "1h")
	}()
	receiveAgent(t, started)
	s.tick(func(RunContext) (string, string, *RunResult, error) {
		t.Error("scheduled run overlapped RunNow")
		return "success", "", nil, nil
	})
	s.Stop()
	<-done
	if _, _, err := s.RunNow("a", "", nil, "1h"); err == nil {
		t.Fatal("RunNow admitted after Stop")
	}
}

func TestSchedulerStopBeforeStartAndConcurrentStop(t *testing.T) {
	s := concurrencyScheduler(t, "a")
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { ; s.Stop() })
	}
	wg.Wait()
	runner := func(RunContext) (string, string, *RunResult, error) {
		t.Error("admitted after stop")
		return "success", "", nil, nil
	}
	s.Start(runner)
	s.tick(runner)
	if s.AnyInFlight() {
		t.Fatal("admitted after stop")
	}
}

func TestSchedulerPanicAndMissingAgentReleaseLanes(t *testing.T) {
	s := concurrencyScheduler(t, "a", "b")
	s.maxConcurrent = 1
	defer s.Stop()
	// a is first and missing at dispatch time (without registry reconciliation).
	if err := s.reg.Delete("a"); err != nil {
		t.Fatal(err)
	}
	runner := func(RunContext) (string, string, *RunResult, error) { panic("runner panic") }
	s.tick(runner)
	waitSchedulerIdle(t, s)
	// Reconciliation removes the missing job without releasing a live lane.
	s.ReconcileWithRegistry()
	s.mu.Lock()
	for _, j := range s.jobs {
		j.NextRun = time.Time{}
	}
	s.mu.Unlock()
	s.tick(runner)
	waitSchedulerIdle(t, s)
	for _, j := range s.ListJobs() {
		if j.InFlight || j.RunCount != 1 {
			t.Fatalf("panic not finalized: %+v", j)
		}
	}
}

func TestSchedulerBusyLaneDoesNotConsumeBreakerProbe(t *testing.T) {
	s := concurrencyScheduler(t, "a")
	defer s.Stop()
	s.cbStore = NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1, Cooldown: time.Nanosecond})
	cb := s.cbStore.Get("a")
	cb.RecordFailure()
	// Both same-agent exclusion and global saturation must precede Allowed.
	for _, name := range []string{"a", "other"} {
		s.maxConcurrent = 1
		if name == "a" {
			s.maxConcurrent = 2
		}
		s.activeAgents[name] = true
		s.tick(func(RunContext) (string, string, *RunResult, error) {
			t.Error("busy lane dispatched")
			return "success", "", nil, nil
		})
		delete(s.activeAgents, name)
	}
	if !cb.Allow() {
		t.Fatal("busy tick consumed the half-open probe")
	}
	cb.RecordSuccess()
}

func TestSchedulerConcurrencyConfig(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 3}, {-1, 1}, {1, 1}, {2, 2}, {100, 12}} {
		s := NewScheduler(SchedulerConfig{MaxConcurrent: tc.in})
		if s.maxConcurrent != tc.want {
			t.Errorf("MaxConcurrent(%d)=%d, want %d", tc.in, s.maxConcurrent, tc.want)
		}
		s.Stop()
	}
}

func TestSchedulerIdleOnlyAfterAllWorkers(t *testing.T) {
	s := concurrencyScheduler(t, "a", "b")
	defer s.Stop()
	idle := make(chan struct{}, 2)
	s.onCycleIdle = func() { idle <- struct{}{} }
	started := make(chan string, 2)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	runner := func(rc RunContext) (string, string, *RunResult, error) {
		started <- rc.AgentName
		release := releaseA
		if rc.AgentName == "b" {
			release = releaseB
		}
		select {
		case <-release:
		case <-rc.Context.Done():
		}
		return "success", "", nil, nil
	}
	s.tick(runner)
	receiveAgent(t, started)
	receiveAgent(t, started)
	close(releaseA)
	deadline := time.After(2 * time.Second)
	for {
		s.mu.RLock()
		active := s.activeAgents["a"]
		s.mu.RUnlock()
		if !active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("a did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case <-idle:
		t.Fatal("idle callback fired with b still active")
	default:
	}
	if !s.AnyInFlight() {
		t.Fatal("b disappeared from deploy guard")
	}
	close(releaseB)
	select {
	case <-idle:
	case <-time.After(2 * time.Second):
		t.Fatal("last worker did not signal idle")
	}
}

func TestSchedulerConcurrentDueAgents(t *testing.T) {
	s := concurrencyScheduler(t, "long", "short")
	started := make(chan string, 10)
	release := make(chan struct{})
	runner := func(rc RunContext) (string, string, *RunResult, error) {
		started <- rc.AgentName
		select {
		case <-release:
		case <-rc.Context.Done():
		}
		return "success", "", nil, nil
	}
	tickDone := make(chan struct{})
	go func() { s.tick(runner); close(tickDone) }()
	defer func() { close(release); <-tickDone; s.Stop() }()
	first := receiveAgent(t, started)
	second := receiveAgent(t, started)
	if first == second {
		t.Fatal("same agent overlapped")
	}
	select {
	case <-tickDone:
	case <-time.After(time.Second):
		t.Fatal("tick waited for runners")
	}
	// Repeated ticks cannot admit the same still-due jobs again.
	for range 10 {
		s.tick(runner)
	}
	select {
	case n := <-started:
		t.Fatalf("duplicate run: %s", n)
	default:
	}
}

func TestSchedulerStopCancelsAndDrains(t *testing.T) {
	s := concurrencyScheduler(t, "long")
	started := make(chan string, 1)
	canceled := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		s.Start(func(rc RunContext) (string, string, *RunResult, error) {
			started <- rc.AgentName
			<-rc.Context.Done()
			close(canceled)
			<-release
			return "failure", "", nil, context.Canceled
		})
		close(done)
	}()
	receiveAgent(t, started)
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	defer close(release)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel runner")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned before runner drained")
	default:
	}
	// Allow the runner to finish without closing the release channel twice.
	release <- struct{}{}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop failed to drain")
	}
	<-done
	s.Stop() // idempotent
	if s.AnyInFlight() {
		t.Fatal("in-flight state leaked after shutdown")
	}
}
