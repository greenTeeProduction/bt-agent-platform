package reliability

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fullBlockedPool(t *testing.T) (*WorkerPool, func(), *atomic.Int64) {
	t.Helper()
	pool := NewWorkerPool(1)
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(func() { release(); pool.Shutdown() })
	completed := &atomic.Int64{}
	if !pool.Submit(func() { close(started); <-gate; completed.Add(1) }) {
		t.Fatal("initial admission rejected")
	}
	<-started
	for range cap(pool.tasks) {
		if !pool.Submit(func() { completed.Add(1) }) {
			t.Fatal("queued admission rejected")
		}
	}
	return pool, release, completed
}

func TestWorkerPoolDeadlineRejectsFullQueueWithoutAdmission(t *testing.T) {
	pool, release, completed := fullBlockedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := pool.SubmitWithContext(ctx, func() { t.Error("rejected task executed") }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submission error=%v", err)
	}
	_, queued, total, _ := pool.Stats()
	if queued != 100 || total != 101 {
		t.Fatalf("rejection changed admission: queued=%d total=%d", queued, total)
	}
	release()
	pool.Shutdown()
	if completed.Load() != 101 {
		t.Fatal("shutdown lost admitted work")
	}
}

func TestWorkerPoolShutdownUnblocksFullQueueSubmitter(t *testing.T) {
	pool, release, completed := fullBlockedPool(t)
	result := make(chan error, 1)
	go func() {
		result <- pool.SubmitWithContext(context.Background(), func() { t.Error("rejected work ran") })
	}()
	shutdown := make(chan struct{})
	go func() { pool.Shutdown(); close(shutdown) }()
	<-pool.quit
	select {
	case err := <-result:
		if !errors.Is(err, ErrWorkerPoolClosed) {
			t.Fatalf("shutdown rejection=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown could not wake blocked admission")
	}
	select {
	case <-shutdown:
		t.Fatal("shutdown returned before admitted work finished")
	default:
	}
	release()
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown failed to drain admitted work")
	}
	if completed.Load() != 101 {
		t.Fatal("shutdown lost accepted work")
	}
}

func TestAdmissionPreCanceledContextConsumesNoCapacity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool := NewWorkerPool(1)
	defer pool.Shutdown()
	if err := pool.SubmitWithContext(ctx, func() { t.Error("canceled task executed") }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, queued, total, _ := pool.Stats()
	if queued != 0 || total != 0 {
		t.Fatal("canceled submission was admitted")
	}
	if err := pool.SubmitWithContext(context.Background(), nil); !errors.Is(err, ErrNilWorkerTask) {
		t.Fatal(err)
	}
	limiter := NewConcurrencyLimiter(1)
	if err := limiter.AcquireWithContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	active, waiting, acquired := limiter.Stats()
	if active != 0 || waiting != 0 || acquired != 0 || limiter.Available() != 1 {
		t.Fatal("canceled reservation consumed capacity")
	}
}

func TestLimiterDeadlineRetainsExistingOwnerAndRecoversCapacity(t *testing.T) {
	limiter := NewConcurrencyLimiter(1)
	limiter.Acquire()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := limiter.AcquireWithContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	active, waiting, total := limiter.Stats()
	if active != 1 || waiting != 0 || total != 1 {
		t.Fatalf("wrong terminal accounting: %d %d %d", active, waiting, total)
	}
	limiter.Release()
	if !limiter.TryAcquire() {
		t.Fatal("canceled waiter leaked capacity")
	}
	limiter.Release()
}

func TestLimiterConcurrentReservationsBalance(t *testing.T) {
	limiter := NewConcurrencyLimiter(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			for range 50 {
				if err := limiter.AcquireWithContext(ctx); err != nil {
					t.Error(err)
					return
				}
				limiter.Release()
			}
		})
	}
	workers.Wait()
	active, waiting, total := limiter.Stats()
	if active != 0 || waiting != 0 || total != 1000 || limiter.Available() != 2 {
		t.Fatalf("unbalanced reservations: %d %d %d available=%d", active, waiting, total, limiter.Available())
	}
	if NewConcurrencyLimiter(0).Capacity() != 1 || NewConcurrencyLimiter(-1).Capacity() != 1 {
		t.Fatal("invalid capacity did not retain a usable slot")
	}
}
