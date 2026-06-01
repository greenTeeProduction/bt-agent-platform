package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/gardener"
	"github.com/nico/go-bt-evolve/internal/llm"
)

const defaultMinSuccessRate = 0.80

type treeResult struct {
	Name        string             `json:"name"`
	Suite       string             `json:"suite"`
	Tasks       int                `json:"tasks"`
	SuccessRate float64            `json:"success_rate"`
	Successes   int                `json:"successes"`
	Failures    int                `json:"failures"`
	DurationMs  int64              `json:"duration_ms"`
	Passed      bool               `json:"passed"`
	Results     []benchmark.Result `json:"results,omitempty"`
}

type validationReport struct {
	StartedAt      time.Time    `json:"started_at"`
	FinishedAt     time.Time    `json:"finished_at"`
	DurationMs     int64        `json:"duration_ms"`
	StorageDir     string       `json:"storage_dir"`
	TotalTrees     int          `json:"total_trees"`
	ValidatedTrees int          `json:"validated_trees"`
	PassedTrees    int          `json:"passed_trees"`
	FailedTrees    int          `json:"failed_trees"`
	MinSuccessRate float64      `json:"min_success_rate"`
	LLMProvider    string       `json:"llm_provider"`
	Passed         bool         `json:"passed"`
	Results        []treeResult `json:"results"`
}

type evidenceVerificationReport struct {
	ReportPath     string    `json:"report_path"`
	VerifiedAt     time.Time `json:"verified_at"`
	ExpectedTrees  int       `json:"expected_trees"`
	ValidatedTrees int       `json:"validated_trees"`
	LLMProvider    string    `json:"llm_provider"`
	Checks         int       `json:"checks"`
	Valid          bool      `json:"valid"`
	Errors         []string  `json:"errors,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bt-tree-integration", flag.ContinueOnError)
	fs.SetOutput(stderr)
	storageDir := fs.String("storage", defaultStorageDir(), "tree/reflection storage directory")
	maxTrees := fs.Int("max-trees", 0, "maximum trees to validate; 0 means all")
	minSuccess := fs.Float64("min-success", defaultMinSuccessRate, "minimum per-tree success rate required")
	listOnly := fs.Bool("list", false, "list registered trees and exit without invoking Ollama")
	jsonOnly := fs.Bool("json", false, "emit only JSON output")
	includeResults := fs.Bool("include-results", false, "include per-task results in JSON report")
	outputPath := fs.String("output", "", "optional path to write JSON report")
	verifyReport := fs.String("verify-report", "", "validate an existing JSON evidence report and exit without invoking Ollama")
	expectTrees := fs.Int("expect-trees", 0, "minimum validated tree count required when verifying a report; 0 accepts the report count")
	maxAgeHours := fs.Float64("max-age-hours", 168, "maximum report age in hours when verifying; 0 disables age check")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *maxTrees < 0 {
		fmt.Fprintln(stderr, "max-trees must be >= 0")
		return 2
	}
	if *expectTrees < 0 {
		fmt.Fprintln(stderr, "expect-trees must be >= 0")
		return 2
	}
	if *maxAgeHours < 0 {
		fmt.Fprintln(stderr, "max-age-hours must be >= 0")
		return 2
	}
	if *minSuccess < 0 || *minSuccess > 1 {
		fmt.Fprintln(stderr, "min-success must be between 0 and 1")
		return 2
	}
	if *verifyReport != "" {
		report, err := verifyEvidenceReport(*verifyReport, *expectTrees, time.Duration(*maxAgeHours*float64(time.Hour)), time.Now())
		if err != nil {
			fmt.Fprintf(stderr, "failed to verify report: %v\n", err)
			return 2
		}
		if !*jsonOnly {
			status := "FAIL"
			if report.Valid {
				status = "PASS"
			}
			fmt.Fprintf(stdout, "BT tree integration evidence: %s (%d checks, report=%s)\n", status, report.Checks, report.ReportPath)
			for _, errMsg := range report.Errors {
				fmt.Fprintf(stdout, "- %s\n", errMsg)
			}
		}
		if code := encodeJSON(stdout, stderr, report); code != 0 {
			return code
		}
		if !report.Valid {
			return 1
		}
		return 0
	}

	reg := gardener.NewRegistry(*storageDir)
	entries := reg.List()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	if *listOnly {
		if *jsonOnly {
			payload := struct {
				Total int      `json:"total"`
				Trees []string `json:"trees"`
			}{Total: len(entries)}
			for _, entry := range entries {
				payload.Trees = append(payload.Trees, entry.Name)
			}
			return encodeJSON(stdout, stderr, payload)
		}
		fmt.Fprintf(stdout, "Registered BT trees: %d\n", len(entries))
		for _, entry := range entries {
			fmt.Fprintf(stdout, "- %s\n", entry.Name)
		}
		return 0
	}

	llmClient, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		fmt.Fprintf(stderr, "real Ollama LLM unavailable: %v\n", err)
		return 1
	}

	started := time.Now()
	report := validationReport{
		StartedAt:      started,
		StorageDir:     *storageDir,
		TotalTrees:     len(entries),
		MinSuccessRate: *minSuccess,
		LLMProvider:    "ollama",
		Passed:         true,
	}

	limit := len(entries)
	if *maxTrees > 0 && *maxTrees < limit {
		limit = *maxTrees
	}
	for _, entry := range entries[:limit] {
		if !entry.Active || entry.Tree == nil {
			continue
		}
		suite := benchmark.SuiteForTree(entry.Name)
		start := time.Now()
		metrics := benchmark.RunSuite(entry.Tree, suite, llmClient)
		tr := treeResult{
			Name:        entry.Name,
			Suite:       suite.Name,
			Tasks:       metrics.TotalTasks,
			SuccessRate: metrics.SuccessRate,
			Successes:   metrics.Successes,
			Failures:    metrics.Failures,
			DurationMs:  time.Since(start).Milliseconds(),
			Passed:      metrics.SuccessRate >= *minSuccess,
		}
		if *includeResults {
			tr.Results = metrics.Results
		}
		report.ValidatedTrees++
		if tr.Passed {
			report.PassedTrees++
		} else {
			report.FailedTrees++
			report.Passed = false
		}
		report.Results = append(report.Results, tr)
		if !*jsonOnly {
			status := "PASS"
			if !tr.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(stdout, "%s %s suite=%s success=%.1f%% tasks=%d duration=%s\n", status, tr.Name, tr.Suite, tr.SuccessRate*100, tr.Tasks, time.Since(start).Round(time.Second))
		}
	}
	report.FinishedAt = time.Now()
	report.DurationMs = report.FinishedAt.Sub(started).Milliseconds()

	if *outputPath != "" {
		if err := writeJSONFile(*outputPath, report); err != nil {
			fmt.Fprintf(stderr, "failed to write report: %v\n", err)
			return 2
		}
	}
	if !*jsonOnly {
		status := "FAIL"
		if report.Passed {
			status = "PASS"
		}
		fmt.Fprintf(stdout, "BT tree real-Ollama integration: %s (%d/%d trees passed, min %.0f%%)\n", status, report.PassedTrees, report.ValidatedTrees, *minSuccess*100)
	}
	if code := encodeJSON(stdout, stderr, report); code != 0 {
		return code
	}
	if !report.Passed {
		return 1
	}
	return 0
}

func encodeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "failed to encode JSON: %v\n", err)
		return 2
	}
	return 0
}

func writeJSONFile(path string, report validationReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func verifyEvidenceReport(path string, expectedTrees int, maxAge time.Duration, now time.Time) (evidenceVerificationReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return evidenceVerificationReport{}, err
	}
	var report validationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return evidenceVerificationReport{}, err
	}
	verification := evidenceVerificationReport{
		ReportPath:     path,
		VerifiedAt:     now,
		ExpectedTrees:  expectedTrees,
		ValidatedTrees: report.ValidatedTrees,
		LLMProvider:    report.LLMProvider,
		Valid:          true,
	}
	check := func(ok bool, msg string) {
		verification.Checks++
		if !ok {
			verification.Valid = false
			verification.Errors = append(verification.Errors, msg)
		}
	}

	if expectedTrees == 0 {
		expectedTrees = report.TotalTrees
		verification.ExpectedTrees = expectedTrees
	}
	check(report.Passed, "report did not pass")
	check(report.LLMProvider == "ollama", "report was not produced by real Ollama")
	check(report.TotalTrees > 0, "report has no registered trees")
	check(report.ValidatedTrees >= expectedTrees, fmt.Sprintf("validated %d trees, expected at least %d", report.ValidatedTrees, expectedTrees))
	check(report.PassedTrees == report.ValidatedTrees, fmt.Sprintf("passed %d of %d validated trees", report.PassedTrees, report.ValidatedTrees))
	check(report.FailedTrees == 0, fmt.Sprintf("report has %d failed trees", report.FailedTrees))
	check(len(report.Results) == report.ValidatedTrees, fmt.Sprintf("report has %d result rows for %d validated trees", len(report.Results), report.ValidatedTrees))
	check(!report.StartedAt.IsZero() && !report.FinishedAt.IsZero() && !report.FinishedAt.Before(report.StartedAt), "report timestamps are missing or invalid")
	if maxAge > 0 {
		check(!report.FinishedAt.IsZero() && now.Sub(report.FinishedAt) <= maxAge, fmt.Sprintf("report is older than %s", maxAge))
	}
	for _, result := range report.Results {
		check(result.Passed, fmt.Sprintf("tree %s failed evidence gate", result.Name))
		check(result.SuccessRate >= report.MinSuccessRate, fmt.Sprintf("tree %s success %.2f below min %.2f", result.Name, result.SuccessRate, report.MinSuccessRate))
		check(result.Tasks > 0, fmt.Sprintf("tree %s has no benchmark tasks", result.Name))
	}
	return verification, nil
}

func defaultStorageDir() string {
	if v := os.Getenv("BT_TREE_STORAGE"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".go-bt-reflections")
}
