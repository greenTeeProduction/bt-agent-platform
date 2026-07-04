package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// MessageKind discriminates the three auction message types exchanged during
// task allocation: announcement → bid → award.
type MessageKind string

const (
	// KindAnnouncement labels a TaskAnnouncement broadcast by an auctioneer.
	KindAnnouncement MessageKind = "task_announcement"
	// KindBid labels a Bid submitted by a candidate agent.
	KindBid MessageKind = "bid"
	// KindAward labels an Award naming the winning bidder.
	KindAward MessageKind = "award"
)

// ErrNoEligibleBids is returned by a BidEvaluator when no submitted bid is
// eligible to win the announced task (wrong task, or below min confidence).
var ErrNoEligibleBids = errors.New("a2a: no eligible bids for announced task")

// TaskAnnouncement is broadcast to candidate agents to open an auction for a
// single unit of work.
type TaskAnnouncement struct {
	TaskID        string    `json:"task_id"`
	Description   string    `json:"description,omitempty"`
	RequiredTags  []string  `json:"required_tags,omitempty"`
	MinConfidence float64   `json:"min_confidence,omitempty"`
	Deadline      time.Time `json:"deadline,omitempty"`
}

// Kind identifies this message as a task announcement.
func (TaskAnnouncement) Kind() MessageKind { return KindAnnouncement }

// Validate reports whether the announcement is well-formed.
func (a TaskAnnouncement) Validate() error {
	if a.TaskID == "" {
		return fmt.Errorf("a2a: announcement TaskID is required")
	}
	if a.MinConfidence < 0 || a.MinConfidence > 1 {
		return fmt.Errorf("a2a: announcement MinConfidence %v out of range [0,1]", a.MinConfidence)
	}
	return nil
}

// Bid is a candidate agent's offer to perform an announced task.
type Bid struct {
	TaskID     string  `json:"task_id"`
	BidderName string  `json:"bidder_name"`
	Cost       float64 `json:"cost"`
	Confidence float64 `json:"confidence"`
}

// Kind identifies this message as a bid.
func (Bid) Kind() MessageKind { return KindBid }

// Validate reports whether the bid is well-formed.
func (b Bid) Validate() error {
	if b.TaskID == "" {
		return fmt.Errorf("a2a: bid TaskID is required")
	}
	if b.BidderName == "" {
		return fmt.Errorf("a2a: bid BidderName is required")
	}
	if b.Cost < 0 {
		return fmt.Errorf("a2a: bid Cost %v must be non-negative", b.Cost)
	}
	if b.Confidence < 0 || b.Confidence > 1 {
		return fmt.Errorf("a2a: bid Confidence %v out of range [0,1]", b.Confidence)
	}
	return nil
}

// Award names the winning bidder for an announced task.
type Award struct {
	TaskID     string `json:"task_id"`
	WinnerName string `json:"winner_name"`
	WinningBid Bid    `json:"winning_bid"`
}

// Kind identifies this message as an award.
func (Award) Kind() MessageKind { return KindAward }

// BidEvaluator selects the winning bid for an announced task.
type BidEvaluator interface {
	Evaluate(ann TaskAnnouncement, bids []Bid) (Award, error)
}

// ScoreEvaluator picks the eligible bid with the lowest cost, breaking ties in
// favor of higher confidence. A bid is eligible when it targets the announced
// task and meets the announcement's minimum confidence.
type ScoreEvaluator struct{}

// Evaluate implements BidEvaluator.
func (ScoreEvaluator) Evaluate(ann TaskAnnouncement, bids []Bid) (Award, error) {
	var winner *Bid
	for i := range bids {
		b := bids[i]
		if b.TaskID != ann.TaskID {
			continue // foreign-task bid
		}
		if b.Confidence < ann.MinConfidence {
			continue // below required confidence
		}
		if winner == nil || betterBid(b, *winner) {
			w := b
			winner = &w
		}
	}
	if winner == nil {
		return Award{}, ErrNoEligibleBids
	}
	return Award{
		TaskID:     ann.TaskID,
		WinnerName: winner.BidderName,
		WinningBid: *winner,
	}, nil
}

// betterBid reports whether candidate should beat the incumbent: lower cost
// wins, and on equal cost the higher-confidence bid wins.
func betterBid(candidate, incumbent Bid) bool {
	if candidate.Cost != incumbent.Cost {
		return candidate.Cost < incumbent.Cost
	}
	return candidate.Confidence > incumbent.Confidence
}

// ScoreAnnouncement is the bidder-side inverse of EligibleBidders: an agent
// scores an announced task against its own agent card and produces the bid it
// would submit. It reports ok=false — no bid — when the card cannot cover the
// announcement's RequiredTags, or when the resulting confidence falls below the
// announcement's MinConfidence (a bid the auctioneer would reject anyway).
//
// Confidence measures focus: the fraction of the card's distinct capabilities
// that the task actually demands, so a specialist whose every skill is wanted
// bids at confidence 1, while a generalist carrying irrelevant skills is
// diluted. Cost counts those irrelevant capabilities, so the more focused agent
// bids cheaper. An announcement with no RequiredTags is open to everyone and is
// treated as a perfect fit (confidence 1, cost 0).
func ScoreAnnouncement(bidderName string, card *a2a.AgentCard, ann TaskAnnouncement) (Bid, bool) {
	if card == nil || !cardCoversTags(card, ann.RequiredTags) {
		return Bid{}, false
	}

	confidence, cost := 1.0, 0.0
	if len(ann.RequiredTags) > 0 {
		required := make(map[string]struct{}, len(ann.RequiredTags))
		for _, tag := range ann.RequiredTags {
			required[tag] = struct{}{}
		}

		have := make(map[string]struct{})
		for _, skill := range card.Skills {
			for _, tag := range skill.Tags {
				have[tag] = struct{}{}
			}
		}

		demanded := 0
		for tag := range have {
			if _, ok := required[tag]; ok {
				demanded++
			}
		}
		total := len(have)
		if total == 0 {
			return Bid{}, false
		}
		confidence = float64(demanded) / float64(total)
		cost = float64(total - demanded)
	}

	if confidence < ann.MinConfidence {
		return Bid{}, false
	}

	return Bid{
		TaskID:     ann.TaskID,
		BidderName: bidderName,
		Cost:       cost,
		Confidence: confidence,
	}, true
}

// BidCollector delivers a task announcement to a single candidate agent and
// returns the candidate's raw text response. It is the one seam the auctioneer
// needs from the A2A transport, and is satisfied by *BTAgentClient in
// production and by a fake in tests. An empty response signals that the
// candidate declined to bid.
type BidCollector interface {
	SendTask(ctx context.Context, agentURL, taskText string) (string, error)
}

// Auctioneer opens an auction by fanning a TaskAnnouncement out to candidate
// agents over a BidCollector and gathering their bids. Unreachable, silent,
// malformed, and foreign-task candidates are tolerated: a single bad candidate
// never fails the auction, it is simply omitted from the returned bids.
type Auctioneer struct {
	transport BidCollector
}

// NewAuctioneer creates an Auctioneer that announces and collects bids over the
// given transport.
func NewAuctioneer(transport BidCollector) *Auctioneer {
	return &Auctioneer{transport: transport}
}

// AuctionResult is the outcome of a completed auction: the Award naming the
// winning bidder and the execution Result the winner returned after being
// dispatched the real task text.
type AuctionResult struct {
	Award  Award
	Result string
}

// RunAuction composes the three auction stages end to end: it fans the
// announcement out via CollectBids, picks the winner with a ScoreEvaluator,
// then dispatches the announcement's Description (the real task text) to the
// winning agent's URL over the same transport and returns that execution result
// alongside the Award.
//
// candidates maps a candidate's display name to its A2A base URL — the same map
// handed to CollectBids. The winning agent is located by matching the Award's
// WinnerName back to that map. Only the winner is dispatched the real task; the
// losing candidates are never invoked again. When no bid is eligible to win,
// the evaluator's error (ErrNoEligibleBids) is propagated and nothing is
// dispatched.
func (a *Auctioneer) RunAuction(ctx context.Context, ann TaskAnnouncement, candidates map[string]string) (AuctionResult, error) {
	bids, err := a.CollectBids(ctx, ann, candidates)
	if err != nil {
		return AuctionResult{}, err
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		return AuctionResult{}, err
	}

	winnerURL, ok := candidates[award.WinnerName]
	if !ok {
		return AuctionResult{}, fmt.Errorf("a2a: winning bidder %q has no candidate URL", award.WinnerName)
	}

	result, err := a.transport.SendTask(ctx, winnerURL, ann.Description)
	if err != nil {
		return AuctionResult{}, fmt.Errorf("a2a: dispatch task to winner %q: %w", award.WinnerName, err)
	}

	return AuctionResult{Award: award, Result: result}, nil
}

// CollectBids validates the announcement, fans it out to every candidate
// concurrently, and returns the valid bids sorted by bidder name.
//
// candidates maps a candidate's display name to its A2A base URL. The
// announcement is delivered JSON-encoded as the task text. A candidate's
// response is kept only when it is a well-formed Bid (Validate passes) for the
// announced task; declines (empty response), transport errors, non-JSON
// garbage, structurally invalid bids, and bids for a different task are all
// dropped. An invalid announcement is rejected before any fan-out.
func (a *Auctioneer) CollectBids(ctx context.Context, ann TaskAnnouncement, candidates map[string]string) ([]Bid, error) {
	if err := ann.Validate(); err != nil {
		return nil, fmt.Errorf("a2a: invalid announcement: %w", err)
	}

	payload, err := json.Marshal(ann)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal announcement: %w", err)
	}
	taskText := string(payload)

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		bids []Bid
	)
	for _, agentURL := range candidates {
		wg.Add(1)
		go func(agentURL string) {
			defer wg.Done()

			resp, err := a.transport.SendTask(ctx, agentURL, taskText)
			if err != nil || resp == "" {
				return // candidate unreachable or declined to bid
			}

			var bid Bid
			if err := json.Unmarshal([]byte(resp), &bid); err != nil {
				return // response was not a bid
			}
			if bid.TaskID != ann.TaskID {
				return // bid for a different task
			}
			if err := bid.Validate(); err != nil {
				return // structurally invalid bid
			}

			mu.Lock()
			bids = append(bids, bid)
			mu.Unlock()
		}(agentURL)
	}
	wg.Wait()

	sort.Slice(bids, func(i, j int) bool {
		return bids[i].BidderName < bids[j].BidderName
	})
	return bids, nil
}
