package blackboard

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type scopedStore struct {
	mu         sync.RWMutex
	entries    map[string]Entry
	limits     Limits
	totalBytes int64
}

func newScopedStore(limits Limits) *scopedStore {
	return &scopedStore{
		entries: make(map[string]Entry),
		limits:  limits,
	}
}

func (s *scopedStore) get(key string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[key]
	return e, ok
}

func (s *scopedStore) set(key string, entry Entry) error {
	key = normalizeKey(key)
	if key == "" {
		return fmt.Errorf("blackboard key required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLocked(key, entry)
}

// appendVal atomically appends value to the entry at key, creating it when
// absent. When the key already holds content, sep is inserted between the
// existing value and the new value. The whole read-modify-write happens under
// the store lock so concurrent appends to a shared scope (e.g. a pipeline
// session accumulating subtask results) never lose updates. Limits and
// eviction are enforced exactly as set does. The resulting entry is returned.
func (s *scopedStore) appendVal(key, value, sep, contentType string) (Entry, error) {
	key = normalizeKey(key)
	if key == "" {
		return Entry{}, fmt.Errorf("blackboard key required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.entries[key]
	newVal := value
	if ok && existing.Value != "" {
		if sep != "" {
			newVal = existing.Value + sep + value
		} else {
			newVal = existing.Value + value
		}
	}
	entry := Entry{Key: key, Value: newVal, ContentType: contentType}
	if ok {
		entry.CreatedAt = existing.CreatedAt
		if contentType == "" {
			entry.ContentType = existing.ContentType
		}
	}
	if err := s.setLocked(key, entry); err != nil {
		return Entry{}, err
	}
	return s.entries[key], nil
}

// setLocked writes entry under key. Callers must hold s.mu.
func (s *scopedStore) setLocked(key string, entry Entry) error {
	size := int64(len(entry.Value))
	_, isUpdate := s.entries[key]
	if isUpdate {
		s.totalBytes -= int64(len(s.entries[key].Value))
	}

	// Entry-count limit. A new key needs a free slot; an update reuses its own.
	if s.limits.MaxEntries > 0 && !isUpdate && len(s.entries) >= s.limits.MaxEntries {
		if !s.limits.Evict {
			return fmt.Errorf("blackboard entry limit reached (%d)", s.limits.MaxEntries)
		}
		for len(s.entries) >= s.limits.MaxEntries {
			if !s.evictOldest(key) {
				return fmt.Errorf("blackboard entry limit reached (%d)", s.limits.MaxEntries)
			}
		}
	}

	// Byte limit. Evict oldest entries until the incoming value fits.
	if s.limits.MaxTotalBytes > 0 && s.totalBytes+size > s.limits.MaxTotalBytes {
		if !s.limits.Evict {
			return fmt.Errorf("blackboard byte limit reached")
		}
		for s.totalBytes+size > s.limits.MaxTotalBytes {
			if !s.evictOldest(key) {
				return fmt.Errorf("blackboard byte limit reached: value too large (%d bytes)", size)
			}
		}
	}

	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	entry.Key = key
	entry.SizeBytes = len(entry.Value)
	if entry.ContentType == "" {
		entry.ContentType = "text"
	}
	if entry.Summary == "" {
		entry.Summary = truncateSummary(entry.Value, 500)
	}

	s.entries[key] = entry
	s.totalBytes += size
	return nil
}

func (s *scopedStore) delete(key string) error {
	key = normalizeKey(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.entries[key]
	if !ok {
		return fmt.Errorf("key %q not found", key)
	}
	delete(s.entries, key)
	s.totalBytes -= int64(len(old.Value))
	return nil
}

// evictOldest removes the least-recently-updated entry (excluding exceptKey, the
// key currently being written) to free a slot/bytes. Callers must hold s.mu.
// Returns false when there is nothing left to evict.
func (s *scopedStore) evictOldest(exceptKey string) bool {
	var oldestKey string
	var oldestAt time.Time
	found := false
	for k, e := range s.entries {
		if k == exceptKey {
			continue
		}
		if !found || e.UpdatedAt.Before(oldestAt) {
			oldestKey, oldestAt, found = k, e.UpdatedAt, true
		}
	}
	if !found {
		return false
	}
	s.totalBytes -= int64(len(s.entries[oldestKey].Value))
	delete(s.entries, oldestKey)
	return true
}

func (s *scopedStore) list(prefix string, limit int) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	prefix = normalizeKey(prefix)
	out := make([]Entry, 0, 8)
	for k, e := range s.entries {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.Trim(key, "/")
	key = strings.ReplaceAll(key, "..", "")
	return key
}

func truncateSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
