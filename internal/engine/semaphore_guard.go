package engine

import (
	"strings"
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

var semaphores = struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}{m: map[string]chan struct{}{}}

func namedSemaphore(name string, permits int) chan struct{} {
	semaphores.mu.Lock()
	defer semaphores.mu.Unlock()
	if s, ok := semaphores.m[name]; ok {
		return s
	}
	s := make(chan struct{}, permits)
	semaphores.m[name] = s
	return s
}

// BuildSemaphoreGuard bounds concurrent execution of its child across ALL
// trees in the process (e.g. cap simultaneous Claude Code invocations on a
// memory-constrained host). Non-blocking: no free permit → RUNNING, so a
// parent Parallel keeps other branches progressing.
func BuildSemaphoreGuard(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	name, _ := node.Metadata["semaphore"].(string)
	if strings.TrimSpace(name) == "" || len(node.Children) != 1 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "SemaphoreGuard requires metadata.semaphore and exactly one child"
			return -1
		})
	}
	permits := 1
	switch v := node.Metadata["permits"].(type) {
	case int:
		permits = v
	case float64:
		permits = int(v)
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	sem := namedSemaphore(name, permits)
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			return child.Run(ctx)
		default:
			return 0 // contended: report RUNNING, retry next tick
		}
	})
}
