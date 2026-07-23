package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStore_LoadAll_ExcludesNonReflectionJSONFiles pins the 2026-07-22 fix:
// production shares one directory between the reflection Store, the tree
// Store (tree.json / tree-<id>.json), and the evaluator's transposition
// table (transposition.json). LoadAll must only unmarshal the files Save
// actually writes (reflection-<task-id>.json), not every *.json in the
// directory, or those sibling files decode as zero-value phantom records.
func TestStore_LoadAll_ExcludesNonReflectionJSONFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Save(&Record{TaskID: "real", Task: "real task", Outcome: Success}); err != nil {
		t.Fatal(err)
	}

	// Simulate sibling stores writing into the same directory.
	for _, name := range []string{"tree.json", "tree-agent-a.json", "transposition.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"type":"Sequence"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (sibling JSON files excluded), got %d", len(records))
	}
	if records[0].TaskID != "real" {
		t.Errorf("expected the real record, got %+v", records[0])
	}
}

// TestStore_CountFailures_ExcludesNonReflectionJSONFiles guards the
// consequence of the LoadAll quirk on CountFailures/RecentFailures: a
// phantom tree.json record decodes with Outcome == "" (not Failure), so it
// silently inflates total_tasks without inflating the failure count —
// understating the failure rate rather than the success rate.
func TestStore_CountFailures_ExcludesNonReflectionJSONFiles(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "tree.json"), []byte(`{"type":"Sequence"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if got := store.CountFailures(); got != 0 {
		t.Errorf("expected 0 failures with only a phantom tree.json present, got %d", got)
	}

	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records with only tree.json present, got %d", len(records))
	}
}
