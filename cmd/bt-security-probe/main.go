package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/nico/go-bt-evolve/internal/security"
)

const defaultTarget = "http://localhost:9800"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil))
}

func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	fs := flag.NewFlagSet("bt-security-probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", envDefault("BT_DASHBOARD_URL", defaultTarget), "dashboard base URL to probe")
	apiKey := fs.String("api-key", os.Getenv("BT_API_KEY"), "optional dashboard API key")
	timeout := fs.Duration("timeout", 10*time.Second, "overall probe timeout")
	jsonOnly := fs.Bool("json", false, "emit only the JSON report")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "timeout must be positive")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if client == nil {
		client = &http.Client{Timeout: *timeout}
	}

	report, err := security.ProbeDashboard(ctx, *target, *apiKey, client)
	if err != nil {
		fmt.Fprintf(stderr, "security probe transport error: %v\n", err)
	}

	if !*jsonOnly {
		status := "FAIL"
		if report.Passed {
			status = "PASS"
		}
		fmt.Fprintf(stdout, "BT dashboard security probe: %s (%s)\n", status, report.Summary())
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if encodeErr := enc.Encode(report); encodeErr != nil {
		fmt.Fprintf(stderr, "failed to encode report: %v\n", encodeErr)
		return 2
	}
	if err != nil || !report.Passed {
		return 1
	}
	return 0
}

func envDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
