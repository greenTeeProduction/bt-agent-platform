# ADR-007: Reliability Architecture — Circuit Breakers, Retry, and Dead Letter Queue

**Status:** Accepted
**Date:** 2026-05-29
**Deciders:** Nico (via Hermes Agent)

## Context

Long-running agent tasks on Jetson ARM64 face multiple failure modes: Ollama timeouts (2-4 min/call), OOM kills (qwen3.6:35b ~24 GB), and process crashes. Without reliability patterns, failures cascade and tasks are silently lost. Options:

1. **Simple retry in each handler** — ad-hoc, inconsistent, no backoff
2. **External message queue (NATS/Kafka)** — robust but heavy deps on edge hardware
3. **In-process reliability primitives** — lightweight, no external deps, sufficient for single-node Jetson

## Decision

Implement 14 reliability primitives in `internal/reliability/`:

| Component | Purpose |
|---|---|
| **CircuitBreaker** | Three-state (closed/open/half-open) with configurable threshold and cooldown |
| **Backoff** | Exponential: `base × 2^(attempt-1)`, capped at `maxDelay` |
| **RetryWithBackoff** | Wrapper combining backoff with circuit breaker awareness |
| **DeadLetterQueue** | File-backed persistent queue for failed tasks (JSON, atomic writes) |
| **WorkerPool** | Fixed-size goroutine pool with graceful shutdown |
| **TaskQueue** | File-backed FIFO implementing `Queue` interface |
| **PriorityTaskQueue** | Min-heap with 5 levels (Critical→Background), persisted to disk |
| **ConcurrencyLimiter** | Channel semaphore capping concurrent execution |
| **AgentExecutor** | Interface for pluggable execution backends (local/HTTP/gRPC) |
| **AgentRouter** | Health-aware round-robin + least-connections across executor pool |
| **SafeGo/Recover** | Panic recovery wrappers for all goroutines |
| **AgentResult** | Structured execution result with duration, quality score, error |
| **ScalabilityStatus** | Aggregation snapshot of all runtime capacity |
| **JobStore** | Scheduler crash recovery with InFlight flag persistence |

## Consequences

- **Positive**: Zero external dependencies — all primitives use Go stdlib only
- **Positive**: File-backed persistence enables crash recovery without database
- **Positive**: Pluggable Queue/PriorityTaskQueue interfaces enable Redis swap-in for distributed mode
- **Positive**: AgentRouter failover with weighted least-connections routing supports multi-node horizontal scaling
- **Positive**: DLQ replay dashboard API enables manual inspection and retry of failed tasks
- **Negative**: File-based queues have higher latency than Redis-backed alternatives
- **Negative**: Single-node reliability doesn't address split-brain in multi-node deployments
- **Negative**: In-process worker pool shares Jetson memory with Ollama — OOM still possible under load
