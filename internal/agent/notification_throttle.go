package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// routineHeartbeatInterval is the longest a routine-only agent stays silent:
// once this much time passes without any delivered notification for an agent,
// its next no_change run goes out as a daily heartbeat instead of being
// suppressed, so throttled quiet ≠ dead agent.
const routineHeartbeatInterval = 24 * time.Hour

// throttleEntry is one agent's suppression state.
type throttleEntry struct {
	Suppressed      int       `json:"suppressed"`
	FirstSuppressed time.Time `json:"first_suppressed"`
	LastSent        time.Time `json:"last_sent"`
}

// routineThrottle suppresses repeat "no_change" task_complete notifications
// per agent: the first no_change after a delivered notification goes out as a
// baseline, further ones inside the heartbeat window are counted instead of
// sent, and the count is rolled up into the next delivered notification.
// State persists across daemon restarts (ADR-003 JSON, atomic tmp+rename).
// Every decision is fail-open: any state I/O problem delivers the event.
type routineThrottle struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

func newRoutineThrottle(path string) *routineThrottle {
	return &routineThrottle{path: path, now: time.Now}
}

// decide reports whether a task_complete notification should be delivered and
// optionally returns an annotated summary to deliver instead of the original.
func (th *routineThrottle) decide(agent, outcome, summary string) (send bool, annotated string) {
	th.mu.Lock()
	defer th.mu.Unlock()

	state := th.load()
	e := state[agent]
	if e == nil {
		e = &throttleEntry{}
		state[agent] = e
	}
	now := th.now()

	if outcome == "no_change" {
		heartbeatDue := e.LastSent.IsZero() || now.Sub(e.LastSent) >= routineHeartbeatInterval
		if !heartbeatDue {
			e.Suppressed++
			if e.FirstSuppressed.IsZero() {
				e.FirstSuppressed = now
			}
			th.save(state)
			return false, ""
		}
		if e.Suppressed > 0 {
			annotated = fmt.Sprintf("%s\n(daily heartbeat — %d routine no-change runs suppressed since %s)",
				summary, e.Suppressed, e.FirstSuppressed.Format("Jan 2 15:04"))
		}
		e.Suppressed = 0
		e.FirstSuppressed = time.Time{}
		e.LastSent = now
		th.save(state)
		return true, annotated
	}

	if e.Suppressed > 0 {
		annotated = fmt.Sprintf("%s\n(+%d routine no-change runs suppressed since %s)",
			summary, e.Suppressed, e.FirstSuppressed.Format("Jan 2 15:04"))
		e.Suppressed = 0
		e.FirstSuppressed = time.Time{}
	}
	e.LastSent = now
	th.save(state)
	return true, annotated
}

// load reads the persisted per-agent state; missing or corrupt files start
// fresh (fail-open: worst case a baseline notification is re-sent).
func (th *routineThrottle) load() map[string]*throttleEntry {
	state := map[string]*throttleEntry{}
	data, err := os.ReadFile(th.path)
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return map[string]*throttleEntry{}
	}
	return state
}

// save persists the state atomically (ADR-003 tmp+rename); errors are logged
// and swallowed — a notification decision must never fail on state I/O.
func (th *routineThrottle) save(state map[string]*throttleEntry) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		slog.Warn("notification throttle: marshal failed", "error", err)
		return
	}
	dir := filepath.Dir(th.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("notification throttle: mkdir failed", "error", err)
		return
	}
	tmp, err := os.CreateTemp(dir, ".notification-throttle-*")
	if err != nil {
		slog.Warn("notification throttle: temp file failed", "error", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		slog.Warn("notification throttle: write failed", "error", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		slog.Warn("notification throttle: close failed", "error", err)
		return
	}
	if err := os.Rename(tmpName, th.path); err != nil {
		os.Remove(tmpName)
		slog.Warn("notification throttle: rename failed", "error", err)
	}
}

// eventDataString extracts a string field from an event's Data map, tolerating
// nil maps and non-string values.
func eventDataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	s, _ := data[key].(string)
	return s
}
