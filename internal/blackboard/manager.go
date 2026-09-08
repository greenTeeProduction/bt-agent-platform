package blackboard

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// scopeLockTimeout bounds the wait for a persisted scope's cross-process
// sidecar flock. A peer that wedges while holding it (stopped process, hung
// filesystem) then surfaces as a failed blackboard operation instead of an
// unbounded hang on the engine's tool path. It is far above observed
// contention: the flock retry ticks every 10ms and each holder only performs a
// load/mutate/save cycle. A var so tests can shorten the wait.
var scopeLockTimeout = 10 * time.Second

// scopeGate serializes operations on one scope. refs is guarded by
// Manager.gatesMu and bounds the gate map: the entry is dropped once its last
// user leaves, so diagnostic reads of unknown scope IDs cannot accumulate
// gates the store map already refuses to keep.
type scopeGate struct {
	mu   sync.Mutex
	refs int
}

// Manager stores entries partitioned by scope.
type Manager struct {
	gatesMu    sync.Mutex
	gates      map[string]*scopeGate
	mu         sync.RWMutex
	stores     map[string]*scopedStore
	storeRoots map[string]string // persistence namespace of each cached persistent scope
	limits     map[ScopeKind]Limits
	persistDir string
}

// NewManager creates a blackboard manager with the given per-scope limits.
func NewManager(limits map[ScopeKind]Limits) *Manager {
	if limits == nil {
		limits = DefaultLimits()
	}
	return &Manager{
		stores:     make(map[string]*scopedStore),
		storeRoots: make(map[string]string),
		gates:      make(map[string]*scopeGate),
		limits:     limits,
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

func (m *Manager) storeFor(dir string, scope Scope) (*scopedStore, error) {
	id, err := m.scopeID(scope)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stores[id]; ok {
		if !isPersistentScope(scope.Kind) {
			return s, nil
		}
		// Preserve in-memory entries when persistence is first enabled, but
		// never carry cached data between different persistence namespaces.
		if root := m.storeRoots[id]; root == dir || root == "" {
			m.storeRoots[id] = dir
			return s, nil
		}
	}
	// A bound also covers orphaned runs and diagnostic reads of unknown IDs.
	if scope.Kind == ScopeRun {
		count := 0
		for key := range m.stores {
			if strings.HasPrefix(key, string(ScopeRun)+":") {
				count++
			}
		}
		if count >= 1024 {
			return nil, fmt.Errorf("run scope limit reached")
		}
	}
	lim, ok := m.limits[scope.Kind]
	if !ok {
		lim = Limits{MaxEntries: 200, MaxTotalBytes: 10 * 1024 * 1024, Evict: true}
	}
	s := newScopedStore(lim)
	// Disk I/O belongs in beginScope under only the per-scope gate. Holding
	// m.mu across a slow read would stall every other scope and reconfiguration.
	if isPersistentScope(scope.Kind) {
		m.storeRoots[id] = dir
	}
	m.stores[id] = s
	return s, nil
}

// Get returns an entry from a scope.
func (m *Manager) Get(scope Scope, key string) (Entry, error) {
	s, _, release, err := m.beginScope(scope)
	if err != nil {
		return Entry{}, err
	}
	defer release()
	e, ok := s.get(key)
	if !ok {
		return Entry{}, fmt.Errorf("key %q not found in %s", key, scope.Kind)
	}
	return e, nil
}

// Set writes an entry to a scope.
func (m *Manager) Set(scope Scope, key, value, summary, contentType string) error {
	s, path, release, err := m.beginScope(scope)
	if err != nil {
		return err
	}
	defer release()
	if err := s.set(key, Entry{
		Key:         key,
		Value:       value,
		Summary:     summary,
		ContentType: contentType,
	}); err != nil {
		return err
	}
	return persistScope(path, s)
}

// Append atomically appends value to the entry at key in scope, joining with
// sep when the key already holds content. It returns the resulting entry. Use
// it to accumulate running context (task history, subtask results, error logs)
// across nodes without a separate read-modify-write that could race on a shared
// scope.
func (m *Manager) Append(scope Scope, key, value, sep, contentType string) (Entry, error) {
	s, path, release, err := m.beginScope(scope)
	if err != nil {
		return Entry{}, err
	}
	defer release()
	e, err := s.appendVal(key, value, sep, contentType)
	if err != nil {
		return Entry{}, err
	}
	if err := persistScope(path, s); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Delete removes a key from a scope.
func (m *Manager) Delete(scope Scope, key string) error {
	s, path, release, err := m.beginScope(scope)
	if err != nil {
		return err
	}
	defer release()
	if err := s.delete(key); err != nil {
		return err
	}
	return persistScope(path, s)
}

// List returns entries in a scope with an optional key prefix.
func (m *Manager) List(scope Scope, prefix string, limit int) ([]Entry, error) {
	s, _, release, err := m.beginScope(scope)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.list(prefix, limit), nil
}

// ListRecent returns entries in a scope with an optional key prefix, ordered by
// most-recently-updated first and capped to limit. Where List sorts by key —
// and so hides the newest entries behind the limit once a scope fills — ListRecent
// always surfaces the latest context (recent subtask results, error logs), which
// is what nodes need for error recovery and context management across a long
// multi-step run.
func (m *Manager) ListRecent(scope Scope, prefix string, limit int) ([]Entry, error) {
	s, _, release, err := m.beginScope(scope)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.listRecent(prefix, limit), nil
}

// acquireGate locks the gate for one scope, creating it on first use.
// Different scopes never contend: engine tools share a single Manager across
// run and session scopes, so a session scope waiting on a peer process's flock
// must leave unrelated run-scope reads and writes running — run and task
// scopes are never persisted and take no file lock at all.
func (m *Manager) acquireGate(id string) *scopeGate {
	m.gatesMu.Lock()
	if m.gates == nil {
		m.gates = make(map[string]*scopeGate)
	}
	g, ok := m.gates[id]
	if !ok {
		g = &scopeGate{}
		m.gates[id] = g
	}
	g.refs++
	m.gatesMu.Unlock()
	g.mu.Lock()
	return g
}

func (m *Manager) releaseGate(id string, g *scopeGate) {
	g.mu.Unlock()
	m.gatesMu.Lock()
	g.refs--
	if g.refs == 0 {
		delete(m.gates, id)
	}
	m.gatesMu.Unlock()
}

// beginScope serializes snapshot and persistence with mutation for one scope,
// and returns that scope's store, its persistence path ("" when the scope is
// not persisted) and the release func the caller must invoke. A file lock
// additionally makes reload/modify/save authoritative across independent
// managers/processes; it is taken per scope and under a deadline, so a wedged
// peer fails this one scope's operation rather than stalling the Manager.
// The persistence root is resolved once here and threaded through the whole
// operation so a load/mutate/save cycle cannot straddle EnablePersistence.
func (m *Manager) beginScope(scope Scope) (*scopedStore, string, func(), error) {
	id, err := m.scopeID(scope)
	if err != nil {
		return nil, "", nil, err
	}
	gate := m.acquireGate(id)
	// Resolve after joining the scope's serial order: queued operations must
	// not retain a root captured before another operation/reconfiguration.
	dir := m.persistRoot()
	path := scopePath(dir, scope)
	release := func() { m.releaseGate(id, gate) }
	if path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), scopeLockTimeout)
		unlock, lockErr := reliability.AcquireFileLockWithContext(ctx, path)
		cancel()
		if lockErr != nil {
			release()
			return nil, "", nil, fmt.Errorf("blackboard scope %s: %w", id, lockErr)
		}
		release = func() {
			unlock()
			m.releaseGate(id, gate)
		}
	}
	s, err := m.storeFor(dir, scope)
	if err == nil && path != "" {
		err = loadScope(dir, scope, s)
	}
	if err != nil {
		release()
		return nil, "", nil, err
	}
	return s, path, release, nil
}

// ReleaseRun drops ephemeral data after the run's final consumers finish.
func (m *Manager) ReleaseRun(runID string) {
	id := string(ScopeRun) + ":" + runID
	gate := m.acquireGate(id)
	defer m.releaseGate(id, gate)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.stores, id)
}
