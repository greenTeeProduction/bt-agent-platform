package langagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/factory"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// --- Tool: RunTask ---

type RunTaskTool struct {
	bb   *engine.Blackboard
	tree interface{} // stored as btcore.Command[engine.Blackboard], but we avoid the import
	run  func(task string) string
}

func NewRunTaskTool(bb *engine.Blackboard, runFn func(string) string) *RunTaskTool {
	return &RunTaskTool{bb: bb, run: runFn}
}

func (t *RunTaskTool) Name() string        { return "bt_run_task" }
func (t *RunTaskTool) Description() string {
	return "Execute a task through the behavior tree. Input: a task description. Returns: JSON with result, outcome, complexity, duration_ms, and plan."
}
func (t *RunTaskTool) Call(ctx context.Context, input string) (string, error) {
	result := t.run(input)
	out := map[string]interface{}{
		"result":       result,
		"outcome":      t.bb.Outcome,
		"complexity":   t.bb.Complexity,
		"duration_ms":  t.bb.DurationMs,
		"plan_summary": truncateStr(t.bb.Plan, 200),
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Tool: Reflect ---

type ReflectTool struct {
	bb *engine.Blackboard
}

func NewReflectTool(bb *engine.Blackboard) *ReflectTool {
	return &ReflectTool{bb: bb}
}

func (t *ReflectTool) Name() string        { return "bt_reflect" }
func (t *ReflectTool) Description() string {
	return "Generate a reflection on the last executed task. Returns: what went well and what to improve."
}
func (t *ReflectTool) Call(ctx context.Context, input string) (string, error) {
	if t.bb.LLM == nil {
		return `{"went_well": "no LLM available", "to_improve": "configure LLM"}`, nil
	}
	ww, ti := t.bb.LLM.Reflect(t.bb.Task, t.bb.Outcome, t.bb.Plan)
	out := map[string]interface{}{
		"went_well":    ww,
		"to_improve":   ti,
		"task":         t.bb.Task,
		"outcome":      t.bb.Outcome,
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Tool: GetFitness ---

type FitnessTool struct {
	refStore  *reflection.Store
	treeStore *evolution.TreeStore
}

func NewFitnessTool(refStore *reflection.Store, treeStore *evolution.TreeStore) *FitnessTool {
	return &FitnessTool{refStore: refStore, treeStore: treeStore}
}

func (t *FitnessTool) Name() string        { return "bt_get_fitness" }
func (t *FitnessTool) Description() string {
	return "Get behavior tree fitness stats: total tasks, successes, failures, success rate, node count."
}
func (t *FitnessTool) Call(ctx context.Context, input string) (string, error) {
	tree, _ := t.treeStore.Load()
	records, _ := t.refStore.LoadAll()
	failures := t.refStore.CountFailures()
	successes := len(records) - failures
	rate := 0.0
	if len(records) > 0 {
		rate = float64(successes) / float64(len(records))
	}
	nodeCount := 0
	if tree != nil {
		nodeCount = evolution.CountNodes(tree)
	}
	out := map[string]interface{}{
		"total_tasks":  len(records),
		"successes":    successes,
		"failures":     failures,
		"success_rate": fmt.Sprintf("%.1f%%", rate*100),
		"node_count":   nodeCount,
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Tool: Evolve ---

type EvolveTool struct {
	refStore  *reflection.Store
	treeStore *evolution.TreeStore
}

func NewEvolveTool(refStore *reflection.Store, treeStore *evolution.TreeStore) *EvolveTool {
	return &EvolveTool{refStore: refStore, treeStore: treeStore}
}

func (t *EvolveTool) Name() string        { return "bt_evolve" }
func (t *EvolveTool) Description() string {
	return "Evolve the behavior tree by applying mutations when failure count >= 3. Returns: whether evolution was applied and node count change."
}
func (t *EvolveTool) Call(ctx context.Context, input string) (string, error) {
	tree, err := t.treeStore.Load()
	if err != nil || tree == nil {
		return `{"evolved": false, "reason": "no tree"}`, nil
	}

	failures := t.refStore.CountFailures()
	if failures < 3 {
		return fmt.Sprintf(`{"evolved": false, "reason": "need 3+ failures, have %d"}`, failures), nil
	}

	ops := []evolution.MutationOp{
		{Operation: "wrap_retry", Target: "AnalyzeTask"},
		{Operation: "increase_retries", Target: "RetrySelfCorrect"},
	}
	before := evolution.CountNodes(tree)
	applied := evolution.ApplyMutations(tree, ops)
	after := evolution.CountNodes(tree)
	if applied > 0 {
		_ = t.treeStore.Save(tree)
	}
	out := map[string]interface{}{
		"evolved":      applied > 0,
		"applied":      applied,
		"nodes_before": before,
		"nodes_after":  after,
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Tool: GetTree ---

type GetTreeTool struct {
	treeStore *evolution.TreeStore
}

func NewGetTreeTool(treeStore *evolution.TreeStore) *GetTreeTool {
	return &GetTreeTool{treeStore: treeStore}
}

func (t *GetTreeTool) Name() string        { return "bt_get_tree" }
func (t *GetTreeTool) Description() string {
	return "Get the current behavior tree structure. Returns: full serialized tree JSON."
}
func (t *GetTreeTool) Call(ctx context.Context, input string) (string, error) {
	tree, err := t.treeStore.Load()
	if err != nil || tree == nil {
		return `{"error": "no tree"}`, nil
	}
	data, _ := json.Marshal(tree)
	return string(data), nil
}

// --- Tool: CreateAgent ---

type CreateAgentTool struct {
	factory *factory.AgentFactory
}

func NewCreateAgentTool(f *factory.AgentFactory) *CreateAgentTool {
	return &CreateAgentTool{factory: f}
}

func (t *CreateAgentTool) Name() string        { return "bt_create_agent" }
func (t *CreateAgentTool) Description() string {
	return "Create a new behavior tree agent from a skill file. Input: path to SKILL.md file. Returns: agent name, node count, strategy count."
}
func (t *CreateAgentTool) Call(ctx context.Context, input string) (string, error) {
	if t.factory == nil {
		return `{"error": "factory not configured"}`, nil
	}
	agent, err := t.factory.CreateFromSkillDir(input)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error()), nil
	}
	out := map[string]interface{}{
		"created":    true,
		"agent_name": agent.Name,
		"node_count": evolution.CountNodes(agent.SerTree),
		"root_type":  agent.SerTree.Type,
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Tool: GetReflections ---

type GetReflectionsTool struct {
	refStore *reflection.Store
}

func NewGetReflectionsTool(refStore *reflection.Store) *GetReflectionsTool {
	return &GetReflectionsTool{refStore: refStore}
}

func (t *GetReflectionsTool) Name() string        { return "bt_get_reflections" }
func (t *GetReflectionsTool) Description() string {
	return "Get recent reflection records: what went well, what to improve from past tasks."
}
func (t *GetReflectionsTool) Call(ctx context.Context, input string) (string, error) {
	records, _ := t.refStore.LoadAll()
	// Return last 5, summarized
	n := len(records)
	if n > 5 {
		records = records[n-5:]
	}
	type summary struct {
		Task       string `json:"task"`
		Outcome    string `json:"outcome"`
		WentWell   string `json:"went_well"`
		ToImprove  string `json:"to_improve"`
	}
	var items []summary
	for _, r := range records {
		ww := ""
		ti := ""
		if len(r.WhatWentWell) > 0 {
			ww = r.WhatWentWell[0]
		}
		if len(r.WhatToImprove) > 0 {
			ti = r.WhatToImprove[0]
		}
		items = append(items, summary{
			Task:      r.Task,
			Outcome:   string(r.Outcome),
			WentWell:  truncateStr(ww, 100),
			ToImprove: truncateStr(ti, 100),
		})
	}
	out := map[string]interface{}{
		"total":         n,
		"recent":        items,
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

// --- Helpers ---

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ensure llm.LLM is imported (used by ReflectTool)
var _ llm.LLM = nil
