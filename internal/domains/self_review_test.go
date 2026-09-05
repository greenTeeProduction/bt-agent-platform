package domains

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestSelfReviewTree_StructureBeforeWrap pins the raw (pre-wrap) tree shape:
// a plain Sequence of exactly TaskIsNotEmpty → RunSelfReview → MarkSuccessful,
// mirroring Arc42SeederTree. See the package comment on SelfReviewTree for
// why this stays a single composite action instead of the spec's literal
// four-stage decomposition.
func TestSelfReviewTree_StructureBeforeWrap(t *testing.T) {
	tree := SelfReviewTree()
	if tree == nil {
		t.Fatal("SelfReviewTree returned nil")
	}
	if tree.Type != "Sequence" {
		t.Fatalf("root type = %q, want Sequence (unwrapped)", tree.Type)
	}
	names := make([]string, 0, len(tree.Children))
	for _, c := range tree.Children {
		names = append(names, c.Name)
	}
	want := []string{"TaskIsNotEmpty", "RunSelfReview", "MarkSuccessful"}
	if len(names) != len(want) {
		t.Fatalf("children = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("children = %v, want %v", names, want)
		}
	}
	for _, c := range tree.Children {
		if strings.TrimSpace(c.Description) == "" {
			t.Errorf("child %q has an empty Description", c.Name)
		}
	}
	if strings.TrimSpace(tree.Description) == "" {
		t.Error("root has an empty Description")
	}
}

// TestSelfReviewTree_RegisteredAndDescribed mirrors the auction_demo
// registration guard in domains_test.go.
func TestSelfReviewTree_RegisteredAndDescribed(t *testing.T) {
	if _, ok := AllDomainTrees()["self_review"]; !ok {
		t.Error("self_review not registered in AllDomainTrees")
	}
	if strings.TrimSpace(Descriptions["self_review"]) == "" {
		t.Error("self_review missing a Descriptions entry")
	}
}

// TestSelfReviewTree_WrappedInClaudeErrorHandler mirrors
// TestAllDomainTreesWrappedInClaudeErrorHandler / TestResolveDomainTreeIsWrapped
// in error_handler_wrap_test.go: every AllDomainTrees() entry gets the
// self-extending error handler at the root; SelfReviewTree() itself (called
// directly) must NOT be pre-wrapped.
func TestSelfReviewTree_WrappedInClaudeErrorHandler(t *testing.T) {
	wrapped := AllDomainTrees()["self_review"]
	if wrapped == nil {
		t.Fatal("self_review tree missing from AllDomainTrees")
	}
	if wrapped.Type != "ClaudeErrorHandler" {
		t.Fatalf("root type = %q, want ClaudeErrorHandler", wrapped.Type)
	}
	if len(wrapped.Children) != 1 || wrapped.Children[0].Name != "SelfReview_Main" {
		t.Fatalf("wrapper child = %+v, want SelfReview_Main", wrapped.Children)
	}
}

// TestSelfReviewTree_ActionsRegistered ensures every act()/cond() name in the
// tree resolves to a real registered action/condition — the no-silent-no-op
// guard this tree is built to satisfy (see AuctionDemoTree /
// TestAuctionDemoTreeHasNoSilentNoOps in domains_test.go for the failure mode
// this exists to avoid: unregistered act() names silently succeed via the
// engine's permissive unknown-action fallback).
func TestSelfReviewTree_ActionsRegistered(t *testing.T) {
	for _, name := range []string{"RunSelfReview", "MarkSuccessful"} {
		if engine.GetAction(name) == nil {
			t.Errorf("action %q not registered in engine", name)
		}
	}
	if engine.GetCondition("TaskIsNotEmpty") == nil {
		t.Error("condition TaskIsNotEmpty not registered in engine")
	}
}
