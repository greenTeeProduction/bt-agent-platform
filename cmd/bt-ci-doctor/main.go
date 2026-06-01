package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/nico/go-bt-evolve/internal/cicd"
)

func main() {
	root := flag.String("root", ".", "repository root to validate")
	jsonOut := flag.Bool("json", false, "print the full report as JSON")
	flag.Parse()

	report, err := cicd.ValidateWorkflows(*root)
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
		for _, check := range report.Checks {
			mark := "✓"
			if !check.Passed {
				mark = "✗"
			}
			fmt.Printf("%s %s — %s\n", mark, check.Name, check.Details)
		}
	}
	if !report.AllPassed {
		os.Exit(1)
	}
}
