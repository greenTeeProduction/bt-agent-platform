package engine

import (
	"sync"
	"testing"
	"time"
)

func TestSLOMetrics_RecordSuccess(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	m.RecordSuccess(100 * time.Millisecond)
	m.RecordSuccess(200 * time.Millisecond)

	if m.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d, want 2", m.TotalCalls)
	}
	if m.SuccessfulCalls != 2 {
		t.Errorf("SuccessfulCalls = %d, want 2", m.SuccessfulCalls)
	}
	if m.FailedCalls != 0 {
		t.Errorf("FailedCalls = %d, want 0", m.FailedCalls)
	}
	if m.TotalLatencyMs != 300 {
		t.Errorf("TotalLatencyMs = %d, want 300", m.TotalLatencyMs)
	}
	if m.MaxLatencyMs != 200 {
		t.Errorf("MaxLatencyMs = %d, want 200", m.MaxLatencyMs)
	}
}

func TestSLOMetrics_RecordFailure(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	m.RecordFailure(50 * time.Millisecond)
	m.RecordFailure(150 * time.Millisecond)

	if m.TotalCalls != 2 {
		t.Errorf("TotalCalls = %d, want 2", m.TotalCalls)
	}
	if m.FailedCalls != 2 {
		t.Errorf("FailedCalls = %d, want 2", m.FailedCalls)
	}
	if m.SuccessfulCalls != 0 {
		t.Errorf("SuccessfulCalls = %d, want 0", m.SuccessfulCalls)
	}
	if m.TotalLatencyMs != 200 {
		t.Errorf("TotalLatencyMs = %d, want 200", m.TotalLatencyMs)
	}
}

func TestSLOMetrics_RecordRecovery(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	m.RecordRecovery(75 * time.Millisecond)

	if m.RecoveredCalls != 1 {
		t.Errorf("RecoveredCalls = %d, want 1", m.RecoveredCalls)
	}
	// Recovery should NOT increment TotalCalls or FailedCalls.
	if m.TotalCalls != 0 {
		t.Errorf("TotalCalls = %d, want 0 (recovery shouldn't increment TotalCalls)", m.TotalCalls)
	}
	if m.FailedCalls != 0 {
		t.Errorf("FailedCalls = %d, want 0", m.FailedCalls)
	}
	if m.TotalLatencyMs != 75 {
		t.Errorf("TotalLatencyMs = %d, want 75", m.TotalLatencyMs)
	}
}

func TestSLOMetrics_FailureAndRecovery(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	// Simulate: 2 failures, 1 recovery
	m.RecordFailure(50 * time.Millisecond)
	m.RecordFailure(50 * time.Millisecond)
	m.RecordRecovery(30 * time.Millisecond)

	if m.FailedCalls != 2 {
		t.Errorf("FailedCalls = %d, want 2", m.FailedCalls)
	}
	if m.RecoveredCalls != 1 {
		t.Errorf("RecoveredCalls = %d, want 1", m.RecoveredCalls)
	}

	rate := m.RecoveryRate()
	expected := 0.5
	if rate != expected {
		t.Errorf("RecoveryRate = %f, want %f", rate, expected)
	}
}

func TestSLOMetrics_SuccessRate(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	// No calls yet: success rate should be 1.0 (default)
	if rate := m.SuccessRate(); rate != 1.0 {
		t.Errorf("SuccessRate with zero calls = %f, want 1.0", rate)
	}

	// 3 successes, 1 failure
	m.RecordSuccess(10 * time.Millisecond)
	m.RecordSuccess(10 * time.Millisecond)
	m.RecordSuccess(10 * time.Millisecond)
	m.RecordFailure(10 * time.Millisecond)

	rate := m.SuccessRate()
	if rate != 0.75 {
		t.Errorf("SuccessRate = %f, want 0.75", rate)
	}
}

func TestSLOMetrics_RecoveryRate_ZeroFailed(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	// No failed calls: recovery rate should be 0
	if rate := m.RecoveryRate(); rate != 0 {
		t.Errorf("RecoveryRate with zero failed calls = %f, want 0", rate)
	}

	// Only successes, no failures
	m.RecordSuccess(10 * time.Millisecond)
	m.RecordSuccess(10 * time.Millisecond)
	if rate := m.RecoveryRate(); rate != 0 {
		t.Errorf("RecoveryRate with successes only = %f, want 0", rate)
	}
}

func TestSLOMetrics_AvgLatencyMs(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	// No calls
	if avg := m.AvgLatencyMs(); avg != 0 {
		t.Errorf("AvgLatencyMs with zero calls = %f, want 0", avg)
	}

	m.RecordSuccess(100 * time.Millisecond)
	m.RecordSuccess(300 * time.Millisecond)
	m.RecordFailure(200 * time.Millisecond)

	avg := m.AvgLatencyMs()
	if avg != 200.0 {
		t.Errorf("AvgLatencyMs = %f, want 200.0", avg)
	}
}

func TestSLOMetrics_Summary(t *testing.T) {
	m := &SLOMetrics{AgentName: "TestAgent", TreeName: "test_tree"}

	m.RecordSuccess(100 * time.Millisecond)
	m.RecordFailure(200 * time.Millisecond)

	summary := m.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
	// Check it contains agent name and tree name
	if !stringsContains(summary, "TestAgent") || !stringsContains(summary, "test_tree") {
		t.Errorf("Summary missing agent/tree info: %s", summary)
	}
}

func TestGetSLOMetrics_SameInstance(t *testing.T) {
	// GetSLOMetrics should return the same instance for the same key
	m1 := GetSLOMetrics("agentA", "treeX")
	m2 := GetSLOMetrics("agentA", "treeX")

	if m1 != m2 {
		t.Error("GetSLOMetrics should return the same instance for the same key")
	}

	// Mutate through m1, observe through m2
	m1.RecordSuccess(50 * time.Millisecond)
	if m2.TotalCalls != 1 {
		t.Errorf("m2.TotalCalls = %d, want 1 (should reflect mutation via m1)", m2.TotalCalls)
	}

	// Different agent/tree should return a different instance
	m3 := GetSLOMetrics("agentB", "treeY")
	if m1 == m3 {
		t.Error("GetSLOMetrics should return different instances for different keys")
	}
}

func TestAllSLOMetrics(t *testing.T) {
	// Register multiple metrics
	GetSLOMetrics("agent1", "tree1")
	GetSLOMetrics("agent2", "tree2")
	GetSLOMetrics("agent3", "tree1")

	all := AllSLOMetrics()

	if len(all) < 3 {
		t.Errorf("AllSLOMetrics should have at least 3 entries, got %d", len(all))
	}

	// Check keys
	found := make(map[string]bool)
	for k := range all {
		found[k] = true
	}
	if !found["agent1:tree1"] {
		t.Error("missing agent1:tree1 in AllSLOMetrics")
	}
	if !found["agent2:tree2"] {
		t.Error("missing agent2:tree2 in AllSLOMetrics")
	}
	if !found["agent3:tree1"] {
		t.Error("missing agent3:tree1 in AllSLOMetrics")
	}
}

func TestSLOMetrics_Concurrency(t *testing.T) {
	m := &SLOMetrics{AgentName: "concurrent", TreeName: "test"}

	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.RecordSuccess(10 * time.Millisecond)
		}()
	}
	wg.Wait()

	if m.TotalCalls != int64(n) {
		t.Errorf("TotalCalls = %d, want %d (concurrent)", m.TotalCalls, n)
	}
	if m.SuccessfulCalls != int64(n) {
		t.Errorf("SuccessfulCalls = %d, want %d (concurrent)", m.SuccessfulCalls, n)
	}
}

func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Summary used to take RLock and then call SuccessRate/RecoveryRate/AvgLatencyMs,
// which RLock again. sync.RWMutex blocks recursive RLock when a writer is
// queued between the two acquisitions, deadlocking reader and writer.
func TestSLOMetrics_Summary_NoDeadlockWithConcurrentWriters(t *testing.T) {
	m := &SLOMetrics{AgentName: "agent1", TreeName: "main"}

	stop := make(chan struct{})
	var writers sync.WaitGroup
	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					m.RecordSuccess(time.Millisecond)
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			_ = m.Summary()
		}
		close(done)
	}()

	select {
	case <-done:
		close(stop)
		writers.Wait()
	case <-time.After(10 * time.Second):
		t.Fatal("Summary deadlocked with concurrent writers (recursive RLock)")
	}
}
