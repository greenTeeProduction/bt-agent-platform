package a2a

import (
	"errors"
	"fmt"
	"time"
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
