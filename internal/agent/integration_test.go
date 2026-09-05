package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ─── Registry Integration ───

func TestRegistry_FullLifecycle(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Create
	inst, err := reg.Create(Definition{Name: "lifecycle", Tree: "domain:default", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.State != StateCreated {
		t.Errorf("expected created state, got %s", inst.State)
	}

	// Get
	got, err := reg.Get("lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if got.Definition.Tree != "domain:default" {
		t.Errorf("wrong tree: %s", got.Definition.Tree)
	}

	// Update state
	if err := reg.UpdateState("lifecycle", StateRunning, ""); err != nil {
		t.Fatal(err)
	}
	got2, _ := reg.Get("lifecycle")
	if got2.State != StateRunning {
		t.Errorf("expected running, got %s", got2.State)
	}

	// Delete
	if err := reg.Delete("lifecycle"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Get("lifecycle"); err == nil {
		t.Error("should not find deleted agent")
	}
}

func TestRegistry_DuplicateCreate(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "dup", Tree: "domain:default"})
	_, err := reg.Create(Definition{Name: "dup", Tree: "domain:default"})
	if err == nil {
		t.Error("duplicate create should fail")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "persist", Tree: "domain:default", Description: "test", Version: "1.0.0"})

	// Reload
	reg2, _ := NewRegistry(dir)
	inst, err := reg2.Get("persist")
	if err != nil {
		t.Fatal("should persist across registry instances:", err)
	}
	if inst.Definition.Description != "test" {
		t.Errorf("description lost: %s", inst.Definition.Description)
	}
}

// ─── Catalog Integration ───

func TestCatalog_InstallAndSearch(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	tmplDir := filepath.Join(dir, "templates")
	_ = os.MkdirAll(tmplDir, 0755)
	_ = os.WriteFile(filepath.Join(tmplDir, "test-agent.yaml"), []byte(`name: test-agent
description: Test agent for catalog
tree: domain:default
category: testing
tags: "test,example"`), 0644)

	cat := NewCatalog(reg, tmplDir)

	// Install from template
	inst, err := cat.InstallFromTemplate("test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Definition.Name != "test-agent" {
		t.Errorf("wrong name: %s", inst.Definition.Name)
	}

	// Search
	results := cat.Search("example")
	if len(results) == 0 {
		t.Error("should find by tag")
	}

	// Export
	outPath := filepath.Join(dir, "exported.yaml")
	if err := cat.Export("test-agent", outPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(outPath)
	if len(data) < 40 {
		t.Error("exported file too short")
	}
}

// ─── History Integration ───

func TestHistory_Cleanup(t *testing.T) {
	dir := t.TempDir()
	h, _ := NewHistory(dir)
	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now()

	_ = h.Record(RunRecord{AgentName: "clean", Outcome: "success", EndedAt: oldTime})
	_ = h.Record(RunRecord{AgentName: "clean", Outcome: "success", EndedAt: recentTime})

	removed, err := h.Cleanup(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	runs := h.List("clean", 10)
	if len(runs) != 1 {
		t.Errorf("expected 1 run after cleanup, got %d", len(runs))
	}
}

func TestHistory_AllStats(t *testing.T) {
	dir := t.TempDir()
	h, _ := NewHistory(dir)
	_ = h.Record(RunRecord{AgentName: "a", Outcome: "success", Duration: "5s", Quality: 0.9, EndedAt: time.Now()})
	_ = h.Record(RunRecord{AgentName: "b", Outcome: "failure", Duration: "2s", Quality: 0.3, EndedAt: time.Now()})

	allStats := h.AllStats()
	if len(allStats) != 2 {
		t.Errorf("expected 2 agents in stats, got %d", len(allStats))
	}
}

// ─── Scheduler Integration ───

func TestScheduler_RemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)
	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})

	if err := sched.RemoveJob("nonexistent"); err == nil {
		t.Error("should error on nonexistent job")
	}
}

func TestScheduler_UnknownAgent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)
	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})

	_, err := sched.Schedule("nonexistent", "every 1h", "30m", 3)
	if err == nil {
		t.Error("should fail scheduling unknown agent")
	}
}

// ─── Catalog Edge Cases ───

func TestCatalog_EmptyTemplates(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	cat := NewCatalog(reg, "/nonexistent/path")

	tmpls, err := cat.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpls) != 0 {
		t.Errorf("expected 0 templates for nonexistent dir, got %d", len(tmpls))
	}
}

func TestCatalog_InstallMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	cat := NewCatalog(reg, dir)

	_, err := cat.InstallFromTemplate("nonexistent")
	if err == nil {
		t.Error("should fail on missing template")
	}
}
