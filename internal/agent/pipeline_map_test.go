package agent

import (
	"os"
	"testing"
)

func TestRemoveAgentJobs(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("BT_AGENT_HOME")
	t.Setenv("BT_AGENT_HOME", dir)
	t.Cleanup(func() {
		if origHome == "" {
			os.Unsetenv("BT_AGENT_HOME")
		} else {
			t.Setenv("BT_AGENT_HOME", origHome)
		}
	})

	store := NewFileJobStore(SchedulerJobsFile())
	if err := store.Save([]ScheduledJob{
		{ID: "a", AgentName: "keep-me", Schedule: "every 1h", Active: true},
		{ID: "b", AgentName: "drop-me", Schedule: "every 1h", Active: true},
	}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveAgentJobs("drop-me"); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].AgentName != "keep-me" {
		t.Fatalf("unexpected jobs after remove: %+v", jobs)
	}
}

func TestResolvePipelineAgent_Aliases(t *testing.T) {
	cases := map[string]string{
		"notebooklm":        "notebooklm-bridge",
		"notebooklm-bridge": "notebooklm-bridge",
		"session-indexer":   "notebooklm-consumer",
		"hermes-researcher": "research:deep_research",
	}
	for name, want := range cases {
		if got := ResolvePipelineAgent(name); got != want {
			t.Fatalf("ResolvePipelineAgent(%q) = %q, want %q", name, got, want)
		}
	}
}
