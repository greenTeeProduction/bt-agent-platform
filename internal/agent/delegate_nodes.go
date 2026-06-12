package agent

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerDelegateNodes()
}

func registerDelegateNodes() {
	engine.RegisterCondition("HasDelegateTarget", func(b *engine.Blackboard) bool {
		if b == nil || b.ChainState == nil {
			return false
		}
		id, _ := b.ChainState["delegate_tree_id"].(string)
		return strings.TrimSpace(id) != ""
	})

	engine.RegisterAction("DelegateToTree", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = make(map[string]any)
		}
		treeID, _ := b.ChainState["delegate_tree_id"].(string)
		treeID = strings.TrimSpace(treeID)
		if treeID == "" {
			b.Result = "delegate_tree_id not set in chain state"
			b.Outcome = "failure"
			return -1
		}
		task := b.Task
		if t, ok := b.ChainState["delegate_task"].(string); ok && strings.TrimSpace(t) != "" {
			task = t
		}
		if strings.TrimSpace(task) == "" {
			b.Result = "no task for delegation"
			b.Outcome = "failure"
			return -1
		}
		if engine.DelegateToTreeFn == nil {
			b.Result = "DelegateToTree not configured (wire engine.DelegateToTreeFn at startup)"
			b.Outcome = "failure"
			return -1
		}
		out, err := engine.DelegateToTreeFn(treeID, b)
		if err != nil {
			b.Result = fmt.Sprintf("delegation failed: %v", err)
			b.Outcome = "failure"
			return -1
		}
		b.Result = out
		b.Outcome = "success"
		return 1
	})

	engine.RegisterAction("LoadAgentMemory", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = make(map[string]any)
		}
		agentName := agentNameFromBB(b)
		if agentName == "" || engine.AgentMemoryBaseDir == "" {
			b.ChainState["agent_memory"] = ""
			return 1
		}
		ms, err := NewMemoryStore(engine.AgentMemoryBaseDir+"/.go-bt-evolve/memory", agentName, 200)
		if err != nil {
			b.Result = fmt.Sprintf("memory load failed: %v", err)
			return -1
		}
		b.ChainState["agent_memory"] = ms.ContextBlock()
		return 1
	})

	engine.RegisterAction("WriteAgentMemory", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		agentName := agentNameFromBB(b)
		if agentName == "" || engine.AgentMemoryBaseDir == "" {
			return 1
		}
		ms, err := NewMemoryStore(engine.AgentMemoryBaseDir+"/.go-bt-evolve/memory", agentName, 200)
		if err != nil {
			b.Result = fmt.Sprintf("memory write failed: %v", err)
			return -1
		}
		summary := b.Result
		if summary == "" {
			summary = b.Task
		}
		key := "last_run_summary"
		if k, ok := b.ChainState["memory_write_key"].(string); ok && k != "" {
			key = k
		}
		if err := ms.Write(key, summary, "state", "medium", "bt_run"); err != nil {
			b.Result = fmt.Sprintf("memory write failed: %v", err)
			return -1
		}
		return 1
	})

	engine.RegisterAction("MergeResults", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		if len(b.Results) == 0 {
			if b.Result != "" {
				return 1
			}
			b.Result = "no results to merge"
			return -1
		}
		var parts []string
		for i, r := range b.Results {
			parts = append(parts, fmt.Sprintf("### Part %d\n%s", i+1, r))
		}
		b.Result = strings.Join(parts, "\n\n")
		b.Results = append(b.Results, b.Result)
		return 1
	})

	engine.RegisterAction("PrepareA2AHandoff", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = make(map[string]any)
		}
		if url, ok := b.ChainState["a2a_url"].(string); ok && url != "" {
			b.ChainState["a2a_target_url"] = url
		}
		if _, ok := b.ChainState["a2a_target_url"].(string); !ok || b.ChainState["a2a_target_url"] == "" {
			b.Result = "a2a_url or a2a_target_url required in chain state"
			b.Outcome = "failure"
			return -1
		}
		return 1
	})
}

func agentNameFromBB(b *engine.Blackboard) string {
	if b == nil || b.ChainState == nil {
		return ""
	}
	if n, ok := b.ChainState["agent_name"].(string); ok {
		return n
	}
	return ""
}
