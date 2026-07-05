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
	nodesArg := fs.String("nodes", os.Getenv("BT_DASHBOARD_NODES"), "comma-separated dashboard base URLs (requires at least 2 for multi-node probe)")
	target := fs.String("target", os.Getenv("BT_TARGET"), "single dashboard base URL (alternative to --nodes for single-node probe)")
	apiKey := fs.String("api-key", os.Getenv("BT_API_KEY"), "optional dashboard API key")
	timeout := fs.Duration("timeout", 10*time.Second, "overall probe timeout")
	required := fs.Int("required-healthy", 0, "minimum healthy nodes required (multi-node only; default: all nodes)")
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

	// Single-node mode: use --target or a single --nodes entry
	if *target != "" || len(nodes) == 1 {
		baseURL := *target
		if baseURL == "" {
			baseURL = nodes[0]
		}
		report := reliability.ProbeSingleNodeDashboard(ctx, reliability.SingleNodeProbeConfig{
			BaseURL: baseURL,
			APIKey:  *apiKey,
			Execute: *execute,
			Agent:   *agent,
			Task:    *task,
			Client:  client,
		})
		if !*jsonOnly {
			fmt.Fprintf(stdout, "BT dashboard scalability probe: %s\n", report.Summary())
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if encodeErr := enc.Encode(report); encodeErr != nil {
			fmt.Fprintf(stderr, "failed to encode report: %v\n", encodeErr)
			return 2
		}
		if !report.Passed {
			return 1
		}
		return 0
	}

	// Multi-node mode
	if len(nodes) < 2 {
		fmt.Fprintln(stderr, "bt-scalability-probe: use --target <url> for single-node, or --nodes url1,url2 for multi-node")
		return 2
	}

	mnr, err := reliability.ProbeMultiNodeDashboard(ctx, reliability.MultiNodeProbeConfig{
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

	// Milestone 4/5: when execute is requested, don't stop at independently
	// poking each node's execute endpoint — drive the real horizontal-scaling
	// substrate. Build a RemoteExecutor per node, distribute a routed task
	// stream across them via an AgentRouter, and record evidence that the
	// stream actually fanned out over more than one backend.
	report := multiNodeReport{MultiNodeProbeReport: mnr}
	if *execute {
		dd := driveDistributedDispatch(nodes, *apiKey, *agent, *task)
		report.DistributedDispatch = &dd
	}

	if !*jsonOnly {
		fmt.Fprintf(stdout, "BT dashboard scalability probe: %s\n", mnr.Summary())
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if encodeErr := enc.Encode(report); encodeErr != nil {
		fmt.Fprintf(stderr, "failed to encode report: %v\n", encodeErr)
		return 2
	}
	if !mnr.Passed {
		return 1
	}
	return 0
}

// multiNodeReport augments the reliability multi-node probe report with evidence
// that the probe drove the RemoteExecutor + AgentRouter substrate end-to-end.
// The embedded report's fields are inlined into the top-level JSON object.
type multiNodeReport struct {
	reliability.MultiNodeProbeReport
	DistributedDispatch *distributedDispatch `json:"distributed_dispatch,omitempty"`
}

// distributedDispatch summarizes a routed task stream issued through an
// AgentRouter over per-node RemoteExecutors. distinct_nodes proves the stream
// was genuinely distributed rather than pinned to a single backend.
type distributedDispatch struct {
	DispatchCount int      `json:"dispatch_count"`
	DistinctNodes int      `json:"distinct_nodes"`
	NodesHit      []string `json:"nodes_hit"`
	OK            bool     `json:"ok"`
	Error         string   `json:"error,omitempty"`
}

// driveDistributedDispatch wires each node into a RemoteExecutor, fronts them
// with a round-robin AgentRouter, and issues a routed dispatch stream. Each
// backend echoes its identity in the result Output, so the set of distinct
// outputs is direct evidence of how many nodes the router actually reached.
func driveDistributedDispatch(nodes []string, apiKey, agent, task string) distributedDispatch {
	dd := distributedDispatch{NodesHit: []string{}}
	if len(nodes) < 2 {
		dd.Error = "distributed dispatch requires at least 2 nodes"
		return dd
	}

	executors := make([]reliability.AgentExecutor, 0, len(nodes))
	for i, base := range nodes {
		executors = append(executors, reliability.NewRemoteExecutor(reliability.RemoteExecutorConfig{
			Name:    fmt.Sprintf("node-%d", i+1),
			BaseURL: base,
			APIKey:  apiKey,
		}))
	}
	router := reliability.NewAgentRouter(executors...)

	// Two passes over the node set so round-robin routing has a chance to reach
	// every backend at least once.
	dispatches := 2 * len(nodes)
	seen := make(map[string]bool, len(nodes))
	for i := 0; i < dispatches; i++ {
		result, execErr := router.Execute(agent, task)
		if execErr != nil {
			dd.Error = appendDispatchErr(dd.Error, execErr.Error())
			continue
		}
		dd.DispatchCount++
		node := strings.TrimSpace(result.Output)
		if node != "" && !seen[node] {
			seen[node] = true
			dd.NodesHit = append(dd.NodesHit, node)
		}
	}
	dd.DistinctNodes = len(dd.NodesHit)
	dd.OK = dd.Error == "" && dd.DispatchCount >= 2 && dd.DistinctNodes >= 2
	return dd
}

func appendDispatchErr(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
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
