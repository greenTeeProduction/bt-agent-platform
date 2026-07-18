package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/nico/go-bt-evolve/internal/cicd"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the bt-ci-doctor CLI against args, writing human/JSON output
// to out and error output to errOut, and returns the process exit code.
func run(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("bt-ci-doctor", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", ".", "repository root to validate")
	jsonOut := fs.Bool("json", false, "print the full report as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, err := cicd.ValidateWorkflows(*root)
	if err != nil {
		fmt.Fprintf(errOut, "bt-ci-doctor: %v\n", err)
		return 2
	}
	if *jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Fprintln(out, report.Summary)
		var advisoryCount int
		for _, check := range report.Checks {
			mark := "✓"
			if !check.Passed {
				mark = "✗"
			}
			fmt.Fprintf(out, "%s %s — %s\n", mark, check.Name, check.Details)
			if check.Advisory && !check.Passed {
				advisoryCount++
			}
		}
		if advisoryCount > 0 {
			fmt.Fprintf(out, "\nℹ  %d advisory check(s) failed (environment-dependent, not workflow structure issues)\n", advisoryCount)
		}
	}
	if !report.AllPassed {
		return 1
	}
	return 0
}
