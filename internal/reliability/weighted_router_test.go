package reliability

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// ─── Routing Strategy Tests ──────────────────────────────────────────────────

func TestRoutingStrategy_Defaults(t *testing.T) {
	router := NewAgentRouter()
	if router.Strategy() != RoutingRoundRobin {
		t.Errorf("default strategy should be round_robin, got %s", router.Strategy())
	}
}

func TestRoutingStrategy_SetAndGet(t *testing.T) {
	router := NewAgentRouter()
	router.SetStrategy(RoutingLeastConnections)
	if router.Strategy() != RoutingLeastConnections {
		t.Errorf("expected least_connections, got %s", router.Strategy())
	}

	router.SetStrategy(RoutingRoundRobin)
	if router.Strategy() != RoutingRoundRobin {
		t.Errorf("expected round_robin, got %s", router.Strategy())
	}
}

func TestRoutingStrategy_String(t *testing.T) {
	if s := RoutingRoundRobin.String(); s != "round_robin" {
		t.Errorf("unexpected: %q", s)
	}
	if s := RoutingLeastConnections.String(); s != "least_connections" {
		t.Errorf("unexpected: %q", s)
	}
	if s := RoutingStrategy(99).String(); s != "unknown(99)" {
		t.Errorf("unexpected: %q", s)
	}
}

// ─── Least-Connections Routing Tests ─────────────────────────────────────────

func TestLeastConnections_PicksExecutorWithFewestActive(t *testing.T) {
	var mu sync.Mutex
	callCount := map[string]int{}
	makeExec := func(name string) *LocalExecutor {
		return NewLocalExecutor(name, func(agent, task string) (*AgentResult, error) {
			mu.Lock()
			callCount[name]++
			mu.Unlock()
			// Simulate variable processing time so active counts matter
			time.Sleep(10 * time.Millisecond)
			return &AgentResult{Agent: agent, Task: task, Success: true}, nil
		})
	}

	e1 := makeExec("e1")
	e2 := makeExec("e2")
	e3 := makeExec("e3")

	router := NewAgentRouter(e1, e2, e3)
	router.SetStrategy(RoutingLeastConnections)

	// Execute many tasks concurrently to exercise least-connections distribution.
	// With least-connections, each task should go to the executor with fewest
	// in-flight requests, resulting in roughly even distribution.
	var wg sync.WaitGroup
	n := 30
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			router.Execute("agent", "task")
		}()
	}
	wg.Wait()

	// All executors should have been used (each got some tasks).
	mu.Lock()
	e1Count := callCount["e1"]
	e2Count := callCount["e2"]
	e3Count := callCount["e3"]
	mu.Unlock()

	if e1Count == 0 || e2Count == 0 || e3Count == 0 {
		t.Errorf("least-connections should distribute across all executors, got e1=%d e2=%d e3=%d",
			e1Count, e2Count, e3Count)
	}

	// Distribution should be reasonably even (within 2x of each other).
	max := e1Count
	min := e1Count
	for _, c := range []int{e2Count, e3Count} {
		if c > max {
			max = c
		}
		if c < min {
			min = c
		}
	}
	if max > 2*min {
		t.Errorf("least-connections should balance load, got e1=%d e2=%d e3=%d (max=%d min=%d)",
			callCount["e1"], callCount["e2"], callCount["e3"], max, min)
	}
}

func TestLeastConnections_SkipsUnhealthy(t *testing.T) {
	callCount := map[string]int{}
	healthy := NewLocalExecutor("healthy", func(agent, task string) (*AgentResult, error) {
		callCount["healthy"]++
		return &AgentResult{Success: true}, nil
	})
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })

	router := NewAgentRouter(unhealthy, healthy)
	router.SetStrategy(RoutingLeastConnections)

	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if callCount["unhealthy"] > 0 {
		t.Error("unhealthy executor should never receive tasks")
	}
	if callCount["healthy"] != 1 {
		t.Errorf("healthy executor should receive the task, got %d calls", callCount["healthy"])
	}
}

func TestLeastConnections_AllUnhealthyFallsBackToLocal(t *testing.T) {
	unhealthy1 := NewLocalExecutor("u1", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	unhealthy2 := NewLocalExecutor("u2", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local-fallback"}, nil
	})

	router := NewAgentRouter(unhealthy1, unhealthy2)
	router.SetStrategy(RoutingLeastConnections)
	router.SetLocal(local)

	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected local fallback, got error: %v", err)
	}
	if result.Output != "local-fallback" {
		t.Errorf("expected local-fallback, got %q", result.Output)
	}
}

func TestLeastConnections_ActiveCounts(t *testing.T) {
	router := NewAgentRouter()
	// Before setting strategy, activeCounts should be nil.
	if counts := router.ActiveCounts(); counts != nil {
		t.Errorf("expected nil activeCounts before strategy set, got %v", counts)
	}

	router.SetStrategy(RoutingLeastConnections)
	// After setting strategy but no executors, activeCounts should be empty.
	counts := router.ActiveCounts()
	if len(counts) != 0 {
		t.Errorf("expected empty activeCounts with no executors, got %v", counts)
	}

	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	router.Add(e1)
	router.Add(e2)

	counts = router.ActiveCounts()
	if len(counts) != 2 {
		t.Fatalf("expected 2 active counts, got %d", len(counts))
	}
	if counts[0] != 0 || counts[1] != 0 {
		t.Errorf("fresh executors should have 0 active, got [%d, %d]", counts[0], counts[1])
	}
}

func TestLeastConnections_TieGoesToFirst(t *testing.T) {
	// When all executors have equal active counts, the first healthy one should win.
	callOrder := []string{}
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		callOrder = append(callOrder, "e1")
		time.Sleep(20 * time.Millisecond) // hold the connection
		return &AgentResult{Success: true}, nil
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		callOrder = append(callOrder, "e2")
		time.Sleep(20 * time.Millisecond)
		return &AgentResult{Success: true}, nil
	})

	router := NewAgentRouter(e1, e2)
	router.SetStrategy(RoutingLeastConnections)

	// Sequential calls should alternate because each call holds a connection.
	// First call: both have 0 active → picks e1 (first). e1 active=1.
	// Second call: e1 active=1, e2 active=0 → picks e2.
	_, err := router.Execute("agent", "task1")
	if err != nil {
		t.Fatalf("call 1: %v", err)
	}
	// Wait for the first request to complete (active count decremented).
	time.Sleep(30 * time.Millisecond)

	_, err = router.Execute("agent", "task2")
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if callOrder[0] != "e1" {
		t.Errorf("first call should go to e1 (first with 0 active), got %q", callOrder[0])
	}
	if callOrder[1] != "e1" {
		t.Errorf("second call should go to e1 (both at 0 after first completes), got %q", callOrder[1])
	}
}

func TestLeastConnections_FailoverToNextHealthy(t *testing.T) {
	// When the least-connections executor's Execute() fails, router should
	// failover to the next healthy executor (second-least connections).
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "from e2"}, nil
	})

	router := NewAgentRouter(e1, e2)
	router.SetStrategy(RoutingLeastConnections)

	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected failover to e2, got error: %v", err)
	}
	if result.Output != "from e2" {
		t.Errorf("expected output from e2, got %q", result.Output)
	}
}

func TestLeastConnections_RespectsMaxFailover(t *testing.T) {
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("e2 failed")
	})
	e3 := NewLocalExecutor("e3", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "from e3"}, nil
	})

	router := NewAgentRouter(e1, e2, e3)
	router.SetStrategy(RoutingLeastConnections)
	router.MaxFailover = 2 // only try 2 executors

	// Start is least-connections → e1 (both at 0).
	// e1 fails → tries e2 (failover).
	// e2 fails → MaxFailover=2 reached, so e3 is NOT tried.
	_, err := router.Execute("agent", "task")
	if err == nil {
		t.Error("expected error when MaxFailover exhausted before reaching healthy executor")
	}
}

func TestLeastConnections_DynamicExecutorAddition(t *testing.T) {
	router := NewAgentRouter()
	router.SetStrategy(RoutingLeastConnections)

	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	router.Add(e1)
	if len(router.ActiveCounts()) != 1 {
		t.Errorf("expected 1 active count after adding executor, got %d", len(router.ActiveCounts()))
	}

	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	router.Add(e2)
	if len(router.ActiveCounts()) != 2 {
		t.Errorf("expected 2 active counts after adding second executor, got %d", len(router.ActiveCounts()))
	}

	// Existing active counts should be preserved.
	counts := router.ActiveCounts()
	if counts[0] != 0 {
		t.Errorf("e1 active count should be 0, got %d", counts[0])
	}
}

func TestWeightedRouter_StrategyPersistence(t *testing.T) {
	// Switching strategies should not lose state.
	router := NewAgentRouter()
	router.SetStrategy(RoutingLeastConnections)
	if router.Strategy() != RoutingLeastConnections {
		t.Fatal("expected least_connections")
	}
	router.SetStrategy(RoutingRoundRobin)
	if router.Strategy() != RoutingRoundRobin {
		t.Fatal("expected round_robin")
	}
	// activeCounts should survive strategy switch
	router.SetStrategy(RoutingLeastConnections)
	if router.Strategy() != RoutingLeastConnections {
		t.Fatal("expected least_connections after switch back")
	}
}
