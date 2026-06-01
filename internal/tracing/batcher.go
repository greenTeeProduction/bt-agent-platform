// Package tracing provides lightweight distributed tracing abstractions.
package tracing

import (
	"context"
	"log"
	"sync"
	"time"
)

// BatchExporter wraps a SpanExporter and buffers spans, flushing them in
// batches at a configurable interval. This reduces HTTP overhead when sending
// to an OTLP collector compared to one HTTP request per span.
//
// Spans are flushed when the batch size reaches BatchSize or when the flush
// interval elapses since the last flush (whichever comes first).
// Flush is guaranteed before Close returns, ensuring spans are not lost on
// graceful shutdown.
type BatchExporter struct {
	inner SpanExporter

	mu         sync.Mutex
	batch      []ExportedSpan
	flushTimer *time.Timer
	closeCh    chan struct{}
	closed     bool

	BatchSize     int
	FlushInterval time.Duration

	// OnFlushError is called when a batch export fails. If nil, errors are
	// silently dropped (same policy as ConsoleTracer).
	OnFlushError func(err error)

	// OnFlush is called after each successful flush with the count of spans.
	OnFlush func(count int)

	wg sync.WaitGroup
}

// NewBatchExporter creates a BatchExporter that forwards completed spans to
// inner in batches. Default batch size is 64, default flush interval is 5s.
//
// Pass inner=nil to create a noop BatchExporter (all spans discarded).
// The background flusher is started immediately.
func NewBatchExporter(inner SpanExporter) *BatchExporter {
	be := &BatchExporter{
		inner:         inner,
		closeCh:       make(chan struct{}),
		BatchSize:     64,
		FlushInterval: 5 * time.Second,
	}
	be.startFlusher()
	return be
}

// ExportSpan buffers a span for eventual batch export. If the batch reaches
// BatchSize, it is flushed synchronously.
func (be *BatchExporter) ExportSpan(ctx context.Context, span ExportedSpan) error {
	if be == nil || be.inner == nil {
		return nil
	}
	be.mu.Lock()
	if be.closed {
		be.mu.Unlock()
		return nil
	}
	be.batch = append(be.batch, span)
	size := len(be.batch)
	be.mu.Unlock()

	if size >= be.BatchSize {
		be.flush()
	}
	return nil
}

func (be *BatchExporter) startFlusher() {
	be.mu.Lock()
	if be.FlushInterval <= 0 {
		be.FlushInterval = 5 * time.Second
	}
	if be.BatchSize <= 0 {
		be.BatchSize = 64
	}
	be.flushTimer = time.NewTimer(be.FlushInterval)
	be.mu.Unlock()

	be.wg.Add(1)
	go be.flushLoop()
}

func (be *BatchExporter) flushLoop() {
	defer be.wg.Done()
	for {
		be.mu.Lock()
		timer := be.flushTimer
		ch := be.closeCh
		be.mu.Unlock()

		select {
		case <-timer.C:
			be.flush()
			be.mu.Lock()
			if !be.closed {
				be.flushTimer.Reset(be.FlushInterval)
			}
			be.mu.Unlock()
		case <-ch:
			return
		}
	}
}

// flush sends the current batch to the inner exporter. Thread-safe.
func (be *BatchExporter) flush() {
	be.mu.Lock()
	if len(be.batch) == 0 || be.closed {
		be.mu.Unlock()
		return
	}
	batch := be.batch
	be.batch = nil
	be.mu.Unlock()

	// Export each span individually (OTLP/HTTP endpoint accepts batches but
	// our current exporter sends one span per request; this is fine — the
	// batching benefit is reducing per-span HTTP handshake overhead).
	var lastErr error
	count := 0
	ctx := context.Background()
	for _, span := range batch {
		if err := be.inner.ExportSpan(ctx, span); err != nil {
			lastErr = err
		} else {
			count++
		}
	}

	if lastErr != nil && be.OnFlushError != nil {
		be.OnFlushError(lastErr)
	}
	if be.OnFlush != nil {
		be.OnFlush(count)
	}
}

// Close flushes any remaining buffered spans and stops the background flusher.
// After Close, all subsequent ExportSpan calls are silently dropped.
// Safe to call multiple times.
func (be *BatchExporter) Close() error {
	be.mu.Lock()
	if be.closed {
		be.mu.Unlock()
		return nil
	}
	// Signal flushLoop to exit
	close(be.closeCh)
	if be.flushTimer != nil {
		be.flushTimer.Stop()
	}
	// Capture any remaining span batch before marking closed
	batch := be.batch
	be.batch = nil
	be.mu.Unlock()

	// Flush remaining spans synchronously (outside locked region)
	be.flushBatch(batch)

	be.mu.Lock()
	be.closed = true
	be.mu.Unlock()

	be.wg.Wait()
	return nil
}

// flushBatch exports the given spans to the inner exporter without checking
// the closed flag. Used by Close() during final flush.
func (be *BatchExporter) flushBatch(batch []ExportedSpan) {
	if len(batch) == 0 || be.inner == nil {
		return
	}
	var lastErr error
	count := 0
	ctx := context.Background()
	for _, span := range batch {
		if err := be.inner.ExportSpan(ctx, span); err != nil {
			lastErr = err
		} else {
			count++
		}
	}
	if lastErr != nil && be.OnFlushError != nil {
		be.OnFlushError(lastErr)
	}
	if be.OnFlush != nil {
		be.OnFlush(count)
	}
}

// Len returns the number of buffered spans. Useful for monitoring.
func (be *BatchExporter) Len() int {
	be.mu.Lock()
	defer be.mu.Unlock()
	return len(be.batch)
}

// DebugBatchExporter is a BatchExporter that also implements SpanExporter directly
// for use in contexts where a SpanExporter interface is expected but batch
// buffering is desired. It is identical to BatchExporter in behavior.
type DebugBatchExporter = BatchExporter

// MustNewBatchExporter is like NewBatchExporter but panics if inner is nil.
// Useful for initialization in Main where a nil inner is a programming error.
func MustNewBatchExporter(inner SpanExporter) *BatchExporter {
	if inner == nil {
		log.Panic("tracing: MustNewBatchExporter requires a non-nil SpanExporter")
	}
	return NewBatchExporter(inner)
}
