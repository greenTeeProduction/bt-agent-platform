package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// Phase 5 tests: user feedback as fitness, compile-time seed reflections, and
// per-user tree persistence.

func newFeedbackDeps(t *testing.T) *mcpDeps {
	t.Helper()
	refStore, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("ref store: %v", err)
	}
	personaStore, err := persona.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("persona store: %v", err)
	}
	treeStore, err := evolution.NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatalf("tree store: %v", err)
	}
	return &mcpDeps{refStore: refStore, personaStore: personaStore, treeStore: treeStore}
}

func TestRecordUserFeedback_FeedsSatisfactionAndFlags(t *testing.T) {
	deps := newFeedbackDeps(t)

	result := recordUserFeedback(deps, "nico", "goal:automate_reports", "positive", "great summary")
	if result["recorded"] != true || result["satisfaction"] != 1.0 {
		t.Fatalf("positive feedback not recorded: %v", result)
	}

	// First negative: recorded but not yet flagged.
	result = recordUserFeedback(deps, "nico", "goal:automate_reports", "👎", "wrong currency")
	if result["recorded"] != true {
		t.Fatalf("negative feedback not recorded: %v", result)
	}
	if result["flagged_for_review"] == true {
		t.Error("one negative must not flag the tree yet")
	}

	// Second negative crosses the review threshold.
	result = recordUserFeedback(deps, "nico", "goal:automate_reports", "negative", "")
	if result["flagged_for_review"] != true {
		t.Errorf("two negatives must flag the tree for review: %v", result)
	}
	if sat, _ := result["satisfaction"].(float64); sat < 0.33 || sat > 0.34 {
		t.Errorf("satisfaction = %v, want 1/3", result["satisfaction"])
	}

	// The records carry the feedback signal the evaluator folds into fitness.
	all, err := deps.refStore.LoadAll()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	records := evolution.FilterByTreeNameStrict(all, "goal:automate_reports")
	if len(records) != 3 {
		t.Fatalf("expected 3 feedback records, got %d", len(records))
	}
	withSignal := 0
	for _, r := range records {
		if r.UserFeedback != "" {
			withSignal++
		}
		if r.User != "nico" {
			t.Errorf("record not attributed to user: %+v", r)
		}
	}
	if withSignal != 3 {
		t.Errorf("all records must carry UserFeedback, got %d/3", withSignal)
	}
	// Comment text lands in the reflection so evolution sees the correction.
	found := false
	for _, r := range records {
		for _, s := range r.WhatToImprove {
			if strings.Contains(s, "wrong currency") {
				found = true
			}
		}
	}
	if !found {
		t.Error("correction comment must be stored in WhatToImprove")
	}
}

func TestRecordUserFeedback_RejectsBadInput(t *testing.T) {
	deps := newFeedbackDeps(t)
	if res := recordUserFeedback(deps, "nico", "goal:x", "meh", ""); res["error"] == nil {
		t.Errorf("invalid signal must error: %v", res)
	}
	if res := recordUserFeedback(deps, "", "goal:x", "positive", ""); res["error"] == nil {
		t.Errorf("missing user must error: %v", res)
	}
	if res := recordUserFeedback(deps, "nico", "", "positive", ""); res["error"] == nil {
		t.Errorf("missing tree must error: %v", res)
	}
}

func TestBTFeedbackRegistered(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerMCPTools(server, deps)
	if !server.HasTool("bt_feedback") {
		t.Fatal("bt_feedback must be registered by registerMCPTools")
	}
	res, ok := server.Invoke("bt_feedback", json.RawMessage(`{"user":"nico","tree":"goal:x","signal":"positive"}`))
	if !ok || res == nil || len(res.Content) == 0 {
		t.Fatal("bt_feedback returned no content")
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if out["recorded"] != true {
		t.Fatalf("expected recorded=true, got %v", out)
	}
}

func TestSeedCompileReflection_WritesOnceAndOverwrites(t *testing.T) {
	deps := newFeedbackDeps(t)

	seedCompileReflection(deps, "nico", "goal:automate_reports", "automate reports", []string{"gather", "summarize"})
	seedCompileReflection(deps, "nico", "goal:automate_reports", "automate reports", []string{"gather", "summarize"})

	all, err := deps.refStore.LoadAll()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	records := evolution.FilterByTreeNameStrict(all, "goal:automate_reports")
	if len(records) != 1 {
		t.Fatalf("recompiling must overwrite the seed, got %d records", len(records))
	}
	rec := records[0]
	if rec.Outcome != evolution.Success || rec.User != "nico" {
		t.Errorf("seed record wrong: %+v", rec)
	}
	if !strings.Contains(rec.Plan, "gather") {
		t.Errorf("seed record must carry the plan steps, got %q", rec.Plan)
	}
}

func TestPersistGeneratedTreeForUser_UserWorkspaceAndFallback(t *testing.T) {
	deps := newFeedbackDeps(t)
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "goal:automate_reports",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}

	// User-attributed: the tree lands in the user's workspace trees dir.
	result := map[string]interface{}{}
	persistGeneratedTreeForUser(deps, "nico", "goal:automate_reports", tree, result)
	if result["persisted"] != true {
		t.Fatalf("persist failed: %v", result)
	}
	wantDir := deps.personaStore.Workspace("nico").TreesDir()
	file, _ := result["file"].(string)
	if filepath.Dir(file) != wantDir {
		t.Errorf("file = %q, want it inside user trees dir %q", file, wantDir)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("persisted file missing: %v", err)
	}
	if result["owner"] != "nico" {
		t.Errorf("result must carry the owner: %v", result)
	}

	// No user: falls back to the shared tree store.
	shared := map[string]interface{}{}
	persistGeneratedTreeForUser(deps, "", "goal:automate_reports", tree, shared)
	if shared["persisted"] != true {
		t.Fatalf("shared persist failed: %v", shared)
	}
	sharedFile, _ := shared["file"].(string)
	if filepath.Dir(sharedFile) == wantDir {
		t.Error("anonymous persist must not land in a user workspace")
	}
}

// Regression: recordUserFeedback validates user/treeID with TrimSpace but
// stored and filtered with the raw values, so a trailing-space tree id
// created records that FilterByTreeNameStrict could never match again.
func TestRecordUserFeedback_TrimsUserAndTreeID(t *testing.T) {
	deps := newFeedbackDeps(t)

	result := recordUserFeedback(deps, " nico ", "goal:demo ", "positive", "")
	if result["recorded"] != true {
		t.Fatalf("feedback not recorded: %v", result)
	}

	all, err := deps.refStore.LoadAll()
	if err != nil {
		t.Fatalf("load records: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}
	if all[0].TreeName != "goal:demo" {
		t.Errorf("TreeName = %q, want trimmed %q", all[0].TreeName, "goal:demo")
	}
	if all[0].User != "nico" {
		t.Errorf("User = %q, want trimmed %q", all[0].User, "nico")
	}

	// Follow-up feedback on the canonical id must tally both signals; the
	// untrimmed bug left the first record invisible to the strict filter.
	result = recordUserFeedback(deps, "nico", "goal:demo", "negative", "")
	if result["recorded"] != true {
		t.Fatalf("follow-up feedback not recorded: %v", result)
	}
	if result["positives"] != 1 {
		t.Errorf("positives = %v, want 1 (trailing-space record must count)", result["positives"])
	}
	if result["negatives"] != 1 {
		t.Errorf("negatives = %v, want 1", result["negatives"])
	}
}
