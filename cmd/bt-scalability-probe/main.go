package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, nil))
}

func run(args []string, stdout, stderr io.Writer, client *http.Client) int {
	fs := flag.NewFlagSet("bt-scalability-probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nodesArg := fs.String("nodes", os.Getenv("BT_DASHBOARD_NODES"), "comma-separated dashboard base URLs (requires at least 2)")
	apiKey := fs.String("api-key", os.Getenv("BT_API_KEY"), "optional dashboard API key")
	timeout := fs.Duration("timeout", 10*time.Second, "overall probe timeout")
	required := fs.Int("required-healthy", 0, "minimum healthy nodes required (default: all nodes)")
	execute := fs.Bool("execute", false, "also POST /api/agents/execute on each node")
	agent := fs.String("agent", "scalability-smoke", "agent name for --execute smoke test")
	task := fs.String("task", "check distributed execution smoke path", "task for --execute smoke test")
	jsonOnly := fs.Bool("json", false, "emit only the JSON report")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "timeout must be positive")
		return 2
	}
	nodes := parseNodes(*nodesArg)
	if client == nil {
		client = &http.Client{Timeout: *timeout}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	report, err := reliability.ProbeMultiNodeDashboard(ctx, reliability.MultiNodeProbeConfig{
		Nodes:           nodes,
		APIKey:          *apiKey,
		RequiredHealthy: *required,
		Execute:         *execute,
		Agent:           *agent,
		Task:            *task,
		Client:          client,
	})
	if err != nil {
		fmt.Fprintf(stderr, "scalability probe validation error: %v\n", err)
	}
	if !*jsonOnly {
		fmt.Fprintf(stdout, "BT dashboard scalability probe: %s\n", report.Summary())
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

func parseNodes(raw string) []string {
	parts := strings.Split(raw, ",")
	nodes := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			nodes = append(nodes, strings.TrimRight(trimmed, "/"))
		}
	}
	return nodes
}
