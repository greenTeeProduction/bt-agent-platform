package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
)

func TestListAgentsFromRegistry(t *testing.T) {
	dir := t.TempDir()
	regDir := filepath.Join(dir, "agents")
	histDir := filepath.Join(dir, "history")
	_ = os.MkdirAll(regDir, 0755)
	_ = os.MkdirAll(histDir, 0755)

	t.Setenv("BT_AGENT_HOME", dir)

	reg, err := agent.NewRegistry(regDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Create(agent.Definition{
		Name:        "code-reviewer",
		Description: "Review PRs",
		Tree:        "domain:code_review",
		Schedule:    "on_demand",
	})
	if err != nil {
		t.Fatal(err)
	}

	agents := ListAgentsWithCB()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "code-reviewer" {
		t.Fatalf("unexpected agent: %+v", agents[0])
	}
	if agents[0].Tree != "domain:code_review" {
		t.Fatalf("unexpected tree: %s", agents[0].Tree)
	}
}

func TestDefaultAgentTask(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_AGENT_HOME", dir)
	reg, err := agent.NewRegistry(filepath.Join(dir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Create(agent.Definition{
		Name:        "monitor",
		Description: "Check disk and memory",
		Tree:        "domain:agent_monitor",
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := DefaultAgentTask("monitor"); got != "Check disk and memory" {
		t.Fatalf("expected description as task, got %q", got)
	}
	if got := DefaultAgentTask("missing"); got != "missing" {
		t.Fatalf("expected agent name fallback, got %q", got)
	}
}

func TestCreateAgent_RegistersSchedule(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_AGENT_HOME", dir)

	if err := CreateAgent(AgentYAMLConfig{
		Name:        "hourly",
		Description: "Hourly check",
		Tree:        "domain:default",
		Schedule:    "every 1h",
	}); err != nil {
		t.Fatal(err)
	}

	store := agent.NewFileJobStore(agent.SchedulerJobsFile())
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].AgentName != "hourly" || jobs[0].Schedule != "every 1h" {
		t.Fatalf("unexpected scheduler jobs: %+v", jobs)
	}
}

func TestDeleteAgent_ClearsSchedulerJobs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_AGENT_HOME", dir)

	if err := CreateAgent(AgentYAMLConfig{
		Name:        "scheduled-agent",
		Description: "Temp",
		Tree:        "domain:default",
		Schedule:    "every 1h",
	}); err != nil {
		t.Fatal(err)
	}

	if err := DeleteAgent("scheduled-agent"); err != nil {
		t.Fatal(err)
	}

	store := agent.NewFileJobStore(agent.SchedulerJobsFile())
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.AgentName == "scheduled-agent" {
			t.Fatalf("expected scheduler jobs cleared, still have %+v", jobs)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "scheduled-agent.yaml")); !os.IsNotExist(err) {
		t.Fatal("expected agent yaml removed")
	}
}
