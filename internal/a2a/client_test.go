package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
)

// fakeTransport is an in-memory BidCollector: it records the announcement text
// delivered to each candidate URL and returns a canned bid response (or error)
// per URL. It stands in for the real A2A client round-trip so the auctioneer
// can be unit-tested without a network.
type fakeTransport struct {
	mu        sync.Mutex
	sent      map[string]string // agentURL -> task text delivered
	responses map[string]string // agentURL -> raw response text to return
	errs      map[string]error  // agentURL -> error to return instead
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		sent:      map[string]string{},
		responses: map[string]string{},
		errs:      map[string]error{},
	}
}

func (f *fakeTransport) SendTask(_ context.Context, agentURL, taskText string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[agentURL] = taskText
	if err := f.errs[agentURL]; err != nil {
		return "", err
	}
	return f.responses[agentURL], nil
}

func (f *fakeTransport) sentURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	urls := make([]string, 0, len(f.sent))
	for u := range f.sent {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	return urls
}

// bidJSON marshals a Bid to the wire form the auctioneer expects back from a
// candidate agent.
func bidJSON(t *testing.T, b Bid) string {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal bid: %v", err)
	}
	return string(raw)
}

// ---- fan-out: every candidate receives the announcement ---------------------

func TestAuctioneer_FansOutToAllCandidates(t *testing.T) {
	ft := newFakeTransport()
	ann := TaskAnnouncement{TaskID: "t1", Description: "review a PR", MinConfidence: 0.5}
	ft.responses["http://a"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "a", Cost: 10, Confidence: 0.8})
	ft.responses["http://b"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "b", Cost: 20, Confidence: 0.9})

	auc := NewAuctioneer(ft)
	candidates := map[string]string{"a": "http://a", "b": "http://b"}

	if _, err := auc.CollectBids(context.Background(), ann, candidates); err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}

	// Both candidate URLs must have been announced to.
	if got, want := ft.sentURLs(), []string{"http://a", "http://b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("announced to %v, want %v", got, want)
	}

	// The delivered text must carry the announcement (JSON-encoded).
	for _, url := range []string{"http://a", "http://b"} {
		var delivered TaskAnnouncement
		if err := json.Unmarshal([]byte(ft.sent[url]), &delivered); err != nil {
			t.Fatalf("announcement to %s was not valid JSON: %v (text=%q)", url, err, ft.sent[url])
		}
		if delivered.TaskID != ann.TaskID {
			t.Errorf("announcement to %s TaskID = %q, want %q", url, delivered.TaskID, ann.TaskID)
		}
	}
}

// ---- collection: valid bids are parsed and returned -------------------------

func TestAuctioneer_CollectsValidBids(t *testing.T) {
	ft := newFakeTransport()
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	ft.responses["http://a"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "a", Cost: 10, Confidence: 0.8})
	ft.responses["http://b"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "b", Cost: 25, Confidence: 0.9})

	auc := NewAuctioneer(ft)
	candidates := map[string]string{"a": "http://a", "b": "http://b"}

	bids, err := auc.CollectBids(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}

	if len(bids) != 2 {
		t.Fatalf("collected %d bids, want 2: %+v", len(bids), bids)
	}
	// Result must be deterministic: sorted by bidder name.
	if bids[0].BidderName != "a" || bids[1].BidderName != "b" {
		t.Errorf("bids not sorted by bidder: got %q, %q", bids[0].BidderName, bids[1].BidderName)
	}
	if bids[0].Cost != 10 || bids[1].Cost != 25 {
		t.Errorf("bid costs = %v, %v, want 10, 25", bids[0].Cost, bids[1].Cost)
	}

	// The collected bids must feed straight into the evaluator from milestone 1.
	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		t.Fatalf("evaluate collected bids: %v", err)
	}
	if award.WinnerName != "a" {
		t.Errorf("winner from collected bids = %q, want a", award.WinnerName)
	}
}

// ---- resilience: declining / failing / malformed candidates are skipped -----

func TestAuctioneer_SkipsErroringAndEmptyCandidates(t *testing.T) {
	ft := newFakeTransport()
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	ft.responses["http://good"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "good", Cost: 10, Confidence: 0.8})
	ft.errs["http://down"] = errors.New("connection refused")
	ft.responses["http://silent"] = "" // agent declined to bid

	auc := NewAuctioneer(ft)
	candidates := map[string]string{
		"good":   "http://good",
		"down":   "http://down",
		"silent": "http://silent",
	}

	bids, err := auc.CollectBids(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("CollectBids must tolerate individual candidate failures, got: %v", err)
	}
	if len(bids) != 1 || bids[0].BidderName != "good" {
		t.Fatalf("collected %+v, want only the 'good' bid", bids)
	}
}

func TestAuctioneer_DropsInvalidAndForeignBids(t *testing.T) {
	ft := newFakeTransport()
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	ft.responses["http://ok"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "ok", Cost: 10, Confidence: 0.8})
	// Bid for a different task must be dropped.
	ft.responses["http://foreign"] = bidJSON(t, Bid{TaskID: "other", BidderName: "foreign", Cost: 1, Confidence: 0.9})
	// Structurally invalid bid (negative cost) must be dropped.
	ft.responses["http://invalid"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "invalid", Cost: -5, Confidence: 0.9})
	// Non-JSON garbage must be dropped, not crash the auction.
	ft.responses["http://garbage"] = "not a bid"

	auc := NewAuctioneer(ft)
	candidates := map[string]string{
		"ok":      "http://ok",
		"foreign": "http://foreign",
		"invalid": "http://invalid",
		"garbage": "http://garbage",
	}

	bids, err := auc.CollectBids(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}
	if len(bids) != 1 || bids[0].BidderName != "ok" {
		t.Fatalf("collected %+v, want only the 'ok' bid (foreign/invalid/garbage dropped)", bids)
	}
}

// ---- attribution: a bid belongs to the candidate the announcement reached ---

// A candidate must not be able to mis-attribute its bid by self-reporting a
// BidderName other than the identity it was actually announced to under.
// CollectBids attributes each collected bid to the candidate it came from (the
// candidates-map key), not the untrusted payload field, so the downstream Award
// always resolves back to a real candidate URL. Otherwise a single candidate
// that returns a valid-looking bid under an unknown name fails the whole
// auction (RunAuction's winner lookup misses), violating the resilience
// contract that one bad candidate is only omitted, never fatal.
func TestAuctioneer_AttributesBidToAnnouncedCandidate(t *testing.T) {
	ft := newFakeTransport()
	ann := TaskAnnouncement{TaskID: "t1", MinConfidence: 0.5}
	// The agent known as "alice" returns a bid claiming to be "ghost" — a name
	// that is not among the candidates.
	ft.responses["http://alice"] = bidJSON(t, Bid{TaskID: "t1", BidderName: "ghost", Cost: 10, Confidence: 0.8})

	auc := NewAuctioneer(ft)
	candidates := map[string]string{"alice": "http://alice"}

	bids, err := auc.CollectBids(context.Background(), ann, candidates)
	if err != nil {
		t.Fatalf("CollectBids failed: %v", err)
	}
	if len(bids) != 1 {
		t.Fatalf("collected %d bids, want 1: %+v", len(bids), bids)
	}
	if bids[0].BidderName != "alice" {
		t.Errorf("bid attributed to %q, want announced candidate %q (self-reported name must not be trusted)", bids[0].BidderName, "alice")
	}
	// The attributed name must resolve back to a real candidate URL so a
	// downstream RunAuction can dispatch to the winner instead of erroring.
	if _, ok := candidates[bids[0].BidderName]; !ok {
		t.Errorf("collected bid bidder %q does not resolve in candidates map %v", bids[0].BidderName, candidates)
	}
}

// ---- guard: a malformed announcement is rejected before fan-out -------------

func TestAuctioneer_RejectsInvalidAnnouncement(t *testing.T) {
	ft := newFakeTransport()
	auc := NewAuctioneer(ft)

	_, err := auc.CollectBids(context.Background(), TaskAnnouncement{TaskID: ""}, map[string]string{"a": "http://a"})
	if err == nil {
		t.Fatal("expected error for announcement with empty TaskID")
	}
	if len(ft.sentURLs()) != 0 {
		t.Errorf("no announcements should be sent for an invalid announcement, got %v", ft.sentURLs())
	}
}
