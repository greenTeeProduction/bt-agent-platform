package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// ---- test isolation: never touch the real ~/.go-bt-evolve -----------------

// TestMain isolates BT_AGENT_HOME to a throwaway temp directory for every test
// in this package, so persistent-auctioneer tests (e.g.
// TestAuctionDelegate_AwardsWinnerFromCardRegistry) that drive AuctionDelegate
// through a real NewPersistentAuctioneer never flush winner circuit breaker
// state into the developer's real ~/.go-bt-evolve/circuit_breakers.json.
func TestMain(m *testing.M) {
	origHome, hadHome := os.LookupEnv("BT_AGENT_HOME")
	dir, err := os.MkdirTemp("", "a2a-test-home-")
	if err != nil {
		panic("a2a TestMain: MkdirTemp: " + err.Error())
	}
	os.Setenv("BT_AGENT_HOME", dir)

	code := m.Run()

	os.RemoveAll(dir)
	if hadHome {
		os.Setenv("BT_AGENT_HOME", origHome)
	} else {
		os.Unsetenv("BT_AGENT_HOME")
	}
	os.Exit(code)
}

// TestAuctionPersistentTestsIsolateHomeDir guards against real ~/.go-bt-evolve
// pollution: several tests in this file (e.g.
// TestAuctionDelegate_AwardsWinnerFromCardRegistry) drive AuctionDelegate,
// which always builds a NewPersistentAuctioneer and flushes winner circuit
// breaker state to agent.CircuitBreakersFile(). Without isolating
// BT_AGENT_HOME for the whole package (e.g. via a package TestMain), those
// flushes land in the developer's real home directory — confirmed on this
// machine, whose real circuit_breakers.json carries leaked
// "a2a.auction.winner.cheap"/"override"/"reviewer" entries from prior test
// runs — instead of a throwaway test directory.
func TestAuctionPersistentTestsIsolateHomeDir(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	realDataDir := filepath.Join(realHome, ".go-bt-evolve")

	got := agent.CircuitBreakersFile()
	if strings.HasPrefix(got, realDataDir) {
		t.Fatalf("agent.CircuitBreakersFile() = %q resolves under the real home directory %q; "+
			"this package's tests must isolate BT_AGENT_HOME (e.g. via a package TestMain) so "+
			"persistent-auctioneer tests never write real winner circuit-breaker state", got, realDataDir)
	}
}

// ---- message-type contract ------------------------------------------------

func TestAuctionMessageKinds(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1"}
	bid := Bid{TaskID: "t1", BidderName: "a"}
	award := Award{TaskID: "t1", WinnerName: "a"}

	if ann.Kind() != KindAnnouncement {
		t.Errorf("announcement kind = %q, want %q", ann.Kind(), KindAnnouncement)
	}
	if bid.Kind() != KindBid {
		t.Errorf("bid kind = %q, want %q", bid.Kind(), KindBid)
	}
	if award.Kind() != KindAward {
		t.Errorf("award kind = %q, want %q", award.Kind(), KindAward)
	}
}

func TestTaskAnnouncement_Validate(t *testing.T) {
	valid := TaskAnnouncement{
		TaskID:        "t1",
		Description:   "review a PR",
		RequiredTags:  []string{"domain", "code"},
		MinConfidence: 0.5,
		Deadline:      time.Now().Add(time.Hour),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid announcement rejected: %v", err)
	}

	if err := (TaskAnnouncement{TaskID: ""}).Validate(); err == nil {
		t.Error("expected error for empty TaskID")
	}
	if err := (TaskAnnouncement{TaskID: "t1", MinConfidence: 1.5}).Validate(); err == nil {
		t.Error("expected error for MinConfidence > 1")
	}
	if err := (TaskAnnouncement{TaskID: "t1", MinConfidence: -0.1}).Validate(); err == nil {
		t.Error("expected error for negative MinConfidence")
	}
}

func TestBid_Validate(t *testing.T) {
	valid := Bid{TaskID: "t1", BidderName: "agent-a", Cost: 10, Confidence: 0.8}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid bid rejected: %v", err)
	}

	if err := (Bid{TaskID: "", BidderName: "a", Confidence: 0.5}).Validate(); err == nil {
		t.Error("expected error for empty TaskID")
	}
	if err := (Bid{TaskID: "t1", BidderName: "", Confidence: 0.5}).Validate(); err == nil {
		t.Error("expected error for empty BidderName")
	}
	if err := (Bid{TaskID: "t1", BidderName: "a", Cost: -1, Confidence: 0.5}).Validate(); err == nil {
		t.Error("expected error for negative Cost")
	}
	if err := (Bid{TaskID: "t1", BidderName: "a", Confidence: 1.1}).Validate(); err == nil {
		t.Error("expected error for Confidence > 1")
	}
}

// ---- bid-evaluation contract ----------------------------------------------

func TestScoreEvaluator_PicksLowestCost(t *testing.T) {
	var ev BidEvaluator = ScoreEvaluator{}
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	bids := []Bid{
		{TaskID: "t1", BidderName: "expensive", Cost: 30, Confidence: 0.9},
		{TaskID: "t1", BidderName: "cheap", Cost: 10, Confidence: 0.7},
		{TaskID: "t1", BidderName: "mid", Cost: 20, Confidence: 0.8},
	}

	award, err := ev.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if award.TaskID != "t1" {
		t.Errorf("award TaskID = %q, want t1", award.TaskID)
	}
	if award.WinnerName != "cheap" {
		t.Errorf("winner = %q, want cheap (lowest cost above min confidence)", award.WinnerName)
	}
	if award.WinningBid.BidderName != "cheap" {
		t.Errorf("winning bid bidder = %q, want cheap", award.WinningBid.BidderName)
	}
}

func TestScoreEvaluator_RejectsBelowMinConfidence(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.75}
	bids := []Bid{
		{TaskID: "t1", BidderName: "cheap-unsure", Cost: 5, Confidence: 0.5},
		{TaskID: "t1", BidderName: "pricey-sure", Cost: 25, Confidence: 0.9},
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if award.WinnerName != "pricey-sure" {
		t.Errorf("winner = %q, want pricey-sure (cheap bid was below min confidence)", award.WinnerName)
	}
}

func TestScoreEvaluator_TieBreakByConfidence(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	bids := []Bid{
		{TaskID: "t1", BidderName: "less-sure", Cost: 10, Confidence: 0.6},
		{TaskID: "t1", BidderName: "more-sure", Cost: 10, Confidence: 0.9},
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if award.WinnerName != "more-sure" {
		t.Errorf("winner = %q, want more-sure (equal cost, higher confidence wins)", award.WinnerName)
	}
}

func TestScoreEvaluator_IgnoresForeignTaskBids(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	bids := []Bid{
		{TaskID: "other", BidderName: "wrong-task", Cost: 1, Confidence: 0.9},
		{TaskID: "t1", BidderName: "right-task", Cost: 15, Confidence: 0.8},
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if award.WinnerName != "right-task" {
		t.Errorf("winner = %q, want right-task (foreign-task bid must be ignored)", award.WinnerName)
	}
}

func TestScoreEvaluator_NoEligibleBids(t *testing.T) {
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.9}
	bids := []Bid{
		{TaskID: "t1", BidderName: "unsure", Cost: 5, Confidence: 0.4},
	}

	_, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err == nil {
		t.Fatal("expected error when no bid meets min confidence")
	}
	if !errors.Is(err, ErrNoEligibleBids) {
		t.Errorf("error = %v, want ErrNoEligibleBids", err)
	}
}

// ---- card.go bridge: which agents may bid on an announcement ---------------

func TestEligibleBidders(t *testing.T) {
	cards := map[string]*a2a.AgentCard{
		"reviewer": {
			Name: "reviewer",
			Skills: []a2a.AgentSkill{{
				ID:   "domain:code_review",
				Tags: []string{"domain", "code", "review"},
			}},
		},
		"researcher": {
			Name: "researcher",
			Skills: []a2a.AgentSkill{{
				ID:   "research:deep_research",
				Tags: []string{"research", "deep", "research"},
			}},
		},
	}

	ann := TaskAnnouncement{TaskID: "t1", RequiredTags: []string{"domain", "code"}}
	got := EligibleBidders(cards, ann)

	if len(got) != 1 || got[0] != "reviewer" {
		t.Fatalf("EligibleBidders = %v, want [reviewer]", got)
	}
}

func TestEligibleBidders_NoRequiredTagsMatchesAll(t *testing.T) {
	cards := map[string]*a2a.AgentCard{
		"a": {Name: "a", Skills: []a2a.AgentSkill{{Tags: []string{"x"}}}},
		"b": {Name: "b", Skills: []a2a.AgentSkill{{Tags: []string{"y"}}}},
	}

	got := EligibleBidders(cards, TaskAnnouncement{TaskID: "t1"})
	if len(got) != 2 {
		t.Fatalf("EligibleBidders with no required tags = %v, want both agents", got)
	}
	// result must be deterministic (sorted) for stable auctions
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("EligibleBidders = %v, want sorted [a b]", got)
	}
}

// ---- control-plane: announce → evaluate → dispatch end to end ---------------

// auctionTransport is a BidCollector that plays both auction roles for each
// candidate URL. It answers the announcement fan-out (a JSON-encoded
// TaskAnnouncement) with a canned bid, and answers the follow-up dispatch of the
// real task text to the winner with a canned execution result. It tells the two
// calls apart by whether the delivered text parses as a TaskAnnouncement, and
// records exactly which URLs were dispatched the real task so a test can assert
// the loser was never invoked.
type auctionTransport struct {
	mu         sync.Mutex
	bids       map[string]string // agentURL -> bid JSON returned during collection
	results    map[string]string // agentURL -> execution result returned on dispatch
	dispatched map[string]string // agentURL -> task text delivered on dispatch
}

func newAuctionTransport() *auctionTransport {
	return &auctionTransport{
		bids:       map[string]string{},
		results:    map[string]string{},
		dispatched: map[string]string{},
	}
}

func (f *auctionTransport) SendTask(_ context.Context, agentURL, taskText string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var probe TaskAnnouncement
	if err := json.Unmarshal([]byte(taskText), &probe); err == nil && probe.TaskID != "" {
		return f.bids[agentURL], nil // announcement fan-out: return this candidate's bid
	}

	// task dispatch to the winner: record it and return the execution result.
	f.dispatched[agentURL] = taskText
	return f.results[agentURL], nil
}

// RunAuction must compose the three existing stages: fan the announcement out
// via CollectBids, pick the winner with a ScoreEvaluator, then dispatch the real
// task text to the winning agent's URL and return that result alongside the
// Award. Before this wiring existed a picked Award was never acted on.
func TestAuctioneer_RunAuction_DispatchesToWinnerAndReturnsResult(t *testing.T) {
	ft := newAuctionTransport()
	ann := TaskAnnouncement{TaskID: "t1", Description: "review PR #42", MinConfidence: 0.5}

	// Both candidates bid; 'cheap' wins on lowest cost above min confidence.
	ft.bids["http://cheap"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "cheap", Cost: 10, Confidence: 0.8})
	ft.bids["http://pricey"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "pricey", Cost: 30, Confidence: 0.9})
	// Only the winner should be handed the real task and produce a result.
	ft.results["http://cheap"] = "done: reviewed PR #42"
	ft.results["http://pricey"] = "loser must not be dispatched"

	auc := NewAuctioneer(ft)
	candidates := map[string]string{"cheap": "http://cheap", "pricey": "http://pricey"}

	res, err := auc.RunAuction(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("RunAuction failed: %v", err)
	}

	// The award must name the lowest-cost eligible bidder.
	if res.Award.WinnerName != "cheap" {
		t.Errorf("award winner = %q, want cheap", res.Award.WinnerName)
	}
	if res.Award.TaskID != "t1" {
		t.Errorf("award TaskID = %q, want t1", res.Award.TaskID)
	}
	// The winner's execution result must be returned to the caller.
	if res.Result != "done: reviewed PR #42" {
		t.Errorf("result = %q, want the winner's execution result", res.Result)
	}
	// The real task text (not the announcement JSON) must be dispatched to the
	// winner, and only the winner.
	if got := ft.dispatched["http://cheap"]; got != ann.Description {
		t.Errorf("dispatched task text to winner = %q, want %q", got, ann.Description)
	}
	if _, dispatched := ft.dispatched["http://pricey"]; dispatched {
		t.Error("loser was dispatched the task; only the winner should receive the real task text")
	}
}

// ---- production delegate: engine.AuctionDelegateFn wiring -------------------

// withAuctionSeams temporarily installs a fake card source and collector for the
// production AuctionDelegate, restoring both when the test ends.
func withAuctionSeams(t *testing.T, cards map[string]*a2a.AgentCard, collector BidCollector) {
	t.Helper()
	origCards, origCollector := AuctionCardsFn, newAuctionCollector
	AuctionCardsFn = func() map[string]*a2a.AgentCard { return cards }
	newAuctionCollector = func() BidCollector { return collector }
	t.Cleanup(func() {
		AuctionCardsFn = origCards
		newAuctionCollector = origCollector
	})
}

// cardWithURL builds a minimal AgentCard advertising one skill (with the given
// tags) reachable at url — the shape auctionCandidates derives its map from.
func cardWithURL(name, url string, tags ...string) *a2a.AgentCard {
	return &a2a.AgentCard{
		Name:                name,
		Skills:              []a2a.AgentSkill{{Tags: tags}},
		SupportedInterfaces: []*a2a.AgentInterface{{URL: url}},
	}
}

// AuctionDelegate must build its candidate map from the live card registry, run
// the auction over the real-transport seam, and return the winner's result with
// awarded=true.
func TestAuctionDelegate_AwardsWinnerFromCardRegistry(t *testing.T) {
	ft := newAuctionTransport()
	ft.bids["http://cheap"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "cheap", Cost: 10, Confidence: 0.9})
	ft.bids["http://pricey"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "pricey", Cost: 30, Confidence: 0.9})
	ft.results["http://cheap"] = "done by cheap"

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"cheap":  cardWithURL("cheap", "http://cheap", "domain"),
		"pricey": cardWithURL("pricey", "http://pricey", "domain"),
	}, ft)

	result, awarded, err := AuctionDelegate("do the work", nil)
	if err != nil {
		t.Fatalf("AuctionDelegate errored: %v", err)
	}
	if !awarded {
		t.Fatal("expected awarded=true when an eligible bidder wins")
	}
	if result != "done by cheap" {
		t.Errorf("result = %q, want the winner's execution result", result)
	}
	// The winner must be dispatched the real task text, not the announcement JSON.
	if got := ft.dispatched["http://cheap"]; got != "do the work" {
		t.Errorf("winner dispatched %q, want the real task text", got)
	}
}

// An "auction_candidates" chainState override must fully replace the derived
// candidate set, so the auction runs even with no live card registry.
func TestAuctionDelegate_ChainStateCandidateOverride(t *testing.T) {
	ft := newAuctionTransport()
	ft.bids["http://override"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "override", Cost: 1, Confidence: 0.9})
	ft.results["http://override"] = "override won"

	withAuctionSeams(t, nil, ft) // no card registry: override is the only source

	chainState := map[string]any{
		"auction_candidates": map[string]string{"override": "http://override"},
	}
	result, awarded, err := AuctionDelegate("task", chainState)
	if err != nil {
		t.Fatalf("AuctionDelegate errored: %v", err)
	}
	if !awarded || result != "override won" {
		t.Fatalf("override candidate did not win: awarded=%v result=%q", awarded, result)
	}
}

// chainState "auction_required_tags" must narrow candidate selection to agents
// whose cards cover those tags, excluding others from the auction entirely.
func TestAuctionDelegate_RequiredTagsFilterCandidates(t *testing.T) {
	ft := newAuctionTransport()
	// Only the reviewer covers the "code" tag; the researcher must be excluded,
	// so its bid (cheaper) must never be collected or win.
	ft.bids["http://reviewer"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "reviewer", Cost: 20, Confidence: 0.9})
	ft.bids["http://researcher"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "researcher", Cost: 1, Confidence: 0.9})
	ft.results["http://reviewer"] = "reviewed"

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"reviewer":   cardWithURL("reviewer", "http://reviewer", "domain", "code", "review"),
		"researcher": cardWithURL("researcher", "http://researcher", "research", "deep"),
	}, ft)

	chainState := map[string]any{"auction_required_tags": []string{"code"}}
	result, awarded, err := AuctionDelegate("review this", chainState)
	if err != nil {
		t.Fatalf("AuctionDelegate errored: %v", err)
	}
	if !awarded || result != "reviewed" {
		t.Fatalf("required-tag filter did not restrict to reviewer: awarded=%v result=%q", awarded, result)
	}
	if _, dispatched := ft.dispatched["http://researcher"]; dispatched {
		t.Error("researcher was excluded by required tags but still received the task")
	}
}

// With no candidate source and no override, AuctionDelegate reports awarded=false
// (and no error) so the AuctionDelegate action falls back to its delegate tree.
func TestAuctionDelegate_NoCandidatesFallsBack(t *testing.T) {
	origCards := AuctionCardsFn
	AuctionCardsFn = nil
	t.Cleanup(func() { AuctionCardsFn = origCards })

	result, awarded, err := AuctionDelegate("task", nil)
	if err != nil {
		t.Fatalf("AuctionDelegate errored: %v", err)
	}
	if awarded || result != "" {
		t.Errorf("expected no award when no candidates exist, got awarded=%v result=%q", awarded, result)
	}
}

// When candidates exist but no bid is eligible to win, AuctionDelegate must
// swallow ErrNoEligibleBids into awarded=false (fall back), not surface an error.
func TestAuctionDelegate_NoEligibleBidsFallsBack(t *testing.T) {
	ft := newAuctionTransport()
	// Bid sits below the announcement's min confidence, so nothing is eligible.
	ft.bids["http://a"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "a", Cost: 5, Confidence: 0.4})

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"a": cardWithURL("a", "http://a", "domain"),
	}, ft)

	chainState := map[string]any{"auction_min_confidence": 0.9}
	result, awarded, err := AuctionDelegate("task", chainState)
	if err != nil {
		t.Fatalf("AuctionDelegate should not error on no eligible bids: %v", err)
	}
	if awarded || result != "" {
		t.Errorf("expected fall-back (awarded=false) when no bid is eligible, got awarded=%v result=%q", awarded, result)
	}
}

// ---- milestone 3: consume the announcement deadline & guard empty task text --

// deadlineProbeTransport records, per candidate URL, whether that candidate's
// SendTask observed a context deadline and what that deadline was. It always
// answers with the same canned bid so a test can assert CollectBids equips each
// per-candidate fan-out call with a deadline derived from the announcement — the
// mechanism that stops one hung candidate from stalling the whole auction.
type deadlineProbeTransport struct {
	mu       sync.Mutex
	bid      string
	hasDL    map[string]bool
	deadline map[string]time.Time
}

func newDeadlineProbeTransport(bid string) *deadlineProbeTransport {
	return &deadlineProbeTransport{
		bid:      bid,
		hasDL:    map[string]bool{},
		deadline: map[string]time.Time{},
	}
}

func (f *deadlineProbeTransport) SendTask(ctx context.Context, agentURL, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dl, ok := ctx.Deadline()
	f.hasDL[agentURL] = ok
	f.deadline[agentURL] = dl
	return f.bid, nil
}

// CollectBids must derive a per-candidate context deadline from the
// announcement's Deadline, so a candidate that hangs is bounded by the auction's
// deadline rather than blocking the fan-out forever.
func TestCollectBids_DerivesPerCandidateDeadlineFromAnnouncement(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	ft := newDeadlineProbeTransport(bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}))
	ann := TaskAnnouncement{TaskID: "t1", Description: "work", Deadline: deadline}

	auc := NewAuctioneer(ft)
	if _, err := auc.CollectBids(context.Background(), ann, map[string]string{"a": "http://a"}); err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}

	if !ft.hasDL["http://a"] {
		t.Fatal("expected the per-candidate context to carry a deadline derived from the announcement")
	}
	// The derived deadline must track the announcement's deadline (an hour out),
	// not some unrelated default — allow only microsecond computation skew.
	if diff := ft.deadline["http://a"].Sub(deadline); diff > time.Second || diff < -time.Second {
		t.Errorf("derived deadline %v not within 1s of announcement deadline %v", ft.deadline["http://a"], deadline)
	}
}

// When the announcement carries no deadline (zero value), CollectBids must still
// apply a default per-candidate deadline so an unbounded announcement cannot let
// a hung candidate stall the auction.
func TestCollectBids_AppliesDefaultDeadlineWhenAnnouncementHasNone(t *testing.T) {
	before := time.Now()
	ft := newDeadlineProbeTransport(bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}))
	ann := TaskAnnouncement{TaskID: "t1", Description: "work"} // zero Deadline

	auc := NewAuctioneer(ft)
	if _, err := auc.CollectBids(context.Background(), ann, map[string]string{"a": "http://a"}); err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}

	if !ft.hasDL["http://a"] {
		t.Fatal("expected a default per-candidate deadline when the announcement has none")
	}
	if got := ft.deadline["http://a"]; !got.After(before) {
		t.Errorf("default deadline %v is not in the future", got)
	}
}

// panicBidTransport is a BidCollector whose SendTask panics for a configured
// set of candidate URLs (simulating a bug while unmarshaling or attributing an
// untrusted candidate's bid response) and returns a canned, well-formed bid for
// every other candidate.
type panicBidTransport struct {
	panicURLs map[string]bool
	bid       string
}

func (f *panicBidTransport) SendTask(_ context.Context, agentURL, _ string) (string, error) {
	if f.panicURLs[agentURL] {
		panic("boom: candidate transport panicked while handling SendTask")
	}
	return f.bid, nil
}

// CollectBids must isolate each candidate's fan-out goroutine with panic
// recovery so a panic while unmarshaling or attributing one untrusted
// candidate's bid response cannot take down the whole auctioneer process —
// the other, well-behaved candidates' bids must still come back.
func TestCollectBids_SurvivesPanickingCandidate(t *testing.T) {
	ft := &panicBidTransport{
		panicURLs: map[string]bool{"http://evil": true},
		bid:       bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}),
	}
	ann := TaskAnnouncement{TaskID: "t1", Description: "work"}

	auc := NewAuctioneer(ft)
	bids, err := auc.CollectBids(context.Background(), ann, map[string]string{
		"good": "http://good",
		"evil": "http://evil",
	})
	if err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}
	if len(bids) != 1 || bids[0].BidderName != "good" {
		t.Fatalf("bids = %+v, want exactly the well-behaved candidate's bid", bids)
	}
}

// RunAuction must reject an announcement whose Description is empty before it
// would dispatch that empty text to the winning agent as the real task.
func TestAuctioneer_RunAuction_RejectsEmptyDescription(t *testing.T) {
	ft := newAuctionTransport()
	ann := TaskAnnouncement{TaskID: "t1", Description: "", MinConfidence: 0.5}
	ft.bids["http://a"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "a", Cost: 1, Confidence: 0.9})
	ft.results["http://a"] = "winner must not be dispatched an empty task"

	auc := NewAuctioneer(ft)
	_, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"})
	if err == nil {
		t.Fatal("expected RunAuction to reject an announcement with an empty Description")
	}
	if len(ft.dispatched) != 0 {
		t.Errorf("no task should be dispatched for an empty Description, dispatched=%v", ft.dispatched)
	}
}

// ---- milestone 4: bound the winning-agent dispatch with a deadline ---------

// dispatchDeadlineTransport plays both auction roles for a single candidate. It
// answers the announcement fan-out (text that parses as a TaskAnnouncement) with
// a canned bid so a winner is picked, and — for the follow-up winner dispatch of
// the real task text — records whether SendTask observed a context deadline and
// what that deadline was. It lets a test assert RunAuction bounds the winning
// dispatch with a deadline instead of forwarding the raw caller context straight
// through to SendTask.
type dispatchDeadlineTransport struct {
	mu            sync.Mutex
	bid           string
	dispatched    bool
	dispatchHasDL bool
	dispatchDL    time.Time
}

func (f *dispatchDeadlineTransport) SendTask(ctx context.Context, _, taskText string) (string, error) {
	var probe TaskAnnouncement
	if err := json.Unmarshal([]byte(taskText), &probe); err == nil && probe.TaskID != "" {
		return f.bid, nil // announcement fan-out: return this candidate's bid
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatched = true
	dl, ok := ctx.Deadline()
	f.dispatchHasDL = ok
	f.dispatchDL = dl
	return "done", nil
}

// RunAuction must bound the winning-agent dispatch with a deadline derived from
// the announcement's Deadline, so a winner that hangs is bounded by the auction's
// deadline rather than blocking on the raw caller context indefinitely.
func TestAuctioneer_RunAuction_BoundsWinnerDispatchWithAnnouncementDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Hour)
	ft := &dispatchDeadlineTransport{bid: bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9})}
	ann := TaskAnnouncement{TaskID: "t1", Description: "review PR #42", MinConfidence: 0.5, Deadline: deadline}

	auc := NewAuctioneer(ft)
	if _, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"}); err != nil {
		t.Fatalf("RunAuction failed: %v", err)
	}

	if !ft.dispatched {
		t.Fatal("winner was never dispatched")
	}
	if !ft.dispatchHasDL {
		t.Fatal("expected the winner-dispatch context to carry a deadline, not the raw caller context")
	}
	// The derived deadline must track the announcement's deadline (an hour out),
	// not some unrelated value — allow only sub-second computation skew.
	if diff := ft.dispatchDL.Sub(deadline); diff > time.Second || diff < -time.Second {
		t.Errorf("dispatch deadline %v not within 1s of announcement deadline %v", ft.dispatchDL, deadline)
	}
}

// When the announcement carries no deadline (zero value), RunAuction must still
// apply a default dispatch deadline so an unbounded announcement cannot let a
// hung winner block the caller forever.
func TestAuctioneer_RunAuction_AppliesDefaultDispatchDeadline(t *testing.T) {
	before := time.Now()
	ft := &dispatchDeadlineTransport{bid: bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9})}
	ann := TaskAnnouncement{TaskID: "t1", Description: "work", MinConfidence: 0.5} // zero Deadline

	auc := NewAuctioneer(ft)
	if _, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"}); err != nil {
		t.Fatalf("RunAuction failed: %v", err)
	}

	if !ft.dispatched {
		t.Fatal("winner was never dispatched")
	}
	if !ft.dispatchHasDL {
		t.Fatal("expected a default dispatch deadline when the announcement has none")
	}
	if !ft.dispatchDL.After(before) {
		t.Errorf("default dispatch deadline %v is not in the future", ft.dispatchDL)
	}
}

func TestAuctioneer_RunAuction_NoEligibleBidsDispatchesNothing(t *testing.T) {
	ft := newAuctionTransport()
	ann := TaskAnnouncement{TaskID: "t1", Description: "review PR", MinConfidence: 0.9}
	// Sole bid is below the announcement's min confidence, so nothing is eligible.
	ft.bids["http://a"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "a", Cost: 5, Confidence: 0.4})

	auc := NewAuctioneer(ft)
	_, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"})
	if err == nil {
		t.Fatal("expected an error when no bid is eligible to win")
	}
	if !errors.Is(err, ErrNoEligibleBids) {
		t.Errorf("error = %v, want ErrNoEligibleBids", err)
	}
	if len(ft.dispatched) != 0 {
		t.Errorf("no task should be dispatched when no bid wins, dispatched=%v", ft.dispatched)
	}
}

// ---- milestone 5: thread AuctionResult.Award through AuctionDelegate -------

// AuctionDelegate must thread the winning AuctionResult.Award back through
// chainState so a caller whose (result, awarded, err) return only carries a
// bare result string — like the engine.AuctionDelegateFn call site — can still
// attribute the run to the actual winning agent (e.g. for a follow-up
// History.Record call) instead of losing the Award entirely.
func TestAuctionDelegate_ThreadsAwardIntoChainState(t *testing.T) {
	ft := newAuctionTransport()
	ft.bids["http://cheap"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "cheap", Cost: 10, Confidence: 0.9})
	ft.results["http://cheap"] = "done by cheap"

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"cheap": cardWithURL("cheap", "http://cheap", "domain"),
	}, ft)

	// A nil chainState (the zero value most tree runs pass — confirmed a real
	// input by internal/engine's TestEvaluateGuardCondition_NilChainState) must
	// not panic when AuctionDelegate tries to write the Award back: writing
	// into a nil map panics, so the implementation must guard this path.
	if result, awarded, err := AuctionDelegate("do the work", nil); err != nil || !awarded || result != "done by cheap" {
		t.Fatalf("AuctionDelegate with nil chainState = (%q, %v, %v), want (\"done by cheap\", true, nil)", result, awarded, err)
	}

	chainState := map[string]any{}
	result, awarded, err := AuctionDelegate("do the work", chainState)
	if err != nil {
		t.Fatalf("AuctionDelegate errored: %v", err)
	}
	if !awarded || result != "done by cheap" {
		t.Fatalf("expected an award: awarded=%v result=%q", awarded, result)
	}

	award, ok := chainState["auction_award"].(Award)
	if !ok {
		t.Fatalf("chainState[%q] = %#v, want an Award", "auction_award", chainState["auction_award"])
	}
	if award.WinnerName != "cheap" {
		t.Errorf("award.WinnerName = %q, want %q", award.WinnerName, "cheap")
	}
	if award.WinningBid.Cost != 10 {
		t.Errorf("award.WinningBid.Cost = %v, want 10", award.WinningBid.Cost)
	}
}

// ---- widened fallback: circuit-breaker-open / retry-exhausted dispatch -----

// dispatchFailTransport plays the announcement-fan-out role normally (a canned
// bid) but always fails the follow-up winner dispatch with dispatchErr, so a
// test can drive RunAuction's winner-dispatch retry policy to exhaustion.
type dispatchFailTransport struct {
	bid         string
	dispatchErr error
}

func (f *dispatchFailTransport) SendTask(_ context.Context, _, taskText string) (string, error) {
	var probe TaskAnnouncement
	if err := json.Unmarshal([]byte(taskText), &probe); err == nil && probe.TaskID != "" {
		return f.bid, nil // announcement fan-out: return the canned bid
	}
	return "", f.dispatchErr // winner dispatch: always fails
}

// AuctionDelegate must widen its fallback condition beyond ErrNoEligibleBids:
// when the winner dispatch exhausts its retries on a transient (retryable)
// error, that must also report awarded=false with a nil error — so the
// AuctionDelegate engine action falls back to its delegate tree instead of
// hard-failing the whole run — not surface the dispatch error to the caller.
func TestAuctionDelegate_DispatchRetryExhaustedFallsBack(t *testing.T) {
	ft := &dispatchFailTransport{
		bid:         bidJSON(t, Bid{TaskID: "auction", BidderName: "flakywinner", Cost: 10, Confidence: 0.9}),
		dispatchErr: errors.New("connection refused"), // classifies as retryable ErrCatNetwork
	}

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"flakywinner": cardWithURL("flakywinner", "http://flakywinner", "domain"),
	}, ft)

	result, awarded, err := AuctionDelegate("do the work", nil)
	if err != nil {
		t.Fatalf("AuctionDelegate errored instead of falling back: %v", err)
	}
	if awarded || result != "" {
		t.Errorf("expected fallback (awarded=false, result=\"\") when dispatch retries are exhausted, got awarded=%v result=%q", awarded, result)
	}
}

// AuctionDelegate must also widen its fallback condition to a winner whose
// circuit breaker is already open: RunAuction refuses to dispatch at all in
// that case, and that refusal must report awarded=false with a nil error, not
// a hard error, so the caller falls back to its delegate tree instead of
// treating an open breaker as a fatal auction failure.
func TestAuctionDelegate_WinnerCircuitBreakerOpenFallsBack(t *testing.T) {
	// Force the winner's shared, persistent circuit breaker open by recording
	// enough consecutive failures directly — the same store AuctionDelegate's
	// NewPersistentAuctioneer consults via winnerBreaker.
	breaker := winnerBreakers.get("breakerwinner")
	for i := 0; i < winnerCircuitBreakerThreshold; i++ {
		breaker.RecordFailure()
	}

	ft := newAuctionTransport()
	ft.bids["http://breakerwinner"] = bidJSON(t, Bid{TaskID: "auction", BidderName: "breakerwinner", Cost: 10, Confidence: 0.9})
	ft.results["http://breakerwinner"] = "should never be dispatched"

	withAuctionSeams(t, map[string]*a2a.AgentCard{
		"breakerwinner": cardWithURL("breakerwinner", "http://breakerwinner", "domain"),
	}, ft)

	result, awarded, err := AuctionDelegate("do the work", nil)
	if err != nil {
		t.Fatalf("AuctionDelegate errored instead of falling back: %v", err)
	}
	if awarded || result != "" {
		t.Errorf("expected fallback (awarded=false, result=\"\") when the winner's circuit breaker is open, got awarded=%v result=%q", awarded, result)
	}
	if _, dispatched := ft.dispatched["http://breakerwinner"]; dispatched {
		t.Error("winner should not be dispatched while its circuit breaker is open")
	}
}

// ---- winner breaker persistence: one owner, one lock ----------------------

// TestWinnerBreakerStore_LoadRestoresPersistedStateVerbatim pins that winner
// breaker state is RESTORED from what is on disk rather than RE-DERIVED by
// replaying the persisted failure count through fresh RecordFailure calls.
// Replay stamps lastFailureTime with the restart time, so the next save
// rewrites every restored winner's last_failure to "when this process booted" —
// corrupting the very field internal/dashboard's loadCircuitBreakers renders in
// its cb_status column, on every restart with any tripped winner. Routing the
// load through internal/agent's store (the single owner of this file, which
// restores via RestoreState) keeps the real failure timestamp.
func TestWinnerBreakerStore_LoadRestoresPersistedStateVerbatim(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	path := agent.CircuitBreakersFile()

	const lastFailure = "2026-07-17T12:00:00Z"
	key := winnerBreakerKeyPrefix + "known-bad-winner"
	blob := `{"breakers":{"` + key + `":{"status":"open","failures":4,"last_failure":"` + lastFailure + `"}}}`
	if err := os.WriteFile(path, []byte(blob), 0644); err != nil {
		t.Fatal(err)
	}

	store := &winnerBreakerStore{} // fresh store: the load happens on first get
	cb := store.get("known-bad-winner")
	if cb.State() != reliability.CircuitOpen {
		t.Fatalf("restored winner breaker state = %v, want open", cb.State())
	}
	if got := cb.LastFailureTime().UTC().Format(time.RFC3339); got != lastFailure {
		t.Errorf("restored last failure time = %q, want %q — replaying RecordFailure re-stamps it with the restart time instead of restoring it", got, lastFailure)
	}

	if err := store.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Breakers map[string]struct {
			Status      string `json:"status"`
			Failures    int    `json:"failures"`
			LastFailure string `json:"last_failure"`
		} `json:"breakers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse saved file: %v\n%s", err, data)
	}
	entry, ok := file.Breakers[key]
	if !ok {
		t.Fatalf("winner key %q missing after save: %+v", key, file.Breakers)
	}
	if entry.LastFailure != lastFailure {
		t.Errorf("persisted last_failure = %q, want %q preserved across a restart round trip", entry.LastFailure, lastFailure)
	}
	if entry.Status != "open" || entry.Failures != 4 {
		t.Errorf("persisted entry = %+v, want status=open failures=4", entry)
	}
}

// TestWinnerBreakerStore_SaveLocksReadModifyWrite pins that winner breaker
// persistence goes through the same locked single owner the scheduler uses:
// the read-merge-write cycle over the shared circuit_breakers.json must be
// serialized against other processes by the advisory `<path>.lock` sidecar.
// A hand-rolled save that only renames atomically still reads the file before
// a concurrent daemon/dashboard writer commits, and its rename then drops that
// writer's entry.
func TestWinnerBreakerStore_SaveLocksReadModifyWrite(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	path := agent.CircuitBreakersFile()
	if err := os.WriteFile(path, []byte(`{"breakers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	store := &winnerBreakerStore{}
	store.get("flaky-winner").RecordFailure() // load now, before the lock is held

	// Stand in for the daemon scheduler mid read-modify-write on the same file.
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		t.Fatalf("AcquireFileLock: %v", err)
	}
	defer release() // safe to call twice; released explicitly below

	done := make(chan error, 1)
	go func() { done <- store.save() }()

	select {
	case saveErr := <-done:
		t.Fatalf("save returned (err=%v) while another process held %s.lock; winner breaker persistence must go through the locked single owner", saveErr, path)
	case <-time.After(250 * time.Millisecond):
		// Still blocked, as required.
	}

	scheduler := `{"breakers":{"scheduler-agent":{"status":"open","failures":9,"last_failure":"2026-07-17T12:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(scheduler), 0644); err != nil {
		t.Fatal(err)
	}
	release()

	select {
	case saveErr := <-done:
		if saveErr != nil {
			t.Fatalf("save: %v", saveErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("save never completed after the advisory file lock was released")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Breakers map[string]struct {
			Status   string `json:"status"`
			Failures int    `json:"failures"`
		} `json:"breakers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse saved file: %v\n%s", err, data)
	}
	if entry, ok := file.Breakers["scheduler-agent"]; !ok || entry.Failures != 9 {
		t.Errorf("scheduler entry = %+v (present=%v), want failures=9 preserved — the winner save read the file before taking the lock and clobbered a concurrent writer", entry, ok)
	}
	if _, ok := file.Breakers[winnerBreakerKeyPrefix+"flaky-winner"]; !ok {
		t.Errorf("winner entry missing from saved file: %+v", file.Breakers)
	}
}
