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
type Limits struct {
	MaxEntries   int
	MaxTotalBytes int64
}

// DefaultLimits returns conservative defaults for Phase 1.
func DefaultLimits() map[ScopeKind]Limits {
	return map[ScopeKind]Limits{
		ScopeRun:     {MaxEntries: 200, MaxTotalBytes: 10 * 1024 * 1024},
		ScopeSession: {MaxEntries: 500, MaxTotalBytes: 50 * 1024 * 1024},
		ScopeAgent:   {MaxEntries: 1000, MaxTotalBytes: 20 * 1024 * 1024},
	}
}
