package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestResolveTreeID_ConsultsDynamicResolver pins ADR-010 Phase 0: an ID with
// no compiled-in mapping must be offered to DynamicResolveFn before falling
// back to DefaultTree, so runtime-generated trees are actually executable.
func TestResolveTreeID_ConsultsDynamicResolver(t *testing.T) {
	orig := DynamicResolveFn
	defer func() { DynamicResolveFn = orig }()

	generated := &evolution.SerializableNode{Type: "Sequence", Name: "Generated_ByFactory"}
	var asked []string
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		asked = append(asked, id)
		if id == "core:generated_tree" {
			return generated
		}
		return nil
	}

	if got := ResolveTreeID("core:generated_tree"); got != generated {
		t.Fatalf("expected dynamic resolver's tree, got %v", got)
	}

	// Unknown everywhere → DefaultTree fallback, but the hook must have been asked.
	if got := ResolveTreeID("no:such_tree"); got == nil || got == generated {
		t.Fatalf("expected DefaultTree fallback, got %v", got)
	}
	if len(asked) != 2 || asked[1] != "no:such_tree" {
		t.Fatalf("dynamic resolver not consulted as expected: %v", asked)
	}
}

// TestResolveTreeID_BuiltinsShadowDynamicResolver ensures a generated tree can
// never hijack a compiled-in ID: builtins resolve first.
func TestResolveTreeID_BuiltinsShadowDynamicResolver(t *testing.T) {
	orig := DynamicResolveFn
	defer func() { DynamicResolveFn = orig }()

	called := false
	DynamicResolveFn = func(id string) *evolution.SerializableNode {
		called = true
		return &evolution.SerializableNode{Type: "Sequence", Name: "Hijack"}
	}

	tree := ResolveTreeID("godev")
	if tree == nil || tree.Name == "Hijack" {
		t.Fatalf("builtin godev must win over dynamic resolver, got %v", tree)
	}
	if called {
		t.Error("dynamic resolver must not be consulted for compiled-in IDs")
	}
}

// TestResolveTreeID_NilHookFallsBack guards the default (unwired) behavior.
func TestResolveTreeID_NilHookFallsBack(t *testing.T) {
	orig := DynamicResolveFn
	defer func() { DynamicResolveFn = orig }()
	DynamicResolveFn = nil

	if got := ResolveTreeID("no:such_tree"); got == nil {
		t.Fatal("expected DefaultTree fallback with nil hook, got nil")
	}
}
