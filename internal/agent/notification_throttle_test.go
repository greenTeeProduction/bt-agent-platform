package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// throttleTestServer captures bt-task-complete webhook deliveries.
type throttleTestServer struct {
	mu        sync.Mutex
	posts     int
	summaries []string
	srv       *httptest.Server
}

func newThrottleTestServer(t *testing.T) *throttleTestServer {
	t.Helper()
	ts := &throttleTestServer{}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/bt-task-complete") {
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Data map[string]any `json:"data"`
			}
			_ = json.Unmarshal(body, &payload)
			ts.mu.Lock()
			ts.posts++
			if s, ok := payload.Data["summary"].(string); ok {
				ts.summaries = append(ts.summaries, s)
			}
			ts.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *throttleTestServer) count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.posts
}

func (ts *throttleTestServer) lastSummary() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.summaries) == 0 {
		return ""
	}
	return ts.summaries[len(ts.summaries)-1]
}

func taskCompleteEvent(agent, outcome, summary string) AgentEvent {
	return AgentEvent{
		Type:      "task_complete",
		Source:    agent,
		Message:   agent + ": " + outcome,
		Data:      map[string]any{"outcome": outcome, "summary": summary, "tree": "domain:test"},
		Timestamp: time.Now(),
	}
}

// 13 of bt-fusion's 15 notifications on 2026-07-15 were the identical hourly
// "0 new findings" no-op — 22% of the day's Telegram volume. After a baseline
// no_change notification, further no_change runs must be suppressed and
// rolled up into the next real notification instead.
func TestRoutineNoChangeNotificationsThrottledAfterBaseline(t *testing.T) {
	ts := newThrottleTestServer(t)
	pub := NewWebhookPublisher(ts.srv.URL, DefaultWebhookSecrets())

	pub.handleEvent(taskCompleteEvent("throttle-fusion-a", "no_change", "0 new findings"))
	if got := ts.count(); got != 1 {
		t.Fatalf("baseline no_change must notify once, got %d posts", got)
	}

	pub.handleEvent(taskCompleteEvent("throttle-fusion-a", "no_change", "0 new findings"))
	pub.handleEvent(taskCompleteEvent("throttle-fusion-a", "no_change", "0 new findings"))
	if got := ts.count(); got != 1 {
		t.Fatalf("repeat no_change within the heartbeat window must be suppressed, got %d posts", got)
	}

	pub.handleEvent(taskCompleteEvent("throttle-fusion-a", "success", "2 new findings recorded"))
	if got := ts.count(); got != 2 {
		t.Fatalf("real outcome must notify, got %d posts", got)
	}
	if s := ts.lastSummary(); !strings.Contains(s, "2 routine no-change") {
		t.Fatalf("real notification must roll up the 2 suppressed runs, summary: %q", s)
	}

	pub.handleEvent(taskCompleteEvent("throttle-fusion-a", "success", "1 new finding recorded"))
	if s := ts.lastSummary(); strings.Contains(s, "routine no-change") {
		t.Fatalf("rollup counter must reset after being reported, summary: %q", s)
	}
}

// Suppression must never go fully silent: after a quiet day of no_change
// runs, one heartbeat notification goes out so silence stays distinguishable
// from a dead agent.
func TestRoutineNoChangeHeartbeatAfterQuietDay(t *testing.T) {
	ts := newThrottleTestServer(t)
	pub := NewWebhookPublisher(ts.srv.URL, DefaultWebhookSecrets())
	base := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	now := base
	pub.throttle.now = func() time.Time { return now }

	pub.handleEvent(taskCompleteEvent("throttle-fusion-hb", "no_change", "0 new findings"))
	now = base.Add(1 * time.Hour)
	pub.handleEvent(taskCompleteEvent("throttle-fusion-hb", "no_change", "0 new findings"))
	if got := ts.count(); got != 1 {
		t.Fatalf("expected 1 post before the heartbeat window elapses, got %d", got)
	}

	now = base.Add(25 * time.Hour)
	pub.handleEvent(taskCompleteEvent("throttle-fusion-hb", "no_change", "0 new findings"))
	if got := ts.count(); got != 2 {
		t.Fatalf("no_change after a quiet day must send a heartbeat, got %d posts", got)
	}
	if s := ts.lastSummary(); !strings.Contains(s, "heartbeat") || !strings.Contains(s, "1 routine no-change") {
		t.Fatalf("heartbeat must identify itself and the suppressed count, summary: %q", s)
	}
}

// Only the healthy no_change outcome is throttleable — failures and degraded
// runs signal problems and must always reach the operator.
func TestFailureAndDegradedOutcomesNeverThrottled(t *testing.T) {
	ts := newThrottleTestServer(t)
	pub := NewWebhookPublisher(ts.srv.URL, DefaultWebhookSecrets())

	pub.handleEvent(taskCompleteEvent("throttle-fusion-f", "no_change", "0 new findings"))
	pub.handleEvent(taskCompleteEvent("throttle-fusion-f", "no_change", "0 new findings"))
	pub.handleEvent(taskCompleteEvent("throttle-fusion-f", "failure", "cycle died"))
	pub.handleEvent(taskCompleteEvent("throttle-fusion-f", "degraded", "fell back to deterministic analysis"))
	if got := ts.count(); got != 3 {
		t.Fatalf("failure and degraded must always notify (baseline + 2), got %d posts", got)
	}
}

// The throttle state must survive daemon restarts (4 on 2026-07-15 alone) —
// otherwise every restart re-sends a baseline no-op notification.
func TestThrottleStatePersistsAcrossPublisherRestarts(t *testing.T) {
	ts := newThrottleTestServer(t)
	pub1 := NewWebhookPublisher(ts.srv.URL, DefaultWebhookSecrets())
	pub1.handleEvent(taskCompleteEvent("throttle-fusion-p", "no_change", "0 new findings"))
	if got := ts.count(); got != 1 {
		t.Fatalf("baseline must notify, got %d", got)
	}

	pub2 := NewWebhookPublisher(ts.srv.URL, DefaultWebhookSecrets())
	pub2.handleEvent(taskCompleteEvent("throttle-fusion-p", "no_change", "0 new findings"))
	if got := ts.count(); got != 1 {
		t.Fatalf("suppression state must persist across publisher restarts, got %d posts", got)
	}
}
