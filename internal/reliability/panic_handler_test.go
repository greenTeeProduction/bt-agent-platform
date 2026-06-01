package reliability

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSafeGo_RecoversPanic(t *testing.T) {
	var handlerCalled bool
	var handlerVal any
	var handlerCtx string

	wg := sync.WaitGroup{}
	wg.Add(1)

	SafeGo("test-goroutine", func() {
		defer wg.Done()
		panic("boom")
	}, func(v any, ctx string) {
		handlerCalled = true
		handlerVal = v
		handlerCtx = ctx
	})

	wg.Wait()
	time.Sleep(10 * time.Millisecond) // let handler goroutine run

	if !handlerCalled {
		t.Error("handler was not called after panic")
	}
	if handlerVal != "boom" {
		t.Errorf("handler received wrong panic value: %v", handlerVal)
	}
	if handlerCtx != "test-goroutine" {
		t.Errorf("handler received wrong context: %q", handlerCtx)
	}
}

func TestSafeGo_NormalExecution(t *testing.T) {
	var ran bool
	wg := sync.WaitGroup{}
	wg.Add(1)

	SafeGo("normal", func() {
		defer wg.Done()
		ran = true
	}, nil)

	wg.Wait()
	if !ran {
		t.Error("function did not run")
	}
}

func TestSafeGo_HandlerPanicIsRecovered(t *testing.T) {
	// If the panic handler itself panics, it should be recovered.
	wg := sync.WaitGroup{}
	wg.Add(1)

	SafeGo("handler-panic", func() {
		defer wg.Done()
		panic("original panic")
	}, func(v any, ctx string) {
		panic("handler crashed too")
	})

	wg.Wait()
	time.Sleep(10 * time.Millisecond)
	// Should not crash the test — handler's panic is recovered by the
	// inner defer in SafeGo.
}

func TestRecover_CatchesPanic(t *testing.T) {
	err := Recover("test-recover", func() {
		panic("caught")
	})
	if err == nil {
		t.Error("expected error from panic, got nil")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRecover_NoPanic(t *testing.T) {
	err := Recover("no-panic", func() {
		// no panic
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRecover_ReturnsErrorWithContext(t *testing.T) {
	err := Recover("my-context", func() {
		panic("my error")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Should contain the context string
	if !strings.Contains(err.Error(), "my-context") {
		t.Errorf("error should contain context: %v", err)
	}
}

func TestWorkerPool_PanicRecovery(t *testing.T) {
	wp := NewWorkerPool(1)
	var completed bool

	// Submit a task that panics
	ok := wp.Submit(func() {
		panic("task-explosion")
	})
	if !ok {
		t.Fatal("Submit returned false")
	}

	// Submit a normal task after the panicking one
	ok = wp.Submit(func() {
		completed = true
	})
	if !ok {
		t.Fatal("Submit for normal task returned false")
	}

	// Wait for both tasks to process
	time.Sleep(100 * time.Millisecond)
	wp.Shutdown()

	if !completed {
		t.Error("worker died after first task panicked — normal task never executed")
	}

	_, _, total, comp := wp.Stats()
	if total < 2 {
		t.Errorf("expected at least 2 tasks submitted, got %d", total)
	}
	// The panicking task still counts as completed (we recovered and kept going)
	if comp < 2 {
		t.Errorf("expected at least 2 tasks completed (including panicked), got %d", comp)
	}
}

func TestWorkerPool_MultiplePanics(t *testing.T) {
	wp := NewWorkerPool(1)
	count := 0

	// Submit 3 tasks, all panic
	for i := 0; i < 3; i++ {
		wp.Submit(func() {
			panic("repeated")
		})
	}
	// Submit a normal task
	wp.Submit(func() {
		count++
	})

	time.Sleep(200 * time.Millisecond)
	wp.Shutdown()

	if count != 1 {
		t.Errorf("expected normal task to run once, got %d (worker likely died)", count)
	}
}
