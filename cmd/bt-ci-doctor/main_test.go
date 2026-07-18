package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/cicd"
)

// repoRoot walks up from the test's working directory to find the module
// root (identified by go.mod), so fixtures can point at this repository's
// own, real CI/CD configuration.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod not found)")
		}
		dir = parent
	}
}

// noRunnerHome returns a fresh HOME with no actions-runner directory, so the
// advisory "self-hosted runner installed" check deterministically fails.
func noRunnerHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// runnerHome returns a HOME with a fake actions-runner/.runner marker, so
// the advisory "self-hosted runner installed" check deterministically passes.
func runnerHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	runnerDir := filepath.Join(home, "actions-runner")
	if err := os.MkdirAll(runnerDir, 0o755); err != nil {
		t.Fatalf("mkdir runner dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runnerDir, ".runner"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write runner marker: %v", err)
	}
	return home
}

func TestRun_TextOutput(t *testing.T) {
	cases := []struct {
		name            string
		argsFn          func(t *testing.T) []string
		homeFn          func(t *testing.T) string
		wantExit        int
		wantContains    []string
		wantNotContains []string
		wantErrContains []string
	}{
		{
			name:     "all checks pass against this repo's real CI config",
			argsFn:   func(t *testing.T) []string { return []string{"--root", repoRoot(t)} },
			homeFn:   runnerHome,
			wantExit: 0,
			wantContains: []string{
				"CI/CD workflow checks passed",
				"✓ self-hosted runner installed",
			},
			wantNotContains: []string{"✗"},
		},
		{
			name:     "missing workflow files fail with default root",
			argsFn:   func(t *testing.T) []string { return nil },
			homeFn:   noRunnerHome,
			wantExit: 1,
			wantContains: []string{
				"✗ ci workflow exists and parses",
				"✗ nightly workflow exists and parses",
				"✗ codeql workflow exists and parses",
				"✗ dependabot config exists and parses",
				"✗ self-hosted runner installed",
				"1 advisory check(s) failed",
			},
		},
		{
			name:     "missing workflow files fail with explicit empty root",
			argsFn:   func(t *testing.T) []string { return []string{"--root", t.TempDir()} },
			homeFn:   noRunnerHome,
			wantExit: 1,
			wantContains: []string{
				"✗ ci workflow exists and parses",
			},
		},
		{
			name:            "unknown flag returns usage error",
			argsFn:          func(t *testing.T) []string { return []string{"--nope"} },
			homeFn:          noRunnerHome,
			wantExit:        2,
			wantErrContains: []string{"nope"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", tc.homeFn(t))
			var out, errOut bytes.Buffer
			code := run(tc.argsFn(t), &out, &errOut)
			if code != tc.wantExit {
				t.Fatalf("run() exit code = %d, want %d; stdout=%s stderr=%s", code, tc.wantExit, out.String(), errOut.String())
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(out.String(), want) {
					t.Errorf("expected stdout to contain %q, got:\n%s", want, out.String())
				}
			}
			for _, notWant := range tc.wantNotContains {
				if strings.Contains(out.String(), notWant) {
					t.Errorf("expected stdout NOT to contain %q, got:\n%s", notWant, out.String())
				}
			}
			for _, want := range tc.wantErrContains {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("expected stderr to contain %q, got:\n%s", want, errOut.String())
				}
			}
		})
	}
}

func TestRun_JSONOutput(t *testing.T) {
	t.Setenv("HOME", noRunnerHome(t))
	root := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"--root", root, "--json"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("run() exit code = %d, want 1; stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	if strings.Contains(out.String(), "✓") || strings.Contains(out.String(), "✗") {
		t.Fatalf("expected --json to suppress human-readable marks, got:\n%s", out.String())
	}

	var report cicd.WorkflowReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("expected valid JSON report, got error %v; output=%s", err, out.String())
	}
	if report.AllPassed {
		t.Fatalf("expected AllPassed=false for missing workflows, got report=%+v", report)
	}
	wantRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("filepath.Abs(%q): %v", root, err)
	}
	if report.Root != wantRoot {
		t.Fatalf("report.Root = %q, want %q", report.Root, wantRoot)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected at least one check in the JSON report")
	}
}
