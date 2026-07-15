package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	a2acard "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// cardAt builds a minimal AgentCard advertising a single JSON-RPC interface at
// the given URL — mirroring the shape internal/a2a.ConvertToAgentCard produces.
func cardAt(name, url string) *a2acard.AgentCard {
	return &a2acard.AgentCard{
		Name: name,
		SupportedInterfaces: []*a2acard.AgentInterface{
			a2acard.NewAgentInterface(url, a2acard.TransportProtocolJSONRPC),
		},
	}
}

// TestEndpointsFromCardsExcludesSelfAndDedupes pins the daemon's reduction of
// the live A2A card registry to remote router endpoints: cards served by this
// very node are dropped (no self-routing), a peer hosting many agent cards
// collapses to a single endpoint (one RemoteExecutor per node), and cards with
// no reachable interface are skipped. Without this the router would either route
// tasks back to itself or spin up a RemoteExecutor per advertised agent.
func TestEndpointsFromCardsExcludesSelfAndDedupes(t *testing.T) {
	self := "http://localhost:8686"
	cards := map[string]*a2acard.AgentCard{
		"self-a":   cardAt("self-a", self+"/agents/self-a"),
		"self-b":   cardAt("self-b", self+"/agents/self-b"),
		"peer1-x":  cardAt("peer1-x", "http://10.0.0.1:8686/agents/peer1-x"),
		"peer1-y":  cardAt("peer1-y", "http://10.0.0.1:8686/agents/peer1-y"),
		"peer2":    cardAt("peer2", "http://10.0.0.2:8686/agents/peer2"),
		"no-iface": {Name: "no-iface"},
		"nil-card": nil,
	}

	eps := endpointsFromCards(cards, self)

	if len(eps) != 2 {
		t.Fatalf("expected 2 peer endpoints (self excluded, peer1 deduped, no-iface skipped), got %d: %+v", len(eps), eps)
	}
	bases := map[string]bool{}
	for _, ep := range eps {
		bases[ep.BaseURL] = true
		if ep.BaseURL == self {
			t.Errorf("self base URL must never be an endpoint: %+v", ep)
		}
	}
	if !bases["http://10.0.0.1:8686"] || !bases["http://10.0.0.2:8686"] {
		t.Errorf("expected peer1 + peer2 node base URLs, got %v", bases)
	}
}

// TestEndpointsFromCardsSingleNodeYieldsNoPeers pins that a registry containing
// only this node's own cards produces zero remote endpoints, so
// NewRouterFromEndpoints falls back to the local in-process executor and
// single-node deployments behave exactly as before adopting the substrate.
func TestEndpointsFromCardsSingleNodeYieldsNoPeers(t *testing.T) {
	self := "http://localhost:8686"
	cards := map[string]*a2acard.AgentCard{
		"a": cardAt("a", self+"/agents/a"),
		"b": cardAt("b", self+"/agents/b"),
	}

	eps := endpointsFromCards(cards, self)
	if len(eps) != 0 {
		t.Fatalf("single-node registry must yield no peers, got %d: %+v", len(eps), eps)
	}

	local := reliability.NewLocalExecutor("solo", func(agentName, task string) (*reliability.AgentResult, error) {
		return &reliability.AgentResult{Agent: agentName, Task: task, Success: true, Output: "local"}, nil
	})
	router := reliability.NewRouterFromEndpoints(local, eps)
	if n := len(router.Executors()); n != 0 {
		t.Fatalf("expected router with no remote executors, got %d", n)
	}
	res, err := router.Execute("agent", "task")
	if err != nil || res.Output != "local" {
		t.Fatalf("empty router must route to local; got res=%+v err=%v", res, err)
	}
}

// TestDaemonResolvesWiredGoapFusionLoopTree pins that THE DAEMON BINARY —
// whatever its import graph looks like in the future — resolves the
// scheduled goap_fusion_loop tree with production wiring applied. Today the
// wiring arrives via internal/agentexec's init (linked through tools.go);
// if that import is ever dropped, this test fails instead of the scheduled
// loop silently running unwired again (no preflight, no circuit gate, empty
// CIRCUITPOLICY state-hash history → breaker always answers CONTINUE).
func TestDaemonResolvesWiredGoapFusionLoopTree(t *testing.T) {
	tree := resolveTree("domain:goap_fusion_loop")
	if tree == nil {
		t.Fatal("domain:goap_fusion_loop did not resolve")
	}
	// Every catalog tree root is wrapped in a ClaudeErrorHandler decorator
	// (internal/domains/trees.go wrapWithErrorHandler); the previously-root
	// wired Sequence is now tree.Children[0], so the Phase-0 preflight check
	// descends one level.
	if len(tree.Children) == 0 {
		t.Fatalf("resolved tree has no children (want ClaudeErrorHandler wrapper around the wired Sequence)")
	}
	inner := tree.Children[0]
	if len(inner.Children) == 0 || inner.Children[0].Name != "GoapFusionPreflight" {
		t.Fatalf("daemon must resolve the WIRED goap_fusion_loop tree (preflight first); first child = %q", inner.Children[0].Name)
	}
}

// TestDaemonConfiguresAuctionDelegateHook pins that THE DAEMON BINARY installs
// the auctioneer production wiring: engine.AuctionDelegateFn must be non-nil by
// the time the binary's packages are linked. The hook arrives via the same
// init-side-effect seam as the goap_fusion_loop wiring above
// (internal/agentexec, linked through tools.go), so this test fails if the
// auction wiring is ever dropped or never installed — instead of the
// AuctionDelegate action silently reporting "auction delegate not configured
// (set engine.AuctionDelegateFn)" at runtime.
func TestDaemonConfiguresAuctionDelegateHook(t *testing.T) {
	if engine.AuctionDelegateFn == nil {
		t.Fatal("daemon must configure engine.AuctionDelegateFn at startup (auctioneer production wiring); hook is nil")
	}
}

// TestDaemonWiresFeedbackPersistencePath pins that THE DAEMON BINARY resolves the
// same on-disk feedback-snapshot path the scheduler persists knowledge-graph
// feedback to. The daemon must expose feedbackSnapshotPath() and set it as
// SchedulerConfig.FeedbackPath so Fitness/RunCount/tool-edges rehydrate on
// startup instead of resetting every restart; if the helper diverges from
// agent.FeedbackFile() (the path the scheduler loads/persists), the learn→
// discover→evolve loop silently resets across restarts again.
func TestDaemonWiresFeedbackPersistencePath(t *testing.T) {
	got := feedbackSnapshotPath()
	if got == "" {
		t.Fatal("daemon must resolve a non-empty feedback-snapshot path (feedbackSnapshotPath())")
	}
	if want := agent.FeedbackFile(); got != want {
		t.Fatalf("daemon feedback-snapshot path must equal agent.FeedbackFile(); got %q, want %q", got, want)
	}
}

// TestDaemonSchedulerConfigWiresFeedbackPath pins the SchedulerConfig the daemon
// actually hands to agent.NewScheduler — not just the feedbackSnapshotPath()
// helper. The previous test (TestDaemonWiresFeedbackPersistencePath) only checks
// that the helper equals agent.FeedbackFile(); it stays green even if someone
// deletes the `FeedbackPath: feedbackSnapshotPath()` line from the scheduler
// config, silently disabling persistence. This test closes that gap by asserting
// the assembled config carries FeedbackPath (rehydration), the durable FileJobStore,
// the circuit-breaker store, and the passed-in Registry/History end-to-end.
func TestDaemonSchedulerConfigWiresFeedbackPath(t *testing.T) {
	cfg, _ := config.Load()
	if cfg == nil {
		t.Fatal("config.Load returned nil config")
	}
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	hist, err := agent.NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	scfg := buildSchedulerConfig(cfg, reg, hist, "test-revision", true)

	if want := agent.FeedbackFile(); scfg.FeedbackPath != want {
		t.Fatalf("SchedulerConfig.FeedbackPath = %q, want %q (agent.FeedbackFile()); feedback persistence disabled", scfg.FeedbackPath, want)
	}
	if scfg.FeedbackPath != feedbackSnapshotPath() {
		t.Fatalf("SchedulerConfig.FeedbackPath = %q, want feedbackSnapshotPath() %q", scfg.FeedbackPath, feedbackSnapshotPath())
	}
	if scfg.Registry != reg {
		t.Fatal("SchedulerConfig.Registry not wired from argument")
	}
	if scfg.History != hist {
		t.Fatal("SchedulerConfig.History not wired from argument")
	}
	if scfg.JobStore == nil {
		t.Fatal("SchedulerConfig.JobStore must be set (durable FileJobStore)")
	}
	if scfg.CBStore == nil {
		t.Fatal("SchedulerConfig.CBStore must be set (per-agent circuit breakers)")
	}
	if scfg.BuildRevision != "test-revision" {
		t.Fatalf("SchedulerConfig.BuildRevision = %q, want %q (deploy-drift detection disabled)", scfg.BuildRevision, "test-revision")
	}
}

// TestDaemonWiresExperienceBankPath pins that THE DAEMON BINARY resolves an
// on-disk experience-bank directory (experienceBankDir()) the same way it
// resolves the knowledge-feedback snapshot path (feedbackSnapshotPath()): a
// non-empty location rooted under agent.HomeDir(), so mutation experiences
// recorded by bt_evolve_genetic survive restarts alongside the rest of the
// platform state and honor BT_AGENT_HOME redirection in tests/deployments.
// Without this helper the ExperienceBank stays orphaned in prod — built and
// tested in internal/evolution but never handed a durable path by any binary.
func TestDaemonWiresExperienceBankPath(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	dir := experienceBankDir()
	if dir == "" {
		t.Fatal("daemon must resolve a non-empty experience-bank directory (experienceBankDir())")
	}
	if home := agent.HomeDir(); !strings.HasPrefix(dir, home) {
		t.Fatalf("experienceBankDir() = %q must live under agent.HomeDir() %q so it honors BT_AGENT_HOME and persists with platform state", dir, home)
	}
}

// TestDaemonPlumbsExperienceBankIntoMCPDeps pins — at the source level, the
// same way TestRegisterMCPToolsCommentMatchesActualToolCount audits tool
// registrations — that main() actually constructs the persistent
// ExperienceBank at experienceBankDir() and hands it to registerMCPTools via
// the mcpDeps.expBank field. The behavioral test below can only prove the
// tool uses a bank when given one; this closes the remaining gap where the
// `expBank:` line is deleted from main() and evolution silently reverts to
// the memoryless path while all tests stay green.
func TestDaemonPlumbsExperienceBankIntoMCPDeps(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "experienceBankDir()") {
		t.Error("main.go must construct the ExperienceBank at experienceBankDir(); no reference found")
	}
	if !strings.Contains(string(src), "expBank:") {
		t.Error("main.go must plumb the ExperienceBank into registerMCPTools via mcpDeps{expBank: ...}; no `expBank:` field assignment found")
	}
}

// TestBTEvolveGeneticRoutesThroughExperienceBank pins milestone 2/4 of the
// experience-grounded evolution feedback loop (Q2 Evolvability) end-to-end:
// bt_evolve_genetic must route evolution through Population.
// EvolveWithExperience against the daemon's persistent ExperienceBank —
// warm-starting from prior same-tree-type experiences — and report the bank's
// entry count and the number of warm-start retrieval hits in its JSON result.
// A deps bundle without a bank (every other test in this package passes
// &mcpDeps{}) must degrade gracefully to plain Evolve while keeping the
// result shape uniform (both counters present, zero).
func TestBTEvolveGeneticRoutesThroughExperienceBank(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	bank, err := evolution.NewExperienceBank(experienceBankDir())
	if err != nil {
		t.Fatalf("NewExperienceBank(experienceBankDir()): %v", err)
	}
	// Seed one prior success for the same base tree the tool will evolve, so
	// the warm-start retrieval (RetrieveByTreeType on the base tree's type)
	// deterministically hits it.
	baseTree := resolveTree("godev")
	if baseTree == nil {
		t.Fatal("godev tree did not resolve")
	}
	if err := bank.AddFromMutation(baseTree,
		evolution.MutationOp{Operation: "add_before", Target: "seed"},
		0.10, 0.40, nil); err != nil {
		t.Fatalf("seed AddFromMutation: %v", err)
	}
	if bank.Count() != 1 {
		t.Fatalf("seeded bank Count() = %d, want 1", bank.Count())
	}

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{expBank: bank})

	res, ok := server.Invoke("bt_evolve_genetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_genetic) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_genetic returned no content")
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_genetic result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_genetic unexpectedly returned an error: %v", out)
	}

	entries, present := out["experience_bank_entries"].(float64)
	if !present {
		t.Fatalf("bt_evolve_genetic result missing numeric \"experience_bank_entries\"; got keys %v", out)
	}
	if entries < 1 {
		t.Errorf("experience_bank_entries = %v, want >= 1 (bank was seeded with one entry)", entries)
	}
	hits, present := out["experience_retrieval_hits"].(float64)
	if !present {
		t.Fatalf("bt_evolve_genetic result missing numeric \"experience_retrieval_hits\"; got keys %v", out)
	}
	if hits < 1 {
		t.Errorf("experience_retrieval_hits = %v, want >= 1 (a same-tree-type experience was seeded; evolution did not consult the bank)", hits)
	}

	// Nil-bank degrade: the shared &mcpDeps{} shape used across this package
	// must keep working, with both counters present and zero.
	bare := engine.NewServer("bare")
	registerMCPTools(bare, &mcpDeps{})
	bres, ok := bare.Invoke("bt_evolve_genetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2}`))
	if !ok || bres == nil || len(bres.Content) == 0 {
		t.Fatal("bt_evolve_genetic must still run without an ExperienceBank (nil bank degrades to plain Evolve)")
	}
	var bout map[string]interface{}
	if err := json.Unmarshal([]byte(bres.Content[0].Text), &bout); err != nil {
		t.Fatalf("nil-bank bt_evolve_genetic result is not valid JSON: %v", err)
	}
	if _, isErr := bout["error"]; isErr {
		t.Fatalf("nil-bank bt_evolve_genetic unexpectedly returned an error: %v", bout)
	}
	if v, present := bout["experience_bank_entries"].(float64); !present || v != 0 {
		t.Errorf("nil-bank experience_bank_entries = %v (present=%v), want 0", bout["experience_bank_entries"], present)
	}
	if v, present := bout["experience_retrieval_hits"].(float64); !present || v != 0 {
		t.Errorf("nil-bank experience_retrieval_hits = %v (present=%v), want 0", bout["experience_retrieval_hits"], present)
	}
}

// TestDaemonConfiguresGoalPlanBrainstorm pins that the daemon binary installs
// the LLM plan-expansion (brainstorming) seam via internal/agentexec, so
// substantial goals get decomposed into deeper multi-task plans instead of
// one bounded task per goal.
func TestDaemonConfiguresGoalPlanBrainstorm(t *testing.T) {
	if !engine.GoalPlanBrainstormWired() {
		t.Fatal("daemon must wire engine.WireGoalPlanBrainstorm() at startup; plan-expansion seam is nil")
	}
}

// TestAttemptOutcomeErrorCarriesOutputTail pins the scheduler retry closure's
// no-runErr failure path: when RunOnce returns a non-success outcome with a nil
// runErr, the attempt error the retry policy sees (and that lands in the DLQ
// Error field on exhaustion) must carry the run-output detail via
// agent.OutcomeErrorDetail, not the bare "agent outcome: %s" string. Without
// this, retry-exhaustion dead-letters record only the outcome word and lose the
// output tail that says *why* the agent failed.
func TestAttemptOutcomeErrorCarriesOutputTail(t *testing.T) {
	// (a) non-empty output: both the outcome and the flattened output tail
	// must appear, and the bare format must be gone.
	got := attemptOutcomeError("failure", "step A ok\nstep B: exit 1").Error()
	if !strings.Contains(got, "agent outcome: failure") {
		t.Errorf("error must name the outcome; got %q", got)
	}
	if !strings.Contains(got, "step B: exit 1") {
		t.Errorf("error must carry the output tail; got %q", got)
	}
	if !strings.Contains(got, "step A ok | step B: exit 1") {
		t.Errorf("output newlines must be flattened with %q; got %q", " | ", got)
	}
	if got == "agent outcome: failure" {
		t.Errorf("bare format must be gone; got exactly the pre-fix string %q", got)
	}

	// (b) empty output: OutcomeErrorDetail's no-output sentinel must surface.
	gotEmpty := attemptOutcomeError("failure", "").Error()
	if !strings.Contains(gotEmpty, "no run output") {
		t.Errorf("empty output must yield the no-run-output sentinel; got %q", gotEmpty)
	}

	// (c) >400-byte output: only the tail is retained, so the error must end
	// with the output's last line.
	lastLine := "FINAL: retry exhausted here"
	big := strings.Repeat("noise line filler xxxxxxxxxxxxxxxxxxxx\n", 20) + lastLine
	if len(big) <= 400 {
		t.Fatalf("test fixture must exceed 400 bytes to exercise tail retention; got %d", len(big))
	}
	gotBig := attemptOutcomeError("timeout", big).Error()
	if !strings.HasSuffix(gotBig, lastLine) {
		t.Errorf("error must end with the output's last line (tail retention); got %q", gotBig)
	}
}
