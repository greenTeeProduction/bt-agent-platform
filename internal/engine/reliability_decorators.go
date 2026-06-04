package engine

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
	btdec "github.com/rvitorper/go-bt/decorators"
)

var nodeCircuitBreakers sync.Map // name -> *reliability.CircuitBreaker

func circuitBreakerFor(name string, threshold int, cooldown time.Duration) *reliability.CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	if v, ok := nodeCircuitBreakers.Load(name); ok {
		return v.(*reliability.CircuitBreaker)
	}
	cb := reliability.NewCircuitBreaker(name, threshold, cooldown)
	actual, _ := nodeCircuitBreakers.LoadOrStore(name, cb)
	return actual.(*reliability.CircuitBreaker)
}

func cooldownFromNode(node *evolution.SerializableNode) time.Duration {
	if node.Metadata != nil {
		if secs, ok := node.Metadata["cooldown_secs"].(float64); ok && secs > 0 {
			return time.Duration(secs) * time.Second
		}
		if secs, ok := node.Metadata["cooldown_secs"].(int); ok && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 60 * time.Second
}

// recordNodeFailure stores classified error state on the blackboard for fallback nodes.
func recordNodeFailure(bb *Blackboard, nodeName string, err error) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	if err == nil {
		err = errors.New(bb.Result)
	}
	cat := reliability.ClassifyError(err)
	bb.ChainState["last_error_category"] = cat.String()
	bb.ChainState["last_error_node"] = nodeName
	bb.ChainState["last_error"] = err.Error()
	bb.FailureCount++
}

func recordNodeSuccess(bb *Blackboard, nodeName string) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState["last_error_category"] = ""
	bb.ChainState["last_error_node"] = ""
	delete(bb.ChainState, "last_error")
	_ = nodeName
}

// errorAwareCmd wraps a BT command to record failures with error categories.
type errorAwareCmd struct {
	name  string
	child btcore.Command[Blackboard]
}

func (e *errorAwareCmd) Run(ctx *btcore.BTContext[Blackboard]) int {
	code := e.child.Run(ctx)
	if code == 1 {
		recordNodeSuccess(ctx.Blackboard, e.name)
		return 1
	}
	if code < 0 {
		recordNodeFailure(ctx.Blackboard, e.name, errors.New(ctx.Blackboard.Result))
	}
	return code
}

func wrapErrorAware(name string, child btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	return &errorAwareCmd{name: name, child: child}
}

// circuitBreakerCmd gates child execution behind a circuit breaker.
type circuitBreakerCmd struct {
	name      string
	threshold int
	cooldown  time.Duration
	child     btcore.Command[Blackboard]
}

func (c *circuitBreakerCmd) Run(ctx *btcore.BTContext[Blackboard]) int {
	cb := circuitBreakerFor(c.name, c.threshold, c.cooldown)
	if !cb.Allow() {
		ctx.Blackboard.Result = fmt.Sprintf("circuit open for node %s — skipping until cooldown", c.name)
		recordNodeFailure(ctx.Blackboard, c.name, reliability.NewCategorizedError(reliability.ErrCatResourceExhausted, errors.New("circuit open")))
		return -1
	}
	code := c.child.Run(ctx)
	if code == 1 {
		cb.RecordSuccess()
		recordNodeSuccess(ctx.Blackboard, c.name)
		return 1
	}
	if code < 0 {
		cb.RecordFailureWithCategory(errors.New(ctx.Blackboard.Result))
		recordNodeFailure(ctx.Blackboard, c.name, errors.New(ctx.Blackboard.Result))
	}
	return code
}

func buildCircuitBreaker(node *evolution.SerializableNode, child btcore.Command[Blackboard], bb *Blackboard) btcore.Command[Blackboard] {
	name := node.Name
	if name == "" {
		name = "CircuitBreaker"
	}
	threshold := node.MaxRetries
	if threshold <= 0 {
		threshold = 3
	}
	cbCmd := &circuitBreakerCmd{
		name:      name,
		threshold: threshold,
		cooldown:  cooldownFromNode(node),
		child:     child,
	}
	_ = bb
	return wrapErrorAware(name, cbCmd)
}

func buildTimeout(node *evolution.SerializableNode, child btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	dur := time.Duration(node.TimeoutMs) * time.Millisecond
	if dur <= 0 {
		dur = 120 * time.Second
	}
	name := node.Name
	if name == "" {
		name = "Timeout"
	}
	timed := btdec.NewTimeout(child, dur)
	return wrapErrorAware(name, timed)
}

// ResetNodeCircuitBreakers clears all node-level circuit breakers (for tests).
func ResetNodeCircuitBreakers() {
	nodeCircuitBreakers.Range(func(key, _ any) bool {
		nodeCircuitBreakers.Delete(key)
		return true
	})
}
