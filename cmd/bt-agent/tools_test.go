package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestRegisterMCPToolsCommentMatchesActualToolCount guards the doc comment on
// registerMCPTools against drift: the comment claims it "registers all N MCP
// tools on the server", and N must equal the true number of tools the function
// wires up. registerMCPTools registers tools both directly (server.RegisterTool
// calls in tools.go) and via the registerBlackboardTools / registerBlockTools /
// registerHITLTools helpers, so the count is summed across every non-test source
// file in the package. When a tool is added or removed, this test fails until
// the comment is corrected.
func TestRegisterMCPToolsCommentMatchesActualToolCount(t *testing.T) {
	// Count every server.RegisterTool( call across the package's non-test Go
	// source. This mirrors exactly what registerMCPTools reaches, directly and
	// through its helper registrars.
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package sources: %v", err)
	}
	actual := 0
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		actual += strings.Count(string(data), "server.RegisterTool(")
	}
	if actual == 0 {
		t.Fatal("found no server.RegisterTool( calls; test cannot verify the count")
	}

	// Extract the documented count from the registerMCPTools doc comment.
	toolsSrc, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	m := regexp.MustCompile(`registers all (\d+) MCP tools`).FindSubmatch(toolsSrc)
	if m == nil {
		t.Fatal("could not find the 'registers all N MCP tools' comment in tools.go")
	}
	documented, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("parse documented count %q: %v", m[1], err)
	}

	if documented != actual {
		t.Errorf("registerMCPTools comment says %d MCP tools but %d are actually registered; correct the comment",
			documented, actual)
	}
}

// TestBTEvolveQDRegisteredAndReturnsQDMetrics pins the bt_evolve_qd MAP-Elites
// quality-diversity MCP tool: it must be registered by registerMCPTools, run a
// deterministic (LLM-free) evolution, insert the evolved population into a
// MAP-Elites grid, and report the QD metrics — diversity_score, cell_count,
// elites, and specialist_distribution — as JSON. An unknown tree id must yield
// the shared {"error":"unknown tree"} shape rather than a partial/panicking
// result.
func TestBTEvolveQDRegisteredAndReturnsQDMetrics(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_qd") {
		t.Fatal("bt_evolve_qd tool must be registered by registerMCPTools")
	}

	// Happy path: a real resolvable base tree with a small population/generations
	// (kept tiny so the deterministic structural evolution stays -short-safe).
	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	res, ok := server.Invoke("bt_evolve_qd", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_qd) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_qd returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_qd result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_qd unexpectedly returned an error for a resolvable tree: %v", out)
	}
	for _, key := range []string{"diversity_score", "cell_count", "elites", "specialist_distribution"} {
		if _, present := out[key]; !present {
			t.Errorf("bt_evolve_qd result missing %q key; got keys %v", key, out)
		}
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_qd", json.RawMessage(`{"tree":"domain:__no_such_tree__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_qd) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_qd returned no content for an unknown tree")
	}
	var errOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut); err != nil {
		t.Fatalf("bt_evolve_qd unknown-tree result is not valid JSON: %v", err)
	}
	if errOut["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_qd unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut)
	}
}

// TestBTEvolveMultiObjectiveRegisteredAndReturnsParetoMetrics pins the
// bt_evolve_multiobjective NSGA-II MCP tool: it must be registered by
// registerMCPTools, run a deterministic (LLM-free) NSGA-II evolution over a
// fixed set of FitnessDimensions, and report the objective dimensions, the best
// tree's node_count, the per-dimension best scores, and the Pareto front size as
// JSON. An unknown tree id must yield the shared {"error":"unknown tree"} shape
// rather than a partial/panicking result.
func TestBTEvolveMultiObjectiveRegisteredAndReturnsParetoMetrics(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_multiobjective") {
		t.Fatal("bt_evolve_multiobjective tool must be registered by registerMCPTools")
	}

	// Happy path: a real resolvable base tree with a small population/generations
	// (kept tiny so the deterministic NSGA-II evolution stays -short-safe).
	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	res, ok := server.Invoke("bt_evolve_multiobjective", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_multiobjective) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_multiobjective returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_multiobjective result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_multiobjective unexpectedly returned an error for a resolvable tree: %v", out)
	}

	// The objective dimensions must be echoed as a non-empty list including the
	// three fixed axes the tool optimizes over.
	dims, ok := out["dimensions"].([]interface{})
	if !ok || len(dims) == 0 {
		t.Fatalf("bt_evolve_multiobjective result must echo a non-empty 'dimensions' list; got %v", out["dimensions"])
	}
	wantDims := map[string]bool{"success_rate": false, "node_efficiency": false, "stability": false}
	for _, d := range dims {
		if s, isStr := d.(string); isStr {
			if _, tracked := wantDims[s]; tracked {
				wantDims[s] = true
			}
		}
	}
	for name, present := range wantDims {
		if !present {
			t.Errorf("bt_evolve_multiobjective 'dimensions' missing %q; got %v", name, dims)
		}
	}

	// The best tree must have a non-zero node count.
	nodeCount, ok := out["node_count"].(float64)
	if !ok || nodeCount <= 0 {
		t.Errorf("bt_evolve_multiobjective must report a non-zero 'node_count'; got %v", out["node_count"])
	}

	// Per-dimension best scores must be reported.
	if _, present := out["dimension_bests"]; !present {
		t.Errorf("bt_evolve_multiobjective result missing 'dimension_bests' key; got keys %v", out)
	}

	// The Pareto front size must be reported and be at least one individual.
	frontSize, ok := out["pareto_front_size"].(float64)
	if !ok || frontSize < 1 {
		t.Errorf("bt_evolve_multiobjective must report a 'pareto_front_size' >= 1; got %v", out["pareto_front_size"])
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_multiobjective", json.RawMessage(`{"tree":"domain:__no_such_tree__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_multiobjective) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_multiobjective returned no content for an unknown tree")
	}
	var errOut2 map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut2); err != nil {
		t.Fatalf("bt_evolve_multiobjective unknown-tree result is not valid JSON: %v", err)
	}
	if errOut2["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_multiobjective unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut2)
	}
}
