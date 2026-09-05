package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalog_ListInstalled(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}

	cat := NewCatalog(reg, dir)
	inst, err := reg.Create(Definition{
		Name:        "test-agent",
		Description: "A test agent",
		Version:     "1.0.0",
		Tree:        "domain:default",
		Schedule:    "on_demand",
		Metadata: map[string]string{
			"category": "testing",
			"tags":     "test,mock",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = inst

	entries := cat.ListInstalled()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "test-agent" {
		t.Errorf("expected test-agent, got %s", entries[0].Name)
	}
	if entries[0].Category != "testing" {
		t.Errorf("expected testing category, got %s", entries[0].Category)
	}
	if len(entries[0].Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(entries[0].Tags))
	}
}

func TestCatalog_ListTemplates(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Create a template directory with one template
	tmplDir := filepath.Join(dir, "templates")
	_ = os.MkdirAll(tmplDir, 0755)
	_ = os.WriteFile(filepath.Join(tmplDir, "code-reviewer.yaml"), []byte(`name: code-reviewer
description: Reviews code
tree: domain:code_review
category: software-development
tags: "code-review,bugs"`), 0644)

	cat := NewCatalog(reg, tmplDir)
	templates, err := cat.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template, got %d", len(templates))
	}
}

func TestCatalog_Search(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	tmplDir := filepath.Join(dir, "templates")
	_ = os.MkdirAll(tmplDir, 0755)

	_, _ = reg.Create(Definition{
		Name:        "search-test",
		Description: "Searchable agent",
		Version:     "1.0.0",
		Tree:        "domain:default",
		Metadata:    map[string]string{"category": "search", "tags": "find,locate"},
	})

	cat := NewCatalog(reg, tmplDir)
	results := cat.Search("search")
	if len(results) == 0 {
		t.Error("expected search results")
	}
}

func TestCatalog_SkillToAgent(t *testing.T) {
	skillPath := filepath.Join(t.TempDir(), "SKILL.md")
	_ = os.WriteFile(skillPath, []byte(`---
name: test-skill
description: A skill for testing code review
tags: [test, review]
---
# Test Skill
`), 0644)

	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	cat := NewCatalog(reg, dir)

	def, err := cat.SkillToAgent(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "test-skill" {
		t.Errorf("expected test-skill, got %s", def.Name)
	}
	if def.Tree != "domain:code_review" {
		t.Errorf("expected domain:code_review for review skill, got %s", def.Tree)
	}
}

func TestCatalog_Export(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{
		Name:        "export-test",
		Description: "Export me",
		Version:     "1.0.0",
		Tree:        "domain:default",
		Schedule:    "on_demand",
		Metadata:    map[string]string{"category": "test", "tags": "export"},
	})

	cat := NewCatalog(reg, dir)
	outPath := filepath.Join(dir, "export-test.yaml")
	if err := cat.Export("export-test", outPath); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 50 {
		t.Error("exported file too short")
	}
}

func TestInferTree(t *testing.T) {
	tests := []struct {
		content, expected string
	}{
		{"code review tool for PRs", "domain:code_review"},
		{"research deep learning", "research:deep_research"},
		{"system health monitor", "domain:agent_monitor"},
		{"etl pipeline for data", "domain:data_pipeline"},
		{"meeting transcript summarizer", "domain:meeting_notes"},
		{"financial model builder", "finance:market_researcher"},
		{"ci/cd devops deployment", "domain:devops_ci"},
		{"security audit vulnerability scan", "domain:security_audit"},
		{"random generic task", "domain:default"},
	}

	for _, tt := range tests {
		result := inferTree(tt.content)
		if result != tt.expected {
			t.Errorf("inferTree(%q) = %q, want %q", tt.content, result, tt.expected)
		}
	}
}
