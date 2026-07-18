package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
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

// Q4 Personalization & Self-Growth milestone 4: the flagged_for_review signal
// (crossed at feedbackReviewThreshold negatives) must escalate to a real HITL
// request and pause the tree's automation — not just log a warning. Pausing
// means flipping the persona.AutomationRecord.Status away from "approved" to
// a new "flagged" state, which Milestone 1's automationApproved guard
// (internal/agentexec/wiring.go) already treats as non-executable since it
// only allows Status == persona.AutomationApproved through.
func TestRecordUserFeedback_FlaggedForReviewEscalatesAndPausesAutomation(t *testing.T) {
	deps := newFeedbackDeps(t)

	prevStore := hitl.DefaultStore
	prevPolicy := hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "review"})
	t.Cleanup(func() {
		hitl.DefaultStore = prevStore
		hitl.SetPolicy(prevPolicy)
	})

	const user = "nico"
	const treeID = "goal:automate_reports"
	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(user))
	if err != nil {
		t.Fatalf("automation store: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: "weekly_sales_report",
		Status:    persona.AutomationApproved,
		TreeID:    treeID,
		AgentName: "auto-nico-weekly_sales_report",
	}); err != nil {
		t.Fatalf("seed automation record: %v", err)
	}

	recordUserFeedback(deps, user, treeID, "negative", "wrong currency")
	result := recordUserFeedback(deps, user, treeID, "negative", "still wrong")
	if result["flagged_for_review"] != true {
		t.Fatalf("expected flagged_for_review, got %v", result)
	}

	hitlID, _ := result["hitl_id"].(string)
	if hitlID == "" {
		t.Fatalf("flagging must raise a HITL escalation and surface its id in the result: %v", result)
	}
	req, ok := hitl.DefaultStore.Get(hitlID)
	if !ok {
		t.Fatalf("HITL request %q was not created", hitlID)
	}
	if req.Context["tree_id"] != treeID || req.Context["user"] != user {
		t.Errorf("HITL request context missing tree/user: %+v", req.Context)
	}

	rec, exists, err := ledger.Get("weekly_sales_report")
	if err != nil || !exists {
		t.Fatalf("automation record missing after flagging: exists=%v err=%v", exists, err)
	}
	if rec.Status != "flagged" {
		t.Errorf("automation status = %q, want %q so Milestone 1's guard treats it as non-executable and pauses the automation", rec.Status, "flagged")
	}
}

// Q4 Personalization & Self-Growth milestone 1: once a tree is flagged and its
// automation record sits in automationFlaggedStatus pending human review,
// every subsequent bt_feedback call must NOT raise another HITL escalation —
// the ledger record is already flagged, so re-escalating on each call would
// spam duplicate review requests for the same pending decision.
func TestRecordUserFeedback_FlaggedForReviewDoesNotReEscalateWhilePending(t *testing.T) {
	deps := newFeedbackDeps(t)

	prevStore := hitl.DefaultStore
	prevPolicy := hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "review"})
	t.Cleanup(func() {
		hitl.DefaultStore = prevStore
		hitl.SetPolicy(prevPolicy)
	})

	const user = "nico"
	const treeID = "goal:automate_reports"
	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(user))
	if err != nil {
		t.Fatalf("automation store: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: "weekly_sales_report",
		Status:    persona.AutomationApproved,
		TreeID:    treeID,
		AgentName: "auto-nico-weekly_sales_report",
	}); err != nil {
		t.Fatalf("seed automation record: %v", err)
	}

	// Two negatives trip the threshold and raise the first (only) escalation.
	recordUserFeedback(deps, user, treeID, "negative", "wrong currency")
	tripped := recordUserFeedback(deps, user, treeID, "negative", "still wrong")
	if tripped["flagged_for_review"] != true {
		t.Fatalf("expected flagged_for_review on threshold trip, got %v", tripped)
	}
	firstHitlID, _ := tripped["hitl_id"].(string)
	if firstHitlID == "" {
		t.Fatalf("threshold trip must raise a HITL escalation: %v", tripped)
	}

	// Two more negative-feedback calls while the ledger record is still
	// automationFlaggedStatus must not create additional HITL requests.
	for i := 0; i < 2; i++ {
		result := recordUserFeedback(deps, user, treeID, "negative", "yet again")
		if result["flagged_for_review"] != true {
			t.Errorf("call %d: still over threshold, expected flagged_for_review=true, got %v", i, result)
		}
		if hitlID, _ := result["hitl_id"].(string); hitlID != "" && hitlID != firstHitlID {
			t.Errorf("call %d: re-escalated with a new HITL request %q while %q is still pending review", i, hitlID, firstHitlID)
		}
	}

	pending := hitl.DefaultStore.ListPending()
	if len(pending) != 1 {
		t.Fatalf("expected exactly 1 HITL request after threshold trip + 2 more negatives, got %d", len(pending))
	}

	rec, exists, err := ledger.Get("weekly_sales_report")
	if err != nil || !exists {
		t.Fatalf("automation record missing: exists=%v err=%v", exists, err)
	}
	if rec.Status != automationFlaggedStatus {
		t.Errorf("automation status = %q, want %q", rec.Status, automationFlaggedStatus)
	}
}

// Feedback on a tree with no tracked automation (e.g. a manually compiled
// tree, or one never proposed through the autopilot) must still record the
// flag but has no automation to pause — no HITL escalation should be raised.
func TestRecordUserFeedback_FlaggedForReviewNoAutomationTracked(t *testing.T) {
	deps := newFeedbackDeps(t)

	prevStore := hitl.DefaultStore
	prevPolicy := hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "review"})
	t.Cleanup(func() {
		hitl.DefaultStore = prevStore
		hitl.SetPolicy(prevPolicy)
	})

	recordUserFeedback(deps, "nico", "goal:untracked", "negative", "")
	result := recordUserFeedback(deps, "nico", "goal:untracked", "negative", "")
	if result["flagged_for_review"] != true {
		t.Fatalf("expected flagged_for_review, got %v", result)
	}
	if hitlID, _ := result["hitl_id"].(string); hitlID != "" {
		t.Errorf("no automation is tracked for this tree; must not raise a HITL escalation, got hitl_id=%q", hitlID)
	}
	if len(hitl.DefaultStore.ListPending()) != 0 {
		t.Errorf("no automation is tracked for this tree; HITL store must stay empty")
	}
}

// Q4 Personalization & Self-Growth milestone 2/2: the feedback-escalation loop
// must actually resume, not just pause forever. This is the true end-to-end
// path: escalateFlaggedTreeForReview (via recordUserFeedback) pauses the
// automation and the engine's execution gate (domains.ResolveTreeIDForUser,
// wired here as resolveTreeForUser) must refuse to resolve the tree while
// flagged; once a human approves the HITL escalation, finalizeFeedbackEscalation
// must flip the persona.AutomationRecord back to persona.AutomationApproved so
// the SAME execution gate resolves the tree again — proving the automation
// genuinely recovers instead of staying dead forever after one bad streak.
func TestFinalizeFeedbackEscalation_ResumesAutomationAfterHumanApproval(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	refStore, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("ref store: %v", err)
	}
	// Rooted at agent.UsersDir() (not an arbitrary t.TempDir()) so the
	// production resolution path — resolveTreeForUser, which the daemon wires
	// through domains.DynamicResolveForUserFn to
	// agentexec.ResolveGeneratedTreeForUser — reads the very same workspace
	// this test writes into.
	personaStore, err := persona.NewStore(agent.UsersDir())
	if err != nil {
		t.Fatalf("persona store: %v", err)
	}
	treeStore, err := evolution.NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatalf("tree store: %v", err)
	}
	deps := &mcpDeps{refStore: refStore, personaStore: personaStore, treeStore: treeStore}

	prevStore := hitl.DefaultStore
	prevPolicy := hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "review"})
	t.Cleanup(func() {
		hitl.DefaultStore = prevStore
		hitl.SetPolicy(prevPolicy)
	})

	const user = "nico"
	const treeID = "goal:automate_reports"

	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(user))
	if err != nil {
		t.Fatalf("automation store: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: "weekly_sales_report",
		Status:    persona.AutomationApproved,
		TreeID:    treeID,
		AgentName: "auto-nico-weekly_sales_report",
	}); err != nil {
		t.Fatalf("seed automation record: %v", err)
	}

	tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "WeeklySalesReport"}
	if _, err := evolution.SaveNamedTree(deps.personaStore.Workspace(user).TreesDir(), treeID, tree); err != nil {
		t.Fatalf("SaveNamedTree: %v", err)
	}

	// Sanity: before any feedback, the approved automation's tree resolves.
	if got := resolveTreeForUser(user, treeID); got == nil {
		t.Fatal("expected tree to resolve before any negative feedback (automation starts approved)")
	}

	recordUserFeedback(deps, user, treeID, "negative", "wrong currency")
	tripped := recordUserFeedback(deps, user, treeID, "negative", "still wrong")
	if tripped["flagged_for_review"] != true {
		t.Fatalf("expected flagged_for_review on threshold trip, got %v", tripped)
	}
	hitlID, _ := tripped["hitl_id"].(string)
	if hitlID == "" {
		t.Fatalf("threshold trip must raise a HITL escalation: %v", tripped)
	}

	// automationApproved must now gate execution off: the engine's execution
	// gate refuses to resolve a flagged tree.
	if got := resolveTreeForUser(user, treeID); got != nil {
		t.Fatalf("expected flagged automation's tree to be gated from execution, got %+v", got)
	}

	req, err := hitl.DefaultStore.Approve(hitlID, "human", "reviewed — looks fine now")
	if err != nil {
		t.Fatalf("approve escalation: %v", err)
	}

	// finalizeFeedbackEscalation is the resume half of the loop: a human
	// approving the escalation must resume the paused automation.
	finalizeFeedbackEscalation(deps, req, true)

	rec, exists, err := ledger.Get("weekly_sales_report")
	if err != nil || !exists {
		t.Fatalf("automation record missing after resume: exists=%v err=%v", exists, err)
	}
	if rec.Status != persona.AutomationApproved {
		t.Errorf("automation status = %q, want %q after finalizeFeedbackEscalation resumes it", rec.Status, persona.AutomationApproved)
	}

	// automationApproved must return true again: the engine's execution gate
	// resolves the tree once more, closing the loop end-to-end.
	if got := resolveTreeForUser(user, treeID); got == nil {
		t.Fatal("expected tree to resolve again after finalizeFeedbackEscalation resumed the automation")
	}
}
