package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

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
