package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blocks"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestBTBlocksComposeRejectsUnknownStrategyTree pins the unknown-strategy
// contract of bt_blocks_compose: a non-empty 'strategy' parameter that
// resolveTree cannot resolve (nil — e.g. the typo "domain:code_reviw") must
// fail fast with the structured {"error":"unknown strategy tree \"<id>\""}
// shape in all three compose branches — preset, task_tree, and the default
// block_ids path. Today each branch silently drops the nil StrategyRouter and
// reports composed:true, so a caller who requested condition-node routing gets
// a tree with that routing amputated and no signal anything went wrong. The
// rejection must carry no partial happy-path result. An empty strategy remains
// valid (no router requested), and a resolvable strategy id must keep
// composing successfully on every branch.
func TestBTBlocksComposeRejectsUnknownStrategyTree(t *testing.T) {
	server := engine.NewServer("test")
	registerBlockTools(server, &mcpDeps{})

	if !server.HasTool("bt_blocks_compose") {
		t.Fatal("bt_blocks_compose tool must be registered by registerBlockTools")
	}

	invoke := func(t *testing.T, args string) map[string]any {
		t.Helper()
		res, ok := server.Invoke("bt_blocks_compose", json.RawMessage(args))
		if !ok {
			t.Fatal("Invoke(bt_blocks_compose) reported the tool as unregistered")
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_blocks_compose returned no content for args %s", args)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_blocks_compose result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
		}
		return out
	}

	// Each branch of the compose switch resolves the strategy independently,
	// so all three must reject an unresolvable id.
	branches := []struct {
		name    string
		badArgs string
		okArgs  string
	}{
		{
			name:    "default block_ids branch",
			badArgs: `{"block_ids":"core:tool_execution","strategy":"domain:__no_such_tree__"}`,
			okArgs:  `{"block_ids":"core:tool_execution","strategy":"domain:code_review"}`,
		},
		{
			name:    "task_tree branch",
			badArgs: `{"block_ids":"core:tool_execution","task_tree":true,"strategy":"domain:__no_such_tree__"}`,
			okArgs:  `{"block_ids":"core:tool_execution","task_tree":true,"strategy":"domain:code_review"}`,
		},
		{
			name:    "preset branch",
			badArgs: `{"block_ids":"core:tool_execution","preset":"default","strategy":"domain:__no_such_tree__"}`,
			okArgs:  `{"block_ids":"core:tool_execution","preset":"default","strategy":"domain:code_review"}`,
		},
	}
	for _, branch := range branches {
		t.Run(branch.name, func(t *testing.T) {
			out := invoke(t, branch.badArgs)
			if out["error"] != `unknown strategy tree "domain:__no_such_tree__"` {
				t.Errorf("bt_blocks_compose must reject an unresolvable strategy with {\"error\":\"unknown strategy tree \\\"domain:__no_such_tree__\\\"\"}; got %v", out)
			}
			if _, partial := out["composed"]; partial {
				t.Errorf("bt_blocks_compose unknown-strategy rejection must not carry a partial 'composed' result; got %v", out)
			}

			// The same branch with a resolvable strategy id must keep composing.
			ok := invoke(t, branch.okArgs)
			if _, isErr := ok["error"]; isErr {
				t.Fatalf("bt_blocks_compose unexpectedly rejected the resolvable strategy domain:code_review: %v", ok)
			}
			if composed, isBool := ok["composed"].(bool); !isBool || !composed {
				t.Errorf("bt_blocks_compose must report composed:true for a resolvable strategy; got %v", ok["composed"])
			}
		})
	}

	// Omitting the strategy entirely stays valid: no router was requested.
	none := invoke(t, `{"block_ids":"core:tool_execution"}`)
	if _, isErr := none["error"]; isErr {
		t.Fatalf("bt_blocks_compose must not reject an omitted strategy: %v", none)
	}
	if composed, isBool := none["composed"].(bool); !isBool || !composed {
		t.Errorf("bt_blocks_compose must report composed:true when no strategy is requested; got %v", none["composed"])
	}
}

// TestBTBlocksComposeSaveGatesActivation pins the save:true contract of
// bt_blocks_compose: activation of the composed tree as the live agent tree
// must be gated on engine.ValidateTree returning no messages AND on
// treeStore.Save succeeding. Today the handler ignores the Save error and
// unconditionally runs `*deps.bt = engine.BuildTree(tree, deps.bb)`, so an
// invalid composition (an Action with no registered handler — the
// auction_demo precedent) replaces the live tree with a dead-lettering
// fallback command and reports composed:true with no signal, and a failed
// Save silently activates a tree that will not survive a restart. On either
// failure the tool must return the structured {"error": ...} shape carrying
// the validation messages / save error, leave the active tree untouched, and
// not report a partial happy-path result. A valid composition that saves
// must include "saved": true in the success payload.
func TestBTBlocksComposeSaveGatesActivation(t *testing.T) {
	// A block whose schema is valid (known node types, so Registry.Register
	// accepts it) but whose Action has no engine handler, so the composed
	// tree fails engine.ValidateTree. DefaultRegistry has no Unregister; the
	// unique custom id and Mutable:false keep the block inert for every
	// other test in this binary.
	const bogusBlockID = "custom:task2_bogus_compose_gate"
	const bogusAction = "Task2BogusUnregisteredAction"
	if err := blocks.DefaultRegistry.Register(blocks.Block{
		ID:          bogusBlockID,
		Name:        "Task2BogusComposeGate",
		Description: "test-only block whose Action has no registered engine handler",
		Tree: &evolution.SerializableNode{
			Type: "Sequence",
			Name: "Task2BogusComposeGateSeq",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: bogusAction, Description: "no handler registered"},
			},
		},
		Mutable: false,
	}); err != nil {
		t.Fatalf("register bogus block: %v", err)
	}

	// newHarness returns an isolated server whose deps carry a nil live-tree
	// sentinel: any activation flips *live to non-nil, so "the active tree
	// was left untouched" is exactly *live == nil.
	newHarness := func(t *testing.T) (*engine.Server, *btcore.Command[engine.Blackboard], *evolution.TreeStore, string) {
		t.Helper()
		dir := t.TempDir()
		store, err := evolution.NewTreeStore(dir)
		if err != nil {
			t.Fatalf("tree store: %v", err)
		}
		var live btcore.Command[engine.Blackboard]
		deps := &mcpDeps{bb: &engine.Blackboard{}, bt: &live, treeStore: store}
		server := engine.NewServer("test")
		registerBlockTools(server, deps)
		return server, &live, store, dir
	}

	invoke := func(t *testing.T, server *engine.Server, args string) map[string]any {
		t.Helper()
		res, ok := server.Invoke("bt_blocks_compose", json.RawMessage(args))
		if !ok {
			t.Fatal("Invoke(bt_blocks_compose) reported the tool as unregistered")
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_blocks_compose returned no content for args %s", args)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_blocks_compose result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
		}
		return out
	}

	t.Run("invalid composition with save true is rejected and never activates", func(t *testing.T) {
		server, live, store, _ := newHarness(t)
		out := invoke(t, server,
			`{"name":"Task2Invalid","block_ids":"`+bogusBlockID+`","inline":true,"save":true}`)
		errMsg, _ := out["error"].(string)
		if errMsg == "" || !strings.Contains(errMsg, bogusAction) {
			t.Errorf("save:true with an invalid tree must return an error carrying the validation message %q; got %v",
				bogusAction, out)
		}
		if _, partial := out["composed"]; partial {
			t.Errorf("validation rejection must not carry a partial 'composed' result; got %v", out)
		}
		if *live != nil {
			t.Error("invalid composition must leave the active tree untouched, but *deps.bt was replaced")
		}
		if persisted, err := store.Load(); err != nil || persisted != nil {
			t.Errorf("invalid composition must not be persisted; treeStore.Load() = %v, err = %v", persisted, err)
		}
	})

	t.Run("save failure surfaces and never activates", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: read-only dir cannot force a Save failure")
		}
		server, live, _, dir := newHarness(t)
		// A read-only store dir makes TreeStore.Save fail at the tmp-file write.
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatalf("chmod store dir read-only: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		out := invoke(t, server,
			`{"name":"Task2SaveFail","block_ids":"core:tool_execution","inline":true,"save":true}`)
		if errMsg, _ := out["error"].(string); errMsg == "" {
			t.Errorf("a failed treeStore.Save must surface as an MCP error; got %v", out)
		}
		if saved, isBool := out["saved"].(bool); isBool && saved {
			t.Errorf("must not report saved:true when Save failed; got %v", out)
		}
		if *live != nil {
			t.Error("a failed Save must leave the active tree untouched, but *deps.bt was replaced")
		}
	})

	t.Run("valid composition with save true activates and reports saved", func(t *testing.T) {
		server, live, store, _ := newHarness(t)
		out := invoke(t, server,
			`{"name":"Task2Valid","block_ids":"core:tool_execution","inline":true,"save":true}`)
		if _, isErr := out["error"]; isErr {
			t.Fatalf("valid save:true composition must not error: %v", out)
		}
		if composed, isBool := out["composed"].(bool); !isBool || !composed {
			t.Errorf("valid save:true composition must report composed:true; got %v", out["composed"])
		}
		if saved, isBool := out["saved"].(bool); !isBool || !saved {
			t.Errorf("success payload must include saved:true after a gated save; got %v", out["saved"])
		}
		if *live == nil {
			t.Error("valid save:true composition must replace the active tree, but *deps.bt is still the nil sentinel")
		}
		persisted, err := store.Load()
		if err != nil || persisted == nil || persisted.Name != "Task2Valid" {
			t.Errorf("valid save:true composition must persist the composed tree; treeStore.Load() = %v, err = %v",
				persisted, err)
		}
	})
}

// blockOnHeldServerLock is the shared assertion behind the regression tests
// below: it registers a throwaway tool through server.RegisterBlackboardTool
// that blocks until released, invokes it in a goroutine to occupy the
// Server-wide mutex (internal/engine/mcp_server.go's bbMu, shared by every
// tool registered via RegisterBlackboardTool), runs fn, and checks that fn
// does NOT complete while that lock is held. A handler registered via the
// plain server.RegisterTool never contends on the shared mutex, so it races
// straight through and the "still blocked" assertion fails — that is the RED
// signal this milestone closes. Once fn's target handler is correctly
// registered via RegisterBlackboardTool, it blocks until the lock is
// released here, then completes.
func blockOnHeldServerLock(t *testing.T, server *engine.Server, fn func(), desc string) {
	t.Helper()
	release := make(chan struct{})
	held := make(chan struct{})
	server.RegisterBlackboardTool("test_hold_server_lock", "test-only lock holder",
		map[string]engine.Property{}, nil,
		func(_ json.RawMessage) *engine.ToolResult {
			close(held)
			<-release
			return &engine.ToolResult{}
		})
	go server.Invoke("test_hold_server_lock", json.RawMessage(`{}`))
	<-held

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		t.Errorf("%s completed while the server-wide blackboard lock was held by another handler — "+
			"it must be registered via server.RegisterBlackboardTool to contend on that lock", desc)
	case <-time.After(100 * time.Millisecond):
		// Expected: still blocked waiting on the server-wide lock.
	}
	close(release)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s never completed after the server-wide lock was released — deadlock", desc)
	}
}

// TestBTBlocksComposeSaveHoldsServerLock pins milestone 4/4 of the Q1
// Correctness / Q3 Reliability mcpDeps shared-blackboard race program:
// bt_blocks_compose's save:true branch (blocks_tools.go) writes
// deps.bb.TreeStore and swaps *deps.bt, and must be registered via
// server.RegisterBlackboardTool so that mutation contends on the same
// Server-wide mutex bt_run_task and the other migrated tools now share. RED
// before the fix: the handler registers via the plain server.RegisterTool,
// so it races straight through even while the lock is held by another
// caller.
func TestBTBlocksComposeSaveHoldsServerLock(t *testing.T) {
	dir := t.TempDir()
	store, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatalf("tree store: %v", err)
	}
	var live btcore.Command[engine.Blackboard]
	deps := &mcpDeps{bb: &engine.Blackboard{}, bt: &live, treeStore: store}
	server := engine.NewServer("test")
	registerBlockTools(server, deps)

	blockOnHeldServerLock(t, server, func() {
		server.Invoke("bt_blocks_compose", json.RawMessage(
			`{"block_ids":"core:tool_execution","inline":true,"save":true}`))
	}, "bt_blocks_compose(save:true)")
}

// TestBTHITLComposeTaskSaveHoldsServerLock is the hitl_tools.go counterpart:
// bt_hitl_compose_task's save:true branch also swaps *deps.bt and must be
// registered via server.RegisterBlackboardTool, for the same reason as
// TestBTBlocksComposeSaveHoldsServerLock.
func TestBTHITLComposeTaskSaveHoldsServerLock(t *testing.T) {
	dir := t.TempDir()
	store, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatalf("tree store: %v", err)
	}
	var live btcore.Command[engine.Blackboard]
	deps := &mcpDeps{bb: &engine.Blackboard{}, bt: &live, treeStore: store}
	server := engine.NewServer("test")
	registerHITLTools(server, deps)

	blockOnHeldServerLock(t, server, func() {
		server.Invoke("bt_hitl_compose_task", json.RawMessage(
			`{"name":"Task2HITLLockRegression","save":true}`))
	}, "bt_hitl_compose_task(save:true)")
}
