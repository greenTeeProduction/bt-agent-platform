package hitl

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NewRequest builds a pending approval request from execution context.
func NewRequest(nodeName, nodeType, task, plan, proposed, prompt string, meta map[string]any) *Request {
	pol := GetPolicy()
	if prompt == "" {
		prompt = pol.DefaultPrompt
	}
	if meta == nil {
		meta = map[string]any{}
	}
	ctx := map[string]string{}
	for k, v := range meta {
		if s, ok := v.(string); ok {
			ctx[k] = s
		}
	}
	now := time.Now()
	req := &Request{
		ID:        "hitl-" + uuid.New().String()[:8],
		Status:    StatusPending,
		NodeName:  nodeName,
		NodeType:  nodeType,
		Prompt:    prompt,
		Task:      task,
		Plan:      plan,
		Proposed:  proposed,
		Context:   ctx,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(pol.Timeout),
	}
	if agent, ok := meta["agent_name"].(string); ok {
		req.AgentName = agent
	}
	if tree, ok := meta["tree_id"].(string); ok {
		req.TreeID = tree
	}
	if taskID, ok := meta["task_id"].(string); ok && taskID != "" {
		req.SetTaskID(taskID)
	}
	return req
}

// ApplyAutoApproveIfPolicy auto-approves when policy allows.
func ApplyAutoApproveIfPolicy(req *Request) *Request {
	pol := GetPolicy()
	if !pol.Enabled || pol.AutoApprove {
		now := time.Now()
		req.Status = StatusSkipped
		req.Reviewer = "policy:auto"
		req.Reason = "auto-approved by HITL policy"
		req.ApprovedAt = &now
		req.UpdatedAt = now
	}
	return req
}

// FormatSummary returns a human-readable summary for dashboards/MCP.
func (r *Request) FormatSummary() string {
	return fmt.Sprintf("[%s] %s — %s (task: %.80s)", r.Status, r.NodeName, r.Prompt, r.Task)
}
