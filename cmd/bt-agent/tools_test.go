package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
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
//
// Algorithm selection: a bottleneck tree carrying tunable parameters
// (domain:code_review has a Retry MaxRetries knob) must be routed to CMA-ES
// parameter tuning via evolution.TuneTreeParameters — its report entry tagged
// "algorithm":"cmaes" with a positive "tuned_params" count — while a tree with
// no tunable parameters (domain:alert_router) must fall back to the genetic
// EvolveWithExperience path, tagged "algorithm":"genetic". The top-level
// "algorithms" tally must state which algorithm handled how many trees.
func TestBTEvolveBottlenecksRegisteredAndReturnsBeforeAfterReport(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	// Underperforming tree that resolves to a real domain behavior tree with
	// at least one tunable parameter (CMA-ES eligible).
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 12, RunCount: 5,
	})
	// Underperforming tree that resolves to a real domain behavior tree with
	// zero tunable parameters: must fall back to the genetic path.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:alert_router", Name: "Alert Router", Category: "domain",
		Fitness: 8, RunCount: 3,
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

	// All three underperformers count as detected bottlenecks.
	if n, isNum := out["bottlenecks"].(float64); !isNum || int(n) != 3 {
		t.Errorf("bt_evolve_bottlenecks must report 'bottlenecks' = 3 (all RunCount>=3, Fitness<30 trees); got %v", out["bottlenecks"])
	}

	// The per-tree report holds exactly the two resolvable bottlenecks, with
	// the KG fitness echoed as before_fitness and a computed after_fitness.
	// Bottleneck ordering is not guaranteed, so entries are looked up by tree.
	report, isList := out["report"].([]interface{})
	if !isList {
		t.Fatalf("bt_evolve_bottlenecks must return a 'report' JSON array; got %T (%v)", out["report"], out["report"])
	}
	if len(report) != 2 {
		t.Fatalf("bt_evolve_bottlenecks 'report' must hold exactly the 2 resolvable bottlenecks; got %d entries: %v", len(report), report)
	}
	byTree := map[string]map[string]interface{}{}
	for _, e := range report {
		m, isMap := e.(map[string]interface{})
		if !isMap {
			t.Fatalf("bt_evolve_bottlenecks report entries must be JSON objects; got %T (%v)", e, e)
		}
		tree, _ := m["tree"].(string)
		byTree[tree] = m
	}
	if _, evolved := byTree["domain:security_audit"]; evolved {
		t.Errorf("bt_evolve_bottlenecks must not evolve the healthy tree domain:security_audit; report: %v", report)
	}

	// domain:code_review carries a tunable parameter, so it must be routed to
	// CMA-ES parameter tuning and report which algorithm handled it.
	entry := byTree["domain:code_review"]
	if entry == nil {
		t.Fatalf("bt_evolve_bottlenecks report must include an entry for domain:code_review; got %v", report)
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
	if entry["algorithm"] != "cmaes" {
		t.Errorf("bt_evolve_bottlenecks must route the tunable-parameter tree domain:code_review to CMA-ES and tag its entry 'algorithm' = %q; got %v", "cmaes", entry["algorithm"])
	}
	if tuned, isNum := entry["tuned_params"].(float64); !isNum || int(tuned) <= 0 {
		t.Errorf("bt_evolve_bottlenecks cmaes report entry must carry a positive 'tuned_params' count; got %v", entry["tuned_params"])
	}

	// domain:alert_router has no tunable parameters, so it must fall back to
	// the genetic EvolveWithExperience path and say so.
	genEntry := byTree["domain:alert_router"]
	if genEntry == nil {
		t.Fatalf("bt_evolve_bottlenecks report must include an entry for domain:alert_router; got %v", report)
	}
	if before, isNum := genEntry["before_fitness"].(float64); !isNum || before != 8 {
		t.Errorf("bt_evolve_bottlenecks genetic report entry must echo the KG fitness as 'before_fitness' = 8; got %v", genEntry["before_fitness"])
	}
	if after, isNum := genEntry["after_fitness"].(float64); !isNum || after <= 0 {
		t.Errorf("bt_evolve_bottlenecks genetic report entry must carry a positive evolved 'after_fitness'; got %v", genEntry["after_fitness"])
	}
	if genEntry["algorithm"] != "genetic" {
		t.Errorf("bt_evolve_bottlenecks must fall back to the genetic path for the parameterless tree domain:alert_router and tag its entry 'algorithm' = %q; got %v", "genetic", genEntry["algorithm"])
	}

	// The top-level tally states which algorithm handled how many trees.
	algorithms, isMap := out["algorithms"].(map[string]interface{})
	if !isMap {
		t.Fatalf("bt_evolve_bottlenecks must return a top-level 'algorithms' tally object; got %T (%v)", out["algorithms"], out["algorithms"])
	}
	if n, isNum := algorithms["cmaes"].(float64); !isNum || int(n) != 1 {
		t.Errorf("bt_evolve_bottlenecks 'algorithms' tally must count 1 cmaes tree; got %v", algorithms["cmaes"])
	}
	if n, isNum := algorithms["genetic"].(float64); !isNum || int(n) != 1 {
		t.Errorf("bt_evolve_bottlenecks 'algorithms' tally must count 1 genetic tree; got %v", algorithms["genetic"])
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

// TestBTEvolveMemeticRegisteredAndValidatesStrategy pins the bt_evolve_memetic
// MCP tool (Q2 Evolvability milestone 2/5): it must be registered by
// registerMCPTools and expose Population.MemeticEvolve with a selectable
// LocalSearchStrategy. Each of the three implemented strategies — hill-climb,
// simulated-annealing, and tabu — must run a deterministic (LLM-free) memetic
// evolution and report best_fitness, generations, best_nodes, and the echoed
// strategy as JSON. Omitting the strategy falls back to the documented default
// (hill-climb) and echoes it. An unknown strategy value must be rejected with
// the structured {"error":"unknown strategy: <value>"} shape — never silently
// mapped to a default — and must not leak a partial happy-path result. An
// unknown tree id keeps the shared {"error":"unknown tree"} shape.
func TestBTEvolveMemeticRegisteredAndValidatesStrategy(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_memetic") {
		t.Fatal("bt_evolve_memetic tool must be registered by registerMCPTools")
	}

	// Happy path per strategy: a real resolvable base tree with a small
	// population/generations (kept tiny so the deterministic structural
	// evolution stays -short-safe).
	for _, strategy := range []string{"hill-climb", "simulated-annealing", "tabu"} {
		t.Run(strategy, func(t *testing.T) {
			args := json.RawMessage(fmt.Sprintf(
				`{"tree":"godev","population":4,"generations":2,"strategy":%q}`, strategy))
			res, ok := server.Invoke("bt_evolve_memetic", args)
			if !ok {
				t.Fatal("Invoke(bt_evolve_memetic) reported the tool as unregistered")
			}
			if res == nil || len(res.Content) == 0 {
				t.Fatal("bt_evolve_memetic returned no content")
			}
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
				t.Fatalf("bt_evolve_memetic result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
			}
			if _, isErr := out["error"]; isErr {
				t.Fatalf("bt_evolve_memetic unexpectedly returned an error for strategy %q: %v", strategy, out)
			}
			if out["strategy"] != strategy {
				t.Errorf("bt_evolve_memetic must echo 'strategy' = %q; got %v", strategy, out["strategy"])
			}
			for _, key := range []string{"best_fitness", "generations", "best_nodes"} {
				if _, present := out[key]; !present {
					t.Errorf("bt_evolve_memetic result missing %q key; got keys %v", key, out)
				}
			}
			if bf, isNum := out["best_fitness"].(float64); !isNum || bf <= 0 {
				t.Errorf("bt_evolve_memetic must report a positive numeric 'best_fitness'; got %v", out["best_fitness"])
			}
		})
	}

	// Omitted strategy: falls back to the documented default and says so.
	def, ok := server.Invoke("bt_evolve_memetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_memetic) reported the tool as unregistered on the default-strategy path")
	}
	if def == nil || len(def.Content) == 0 {
		t.Fatal("bt_evolve_memetic returned no content for an omitted strategy")
	}
	var defOut map[string]interface{}
	if err := json.Unmarshal([]byte(def.Content[0].Text), &defOut); err != nil {
		t.Fatalf("bt_evolve_memetic omitted-strategy result is not valid JSON: %v (text=%q)", err, def.Content[0].Text)
	}
	if _, isErr := defOut["error"]; isErr {
		t.Fatalf("bt_evolve_memetic must not reject an omitted strategy (default hill-climb); got %v", defOut)
	}
	if defOut["strategy"] != "hill-climb" {
		t.Errorf("bt_evolve_memetic must echo the default 'strategy' = \"hill-climb\" when omitted; got %v", defOut["strategy"])
	}

	// Unknown strategy: must surface a structured MCP error naming the bad
	// value — a silent default would mask caller typos — with no partial
	// happy-path result (proof no evolution ran).
	bad, ok := server.Invoke("bt_evolve_memetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2,"strategy":"quantum"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_memetic) reported the tool as unregistered on the unknown-strategy path")
	}
	if bad == nil || len(bad.Content) == 0 {
		t.Fatal("bt_evolve_memetic returned no content for an unknown strategy")
	}
	var badOut map[string]interface{}
	if err := json.Unmarshal([]byte(bad.Content[0].Text), &badOut); err != nil {
		t.Fatalf("bt_evolve_memetic unknown-strategy result is not valid JSON: %v", err)
	}
	if badOut["error"] != "unknown strategy: quantum" {
		t.Fatalf("bt_evolve_memetic unknown strategy should return {\"error\":\"unknown strategy: quantum\"}; got %v", badOut)
	}
	if _, partial := badOut["best_fitness"]; partial {
		t.Errorf("bt_evolve_memetic unknown-strategy rejection must not carry a partial 'best_fitness' result; got %v", badOut)
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_memetic", json.RawMessage(`{"tree":"domain:__no_such_tree__","strategy":"hill-climb"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_memetic) reported the tool as unregistered on the unknown-tree path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_memetic returned no content for an unknown tree")
	}
	var errOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut); err != nil {
		t.Fatalf("bt_evolve_memetic unknown-tree result is not valid JSON: %v", err)
	}
	if errOut["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_memetic unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut)
	}
}

// TestEvolveToolsRejectDegeneratePopulationAtMCPBoundary pins the MCP-boundary
// validation for degenerate evolve params (Q3 defense-in-depth on top of the
// engine-side eliteCount clamp): all six evolve tools — bt_evolve_genetic,
// bt_evolve_multiobjective, bt_evolve_bottlenecks, bt_evolve_memetic,
// bt_evolve_qd, and bt_evolve_island — must reject an explicitly supplied population < 2
// with the structured {"error":"population must be at least 2"} shape before
// any engine work runs — a one-individual "population" cannot evolve (elitism
// and crossover both need two individuals) and historically panicked deep in
// the engine. The rejection must not leak a partial happy-path result, must
// fire before dependency checks (bt_evolve_bottlenecks rejects population 1
// even when the knowledge graph is unavailable), and must not break the
// documented default: omitting population still falls back to 20 and succeeds.
func TestEvolveToolsRejectDegeneratePopulationAtMCPBoundary(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	cases := []struct {
		tool string
		// args carries a %d placeholder for the degenerate population value.
		args string
		// partialKey is a happy-path result key that must never accompany the
		// structured error (proof the engine was not reached).
		partialKey string
	}{
		{"bt_evolve_genetic", `{"tree":"godev","population":%d,"generations":2}`, "best_fitness"},
		{"bt_evolve_multiobjective", `{"tree":"godev","population":%d,"generations":2}`, "pareto_front_size"},
		{"bt_evolve_bottlenecks", `{"population":%d,"generations":2}`, "report"},
		{"bt_evolve_memetic", `{"tree":"godev","population":%d,"generations":2,"strategy":"hill-climb"}`, "best_fitness"},
		{"bt_evolve_qd", `{"tree":"godev","population":%d,"generations":2}`, "diversity_score"},
		{"bt_evolve_island", `{"tree":"godev","population":%d,"generations":2}`, "per_island_best"},
	}
	for _, tc := range cases {
		for _, pop := range []int{1, -3} {
			args := json.RawMessage(fmt.Sprintf(tc.args, pop))
			res, ok := server.Invoke(tc.tool, args)
			if !ok {
				t.Fatalf("Invoke(%s) reported the tool as unregistered", tc.tool)
			}
			if res == nil || len(res.Content) == 0 {
				t.Fatalf("%s returned no content for population=%d", tc.tool, pop)
			}
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
				t.Fatalf("%s population=%d result is not valid JSON: %v (text=%q)", tc.tool, pop, err, res.Content[0].Text)
			}
			if out["error"] != "population must be at least 2" {
				t.Errorf("%s must reject population=%d with {\"error\":\"population must be at least 2\"}; got %v", tc.tool, pop, out)
			}
			if _, partial := out[tc.partialKey]; partial {
				t.Errorf("%s population=%d rejection must not carry a partial %q result; got %v", tc.tool, pop, tc.partialKey, out)
			}
		}
	}

	// Omitting population entirely must keep the documented default (20), not
	// trip the degenerate-param rejection.
	res, ok := server.Invoke("bt_evolve_genetic", json.RawMessage(`{"tree":"godev","generations":1}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_genetic) reported the tool as unregistered on the default-population path")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_genetic returned no content for an omitted population")
	}
	var defOut map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &defOut); err != nil {
		t.Fatalf("bt_evolve_genetic omitted-population result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := defOut["error"]; isErr {
		t.Fatalf("bt_evolve_genetic must not reject an omitted population (default 20); got %v", defOut)
	}

	// The boundary check must precede dependency checks: a degenerate
	// population is rejected as such even while the knowledge graph is
	// unavailable, but a valid invocation still surfaces the dependency error.
	kgless, ok := server.Invoke("bt_evolve_bottlenecks", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_bottlenecks) reported the tool as unregistered on the kg-less path")
	}
	if kgless == nil || len(kgless.Content) == 0 {
		t.Fatal("bt_evolve_bottlenecks returned no content without a knowledge graph")
	}
	var kglessOut map[string]interface{}
	if err := json.Unmarshal([]byte(kgless.Content[0].Text), &kglessOut); err != nil {
		t.Fatalf("bt_evolve_bottlenecks kg-less result is not valid JSON: %v", err)
	}
	if kglessOut["error"] != "knowledge graph unavailable" {
		t.Fatalf("bt_evolve_bottlenecks with a valid population and no knowledge graph must keep returning {\"error\":\"knowledge graph unavailable\"}; got %v", kglessOut)
	}
}

// TestBTEvolveQLearningRegisteredAndLearnsGreedily pins the bt_evolve_qlearning
// MCP tool (Q2 Evolvability milestone 3/5): it must be registered by
// registerMCPTools and drive mutation-category selection across generations via
// QTable.GetState/SelectAction/Update, reporting the learned per-state best
// actions alongside the evolved winner. epsilon=0 makes the run deterministic
// once a state has Q-values (pure greedy selection, no exploration), so the
// learned_actions map must be non-empty and every learned action must be one of
// the five known mutation categories. An unknown tree id must yield the shared
// {"error":"unknown tree"} shape.
func TestBTEvolveQLearningRegisteredAndLearnsGreedily(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_qlearning") {
		t.Fatal("bt_evolve_qlearning tool must be registered by registerMCPTools")
	}

	// Happy path: epsilon=0 for a deterministic greedy policy, small
	// population/generations to stay -short-safe (LLM-free structural fitness).
	args := json.RawMessage(`{"tree":"godev","population":4,"generations":3,"epsilon":0}`)
	res, ok := server.Invoke("bt_evolve_qlearning", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_qlearning) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_qlearning returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_qlearning result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_qlearning unexpectedly returned an error for a resolvable tree: %v", out)
	}

	// The evolved winner must be reported alongside the learned policy.
	if fitness, isNum := out["best_fitness"].(float64); !isNum || fitness <= 0 {
		t.Errorf("bt_evolve_qlearning must report a positive numeric 'best_fitness'; got %v", out["best_fitness"])
	}
	if nodes, isNum := out["best_nodes"].(float64); !isNum || nodes <= 0 {
		t.Errorf("bt_evolve_qlearning must report a positive numeric 'best_nodes' for the evolved winner; got %v", out["best_nodes"])
	}
	if eps, isNum := out["epsilon"].(float64); !isNum || eps != 0 {
		t.Errorf("bt_evolve_qlearning must echo the requested 'epsilon' = 0; got %v", out["epsilon"])
	}

	// learned_actions is the per-state best-action map extracted from the
	// QTable after the final generation. Running generations>=1 with Update
	// applied each generation guarantees at least one learned state.
	learned, isObj := out["learned_actions"].(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_qlearning 'learned_actions' must be a JSON object mapping state -> best action; got %T (%v)", out["learned_actions"], out["learned_actions"])
	}
	if len(learned) == 0 {
		t.Fatal("bt_evolve_qlearning 'learned_actions' must be non-empty after learning generations (QTable.Update was never applied)")
	}
	validActions := map[string]bool{
		"add_before": true, "add_after": true, "add_fallback": true,
		"replace_node": true, "remove_node": true,
	}
	for state, action := range learned {
		// QTable.GetState encodes states as "<category>:<size-bucket>:<depth>".
		if strings.Count(state, ":") != 2 {
			t.Errorf("bt_evolve_qlearning learned state %q must use the QTable.GetState \"category:bucket:depth\" encoding", state)
		}
		actionStr, isStr := action.(string)
		if !isStr || !validActions[actionStr] {
			t.Errorf("bt_evolve_qlearning learned action for state %q must be one of the five mutation categories; got %v", state, action)
		}
	}

	// Unknown tree: shared error shape, no partial result.
	unknown, ok := server.Invoke("bt_evolve_qlearning", json.RawMessage(`{"tree":"domain:__no_such_tree__","epsilon":0}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_qlearning) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_qlearning returned no content for an unknown tree")
	}
	var errOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut); err != nil {
		t.Fatalf("bt_evolve_qlearning unknown-tree result is not valid JSON: %v", err)
	}
	if errOut["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_qlearning unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut)
	}
	if _, partial := errOut["learned_actions"]; partial {
		t.Errorf("bt_evolve_qlearning unknown-tree error must carry no partial 'learned_actions'; got %v", errOut)
	}
}

// injectSelectorProbeTree installs a DynamicResolveFn that resolves the
// unqualified probe id "domain:selector_probe" to a fixed Selector-ordering
// tree (Router selector over Cheap, Reliable, and an AlwaysSucceed Fallback),
// mirroring the gardener's selectorOrderingTree fixture. Every other id resolves
// to nil so the unknown-tree path and the qd/island tests' "domain:__no_such_tree__"
// expectations are unaffected. The previous resolver is restored on cleanup so
// this global does not leak into sibling tests (see the A2A tree-resolver global
// leak lesson).
func injectSelectorProbeTree(t *testing.T) {
	t.Helper()
	prev := domains.DynamicResolveFn
	t.Cleanup(func() { domains.DynamicResolveFn = prev })
	domains.DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id != "domain:selector_probe" {
			return nil
		}
		return &evolution.SerializableNode{
			Type: "Sequence", Name: "Root",
			Children: []evolution.SerializableNode{
				{
					Type: "Selector", Name: "Router",
					Children: []evolution.SerializableNode{
						{Type: "Sequence", Name: "Cheap"},
						{Type: "Sequence", Name: "Reliable"},
						{Type: "AlwaysSucceed", Name: "Fallback"},
					},
				},
			},
		}
	}
}

// seedSelectorProbeStats writes durable Selector telemetry to path so that under
// the "Router" selector the Reliable child (0.90 success) beats the Cheap child
// (0.20 success); the AlwaysSucceed Fallback has a perfect success rate but must
// stay last under the fallback/default-path guard. This is the on-disk format
// SelectorOptimizer.SaveSelectorStats produces, which the tool must load.
func seedSelectorProbeStats(t *testing.T, path string) {
	t.Helper()
	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	rec := func(child, outcome string, n int) {
		for i := 0; i < n; i++ {
			so.Record("Router", evolution.NodeExecutionRecord{NodeName: child, Outcome: outcome})
		}
	}
	rec("Cheap", "success", 2)
	rec("Cheap", "failure", 8) // 0.20 success rate
	rec("Reliable", "success", 9)
	rec("Reliable", "failure", 1)  // 0.90 success rate
	rec("Fallback", "success", 10) // 1.00 — guard keeps it last anyway
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}
}

// TestBTEvolveSelectorsRegisteredAndReordersFromDurableStats pins the
// bt_evolve_selectors MCP tool (Selector-ordering optimizer milestone 5/5): it
// must be registered by registerMCPTools, load the durable Selector telemetry
// from a supplied stats path, run the deterministic reordering pass over a named
// tree, and report the per-Selector reorder count plus an entropy/information-gain
// metric as JSON. The seeded telemetry makes the Reliable child out-rank the
// Cheap child under the one "Router" Selector, so exactly one Selector must be
// reordered. An empty/missing-stats input must be handled cleanly (zero reorders,
// no error) rather than panicking, and an unknown tree id must yield the shared
// {"error":"unknown tree"} shape.
func TestBTEvolveSelectorsRegisteredAndReordersFromDurableStats(t *testing.T) {
	injectSelectorProbeTree(t)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_selectors") {
		t.Fatal("bt_evolve_selectors tool must be registered by registerMCPTools")
	}

	// Happy path: a resolvable tree plus durable telemetry that flips one
	// Selector's child ordering (Reliable ahead of Cheap, Fallback last).
	statsPath := filepath.Join(t.TempDir(), "selector_stats.json")
	seedSelectorProbeStats(t, statsPath)

	args := json.RawMessage(fmt.Sprintf(`{"tree":"domain:selector_probe","stats_path":%q}`, statsPath))
	res, ok := server.Invoke("bt_evolve_selectors", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_selectors) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_selectors returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_selectors result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_selectors unexpectedly returned an error for a resolvable tree with telemetry: %v", out)
	}

	// The per-Selector reorder count must be reported, and the seeded telemetry
	// reorders exactly one Selector ("Router").
	reorders, isNum := out["reorders"].(float64)
	if !isNum {
		t.Fatalf("bt_evolve_selectors must report a numeric 'reorders' count; got %T (%v)", out["reorders"], out["reorders"])
	}
	if int(reorders) != 1 {
		t.Errorf("bt_evolve_selectors must reorder exactly the one telemetry-flipped Selector; got reorders=%v", out["reorders"])
	}

	// An entropy / information-gain reduction metric must accompany the count.
	ig, isNum := out["information_gain"].(float64)
	if !isNum {
		t.Fatalf("bt_evolve_selectors must report a numeric 'information_gain' metric; got %T (%v)", out["information_gain"], out["information_gain"])
	}
	if ig < 0 {
		t.Errorf("bt_evolve_selectors 'information_gain' must be non-negative; got %v", ig)
	}

	// The reordered tree must be run through the persistence path so its outcome
	// is reported (persisted true, or a persist/validation error under bare deps).
	if _, present := out["persisted"]; !present {
		t.Errorf("bt_evolve_selectors must report a 'persisted' outcome for the reordered tree; got keys %v", out)
	}

	// Empty/missing-stats boundary: a resolvable tree with no telemetry must be
	// handled cleanly (zero reorders, no error) rather than panicking.
	emptyStats := filepath.Join(t.TempDir(), "does_not_exist.json")
	empty, ok := server.Invoke("bt_evolve_selectors", json.RawMessage(fmt.Sprintf(`{"tree":"domain:selector_probe","stats_path":%q}`, emptyStats)))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selectors) reported the tool as unregistered on the empty-stats path")
	}
	if empty == nil || len(empty.Content) == 0 {
		t.Fatal("bt_evolve_selectors returned no content for empty stats")
	}
	var emptyOut map[string]interface{}
	if err := json.Unmarshal([]byte(empty.Content[0].Text), &emptyOut); err != nil {
		t.Fatalf("bt_evolve_selectors empty-stats result is not valid JSON: %v (text=%q)", err, empty.Content[0].Text)
	}
	if _, isErr := emptyOut["error"]; isErr {
		t.Fatalf("bt_evolve_selectors must not error on empty telemetry; got %v", emptyOut)
	}
	if n, isNum := emptyOut["reorders"].(float64); !isNum || int(n) != 0 {
		t.Errorf("bt_evolve_selectors must report 'reorders' = 0 for empty telemetry; got %v", emptyOut["reorders"])
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_selectors", json.RawMessage(`{"tree":"domain:__no_such_tree__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selectors) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_selectors returned no content for an unknown tree")
	}
	var errOut2 map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut2); err != nil {
		t.Fatalf("bt_evolve_selectors unknown-tree result is not valid JSON: %v", err)
	}
	if errOut2["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_selectors unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut2)
	}
}
