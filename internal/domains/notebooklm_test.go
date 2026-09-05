package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// findChildByName returns the first child with the given name, or nil.
func findChildByName(node *evolution.SerializableNode, name string) *evolution.SerializableNode {
	for i := range node.Children {
		if node.Children[i].Name == name {
			return &node.Children[i]
		}
	}
	return nil
}

func TestNotebookLMTreeRoutesResearchBeforeIngestAndQuery(t *testing.T) {
	tree := NotebookLMTree()
	if tree == nil {
		t.Fatal("NotebookLMTree returned nil")
	}

	// The router Selector must be literally named "StrategyRouter" — the
	// platform-wide convention every other domain tree uses (see e.g.
	// CodeReviewTree/DevOpsCITree/SecurityAuditTree in trees.go). The engine's
	// buildNodeInner only records bb.CurrentPath/VisitedPaths for a Sequence
	// child whose parent Selector is named exactly "StrategyRouter" (see
	// internal/engine/tree.go). A differently-named router (previously
	// "NotebookLM_Router") silently disables real path attribution: benchmark
	// suites then fall back to keyword-guessed paths instead of the tree's
	// actual ResearchPath/QueryPath/DefaultPath nodes.
	router := findChildByName(tree, "StrategyRouter")
	if router == nil {
		t.Fatal("StrategyRouter not found in tree children — NotebookLM router must use the platform-wide StrategyRouter naming convention for path tracking to work")
	}
	if len(router.Children) < 2 {
		t.Fatalf("expected router paths, got %d", len(router.Children))
	}

	if got := router.Children[0].Name; got != "ResearchPath" {
		t.Fatalf("first router path = %q, want ResearchPath", got)
	}
	if got := router.Children[1].Name; got != "QueryPath" {
		t.Fatalf("second router path = %q, want QueryPath", got)
	}
}

// TestNotebookLMTreeRecordsRealPathDuringExecution guards the actual production
// defect behind milestone 2 of the SuiteForTree benchmark-gating fix: running
// NotebookLMTree() end-to-end (the same engine.BuildTree/engine.RunTask path
// benchmark.RunSuite uses to score mutations) must set bb.CurrentPath to the
// tree's real Sequence node name for the strategy that fired — "ResearchPath"
// or "QueryPath" — not leave it empty (which forces benchmark scoring to fall
// back to a keyword-guessed, non-existent "NotebookLMPath").
func TestNotebookLMTreeRecordsRealPathDuringExecution(t *testing.T) {
	tests := []struct {
		name     string
		task     string
		wantPath string
	}{
		{"research task", "run deep research on BT optimization and save sources", "ResearchPath"},
		{"query task", "ask the notebook what the key findings are", "QueryPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task, LLM: &engine.MockLLM{}, Sandbox: true}
			bt := engine.BuildTree(NotebookLMTree(), bb)
			engine.RunTask(bb, bt)
			if bb.CurrentPath != tt.wantPath {
				t.Fatalf("bb.CurrentPath = %q, want %q (real NotebookLMTree node name)", bb.CurrentPath, tt.wantPath)
			}
		})
	}
}

func TestNotebookLMTreeIsZeroLLM(t *testing.T) {
	tree := NotebookLMTree()

	// Walk entire tree — no ChainAction nodes allowed
	var chains []string
	walkTree(tree, func(node *evolution.SerializableNode) {
		if node.Type == "ChainAction" {
			chains = append(chains, node.Name)
		}
	})
	if len(chains) > 0 {
		t.Fatalf("tree contains ChainAction nodes (should be zero-LLM): %v", chains)
	}

	// Verify key action nodes exist
	requiredActions := []string{
		"CheckNotebookLMAuthAndRefresh",
		"GetNotebookLMNotebook",
		"ResearchNotebookLM",
		"QueryNotebookLM",
		"SaveNotebookLMFindings",
		"VerifyNotebookLMEvidence",
		"LoadNotebookLMState",
		"SaveNotebookLMState",
	}
	for _, name := range requiredActions {
		if !hasNode(tree, name) {
			t.Fatalf("tree missing required action: %s", name)
		}
	}
}

func TestNotebookLMTreeUsesDeterministicEvidenceGateBeforeSuccess(t *testing.T) {
	tree := NotebookLMTree()
	if tree == nil {
		t.Fatal("NotebookLMTree returned nil")
	}

	foundGate := -1
	foundOutcome := -1
	for i, child := range tree.Children {
		if child.Type == "Action" && child.Name == "VerifyNotebookLMEvidence" {
			foundGate = i
		}
		if child.Name == "OutcomeSelector" {
			foundOutcome = i
		}
	}
	if foundGate < 0 {
		t.Fatal("NotebookLM tree must include deterministic VerifyNotebookLMEvidence action")
	}
	if foundOutcome < 0 {
		t.Fatal("NotebookLM tree must include OutcomeSelector")
	}
	if foundGate >= foundOutcome {
		t.Fatalf("VerifyNotebookLMEvidence index=%d must run before OutcomeSelector index=%d", foundGate, foundOutcome)
	}
}

func TestNotebookLMTreeUsesZeroLLMActions(t *testing.T) {
	tree := NotebookLMTree()

	// PreGate should use CheckNotebookLMAuthAndRefresh (zero-LLM), not SetupNotebookLMTools or ChainAction
	pregate := findChildByName(tree, "NotebookLM_PreGate")
	if pregate == nil {
		t.Fatal("NotebookLM_PreGate not found")
	}
	foundAuth := false
	for _, child := range pregate.Children {
		if child.Type == "Action" && child.Name == "CheckNotebookLMAuthAndRefresh" {
			foundAuth = true
			break
		}
		if child.Type == "ChainAction" {
			t.Fatal("NotebookLMTree must not use ChainAction — it is zero-LLM")
		}
	}
	if !foundAuth {
		t.Fatal("NotebookLM_PreGate must include CheckNotebookLMAuthAndRefresh")
	}
}

func TestNotebookLMTreeIncludesIdempotencyStateActions(t *testing.T) {
	tree := NotebookLMTree()

	if !hasNode(tree, "LoadNotebookLMState") {
		t.Fatal("tree must include LoadNotebookLMState action for idempotency")
	}
	if !hasNode(tree, "SaveNotebookLMState") {
		t.Fatal("tree must include SaveNotebookLMState action for idempotency")
	}

	// Save must come before Verify but after the router
	foundSave := -1
	foundVerify := -1
	for i, child := range tree.Children {
		if child.Name == "SaveNotebookLMState" {
			foundSave = i
		}
		if child.Name == "VerifyNotebookLMEvidence" {
			foundVerify = i
		}
	}
	if foundSave >= foundVerify {
		t.Fatalf("SaveNotebookLMState index=%d must run before VerifyNotebookLMEvidence index=%d", foundSave, foundVerify)
	}
}

// hasNode walks the tree and returns true if any node has the given name.
func hasNode(node *evolution.SerializableNode, name string) bool {
	if node.Name == name {
		return true
	}
	for _, child := range node.Children {
		if hasNode(&child, name) {
			return true
		}
	}
	return false
}

// walkTree visits every node in the tree.
func walkTree(node *evolution.SerializableNode, fn func(*evolution.SerializableNode)) {
	fn(node)
	for i := range node.Children {
		walkTree(&node.Children[i], fn)
	}
}
