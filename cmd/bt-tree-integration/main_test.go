package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestDefaultStorageDirUsesEnv(t *testing.T) {
	t.Setenv("BT_TREE_STORAGE", "/tmp/custom-tree-storage")
	if got := defaultStorageDir(); got != "/tmp/custom-tree-storage" {
		t.Fatalf("defaultStorageDir() = %q", got)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
