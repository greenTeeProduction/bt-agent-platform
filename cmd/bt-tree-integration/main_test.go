package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunListJSON(t *testing.T) {
	tmp := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"--storage", tmp, "--list", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run() exit code = %d, stderr=%s, stdout=%s", code, errOut.String(), out.String())
	}
	var payload struct {
		Total int      `json:"total"`
		Trees []string `json:"trees"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out.String())
	}
	if payload.Total == 0 || len(payload.Trees) != payload.Total {
		t.Fatalf("expected non-empty tree listing, got %+v", payload)
	}
	if !contains(payload.Trees, "default") || !contains(payload.Trees, "godev") {
		t.Fatalf("expected built-in trees in listing, got %v", payload.Trees)
	}
}

func TestRunListHumanSummary(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--storage", t.TempDir(), "--list"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run() exit code = %d, stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Registered BT trees:") || !strings.Contains(out.String(), "- default") {
		t.Fatalf("expected human tree listing, got %q", out.String())
	}
}

func TestRunRejectsInvalidFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "negative max", args: []string{"--max-trees", "-1"}, want: "max-trees must be >= 0"},
		{name: "low min", args: []string{"--min-success", "-0.1"}, want: "min-success must be between 0 and 1"},
		{name: "high min", args: []string{"--min-success", "1.1"}, want: "min-success must be between 0 and 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tc.args, &out, &errOut)
			if code != 2 {
				t.Fatalf("run() exit code = %d, want 2", code)
			}
			if !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("expected %q in stderr, got %q", tc.want, errOut.String())
			}
		})
	}
}

func TestWriteJSONFileCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	report := validationReport{TotalTrees: 2, Passed: true}
	if err := writeJSONFile(path, report); err != nil {
		t.Fatalf("writeJSONFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("report not written: %v", err)
	}
	if !strings.Contains(string(data), `"total_trees": 2`) {
		t.Fatalf("unexpected report content: %s", data)
	}
}

func TestVerifyEvidenceReportPassesFreshOllamaArtifact(t *testing.T) {
	now := mustParseTime(t, "2026-06-01T10:00:00Z")
	path := writeTestReport(t, validationReport{
		StartedAt:      now.Add(-10 * time.Second),
		FinishedAt:     now.Add(-5 * time.Second),
		DurationMs:     5000,
		TotalTrees:     2,
		ValidatedTrees: 2,
		PassedTrees:    2,
		FailedTrees:    0,
		MinSuccessRate: 0.8,
		LLMProvider:    "ollama",
		Passed:         true,
		Results: []treeResult{
			{Name: "default", Tasks: 2, SuccessRate: 1, Passed: true},
			{Name: "godev", Tasks: 2, SuccessRate: 0.9, Passed: true},
		},
	})
	report, err := verifyEvidenceReport(path, 2, time.Hour, now)
	if err != nil {
		t.Fatalf("verifyEvidenceReport() error = %v", err)
	}
	if !report.Valid || len(report.Errors) != 0 {
		t.Fatalf("expected valid report, got %+v", report)
	}
	if report.Checks == 0 || report.ValidatedTrees != 2 || report.LLMProvider != "ollama" {
		t.Fatalf("unexpected verification metadata: %+v", report)
	}
}

func TestVerifyEvidenceReportRejectsMockPartialAndStaleArtifacts(t *testing.T) {
	now := mustParseTime(t, "2026-06-01T10:00:00Z")
	path := writeTestReport(t, validationReport{
		StartedAt:      now.Add(-48 * time.Hour),
		FinishedAt:     now.Add(-47 * time.Hour),
		TotalTrees:     3,
		ValidatedTrees: 1,
		PassedTrees:    0,
		FailedTrees:    1,
		MinSuccessRate: 0.8,
		LLMProvider:    "mock",
		Passed:         false,
		Results:        []treeResult{{Name: "default", Tasks: 0, SuccessRate: 0.5, Passed: false}},
	})
	report, err := verifyEvidenceReport(path, 3, time.Hour, now)
	if err != nil {
		t.Fatalf("verifyEvidenceReport() error = %v", err)
	}
	if report.Valid {
		t.Fatalf("expected invalid report")
	}
	joined := strings.Join(report.Errors, "\n")
	for _, want := range []string{"report did not pass", "real Ollama", "validated 1 trees", "older than"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in errors, got %q", want, joined)
		}
	}
}

func TestRunVerifyReportJSON(t *testing.T) {
	now := time.Now().UTC()
	path := writeTestReport(t, validationReport{
		StartedAt:      now.Add(-time.Minute),
		FinishedAt:     now,
		TotalTrees:     1,
		ValidatedTrees: 1,
		PassedTrees:    1,
		MinSuccessRate: 0.8,
		LLMProvider:    "ollama",
		Passed:         true,
		Results:        []treeResult{{Name: "default", Tasks: 1, SuccessRate: 1, Passed: true}},
	})
	var out, errOut bytes.Buffer
	code := run([]string{"--verify-report", path, "--expect-trees", "1", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("run() exit code = %d, stderr=%s, stdout=%s", code, errOut.String(), out.String())
	}
	var report evidenceVerificationReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !report.Valid || report.ExpectedTrees != 1 {
		t.Fatalf("unexpected verification report: %+v", report)
	}
}

func TestDefaultStorageDirUsesEnv(t *testing.T) {
	t.Setenv("BT_TREE_STORAGE", "/tmp/custom-tree-storage")
	if got := defaultStorageDir(); got != "/tmp/custom-tree-storage" {
		t.Fatalf("defaultStorageDir() = %q", got)
	}
}

func writeTestReport(t *testing.T, report validationReport) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	return path
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return parsed
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
