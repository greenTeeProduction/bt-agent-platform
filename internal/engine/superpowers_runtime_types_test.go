package engine

import "testing"

func TestSuperpowersRunState(t *testing.T) {
	bb := &Blackboard{}
	run := &SuperpowersRun{ID: "run-1", Task: "test", Mode: SuperpowersModeDryRun}
	setSuperpowersRun(bb, run)
	got, ok := getSuperpowersRun(bb)
	if !ok {
		t.Fatal("expected run in blackboard")
	}
	if got.ID != run.ID || got.Mode != SuperpowersModeDryRun {
		t.Fatalf("unexpected run: %#v", got)
	}
}

func TestSuperpowersModeFromTask(t *testing.T) {
	if superpowersModeFromTask("dry_run: test") != SuperpowersModeDryRun {
		t.Fatal("dry_run task should select dry-run mode")
	}
	if superpowersModeFromTask("implement feature") != SuperpowersModeApply {
		t.Fatal("normal task should select apply mode")
	}
}
