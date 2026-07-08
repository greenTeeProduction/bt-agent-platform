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
	"github.com/nico/go-bt-evolve/internal/knowledge"
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

// TestBTEvolveIslandRegisteredAndReturnsIslandMetrics pins the bt_evolve_island
// island-model MCP tool: it must be registered by registerMCPTools, run a
// deterministic (LLM-free) evolution across N isolated island populations with
// periodic migration, and report per_island_best (one entry per island),
// migrations, cross_diversity, generations, and islands as JSON. An unknown
// tree id must yield the shared {"error":"unknown tree"} shape rather than a
// partial/panicking result.
func TestBTEvolveIslandRegisteredAndReturnsIslandMetrics(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_island") {
		t.Fatal("bt_evolve_island tool must be registered by registerMCPTools")
	}

	// Happy path: a real resolvable base tree with a small island count,
	// per-island population, and generation count (kept tiny so the
	// deterministic structural evolution stays -short-safe).
	const wantIslands = 2
	args := json.RawMessage(`{"tree":"godev","islands":2,"population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`)
	res, ok := server.Invoke("bt_evolve_island", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_island) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_island returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_island result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_island unexpectedly returned an error for a resolvable tree: %v", out)
	}
	for _, key := range []string{"per_island_best", "migrations", "cross_diversity", "generations", "islands"} {
		if _, present := out[key]; !present {
			t.Errorf("bt_evolve_island result missing %q key; got keys %v", key, out)
		}
	}

	// per_island_best must hold exactly one best-fitness entry per island.
	perIsland, ok := out["per_island_best"].(map[string]interface{})
	if !ok {
		t.Fatalf("bt_evolve_island 'per_island_best' must be a JSON object; got %T (%v)", out["per_island_best"], out["per_island_best"])
	}
	if len(perIsland) != wantIslands {
		t.Errorf("bt_evolve_island 'per_island_best' must hold exactly %d entries (one per island); got %d: %v", wantIslands, len(perIsland), perIsland)
	}

	// The island count must be echoed back.
	if islands, isNum := out["islands"].(float64); !isNum || int(islands) != wantIslands {
		t.Errorf("bt_evolve_island must echo 'islands' = %d; got %v", wantIslands, out["islands"])
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_island", json.RawMessage(`{"tree":"domain:__no_such_tree__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_island) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_island returned no content for an unknown tree")
	}
	var errOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut); err != nil {
		t.Fatalf("bt_evolve_island unknown-tree result is not valid JSON: %v", err)
	}
	if errOut["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_island unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut)
	}
}

// TestBTEvolveIslandDomainSeeding pins the domain-seeded island mode of
// bt_evolve_island: an optional comma-separated 'domains' param maps each
// named registered domain tree (resolved via resolveTree("domain:"+name)) to
// its own island, so per_island_best is keyed by domain name instead of the
// anonymous island_N labels, and the numeric 'islands' param is ignored. An
// unresolvable domain name must fail fast with
// {"error":"unknown domain: <name>"} and no partial evolution result.
func TestBTEvolveIslandDomainSeeding(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	// Happy path: two registered domain trees seed two islands keyed by their
	// domain names. The conflicting islands=5 must be ignored in favor of the
	// domain list. Params stay tiny so the deterministic structural evolution
	// remains -short-safe.
	args := json.RawMessage(`{"tree":"godev","domains":"code_review,security_audit","islands":5,"population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`)
	res, ok := server.Invoke("bt_evolve_island", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_island) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_island returned no content for domain-seeded islands")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_island domain-seeded result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_island unexpectedly returned an error for registered domains: %v", out)
	}

	perIsland, isObj := out["per_island_best"].(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_island 'per_island_best' must be a JSON object; got %T (%v)", out["per_island_best"], out["per_island_best"])
	}
	wantDomains := []string{"code_review", "security_audit"}
	if len(perIsland) != len(wantDomains) {
		t.Errorf("bt_evolve_island with domains %v must key 'per_island_best' by exactly those domains; got %d entries: %v", wantDomains, len(perIsland), perIsland)
	}
	for _, name := range wantDomains {
		if _, present := perIsland[name]; !present {
			t.Errorf("bt_evolve_island 'per_island_best' missing domain key %q; got keys %v", name, perIsland)
		}
	}

	// Unknown domain: the whole invocation must fail fast with the
	// domain-specific error shape and produce no partial evolution result.
	unknown, ok := server.Invoke("bt_evolve_island", json.RawMessage(`{"tree":"godev","domains":"code_review,__no_such_domain__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_island) reported the tool as unregistered on the unknown-domain path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_island returned no content for an unknown domain")
	}
	var errOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut); err != nil {
		t.Fatalf("bt_evolve_island unknown-domain result is not valid JSON: %v", err)
	}
	if errOut["error"] != "unknown domain: __no_such_domain__" {
		t.Fatalf("bt_evolve_island unknown domain should return {\"error\":\"unknown domain: __no_such_domain__\"}; got %v", errOut)
	}
	if _, partial := errOut["per_island_best"]; partial {
		t.Fatalf("bt_evolve_island unknown-domain error must carry no partial result; got %v", errOut)
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

// TestBTEvolveBottlenecksRegisteredAndReturnsBeforeAfterReport pins the
// bt_evolve_bottlenecks MCP tool that closes the learn→discover→evolve loop:
// it must be registered by registerMCPTools, consume the knowledge graph's
// bottleneck list (trees with RunCount >= 3 and Fitness < 30 — the same
// criteria ComputeAnalytics uses, today surfaced only as human-readable
// SuggestedActions strings), run a deterministic (LLM-free) experience-grounded
// evolution on each underperforming tree, and report per-tree before/after
// fitness as JSON. Healthy trees must be left alone, a bottleneck whose KG id
// does not resolve to a real behavior tree must be skipped (reported under
// "skipped") without aborting the rest of the report, and a graph with no
// bottlenecks must yield an empty report rather than an error. A nil
// experience bank (as in this test's bare mcpDeps) must degrade gracefully,
// with the consulted bank surfaced via "experience_bank_entries".
func TestBTEvolveBottlenecksRegisteredAndReturnsBeforeAfterReport(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	// Underperforming tree that resolves to a real domain behavior tree.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 12, RunCount: 5,
	})
	// Healthy tree: must never appear in the evolution report.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:security_audit", Name: "Security Audit", Category: "domain",
		Fitness: 85, RunCount: 10,
	})
	// Underperforming tree whose id resolves to no behavior tree (unknown
	// domain suffix): must be skipped, not abort the whole invocation.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:__no_such_tree__", Name: "Ghost", Category: "domain",
		Fitness: 5, RunCount: 4,
	})

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{kg: kg})

	if !server.HasTool("bt_evolve_bottlenecks") {
		t.Fatal("bt_evolve_bottlenecks tool must be registered by registerMCPTools")
	}

	// Happy path: tiny population/generations so the deterministic structural
	// evolution stays -short-safe.
	res, ok := server.Invoke("bt_evolve_bottlenecks", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_bottlenecks) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_bottlenecks returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_bottlenecks result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_bottlenecks unexpectedly returned an error for a graph with bottlenecks: %v", out)
	}

	// Both underperformers count as detected bottlenecks.
	if n, isNum := out["bottlenecks"].(float64); !isNum || int(n) != 2 {
		t.Errorf("bt_evolve_bottlenecks must report 'bottlenecks' = 2 (both RunCount>=3, Fitness<30 trees); got %v", out["bottlenecks"])
	}

	// The per-tree report holds exactly the one resolvable bottleneck, with its
	// KG fitness echoed as before_fitness and a computed after_fitness.
	report, isList := out["report"].([]interface{})
	if !isList {
		t.Fatalf("bt_evolve_bottlenecks must return a 'report' JSON array; got %T (%v)", out["report"], out["report"])
	}
	if len(report) != 1 {
		t.Fatalf("bt_evolve_bottlenecks 'report' must hold exactly the 1 resolvable bottleneck; got %d entries: %v", len(report), report)
	}
	entry, isObj := report[0].(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_bottlenecks report entries must be JSON objects; got %T (%v)", report[0], report[0])
	}
	if entry["tree"] != "domain:code_review" {
		t.Errorf("bt_evolve_bottlenecks report entry must name 'tree' = %q; got %v", "domain:code_review", entry["tree"])
	}
	if before, isNum := entry["before_fitness"].(float64); !isNum || before != 12 {
		t.Errorf("bt_evolve_bottlenecks report entry must echo the KG fitness as 'before_fitness' = 12; got %v", entry["before_fitness"])
	}
	if after, isNum := entry["after_fitness"].(float64); !isNum || after <= 0 {
		t.Errorf("bt_evolve_bottlenecks report entry must carry a positive evolved 'after_fitness'; got %v", entry["after_fitness"])
	}
	if runs, isNum := entry["runs"].(float64); !isNum || int(runs) != 5 {
		t.Errorf("bt_evolve_bottlenecks report entry must echo 'runs' = 5; got %v", entry["runs"])
	}
	for _, e := range report {
		if m, isMap := e.(map[string]interface{}); isMap && m["tree"] == "domain:security_audit" {
			t.Errorf("bt_evolve_bottlenecks must not evolve the healthy tree domain:security_audit; report: %v", report)
		}
	}

	// The unresolvable bottleneck is surfaced under 'skipped', not silently
	// dropped and not fatal.
	skipped, isList := out["skipped"].([]interface{})
	if !isList || len(skipped) != 1 || skipped[0] != "domain:__no_such_tree__" {
		t.Errorf("bt_evolve_bottlenecks must list the unresolvable bottleneck under 'skipped' = [\"domain:__no_such_tree__\"]; got %v", out["skipped"])
	}

	// Experience-grounded: the consulted bank is reported even when nil (0).
	if _, present := out["experience_bank_entries"]; !present {
		t.Errorf("bt_evolve_bottlenecks result missing 'experience_bank_entries' key; got keys %v", out)
	}

	// No bottlenecks: a healthy-only graph yields an empty report, not an error.
	healthyOnly := knowledge.NewKnowledgeGraph()
	healthyOnly.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 90, RunCount: 20,
	})
	server2 := engine.NewServer("test2")
	registerMCPTools(server2, &mcpDeps{kg: healthyOnly})
	empty, ok := server2.Invoke("bt_evolve_bottlenecks", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_bottlenecks) reported the tool as unregistered on the no-bottleneck path")
	}
	if empty == nil || len(empty.Content) == 0 {
		t.Fatal("bt_evolve_bottlenecks returned no content for a healthy graph")
	}
	var emptyOut map[string]interface{}
	if err := json.Unmarshal([]byte(empty.Content[0].Text), &emptyOut); err != nil {
		t.Fatalf("bt_evolve_bottlenecks healthy-graph result is not valid JSON: %v (text=%q)", err, empty.Content[0].Text)
	}
	if _, isErr := emptyOut["error"]; isErr {
		t.Fatalf("bt_evolve_bottlenecks must not error on a graph without bottlenecks; got %v", emptyOut)
	}
	if n, isNum := emptyOut["bottlenecks"].(float64); !isNum || int(n) != 0 {
		t.Errorf("bt_evolve_bottlenecks must report 'bottlenecks' = 0 for a healthy graph; got %v", emptyOut["bottlenecks"])
	}
	if emptyReport, isList := emptyOut["report"].([]interface{}); !isList || len(emptyReport) != 0 {
		t.Errorf("bt_evolve_bottlenecks must return an empty 'report' array for a healthy graph; got %v", emptyOut["report"])
	}
}
