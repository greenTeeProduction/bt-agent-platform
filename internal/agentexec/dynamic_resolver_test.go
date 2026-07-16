package agentexec

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestDynamicTreeResolverIsProductionWired pins ADR-133 Phase 0 at the same
// layer as the goap_fusion wiring guard: any binary that links agentexec
// (bt-agent, bt-agent-cli, bt-dashboard) must resolve runtime-generated
// tree-<id>.json files through domains.ResolveTreeID instead of silently
// falling back to DefaultTree.
func TestDynamicTreeResolverIsProductionWired(t *testing.T) {
	if domains.DynamicResolveFn == nil {
		t.Fatal("domains.DynamicResolveFn is nil — agentexec init() did not install the dynamic tree resolver")
	}
}

func TestResolveGeneratedTree_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	origDir := generatedTreeDir
	generatedTreeDir = dir
	defer func() { generatedTreeDir = origDir }()

	ts, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Generated_EndToEnd",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}
	if _, err := ts.SaveNamed("core:generated_end_to_end", tree); err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}

	// The full production path: ResolveTreeID falls through every builtin
	// mapping and lands on the wired dynamic resolver.
	resolved := domains.ResolveTreeID("core:generated_end_to_end")
	if resolved == nil || resolved.Name != "Generated_EndToEnd" {
		t.Fatalf("generated tree did not resolve through ResolveTreeID, got %+v", resolved)
	}

	// A never-persisted ID must keep the legacy DefaultTree fallback.
	fallback := domains.ResolveTreeID("core:never_generated")
	if fallback == nil {
		t.Fatal("expected DefaultTree fallback for unknown ID, got nil")
	}
	if fallback.Name == "Generated_EndToEnd" {
		t.Fatal("unknown ID wrongly resolved to a generated tree")
	}
}

// TestResolveGeneratedTree_UserWorkspaceFallback pins ADR-133 Phase 5:
// user-attributed compiles persist into users/<user>/trees instead of the
// shared store, and the dynamic resolver must find them there so scheduled
// automations keep executing the personal tree.
func TestResolveGeneratedTree_UserWorkspaceFallback(t *testing.T) {
	sharedDir := t.TempDir()
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = sharedDir, usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "goal:automate_reports",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}
	dir := usersRoot + "/nico/trees"
	if _, err := evolution.SaveNamedTree(dir, "goal:automate_reports", tree); err != nil {
		t.Fatalf("SaveNamedTree: %v", err)
	}

	resolved := ResolveGeneratedTree("goal:automate_reports")
	if resolved == nil || resolved.Name != "goal:automate_reports" {
		t.Fatalf("personal tree did not resolve via user-workspace fallback, got %+v", resolved)
	}

	if ResolveGeneratedTree("goal:never_generated") != nil {
		t.Fatal("unknown ID must not resolve from user workspaces")
	}
}
