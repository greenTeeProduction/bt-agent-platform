package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
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
// treeTags): "domain:code_review" advertises tags domain/code/review.
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
// through ctx exactly as the per-agent endpoint's interceptor delivers it — and
// returns every yielded event.
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
	// The agent's tree tags (domain, code, review) cover the announcement's
	// RequiredTags, so it should bid rather than run any tree.
	exec := executorForAgent(t, "coder", "domain:code_review")

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}
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
	// The agent's tree tags (research, deep) cannot cover the announcement, so it
	// must decline silently — not mis-execute the announcement as a task and fail.
	exec := executorForAgent(t, "researcher", "research:deep_research")

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}, MinConfidence: 0.3}
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
