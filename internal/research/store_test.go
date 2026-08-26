package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenMissingFileYieldsEmptyStore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("Open on missing file: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("expected empty store, got %d entries", s.Len())
	}
}

func TestRecordDeduplicatesByNormalizedContent(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "knowledge.json"))
	if !s.Record("bt_fusion:pattern", "finding", "Typed-edge validation: preserve guard semantics.") {
		t.Fatal("first Record must report new knowledge")
	}
	// Same content reflowed across lines and extra whitespace must dedupe.
	if s.Record("vault:note.md", "finding", "Typed-edge validation:\n  preserve guard\tsemantics.") {
		t.Fatal("reflowed duplicate must not report as new")
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", s.Len())
	}
	e := s.Entries[Key("Typed-edge validation: preserve guard semantics.")]
	if e == nil {
		t.Fatal("entry not found under content key")
	}
	if e.SeenCount != 2 {
		t.Fatalf("SeenCount = %d, want 2", e.SeenCount)
	}
	if e.Source != "bt_fusion:pattern" {
		t.Fatalf("Source must keep first recorder, got %q", e.Source)
	}
}

func TestSaveAndReopenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "knowledge.json")
	s, _ := Open(path)
	s.Record("bt_fusion:pattern", "a", "alpha finding")
	s.Record("vault:x.md", "b", "beta finding")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Atomic write: no tmp file left behind.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file left behind: %v", err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if r.Len() != 2 {
		t.Fatalf("expected 2 entries after reopen, got %d", r.Len())
	}
	if r.Record("bt_fusion:pattern", "a", "alpha finding") {
		t.Fatal("persisted knowledge must still deduplicate after reopen")
	}
}

func TestOpenCorruptFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open on corrupt file must error, not silently clobber")
	}
}

func TestExcerptIsBoundedAndRuneSafe(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "knowledge.json"))
	var long strings.Builder
	for range 200 {
		long.WriteString("Grüße-") // multi-byte runes across the truncation boundary
	}
	s.Record("vault:big.md", "big", long.String())
	for _, e := range s.Entries {
		if len(e.Excerpt) > excerptLimit+4 {
			t.Fatalf("excerpt too long: %d bytes", len(e.Excerpt))
		}
		for _, r := range e.Excerpt {
			if r == '�' {
				t.Fatal("excerpt truncation split a rune")
			}
		}
	}
}
