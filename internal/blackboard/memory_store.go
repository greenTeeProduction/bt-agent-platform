package blackboard

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type scopedStore struct {
	mu       sync.RWMutex
	entries  map[string]Entry
	limits   Limits
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

	size := int64(len(entry.Value))
	if old, ok := s.entries[key]; ok {
		s.totalBytes -= int64(len(old.Value))
	}
	if s.limits.MaxEntries > 0 && len(s.entries) >= s.limits.MaxEntries {
		if _, exists := s.entries[key]; !exists {
			return fmt.Errorf("blackboard entry limit reached (%d)", s.limits.MaxEntries)
		}
	}
	if s.limits.MaxTotalBytes > 0 && s.totalBytes+size > s.limits.MaxTotalBytes {
		return fmt.Errorf("blackboard byte limit reached")
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
