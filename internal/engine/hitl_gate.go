package engine

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/reflection"
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
	if node.Description != "" {
		return node.Description
	}
	return ""
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

// humanApprovalGateCmd blocks until a human approves (or policy auto-approves), then runs children.
type humanApprovalGateCmd struct {
	node  *evolution.SerializableNode
	child btcore.Command[Blackboard]
}

func (h *humanApprovalGateCmd) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	store := hitlStore()
	pol := hitl.GetPolicy()

	if !pol.Enabled {
		if h.child != nil {
			return h.child.Run(ctx)
		}
		return 1
	}

	// Resolve existing request from blackboard
	reqID, _ := bb.ChainState[chainKeyHITLRequestID].(string)
	var req *hitl.Request
	var ok bool

	if reqID != "" && store != nil {
		if st, err := store.RefreshStatus(reqID); err == nil {
			setHITLState(bb, reqID, st)
		}
		req, ok = store.Get(reqID)
	}

	phase := hitlPhase(h.node)
	if phase == "post" && !childExecuted(bb) && h.child != nil {
		code := h.child.Run(ctx)
		markChildExecuted(bb, code)
		if code != 1 {
			delete(bb.ChainState, chainKeyHITLRequestID)
			delete(bb.ChainState, chainKeyHITLStatus)
			return code
		}
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
		req = hitl.ApplyAutoApproveIfPolicy(req)
		if store != nil {
			_ = store.Create(req)
		}
		setHITLState(bb, req.ID, req.Status)
	}

	switch req.Status {
	case hitl.StatusPending:
		bb.Result = fmt.Sprintf("Awaiting human approval (id=%s): %s", req.ID, req.Prompt)
		bb.Outcome = "pending_approval"
		return 0 // RUNNING — RunTask tick loop continues
	case hitl.StatusRejected, hitl.StatusExpired:
		bb.Result = fmt.Sprintf("Human rejected or expired (id=%s): %s", req.ID, req.Reason)
		bb.Outcome = string(reflection.Failure)
		return -1
	case hitl.StatusApproved, hitl.StatusSkipped:
		if phase == "post" && childExecuted(bb) {
			if c, ok := bb.ChainState["hitl_child_code"].(int); ok {
				delete(bb.ChainState, chainKeyHITLRequestID)
				delete(bb.ChainState, chainKeyHITLStatus)
				delete(bb.ChainState, "hitl_child_executed")
				delete(bb.ChainState, "hitl_child_code")
				return c
			}
		}
		if h.child != nil && phase != "post" {
			code := h.child.Run(ctx)
			delete(bb.ChainState, chainKeyHITLRequestID)
			delete(bb.ChainState, chainKeyHITLStatus)
			return code
		}
		delete(bb.ChainState, chainKeyHITLRequestID)
		delete(bb.ChainState, chainKeyHITLStatus)
		return 1
	default:
		return -1
	}
}

func buildHumanApprovalGate(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	var child btcore.Command[Blackboard]
	switch len(node.Children) {
	case 0:
		child = btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
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
