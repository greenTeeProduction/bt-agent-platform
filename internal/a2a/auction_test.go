package a2a

import (
	"errors"
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
