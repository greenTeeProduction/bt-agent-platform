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
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(3)
	}

	cmd := os.Args[1]

	switch cmd {
	case "baseline":
		baselineCmd(os.Args[2:])
	case "check":
		checkCmd(os.Args[2:])
	case "show":
		showCmd()
	case "reset":
		resetCmd()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(3)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `benchcmp — Go benchmark regression detector

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
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "baseline.json")
}

func baselineCmd(args []string) {
	fs := flag.NewFlagSet("baseline", flag.ExitOnError)
	save := fs.Bool("save", false, "save stdin as new baseline")
	fs.Parse(args)

	store := benchmark.NewBaselineStore(baselinePath())
	if *save {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(3)
		}
		results := benchmark.ParseBenchOutput(string(data))
		if len(results) == 0 {
			fmt.Fprintln(os.Stderr, "No benchmark results found in input.")
			os.Exit(3)
		}
		if err := store.UpdateBaseline(results); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving baseline: %v\n", err)
			os.Exit(3)
		}
		fmt.Printf("Baseline saved: %d benchmarks\n", len(results))
	} else {
		// Load stdin and show parsed results (dry-run mode)
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(3)
		}
		results := benchmark.ParseBenchOutput(string(data))
		for _, r := range results {
			fmt.Printf("%-50s %8.0f ns/op %8.0f B/op %5d allocs\n", r.Name, r.NsPerOp, r.BPerOp, r.Allocs)
		}
		fmt.Printf("\n%d benchmarks parsed. Use --save to store as baseline.\n", len(results))
	}
}

func checkCmd(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	warning := fs.Float64("warning", 10.0, "warning threshold percentage")
	critical := fs.Float64("critical", 25.0, "critical threshold percentage")
	minNs := fs.Float64("min-ns", 100.0, "minimum ns/op to consider")
	fs.Parse(args)

	config := benchmark.RegressionConfig{
		WarningThreshold:  *warning,
		CriticalThreshold: *critical,
		MinNsPerOp:        *minNs,
	}

	store := benchmark.NewBaselineStore(baselinePath())
	if err := store.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading baseline: %v\n", err)
		os.Exit(3)
	}
	if len(store.Baseline) == 0 {
		fmt.Fprintln(os.Stderr, "No baseline found. Run 'benchcmp baseline --save' first.")
		os.Exit(3)
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(3)
	}

	current := benchmark.ParseBenchOutput(string(data))
	if len(current) == 0 {
		fmt.Fprintln(os.Stderr, "No benchmark results found in input.")
		os.Exit(3)
	}

	comp := benchmark.NewComparator(store, config)
	results := comp.Compare(current)
	report := benchmark.FormatReport(results)
	fmt.Print(report)

	if benchmark.HasRegressions(results) {
		os.Exit(2)
	} else if benchmark.HasWarnings(results) {
		os.Exit(1)
	}
}

func showCmd() {
	store := benchmark.NewBaselineStore(baselinePath())
	if err := store.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading baseline: %v\n", err)
		os.Exit(3)
	}
	if len(store.Baseline) == 0 {
		fmt.Println("No baseline saved yet.")
		os.Exit(0)
	}
	fmt.Printf("Baseline: %s (%d benchmarks)\n\n", baselinePath(), len(store.Baseline))
	for name, b := range store.Baseline {
		fmt.Printf("%-50s %8.0f ns/op\n", name, b.NsPerOp)
	}
}

func resetCmd() {
	path := baselinePath()
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No baseline to reset.")
			return
		}
		fmt.Fprintf(os.Stderr, "Error removing baseline: %v\n", err)
		os.Exit(3)
	}
	fmt.Println("Baseline reset.")
}
