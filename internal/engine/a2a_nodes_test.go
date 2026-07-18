package engine

import (
	"context"
	"fmt"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

type a2aTestCtxKey struct{}

// ─── DelegateToA2A Tests ────────────────────────────────────────────────────

func TestDelegateToA2A_MissingURL(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	// Save and restore DelegateToA2AFn
	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, _ string) (string, error) {
		return "ok", nil
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{Task: "do something"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != -1 {
		t.Errorf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
	if bb.Result != "a2a_target_url not set in chain state" {
		t.Errorf("unexpected result: %s", bb.Result)
	}
}

// TestDelegateToA2A_ContextPropagates pins the contract that DelegateToA2A
// forwards the tree's context (the *btcore.BTContext, which embeds
// context.Context) into DelegateToA2AFn instead of dropping it — so
// cancellation/deadlines/trace values set on the running tree reach the A2A
// client call.
func TestDelegateToA2A_ContextPropagates(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	origFn := DelegateToA2AFn
	var capturedCtx context.Context
	DelegateToA2AFn = func(ctx context.Context, _, _ string) (string, error) {
		capturedCtx = ctx
		return "ok", nil
	}
	defer func() { DelegateToA2AFn = origFn }()

	wantCtx := context.WithValue(context.Background(), a2aTestCtxKey{}, "trace-id-123")
	bb := &Blackboard{
		Task:       "do task",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Context: wantCtx, Blackboard: bb}
	status := action(ctx)
	if status != 1 {
		t.Errorf("expected success (1), got %d", status)
	}
	if capturedCtx == nil {
		t.Fatal("expected DelegateToA2AFn to receive a non-nil context")
	}
	if got := capturedCtx.Value(a2aTestCtxKey{}); got != "trace-id-123" {
		t.Errorf("expected the tree's context to propagate to DelegateToA2AFn, got value %v", got)
	}
}

// TestDelegateToA2A_RateLimitCarryoverPropagates pins the carryover contract
// across the A2A boundary: when the remote agent's run ended in the rate-limit
// carryover (the A2A server reports the task failed with the sentinel in the
// error text), the delegating tree must surface the carryover outcome — the
// same bb.Outcome + return -1 shape the local GOAP emission sites use — so the
// caller's scheduler treats it as a healthy deferred pause instead of a hard
// delegation failure (breaker failure + retries + DLQ).
func TestDelegateToA2A_RateLimitCarryoverPropagates(t *testing.T) {
	registerA2ANodes()
	action := actionRegistry["DelegateToA2A"]

	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("A2A task failed: goap_fusion_rate_limited: Claude rate-limit backoff active")
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		Task:       "do something",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != -1 {
		t.Errorf("expected -1 (the selector must advance), got %d", status)
	}
	if bb.Outcome != "goap_fusion_rate_limited" {
		t.Errorf("outcome = %q, want the carryover sentinel preserved across the A2A boundary", bb.Outcome)
	}
}

func TestDelegateToA2A_MissingTaskEmptyChainState(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, _ string) (string, error) {
		return "ok", nil
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		Task:       "",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != -1 {
		t.Errorf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}

func TestDelegateToA2A_UsesTaskFromChainState(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	var capturedTask string
	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, task string) (string, error) {
		capturedTask = task
		return "ok", nil
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		ChainState: map[string]any{
			"a2a_target_url": "http://example.com",
			"a2a_task":       "delegated task from state",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != 1 {
		t.Errorf("expected success (1), got %d", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
	if capturedTask != "delegated task from state" {
		t.Errorf("expected task from chain state, got %q", capturedTask)
	}
}

func TestDelegateToA2A_FnNotConfigured(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	origFn := DelegateToA2AFn
	DelegateToA2AFn = nil
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		Task:       "do task",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != -1 {
		t.Errorf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
	if bb.Result != "A2A client not configured (set engine.DelegateToA2AFn)" {
		t.Errorf("unexpected result: %s", bb.Result)
	}
}

func TestDelegateToA2A_FnReturnsError(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, _ string) (string, error) {
		return "", fmt.Errorf("connection refused")
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		Task:       "do task",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != -1 {
		t.Errorf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
	if bb.Result != "A2A delegation failed: connection refused" {
		t.Errorf("unexpected result: %s", bb.Result)
	}
}

func TestDelegateToA2A_Success(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["DelegateToA2A"]
	if !ok {
		t.Fatal("DelegateToA2A not registered")
	}

	origFn := DelegateToA2AFn
	DelegateToA2AFn = func(_ context.Context, _, _ string) (string, error) {
		return "A2A response: task completed", nil
	}
	defer func() { DelegateToA2AFn = origFn }()

	bb := &Blackboard{
		Task:       "do task",
		ChainState: map[string]any{"a2a_target_url": "http://example.com"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != 1 {
		t.Errorf("expected success (1), got %d", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
	if bb.Result != "A2A response: task completed" {
		t.Errorf("unexpected result: %s", bb.Result)
	}
}

// ─── HasA2ATarget Tests ──────────────────────────────────────────────────────

func TestHasA2ATarget_NotSet(t *testing.T) {
	registerA2ANodes()
	cond, ok := conditionRegistry["HasA2ATarget"]
	if !ok {
		t.Fatal("HasA2ATarget not registered")
	}

	bb := &Blackboard{}
	if cond(bb) {
		t.Error("HasA2ATarget should return false when no target set")
	}
}

func TestHasA2ATarget_EmptyURL(t *testing.T) {
	registerA2ANodes()
	cond, ok := conditionRegistry["HasA2ATarget"]
	if !ok {
		t.Fatal("HasA2ATarget not registered")
	}

	bb := &Blackboard{
		ChainState: map[string]any{"a2a_target_url": ""},
	}
	if cond(bb) {
		t.Error("HasA2ATarget should return false for empty URL")
	}
}

func TestHasA2ATarget_Set(t *testing.T) {
	registerA2ANodes()
	cond, ok := conditionRegistry["HasA2ATarget"]
	if !ok {
		t.Fatal("HasA2ATarget not registered")
	}

	bb := &Blackboard{
		ChainState: map[string]any{"a2a_target_url": "http://agent:8080"},
	}
	if !cond(bb) {
		t.Error("HasA2ATarget should return true when URL is set")
	}
}

func TestHasA2ATarget_WrongType(t *testing.T) {
	registerA2ANodes()
	cond, ok := conditionRegistry["HasA2ATarget"]
	if !ok {
		t.Fatal("HasA2ATarget not registered")
	}

	bb := &Blackboard{
		ChainState: map[string]any{"a2a_target_url": 42},
	}
	if cond(bb) {
		t.Error("HasA2ATarget should return false for non-string value")
	}
}

// ─── SetA2ATarget Tests ──────────────────────────────────────────────────────

func TestSetA2ATarget_MissingURL(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["SetA2ATarget"]
	if !ok {
		t.Fatal("SetA2ATarget not registered")
	}

	bb := &Blackboard{}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != -1 {
		t.Errorf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}

func TestSetA2ATarget_WithWorldState(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["SetA2ATarget"]
	if !ok {
		t.Fatal("SetA2ATarget not registered")
	}

	ws := map[string]interface{}{}
	bb := &Blackboard{
		ChainState: map[string]any{
			"a2a_target_url":   "http://agent:8080",
			"goap_world_state": ws,
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != 1 {
		t.Errorf("expected success (1), got %d", status)
	}
	if v, ok := ws["has_a2a_target"]; !ok || v != true {
		t.Error("expected has_a2a_target=true in world state")
	}
}

func TestSetA2ATarget_NoWorldState(t *testing.T) {
	registerA2ANodes()
	action, ok := actionRegistry["SetA2ATarget"]
	if !ok {
		t.Fatal("SetA2ATarget not registered")
	}

	bb := &Blackboard{
		ChainState: map[string]any{
			"a2a_target_url": "http://agent:8080",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	status := action(ctx)
	if status != 1 {
		t.Errorf("expected success (1), got %d", status)
	}
	// No world state set, so nothing to verify — just check it doesn't panic
}
