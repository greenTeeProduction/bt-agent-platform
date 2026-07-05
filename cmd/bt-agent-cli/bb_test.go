package main

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func TestParseBBScopeFlagTrimsID(t *testing.T) {
	cases := []struct {
		name     string
		scope    string
		id       string
		wantKind blackboard.ScopeKind
		wantID   string
	}{
		{name: "agent padded spaces", scope: "agent", id: " agent-1 ", wantKind: blackboard.ScopeAgent, wantID: "agent-1"},
		{name: "session tab and newline", scope: "session", id: "\tsess-9\n", wantKind: blackboard.ScopeSession, wantID: "sess-9"},
		{name: "run trailing space", scope: "run", id: "run-3 ", wantKind: blackboard.ScopeRun, wantID: "run-3"},
		{name: "agent already clean", scope: "agent", id: "agent-2", wantKind: blackboard.ScopeAgent, wantID: "agent-2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc, err := parseBBScopeFlag(tc.scope, tc.id)
			if err != nil {
				t.Fatalf("parseBBScopeFlag(%q, %q) returned error: %v", tc.scope, tc.id, err)
			}
			if sc.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", sc.Kind, tc.wantKind)
			}
			if sc.ID != tc.wantID {
				t.Errorf("ID = %q, want %q (daemon parseBBScope trims; CLI must match)", sc.ID, tc.wantID)
			}
		})
	}
}

func TestBBScopeCrossToolRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Daemon side: parseBBScope trims the id before writing, so the entry
	// lands under the clean scope id.
	daemon := blackboard.DefaultManager()
	if err := daemon.EnablePersistence(dir); err != nil {
		t.Fatalf("EnablePersistence(%q) failed: %v", dir, err)
	}
	daemonScope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: "agent-1"}
	if err := daemon.Set(daemonScope, "status/last", "ok", "daemon-written", "text/plain"); err != nil {
		t.Fatalf("daemon Set failed: %v", err)
	}

	// CLI side: user passes a padded --id; the CLI scope must resolve the
	// same persisted entry through the production manager-construction path.
	cliScope, err := parseBBScopeFlag("agent", " agent-1 ")
	if err != nil {
		t.Fatalf("parseBBScopeFlag failed: %v", err)
	}
	cli := bbManagerAt(dir)

	entry, err := cli.Get(cliScope, "status/last")
	if err != nil {
		t.Fatalf("CLI Get missed daemon-written key: %v", err)
	}
	if entry.Value != "ok" {
		t.Errorf("Get value = %q, want %q", entry.Value, "ok")
	}

	entries, err := cli.List(cliScope, "", 50)
	if err != nil {
		t.Fatalf("CLI List failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Key != "status/last" {
		t.Errorf("List = %+v, want single entry with key status/last", entries)
	}
}

func TestParseBBScopeFlagErrors(t *testing.T) {
	t.Run("whitespace-only id", func(t *testing.T) {
		_, err := parseBBScopeFlag("agent", "  \t ")
		if err == nil || !strings.Contains(err.Error(), "--id required") {
			t.Fatalf("err = %v, want --id required", err)
		}
	})
	t.Run("unknown scope kind", func(t *testing.T) {
		_, err := parseBBScopeFlag("cluster", "id-1")
		if err == nil || !strings.Contains(err.Error(), "--scope must be run, session, or agent") {
			t.Fatalf("err = %v, want scope-kind error", err)
		}
	})
}
