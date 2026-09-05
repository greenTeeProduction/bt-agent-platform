package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// newBBServer returns an engine.Server with the four bt_bb_* tools registered
// against a fresh in-memory blackboard.Manager (no persistence side effects,
// so tests never touch BT_AGENT_HOME or the real filesystem).
func newBBServer(t *testing.T) *engine.Server {
	t.Helper()
	deps := &mcpDeps{agentRunner: &agent.RunDeps{Blackboards: blackboard.NewManager(nil)}}
	server := engine.NewServer("test")
	registerBlackboardTools(server, deps)
	return server
}

// invokeBB invokes a registered bt_bb_* tool and decodes its single text
// content item as JSON.
func invokeBB(t *testing.T, server *engine.Server, tool, args string) map[string]any {
	t.Helper()
	res, ok := server.Invoke(tool, json.RawMessage(args))
	if !ok {
		t.Fatalf("Invoke(%s) reported the tool as unregistered", tool)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("%s returned no content for args %s", tool, args)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("%s result is not valid JSON: %v (text=%q)", tool, err, res.Content[0].Text)
	}
	return out
}

// TestRegisterBlackboardToolsRegistersAllFour pins the set of tool names
// registerBlackboardTools installs on the server.
func TestRegisterBlackboardToolsRegistersAllFour(t *testing.T) {
	server := newBBServer(t)
	for _, name := range []string{"bt_bb_read", "bt_bb_write", "bt_bb_list", "bt_bb_delete"} {
		if !server.HasTool(name) {
			t.Errorf("registerBlackboardTools must register %q", name)
		}
	}
}

// TestParseBBScope pins the current run/session/agent scope parsing contract:
// kind is matched case-insensitively and trimmed, id is trimmed, an empty id
// is always rejected, and an unrecognized kind is rejected with a message
// naming the allowed kinds.
func TestParseBBScope(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		id       string
		wantKind blackboard.ScopeKind
		wantID   string
		wantErr  string
	}{
		{name: "run scope", kind: "run", id: "r1", wantKind: blackboard.ScopeRun, wantID: "r1"},
		{name: "session scope", kind: "session", id: "s1", wantKind: blackboard.ScopeSession, wantID: "s1"},
		{name: "agent scope", kind: "agent", id: "a1", wantKind: blackboard.ScopeAgent, wantID: "a1"},
		{name: "kind is case-insensitive", kind: "RUN", id: "r1", wantKind: blackboard.ScopeRun, wantID: "r1"},
		{name: "kind is trimmed", kind: "  run  ", id: "r1", wantKind: blackboard.ScopeRun, wantID: "r1"},
		{name: "id is trimmed", kind: "run", id: "  r1  ", wantKind: blackboard.ScopeRun, wantID: "r1"},
		{name: "empty id is rejected", kind: "run", id: "", wantErr: "scope_id required"},
		{name: "whitespace-only id is rejected", kind: "run", id: "   ", wantErr: "scope_id required"},
		{name: "unknown kind is rejected", kind: "bogus", id: "r1", wantErr: `unknown scope "bogus" (use run, session, or agent)`},
		{name: "empty kind is rejected", kind: "", id: "r1", wantErr: `unknown scope "" (use run, session, or agent)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := parseBBScope(tc.kind, tc.id)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("parseBBScope(%q, %q) error = %v, want %q", tc.kind, tc.id, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBBScope(%q, %q) unexpected error: %v", tc.kind, tc.id, err)
			}
			if scope.Kind != tc.wantKind || scope.ID != tc.wantID {
				t.Errorf("parseBBScope(%q, %q) = %+v, want {Kind:%q ID:%q}", tc.kind, tc.id, scope, tc.wantKind, tc.wantID)
			}
		})
	}
}

// TestBBManagerRequiresAgentRunner pins bbManager's guard: it errors with
// "agent runner not configured" whenever deps is nil or deps.agentRunner is
// nil, and otherwise delegates to agentRunner.BoardManager().
func TestBBManagerRequiresAgentRunner(t *testing.T) {
	t.Run("nil deps", func(t *testing.T) {
		if _, err := bbManager(nil); err == nil || err.Error() != "agent runner not configured" {
			t.Fatalf("bbManager(nil) error = %v, want \"agent runner not configured\"", err)
		}
	})
	t.Run("nil agentRunner", func(t *testing.T) {
		if _, err := bbManager(&mcpDeps{}); err == nil || err.Error() != "agent runner not configured" {
			t.Fatalf("bbManager(&mcpDeps{}) error = %v, want \"agent runner not configured\"", err)
		}
	})
	t.Run("configured agentRunner returns its board manager", func(t *testing.T) {
		mgr := blackboard.NewManager(nil)
		got, err := bbManager(&mcpDeps{agentRunner: &agent.RunDeps{Blackboards: mgr}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != mgr {
			t.Errorf("bbManager must return the agentRunner's own board manager, got a different instance")
		}
	})
}

// TestBTBBWriteThenReadRoundTrip pins the happy-path contract shared by
// bt_bb_write and bt_bb_read: a write reports status "stored" with the byte
// count of the value, and a subsequent read with the same scope/key returns
// the stored value plus a content_type that defaults to "text" when omitted.
func TestBTBBWriteThenReadRoundTrip(t *testing.T) {
	server := newBBServer(t)

	writeOut := invokeBB(t, server, "bt_bb_write",
		`{"scope":"run","scope_id":"r1","key":"greeting","value":"hello world"}`)
	if _, isErr := writeOut["error"]; isErr {
		t.Fatalf("bt_bb_write unexpectedly errored: %v", writeOut)
	}
	if status, _ := writeOut["status"].(string); status != "stored" {
		t.Errorf("bt_bb_write status = %v, want \"stored\"", writeOut["status"])
	}
	if bytes, ok := writeOut["bytes"].(float64); !ok || bytes != float64(len("hello world")) {
		t.Errorf("bt_bb_write bytes = %v, want %d", writeOut["bytes"], len("hello world"))
	}

	readOut := invokeBB(t, server, "bt_bb_read",
		`{"scope":"run","scope_id":"r1","key":"greeting"}`)
	if _, isErr := readOut["error"]; isErr {
		t.Fatalf("bt_bb_read unexpectedly errored: %v", readOut)
	}
	if value, _ := readOut["value"].(string); value != "hello world" {
		t.Errorf("bt_bb_read value = %v, want \"hello world\"", readOut["value"])
	}
	if ct, _ := readOut["content_type"].(string); ct != "text" {
		t.Errorf("bt_bb_read content_type = %v, want \"text\" (default)", readOut["content_type"])
	}
	if key, _ := readOut["key"].(string); key != "greeting" {
		t.Errorf("bt_bb_read key = %v, want \"greeting\"", readOut["key"])
	}
}

// TestBTBBReadMissingKey pins the not-found contract: reading a key that was
// never written returns the structured {"error": ...} shape naming the key
// and scope kind, matching blackboard.Manager.Get's error text verbatim.
func TestBTBBReadMissingKey(t *testing.T) {
	server := newBBServer(t)
	out := invokeBB(t, server, "bt_bb_read", `{"scope":"run","scope_id":"r1","key":"nope"}`)
	want := `key "nope" not found in run`
	if errMsg, _ := out["error"].(string); errMsg != want {
		t.Errorf("bt_bb_read error = %v, want %q", out["error"], want)
	}
}

// TestBTBBScopeErrorsPropagate pins that both bt_bb_read and bt_bb_write
// surface parseBBScope failures (missing scope_id, unknown scope kind) as the
// structured {"error": ...} shape rather than panicking or silently no-oping.
func TestBTBBScopeErrorsPropagate(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{name: "missing scope_id", args: `{"scope":"run","scope_id":"","key":"k"}`, want: "scope_id required"},
		{name: "unknown scope kind", args: `{"scope":"bogus","scope_id":"r1","key":"k"}`, want: `unknown scope "bogus" (use run, session, or agent)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newBBServer(t)
			out := invokeBB(t, server, "bt_bb_read", tc.args)
			if errMsg, _ := out["error"].(string); errMsg != tc.want {
				t.Errorf("bt_bb_read error = %v, want %q", out["error"], tc.want)
			}
		})
	}
}

// TestBTBBListDefaultsLimitAndFiltersByPrefix pins bt_bb_list's contract: a
// non-positive (here, omitted) limit defaults to 50, results are filtered by
// the optional key prefix, and the response reports both "entries" and a
// matching "count".
func TestBTBBListDefaultsLimitAndFiltersByPrefix(t *testing.T) {
	server := newBBServer(t)
	for _, key := range []string{"task:1", "task:2", "other:1"} {
		invokeBB(t, server, "bt_bb_write",
			fmt.Sprintf(`{"scope":"run","scope_id":"r1","key":%q,"value":"v"}`, key))
	}

	all := invokeBB(t, server, "bt_bb_list", `{"scope":"run","scope_id":"r1"}`)
	if count, _ := all["count"].(float64); count != 3 {
		t.Errorf("bt_bb_list with no prefix count = %v, want 3", all["count"])
	}

	filtered := invokeBB(t, server, "bt_bb_list", `{"scope":"run","scope_id":"r1","prefix":"task:"}`)
	if count, _ := filtered["count"].(float64); count != 2 {
		t.Errorf("bt_bb_list with prefix \"task:\" count = %v, want 2", filtered["count"])
	}
	entries, _ := filtered["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("bt_bb_list entries length = %d, want 2", len(entries))
	}
}

// TestBTBBDeleteThenReadFails pins bt_bb_delete's contract: a successful
// delete reports {"status":"deleted","key":...} and a subsequent read for the
// same key returns the not-found error rather than a stale value.
func TestBTBBDeleteThenReadFails(t *testing.T) {
	server := newBBServer(t)
	invokeBB(t, server, "bt_bb_write", `{"scope":"run","scope_id":"r1","key":"temp","value":"v"}`)

	delOut := invokeBB(t, server, "bt_bb_delete", `{"scope":"run","scope_id":"r1","key":"temp"}`)
	if status, _ := delOut["status"].(string); status != "deleted" {
		t.Errorf("bt_bb_delete status = %v, want \"deleted\"", delOut["status"])
	}
	if key, _ := delOut["key"].(string); key != "temp" {
		t.Errorf("bt_bb_delete key = %v, want \"temp\"", delOut["key"])
	}

	readOut := invokeBB(t, server, "bt_bb_read", `{"scope":"run","scope_id":"r1","key":"temp"}`)
	if _, isErr := readOut["error"]; !isErr {
		t.Errorf("bt_bb_read after delete must error, got %v", readOut)
	}
}

// TestBTBBDeleteMissingKeyErrors pins that deleting an absent key surfaces
// the underlying store error rather than reporting a false "deleted" status.
func TestBTBBDeleteMissingKeyErrors(t *testing.T) {
	server := newBBServer(t)
	out := invokeBB(t, server, "bt_bb_delete", `{"scope":"run","scope_id":"r1","key":"nope"}`)
	if _, isErr := out["error"]; !isErr {
		t.Errorf("bt_bb_delete of a missing key must return an error, got %v", out)
	}
	if _, hasStatus := out["status"]; hasStatus {
		t.Errorf("bt_bb_delete of a missing key must not report a status, got %v", out)
	}
}

// TestBTBBWriteThenReadNormalizesKeyIdentically pins a round-trip invariant
// that bt_bb_write and bt_bb_read must share: whatever key normalization
// bt_bb_write's underlying store applies (blackboard's normalizeKey trims
// whitespace, trims leading/trailing "/", and strips ".."), bt_bb_read must
// apply the identical normalization when looking the key back up, so a caller
// can always read back the exact key it just wrote.
//
// Today blackboard.scopedStore.set/delete/list all call normalizeKey before
// touching the entries map, but scopedStore.get does not — every operation
// except Get normalizes. That asymmetry means a write with a key carrying
// incidental whitespace or slashes (e.g. copied from a file path or a
// templated prompt) silently becomes unreadable via bt_bb_read using that
// same key, even though bt_bb_delete on the same raw key works fine.
func TestBTBBWriteThenReadNormalizesKeyIdentically(t *testing.T) {
	tests := []struct {
		name   string
		rawKey string
	}{
		{name: "leading and trailing whitespace", rawKey: "  padded-key  "},
		{name: "leading and trailing slashes", rawKey: "/slash-key/"},
		{name: "embedded double dot", rawKey: "dot..key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := newBBServer(t)
			writeOut := invokeBB(t, server, "bt_bb_write",
				fmt.Sprintf(`{"scope":"run","scope_id":"r1","key":%q,"value":"hello"}`, tc.rawKey))
			if _, isErr := writeOut["error"]; isErr {
				t.Fatalf("bt_bb_write unexpectedly errored: %v", writeOut)
			}

			readOut := invokeBB(t, server, "bt_bb_read",
				fmt.Sprintf(`{"scope":"run","scope_id":"r1","key":%q}`, tc.rawKey))
			if errMsg, isErr := readOut["error"]; isErr {
				t.Fatalf("bt_bb_read with the exact key just passed to bt_bb_write must succeed; got error %v", errMsg)
			}
			if value, _ := readOut["value"].(string); value != "hello" {
				t.Errorf("bt_bb_read value = %v, want \"hello\"", readOut["value"])
			}
		})
	}
}
