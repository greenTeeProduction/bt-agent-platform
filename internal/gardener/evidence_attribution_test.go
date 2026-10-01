package gardener

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestUnrelatedAndUnownedRecordsCannotSatisfyEvolutionEvidence(t *testing.T) {
	records := []evolution.Record{
		{TaskID: "unattributed", Outcome: evolution.Success},
		{TaskID: "other-tree", TreeName: "other", Outcome: evolution.Success},
		{TaskID: "other-user", TreeName: "goal:task", User: "alice", Outcome: evolution.Success},
		{TaskID: "legacy-user", TreeName: "goal:task", Outcome: evolution.Success},
	}
	for _, entry := range []TreeEntry{{Name: "new-tree"}, {Name: "goal:task", User: "bob"}} {
		if got := recordsForEntry(records, entry); len(got) != 0 {
			t.Errorf("%+v inherited unrelated evidence: %+v", entry, got)
		}
	}
}

func TestSharedCatalogEvidenceMatchesRuntimeIDButNotPersonalOwner(t *testing.T) {
	records := []evolution.Record{
		{TaskID: "shared", TreeName: "domain:code_review", Outcome: evolution.Success},
		{TaskID: "personal", TreeName: "domain:code_review", User: "alice", Outcome: evolution.Failure},
	}
	got := recordsForEntry(records, TreeEntry{Name: "domain_code_review"})
	if len(got) != 1 || got[0].TaskID != "shared" {
		t.Fatalf("catalog identity/ownership mismatch: %+v", got)
	}
}
