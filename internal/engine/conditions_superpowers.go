// Package engine — Superpowers Pipeline condition registrations.
package engine

import "strings"

func init() {
	registerSuperpowersConditions()
}

func registerSuperpowersConditions() {

	// Phase 1: Skill Detection
	RegisterCondition("IsCreativeTask", func(bb *Blackboard) bool {
		creative := []string{"build", "create", "implement", "design", "feature", "add", "make", "develop", "write", "refactor"}
		task := strings.ToLower(bb.Task)
		for _, kw := range creative {
			if strings.Contains(task, kw) {
				return true
			}
		}
		return false
	})

	// Phase 1: Design gate
	RegisterCondition("DesignApproved", func(bb *Blackboard) bool {
		approved, ok := bb.ChainState["design_approved"].(bool)
		return ok && approved
	})

	// Phase 2: Worktree gate
	RegisterCondition("WorktreeReady", func(bb *Blackboard) bool {
		ready, ok := bb.ChainState["worktree_ready"].(bool)
		return ok && ready
	})

	// Phase 3: Design must exist before planning
	RegisterCondition("DesignExists", func(bb *Blackboard) bool {
		path, ok := bb.ChainState["design_path"].(string)
		return ok && path != ""
	})

	// Phase 3: Plan gate
	RegisterCondition("PlanReady", func(bb *Blackboard) bool {
		ready, ok := bb.ChainState["plan_ready"].(bool)
		return ok && ready
	})

	// Phase 4: HITL gate
	RegisterCondition("HITLAlreadyApproved", func(bb *Blackboard) bool {
		approved, ok := bb.ChainState["hitl_approved"].(bool)
		return ok && approved
	})

	// Phase 5: Iterator guard
	RegisterCondition("CheckIndexInRange", func(bb *Blackboard) bool {
		idx, _ := bb.ChainState["current_task_index"].(int)
		tasks, ok := bb.ChainState["task_batch"].([]map[string]string)
		if !ok {
			// Try interface slice
			tasksI, ok := bb.ChainState["task_batch"].([]map[string]string)
			if !ok {
				size, _ := bb.ChainState["task_batch_size"].(int)
				return idx < size
			}
			return idx < len(tasksI)
		}
		return idx < len(tasks)
	})

	// Phase 6: Verification retry gate
	RegisterCondition("VerificationFailed", func(bb *Blackboard) bool {
		results, ok := bb.ChainState["verification_results"].(map[string]bool)
		if !ok {
			// No results yet — assume failed
			return true
		}
		for _, passed := range results {
			if !passed {
				return true
			}
		}
		return false
	})
}
