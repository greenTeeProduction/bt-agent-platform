package blackboard

import (
	"fmt"
	"time"
)

// Handle is a scoped accessor attached to a single agent run.
type Handle struct {
	Mgr       *Manager
	RunID     string
	SessionID string
	AgentName string
}

// NewHandle binds a manager to a run (and optional session/agent metadata).
func NewHandle(mgr *Manager, runID, sessionID, agentName string) *Handle {
	if mgr == nil {
		mgr = DefaultManager()
	}
	return &Handle{
		Mgr:       mgr,
		RunID:     runID,
		SessionID: sessionID,
		AgentName: agentName,
	}
}

func (h *Handle) runScope() Scope {
	return Scope{Kind: ScopeRun, ID: h.RunID}
}

// RunScope returns the run scope for this handle.
func (h *Handle) RunScope() Scope { return h.runScope() }

// Get reads a key from the run scope.
func (h *Handle) Get(key string) (Entry, error) {
	if h == nil || h.Mgr == nil || h.RunID == "" {
		return Entry{}, fmt.Errorf("blackboard handle not configured")
	}
	return h.Mgr.Get(h.runScope(), key)
}

// Set writes a key to the run scope.
func (h *Handle) Set(key, value, summary, contentType string) error {
	if h == nil || h.Mgr == nil || h.RunID == "" {
		return fmt.Errorf("blackboard handle not configured")
	}
	return h.Mgr.Set(h.runScope(), key, value, summary, contentType)
}

// Delete removes a key from the run scope.
func (h *Handle) Delete(key string) error {
	if h == nil || h.Mgr == nil || h.RunID == "" {
		return fmt.Errorf("blackboard handle not configured")
	}
	return h.Mgr.Delete(h.runScope(), key)
}

// List lists keys in the run scope.
func (h *Handle) List(prefix string, limit int) ([]Entry, error) {
	if h == nil || h.Mgr == nil || h.RunID == "" {
		return nil, fmt.Errorf("blackboard handle not configured")
	}
	return h.Mgr.List(h.runScope(), prefix, limit)
}

func (h *Handle) sessionScope() (Scope, error) {
	if h == nil || h.SessionID == "" {
		return Scope{}, fmt.Errorf("session scope not configured")
	}
	return Scope{Kind: ScopeSession, ID: h.SessionID}, nil
}

// GetSession reads a key from the pipeline session scope.
func (h *Handle) GetSession(key string) (Entry, error) {
	if h == nil || h.Mgr == nil {
		return Entry{}, fmt.Errorf("blackboard handle not configured")
	}
	scope, err := h.sessionScope()
	if err != nil {
		return Entry{}, err
	}
	return h.Mgr.Get(scope, key)
}

// SetSession writes a key to the pipeline session scope.
func (h *Handle) SetSession(key, value, summary, contentType string) error {
	if h == nil || h.Mgr == nil {
		return fmt.Errorf("blackboard handle not configured")
	}
	scope, err := h.sessionScope()
	if err != nil {
		return err
	}
	return h.Mgr.Set(scope, key, value, summary, contentType)
}

// ListSession lists keys in the pipeline session scope.
func (h *Handle) ListSession(prefix string, limit int) ([]Entry, error) {
	if h == nil || h.Mgr == nil {
		return nil, fmt.Errorf("blackboard handle not configured")
	}
	scope, err := h.sessionScope()
	if err != nil {
		return nil, err
	}
	return h.Mgr.List(scope, prefix, limit)
}

// NewRunID generates a unique run identifier.
func NewRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UnixNano())
}
