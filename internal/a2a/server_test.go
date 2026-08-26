package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	btcore "github.com/rvitorper/go-bt/core"
)

// ---- bidder-side evaluation: an agent scores an announced task from its own
// agent card capabilities and produces the bid it would submit -----------------
//
// This is the candidate side of the auction and the inverse of EligibleBidders:
// where the auctioneer filters cards by RequiredTags, the bidder scores its own
// card against the announcement to decide whether — and how confidently — to bid.

func skillCard(name string, tags ...string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:   name,
		Skills: []a2a.AgentSkill{{ID: name, Tags: tags}},
	}
}

func TestScoreAnnouncement_SpecialistBidsCheaperAndSurer(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}

	// A card whose every capability is demanded by the task: fully focused.
	specialist, ok := ScoreAnnouncement("specialist", skillCard("specialist", "domain", "code"), ann)
	if !ok {
		t.Fatal("specialist covering all required tags should bid")
	}
	if err := specialist.Validate(); err != nil {
		t.Fatalf("specialist produced an invalid bid: %v", err)
	}
	if specialist.TaskID != "t1" || specialist.BidderName != "specialist" {
		t.Errorf("bid = %+v, want TaskID t1 / BidderName specialist", specialist)
	}
	if specialist.Confidence != 1.0 {
		t.Errorf("specialist confidence = %v, want 1.0 (all card tags demanded)", specialist.Confidence)
	}
	if specialist.Cost != 0 {
		t.Errorf("specialist cost = %v, want 0 (no irrelevant capability)", specialist.Cost)
	}

	// A card that covers the task but carries three extra, irrelevant skills:
	// less focused, so it bids with lower confidence and higher cost.
	generalist, ok := ScoreAnnouncement("generalist",
		skillCard("generalist", "domain", "code", "review", "research", "deep"), ann)
	if !ok {
		t.Fatal("generalist covering all required tags should bid")
	}
	if generalist.Confidence >= specialist.Confidence {
		t.Errorf("generalist confidence %v should be below specialist %v",
			generalist.Confidence, specialist.Confidence)
	}
	if generalist.Cost <= specialist.Cost {
		t.Errorf("generalist cost %v should exceed specialist %v",
			generalist.Cost, specialist.Cost)
	}
	// 2 of 5 distinct card tags are demanded → confidence 0.4, cost 3.
	if generalist.Confidence != 0.4 {
		t.Errorf("generalist confidence = %v, want 0.4 (2 of 5 tags demanded)", generalist.Confidence)
	}
	if generalist.Cost != 3 {
		t.Errorf("generalist cost = %v, want 3 (3 irrelevant tags)", generalist.Cost)
	}
}

func TestScoreAnnouncement_DeclinesWhenTagsNotCovered(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}

	// Card lacks the "code" capability → cannot perform the task → no bid.
	if _, ok := ScoreAnnouncement("researcher", skillCard("researcher", "research", "deep"), ann); ok {
		t.Error("agent whose card does not cover required tags must not bid")
	}
}

func TestScoreAnnouncement_DeclinesBelowMinConfidence(t *testing.T) {
	// Announcement demands high focus; a generalist covers the tags but is too
	// diluted to clear MinConfidence, so it declines rather than submitting a
	// bid the auctioneer would reject anyway.
	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.5}

	if _, ok := ScoreAnnouncement("generalist",
		skillCard("generalist", "domain", "code", "review", "research", "deep"), ann); ok {
		t.Error("agent whose confidence is below MinConfidence must not bid")
	}
}

func TestScoreAnnouncement_NoRequiredTagsBidsFully(t *testing.T) {
	// An announcement with no required tags is open to every agent; a bidder
	// treats it as a perfect fit (confidence 1, no overhead cost).
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.3}

	bid, ok := ScoreAnnouncement("any", skillCard("any", "x", "y"), ann)
	if !ok {
		t.Fatal("agent should bid on an untagged (open) announcement")
	}
	if bid.Confidence != 1.0 {
		t.Errorf("open-announcement confidence = %v, want 1.0", bid.Confidence)
	}
	if bid.Cost != 0 {
		t.Errorf("open-announcement cost = %v, want 0", bid.Cost)
	}
}

// ---- server-side responder: the candidate endpoint replies to an auctioneer's
// announcement task text with its JSON-encoded bid ----------------------------

func TestRespondToAnnouncement_ProducesEvaluableBid(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}
	taskText, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	resp, ok := RespondToAnnouncement("specialist", skillCard("specialist", "domain", "code"), string(taskText))
	if !ok {
		t.Fatal("specialist should respond with a bid")
	}

	var bid Bid
	if err := json.Unmarshal([]byte(resp), &bid); err != nil {
		t.Fatalf("response was not a JSON bid: %v (resp=%q)", err, resp)
	}
	if err := bid.Validate(); err != nil {
		t.Fatalf("responded bid is invalid: %v", err)
	}
	if bid.BidderName != "specialist" || bid.TaskID != "t1" {
		t.Errorf("bid = %+v, want BidderName specialist / TaskID t1", bid)
	}
}

func TestRespondToAnnouncement_NonAnnouncementYieldsNoBid(t *testing.T) {
	card := skillCard("specialist", "domain", "code")

	if _, ok := RespondToAnnouncement("specialist", card, ""); ok {
		t.Error("empty task text must not yield a bid")
	}
	if _, ok := RespondToAnnouncement("specialist", card, "not json"); ok {
		t.Error("non-JSON task text must not yield a bid")
	}
	// Structurally valid JSON but not an announcement (no TaskID).
	if _, ok := RespondToAnnouncement("specialist", card, `{"description":"hi"}`); ok {
		t.Error("JSON that is not a valid announcement must not yield a bid")
	}
}

// ---- round trip: bidder-produced bids are consumable by the auctioneer -------

// cardResponder is a BidCollector that drives each candidate's server-side
// bidder evaluation, closing the auction loop end to end: announcement → per
// agent RespondToAnnouncement → collected bids → award.
type cardResponder struct {
	cards map[string]*a2a.AgentCard // keyed by agent URL (== bidder name in this test)
}

func (c cardResponder) SendTask(_ context.Context, agentURL, taskText string) (string, error) {
	resp, ok := RespondToAnnouncement(agentURL, c.cards[agentURL], taskText)
	if !ok {
		return "", nil // agent declined
	}
	return resp, nil
}

func TestBidderAuctioneerRoundTrip(t *testing.T) {
	cards := map[string]*a2a.AgentCard{
		"specialist": skillCard("specialist", "domain", "code"),
		"generalist": skillCard("generalist", "domain", "code", "review", "research", "deep"),
		"researcher": skillCard("researcher", "research", "deep"), // cannot cover the task
	}
	candidates := map[string]string{
		"specialist": "specialist",
		"generalist": "generalist",
		"researcher": "researcher",
	}

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}

	auctioneer := NewAuctioneer(cardResponder{cards: cards})
	bids, err := auctioneer.CollectBids(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}

	// specialist + generalist bid; researcher (no coverage) is silent.
	if len(bids) != 2 {
		t.Fatalf("collected %d bids, want 2 (researcher should decline): %+v", len(bids), bids)
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if award.WinnerName != "specialist" {
		t.Errorf("winner = %q, want specialist (fully focused → cheapest bid)", award.WinnerName)
	}
}

// ---- server-side executor is bid-aware: an announcement delivered to a real
// per-agent A2A endpoint must yield the agent's Bid instead of being mis-run as
// literal task text against its behavior tree ---------------------------------
//
// This closes the auction end to end: Auctioneer.CollectBids fans a
// JSON-encoded TaskAnnouncement out over the A2A transport, which lands in
// BTAgentExecutor.Execute as the incoming message text. If Execute treats that
// JSON as a task and runs a tree over it (the pre-fix behavior), no production
// bt-agent server can ever answer with a well-formed Bid and the whole fan-out
// is inert. Execute must instead recognize the announcement, score the agent's
// eligibility from its card (tree tags), and emit the Bid as a completed-task
// artifact — the shape BTAgentClient.SendTask extracts and CollectBids consumes.

// executorForAgent builds a BTAgentExecutor backed by a registry holding a
// single agent whose tree ID determines its auction capabilities (via
// treeTags, sourced from knowledge.GlobalGraph): "domain:code_review"
// advertises tags review_code/detect_bugs/suggest_improvements/audit_security.
func executorForAgent(t *testing.T, name, tree string) *BTAgentExecutor {
	t.Helper()
	// resolveTreeByID is a package global; TestSetTreeResolver leaves a resolver
	// behind that captures a finished test's *testing.T. Pin a benign nil-returning
	// resolver so any tree-path fallthrough here fails cleanly ("no tree") instead
	// of panicking on a stale t.Errorf.
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: name, Tree: tree, Description: "test agent"}); err != nil {
		t.Fatalf("Create agent %q: %v", name, err)
	}
	return &BTAgentExecutor{Reg: reg}
}

// drainExecute runs Execute to completion — with the target agent name carried
// through ctx exactly as handleAgentEndpoint delivers it — and returns every
// yielded event.
func drainExecute(t *testing.T, exec *BTAgentExecutor, agentName, taskText string) []a2a.Event {
	t.Helper()
	ctx := context.WithValue(context.Background(), agentNameKey{}, agentName)
	execCtx := &a2asrv.ExecutorContext{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(taskText)),
	}
	var events []a2a.Event
	for ev, err := range exec.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Execute yielded error: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

// bidArtifact scans the event stream for an artifact whose text decodes to a
// valid Bid — the exact payload BTAgentClient.SendTask pulls off a completed
// task and hands back to the auctioneer.
func bidArtifact(events []a2a.Event) (Bid, bool) {
	for _, ev := range events {
		art, ok := ev.(*a2a.TaskArtifactUpdateEvent)
		if !ok || art.Artifact == nil {
			continue
		}
		for _, part := range art.Artifact.Parts {
			text := part.Text()
			if text == "" {
				continue
			}
			var bid Bid
			if json.Unmarshal([]byte(text), &bid) == nil && bid.Validate() == nil {
				return bid, true
			}
		}
	}
	return Bid{}, false
}

// terminalState returns the last task state the executor transitioned to.
func terminalState(events []a2a.Event) a2a.TaskState {
	state := a2a.TaskState("")
	for _, ev := range events {
		if su, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
			state = su.Status.State
		}
	}
	return state
}

// eventKinds renders the event stream's concrete types for failure diagnostics.
func eventKinds(events []a2a.Event) string {
	kinds := make([]string, len(events))
	for i, ev := range events {
		kinds[i] = fmt.Sprintf("%T", ev)
	}
	return fmt.Sprintf("%v", kinds)
}

func TestExecute_AnnouncementYieldsBidNotTreeRun(t *testing.T) {
	// The agent's tree tags (its knowledge-graph capability actions, e.g.
	// review_code) cover the announcement's RequiredTags, so it should bid
	// rather than run any tree.
	exec := executorForAgent(t, "coder", "domain:code_review")

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"review_code", "detect_bugs"}, MinConfidence: 0.3}
	payload, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	events := drainExecute(t, exec, "coder", string(payload))

	bid, ok := bidArtifact(events)
	if !ok {
		t.Fatalf("announcement to an eligible agent must yield a Bid artifact, "+
			"but Execute ran the JSON as a task; events=%s", eventKinds(events))
	}
	if bid.BidderName != "coder" || bid.TaskID != "t1" {
		t.Errorf("bid = %+v, want BidderName coder / TaskID t1", bid)
	}
	// The bid must land on a completed task — BTAgentClient.SendTask only reads
	// artifacts off a TaskStateCompleted task.
	if got := terminalState(events); got != a2a.TaskStateCompleted {
		t.Errorf("terminal state = %q, want completed (bid delivered, tree never run)", got)
	}
}

func TestExecute_IneligibleAnnouncementDeclinesWithoutRunningTree(t *testing.T) {
	// The agent's tree tags (research capability actions, e.g. conduct_research)
	// cannot cover the announcement's review_code requirement, so it must
	// decline silently — not mis-execute the announcement as a task and fail.
	exec := executorForAgent(t, "researcher", "research:deep_research")

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"review_code"}, MinConfidence: 0.3}
	payload, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	events := drainExecute(t, exec, "researcher", string(payload))

	if _, ok := bidArtifact(events); ok {
		t.Error("an ineligible agent must not emit a Bid")
	}
	// A recognized announcement is answered as an auction message (a silent
	// decline), so the task must not end Failed the way a tree run over the JSON
	// blob would.
	if got := terminalState(events); got == a2a.TaskStateFailed {
		t.Errorf("terminal state = failed — the announcement was mis-run as a task instead of declined")
	}
}

func TestExecute_AnnouncementScoresAgainstCardCacheNotFreshConversion(t *testing.T) {
	// The agent's tree tags (research, deep) do NOT cover the announcement's
	// RequiredTags, so a card freshly derived from the tree definition via
	// ConvertToAgentCard would decline. But Server.CardCache holds a different,
	// already-signed card for this agent that DOES cover the required tags.
	// Execute must score the bid-aware branch against the CardCache entry —
	// the same card the A2A server actually advertises and signs — instead of
	// re-deriving (and re-signing) a fresh one from the tree definition on
	// every inbound request.
	exec := executorForAgent(t, "researcher", "research:deep_research")
	exec.CardCache = map[string]*a2a.AgentCard{
		"researcher": skillCard("researcher", "domain", "code"),
	}

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}
	payload, err := json.Marshal(ann)
	if err != nil {
		t.Fatalf("marshal announcement: %v", err)
	}

	events := drainExecute(t, exec, "researcher", string(payload))

	bid, ok := bidArtifact(events)
	if !ok {
		t.Fatalf("Execute must score the announcement against Server.CardCache's card, "+
			"not a freshly converted one from the tree definition; events=%s", eventKinds(events))
	}
	if bid.BidderName != "researcher" || bid.TaskID != "t1" {
		t.Errorf("bid = %+v, want BidderName researcher / TaskID t1", bid)
	}
}

func TestExecute_NonAnnouncementStillRunsTree(t *testing.T) {
	// Plain task text is not an announcement and must keep flowing to the tree
	// path — the bid detection must not swallow ordinary tasks. With no tree
	// registered for the agent, that path fails with "no tree", proving Execute
	// attempted execution rather than treating the text as an auction message.
	exec := executorForAgent(t, "coder", "domain:code_review")

	events := drainExecute(t, exec, "coder", "please review this pull request")

	if _, ok := bidArtifact(events); ok {
		t.Error("plain task text must not be answered with a Bid")
	}
	if got := terminalState(events); got != a2a.TaskStateFailed {
		t.Errorf("terminal state = %q, want failed (ordinary task routed to the tree path)", got)
	}
}

// ---- Execute must leave a History trace, just like Cancel already does ------
//
// runJob/RunTaskResult (the scheduler- and dashboard-driven run paths) both
// call agent.History.Record after every run via agent.RunAgent. Execute is a
// third, independent run path — tasks delivered over A2A, direct or as an
// auction winner — and today it runs engine.RunTask (server.go) but never
// touches e.History, so those runs are invisible to `bt-agent-cli agent
// history` and the dashboard's per-agent run list even though the field
// exists and Cancel already populates it for cancelled tasks. Execute must
// record a RunRecord with the agent name, task text, and the outcome/elapsed
// it already computes right before discarding them.

func TestExecute_RecordsRunToHistory(t *testing.T) {
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "coder", Tree: "domain:code_review", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	hist, err := agent.NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	// AlwaysSucceed (not MarkSuccessful) — MarkSuccessful gates on output
	// quality and requires a pre-populated bb.Result, which this bare tree
	// never produces; AlwaysSucceed is the codebase's standard no-content
	// success leaf (see internal/engine/tree.go) and keeps this test focused
	// on History recording rather than quality-gate semantics.
	tree := &evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	exec := &BTAgentExecutor{
		Reg:     reg,
		History: hist,
		TreeMap: map[string]*evolution.SerializableNode{"coder": tree},
	}

	drainExecute(t, exec, "coder", "please review this pull request")

	runs := hist.List("coder", 0)
	if len(runs) != 1 {
		t.Fatalf("History has %d runs for agent %q after Execute, want 1: %+v", len(runs), "coder", runs)
	}
	if runs[0].Outcome != "success" {
		t.Errorf("recorded outcome = %q, want %q", runs[0].Outcome, "success")
	}
	if runs[0].Task != "please review this pull request" {
		t.Errorf("recorded task = %q, want the original task text", runs[0].Task)
	}
}

// ---- outcome handling must route through TaskStateBridge, not a binary
// success/fail check ------------------------------------------------------
//
// Execute today only special-cases bb.Outcome == "success"; every other
// outcome — including "pending_approval", a real non-terminal HITL wait —
// falls into the generic failure branch and is reported TaskStateFailed.
// That is wrong: a task awaiting human approval has not failed, and A2A
// already has a state for exactly this (TaskStateInputRequired). Execute
// must consult TaskStateBridge.BTToA2A(bb.Outcome) instead of the binary
// check so pending_approval (and any future non-success/non-failure
// outcome the bridge knows how to map) surfaces correctly to the caller.

// ---- runtime card refresh: agents created after the server starts must
// become reachable over A2A/auctions without a process restart -------------
//
// Server.CardCache is built once in NewServer via BuildCardRegistry and never
// revisited afterward. An agent created later — via bt_agent_create or
// autopilot's activateAutomation, both of which call reg.Create against the
// same live *agent.Registry the server was built from — is invisible to the
// per-agent endpoint, the global agent card, and AuctionCardSource's
// candidate pool until the process restarts and NewServer runs again.
// RefreshCards must rebuild CardCache from the live registry and keep the
// executor's mirrored copy (used to score auction bids) in sync.

func TestServer_RefreshCards_PicksUpAgentCreatedAfterStartup(t *testing.T) {
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "existing", Tree: "domain:code_review", Description: "seed agent"}); err != nil {
		t.Fatalf("Create existing agent: %v", err)
	}

	srv, err := NewServer(reg, nil, 0, "http://localhost:0")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if _, ok := srv.CardCache["existing"]; !ok {
		t.Fatalf("CardCache missing seed agent at startup: %+v", srv.CardCache)
	}

	// Simulate an agent created at runtime, after the server (and its
	// one-shot CardCache snapshot) already exist.
	if _, err := reg.Create(agent.Definition{Name: "newcomer", Tree: "domain:code_review", Description: "created after startup"}); err != nil {
		t.Fatalf("Create newcomer agent: %v", err)
	}
	if _, ok := srv.CardCache["newcomer"]; ok {
		t.Fatal("test setup invalid: newcomer already present before refresh")
	}

	if err := srv.RefreshCards(); err != nil {
		t.Fatalf("RefreshCards: %v", err)
	}

	if _, ok := srv.CardCache["newcomer"]; !ok {
		t.Errorf("Server.CardCache missing agent created after startup even after RefreshCards; got %v", srv.CardCache)
	}
	if _, ok := srv.Executor.CardCache["newcomer"]; !ok {
		t.Errorf("Executor.CardCache (auction-bid scoring path) missing agent created after startup even after RefreshCards; got %v", srv.Executor.CardCache)
	}

	// Production auctions draw candidates from this closure — it must
	// reflect the refreshed registry too, not just the raw field.
	if _, ok := srv.AuctionCardSource()()["newcomer"]; !ok {
		t.Error("AuctionCardSource() candidate pool missing agent created after startup even after RefreshCards")
	}
}

// ---- CardCache needs mutex protection: RefreshCards races with every
// concurrent reader ---------------------------------------------------------
//
// RefreshCards (called from bt_agent_create and autopilot's
// activateAutomation, per the doc comment above) reassigns Server.CardCache
// and Executor.CardCache with no synchronization at all. Meanwhile every
// inbound HTTP request reads those same fields concurrently: the per-agent
// endpoint routing table, the global agent card aggregation, the health
// check counter, and the auction-bid scoring branch of Execute. Under
// go test -race, a goroutine calling RefreshCards concurrently with
// goroutines reading srv.CardCache / srv.Executor.CardCache must not trip
// the race detector — it currently does, because neither field is guarded
// by a mutex.
func TestServer_RefreshCards_ConcurrentWithReaders_NoRace(t *testing.T) {
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "seed", Tree: "domain:code_review", Description: "seed agent"}); err != nil {
		t.Fatalf("Create seed agent: %v", err)
	}

	srv, err := NewServer(reg, nil, 0, "http://localhost:0")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: repeatedly rebuilds CardCache, as production code does after
	// every registry mutation.
	wg.Go(func() {
		for range 50 {
			if err := srv.RefreshCards(); err != nil {
				t.Errorf("RefreshCards: %v", err)
				return
			}
		}
		close(done)
	})

	// Readers: exercise every path that touches CardCache without holding
	// any lock — HTTP handlers and the auction-bid scoring branch of Execute.
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
			}

			rec := httptest.NewRecorder()
			srv.handleAgentEndpoint(rec, httptest.NewRequest(http.MethodGet, "/agents/seed", nil))

			rec2 := httptest.NewRecorder()
			srv.handleGlobalAgentCard(rec2, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))

			rec3 := httptest.NewRecorder()
			srv.handleHealth(rec3, httptest.NewRequest(http.MethodGet, "/health", nil))

			_ = srv.AuctionCardSource()()
		}
	})

	// Second reader: the auction-bid scoring branch inside Execute reads
	// e.CardCache directly (server.go:100), independent of the HTTP handlers.
	wg.Go(func() {
		announcement := `{"kind":"task_announcement","task_id":"t1","required_tags":["domain"],"min_confidence":0}`
		for {
			select {
			case <-done:
				return
			default:
			}

			execCtx := &a2asrv.ExecutorContext{ContextID: "seed", Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(announcement))}
			for _, err := range srv.Executor.Execute(context.Background(), execCtx) {
				if err != nil {
					t.Errorf("Execute: %v", err)
				}
			}
		}
	})

	wg.Wait()
}

// ---- handleAgentEndpoint must reuse one shared task store across requests --
//
// handleAgentEndpoint currently builds a brand new a2asrv.NewHandler(...) —
// and therefore a brand new in-memory task store — on every incoming HTTP
// request. A task created by one request (SendMessage) is invisible to the
// very next request against the same per-agent endpoint (GetTask), because
// each request gets its own throwaway store. This breaks any multi-request
// interaction with a task: polling GetTask, resubscribing, or sending a
// follow-up message that references an existing TaskID.

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      any    `json:"id"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func postAgentRPC(t *testing.T, srv *Server, agentName, method string, params any) rpcResponse {
	t.Helper()
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		t.Fatalf("marshal %s request: %v", method, err)
	}

	req := httptest.NewRequest(http.MethodPost, "/agents/"+agentName, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleAgentEndpoint(w, req)

	var resp rpcResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal %s response %s: %v", method, w.Body.String(), err)
	}
	return resp
}

func TestHandleAgentEndpoint_ReusesSharedTaskStoreAcrossRequests(t *testing.T) {
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "coder", Tree: "domain:code_review", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	srv, err := NewServer(reg, nil, 0, "http://localhost:0")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// First request: submit a task. There is no tree registered for "coder"
	// (SetTreeResolver above returns nil), so Execute fails fast — but not
	// before yielding a Task in TaskStateSubmitted, so the task is created
	// and persisted before the terminal Failed event is returned here.
	sendResp := postAgentRPC(t, srv, "coder", "SendMessage", &a2a.SendMessageRequest{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello")),
	})
	if sendResp.Error != nil {
		t.Fatalf("SendMessage rpc error: %s", sendResp.Error.Message)
	}
	var sendResult struct {
		Task *a2a.Task `json:"task"`
	}
	if err := json.Unmarshal(sendResp.Result, &sendResult); err != nil || sendResult.Task == nil || sendResult.Task.ID == "" {
		t.Fatalf("SendMessage result missing task: %s (unmarshal err: %v)", sendResp.Result, err)
	}

	// Second request, same agent endpoint, separate *http.Request: fetch the
	// task the first request just created.
	getResp := postAgentRPC(t, srv, "coder", "GetTask", &a2a.GetTaskRequest{ID: sendResult.Task.ID})
	if getResp.Error != nil {
		t.Fatalf("GetTask on a follow-up request to the same agent endpoint failed: %s — "+
			"handleAgentEndpoint must reuse a single shared A2A request handler/task store "+
			"instead of constructing a brand-new a2asrv.NewHandler(...) on every request",
			getResp.Error.Message)
	}
	var gotTask a2a.Task
	if err := json.Unmarshal(getResp.Result, &gotTask); err != nil {
		t.Fatalf("unmarshal GetTask result: %v (%s)", err, getResp.Result)
	}
	if gotTask.ID != sendResult.Task.ID {
		t.Errorf("GetTask returned task ID %q, want %q", gotTask.ID, sendResult.Task.ID)
	}
}

// ---- Cancel must leave a History trace, not vanish silently -----------------
//
// Execute records every run outcome to the platform's shared History store
// (internal/agent/history.go) so `bt-agent-cli agent history` and the
// dashboard's per-agent run list show what happened. Cancel bypasses that
// chokepoint entirely: it discards its ctx parameter (`_ context.Context`)
// and never touches History, so a task cancelled mid-run leaves no trace —
// it just disappears from the caller's perspective while History shows the
// agent never ran. Cancel must resolve the agent name the same way Execute
// does (ctx.Value(agentNameKey{}) falling back to execCtx.ContextID) and
// record a RunRecord with Outcome "cancelled".

func TestCancel_RecordsCancelledOutcomeToHistory(t *testing.T) {
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "coder", Tree: "domain:code_review", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	hist, err := agent.NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	exec := &BTAgentExecutor{Reg: reg, History: hist}

	// Mirrors how handleAgentEndpoint carries the target agent name via ctx
	// (agentNameKey), which is how Execute learns it — Cancel must use the
	// identical resolution instead of discarding ctx.
	ctx := context.WithValue(context.Background(), agentNameKey{}, "coder")
	execCtx := &a2asrv.ExecutorContext{}

	for _, err := range exec.Cancel(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Cancel yielded error: %v", err)
		}
	}

	runs := hist.List("coder", 0)
	if len(runs) != 1 {
		t.Fatalf("History has %d runs for agent %q after Cancel, want 1: %+v", len(runs), "coder", runs)
	}
	if runs[0].Outcome != "cancelled" {
		t.Errorf("recorded outcome = %q, want %q", runs[0].Outcome, "cancelled")
	}
}

func TestExecute_PendingApprovalRoutesThroughBridge(t *testing.T) {
	dir := t.TempDir()
	if _, err := hitl.InitStore(filepath.Join(dir, "hitl")); err != nil {
		t.Fatalf("InitStore: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour})
	defer hitl.SetPolicy(hitl.DefaultPolicy())

	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "gatekept", Tree: "gatekept", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	tree := &evolution.SerializableNode{
		Type:     "HumanApprovalGate",
		Name:     "Gate",
		Metadata: map[string]any{"prompt": "confirm risky action"},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	exec := &BTAgentExecutor{
		Reg:     reg,
		TreeMap: map[string]*evolution.SerializableNode{"gatekept": tree},
	}

	events := drainExecute(t, exec, "gatekept", "please do the risky thing")

	if got := terminalState(events); got != a2a.TaskStateInputRequired {
		t.Errorf("terminal state = %q, want input-required — pending_approval must route through "+
			"TaskStateBridge instead of the binary success/fail check; events=%s", got, eventKinds(events))
	}
}

// TestFailureEventMessage_CarryoverKeepsSentinel pins the cross-boundary
// carryover contract: the failure message the A2A server yields for a
// rate-limit-carryover run must contain the sentinel outcome even when
// bb.Result carries a human-readable message, because the delegating caller
// (internal/engine's DelegateToA2A node) detects the sentinel in the error
// text to classify the delegation as a healthy deferred pause instead of a
// hard failure.
func TestFailureEventMessage_CarryoverKeepsSentinel(t *testing.T) {
	msg := failureEventMessage("goap:fusion", "goap_fusion_rate_limited",
		"## GOAP Superpowers Rate Limited\n\nClaude rate-limit backoff active.", 3*time.Second)
	if !strings.Contains(msg, "goap_fusion_rate_limited") {
		t.Fatalf("failure message %q must contain the carryover sentinel for caller-side classification", msg)
	}

	// A plain failure with a result keeps the result untouched.
	plain := failureEventMessage("domain:x", "failure", "it broke", time.Second)
	if plain != "it broke" {
		t.Fatalf("plain failure message = %q, want the raw result", plain)
	}

	// No result: the default formatted message names tree and outcome.
	def := failureEventMessage("domain:x", "failure", "", time.Second)
	if !strings.Contains(def, "domain:x") || !strings.Contains(def, "failure") {
		t.Fatalf("default message %q must name the tree and outcome", def)
	}
}

// ---- an auction-won task's History entry must attribute to the winning
// bidder, not the agent that merely hosted the delegating tree --------------
//
// AuctionDelegate (internal/a2a/auction.go) writes the winning Award into
// bb.ChainState["auction_award"] when a production tree delegates a subtask
// through an auction (RunAuction dispatches only the real work to the winner,
// never the losing candidates). Execute's e.History.Record call today always
// attributes the run to agentName — the agent whose endpoint Execute is
// running under — even when the tree it ran was really just a thin auction
// wrapper whose real work was done by a different, winning bidder. Execute
// must read bb.ChainState["auction_award"] after engine.RunTask returns and,
// when an Award with a non-empty WinnerName is present, record the History
// entry under that winner's name instead.

func TestExecute_AuctionAwardAttributesHistoryToWinner(t *testing.T) {
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "executor", Tree: "domain:code_review", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	hist, err := agent.NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	// Stands in for a production tree that delegated its work through an
	// auction (AuctionDelegate), leaving the winning Award in ChainState for
	// the caller to attribute the run to.
	engine.RegisterAction("TestWriteAuctionAward", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		bb.ChainState["auction_award"] = Award{
			TaskID:     "t1",
			WinnerName: "winner-bot",
			WinningBid: Bid{TaskID: "t1", BidderName: "winner-bot", Confidence: 1},
		}
		return 1
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestWriteAuctionAward"},
			{Type: "AlwaysSucceed"},
		},
	}

	exec := &BTAgentExecutor{
		Reg:     reg,
		History: hist,
		TreeMap: map[string]*evolution.SerializableNode{"executor": tree},
	}

	drainExecute(t, exec, "executor", "please handle this task")

	winnerRuns := hist.List("winner-bot", 0)
	if len(winnerRuns) != 1 {
		t.Fatalf("History has %d runs for auction winner %q, want 1: %+v", len(winnerRuns), "winner-bot", winnerRuns)
	}
	if winnerRuns[0].AgentName != "winner-bot" {
		t.Errorf("recorded AgentName = %q, want the auction winner %q", winnerRuns[0].AgentName, "winner-bot")
	}

	executorRuns := hist.List("executor", 0)
	if len(executorRuns) != 0 {
		t.Errorf("History has %d runs for the executing agent %q, want 0 — the auction-won run "+
			"must attribute to the winning bidder, not the executor: %+v", len(executorRuns), "executor", executorRuns)
	}
}
