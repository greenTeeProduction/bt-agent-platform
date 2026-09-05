// Command benchcmp compares Go benchmark output against stored baselines
// to detect performance regressions.
//
// Usage:
//
//	# Save current benchmark results as the new baseline
//	go test -bench=. -benchmem ./... | benchcmp baseline --save
//
//	# Compare current results against saved baseline
//	go test -bench=. -benchmem ./... | benchcmp check
//
//	# Check with custom thresholds
//	go test -bench=. -benchmem ./... | benchcmp check --warning 15 --critical 30
//
//	# View current baseline
//	benchcmp show
//
//	# Reset baseline
//	benchcmp reset
//
// Exit codes:
//
//	0 - all benchmarks within thresholds
//	1 - one or more warnings (benchmarks slower than warning threshold)
//	2 - one or more critical regressions
//	3 - error (invalid input, file error, etc.)
//
// Baselines are stored in .go-bt-benchcmp/baseline.json relative to the working directory.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/benchmark"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the benchcmp CLI against the given args and I/O streams,
// returning the process exit code. Kept separate from main so tests can
// drive it without touching os.Args/os.Exit/os.Stdin/os.Stdout/os.Stderr.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return 3
	}

	cmd := args[0]

	switch cmd {
	case "baseline":
		return baselineCmd(args[1:], stdin, stdout, stderr)
	case "check":
		return checkCmd(args[1:], stdin, stdout, stderr)
	case "show":
		return showCmd(stdout, stderr)
	case "reset":
		return resetCmd(stdout, stderr)
	default:
		fmt.Fprintf(stderr, "Unknown command: %s\n\n", cmd)
		printUsage(stderr)
		return 3
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `benchcmp — Go benchmark regression detector

Usage:
  benchcmp baseline [--save]     Save current bench output as new baseline
  benchcmp check                 Compare stdin bench output against baseline
  benchcmp show                  Display current baseline
  benchcmp reset                 Delete current baseline

Options (check command):
  --warning float    Warning threshold percentage (default: 10)
  --critical float   Critical threshold percentage (default: 25)
  --min-ns float     Minimum ns/op to consider (default: 100)

Pipe go test -bench output to check:
  go test -bench=. -benchmem ./... | benchcmp check

Exit codes:
  0 = all ok, 1 = warnings, 2 = critical regressions, 3 = error
`)
}

func baselinePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".go-bt-benchcmp")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "baseline.json")
}

func baselineCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("baseline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	save := fs.Bool("save", false, "save stdin as new baseline")
	if err := fs.Parse(args); err != nil {
		return 3
	}

	store := benchmark.NewBaselineStore(baselinePath())
	if *save {
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "Error reading stdin: %v\n", err)
			return 3
		}
		results := benchmark.ParseBenchOutput(string(data))
		if len(results) == 0 {
			fmt.Fprintln(stderr, "No benchmark results found in input.")
			return 3
		}
		if err := store.UpdateBaseline(results); err != nil {
			fmt.Fprintf(stderr, "Error saving baseline: %v\n", err)
			return 3
		}
		fmt.Fprintf(stdout, "Baseline saved: %d benchmarks\n", len(results))
	} else {
		// Load stdin and show parsed results (dry-run mode)
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "Error reading stdin: %v\n", err)
			return 3
		}
		results := benchmark.ParseBenchOutput(string(data))
		for _, r := range results {
			fmt.Fprintf(stdout, "%-50s %8.0f ns/op %8.0f B/op %5d allocs\n", r.Name, r.NsPerOp, r.BPerOp, r.Allocs)
		}
		fmt.Fprintf(stdout, "\n%d benchmarks parsed. Use --save to store as baseline.\n", len(results))
	}
	return 0
}

func checkCmd(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	warning := fs.Float64("warning", 10.0, "warning threshold percentage")
	critical := fs.Float64("critical", 25.0, "critical threshold percentage")
	minNs := fs.Float64("min-ns", 100.0, "minimum ns/op to consider")
	if err := fs.Parse(args); err != nil {
		return 3
	}

	config := benchmark.RegressionConfig{
		WarningThreshold:  *warning,
		CriticalThreshold: *critical,
		MinNsPerOp:        *minNs,
	}

	store := benchmark.NewBaselineStore(baselinePath())
	if err := store.Load(); err != nil {
		fmt.Fprintf(stderr, "Error loading baseline: %v\n", err)
		return 3
	}
	if len(store.Baseline) == 0 {
		fmt.Fprintln(stderr, "No baseline found. Run 'benchcmp baseline --save' first.")
		return 3
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "Error reading stdin: %v\n", err)
		return 3
	}

	current := benchmark.ParseBenchOutput(string(data))
	if len(current) == 0 {
		fmt.Fprintln(stderr, "No benchmark results found in input.")
		return 3
	}

	comp := benchmark.NewComparator(store, config)
	results := comp.Compare(current)
	report := benchmark.FormatReport(results)
	fmt.Fprint(stdout, report)

	if benchmark.HasRegressions(results) {
		return 2
	} else if benchmark.HasWarnings(results) {
		return 1
	}
	return 0
}

func showCmd(stdout, stderr io.Writer) int {
	store := benchmark.NewBaselineStore(baselinePath())
	if err := store.Load(); err != nil {
		fmt.Fprintf(stderr, "Error loading baseline: %v\n", err)
		return 3
	}
	if len(store.Baseline) == 0 {
		fmt.Fprintln(stdout, "No baseline saved yet.")
		return 0
	}
	fmt.Fprintf(stdout, "Baseline: %s (%d benchmarks)\n\n", baselinePath(), len(store.Baseline))
	for name, b := range store.Baseline {
		fmt.Fprintf(stdout, "%-50s %8.0f ns/op\n", name, b.NsPerOp)
	}
	return 0
}

func resetCmd(stdout, stderr io.Writer) int {
	path := baselinePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(stdout, "No baseline to reset.")
			return 0
		}
		fmt.Fprintf(stderr, "Error removing baseline: %v\n", err)
		return 3
	}
	fmt.Fprintln(stdout, "Baseline reset.")
	return 0
}
