package reliability

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestReviewWorkerShutdownDrains(t *testing.T) {
	wp := NewWorkerPool(1)
	var done atomic.Int64
	gate := make(chan struct{})
	wp.Submit(func() { <-gate; done.Add(1) })
	for range 50 {
		if !wp.Submit(func() { done.Add(1) }) {
			t.Fatal("submit rejected")
		}
	}
	close(gate)
	wp.Shutdown()
	if done.Load() != 51 {
		t.Fatalf("executed %d of 51 accepted tasks", done.Load())
	}
}
func TestReviewWorkerSubmitShutdown(t *testing.T) {
	wp := NewWorkerPool(2)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("Submit panicked: %v", p)
				}
			}()
			for range 1000 {
				wp.Submit(func() {})
			}
		})
	}
	wp.Shutdown()
	wg.Wait()
	for range 100 {
		if wp.Submit(func() {}) {
			t.Error("accepted after shutdown")
		}
	}
	wp.Shutdown()
}
