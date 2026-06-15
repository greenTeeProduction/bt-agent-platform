package blackboard

import "time"

// ScopeKind identifies a blackboard namespace.
type ScopeKind string

const (
	ScopeRun     ScopeKind = "run"
	ScopeSession ScopeKind = "session"
	ScopeAgent   ScopeKind = "agent"
)

// Scope is a namespaced partition of blackboard storage.
type Scope struct {
	Kind ScopeKind
	ID   string
}

// Entry is a single blackboard value.
type Entry struct {
	Key         string            `json:"key"`
	Value       string            `json:"value"`
	Summary     string            `json:"summary,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	SizeBytes   int               `json:"size_bytes"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Limits caps storage per scope kind.
//
// When Evict is false (the default), Set returns an error once a limit is hit —
// the original strict contract. When Evict is true, Set instead evicts the
// least-recently-updated entries to make room. Eviction keeps the blackboard
// usable across long, multi-subtask runs that would otherwise fill the scope and
// block every subsequent write (and thus stall the agent) once the cap is reached.
type Limits struct {
	MaxEntries    int
	MaxTotalBytes int64
	Evict         bool
}

// DefaultLimits returns conservative defaults for Phase 1.
//
// All platform scopes evict oldest-first when full so that long-running complex
// tasks (many subtask results, accumulated error context) keep recording the most
// recent context instead of failing writes once the cap is reached.
func DefaultLimits() map[ScopeKind]Limits {
	return map[ScopeKind]Limits{
		ScopeRun:     {MaxEntries: 200, MaxTotalBytes: 10 * 1024 * 1024, Evict: true},
		ScopeSession: {MaxEntries: 500, MaxTotalBytes: 50 * 1024 * 1024, Evict: true},
		ScopeAgent:   {MaxEntries: 1000, MaxTotalBytes: 20 * 1024 * 1024, Evict: true},
	}
}
