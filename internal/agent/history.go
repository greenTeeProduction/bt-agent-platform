package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RunRecord is a single execution of an agent. Persisted to disk for history.
type RunRecord struct {
	ID        string    `json:"id"`
	AgentName string    `json:"agent_name"`
	Task      string    `json:"task"`
	Outcome   string    `json:"outcome"`   // success, failure, partial, timeout, panic
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	Duration  string    `json:"duration"`  // "2m34s"
	Quality   float64   `json:"quality"`   // 0.0-1.0 output quality
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// History stores agent execution records. Thread-safe, persisted to JSON.
type History struct {
	mu     sync.RWMutex
	dir    string
	byName map[string][]RunRecord // agent name → runs (most recent last)
}

// NewHistory creates a new history store.
func NewHistory(dir string) (*History, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create history dir: %w", err)
	}
	h := &History{
		dir:    dir,
		byName: make(map[string][]RunRecord),
	}
	return h, h.loadAll()
}

// Record saves a new run record.
func (h *History) Record(r RunRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.ID == "" {
		r.ID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now()
	}
	if r.EndedAt.IsZero() {
		r.EndedAt = time.Now()
	}

	h.byName[r.AgentName] = append(h.byName[r.AgentName], r)

	// Persist
	path := filepath.Join(h.dir, r.AgentName+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open history file: %w", err)
	}
	defer f.Close()

	data, _ := json.Marshal(r)
	f.Write(append(data, '\n'))
	return nil
}

// List returns run history for an agent, most recent first.
func (h *History) List(agentName string, limit int) []RunRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()

	runs := h.byName[agentName]
	if limit > 0 && limit < len(runs) {
		runs = runs[len(runs)-limit:]
	}

	// Most recent first
	result := make([]RunRecord, len(runs))
	for i, r := range runs {
		result[len(runs)-1-i] = r
	}
	return result
}

// Stats returns aggregate statistics for an agent.
func (h *History) Stats(agentName string) RunStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	runs := h.byName[agentName]
	if len(runs) == 0 {
		return RunStats{AgentName: agentName}
	}

	var totalDuration time.Duration
	var successes, failures, panics int
	var totalQuality float64

	for _, r := range runs {
		d, _ := time.ParseDuration(r.Duration)
		totalDuration += d
		switch r.Outcome {
		case "success":
			successes++
		case "panic", "chain_panic":
			panics++
		default:
			failures++
		}
		totalQuality += r.Quality
	}

	return RunStats{
		AgentName:     agentName,
		TotalRuns:     len(runs),
		SuccessRate:   float64(successes) / float64(len(runs)),
		TotalPanics:   panics,
		AvgDuration:   totalDuration / time.Duration(len(runs)),
		AvgQuality:    totalQuality / float64(len(runs)),
		LastRun:       runs[len(runs)-1].EndedAt,
		LastOutcome:   runs[len(runs)-1].Outcome,
	}
}

// AllStats returns stats for all agents.
func (h *History) AllStats() map[string]RunStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[string]RunStats)
	for name := range h.byName {
		result[name] = h.Stats(name)
	}
	return result
}

// RunStats is aggregate statistics for agent runs.
type RunStats struct {
	AgentName   string        `json:"agent_name"`
	TotalRuns   int           `json:"total_runs"`
	SuccessRate float64       `json:"success_rate"`
	TotalPanics int           `json:"total_panics"`
	AvgDuration time.Duration `json:"avg_duration"`
	AvgQuality  float64       `json:"avg_quality"`
	LastRun     time.Time     `json:"last_run"`
	LastOutcome string        `json:"last_outcome"`
}

// Cleanup removes runs older than the given duration.
func (h *History) Cleanup(olderThan time.Duration) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for name, runs := range h.byName {
		var kept []RunRecord
		for _, r := range runs {
			if r.EndedAt.Before(cutoff) {
				removed++
			} else {
				kept = append(kept, r)
			}
		}
		h.byName[name] = kept
	}

	return removed, nil
}

func (h *History) loadAll() error {
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		path := filepath.Join(h.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		agentName := entry.Name()[:len(entry.Name())-6] // strip .jsonl
		lines := splitLines(string(data))
		for _, line := range lines {
			if line == "" {
				continue
			}
			var r RunRecord
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			h.byName[agentName] = append(h.byName[agentName], r)
		}
	}

	// Sort each agent's runs by time
	for _, runs := range h.byName {
		sort.Slice(runs, func(i, j int) bool {
			return runs[i].EndedAt.Before(runs[j].EndedAt)
		})
	}

	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
