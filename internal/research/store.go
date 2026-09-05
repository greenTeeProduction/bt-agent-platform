// Package research persists a deduplicating knowledge index of research
// findings as JSON under ~/.go-bt-evolve (ADR-003: atomic tmp+rename writes).
// The Obsidian vault stays the full-text document store; this index is what
// scheduled research agents consult before reporting, so a finding that was
// already recorded in an earlier cycle is never re-reported as new.
package research

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nico/go-bt-evolve/internal/util"
)

// excerptLimit bounds the stored excerpt per entry; the vault keeps full text.
const excerptLimit = 400

// Entry is one recorded piece of research knowledge, keyed by content hash.
type Entry struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"` // first recorder, e.g. "bt_fusion:pattern" or "vault:<file>"
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	SeenCount int       `json:"seen_count"`
}

// Store is the on-disk knowledge index.
type Store struct {
	path    string
	Entries map[string]*Entry `json:"entries"`
}

// DefaultPath is the ADR-003 location of the shared research knowledge index.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/nico"
	}
	return filepath.Join(home, ".go-bt-evolve", "research", "knowledge.json")
}

// Open loads the store at path; a missing file yields an empty store, a
// corrupt file errors so a broken index is never silently clobbered.
func Open(path string) (*Store, error) {
	s := &Store{path: path, Entries: map[string]*Entry{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("research store %s: %w", path, err)
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("research store %s is corrupt: %w", path, err)
	}
	if s.Entries == nil {
		s.Entries = map[string]*Entry{}
	}
	return s, nil
}

// Key is the content-hash identity of a finding; whitespace runs collapse so
// reflowed markdown still deduplicates.
func Key(content string) string {
	sum := sha256.Sum256([]byte(normalize(content)))
	return hex.EncodeToString(sum[:])[:16]
}

// Known reports whether content is already recorded, without mutating.
func (s *Store) Known(content string) bool {
	_, ok := s.Entries[Key(content)]
	return ok
}

// Record adds content to the index and reports whether it was new knowledge.
// Re-seen content keeps its first recorder and only bumps LastSeen/SeenCount.
func (s *Store) Record(source, title, content string) bool {
	k := Key(content)
	now := time.Now().UTC()
	if e, ok := s.Entries[k]; ok {
		e.LastSeen = now
		e.SeenCount++
		return false
	}
	s.Entries[k] = &Entry{
		ID:        k,
		Source:    source,
		Title:     title,
		Excerpt:   truncateRunes(normalize(content), excerptLimit),
		FirstSeen: now,
		LastSeen:  now,
		SeenCount: 1,
	}
	return true
}

// Len is the number of recorded entries.
func (s *Store) Len() int { return len(s.Entries) }

// Save writes the index atomically (tmp+rename) per ADR-003.
func (s *Store) Save() error {
	return util.SaveJSONAtomic(s.path, s)
}

func normalize(content string) string {
	return strings.Join(strings.Fields(content), " ")
}

// truncateRunes bounds s to at most limit bytes without splitting a rune.
func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
