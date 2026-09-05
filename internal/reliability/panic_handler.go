// Package reliability provides panic recovery utilities for goroutines
// in the BT platform. All background goroutines should use these to ensure
// a single panic doesn't take down the entire system.
package reliability

import (
	"fmt"
	"log/slog"
)

// PanicHandler is called when a goroutine panics. It receives the panic value
// and a context string identifying which goroutine panicked.
type PanicHandler func(panicVal any, context string)

// DefaultPanicHandler logs the panic and stack trace to the default logger.
// It is used by SafeGo when no custom handler is provided.
func DefaultPanicHandler(panicVal any, context string) {
	slog.Error("panic recovered", "context", context, "panic", panicVal)
}

// SafeGo runs fn in a goroutine with panic recovery. If fn panics, handler
// is called with the panic value and the context string. The handler should
// NOT panic itself. If handler is nil, DefaultPanicHandler is used.
//
// Example:
//
//	reliability.SafeGo("worker-pool-3", func() {
//	    process(task)
//	}, nil)
func SafeGo(context string, fn func(), handler PanicHandler) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if handler == nil {
					handler = DefaultPanicHandler
				}
				// Guard against the handler itself panicking
				func() {
					defer func() {
						if r2 := recover(); r2 != nil {
							slog.Error("panic handler crashed", "context", context, "panic", r2)
						}
					}()
					handler(r, context)
				}()
			}
		}()
		fn()
	}()
}

// Recover wraps a function call with panic recovery. Unlike SafeGo,
// this runs synchronously — it recovers the panic and returns it as an error.
// Use this when you need to catch panics in synchronous code paths (e.g.,
// inside a goroutine that should continue after a panic, or in a job runner
// that should record the panic as a failure).
//
// Example:
//
//	err := reliability.Recover("scheduler-tick", func() {
//	    runJob(job, runner)
//	})
//	if err != nil {
//	    slog.Error("job panicked", "error", err)
//	}
func Recover(context string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in [%s]: %v", context, r)
			slog.Error("panic recovered", "context", context, "panic", r)
		}
	}()
	fn()
	return nil
}
