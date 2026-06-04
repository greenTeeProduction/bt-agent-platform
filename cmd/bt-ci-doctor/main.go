package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nico/go-bt-evolve/internal/gardener"
)

func main() {
	root := flag.String("root", ".", "repository root to validate")
	jsonOut := flag.Bool("json", false, "print the full report as JSON")
	flag.Parse()

	report, err := gardener.ValidateWorkflows(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bt-ci-doctor: %v\n", err)
		os.Exit(2)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		fmt.Println(report.Summary)
		var advisoryCount int
		for _, check := range report.Checks {
			mark := "✓"
			if !check.Passed {
				mark = "✗"
			}
			fmt.Printf("%s %s — %s\n", mark, check.Name, check.Details)
			if strings.Contains(check.Name, "advisory") && !check.Passed {
				advisoryCount++
			}
		}
		if advisoryCount > 0 {
			fmt.Printf("\nℹ  %d advisory check(s) failed (environment-dependent, not workflow structure issues)\n", advisoryCount)
		}
	}
	if !report.AllPassed {
		os.Exit(1)
	}
}
