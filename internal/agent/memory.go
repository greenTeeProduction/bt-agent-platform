// Package agent provides persistent key-value memory for behavior tree agents.
//
// AgentMemory is a per-agent JSON store that survives across runs.
// Agents can write_fact() to record learnings during execution and
// read_context() to get relevant past context injected into their LLM prompt.
//
// Design principles:
//   - Keys are namespaced (e.g., "pitfall:outcome_selector", "pattern:code_review")
//   - Values are JSON-serializable (strings, numbers, objects)
//   - Auto-injects last N successful task outputs as context
//   - Memory is bounded — max 100 entries per agent, oldest evicted on overflow
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
)

// MemoryEntry is a single key-value record in agent memory.
type MemoryEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Category  string    `json:"category"` // "fact", "pattern", "pitfall", "preference", "state"
	Priority  string    `json:"priority"` // "high", "medium", "low"
	Source    string    `json:"source"`   // "agent", "reflection", "manual", "extracted"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HitCount  int       `json:"hit_count"` // how many times this entry was read
}

// MemoryStore is a per-agent persistent key-value store.
// Thread-safe, persisted to JSON on every write.
type MemoryStore struct {
	mu      sync.RWMutex
	dir     string
	maxSize int
	entries map[string]*MemoryEntry // key → entry
}

// NewMemoryStore creates or opens a memory store for an agent.
func NewMemoryStore(dir string, agentName string, maxSize int) (*MemoryStore, error) {
	agentDir := filepath.Join(dir, agentName)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}

	ms := &MemoryStore{
		dir:     agentDir,
		maxSize: maxSize,
		entries: make(map[string]*MemoryEntry),
	}

	if err := ms.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load memory: %w", err)
	}

	return ms, nil
}

// Write stores a key-value entry. Creates or updates.
// Returns error if store is full and new key.
func (ms *MemoryStore) Write(key, value, category, priority, source string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now()

	if existing, ok := ms.entries[key]; ok {
		existing.Value = value
		existing.UpdatedAt = now
		if category != "" {
			existing.Category = category
		}
		if priority != "" {
			existing.Priority = priority
		}
	} else {
		// Enforce size limit
		if len(ms.entries) >= ms.maxSize {
			ms.evictLRU()
		}
		if len(ms.entries) >= ms.maxSize {
			return fmt.Errorf("memory store full (%d entries), cannot add new key", ms.maxSize)
		}

		ms.entries[key] = &MemoryEntry{
			Key:       key,
			Value:     value,
			Category:  category,
			Priority:  priority,
			Source:    source,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	return ms.save()
}

// Read retrieves a value by key. Increments hit count.
// Returns empty string if key not found.
func (ms *MemoryStore) Read(key string) string {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, ok := ms.entries[key]
	if !ok {
		return ""
	}
	entry.HitCount++
	// Don't save on read — hit count is approximate, saves I/O
	return entry.Value
}

// Query returns entries matching a category prefix and/or priority.
// If both are empty, returns all entries.
func (ms *MemoryStore) Query(category, priority string, limit int) []MemoryEntry {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var results []MemoryEntry
	for _, e := range ms.entries {
		if category != "" && !strings.HasPrefix(e.Category, category) {
			continue
		}
		if priority != "" && e.Priority != priority {
			continue
		}
		results = append(results, *e)
	}

	// Sort by priority then recency
	sort.Slice(results, func(i, j int) bool {
		pi := priorityWeight(results[i].Priority)
		pj := priorityWeight(results[j].Priority)
		if pi != pj {
			return pi > pj
		}
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results
}

// ContextBlock returns a formatted block of memory context for LLM injection.
// Includes: high-priority facts, recent pitfalls, state entries.
func (ms *MemoryStore) ContextBlock() string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var parts []string

	// High-priority facts
	facts := ms.queryLocked("fact", "high", 5)
	if len(facts) > 0 {
		parts = append(parts, "AGENT MEMORY — High-Priority Facts:")
		for _, f := range facts {
			parts = append(parts, fmt.Sprintf("  • %s: %s", f.Key, truncate(f.Value, 120)))
		}
	}

	// Pitfalls to avoid
	pitfalls := ms.queryLocked("pitfall", "high", 3)
	if len(pitfalls) > 0 {
		parts = append(parts, "\nAGENT MEMORY — Known Pitfalls:")
		for _, p := range pitfalls {
			parts = append(parts, fmt.Sprintf("  ⚠ %s: %s", strings.TrimPrefix(p.Key, "pitfall:"), truncate(p.Value, 120)))
		}
	}

	// Patterns learned
	patterns := ms.queryLocked("pattern", "high", 3)
	if len(patterns) > 0 {
		parts = append(parts, "\nAGENT MEMORY — Patterns Learned:")
		for _, p := range patterns {
			parts = append(parts, fmt.Sprintf("  ✓ %s: %s", strings.TrimPrefix(p.Key, "pattern:"), truncate(p.Value, 120)))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// PreviousRunContext returns the last N successful run outputs.
func (ms *MemoryStore) PreviousRunContext(history *History, agentName string, n int) string {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	runs := history.List(agentName, n+5) // get a few extra to find successes

	var successes []RunRecord
	for _, r := range runs {
		if r.Outcome == "success" && r.Output != "" {
			successes = append(successes, r)
			if len(successes) >= n {
				break
			}
		}
	}

	if len(successes) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("PREVIOUS RUNS — Last %d successful task outputs:", len(successes)))
	for i, s := range successes {
		summary := summarizeOutput(s.Output, 200)
		lines = append(lines, fmt.Sprintf("\n  Run %d (%s, %s):", i+1, s.EndedAt.Format("15:04"), s.Duration))
		lines = append(lines, fmt.Sprintf("    Task: %s", truncate(s.Task, 100)))
		lines = append(lines, fmt.Sprintf("    Output: %s", summary))
	}

	return strings.Join(lines, "\n")
}

// Stats returns memory store statistics.
func (ms *MemoryStore) Stats() map[string]int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	stats := map[string]int{
		"total": len(ms.entries),
	}
	for _, e := range ms.entries {
		stats[e.Category]++
		stats["priority_"+e.Priority]++
	}
	return stats
}

// Delete removes an entry by key.
func (ms *MemoryStore) Delete(key string) bool {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if _, ok := ms.entries[key]; !ok {
		return false
	}
	delete(ms.entries, key)
	ms.save() // best-effort
	return true
}

// ── Internal helpers ──────────────────────────────────────────────────────

func (ms *MemoryStore) load() error {
	path := filepath.Join(ms.dir, "memory.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &ms.entries)
}

func (ms *MemoryStore) save() error {
	path := filepath.Join(ms.dir, "memory.json")
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(ms.entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// evictLRU removes the least-recently-updated entry. Must be called under lock.
func (ms *MemoryStore) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, e := range ms.entries {
		if first || e.UpdatedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.UpdatedAt
			first = false
		}
	}

	if oldestKey != "" {
		delete(ms.entries, oldestKey)
	}
}

// queryLocked is like Query but without locking (caller must hold lock).
func (ms *MemoryStore) queryLocked(category, priority string, limit int) []MemoryEntry {
	var results []MemoryEntry
	for _, e := range ms.entries {
		if category != "" && e.Category != category {
			continue
		}
		if priority != "" && e.Priority != priority {
			continue
		}
		results = append(results, *e)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].UpdatedAt.After(results[j].UpdatedAt)
	})

	if limit > 0 && limit < len(results) {
		results = results[:limit]
	}
	return results
}

func priorityWeight(p string) int {
	switch p {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func truncate(s string, n int) string { return util.Truncate(s, n) }

func summarizeOutput(output string, maxLen int) string {
	// Get first paragraph or first maxLen chars
	cleaned := strings.TrimSpace(output)
	if idx := strings.Index(cleaned, "\n\n"); idx > 0 && idx < maxLen {
		return cleaned[:idx]
	}
	return truncate(cleaned, maxLen)
}
