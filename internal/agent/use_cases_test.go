package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── All agent-platform use cases in one test ───

func TestUseCase_FullAgentLifecycle(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	// 1. Create
	inst, err := reg.Create(Definition{Name: "lifecycle", Tree: "domain:default", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.State != StateCreated {
		t.Error("state")
	}

	// 2. Get + verify
	got, _ := reg.Get("lifecycle")
	if got.Definition.Tree != "domain:default" {
		t.Error("tree")
	}

	// 3. Update state
	_ = reg.UpdateState("lifecycle", StateRunning, "")
	got2, _ := reg.Get("lifecycle")
	if got2.State != StateRunning {
		t.Error("state update")
	}

	// 4. Record runs
	for i := 0; i < 3; i++ {
		_ = hist.Record(RunRecord{AgentName: "lifecycle", Outcome: "success", Duration: "1s", Quality: 0.9})
	}
	_ = hist.Record(RunRecord{AgentName: "lifecycle", Outcome: "failure", Duration: "2s", Quality: 0.3})

	// 5. Check stats
	stats := hist.Stats("lifecycle")
	if stats.TotalRuns != 4 {
		t.Errorf("runs: %d", stats.TotalRuns)
	}
	if stats.SuccessRate != 0.75 {
		t.Errorf("rate: %.2f", stats.SuccessRate)
	}

	// 6. List
	agents := reg.List()
	if len(agents) != 1 {
		t.Error("list")
	}

	// 7. History
	runs := hist.List("lifecycle", 3)
	if len(runs) != 3 {
		t.Error("history limit")
	}

	// 8. Delete
	_ = reg.Delete("lifecycle")
	if _, err := reg.Get("lifecycle"); err == nil {
		t.Error("should be deleted")
	}
}

func TestUseCase_CatalogSearchExport(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	tmplDir := filepath.Join(dir, "templates")
	_ = os.MkdirAll(tmplDir, 0755)
	_ = os.WriteFile(filepath.Join(tmplDir, "search-agent.yaml"), []byte("name: search-agent\ndescription: A searchable test agent\ntree: domain:code_review\ncategory: testing\ntags: \"test,search,example\""), 0644)

	cat := NewCatalog(reg, tmplDir)

	// Install
	inst, err := cat.InstallFromTemplate("search-agent")
	if err != nil {
		t.Fatal(err)
	}
	_ = inst

	// Search by name
	if len(cat.Search("search")) == 0 {
		t.Error("search by name")
	}

	// Search by tag
	if len(cat.Search("example")) == 0 {
		t.Error("search by tag")
	}

	// List installed
	entries := cat.ListInstalled()
	if len(entries) != 1 {
		t.Error("installed")
	}

	// List templates
	tmpls, _ := cat.ListTemplates()
	if len(tmpls) != 1 {
		t.Error("templates")
	}

	// Export
	outPath := filepath.Join(dir, "exported.yaml")
	_ = cat.Export("search-agent", outPath)
	data, _ := os.ReadFile(outPath)
	if len(data) < 30 {
		t.Error("export")
	}
}

func TestUseCase_SchedulerRunAndHistory(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	_, _ = reg.Create(Definition{Name: "scheduled", Tree: "domain:default", Version: "1.0.0"})
	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})

	// Schedule
	job, err := sched.Schedule("scheduled", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}
	if job.AgentName != "scheduled" {
		t.Error("agent name")
	}

	// Run now
	runner := func(_ RunContext) (string, string, error) {
		return "success", "executed", nil
	}
	outcome, _, err := sched.RunNow("scheduled", "test", runner, "10s")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "success" {
		t.Error("outcome")
	}

	// Verify history
	runs := hist.List("scheduled", 5)
	if len(runs) != 1 {
		t.Errorf("expected 1 history record, got %d", len(runs))
	}

	// Cleanup history
	removed, _ := hist.Cleanup(0)
	_ = removed

	// All stats
	allStats := hist.AllStats()
	_ = allStats
}

func TestUseCase_DuplicateHandling(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	_, _ = reg.Create(Definition{Name: "dup", Tree: "domain:default", Version: "1.0.0"})
	_, err := reg.Create(Definition{Name: "dup", Tree: "domain:default", Version: "1.0.0"})
	if err == nil {
		t.Error("duplicate should error")
	}

	_, err = reg.Get("nonexistent")
	if err == nil {
		t.Error("nonexistent should error")
	}

	err = reg.Delete("nonexistent")
	if err == nil {
		t.Error("delete nonexistent should error")
	}
}

func TestUseCase_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	agents := reg.List()
	if len(agents) != 0 {
		t.Error("empty registry should have 0 agents")
	}
}

func TestUseCase_PersistenceAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	reg1, _ := NewRegistry(dir)
	_, _ = reg1.Create(Definition{Name: "persist", Tree: "domain:default", Description: "test persist", Version: "1.0.0"})

	// New instance loads from same dir
	reg2, _ := NewRegistry(dir)
	inst, err := reg2.Get("persist")
	if err != nil {
		t.Fatal("should find persisted agent:", err)
	}
	if inst.Definition.Description != "test persist" {
		t.Error("description lost")
	}
}
