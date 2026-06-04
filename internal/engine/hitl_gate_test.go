package engine

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestHumanApprovalGate_AutoApprove(t *testing.T) {
	dir := t.TempDir()
	_, err := hitl.InitStore(filepath.Join(dir, "hitl"))
	if err != nil {
		t.Fatal(err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: true, Timeout: hitl.DefaultPolicy().Timeout})
	defer hitl.SetPolicy(hitl.DefaultPolicy())

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "HumanApprovalGate",
				Name: "ApproveStep",
				Metadata: map[string]any{
					"prompt": "confirm",
				},
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
				},
			},
		},
	}
	bb := &Blackboard{Task: "test task", ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	code := cmd.Run(ctx)
	for i := 0; code == 0 && i < 5; i++ {
		code = cmd.Run(ctx)
	}
	if code != 1 {
		t.Fatalf("expected success after auto-approve, got %d outcome=%s", code, bb.Outcome)
	}
}

func TestHumanApprovalGate_ManualApprove(t *testing.T) {
	dir := t.TempDir()
	store, err := hitl.InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	defer hitl.SetPolicy(hitl.DefaultPolicy())

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type:     "HumanApprovalGate",
				Name:     "ApproveStep",
				Metadata: map[string]any{"prompt": "confirm risky action"},
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
				},
			},
		},
	}
	bb := &Blackboard{Task: "risky", ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	code := cmd.Run(ctx)
	if code != 0 {
		t.Fatalf("expected running/pending, got %d", code)
	}
	reqID, _ := bb.ChainState["hitl_request_id"].(string)
	if reqID == "" {
		t.Fatal("expected hitl_request_id on blackboard")
	}
	if _, err := store.Approve(reqID, "tester", "ok"); err != nil {
		t.Fatal(err)
	}
	for i := 0; code == 0 && i < 5; i++ {
		code = cmd.Run(ctx)
	}
	if code != 1 {
		t.Fatalf("expected success after approve, got %d outcome=%s", code, bb.Outcome)
	}
}

func TestHumanApprovalGate_PostPhase(t *testing.T) {
	dir := t.TempDir()
	store, err := hitl.InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	defer hitl.SetPolicy(hitl.DefaultPolicy())

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "HumanApprovalGate",
				Name: "PostReview",
				Metadata: map[string]any{
					"phase":  "post",
					"prompt": "review output",
				},
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "MarkSuccessful"},
				},
			},
		},
	}
	bb := &Blackboard{Task: "post task", Result: "draft output", ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	code := cmd.Run(ctx)
	if code != 0 {
		t.Fatalf("expected pending after child run, got %d", code)
	}
	if !childExecuted(bb) {
		t.Fatal("expected child to run before approval in post phase")
	}
	reqID, _ := bb.ChainState["hitl_request_id"].(string)
	if reqID == "" {
		t.Fatal("expected hitl_request_id")
	}
	req, ok := store.Get(reqID)
	if !ok || req.Phase != "post" {
		t.Fatalf("expected post phase request, got %+v ok=%v", req, ok)
	}
	if req.Proposed != "draft output" {
		t.Fatalf("expected proposed from result, got %q", req.Proposed)
	}
	if _, err := store.Approve(reqID, "tester", "ok"); err != nil {
		t.Fatal(err)
	}
	for i := 0; code == 0 && i < 5; i++ {
		code = cmd.Run(ctx)
	}
	if code != 1 {
		t.Fatalf("expected success after post approve, got %d", code)
	}
}
