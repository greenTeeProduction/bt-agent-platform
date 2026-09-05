package engine

import (
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildCachedCondition memoizes its child condition's result for ttl_ms
// (default 30000). NEVER wrap approval/HITL/safety conditions — the validator
// warns on names containing "HITL" or "Approved". Cache lives in ChainState
// so run resets clear it.
func BuildCachedCondition(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) != 1 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "CachedCondition requires exactly one child"
			return -1
		})
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	ttl := 30000
	if v, ok := node.Metadata["ttl_ms"]; ok {
		switch n := v.(type) {
		case int:
			ttl = n
		case float64:
			ttl = int(n)
		}
	}
	key := "condcache/" + node.Name
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		now := time.Now().UnixMilli()
		if entry, ok := ctx.Blackboard.ChainState[key].(map[string]any); ok {
			exp := int64(0)
			switch e := entry["expires_unix_ms"].(type) {
			case int64:
				exp = e
			case float64:
				exp = int64(e)
			}
			if now < exp {
				if v, _ := entry["value"].(bool); v {
					return 1
				}
				return -1
			}
		}
		code := child.Run(ctx)
		if code == 0 {
			return 0 // never cache RUNNING
		}
		ctx.Blackboard.ChainState[key] = map[string]any{
			"value":           code > 0,
			"expires_unix_ms": now + int64(ttl),
		}
		return code
	})
}
