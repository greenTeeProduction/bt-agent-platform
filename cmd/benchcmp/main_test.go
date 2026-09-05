package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleBenchOutput mirrors real `go test -bench -benchmem` output: two
// benchmarks with ns/op, B/op and allocs/op, plus the surrounding noise
// lines (goos/goarch/pkg/cpu/PASS/ok) that ParseBenchOutput must skip.
const sampleBenchOutput = `goos: linux
goarch: amd64
pkg: github.com/nico/go-bt-evolve/internal/benchmark
cpu: Intel(R) Core(TM) i7
BenchmarkFoo-8    1000000    1050 ns/op    128 B/op    3 allocs/op
BenchmarkBar-8     500000    2200 ns/op    256 B/op    5 allocs/op
PASS
ok  	github.com/nico/go-bt-evolve/internal/benchmark	3.456s
`

// saveBaseline runs "benchcmp baseline --save" against sampleBenchOutput so
// tests that need an existing baseline (show/check/reset) can build on it.
func saveBaseline(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline", "--save"}, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("saveBaseline setup failed: exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunNoArgsPrintsUsageAndExits3(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run(nil, strings.NewReader(""), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run() exit code = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "benchcmp — Go benchmark regression detector") {
		t.Fatalf("expected usage banner on stderr, got %q", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"bogus"}, strings.NewReader(""), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run() exit code = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "Unknown command: bogus") {
		t.Fatalf("expected unknown command message, got %q", stderr.String())
	}
}

func TestRunBaselineDryRunPrintsParsedResults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline"}, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "BenchmarkFoo") || !strings.Contains(out, "BenchmarkBar") {
		t.Fatalf("expected parsed benchmark names in dry-run output, got %q", out)
	}
	if !strings.Contains(out, "2 benchmarks parsed. Use --save to store as baseline.") {
		t.Fatalf("expected dry-run summary line, got %q", out)
	}
}

func TestRunBaselineSavePersistsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline", "--save"}, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Baseline saved: 2 benchmarks") {
		t.Fatalf("expected save confirmation, got %q", stdout.String())
	}
	baselineFile := filepath.Join(home, ".go-bt-benchcmp", "baseline.json")
	if _, err := os.Stat(baselineFile); err != nil {
		t.Fatalf("expected baseline file at %s: %v", baselineFile, err)
	}
}

func TestRunBaselineSaveRejectsEmptyInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline", "--save"}, strings.NewReader(""), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run() exit code = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "No benchmark results found in input.") {
		t.Fatalf("expected empty-input error, got %q", stderr.String())
	}
}

func TestRunShowNoBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"show"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No baseline saved yet.") {
		t.Fatalf("expected no-baseline message, got %q", stdout.String())
	}
}

func TestRunShowWithBaseline(t *testing.T) {
	home := t.TempDir()
	saveBaseline(t, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"show"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "BenchmarkFoo") || !strings.Contains(out, "2 benchmarks") {
		t.Fatalf("expected baseline listing, got %q", out)
	}
}

func TestRunCheckNoBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run() exit code = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "No baseline found. Run 'benchcmp baseline --save' first.") {
		t.Fatalf("expected no-baseline error, got %q", stderr.String())
	}
}

func TestRunCheckRejectsEmptyInput(t *testing.T) {
	home := t.TempDir()
	saveBaseline(t, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, strings.NewReader(""), &stdout, &stderr)
	if code != 3 {
		t.Fatalf("run() exit code = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "No benchmark results found in input.") {
		t.Fatalf("expected empty-input error, got %q", stderr.String())
	}
}

func TestRunCheckNoRegressionExitsClean(t *testing.T) {
	home := t.TempDir()
	saveBaseline(t, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"check"}, strings.NewReader(sampleBenchOutput), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "All benchmarks within acceptable thresholds.") {
		t.Fatalf("expected clean report, got %q", stdout.String())
	}
}

func TestRunCheckSeverityExitCodes(t *testing.T) {
	cases := []struct {
		name       string
		currentOut string
		wantCode   int
	}{
		{
			name: "warning regression exits 1",
			currentOut: `BenchmarkFoo-8    1000000    1200 ns/op    128 B/op    3 allocs/op
BenchmarkBar-8     500000    2200 ns/op    256 B/op    5 allocs/op
`,
			wantCode: 1,
		},
		{
			name: "critical regression exits 2",
			currentOut: `BenchmarkFoo-8    1000000    1500 ns/op    128 B/op    3 allocs/op
BenchmarkBar-8     500000    2200 ns/op    256 B/op    5 allocs/op
`,
			wantCode: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			saveBaseline(t, home)
			var stdout, stderr bytes.Buffer
			code := run([]string{"check"}, strings.NewReader(tc.currentOut), &stdout, &stderr)
			if code != tc.wantCode {
				t.Fatalf("run() exit code = %d, want %d; stdout=%s stderr=%s", code, tc.wantCode, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunResetNoBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := run([]string{"reset"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "No baseline to reset.") {
		t.Fatalf("expected no-baseline reset message, got %q", stdout.String())
	}
}

func TestRunResetRemovesBaselineFile(t *testing.T) {
	home := t.TempDir()
	saveBaseline(t, home)
	var stdout, stderr bytes.Buffer
	code := run([]string{"reset"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Baseline reset.") {
		t.Fatalf("expected reset confirmation, got %q", stdout.String())
	}
	baselineFile := filepath.Join(home, ".go-bt-benchcmp", "baseline.json")
	if _, err := os.Stat(baselineFile); !os.IsNotExist(err) {
		t.Fatalf("expected baseline file to be removed, stat err = %v", err)
	}
}
