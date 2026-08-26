package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/persona"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

// TestDashboardDriftWatcherRebuildsItself pins — at the source level, the
// same audit style as cmd/bt-agent/main_test.go's requireBuildIdentityWiring
// — that bt-dashboard's deploy-drift watcher can actually rebuild its own
// binary, not just detect that it has drifted from repo HEAD.
//
// As of Q3 Reliability milestone 2, agent.DefaultRebuildTargets includes
// bt-dashboard too (the daemon's fleet-wide sweep now covers it), and
// agent.DashboardRebuildTargets is a direct alias of it — so this no longer
// guards against a target list that omits bt-dashboard, but it still pins
// that main.go's own watcher wiring names the dashboard-specific alias
// explicitly rather than hardcoding agent.DefaultRebuildTargets, keeping the
// call site self-documenting if the two ever diverge again.
func TestDashboardDriftWatcherRebuildsItself(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "agent.DashboardRebuildTargets(repoDir)") {
		t.Errorf("main.go's deploy-drift watcher must pass agent.DashboardRebuildTargets(repoDir) as " +
			"Targets so bt-dashboard rebuilds its own binary on drift")
	}
}

// TestDashboardDriftWatcherWiresRebuildBackoff pins — the same audit style as
// TestDashboardDriftWatcherRebuildsItself above — that bt-dashboard's
// deploy-drift watcher sets a RebuildBackoff guard so a broken HEAD cannot
// retry-storm `go build` every watcher tick (ADR-045 milestone 4, currently
// unwired per arc42 §Deploy Drift, 2026-07-12).
func TestDashboardDriftWatcherWiresRebuildBackoff(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "Backoff:") {
		t.Error("main.go's deploy-drift watcher must wire a RebuildBackoff (Backoff:); not found")
	}
}

// TestDashboardDriftWatcherWiresAutoRestart pins — the same audit style as
// TestDashboardDriftWatcherWiresRebuildBackoff above — that bt-dashboard's
// own deploy-drift watcher sets AutoRestart, mirroring cmd/bt-agent/main.go's
// wiring. Without it, even a correctly-pathed self-rebuild (see
// internal/agent/rebuild.go's DashboardRebuildTargets OutPath fix) only ever
// logs "rebuilt binaries — restart to adopt" and never actually restarts the
// unit to run the new binary, leaving bt-dashboard's own detection layer
// unable to self-heal without a human running `systemctl restart` by hand.
func TestDashboardDriftWatcherWiresAutoRestart(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "AutoRestart:") {
		t.Error("main.go's deploy-drift watcher must wire AutoRestart (AutoRestart: agent.AutoRestartEnabled()), " +
			"mirroring cmd/bt-agent/main.go, so a successful self-rebuild actually restarts the unit instead of " +
			"silently doing nothing")
	}
}

// TestMainWiresKnowledgeGraphDiscoverIntoDashboard pins that main.go wires
// the already-built knowledge graph's Discover method into
// dashboard.DiscoverTreeFn, so dashboard.PickTreeForTask consults
// knowledge.KnowledgeGraph.Discover instead of relying solely on its static
// 7-branch keyword switch.
func TestMainWiresKnowledgeGraphDiscoverIntoDashboard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "dashboard.DiscoverTreeFn = kg.Discover") {
		t.Error("main.go must wire dashboard.DiscoverTreeFn = kg.Discover so PickTreeForTask consults the knowledge graph")
	}
}

// TestMainWiresKGAnalyticsRefreshFn pins the NotebookLM research goal: make
// cmd/bt-dashboard itself periodically call knowledge.ComputeAnalytics() +
// dashboard.RecordKGAnalytics() (e.g. on a background ticker or on each
// /api/metrics scrape) instead of depending on the separate bt-agent
// process's bt_kg_analytics MCP tool handler to populate the gauges.
// bt-agent and bt-dashboard are separate binaries with separate memory, so
// cmd/bt-agent/tools.go's existing dashboard.RecordKGAnalytics call (inside
// the bt_kg_analytics MCP tool handler) only ever updates bt-agent's own
// in-process gauges — never bt-dashboard's, which is the process that
// actually serves /api/metrics to Prometheus.
//
// This audits main.go at the source level (same style as
// TestMainWiresKnowledgeGraphDiscoverIntoDashboard above) because the
// wiring happens inline during dashboard startup setup, not inside a
// separately callable function: main.go must set
// dashboard.KGAnalyticsRefreshFn to a closure that calls kg.ComputeAnalytics()
// against the dashboard's own in-process knowledge graph and republishes the
// result via dashboard.RecordKGAnalytics, so every /api/metrics scrape
// (internal/dashboard.PrometheusHandler, which invokes KGAnalyticsRefreshFn
// per TestPrometheusHandler_InvokesKGAnalyticsRefreshFn) reflects live graph
// health computed in-process.
func TestMainWiresKGAnalyticsRefreshFn(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	if !strings.Contains(source, "dashboard.KGAnalyticsRefreshFn") {
		t.Error("main.go must wire dashboard.KGAnalyticsRefreshFn so /api/metrics scrapes " +
			"refresh KG analytics gauges from the dashboard's own in-process knowledge graph " +
			"instead of depending on the separate bt-agent process's bt_kg_analytics MCP tool handler")
	}
	if !strings.Contains(source, "kg.ComputeAnalytics()") {
		t.Error("main.go's KGAnalyticsRefreshFn wiring must call kg.ComputeAnalytics() against " +
			"the dashboard's own in-process knowledge graph, not rely on bt-agent's separate process")
	}
	if !strings.Contains(source, "dashboard.RecordKGAnalytics(") {
		t.Error("main.go must call dashboard.RecordKGAnalytics(...) itself with the freshly " +
			"computed analytics counts, not only read them from a remote process")
	}
}

// TestMainWiresAuctionCardsFn pins the NotebookLM research goal: cmd/bt-dashboard
// must install a2a.AuctionCardsFn from its own live agent card registry,
// mirroring cmd/bt-agent/main.go:868's
// `a2a_mod.AuctionCardsFn = a2aSrv.AuctionCardSource()`.
//
// internal/a2a/auction.go's runAuction reads the package-level AuctionCardsFn
// seam to find candidate bidders; it is nil until something assigns it.
// cmd/bt-agent/main.go is the only production call site in the repo — verified
// via repo-wide grep, it assigns it right after constructing its A2A server.
// cmd/bt-dashboard/main.go never imports internal/a2a and never sets this seam,
// even though internal/dashboard/executor.go's PickTreeForTask routes any
// auction-keyword task to auction_demo, which runs in-process via
// agentexec.NewRunDeps() — the same engine that wires the AuctionDelegate BT
// action and consults this same global. So every auction-shaped task
// submitted through the dashboard UI deterministically finds zero bidders and
// fails with "auction produced no bidders and no delegate_tree_id fallback is
// configured", while the identical tree run via cmd/bt-agent completes real
// auctions. This audits main.go at the source level (same style as
// TestMainWiresKGAnalyticsRefreshFn above) because the wiring happens inline
// during dashboard startup setup, not inside a separately callable function.
func TestMainWiresAuctionCardsFn(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	if !strings.Contains(source, "github.com/nico/go-bt-evolve/internal/a2a") {
		t.Error("main.go must import internal/a2a so it can wire the production auction " +
			"card source, mirroring cmd/bt-agent/main.go")
	}
	if !strings.Contains(source, "AuctionCardsFn = ") {
		t.Error("main.go must set a2a.AuctionCardsFn from the dashboard's own live agent " +
			"registry (mirroring cmd/bt-agent/main.go:868's " +
			"a2a_mod.AuctionCardsFn = a2aSrv.AuctionCardSource()), so auction-shaped tasks " +
			"routed to auction_demo via the dashboard's in-process executor find real " +
			"bidders instead of deterministically zero")
	}
}

// TestBuildDashboardKnowledgeGraph_LoadsFeedbackFitness pins milestone 4/4 of
// the Q2 Evolvability KG-adoption program: buildDashboardKnowledgeGraph must
// register the static catalog (via knowledge.BuildKnowledgeGraph) and then
// load accumulated runtime feedback from the given path (via LoadFeedback),
// so dashboard.DiscoverTreeFn and analytics views reflect real fitness/run
// history instead of always showing a zero-feedback seed catalog.
func TestBuildDashboardKnowledgeGraph_LoadsFeedbackFitness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.json")

	seed := knowledge.BuildKnowledgeGraph()
	seedTree, ok := seed.Trees["default"]
	if !ok {
		t.Fatal("seed graph missing expected \"default\" tree")
	}
	seedTree.Fitness = 0.87
	seedTree.RunCount = 12
	if err := seed.SaveFeedback(path); err != nil {
		t.Fatalf("SaveFeedback: %v", err)
	}

	kg := buildDashboardKnowledgeGraph(path)

	tree, ok := kg.Trees["default"]
	if !ok {
		t.Fatal("expected \"default\" tree to still be registered after loading feedback")
	}
	if tree.Fitness != 0.87 {
		t.Errorf("Fitness = %v, want 0.87 (feedback file was not loaded)", tree.Fitness)
	}
	if tree.RunCount != 12 {
		t.Errorf("RunCount = %v, want 12 (feedback file was not loaded)", tree.RunCount)
	}
}

// TestBuildDashboardKnowledgeGraph_SetsExpectedDomainsAndSurfacesGaps pins the
// self-fix (review 2026-07-22) for ADR-182: cmd/bt-dashboard wires
// dashboard.KGAnalyticsRefreshFn and publishes the bt_kg_coverage_gaps gauge
// — the very metric Prometheus scrapes — but buildDashboardKnowledgeGraph
// never sets kg.ExpectedDomains, only cmd/bt-agent/main.go does. Without it,
// knowledge.CoverageGaps falls back to the 8-entry defaultExpectedDomains
// (internal/knowledge/graph.go), every one of which is always present in the
// static catalog, so the gauge is structurally pinned at 0 in bt-dashboard.
//
// This pins two things: (1) buildDashboardKnowledgeGraph must populate
// ExpectedDomains from the same live domain registry cmd/bt-agent uses
// (domains.AllDomainTrees()), and (2) once wired, a dashboard-built graph
// missing one expected domain must surface a non-zero bt_kg_coverage_gaps
// count in the actual /metrics exposition, not just an internal slice.
func TestBuildDashboardKnowledgeGraph_SetsExpectedDomainsAndSurfacesGaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feedback.json")

	kg := buildDashboardKnowledgeGraph(path)

	registry := domains.AllDomainTrees()
	wantExpected := make([]string, 0, len(registry))
	for name := range registry {
		wantExpected = append(wantExpected, "domain:"+name)
	}
	sort.Strings(wantExpected)

	gotExpected := append([]string(nil), kg.ExpectedDomains...)
	sort.Strings(gotExpected)

	if len(gotExpected) != len(wantExpected) {
		t.Fatalf("buildDashboardKnowledgeGraph: kg.ExpectedDomains has %d entries, want %d "+
			"(the same live domain registry cmd/bt-agent uses via domains.AllDomainTrees()); got=%v",
			len(gotExpected), len(wantExpected), gotExpected)
	}
	for i := range wantExpected {
		if gotExpected[i] != wantExpected[i] {
			t.Fatalf("buildDashboardKnowledgeGraph: kg.ExpectedDomains[%d] = %q, want %q "+
				"(mismatch against domains.AllDomainTrees())", i, gotExpected[i], wantExpected[i])
		}
	}

	// Simulate a genuinely missing expected domain: delete a tree that both
	// the static catalog and the live registry agree should exist.
	const missing = "domain:code_review"
	if _, ok := kg.Trees[missing]; !ok {
		t.Fatalf("test setup: %q must be registered in the static catalog before deletion", missing)
	}
	delete(kg.Trees, missing)

	origFn := dashboard.KGAnalyticsRefreshFn
	t.Cleanup(func() { dashboard.KGAnalyticsRefreshFn = origFn })
	dashboard.KGAnalyticsRefreshFn = func() {
		a := kg.ComputeAnalytics()
		dashboard.RecordKGAnalytics(len(a.CoverageGaps), len(a.Bottlenecks), len(a.SelectionPressure))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	dashboard.PrometheusHandler().ServeHTTP(rec, req)
	body := rec.Body.String()

	if strings.Contains(body, "bt_kg_coverage_gaps 0\n") {
		t.Errorf("bt_kg_coverage_gaps is still 0 after deleting expected domain %q from the "+
			"dashboard-wired graph; ADR-182's headline gauge must go non-zero when this process's "+
			"own graph is missing an expected domain. /metrics body:\n%s", missing, body)
	}
	if !strings.Contains(body, "bt_kg_coverage_gaps 1\n") {
		t.Errorf("expected exactly \"bt_kg_coverage_gaps 1\" after deleting one expected domain, got body:\n%s", body)
	}
}

// TestHandleHealth_UsesDashboardHealthJSON pins the NotebookLM research goal:
// handleHealth must delegate to the existing dashboard.HealthJSON(version)
// instead of hand-rolling its own response map with a literal "operational"
// uptime string and static "packages"/"trees" counts that immediately go
// stale as the platform grows.
func TestHandleHealth_UsesDashboardHealthJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var got dashboard.HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	if got.GoVersion == "" {
		t.Errorf("go_version missing from response; handler still hand-rolls its own map instead of calling dashboard.HealthJSON. body=%s", rr.Body.String())
	}
	if got.Uptime == "operational" {
		t.Errorf(`uptime = %q, want a real elapsed-time string from dashboard.HealthJSON (time.Since(...).String()), not the hardcoded literal "operational"`, got.Uptime)
	}
	if _, err := time.ParseDuration(got.Uptime); err != nil {
		t.Errorf("uptime %q does not parse as a Go duration; dashboard.HealthJSON formats it via time.Since(startTime).String(): %v", got.Uptime, err)
	}

	// A handler that truly delegates to dashboard.HealthJSON produces exactly
	// the HealthResponse fields — no leftover hand-rolled "packages"/"trees" keys.
	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	for _, stale := range []string{"packages", "trees"} {
		if _, present := raw[stale]; present {
			t.Errorf("response still contains hand-rolled %q field; want handler to call dashboard.HealthJSON instead", stale)
		}
	}
}

// TestHandleScalability_ReflectsInjectedQueueAndRouter pins milestone 3/5 of the
// horizontal-scaling adoption program: the /api/scalability endpoint must surface
// the injected TaskQueue depth and AgentRouter executor health instead of the
// hardcoded 0/nil placeholders that leave Queue and Router omitted from the JSON.
func TestHandleScalability_ReflectsInjectedQueueAndRouter(t *testing.T) {
	// Preserve and restore the package globals this test mutates.
	origQueue := dashTaskQueue
	origRouter := dashAgentRouter
	t.Cleanup(func() {
		dashTaskQueue = origQueue
		dashAgentRouter = origRouter
	})

	// Inject a task queue carrying two pending items.
	q := reliability.NewTaskQueue(filepath.Join(t.TempDir(), "queue.json"))
	q.Enqueue("task-a")
	q.Enqueue("task-b")
	dashTaskQueue = q

	// Inject an agent router holding a single healthy local executor.
	local := reliability.NewLocalExecutor("local-test", func(_ context.Context, agent, task string) (*reliability.AgentResult, error) {
		return &reliability.AgentResult{Agent: agent, Success: true}, nil
	})
	dashAgentRouter = reliability.NewAgentRouter(local)

	req := httptest.NewRequest(http.MethodGet, "/api/scalability", nil)
	rr := httptest.NewRecorder()
	handleScalability(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var status reliability.ScalabilityStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	if status.Queue == nil {
		t.Fatalf("Queue is nil; endpoint still ignores the injected TaskQueue. body=%s", rr.Body.String())
	}
	if status.Queue.Pending != 2 {
		t.Errorf("Queue.Pending = %d, want 2 (injected queue depth)", status.Queue.Pending)
	}

	if status.Router == nil {
		t.Fatalf("Router is nil; endpoint still ignores the injected AgentRouter. body=%s", rr.Body.String())
	}
	if status.Router.Total != 1 {
		t.Errorf("Router.Total = %d, want 1 (injected executor count)", status.Router.Total)
	}
	if status.Router.Healthy != 1 {
		t.Errorf("Router.Healthy = %d, want 1 (single healthy local executor)", status.Router.Healthy)
	}
}

// TestHandleDLQReplay_RequeuesInsteadOfDropping pins milestone 4/5 of the
// drop-safe-DLQ program. The dashboard runs in a separate process from bt-agent
// and has no tree runner of its own, so it cannot actually execute a replayed
// task. The old handler called dlq.Replay(id), which REMOVES the entry from the
// queue and persists the removal — a cross-process silent drop: bt-agent's
// executor never sees the entry again and the task is lost.
//
// The fixed handler must instead reload the DLQ from disk and mark the entry for
// retry (RequeuedAt) without removing it, so the entry survives on disk and
// bt-agent's executor picks it up on its next scan.
func TestHandleDLQReplay_RequeuesInsteadOfDropping(t *testing.T) {
	origDLQ := dlq
	t.Cleanup(func() { dlq = origDLQ })

	dlqPath := filepath.Join(t.TempDir(), "dead_letter_queue.json")
	dlq = reliability.NewDeadLetterQueue(dlqPath)
	dlq.Push(reliability.DeadLetterEntry{
		ID:    "dead-1",
		Task:  "rebuild-index",
		Agent: "indexer",
		Error: "boom",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/dlq/replay?id=dead-1", nil)
	rr := httptest.NewRecorder()
	handleDLQReplay(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Reload from disk to assert the cross-process contract: the entry must
	// survive the replay rather than being silently dropped.
	entries := reliability.NewDeadLetterQueue(dlqPath).List()
	if len(entries) != 1 {
		t.Fatalf("entries on disk after replay = %d, want 1 (dashboard silently dropped the entry)", len(entries))
	}
	if entries[0].ID != "dead-1" {
		t.Fatalf("surviving entry ID = %q, want dead-1", entries[0].ID)
	}

	// The surviving entry must be flagged for retry so bt-agent's executor
	// requeues it. A zero RequeuedAt means it was left untouched (never picked up).
	if entries[0].RequeuedAt.IsZero() {
		t.Errorf("RequeuedAt is zero; entry was not flagged for retry")
	}
}

// TestHandleDLQ_IncludesCategoryCounts pins that the /api/dlq response
// surfaces a per-error-category rollup (reliability.DeadLetterQueue's
// existing CategoryCounts method) alongside the flat entry list, so the
// dashboard panel can render a breakdown without re-deriving it client-side
// from raw entries. Today handleDLQ's response map only has "count" and
// "entries" — "categories" is silently omitted.
func TestHandleDLQ_IncludesCategoryCounts(t *testing.T) {
	origDLQ := dlq
	t.Cleanup(func() { dlq = origDLQ })

	dlqPath := filepath.Join(t.TempDir(), "dead_letter_queue.json")
	dlq = reliability.NewDeadLetterQueue(dlqPath)
	dlq.Push(reliability.DeadLetterEntry{ID: "e1", Error: "connection refused"})
	dlq.Push(reliability.DeadLetterEntry{ID: "e2", Error: "timeout"})
	dlq.Push(reliability.DeadLetterEntry{ID: "e3", Error: "connection refused"})

	req := httptest.NewRequest(http.MethodGet, "/api/dlq", nil)
	rr := httptest.NewRecorder()
	handleDLQ(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshaling response: %v; body=%s", err, rr.Body.String())
	}

	rawCats, ok := resp["categories"]
	if !ok {
		t.Fatalf("response has no \"categories\" field; body=%s", rr.Body.String())
	}
	cats, ok := rawCats.(map[string]any)
	if !ok {
		t.Fatalf("categories = %v (%T), want a map", rawCats, rawCats)
	}
	if got, want := cats["network"], float64(2); got != want {
		t.Errorf("categories[\"network\"] = %v, want %v (dlq.CategoryCounts() rollup)", got, want)
	}
	if got, want := cats["timeout"], float64(1); got != want {
		t.Errorf("categories[\"timeout\"] = %v, want %v (dlq.CategoryCounts() rollup)", got, want)
	}
}

// TestHandleTaskApproveReject_EscalatedVsPending pins milestone 4/4 of the
// stop-HITL-escalation-from-silently-auto-approving program. handleTaskApprove
// and handleTaskReject call hitl.DefaultStore.ApproveByTaskID/RejectByTaskID,
// which already resolve both StatusPending and StatusEscalated requests (see
// internal/hitl/store_extensions.go), but the HTTP response never tells the
// caller which case it was: a request that had been escalated to a human
// operator resolves silently the same way a routine pending approval does,
// and a task with no matching HITL request at all gets no signal either — the
// hitl.DefaultStore error is swallowed outright. The response must surface
// "hitl_resolved_from" (escalated vs pending) on success and "hitl_note" (the
// underlying "no pending request for task" message) when no request is found.
func TestHandleTaskApproveReject_EscalatedVsPending(t *testing.T) {
	origTaskStore := taskStore
	origHitl := hitl.DefaultStore
	t.Cleanup(func() {
		taskStore = origTaskStore
		hitl.DefaultStore = origHitl
	})

	tests := []struct {
		name             string
		action           string // "approve" or "reject"
		startEscalated   bool
		noHitlRequest    bool
		wantTaskStatus   string
		wantResolvedFrom string
		wantNoteContains string
	}{
		{name: "approve pending request", action: "approve", startEscalated: false, wantTaskStatus: "approved", wantResolvedFrom: "pending"},
		{name: "approve escalated request", action: "approve", startEscalated: true, wantTaskStatus: "approved", wantResolvedFrom: "escalated"},
		{name: "reject pending request", action: "reject", startEscalated: false, wantTaskStatus: "rejected", wantResolvedFrom: "pending"},
		{name: "reject escalated request", action: "reject", startEscalated: true, wantTaskStatus: "rejected", wantResolvedFrom: "escalated"},
		{name: "approve task with no hitl request", action: "approve", noHitlRequest: true, wantTaskStatus: "approved", wantNoteContains: "no pending request for task"},
		{name: "reject task with no hitl request", action: "reject", noHitlRequest: true, wantTaskStatus: "rejected", wantNoteContains: "no pending request for task"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
			store, err := hitl.InitStore(filepath.Join(dir, "hitl"))
			if err != nil {
				t.Fatalf("InitStore: %v", err)
			}

			taskID := "task-" + strings.ReplaceAll(tc.name, " ", "-")
			if err := taskStore.Create(dashboard.Task{ID: taskID, Title: "t"}); err != nil {
				t.Fatalf("taskStore.Create: %v", err)
			}

			var wantReqID string
			if !tc.noHitlRequest {
				req := hitl.NewRequest("N", "HumanApprovalGate", "body", "", "", "p", map[string]any{"task_id": taskID})
				if err := store.Create(req); err != nil {
					t.Fatalf("hitl store.Create: %v", err)
				}
				wantReqID = req.ID
				if tc.startEscalated {
					if _, err := store.Escalate(req.ID, "ops", "needs review"); err != nil {
						t.Fatalf("Escalate: %v", err)
					}
				}
			}

			var rr *httptest.ResponseRecorder
			switch tc.action {
			case "approve":
				httpReq := httptest.NewRequest(http.MethodPost, "/api/task/approve?id="+taskID, nil)
				rr = httptest.NewRecorder()
				handleTaskApprove(rr, httpReq)
			case "reject":
				httpReq := httptest.NewRequest(http.MethodPost, "/api/task/reject?id="+taskID, nil)
				rr = httptest.NewRecorder()
				handleTaskReject(rr, httpReq)
			default:
				t.Fatalf("unknown action %q", tc.action)
			}

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
			}

			var resp map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
			}

			if resp["status"] != tc.wantTaskStatus {
				t.Errorf("status = %q, want %q; full resp=%v", resp["status"], tc.wantTaskStatus, resp)
			}

			if tc.noHitlRequest {
				if !strings.Contains(resp["hitl_note"], tc.wantNoteContains) {
					t.Errorf("hitl_note = %q, want it to contain %q; full resp=%v", resp["hitl_note"], tc.wantNoteContains, resp)
				}
				if resp["hitl_request_id"] != "" {
					t.Errorf("hitl_request_id = %q, want empty since no request existed", resp["hitl_request_id"])
				}
				return
			}

			if resp["hitl_request_id"] != wantReqID {
				t.Errorf("hitl_request_id = %q, want %q", resp["hitl_request_id"], wantReqID)
			}
			if resp["hitl_resolved_from"] != tc.wantResolvedFrom {
				t.Errorf("hitl_resolved_from = %q, want %q (must distinguish an escalated request from a routine pending one); full resp=%v", resp["hitl_resolved_from"], tc.wantResolvedFrom, resp)
			}
		})
	}
}

// TestHandleAnalyze_TaskIDsKeyedOnInsightIndex pins milestone 1/3 of the
// dashboard Workflow/Approval wiring program. handleAnalyze mints each
// auto-generated task's ID as
//
//	fmt.Sprintf("tt-%d-%d", time.Now().UnixNano(), len(f.KeyInsights))
//
// via `for _, insight := range f.KeyInsights[:min(2, len(f.KeyInsights))]`.
// The second Sprintf component, len(f.KeyInsights), is CONSTANT across every
// insight minted from the same ResearchFinding — the ID varies only by
// nanosecond timestamp. Two insights minted for the same fellow within the
// same nanosecond tick therefore collide on an identical ID, and
// dashboard.TaskStore's Get/UpdateStatus/SetOutput (internal/dashboard/tasks.go)
// linear-scan and act on the first ID match only — permanently orphaning the
// second task. A real nanosecond collision cannot be reliably forced from a
// black-box test (verified: 0 collisions across 100k back-to-back
// time.Now().UnixNano() calls on this host), so this pins the fix at the
// source level like TestDashboardDriftWatcherRebuildsItself above: the loop
// must capture its own index and use it instead of the constant slice
// length.
func TestHandleAnalyze_TaskIDsKeyedOnInsightIndex(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	start := strings.Index(source, "func handleAnalyze(")
	if start < 0 {
		t.Fatal("handleAnalyze not found in main.go")
	}
	rest := source[start+len("func handleAnalyze("):]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]

	if strings.Contains(body, "for _, insight := range f.KeyInsights") {
		t.Error("handleAnalyze's insight loop discards its index (`for _, insight " +
			":= range f.KeyInsights`); it must capture the index so it can be folded " +
			"into the task ID, e.g. `for idx, insight := range f.KeyInsights[...]`")
	}

	if strings.Contains(body, `fmt.Sprintf("tt-%d-%d", time.Now().UnixNano(), len(f.KeyInsights))`) {
		t.Error("handleAnalyze still mints task IDs from time.Now().UnixNano() + " +
			"len(f.KeyInsights), which is constant across every insight minted from " +
			"the same finding; key the ID on the insight's own loop index instead so " +
			"two insights minted for the same fellow in the same nanosecond tick " +
			"don't collide and permanently orphan one another in TaskStore")
	}
}

// TestHandleAnalyze_UsesWorkflowForTaskDerivation pins milestone 2/4 of the
// dashboard Workflow/Approval wiring program. handleAnalyze's hand-rolled
// loop over tt.ResearchFindings assigns every generated task straight to a
// fellow's name (e.g. "Victoria Bull") and stamps every task with
// companyState.CurrentSprint (12 in startup.NewDefaultCompany's defaults),
// with no synthesis step and no approval workflow.
//
// dashboard.NewWorkflow + RecommendationsToTasks derives tasks from the
// ThinkTank's Synthesis instead: the main recommendation becomes a
// critical, "ceo"-assigned, sprint-1 task (WorkflowTask.AssigneeRole="ceo",
// SprintTarget=1) — independent of any fellow name and of the company's
// current sprint. That requires handleAnalyze to run the synthesis phase,
// build a *dashboard.Workflow, call RecommendationsToTasks + Prioritize,
// and persist the resulting WorkflowTasks (carrying
// AssigneeRole/SprintTarget/Approval) into taskStore instead of building
// dashboard.Task values directly from tt.ResearchFindings.
func TestHandleAnalyze_UsesWorkflowForTaskDerivation(t *testing.T) {
	origTaskStore := taskStore
	origLLM := sharedLLM
	t.Cleanup(func() {
		taskStore = origTaskStore
		sharedLLM = origLLM
	})

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
	sharedLLM = engine.NewMockLLM()

	httpReq := httptest.NewRequest(http.MethodGet, "/api/thinktank/analyze?topic=Should+we+ship+feature+X", nil)
	rr := httptest.NewRecorder()
	handleAnalyze(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	tasks := taskStore.List()

	var recTask *dashboard.Task
	for i := range tasks {
		if tasks[i].Assignee == "ceo" {
			recTask = &tasks[i]
			break
		}
	}
	if recTask == nil {
		assignees := make([]string, 0, len(tasks))
		for _, tk := range tasks {
			assignees = append(assignees, tk.Assignee)
		}
		t.Fatalf("no task assigned to workflow role %q found among %d tasks (assignees: %v); "+
			"handleAnalyze must derive tasks via dashboard.NewWorkflow + RecommendationsToTasks, "+
			"whose main-recommendation task carries AssigneeRole=%q — not a fellow name",
			"ceo", len(tasks), assignees, "ceo")
		return
	}

	if recTask.Sprint != 1 {
		t.Errorf("recommendation task Sprint = %d, want 1 (from WorkflowTask.SprintTarget); "+
			"got companyState.CurrentSprint (%d) instead, meaning tasks are still being stamped "+
			"with the company's current sprint rather than the workflow-derived sprint target",
			recTask.Sprint, companyState.CurrentSprint)
	}

	if recTask.Priority != "critical" {
		t.Errorf("recommendation task Priority = %q, want %q (from WorkflowPriority.String() "+
			"via RecommendationsToTasks)", recTask.Priority, "critical")
	}
}

// TestHandleAnalyze_SurfacesOrchestratorError pins milestone 3/4 of the Q1
// Correctness program: handleAnalyze discards every phase orchestrator error
// (`_ = orch.RunResearchRound()`, `_ = orch.RunDebate()`,
// `_ = orch.RunSynthesis()`) and always proceeds to derive tasks and respond
// as if the analysis succeeded — even when a phase genuinely failed. None of
// orch.Tank/orch.LLM can be forced nil through handleAnalyze's own guards
// (topic sourced from *thinktank.ThinkTank, which thinktank.NewThinkTank
// always populates with fellows, and c is nil-checked before the
// orchestrator is even constructed), so the only way to pin this at HEAD is
// a source-level audit — the same style already used by
// TestDashboardDriftWatcherRebuildsItself and
// TestHandleAnalyze_TaskIDsKeyedOnInsightIndex in this file. handleAnalyze
// must check each phase's returned error and, on failure, respond with an
// explicit non-empty "error" field instead of continuing on to
// dashboard.NewWorkflow/RecommendationsToTasks with empty findings.
func TestHandleAnalyze_SurfacesOrchestratorError(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	source := string(src)

	start := strings.Index(source, "func handleAnalyze(")
	if start < 0 {
		t.Fatal("handleAnalyze not found in main.go")
	}
	rest := source[start+len("func handleAnalyze("):]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	body := rest[:end]

	for _, discard := range []string{
		"_ = orch.RunResearchRound()",
		"_ = orch.RunDebate()",
		"_ = orch.RunSynthesis()",
	} {
		if strings.Contains(body, discard) {
			t.Errorf("handleAnalyze still discards the orchestrator's returned error via %q; "+
				"it must check the error and, on failure, respond with an explicit \"error\" field "+
				"instead of proceeding to derive tasks from empty findings", discard)
		}
	}

	if !strings.Contains(body, "err != nil") {
		t.Error("handleAnalyze must check the orchestrator phase calls' returned error (err != nil) " +
			"and surface a failure response instead of silently treating a failed analysis as a success")
	}
}

// TestHandleWorkflowApprovalEndpoints pins milestone 4/4 of the Q1 Correctness
// dashboard Workflow/Approval wiring program. internal/dashboard/workflow_engine.go's
// Workflow.PendingApprovals/ApproveTask/RejectTask are fully tested in
// workflow_engine_test.go but have zero callers outside that file (confirmed via
// git grep for ".ApproveTask(" / ".RejectTask(" / ".PendingApprovals("): handleAnalyze
// builds a *dashboard.Workflow locally, copies its derived WorkflowTasks into
// taskStore as plain dashboard.Task values, and lets the Workflow itself fall out of
// scope, so nothing in cmd/bt-dashboard/main.go can ever call these three methods.
//
// This pins the dashboard-level contract the fix must satisfy: handleAnalyze retains
// the *dashboard.Workflow it builds in the package-level `currentWorkflow` var (mirroring
// how taskStore/companyState are already held as package vars), and three new handlers —
// handleWorkflowPending/handleWorkflowApprove/handleWorkflowReject, registered alongside
// the existing /api/tasks/approve and /api/tasks/reject routes per main.go's mux setup —
// operate on it directly, proving the Workflow-level approval gate is reachable over HTTP
// instead of existing only in unit tests.
func TestHandleWorkflowApprovalEndpoints(t *testing.T) {
	origWorkflow := currentWorkflow
	t.Cleanup(func() { currentWorkflow = origWorkflow })

	wf := dashboard.NewWorkflow("test-wf", nil, nil)
	wf.Tasks = []dashboard.WorkflowTask{
		{ID: "task-a", Status: dashboard.StatusPending, Priority: dashboard.PriorityHigh},
		{ID: "task-b", Status: dashboard.StatusPending, Priority: dashboard.PriorityMedium},
	}
	currentWorkflow = wf

	// /api/workflow/pending must surface every WorkflowTask still awaiting a
	// decision, via Workflow.PendingApprovals — not an empty/hardcoded list.
	pendingReq := httptest.NewRequest(http.MethodGet, "/api/workflow/pending", nil)
	pendingRR := httptest.NewRecorder()
	handleWorkflowPending(pendingRR, pendingReq)
	if pendingRR.Code != http.StatusOK {
		t.Fatalf("pending status = %d, want 200; body=%s", pendingRR.Code, pendingRR.Body.String())
	}
	var pending []dashboard.WorkflowTask
	if err := json.Unmarshal(pendingRR.Body.Bytes(), &pending); err != nil {
		t.Fatalf("decode pending response: %v; body=%s", err, pendingRR.Body.String())
	}
	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2 (both seeded tasks, since neither has been decided yet)", len(pending))
	}

	// /api/workflow/approve must call Workflow.ApproveTask on the retained Workflow.
	approveReq := httptest.NewRequest(http.MethodPost, "/api/workflow/approve?id=task-a", nil)
	approveRR := httptest.NewRecorder()
	handleWorkflowApprove(approveRR, approveReq)
	if approveRR.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", approveRR.Code, approveRR.Body.String())
	}
	if !currentWorkflow.Tasks[0].Approval.IsApproved {
		t.Errorf("task-a Approval.IsApproved = false after /api/workflow/approve; handler must call Workflow.ApproveTask")
	}
	if currentWorkflow.Tasks[0].Status != dashboard.StatusApproved {
		t.Errorf("task-a Status = %s after /api/workflow/approve, want %s",
			currentWorkflow.Tasks[0].Status.String(), dashboard.StatusApproved.String())
	}

	// /api/workflow/reject must call Workflow.RejectTask on the retained Workflow.
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/workflow/reject?id=task-b&reason=not+now", nil)
	rejectRR := httptest.NewRecorder()
	handleWorkflowReject(rejectRR, rejectReq)
	if rejectRR.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body=%s", rejectRR.Code, rejectRR.Body.String())
	}
	if currentWorkflow.Tasks[1].Approval.IsApproved {
		t.Errorf("task-b Approval.IsApproved = true after /api/workflow/reject; handler must call Workflow.RejectTask")
	}
	if currentWorkflow.Tasks[1].Status != dashboard.StatusRejected {
		t.Errorf("task-b Status = %s after /api/workflow/reject, want %s",
			currentWorkflow.Tasks[1].Status.String(), dashboard.StatusRejected.String())
	}
	if currentWorkflow.Tasks[1].Approval.Reason != "not now" {
		t.Errorf("task-b Approval.Reason = %q, want %q", currentWorkflow.Tasks[1].Approval.Reason, "not now")
	}

	// Both tasks now decided — pending must reflect that instead of staying stale.
	pendingReq2 := httptest.NewRequest(http.MethodGet, "/api/workflow/pending", nil)
	pendingRR2 := httptest.NewRecorder()
	handleWorkflowPending(pendingRR2, pendingReq2)
	var pending2 []dashboard.WorkflowTask
	if err := json.Unmarshal(pendingRR2.Body.Bytes(), &pending2); err != nil {
		t.Fatalf("decode second pending response: %v; body=%s", err, pendingRR2.Body.String())
	}
	if len(pending2) != 0 {
		t.Errorf("pending count after approve+reject = %d, want 0", len(pending2))
	}
}

// TestHandleWorkflowRunFullPipeline_PersistsWorkflowAndTasks pins milestone
// 3/3 of the Q1 Correctness "wire the dashboard's dead-code RunFullPipeline
// autonomous workflow entry point" program.
//
// internal/dashboard/workflow_engine.go's Workflow.RunFullPipeline is fully
// fixed and tested (milestone 1: iterates every distinct sprint target
// instead of a hardcoded 1,2; milestone 2: checks each thinktank phase's
// returned error instead of discarding it) but has zero callers outside
// workflow_engine_test.go — confirmed via git grep for ".RunFullPipeline(" —
// so the corrected, tested pipeline can never actually run in production.
//
// handleWorkflowRunFullPipeline, registered at
// POST /api/workflow/run-full-pipeline, must build a *thinktank.Orchestrator
// (thinktank.NewOrchestrator) and a *startup.CompanyOrchestrator
// (startup.NewOrchestrator) the same way handleAnalyze/handleSprintExecute
// already do, drive them through Workflow.RunFullPipeline, retain the built
// *dashboard.Workflow in the package-level currentWorkflow (mirroring
// handleAnalyze's currentWorkflowMu locking convention), and persist every
// resulting WorkflowTask into taskStore under the wf.ID+"-"+wt.ID convention
// handleAnalyze/handleWorkflowApprove/handleWorkflowReject already share —
// giving RunFullPipeline its first production caller.
func TestHandleWorkflowRunFullPipeline_PersistsWorkflowAndTasks(t *testing.T) {
	origTaskStore := taskStore
	origLLM := sharedLLM
	origWorkflow := currentWorkflow
	t.Cleanup(func() {
		taskStore = origTaskStore
		sharedLLM = origLLM
		currentWorkflowMu.Lock()
		currentWorkflow = origWorkflow
		currentWorkflowMu.Unlock()
	})

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
	sharedLLM = engine.NewMockLLM()

	httpReq := httptest.NewRequest(http.MethodPost, "/api/workflow/run-full-pipeline?topic=Should+we+ship+feature+X", nil)
	rr := httptest.NewRecorder()
	handleWorkflowRunFullPipeline(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	currentWorkflowMu.RLock()
	wf := currentWorkflow
	currentWorkflowMu.RUnlock()
	if wf == nil {
		t.Fatal("handleWorkflowRunFullPipeline must store the *dashboard.Workflow it builds " +
			"in the package-level currentWorkflow, mirroring handleAnalyze")
	}
	if wf.Status != "completed" {
		t.Errorf("currentWorkflow.Status = %q, want %q — RunFullPipeline must have actually run to completion",
			wf.Status, "completed")
	}
	if len(wf.Tasks) == 0 {
		t.Fatal("currentWorkflow has zero Tasks; RunFullPipeline should have derived tasks " +
			"from the synthesized recommendation via RecommendationsToTasks")
	}

	tasks := taskStore.List()
	if len(tasks) != len(wf.Tasks) {
		t.Fatalf("taskStore has %d tasks, want %d (one persisted per WorkflowTask)", len(tasks), len(wf.Tasks))
	}
	byID := make(map[string]dashboard.Task, len(tasks))
	for _, tk := range tasks {
		byID[tk.ID] = tk
	}
	for _, wt := range wf.Tasks {
		wantID := wf.ID + "-" + wt.ID
		tk, ok := byID[wantID]
		if !ok {
			t.Errorf("taskStore missing task %q derived from workflow task %q (title %q); "+
				"must persist using the wf.ID+\"-\"+wt.ID convention handleAnalyze uses",
				wantID, wt.ID, wt.Title)
			continue
		}
		if tk.Title != wt.Title {
			t.Errorf("task %q Title = %q, want %q", wantID, tk.Title, wt.Title)
		}
	}
}

// TestCurrentWorkflow_GuardedByMutex pins the "guard the package-level
// currentWorkflow against concurrent HTTP access" requirement. currentWorkflow
// (main.go) is read by handleWorkflowPending/handleWorkflowApprove/
// handleWorkflowReject and reassigned by handleAnalyze on every new analysis,
// with zero synchronization today — concurrent requests can race on the bare
// pointer read/write.
//
// The fix must add a package-level mutex (currentWorkflowMu) guarding every
// read and write of currentWorkflow, mirroring the sprintState embedded-mutex
// convention already used elsewhere in this file. This test proves the guard
// actually serializes access: while an external goroutine holds
// currentWorkflowMu for writing, a concurrent handleWorkflowPending call (which
// must take a read lock before touching currentWorkflow) has to block instead
// of running straight through.
func TestCurrentWorkflow_GuardedByMutex(t *testing.T) {
	origWorkflow := currentWorkflow
	t.Cleanup(func() { currentWorkflow = origWorkflow })

	wf := dashboard.NewWorkflow("test-wf", nil, nil)
	wf.Tasks = []dashboard.WorkflowTask{{ID: "task-a", Status: dashboard.StatusPending}}
	currentWorkflow = wf

	currentWorkflowMu.Lock()
	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/workflow/pending", nil)
		rr := httptest.NewRecorder()
		handleWorkflowPending(rr, req)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("handleWorkflowPending returned while an external goroutine held " +
			"currentWorkflowMu for writing — handleWorkflowPending does not " +
			"synchronize on currentWorkflowMu, so concurrent HTTP handlers can " +
			"race on the currentWorkflow pointer")
	case <-time.After(100 * time.Millisecond):
		// Expected: handleWorkflowPending blocks waiting for currentWorkflowMu.
	}

	currentWorkflowMu.Unlock()

	select {
	case <-done:
		// Expected: handleWorkflowPending completes once the external lock is released.
	case <-time.After(time.Second):
		t.Fatal("handleWorkflowPending never completed after currentWorkflowMu was released")
	}
}

// TestHandleWorkflowApproveReject_UpdatesTaskStore pins the reconciliation
// between the two disconnected approval surfaces handleAnalyze created.
//
// handleAnalyze (main.go) derives WorkflowTasks via dashboard.NewWorkflow +
// RecommendationsToTasks, then persists each one into taskStore as a plain
// dashboard.Task whose ID is composed as wf.ID + "-" + wt.ID (see
// TestHandleAnalyze_TaskIDsUniqueAcrossAnalyses above). But
// handleWorkflowApprove/handleWorkflowReject only call
// Workflow.ApproveTask/RejectTask on the in-memory currentWorkflow — they
// never touch taskStore. Meanwhile handleSprintExecute dispatches exclusively
// from taskStore.Approved(), which filters on dashboard.Task.Status ==
// "approved". So approving a task through /api/workflow/approve updates
// currentWorkflow.Tasks[i] but leaves the corresponding taskStore record
// stuck at Status "pending" forever — handleSprintExecute can never see it,
// and the "approval" silently does nothing. This test proves the fix:
// handleWorkflowApprove/handleWorkflowReject must also update the taskStore
// record sharing the composed ID, so taskStore.Approved() reflects the
// decision made through the workflow surface.
func TestHandleWorkflowApproveReject_UpdatesTaskStore(t *testing.T) {
	origTaskStore := taskStore
	origWorkflow := currentWorkflow
	t.Cleanup(func() {
		taskStore = origTaskStore
		currentWorkflow = origWorkflow
	})

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))

	wf := dashboard.NewWorkflow("test-wf", nil, nil)
	wf.ID = "wf-recon-test"
	wf.Tasks = []dashboard.WorkflowTask{
		{ID: "task-a", Status: dashboard.StatusPending, Priority: dashboard.PriorityHigh},
		{ID: "task-b", Status: dashboard.StatusPending, Priority: dashboard.PriorityMedium},
	}
	currentWorkflow = wf

	// Seed taskStore exactly the way handleAnalyze does: composed ID
	// wf.ID + "-" + wt.ID, starting at Status "pending".
	taskAID := wf.ID + "-task-a"
	taskBID := wf.ID + "-task-b"
	if err := taskStore.Create(dashboard.Task{ID: taskAID, Title: "Task A", Priority: "high"}); err != nil {
		t.Fatalf("seed task-a: %v", err)
	}
	if err := taskStore.Create(dashboard.Task{ID: taskBID, Title: "Task B", Priority: "medium"}); err != nil {
		t.Fatalf("seed task-b: %v", err)
	}

	// Approve task-a through the workflow surface.
	approveReq := httptest.NewRequest(http.MethodPost, "/api/workflow/approve?id=task-a", nil)
	approveRR := httptest.NewRecorder()
	handleWorkflowApprove(approveRR, approveReq)
	if approveRR.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", approveRR.Code, approveRR.Body.String())
	}

	storedA, ok := taskStore.Get(taskAID)
	if !ok {
		t.Fatalf("taskStore lost record %q after /api/workflow/approve", taskAID)
	}
	if storedA.Status != "approved" {
		t.Errorf("taskStore record %q Status = %q after /api/workflow/approve, want %q; "+
			"handleWorkflowApprove must also update the taskStore record that "+
			"handleSprintExecute dispatches from, not just currentWorkflow.Tasks",
			taskAID, storedA.Status, "approved")
	}
	if !storedA.Approval.IsApproved {
		t.Errorf("taskStore record %q Approval.IsApproved = false after /api/workflow/approve, want true", taskAID)
	}

	// Reject task-b through the workflow surface.
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/workflow/reject?id=task-b&reason=not+now", nil)
	rejectRR := httptest.NewRecorder()
	handleWorkflowReject(rejectRR, rejectReq)
	if rejectRR.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body=%s", rejectRR.Code, rejectRR.Body.String())
	}

	storedB, ok := taskStore.Get(taskBID)
	if !ok {
		t.Fatalf("taskStore lost record %q after /api/workflow/reject", taskBID)
	}
	if storedB.Status != "rejected" {
		t.Errorf("taskStore record %q Status = %q after /api/workflow/reject, want %q; "+
			"handleWorkflowReject must also update the taskStore record that "+
			"handleSprintExecute dispatches from, not just currentWorkflow.Tasks",
			taskBID, storedB.Status, "rejected")
	}

	// The whole point: handleSprintExecute reads taskStore.Approved(), so the
	// approved task must actually surface there once the workflow-level
	// approval lands.
	approved := taskStore.Approved()
	found := false
	for _, tk := range approved {
		if tk.ID == taskAID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("taskStore.Approved() does not contain %q after /api/workflow/approve; "+
			"handleSprintExecute dispatches exclusively from taskStore.Approved(), so a "+
			"workflow-level approval that never reaches taskStore is invisible to it", taskAID)
	}
}

// TestHandleSprintExecute_UpdatesCurrentWorkflow pins the NotebookLM research
// gap: dual task-state representation in the sprint-execution path.
// handleSprintExecute (main.go) dispatches every taskStore.Approved() task
// and, as each finishes, calls taskStore.UpdateStatus/SetOutput exclusively —
// it never reads or writes the package-level currentWorkflow at all.
// TestHandleWorkflowApproveReject_UpdatesTaskStore above already pins the
// opposite direction (workflow-level approve/reject must also reach
// taskStore); this test pins the missing reverse direction: once a sprint
// actually runs and completes a task, the corresponding
// currentWorkflow.Tasks[i].Status and currentWorkflow.Company.CurrentSprint
// must advance too, or every dashboard surface reading currentWorkflow
// (handleWorkflowPending/PendingApprovals, the sprint-goal UI, ExecuteSprint's
// own Company.CurrentSprint convention in workflow_engine.go) is permanently
// stuck showing the task as still merely "approved" even after it has
// actually finished executing.
func TestHandleSprintExecute_UpdatesCurrentWorkflow(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	origTaskStore := taskStore
	origWorkflow := currentWorkflow
	prevRunner := dashAgentRunner
	t.Cleanup(func() {
		taskStore = origTaskStore
		currentWorkflowMu.Lock()
		currentWorkflow = origWorkflow
		currentWorkflowMu.Unlock()
		dashAgentRunner = prevRunner
	})

	// Force RunTask to succeed deterministically without a real BT agent
	// process: an AlwaysSucceed tree with no QualitySpec, the same technique
	// TestHandleAgentExecute_SetsQualityScoreFromRunResult above uses.
	dashAgentRunner = &agent.RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return &evolution.SerializableNode{Type: "AlwaysSucceed"}
		},
	}

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))

	company := startup.NewDefaultCompany()
	company.CurrentSprint = 1
	wf := dashboard.NewWorkflow("test-wf", nil, company)
	wf.ID = "wf-sprint-test"
	wf.Tasks = []dashboard.WorkflowTask{
		{ID: "task-a", Status: dashboard.StatusApproved, Priority: dashboard.PriorityHigh, SprintTarget: 2},
	}
	currentWorkflowMu.Lock()
	currentWorkflow = wf
	currentWorkflowMu.Unlock()

	// Seed taskStore exactly the way handleAnalyze does: composed ID
	// wf.ID + "-" + wt.ID, then approve it so taskStore.Approved() dispatches
	// it — mirroring TestHandleWorkflowApproveReject_UpdatesTaskStore's setup.
	taskID := wf.ID + "-task-a"
	if err := taskStore.Create(dashboard.Task{
		ID: taskID, Title: "Task A", Priority: "high", Sprint: 2, Assignee: "engineer",
	}); err != nil {
		t.Fatalf("seed task-a: %v", err)
	}
	if err := taskStore.Approve(taskID, "dashboard"); err != nil {
		t.Fatalf("approve task-a: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sprint/execute", nil)
	rr := httptest.NewRecorder()
	handleSprintExecute(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sprint execute status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		sprintState.Lock()
		running := sprintState.Running
		sprintState.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sprint never finished running within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	storedA, ok := taskStore.Get(taskID)
	if !ok {
		t.Fatalf("taskStore lost record %q after sprint execution", taskID)
	}
	if storedA.Status != "completed" {
		t.Fatalf("taskStore record %q Status = %q after sprint execution, want %q (sprint didn't run as expected, "+
			"can't test the currentWorkflow reconciliation)", taskID, storedA.Status, "completed")
	}

	currentWorkflowMu.RLock()
	gotStatus := currentWorkflow.Tasks[0].Status
	gotSprint := currentWorkflow.Company.CurrentSprint
	currentWorkflowMu.RUnlock()

	if gotStatus != dashboard.StatusCompleted {
		t.Errorf("currentWorkflow.Tasks[0].Status = %v after handleSprintExecute completed the task, want %v; "+
			"handleSprintExecute must also update the currentWorkflow.WorkflowTask sharing the composed ID, "+
			"not just taskStore", gotStatus, dashboard.StatusCompleted)
	}
	if gotSprint != 2 {
		t.Errorf("currentWorkflow.Company.CurrentSprint = %d after executing a SprintTarget=2 task, want 2; "+
			"handleSprintExecute never advances Company.CurrentSprint, only taskStore", gotSprint)
	}
}

// TestHandleAnalyze_TaskIDsUniqueAcrossAnalyses pins the fix for guaranteed
// task-ID collisions in handleAnalyze's Workflow-derived task IDs.
//
// handleAnalyze composes each persisted dashboard.Task's ID as
// wf.ID + "-" + wt.ID. dashboard.NewWorkflow mints wf.ID as
// fmt.Sprintf("wf-%d", time.Now().Unix()) — second-granularity — and
// Workflow.RecommendationsToTasks mints every wt.ID from a purely positional
// scheme ("rec-001", "agree-001", ...) that depends only on slice position,
// never on wf.ID, wall-clock time, or any other per-workflow entropy
// (internal/dashboard/workflow_engine.go). So whenever two
// /api/thinktank/analyze requests land within the same wall-clock second —
// routine for rapid or automated callers, and the norm rather than the
// exception under any real traffic — the two Workflows share an identical
// wf.ID, and because RecommendationsToTasks always assigns the very same
// positional WorkflowTask IDs regardless of which Workflow instance calls
// it, the final composed dashboard.Task IDs for the two analyses collide
// exactly. dashboard.TaskStore's Get/UpdateStatus/Approve/Reject
// (internal/dashboard/tasks.go) linear-scan for the first ID match, so the
// second analysis's tasks silently become unreachable/misattributed the
// moment this happens.
//
// A real same-second collision can't be reliably forced from a black-box
// test without racing the wall clock, so — mirroring
// TestHandleAnalyze_TaskIDsKeyedOnInsightIndex's approach above for a sibling
// timestamp-collision bug — this test reproduces the guaranteed scenario
// deterministically: it runs one real analysis via handleAnalyze, then
// derives a second Workflow's tasks the same way handleAnalyze does but with
// its ID forced to match the first (exactly what NewWorkflow's
// time.Now().Unix()-keyed scheme guarantees whenever two analyses land in
// the same second), and asserts the resulting task IDs must still be
// unique — which requires task-ID minting to carry real entropy of its own
// instead of leaning entirely on wf.ID for uniqueness.
func TestHandleAnalyze_TaskIDsUniqueAcrossAnalyses(t *testing.T) {
	origTaskStore := taskStore
	origLLM := sharedLLM
	origWorkflow := currentWorkflow
	t.Cleanup(func() {
		taskStore = origTaskStore
		sharedLLM = origLLM
		currentWorkflow = origWorkflow
	})

	dir := t.TempDir()
	taskStore = dashboard.NewTaskStore(filepath.Join(dir, "tasks.json"))
	sharedLLM = engine.NewMockLLM()

	httpReq := httptest.NewRequest(http.MethodGet, "/api/thinktank/analyze?topic=Should+we+ship+feature+X", nil)
	rr := httptest.NewRecorder()
	handleAnalyze(rr, httpReq)
	if rr.Code != http.StatusOK {
		t.Fatalf("first analyze status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if currentWorkflow == nil {
		t.Fatal("handleAnalyze did not set currentWorkflow")
	}
	firstTasks := taskStore.List()
	if len(firstTasks) == 0 {
		t.Fatal("first analysis persisted 0 tasks; cannot exercise the collision")
	}

	// Build a second analysis's Workflow from different synthesis content,
	// but force its ID to match the first — reproducing exactly what
	// NewWorkflow hands back when invoked within the same wall-clock second
	// as the first call above.
	tt2 := &thinktank.ThinkTank{
		Name: "Council",
		Synthesis: &thinktank.Synthesis{
			Recommendation:       "A different recommendation from the second analysis",
			PointsOfAgreement:    []string{"A different point of agreement"},
			PointsOfDisagreement: []string{"A different point of disagreement"},
			DissentingNotes:      []string{"A different dissenting note"},
		},
	}
	wf2 := dashboard.NewWorkflow("Council", tt2, companyState)
	wf2.ID = currentWorkflow.ID
	wf2.RecommendationsToTasks()
	wf2.Prioritize()
	for _, wt := range wf2.Tasks {
		task := dashboard.Task{
			ID:          wf2.ID + "-" + wt.ID,
			Title:       wt.Title,
			Description: wt.Description,
			Priority:    wt.Priority.String(),
			Assignee:    wt.AssigneeRole,
			Source:      "thinktank",
			SourceID:    wt.Source,
			Sprint:      wt.SprintTarget,
			StoryPoints: wt.EstimatedEffort,
			Approval:    wt.Approval,
		}
		if err := taskStore.Create(task); err != nil {
			t.Fatalf("persist second analysis task: %v", err)
		}
	}

	seen := make(map[string]int)
	for _, tk := range taskStore.List() {
		seen[tk.ID]++
	}
	var collisions []string
	for id, count := range seen {
		if count > 1 {
			collisions = append(collisions, fmt.Sprintf("%s(x%d)", id, count))
		}
	}
	if len(collisions) > 0 {
		t.Errorf("two analyses landing in the same wall-clock second produced colliding task IDs: %v; "+
			"handleAnalyze/NewWorkflow must mint task IDs that stay unique across analyses even when "+
			"wf.ID collides (e.g. a per-workflow random/monotonic component folded into each "+
			"WorkflowTask's ID in RecommendationsToTasks), not derive uniqueness solely from wf.ID + a "+
			"purely positional WorkflowTask.ID", collisions)
	}
}

// TestHandleAgentExecute_SetsQualityScoreFromRunResult pins the NotebookLM
// research gap: handleAgentExecute (POST /api/agents/execute) is the HTTP
// counterpart reliability.RemoteExecutor calls into for horizontal scaling
// (see internal/reliability/remote_executor_test.go, which decodes the JSON
// response straight into a reliability.AgentResult and asserts QualityScore).
// The handler builds its reliability.AgentResult from
// AgentExecutor.RunTask's (output, outcome, err) triple, which drops the
// agent.RunResult.Quality the underlying RunOnce already computed — so every
// remote-routed successful run reports QualityScore 0.0 regardless of actual
// quality. cmd/bt-agent/main.go's local AgentExecutor.Execute (the in-process
// analogue) does this correctly: QualityScore: res.Quality straight from
// RunOnce's result. handleAgentExecute must do the same.
func TestHandleAgentExecute_SetsQualityScoreFromRunResult(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	prevRunner, prevPool, prevLimiter := dashAgentRunner, dashWorkerPool, dashConcurrencyLimiter
	t.Cleanup(func() {
		dashAgentRunner = prevRunner
		dashWorkerPool = prevPool
		dashConcurrencyLimiter = prevLimiter
	})
	// Force the synchronous fallback path in handleAgentExecute so the test
	// doesn't race a worker-pool goroutine.
	dashWorkerPool = nil
	dashConcurrencyLimiter = nil

	// A bare AlwaysSucceed tree with no QualitySpec drives RunOnce's quality
	// estimate deterministically: ValidateQualitySpec(nil, "") falls through
	// to estimateQuality(""), which returns 0.2 for output shorter than 10
	// chars (internal/agent/scheduler.go's estimateQuality) — never 0.0.
	dashAgentRunner = &agent.RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return &evolution.SerializableNode{Type: "AlwaysSucceed"}
		},
	}

	body, _ := json.Marshal(map[string]string{
		"agent": "quality-echo-agent",
		"task":  "run the thing",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/execute", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handleAgentExecute(rr, req)

	var res reliability.AgentResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode /api/agents/execute response: %v; body=%s", err, rr.Body.String())
	}
	if !res.Success {
		t.Fatalf("expected a successful run from an AlwaysSucceed tree, got %+v (body=%s)", res, rr.Body.String())
	}
	if res.QualityScore <= 0 {
		t.Fatalf("handleAgentExecute must set AgentResult.QualityScore from the run's real quality "+
			"estimate (RemoteExecutor decodes this field for horizontal-scaling quality tracking); "+
			"got QualityScore=%v, want > 0 (response=%+v)", res.QualityScore, res)
	}
}

// dashboardMuxAPIPathRE mirrors the mux.HandleFunc("/api/...", ...)
// registrations in main.go's main(). It is deliberately loose (any path
// starting with /api/) so the coverage check below tracks main.go's
// registrations without needing to hand-maintain a parallel list.
var dashboardMuxAPIPathRE = regexp.MustCompile(`mux\.HandleFunc\("(/api/[^"]*)"`)

// TestDashboardAPIRoutesHaveOpenAPICoverage pins that every /api/* endpoint
// registered on the dashboard's mux (cmd/bt-dashboard/main.go) has a
// matching Route in api.DashboardRoutes() (internal/api/openapi.go). The
// dashboard's OpenAPI response validator only checks traffic against
// DashboardRoutes(), so a mux path with no matching Route is silently
// unvalidated in production — this test names every such gap.
func TestDashboardAPIRoutesHaveOpenAPICoverage(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	matches := dashboardMuxAPIPathRE.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("found no mux.HandleFunc(\"/api/...\" registrations in main.go; " +
			"dashboardMuxAPIPathRE may be stale")
	}

	registered := make(map[string]bool)
	for _, route := range api.DashboardRoutes() {
		registered[route.Path] = true
	}

	seenMuxPath := make(map[string]bool)
	var missing []string
	for _, m := range matches {
		muxPath := m[1]
		if seenMuxPath[muxPath] {
			continue
		}
		seenMuxPath[muxPath] = true
		if !registered[muxPath] {
			missing = append(missing, muxPath)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d dashboard mux path(s) registered in main.go have no matching Route in "+
			"api.DashboardRoutes(), so the OpenAPI response validator never checks them: %v",
			len(missing), missing)
	}
}

// TestMainSupportsVersionFlagForDriftSmokeTest pins the --version fast path
// the deploy-drift restart handoff smoke-tests rebuilt binaries with
// (internal/agent/deploy_drift.go runs `<binary> --version` and rolls the
// swap back on failure): without it, arming BT_AUTO_RESTART_ON_DRIFT on this
// unit would roll back every rebuild and the daemon could never self-adopt a
// new revision.
func TestMainSupportsVersionFlagForDriftSmokeTest(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "if versionRequested()") {
		t.Error("main.go must short-circuit on --version (versionRequested) so the deploy-drift smoke test can validate a rebuilt binary")
	}
}

// TestNewAgentExecutor_SetsCBStore pins milestone 2/3 of the Q3 Reliability
// program closing the dashboard's dead task-metrics and circuit-breaker
// recording gap in AgentExecutor.RunTaskResult. Milestone 1 added
// AgentExecutor.CBStore (internal/dashboard/executor.go) and wired
// RunTaskResult to report outcomes to it — but newAgentExecutor
// (cmd/bt-dashboard/agent_executor.go) only sets Runner, never CBStore, so in
// production every dashboard-dispatched RunTaskResult call's
// recordCircuitBreakerOutcome silently no-ops on a nil CBStore: a flaky agent
// invoked only through the dashboard never trips the shared breaker the
// scheduler and A2A auction paths already honor. newAgentExecutor must set
// CBStore from a package-level *agent.AgentCircuitBreakerStore loaded once at
// startup in main.go via agent.CircuitBreakersFile(), mirroring
// cmd/bt-agent/main.go's buildSchedulerConfig wiring.
func TestNewAgentExecutor_SetsCBStore(t *testing.T) {
	e := newAgentExecutor()
	if e.CBStore == nil {
		t.Fatal("newAgentExecutor().CBStore is nil; must be wired from a package-level " +
			"*agent.AgentCircuitBreakerStore loaded at startup in main.go via " +
			"agent.CircuitBreakersFile(), mirroring cmd/bt-agent/main.go's buildSchedulerConfig")
	}
}

// TestHandleAgentExecute_CircuitBreakerOpenReturns503 pins milestone 3/3 of
// the Q3 Reliability program closing the dashboard's dead task-metrics and
// circuit-breaker recording gap in AgentExecutor.RunTaskResult. Milestone 2
// wired newAgentExecutor's CBStore so RunTaskResult *records* outcomes to the
// shared breaker — but nothing on the dashboard's HTTP path *checks* it
// before dispatching, unlike internal/agent/scheduler.go's tick, which skips
// a job outright when s.cbStore.Allowed(job.AgentName) is false. So even
// after a dashboard-triggered agent trips its breaker open, the very next
// POST /api/agents/execute for that agent still submits to the worker pool
// and calls RunTaskResult, wasting resources on a known-broken agent exactly
// like the scheduler gate was added to prevent. handleAgentExecute must
// check CBStore.Allowed(agentName) before submitting to the worker pool and
// return a 503 JSON error instead of executing when the breaker is open.
func TestHandleAgentExecute_CircuitBreakerOpenReturns503(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	prevRunner, prevPool, prevLimiter := dashAgentRunner, dashWorkerPool, dashConcurrencyLimiter
	t.Cleanup(func() {
		dashAgentRunner = prevRunner
		dashWorkerPool = prevPool
		dashConcurrencyLimiter = prevLimiter
	})
	// Force the synchronous fallback path so the test doesn't race a
	// worker-pool goroutine.
	dashWorkerPool = nil
	dashConcurrencyLimiter = nil

	const agentName = "breaker-tripped-execute-agent"
	cb := getDashCBStore().Get(agentName)
	for range 10 {
		cb.RecordFailure()
	}
	if cb.State() != agent.CircuitOpen {
		t.Fatalf("test setup: expected circuit breaker for %q to be open after repeated failures, got %v",
			agentName, cb.State())
	}

	invoked := false
	dashAgentRunner = &agent.RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode {
			invoked = true
			return &evolution.SerializableNode{Type: "AlwaysSucceed"}
		},
	}

	body, _ := json.Marshal(map[string]string{
		"agent": agentName,
		"task":  "run the thing",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/execute", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handleAgentExecute(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleAgentExecute with an open circuit breaker: got status %d, want %d (body=%s)",
			rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}
	if invoked {
		t.Error("handleAgentExecute invoked RunTaskResult (ResolveTree was called) despite an open " +
			"circuit breaker for the agent; it must check CBStore.Allowed(agentName) before submitting " +
			"to the worker pool")
	}
}

// TestHandleAgentRun_CircuitBreakerOpenReturns503 is handleAgentRun's
// counterpart to TestHandleAgentExecute_CircuitBreakerOpenReturns503 above —
// GET /api/agents/run is the dashboard UI's own agent-trigger path and must
// honor the same shared breaker gate as POST /api/agents/execute.
func TestHandleAgentRun_CircuitBreakerOpenReturns503(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	prevRunner, prevPool, prevLimiter := dashAgentRunner, dashWorkerPool, dashConcurrencyLimiter
	t.Cleanup(func() {
		dashAgentRunner = prevRunner
		dashWorkerPool = prevPool
		dashConcurrencyLimiter = prevLimiter
	})
	dashWorkerPool = nil
	dashConcurrencyLimiter = nil

	const agentName = "breaker-tripped-run-agent"
	cb := getDashCBStore().Get(agentName)
	for range 10 {
		cb.RecordFailure()
	}
	if cb.State() != agent.CircuitOpen {
		t.Fatalf("test setup: expected circuit breaker for %q to be open after repeated failures, got %v",
			agentName, cb.State())
	}

	invoked := false
	dashAgentRunner = &agent.RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode {
			invoked = true
			return &evolution.SerializableNode{Type: "AlwaysSucceed"}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/run?agent="+agentName+"&task=run+the+thing", nil)
	rr := httptest.NewRecorder()
	handleAgentRun(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("handleAgentRun with an open circuit breaker: got status %d, want %d (body=%s)",
			rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}
	if invoked {
		t.Error("handleAgentRun invoked RunTaskResult (ResolveTree was called) despite an open circuit " +
			"breaker for the agent; it must check CBStore.Allowed(agentName) before submitting to the " +
			"worker pool")
	}
}

// TestHandleTrees_IncludesFitnessAndLineage pins milestone 2/4 of the
// "surface knowledge-graph fitness and evolution lineage" program:
// /api/trees currently returns only id/name/category/node_count
// (main.go:524-530), leaving a tree's Fitness, StructuralFitness, RunCount,
// EvolvedCount, LastOutcome, and evolution lineage (EvolutionLineage) as a
// dead blind spot for the dashboard UI. The handler must include all of
// these per tree.
func TestHandleTrees_IncludesFitnessAndLineage(t *testing.T) {
	origKG := kg
	t.Cleanup(func() { kg = origKG })

	g := knowledge.NewKnowledgeGraph()
	g.Register(&knowledge.TreeMeta{
		ID:                "base:tree",
		Name:              "Base Tree",
		Category:          "finance",
		NodeCount:         5,
		Fitness:           72.5,
		StructuralFitness: 60.0,
		RunCount:          10,
		EvolvedCount:      2,
		LastOutcome:       "success",
	})
	g.Register(&knowledge.TreeMeta{
		ID:        "base:tree-evolved-1",
		Name:      "Evolved 1",
		Category:  "finance",
		NodeCount: 8,
	})
	g.Connect("base:tree", "base:tree-evolved-1", "evolved_from")
	kg = g

	req := httptest.NewRequest(http.MethodGet, "/api/trees", nil)
	rr := httptest.NewRecorder()
	handleTrees(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var trees []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &trees); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	var base map[string]any
	for _, tr := range trees {
		if tr["id"] == "base:tree" {
			base = tr
			break
		}
	}
	if base == nil {
		t.Fatalf("response missing entry for base:tree; body=%s", rr.Body.String())
	}

	if got, want := base["fitness"], 72.5; got != want {
		t.Errorf("fitness = %v, want %v", got, want)
	}
	if got, want := base["structural_fitness"], 60.0; got != want {
		t.Errorf("structural_fitness = %v, want %v", got, want)
	}
	if got, want := base["run_count"], float64(10); got != want {
		t.Errorf("run_count = %v, want %v", got, want)
	}
	if got, want := base["evolved_count"], float64(2); got != want {
		t.Errorf("evolved_count = %v, want %v", got, want)
	}
	if got, want := base["last_outcome"], "success"; got != want {
		t.Errorf("last_outcome = %v, want %v", got, want)
	}

	lineage, ok := base["lineage"].(map[string]any)
	if !ok {
		t.Fatalf("lineage missing or not an object; body=%s", rr.Body.String())
	}
	if got, want := lineage["base_id"], "base:tree"; got != want {
		t.Errorf("lineage.base_id = %v, want %v", got, want)
	}
	evolvedIDs, ok := lineage["evolved_ids"].([]any)
	if !ok || len(evolvedIDs) != 1 || evolvedIDs[0] != "base:tree-evolved-1" {
		t.Errorf("lineage.evolved_ids = %v, want [base:tree-evolved-1]", lineage["evolved_ids"])
	}
}

// TestHandleTrees_IncludesFullDomainCatalog pins the "Create-Agent tree
// dropdown" gap found in the 2026-07-17 structural review:
// cmd/bt-dashboard/static/js/tabs/agents.js hardcodes a stale client-side
// list of trees, and the obvious fix — fetch the dropdown from /api/trees —
// doesn't actually work today, because handleTrees only echoes back
// kg.Trees (the runtime knowledge-graph registry, ~43 entries) while
// internal/domains.AllDomainTrees() defines 29 additional catalog trees
// (goap_fusion, goap_fusion_loop, bt_fusion, bt_manager, notebooklm,
// auction_demo, the arc42:section1..12 family, etc.) that never appear in
// kg and so can never be selected when creating a new agent. /api/trees
// must merge in every domains.AllDomainTrees() entry missing from kg so it
// can serve as a single, live, complete catalog for the dropdown.
func TestHandleTrees_IncludesFullDomainCatalog(t *testing.T) {
	origKG := kg
	t.Cleanup(func() { kg = origKG })

	g := knowledge.NewKnowledgeGraph()
	g.Register(&knowledge.TreeMeta{
		ID:       "default",
		Name:     "Default Agent",
		Category: "core",
	})
	kg = g

	req := httptest.NewRequest(http.MethodGet, "/api/trees", nil)
	rr := httptest.NewRecorder()
	handleTrees(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var trees []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &trees); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	seen := make(map[string]bool, len(trees))
	for _, tr := range trees {
		if id, ok := tr["id"].(string); ok {
			seen[id] = true
		}
	}

	domainTrees := domains.AllDomainTrees()
	var missing []string
	for name := range domainTrees {
		if !seen["domain:"+name] && !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("/api/trees is missing %d of %d domains.AllDomainTrees() catalog entries not present "+
			"in the runtime knowledge graph (e.g. %v); handleTrees must merge the domain-tree catalog "+
			"into its response so the Create-Agent dropdown can fetch a complete tree list from this "+
			"endpoint instead of a hardcoded client-side list", len(missing), len(domainTrees), missing)
	}
}

// TestHandleTrees_DomainEntriesCarryDescription pins milestone 2/2 of the
// "wire the dashboard's live /api/trees catalog into the Create-Agent
// dropdown" program: handleTrees already merges every domains.AllDomainTrees()
// entry missing from kg (TestHandleTrees_IncludesFullDomainCatalog), but each
// merged entry only carries id/name/category/node_count — the human-readable
// summary in the exported internal/domains.Descriptions map (consulted today
// by internal/gardener/gardener.go and cmd/bt-agent/tools.go, but never
// surfaced through this endpoint) is dropped on the floor. Without it, the
// Create-Agent dropdown can list a tree's raw ID but not explain what it
// does. Each merged domain-tree entry must carry a "description" field
// populated from domains.Descriptions[name].
func TestHandleTrees_DomainEntriesCarryDescription(t *testing.T) {
	origKG := kg
	t.Cleanup(func() { kg = origKG })

	g := knowledge.NewKnowledgeGraph()
	g.Register(&knowledge.TreeMeta{
		ID:       "default",
		Name:     "Default Agent",
		Category: "core",
	})
	kg = g

	req := httptest.NewRequest(http.MethodGet, "/api/trees", nil)
	rr := httptest.NewRecorder()
	handleTrees(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var trees []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &trees); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	wantDesc := domains.Descriptions["goap_fusion"]
	if wantDesc == "" {
		t.Fatal("domains.Descriptions[\"goap_fusion\"] is empty; test fixture assumption broken")
	}

	var entry map[string]any
	for _, tr := range trees {
		if tr["id"] == "domain:goap_fusion" {
			entry = tr
			break
		}
	}
	if entry == nil {
		t.Fatalf("response missing entry for domain:goap_fusion; body=%s", rr.Body.String())
	}

	if got, ok := entry["description"].(string); !ok || got == "" {
		t.Errorf("domain:goap_fusion description = %v, want non-empty string %q", entry["description"], wantDesc)
	} else if got != wantDesc {
		t.Errorf("domain:goap_fusion description = %q, want %q (domains.Descriptions[\"goap_fusion\"])", got, wantDesc)
	}
}

// relocateDomainDescription moves name's description out of
// domains.Descriptions and into domains.ResolverReachableDescriptions for the
// duration of the test, restoring both maps afterwards. It reproduces the
// legitimate maintenance state in which a tree registered in AllDomainTrees()
// is described by one of the other two description maps: domains.DescriptionFor
// still resolves the name, a direct domains.Descriptions index no longer does.
//
// Safe as a global mutation because no test in this package calls t.Parallel().
func relocateDomainDescription(t *testing.T, name string) string {
	t.Helper()
	desc, ok := domains.Descriptions[name]
	if !ok || strings.TrimSpace(desc) == "" {
		t.Fatalf("fixture assumption broken: domains.Descriptions[%q] = %q, want a non-blank description", name, desc)
	}
	delete(domains.Descriptions, name)
	domains.ResolverReachableDescriptions[name] = desc
	t.Cleanup(func() {
		domains.Descriptions[name] = desc
		delete(domains.ResolverReachableDescriptions, name)
	})
	return desc
}

// TestHandleTrees_DomainDescriptionsResolveThroughDescriptionFor is the
// /api/trees follow-up to TestHandleTrees_DomainEntriesCarryDescription: the
// merged domain-catalog entries carry a "description", but handleTrees sources
// it by indexing domains.Descriptions directly.
//
// The domains package deliberately splits descriptions across three maps —
// Descriptions (the curated AllDomainTrees surface), NonRegistryDescriptions,
// and ResolverReachableDescriptions — and DescriptionFor is the single lookup
// spanning all three, so callers need not know which map holds a given name
// (ADR-251, ADR-255). A direct index silently yields "" the moment a registry
// tree's description lives in one of the other two maps, which is exactly what
// a tree promoted onto AllDomainTrees() without its description entry moving
// along with it looks like. The Create-Agent dropdown then lists that tree's
// raw ID with nothing explaining what it does.
//
// RED before the migration: the merged entry's "description" is the empty
// string because domains.Descriptions no longer holds the relocated name.
func TestHandleTrees_DomainDescriptionsResolveThroughDescriptionFor(t *testing.T) {
	origKG := kg
	t.Cleanup(func() { kg = origKG })

	const treeName = "goap_fusion"
	want := relocateDomainDescription(t, treeName)

	g := knowledge.NewKnowledgeGraph()
	g.Register(&knowledge.TreeMeta{
		ID:       "default",
		Name:     "Default Agent",
		Category: "core",
	})
	kg = g

	req := httptest.NewRequest(http.MethodGet, "/api/trees", nil)
	rr := httptest.NewRecorder()
	handleTrees(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var trees []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &trees); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	id := "domain:" + treeName
	var entry map[string]any
	for _, tr := range trees {
		if tr["id"] == id {
			entry = tr
			break
		}
	}
	if entry == nil {
		t.Fatalf("response missing entry for %s; body=%s", id, rr.Body.String())
	}

	if got, _ := entry["description"].(string); got != want {
		t.Errorf("%s description = %q, want %q — handleTrees must resolve domain descriptions via "+
			"domains.DescriptionFor(%q), which spans all three description maps, instead of indexing "+
			"domains.Descriptions directly", id, got, want, treeName)
	}
}

// TestAgentsJS_TreeDropdownGroupedByCategory is the client-side follow-up to
// TestHandleTrees_IncludesFullDomainCatalog: now that /api/trees serves a
// complete, live tree catalog (kg.Trees merged with domains.AllDomainTrees()),
// cmd/bt-dashboard/static/js/tabs/agents.js must actually fetch it and use it
// to populate the Create-Agent "Select tree..." dropdown, instead of the
// hardcoded <option> list baked into renderAgents(). It must also group the
// resulting <option> elements into <optgroup> elements keyed by each tree's
// "category" field (as returned by handleTrees), so trees from different
// categories (core, domain, research, thinktank, finance, ...) are visually
// distinguishable and a flat unsorted dropdown can't silently return.
func TestAgentsJS_TreeDropdownGroupedByCategory(t *testing.T) {
	data, err := staticFS.ReadFile("static/js/tabs/agents.js")
	if err != nil {
		t.Fatalf("read static/js/tabs/agents.js: %v", err)
	}
	js := string(data)

	if !strings.Contains(js, "/api/trees") {
		t.Errorf("agents.js must fetch the live tree catalog from /api/trees to populate the Create-Agent " +
			"dropdown instead of relying on the hardcoded <option> list in renderAgents(); found no " +
			"reference to /api/trees in the embedded JS")
	}
	// The grouping is built via DOM nodes (document.createElement('optgroup')),
	// not innerHTML markup: tree names/ids are partly user-influenced, so
	// string-concatenated markup was an injection surface.
	if !strings.Contains(js, "<optgroup") && !strings.Contains(js, "createElement('optgroup')") && !strings.Contains(js, `createElement("optgroup")`) {
		t.Errorf("agents.js must group the Create-Agent tree dropdown into optgroup elements (one per " +
			"tree category) instead of a flat list; found neither <optgroup markup nor createElement('optgroup') in the embedded JS")
	}
	if !strings.Contains(js, ".category") {
		t.Errorf("agents.js must key the <optgroup> grouping on each tree's category field, as returned " +
			"by handleTrees; found no reference to a tree's .category property in the embedded JS")
	}
}

// TestDashboardJS_EscapesUserInfluencedInterpolations audits the embedded JS
// for the injection-surface fix: lib/api.js must define the shared esc()
// HTML-escaper, and the three renderers that interpolate user-influenced data
// (agent names/descriptions in agents.js, tree names/ids in mindmap.js and
// trees.js) must actually call it. An agent or tree named '<img onerror=...>'
// must never reach innerHTML unescaped.
func TestDashboardJS_EscapesUserInfluencedInterpolations(t *testing.T) {
	api, err := staticFS.ReadFile("static/js/lib/api.js")
	if err != nil {
		t.Fatalf("read lib/api.js: %v", err)
	}
	if !strings.Contains(string(api), "function esc(") {
		t.Fatal("lib/api.js must define the shared esc() HTML-escape helper")
	}
	for _, f := range []string{"static/js/tabs/agents.js", "static/js/tabs/mindmap.js", "static/js/tabs/trees.js"} {
		data, err := staticFS.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if !strings.Contains(string(data), "esc(") {
			t.Errorf("%s interpolates user-influenced data into innerHTML but never calls esc()", f)
		}
	}
	// The specific pre-fix hole: agents.js interpolated a.name raw into the
	// task-title span.
	agentsJS, _ := staticFS.ReadFile("static/js/tabs/agents.js")
	if strings.Contains(string(agentsJS), "'    <span class=\"task-title\">' + a.name +") {
		t.Error("agents.js interpolates a.name raw into the task-title markup; wrap it in esc()")
	}
}

// setupHITLFinalizationTest wires dashboard.AgentRegistry and
// dashboard.PersonaStore (the Q4 Personalization milestone 3/3 injection
// hooks, mirroring the existing dashboard.DiscoverTreeFn pattern) to an
// isolated agent registry and persona store for the duration of the test,
// and installs an isolated hitl.DefaultStore. Restores all three on cleanup.
func setupHITLFinalizationTest(t *testing.T) (*hitl.Store, *agent.Registry, *persona.Store) {
	t.Helper()
	dir := t.TempDir()

	hitlStore, err := hitl.InitStore(filepath.Join(dir, "hitl"))
	if err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	reg, err := agent.NewRegistry(filepath.Join(dir, "registry"))
	if err != nil {
		t.Fatalf("agent registry: %v", err)
	}
	pStore, err := persona.NewStore(filepath.Join(dir, "users"))
	if err != nil {
		t.Fatalf("persona store: %v", err)
	}

	prevHITL := hitl.DefaultStore
	prevReg := dashboard.AgentRegistry
	prevStore := dashboard.PersonaStore
	hitl.DefaultStore = hitlStore
	dashboard.AgentRegistry = reg
	dashboard.PersonaStore = pStore
	t.Cleanup(func() {
		hitl.DefaultStore = prevHITL
		dashboard.AgentRegistry = prevReg
		dashboard.PersonaStore = prevStore
	})

	return hitlStore, reg, pStore
}

// TestHandleHITL_ApproveActivatesAutomation pins milestone 3/3 of the Q4
// Personalization & Self-Growth dashboard HITL approval-finalization
// program: approving a dashboard-surfaced automation-proposal HITL request
// must actually activate the automation as a scheduled agent — the same
// outcome persona.FinalizeAutomationApproval already gives the MCP
// bt_hitl_approve path (milestone 1/2) — instead of merely flipping the HITL
// request's status and leaving the automation dormant.
func TestHandleHITL_ApproveActivatesAutomation(t *testing.T) {
	hitlStore, reg, pStore := setupHITLFinalizationTest(t)

	const user = "nico"
	const treeID = "goal:demo"
	const agentName = "auto-nico-sig-approve"
	const signature = "sig-approve"

	ledger, err := persona.NewAutomationStore(pStore.Workspace(user))
	if err != nil {
		t.Fatalf("automation ledger: %v", err)
	}
	req := hitl.NewRequest("AutomationProposal", "AutomationGate", "demo task", "", "", "please review", map[string]any{
		"automation":        "true",
		"tree_id":           treeID,
		"agent_name":        agentName,
		"user":              user,
		"pattern_signature": signature,
		"schedule":          "0 9 * * *",
	})
	if err := hitlStore.Create(req); err != nil {
		t.Fatalf("create hitl request: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: signature,
		Status:    persona.AutomationPending,
		HITLID:    req.ID,
		TreeID:    treeID,
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/hitl/"+req.ID+"/approve", bytes.NewReader([]byte(`{"reviewer":"tester"}`)))
	dashboard.HandleHITL(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: status %d body %s", rr.Code, rr.Body.String())
	}

	if _, err := reg.Get(agentName); err != nil {
		t.Errorf("dashboard-approved automation must activate an agent in the registry (like the MCP path); reg.Get(%q) failed: %v", agentName, err)
	}
	rec, ok, err := ledger.Get(signature)
	if err != nil || !ok {
		t.Fatalf("ledger record missing after approval: ok=%v err=%v", ok, err)
	}
	if rec.Status != persona.AutomationApproved {
		t.Errorf("ledger status = %q, want %q after dashboard approval", rec.Status, persona.AutomationApproved)
	}
}

// TestHandleHITL_RejectQuarantinesAutomationTree pins the reject half of the
// same milestone: rejecting a dashboard-surfaced automation proposal must
// quarantine its compiled tree and mark the ledger record rejected, exactly
// like the MCP bt_hitl_reject path already does via
// persona.FinalizeAutomationApproval(..., approved=false).
func TestHandleHITL_RejectQuarantinesAutomationTree(t *testing.T) {
	hitlStore, reg, pStore := setupHITLFinalizationTest(t)

	const user = "nico"
	const treeID = "goal:reject-me"
	const agentName = "auto-nico-sig-reject"
	const signature = "sig-reject"

	ws := pStore.Workspace(user)
	ledger, err := persona.NewAutomationStore(ws)
	if err != nil {
		t.Fatalf("automation ledger: %v", err)
	}
	treePath, err := evolution.SaveNamedTree(ws.TreesDir(), treeID, &evolution.SerializableNode{Type: "action", Name: "noop"})
	if err != nil {
		t.Fatalf("save tree: %v", err)
	}
	req := hitl.NewRequest("AutomationProposal", "AutomationGate", "reject task", "", "", "please review", map[string]any{
		"automation":        "true",
		"tree_id":           treeID,
		"agent_name":        agentName,
		"user":              user,
		"pattern_signature": signature,
		"schedule":          "0 9 * * 1",
	})
	if err := hitlStore.Create(req); err != nil {
		t.Fatalf("create hitl request: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: signature,
		Status:    persona.AutomationPending,
		HITLID:    req.ID,
		TreeID:    treeID,
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/hitl/"+req.ID+"/reject", bytes.NewReader([]byte(`{"reviewer":"tester","reason":"no"}`)))
	dashboard.HandleHITL(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject: status %d body %s", rr.Code, rr.Body.String())
	}

	if _, err := reg.Get(agentName); err == nil {
		t.Error("dashboard-rejected automation must not activate an agent")
	}
	if _, err := os.Stat(treePath); err == nil {
		t.Errorf("dashboard-rejected automation's tree file must be quarantined, still present at %s", treePath)
	}
	if _, err := os.Stat(treePath + ".rejected"); err != nil {
		t.Errorf("expected quarantined tree file at %s.rejected: %v", treePath, err)
	}
	rec, ok, err := ledger.Get(signature)
	if err != nil || !ok {
		t.Fatalf("ledger record missing after rejection: ok=%v err=%v", ok, err)
	}
	if rec.Status != persona.AutomationRejected {
		t.Errorf("ledger status = %q, want %q after dashboard rejection", rec.Status, persona.AutomationRejected)
	}
}

// TestHandleHITL_ApproveResumesFeedbackEscalation pins the other shared
// finalization function (persona.FinalizeFeedbackEscalation, milestone
// 2/3): approving a dashboard-surfaced FeedbackReviewEscalation request must
// resume the paused automation (AutomationFlagged -> AutomationApproved),
// exactly like the MCP bt_hitl_approve path already does.
func TestHandleHITL_ApproveResumesFeedbackEscalation(t *testing.T) {
	hitlStore, _, pStore := setupHITLFinalizationTest(t)

	const user = "nico"
	const treeID = "goal:automate_reports"
	const signature = "weekly_sales_report"

	ledger, err := persona.NewAutomationStore(pStore.Workspace(user))
	if err != nil {
		t.Fatalf("automation ledger: %v", err)
	}
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature: signature,
		Status:    persona.AutomationFlagged,
		TreeID:    treeID,
		AgentName: "auto-nico-" + signature,
	}); err != nil {
		t.Fatalf("seed flagged ledger record: %v", err)
	}

	req := hitl.NewRequest("FeedbackReviewEscalation", "FeedbackReviewEscalation", "review escalation", "", "", "please review", map[string]any{
		"user":      user,
		"signature": signature,
	})
	if err := hitlStore.Create(req); err != nil {
		t.Fatalf("create hitl request: %v", err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/hitl/"+req.ID+"/approve", bytes.NewReader([]byte(`{"reviewer":"tester"}`)))
	dashboard.HandleHITL(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: status %d body %s", rr.Code, rr.Body.String())
	}

	rec, ok, err := ledger.Get(signature)
	if err != nil || !ok {
		t.Fatalf("ledger record missing after approval: ok=%v err=%v", ok, err)
	}
	if rec.Status != persona.AutomationApproved {
		t.Errorf("ledger status = %q, want %q after dashboard approval resumes the escalation", rec.Status, persona.AutomationApproved)
	}
}
