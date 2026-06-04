package engine

import (
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

var nodeRateLimiters sync.Map // name -> *rateLimiter

type rateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func getRateLimiter(name string, interval time.Duration) *rateLimiter {
	if interval <= 0 {
		interval = time.Second
	}
	v, _ := nodeRateLimiters.LoadOrStore(name, &rateLimiter{interval: interval})
	return v.(*rateLimiter)
}

// BuildRateLimit throttles child execution (returns Running/0 until interval elapses).
func BuildRateLimit(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	interval := time.Second
	if node.Metadata != nil {
		if v, ok := node.Metadata["interval_ms"].(float64); ok && v > 0 {
			interval = time.Duration(v) * time.Millisecond
		}
		if v, ok := node.Metadata["interval_ms"].(int); ok && v > 0 {
			interval = time.Duration(v) * time.Millisecond
		}
		if v, ok := node.Metadata["rps"].(float64); ok && v > 0 {
			interval = time.Duration(float64(time.Second) / v)
		}
	}
	key := node.Name
	if key == "" {
		key = "default"
	}
	lim := getRateLimiter(key, interval)
	child := buildNode(&node.Children[0], bb, node.Name)
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		if !lim.allow() {
			return 0
		}
		return child.Run(ctx)
	})
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if !r.last.IsZero() && now.Sub(r.last) < r.interval {
		return false
	}
	r.last = now
	return true
}

// ResetRateLimiters clears test/global limiter state.
func ResetRateLimiters() {
	nodeRateLimiters = sync.Map{}
}
