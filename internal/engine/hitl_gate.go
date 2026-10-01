package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	btcomp "github.com/rvitorper/go-bt/composite"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

const (
	chainKeyHITLRequestID = "hitl_request_id"
	chainKeyHITLStatus    = "hitl_status"
)

func init() {
	registerHITLNodes()
}

func registerHITLNodes() {
	RegisterCondition("HumanApprovalGranted", func(b *Blackboard) bool {
		return hitlStatus(b) == string(hitl.StatusApproved) || hitlStatus(b) == string(hitl.StatusSkipped)
	})
	RegisterCondition("HumanApprovalDenied", func(b *Blackboard) bool {
		st := hitlStatus(b)
		return st == string(hitl.StatusRejected) || st == string(hitl.StatusExpired)
	})
	RegisterCondition("RequiresExternalApproval", func(b *Blackboard) bool {
		sec, _ := b.ChainState["side_effect_class"].(string)
		sec = strings.ToLower(sec)
		return sec == "destroy" || sec == "external"
	})
	RegisterCondition("HumanApprovalPending", func(b *Blackboard) bool {
		return hitlStatus(b) == string(hitl.StatusPending)
	})
}

func hitlStatus(b *Blackboard) string {
	if b == nil || b.ChainState == nil {
		return ""
	}
	if s, ok := b.ChainState[chainKeyHITLStatus].(string); ok {
		return s
	}
	return ""
}

func setHITLState(b *Blackboard, id string, status hitl.Status) {
	if b.ChainState == nil {
		b.ChainState = make(map[string]any)
	}
	b.ChainState[chainKeyHITLRequestID] = id
	b.ChainState[chainKeyHITLStatus] = string(status)
}

func hitlStore() *hitl.Store {
	if hitl.DefaultStore != nil {
		return hitl.DefaultStore
	}
	return nil
}

func promptFromNode(node *evolution.SerializableNode) string {
	if node.Metadata != nil {
		if p, ok := node.Metadata["prompt"].(string); ok && p != "" {
			return p
		}
		if p, ok := node.Metadata["hitl_prompt"].(string); ok && p != "" {
			return p
		}
	}
	return node.Description
}

func hitlPhase(node *evolution.SerializableNode) string {
	if node != nil && node.Metadata != nil {
		if p, ok := node.Metadata["phase"].(string); ok && p != "" {
			return p
		}
	}
	return "pre"
}

func childExecuted(bb *Blackboard) bool {
	if bb == nil || bb.ChainState == nil {
		return false
	}
	v, ok := bb.ChainState["hitl_child_executed"].(bool)
	return ok && v
}

func markChildExecuted(bb *Blackboard, code int) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState["hitl_child_executed"] = true
	bb.ChainState["hitl_child_code"] = code
}

func autoApproveFromNode(node *evolution.SerializableNode) bool {
	if node.Metadata != nil {
		if v, ok := node.Metadata["auto_approve"].(bool); ok && v {
			return true
		}
	}
	return false
}

// matchesHITLTask keeps reused node names from sharing approvals across work.
func matchesHITLTask(req *hitl.Request, bb *Blackboard) bool {
	taskID, _ := bb.ChainState["task_id"].(string)
	agentName, _ := bb.ChainState["agent_name"].(string)
	if req.AgentName != agentName {
		return false
	}
	if taskID != "" {
		return req.TaskID == taskID
	}
	return req.TaskID == "" && req.Task == bb.Task
}

func failHITLGate(bb *Blackboard, err error) int {
	delete(bb.ChainState, chainKeyHITLStatus)
	bb.Result = fmt.Sprintf("HITL approval unavailable: %v", err)
	bb.Outcome = string(evolution.Failure)
	return -1
}

// humanApprovalGateCmd blocks until a human approves (or policy auto-approves), then runs children.
type humanApprovalGateCmd struct {
	node  *evolution.SerializableNode
	child btcore.Command[Blackboard]
}

func (h *humanApprovalGateCmd) requestKey() string {
	return "hitl_request:" + h.node.Type + ":" + h.node.Name + ":" + hitlPhase(h.node)
}
func (h *humanApprovalGateCmd) matchesRequest(req *hitl.Request) bool {
	phase := req.Phase
	if phase == "" {
		phase = "pre"
	}
	return req.NodeName == h.node.Name && req.NodeType == h.node.Type && phase == hitlPhase(h.node)
}
func (h *humanApprovalGateCmd) setRequestState(bb *Blackboard, req *hitl.Request) {
	setHITLState(bb, req.ID, req.Status)
	bb.ChainState[h.requestKey()] = req.ID
}
func (h *humanApprovalGateCmd) childDone(bb *Blackboard) bool {
	done, _ := bb.ChainState[h.requestKey()+":child_done"].(bool)
	return done
}
func (h *humanApprovalGateCmd) clearRequestState(bb *Blackboard) {
	delete(bb.ChainState, h.requestKey())
	delete(bb.ChainState, h.requestKey()+":child_done")
	delete(bb.ChainState, h.requestKey()+":child_code")
	delete(bb.ChainState, chainKeyHITLRequestID)
	delete(bb.ChainState, chainKeyHITLStatus)
}

func (h *humanApprovalGateCmd) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	parent := ctx.Context
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return failHITLGate(bb, err)
	}
	if sideEffectRequiresHITL(h.node) {
		if bb.ChainState == nil {
			bb.ChainState = make(map[string]any)
		}
		bb.ChainState["side_effect_class"] = "external"
	}
	// Structural evaluation simulates approval as it simulates action effects.
	// It must not depend on, or mutate, the operator's real approval store.
	if bb.Sandbox {
		setHITLState(bb, "", hitl.StatusSkipped)
		if h.child != nil {
			return h.child.Run(ctx)
		}
		return 1
	}
	store := hitlStore()
	pol := hitl.GetPolicy()

	if !pol.Enabled && !sideEffectRequiresHITL(h.node) {
		if h.child != nil {
			return h.child.Run(ctx)
		}
		return 1
	}
	if store == nil {
		return failHITLGate(bb, fmt.Errorf("HITL store not initialized"))
	}

	// Resolve existing request from blackboard
	reqID, _ := bb.ChainState[h.requestKey()].(string)
	if reqID == "" {
		reqID, _ = bb.ChainState[chainKeyHITLRequestID].(string)
	}
	var req *hitl.Request
	var ok bool

	if reqID != "" && store != nil {
		var err error
		req, err = store.RefreshRequestWithContext(parent, reqID)
		if err != nil && !errors.Is(err, hitl.ErrRequestNotFound) {
			return failHITLGate(bb, err)
		}
		ok = req != nil && h.matchesRequest(req)
		if !ok {
			req = nil
		}
		if ok {
			if !matchesHITLTask(req, bb) {
				return failHITLGate(bb, fmt.Errorf("HITL request belongs to another task or agent"))
			}
			h.setRequestState(bb, req)
		}
	}

	// Deduplicate: when no request is found in the blackboard (fresh scheduler
	// tick), search the persistent store for an existing pending request with
	// the same NodeName.  This prevents duplicate HITL requests when the
	// scheduler re-enters the gate on every tick.
	phase := hitlPhase(h.node)
	if (!ok || req == nil) && store != nil {
		pending, err := store.ListPendingWithContext(parent)
		if err != nil {
			return failHITLGate(bb, err)
		}
		for _, r := range pending {
			if h.matchesRequest(r) && matchesHITLTask(r, bb) {
				req = r
				ok = true
				h.setRequestState(bb, r)
				break
			}
		}
	}
	if phase == "post" && ok && childExecuted(bb) {
		if _, exists := bb.ChainState[h.requestKey()+":child_code"]; !exists {
			bb.ChainState[h.requestKey()+":child_done"] = true
			bb.ChainState[h.requestKey()+":child_code"] = bb.ChainState["hitl_child_code"]
		}
	}
	if phase == "post" && !h.childDone(bb) && h.child != nil {
		code := h.child.Run(ctx)
		if code != 1 {
			return code
		}
		markChildExecuted(bb, code)
		bb.ChainState[h.requestKey()+":child_done"] = true
		bb.ChainState[h.requestKey()+":child_code"] = code
	}

	if !ok || req == nil {
		proposed := bb.Result
		if proposed == "" {
			proposed = bb.Plan
		}
		meta := map[string]any{"phase": phase}
		if bb.ChainState != nil {
			if a, ok := bb.ChainState["agent_name"].(string); ok {
				meta["agent_name"] = a
			}
			if tid, ok := bb.ChainState["task_id"].(string); ok {
				meta["task_id"] = tid
			}
		}
		req = hitl.NewRequest(h.node.Name, h.node.Type, bb.Task, bb.Plan, proposed, promptFromNode(h.node), meta)
		req.Phase = phase
		if autoApproveFromNode(h.node) {
			req.Status = hitl.StatusSkipped
		}
		req = hitl.ApplyAutoApproveIfPolicy(req)
		if store != nil {
			if err := store.CreateWithContext(parent, req); err != nil {
				return failHITLGate(bb, err)
			}
		}
		h.setRequestState(bb, req)
	}

	switch req.Status {
	case hitl.StatusPending:
		bb.Result = fmt.Sprintf("Awaiting human approval (id=%s): %s", req.ID, req.Prompt)
		bb.Outcome = "pending_approval"
		return 0 // RUNNING — RunTask tick loop continues
	case hitl.StatusRejected, hitl.StatusExpired:
		bb.Result = fmt.Sprintf("Human rejected or expired (id=%s): %s", req.ID, req.Reason)
		bb.Outcome = string(evolution.Failure)
		return -1
	case hitl.StatusApproved, hitl.StatusSkipped:
		if phase == "post" && h.childDone(bb) {
			if c, ok := bb.ChainState[h.requestKey()+":child_code"].(int); ok {
				h.clearRequestState(bb)
				delete(bb.ChainState, "hitl_child_executed")
				delete(bb.ChainState, "hitl_child_code")
				return c
			}
		}
		if h.child != nil && phase != "post" {
			code := h.child.Run(ctx)
			if code != 0 {
				h.clearRequestState(bb)
			}
			return code
		}
		h.clearRequestState(bb)
		return 1
	default:
		return -1
	}
}

func buildHumanApprovalGate(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	var child btcore.Command[Blackboard]
	switch len(node.Children) {
	case 0:
		child = btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	case 1:
		child = buildNode(&node.Children[0], bb, node.Name)
	default:
		kids := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			kids[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		child = btcomp.NewSequence(kids...)
	}
	_ = parentName
	return &humanApprovalGateCmd{node: node, child: child}
}

func sideEffectRequiresHITL(node *evolution.SerializableNode) bool {
	if node.Metadata == nil {
		return false
	}
	sec, _ := node.Metadata["side_effect_class"].(string)
	sec = strings.ToLower(sec)
	return sec == "destroy" || sec == "external"
}
