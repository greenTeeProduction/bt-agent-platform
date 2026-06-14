package blackboard

import (
	"fmt"
	"sync"
)

// Manager stores entries partitioned by scope.
type Manager struct {
	mu         sync.RWMutex
	stores     map[string]*scopedStore
	limits     map[ScopeKind]Limits
	persistDir string
}

// NewManager creates a blackboard manager with the given per-scope limits.
func NewManager(limits map[ScopeKind]Limits) *Manager {
	if limits == nil {
		limits = DefaultLimits()
	}
	return &Manager{
		stores: make(map[string]*scopedStore),
		limits: limits,
	}
}

// DefaultManager uses platform default limits.
func DefaultManager() *Manager {
	return NewManager(DefaultLimits())
}

func (m *Manager) scopeID(scope Scope) (string, error) {
	if scope.ID == "" {
		return "", fmt.Errorf("scope id required for %q", scope.Kind)
	}
	return fmt.Sprintf("%s:%s", scope.Kind, scope.ID), nil
}

func (m *Manager) storeFor(scope Scope) (*scopedStore, error) {
	id, err := m.scopeID(scope)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	if s, ok := m.stores[id]; ok {
		m.mu.RUnlock()
		return s, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stores[id]; ok {
		return s, nil
	}
	lim, ok := m.limits[scope.Kind]
	if !ok {
		lim = Limits{MaxEntries: 200, MaxTotalBytes: 10 * 1024 * 1024}
	}
	s := newScopedStore(lim)
	if m.persistDir != "" && isPersistentScope(scope.Kind) {
		_ = loadScopeFile(m.persistFile(scope), s)
	}
	m.stores[id] = s
	return s, nil
}

// Get returns an entry from a scope.
func (m *Manager) Get(scope Scope, key string) (Entry, error) {
	s, err := m.storeFor(scope)
	if err != nil {
		return Entry{}, err
	}
	e, ok := s.get(key)
	if !ok {
		return Entry{}, fmt.Errorf("key %q not found in %s", key, scope.Kind)
	}
	return e, nil
}

// Set writes an entry to a scope.
func (m *Manager) Set(scope Scope, key, value, summary, contentType string) error {
	s, err := m.storeFor(scope)
	if err != nil {
		return err
	}
	if err := s.set(key, Entry{
		Key:         key,
		Value:       value,
		Summary:     summary,
		ContentType: contentType,
	}); err != nil {
		return err
	}
	return m.persistScope(scope, s)
}

// Delete removes a key from a scope.
func (m *Manager) Delete(scope Scope, key string) error {
	s, err := m.storeFor(scope)
	if err != nil {
		return err
	}
	if err := s.delete(key); err != nil {
		return err
	}
	return m.persistScope(scope, s)
}

// List returns entries in a scope with an optional key prefix.
func (m *Manager) List(scope Scope, prefix string, limit int) ([]Entry, error) {
	s, err := m.storeFor(scope)
	if err != nil {
		return nil, err
	}
	return s.list(prefix, limit), nil
}
