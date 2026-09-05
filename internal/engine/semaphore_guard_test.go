package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestSemaphoreGuard_LimitsConcurrentEntry(t *testing.T) {
	inside := 0
	maxInside := 0
	release := make(chan struct{})
	gotInside := make(chan struct{})
	RegisterAction("TestSemBody", func(_ *btcore.BTContext[Blackboard]) int {
		inside++
		if inside > maxInside {
			maxInside = inside
		}
		close(gotInside)
		<-release
		inside--
		return 1
	})
	mk := func(name string) btcore.Command[Blackboard] {
		bb := newTestBlackboard()
		return buildNode(&evolution.SerializableNode{
			Type: "SemaphoreGuard", Name: "SG_" + name,
			Metadata: map[string]any{"semaphore": "test-sem", "permits": 1},
			Children: []evolution.SerializableNode{{Type: "Action", Name: "TestSemBody"}},
		}, bb, "")
	}
	c1, c2 := mk("a"), mk("b")
	done1 := make(chan int)
	go func() { done1 <- c1.Run(newTestBTContext(newTestBlackboard())) }()
	// Wait for first entrant to acquire semaphore and start action
	<-gotInside
	// second guard must yield RUNNING immediately while first holds the permit
	got2 := c2.Run(newTestBTContext(newTestBlackboard()))
	if got2 != 0 {
		t.Fatalf("second entrant: want RUNNING(0), got %d", got2)
	}
	close(release)
	if got1 := <-done1; got1 != 1 {
		t.Fatalf("first entrant: want SUCCESS, got %d", got1)
	}
	if maxInside != 1 {
		t.Fatalf("permits=1 violated: max concurrent inside = %d", maxInside)
	}
}
