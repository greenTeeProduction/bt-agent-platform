package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
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

// ---- honesty: SendTask must not launder a failed/empty response into a
// success string --------------------------------------------------------------
//
// Before this fix, BTAgentClient.SendTask's a2a.SendMessageResult switch
// (client.go) always returned a nil error once the RPC itself succeeded, even
// when the delegated task had actually failed, been canceled, or been
// rejected — it just described the failure inside a "successful" string. That
// let a failed delegation look like a genuine, actionable result to every
// caller, including the auction winner dispatch in auction.go. These tests
// pin the honest contract for interpretSendResult, the pure decision function
// SendTask delegates to: a task that did not complete, or a message that
// carries no content, is reported as an error — never as success text.

func TestInterpretSendResult_HonestlyReportsFailureStates(t *testing.T) {
	tests := []struct {
		name     string
		resp     a2a.SendMessageResult
		wantErr  bool
		wantText string
	}{
		{
			name: "completed task with artifact text succeeds",
			resp: &a2a.Task{
				ID:     "t1",
				Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
				Artifacts: []*a2a.Artifact{
					{Parts: a2a.ContentParts{a2a.NewTextPart("the answer")}},
				},
			},
			wantText: "the answer",
		},
		{
			name: "failed task must be an error, not a success string describing the failure",
			resp: &a2a.Task{
				ID:     "t2",
				Status: a2a.TaskStatus{State: a2a.TaskStateFailed},
			},
			wantErr: true,
		},
		{
			name: "canceled task must be an error",
			resp: &a2a.Task{
				ID:     "t3",
				Status: a2a.TaskStatus{State: a2a.TaskStateCanceled},
			},
			wantErr: true,
		},
		{
			name: "rejected task must be an error",
			resp: &a2a.Task{
				ID:     "t4",
				Status: a2a.TaskStatus{State: a2a.TaskStateRejected},
			},
			wantErr: true,
		},
		{
			name:     "message with text succeeds",
			resp:     a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")),
			wantText: "hello",
		},
		{
			name:    "message with no content parts must be an error, not a fabricated success string",
			resp:    a2a.NewMessage(a2a.MessageRoleAgent),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, err := interpretSendResult(tc.resp)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("interpretSendResult(%+v) = (%q, nil), want a non-nil error", tc.resp, text)
				}
				return
			}
			if err != nil {
				t.Fatalf("interpretSendResult returned an unexpected error: %v", err)
			}
			if text != tc.wantText {
				t.Errorf("interpretSendResult text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

// ---- resilience: the auction winner dispatch must retry and circuit-break --
//
// Before this fix, Auctioneer.RunAuction (auction.go) dispatched the real
// task to the winning candidate exactly once: any transport failure — even a
// transient one — immediately failed the whole auction, and a winner that
// kept failing was hammered again on every subsequent auction with no memory
// of its track record. These tests pin the resilience contract: a transient
// winner-dispatch failure must be retried before RunAuction gives up, and a
// winner that keeps failing must eventually be circuit-broken so further
// auctions stop dispatching to it at all.

// flakyDispatchTransport plays both auction roles like auctionTransport
// (fan-out returns a canned bid), but lets a test script the winner-dispatch
// outcome: the first failUntil dispatch attempts return err, and every
// attempt after that returns result. failUntil == 0 means every dispatch
// attempt fails. Only dispatch attempts (not fan-out bid calls) are counted,
// so a test can assert on retry/circuit-breaker call volume against the
// winner.
type flakyDispatchTransport struct {
	mu            sync.Mutex
	bid           string
	err           error
	result        string
	failUntil     int
	dispatchCalls int
}

func (f *flakyDispatchTransport) SendTask(_ context.Context, _, taskText string) (string, error) {
	var probe TaskAnnouncement
	if jsonErr := json.Unmarshal([]byte(taskText), &probe); jsonErr == nil && probe.TaskID != "" {
		return f.bid, nil // fan-out: return the canned bid, not a dispatch attempt
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.dispatchCalls++
	if f.failUntil <= 0 || f.dispatchCalls <= f.failUntil {
		return "", f.err
	}
	return f.result, nil
}

func (f *flakyDispatchTransport) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dispatchCalls
}

func TestAuctioneer_RunAuction_RetriesWinnerDispatchOnTransientFailure(t *testing.T) {
	ft := &flakyDispatchTransport{
		bid:       bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}),
		err:       errors.New("connection refused"), // classifies as a retryable network error
		result:    "done after retry",
		failUntil: 1, // the first dispatch attempt fails, the second succeeds
	}
	ann := TaskAnnouncement{TaskID: "t1", Description: "work", MinConfidence: 0.5}

	auc := NewAuctioneer(ft)
	res, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"})
	if err != nil {
		t.Fatalf("RunAuction failed despite the winner recovering on retry: %v", err)
	}
	if res.Result != "done after retry" {
		t.Errorf("result = %q, want the retried dispatch's result", res.Result)
	}
	if got := ft.calls(); got < 2 {
		t.Errorf("winner dispatch was attempted %d time(s), want at least 2 (a retry after the transient failure)", got)
	}
}

func TestAuctioneer_RunAuction_ReturnsErrorAfterExhaustingRetries(t *testing.T) {
	ft := &flakyDispatchTransport{
		bid:       bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}),
		err:       errors.New("connection refused"), // retryable, but never recovers
		failUntil: 0,
	}
	ann := TaskAnnouncement{TaskID: "t1", Description: "work", MinConfidence: 0.5}

	auc := NewAuctioneer(ft)
	if _, err := auc.RunAuction(context.Background(), ann, map[string]string{"a": "http://a"}); err == nil {
		t.Fatal("expected RunAuction to return an error when the winner's dispatch never succeeds")
	}
	if got := ft.calls(); got < 2 {
		t.Errorf("winner dispatch was attempted %d time(s), want more than 1 — a persistently failing winner must be retried, not abandoned after a single attempt", got)
	}
}

func TestAuctioneer_RunAuction_CircuitBreaksWinnerAfterRepeatedFailures(t *testing.T) {
	ft := &flakyDispatchTransport{
		bid:       bidJSON(t, Bid{TaskID: "t1", Cost: 1, Confidence: 0.9}),
		err:       errors.New("invalid request: malformed dispatch payload"), // non-retryable, keeps each call fast
		failUntil: 0,
	}
	ann := TaskAnnouncement{TaskID: "t1", Description: "work", MinConfidence: 0.5}
	candidates := map[string]string{"a": "http://a"}

	auc := NewAuctioneer(ft)

	const attempts = 8
	callsAfter := make([]int, attempts)
	for i := 0; i < attempts; i++ {
		if _, err := auc.RunAuction(context.Background(), ann, candidates); err == nil {
			t.Fatalf("RunAuction call %d unexpectedly succeeded against a permanently failing winner", i)
		}
		callsAfter[i] = ft.calls()
	}

	// Once the circuit breaker opens for this winner, later RunAuction calls
	// must stop invoking the transport at all — the fix for "fires once with
	// no fallback": a known-bad winner must not be hammered forever.
	if callsAfter[attempts-1] != callsAfter[attempts-2] {
		t.Errorf("winner dispatch call count kept growing across repeated failures (%v); expected it to plateau once the circuit breaker opens", callsAfter)
	}
}
