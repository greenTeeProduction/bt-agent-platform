package agent

import "testing"

func TestExtractCycleFacts(t *testing.T) {
	out := "## GOAP Superpowers Runtime Complete\n\nRun: `r1`\nApply status: `committed`\nCommit: `abc1234`\n\narc42 sync: updated docs/arc42/x.md\n\nPARTIAL LANDING: task 3 carried forward\n[P0] Program \"X\" milestone 2/5"
	f := extractCycleFacts(out)
	if f["commit"] != "abc1234" || f["apply"] != "committed" {
		t.Fatalf("commit/apply: %+v", f)
	}
	if f["partial_landing"] != true || f["arc42_synced"] != true {
		t.Fatalf("flags: %+v", f)
	}
	if f["program_milestone"] != "2/5" {
		t.Fatalf("milestone: %+v", f)
	}
	// A no-op cycle.
	if extractCycleFacts("## No New Research\n\nAll findings already recorded.")["noop"] != true {
		t.Fatal("noop not detected")
	}
	// arc42 with no impact must not flag synced.
	if extractCycleFacts("arc42 sync: no documentation impact")["arc42_synced"] == true {
		t.Fatal("no-impact arc42 must not flag synced")
	}
}
