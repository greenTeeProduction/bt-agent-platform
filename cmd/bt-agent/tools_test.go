package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reliability"
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

// TestBTEvolveQDAccumulatesDurableArchive pins the missing durable-archive
// wiring for the MAP-Elites illuminator (Q2 Evolvability, NotebookLM
// research): bt_evolve_qd currently builds a fresh evolution.NewMAPElitesGrid
// on every call and discards it, so illuminated niches never survive across
// invocations even though MAPElitesGrid already implements Save/Load/Cap
// (internal/evolution/map_elites.go). Mirroring bt_evolve_island and
// bt_evolve_qlearning, the grid must warm-start from a durable per-tree
// archive and persist back to it after every run. The result JSON must
// report the warm start honestly — "warm_started": false on a cold home,
// true once an archive exists — and a single archive file must exist under
// BT_AGENT_HOME after the first run.
func TestBTEvolveQDAccumulatesDurableArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_qd", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_qd) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_qd returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_qd %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_qd unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}

	first := invoke("first")
	if got, isBool := first["warm_started"].(bool); !isBool || got {
		t.Errorf(`first run on a cold home must report "warm_started": false; got %v`, first["warm_started"])
	}

	matches, err := filepath.Glob(filepath.Join(home, "map_elites_archive*.json"))
	if err != nil {
		t.Fatalf("glob map-elites archives after the first run: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("bt_evolve_qd single-tree runs must persist exactly one durable MAP-Elites archive under BT_AGENT_HOME after the first run; got %v", matches)
	}

	second := invoke("second")
	if got, isBool := second["warm_started"].(bool); !isBool || !got {
		t.Errorf(`second run must warm-start from the durable archive and report "warm_started": true; got %v`, second["warm_started"])
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
	// Isolate the durable island archive (milestone 3/5): without this the
	// tool would warm-start from — and persist test state into — the real
	// platform home.
	t.Setenv("BT_AGENT_HOME", t.TempDir())

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
	// Isolate the durable island archive (milestone 3/5): without this the
	// tool would warm-start from — and persist test state into — the real
	// platform home.
	t.Setenv("BT_AGENT_HOME", t.TempDir())

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

// TestBTEvolveIslandWiresExperienceBankIntoMigration pins the NotebookLM
// research gap: bt_evolve_island constructs its IslandModel via
// evolution.NewIslandModel but never assigns the model's Bank field from
// deps.expBank, even though IslandModel.Migrate already consults Bank (when
// set) to call ExperienceBank.SeedDomain and carry experience entries across
// migrating domains. Without the assignment, migration between islands never
// touches the bank, no matter how many entries it holds. This test seeds the
// bank with one entry tagged for the "code_review" domain, runs bt_evolve_island
// in two-domain mode with migration_interval=1 (so Migrate fires deterministically
// on generation 1, migrating individuals in both directions between the only two
// islands), and asserts SeedDomain's effect is observable: a new entry re-tagged
// for the "security_audit" domain must appear in the bank.
func TestBTEvolveIslandWiresExperienceBankIntoMigration(t *testing.T) {
	// Isolate the durable island archive (milestone 3/5): without this the
	// tool would warm-start from — and persist test state into — the real
	// platform home.
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	// Seed directly (bypassing AddFromMutation's name-derived TreeType) so the
	// entry is unambiguously tagged for the "code_review" domain island.
	bank.Entries = append(bank.Entries, evolution.ExperienceEntry{
		ID:           "seed_code_review",
		TreeType:     "code_review",
		QualityScore: 1.0,
		FitnessDelta: 0.2,
	})
	if bank.Count() != 1 {
		t.Fatalf("seeded bank Count() = %d, want 1", bank.Count())
	}

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{expBank: bank})

	args := json.RawMessage(`{"tree":"godev","domains":"code_review,security_audit","population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`)
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
		t.Fatalf("bt_evolve_island unexpectedly returned an error: %v", out)
	}
	if migrations, _ := out["migrations"].(float64); migrations <= 0 {
		t.Fatalf("bt_evolve_island must report migrations > 0 with migration_interval=1 and two islands; got %v", out["migrations"])
	}

	found := false
	for _, e := range bank.Entries {
		if strings.EqualFold(e.TreeType, "security_audit") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after migration, the bank must contain an entry re-tagged for \"security_audit\" (via IslandModel.Bank.SeedDomain during Migrate); bank has %d entries and none are tagged security_audit: %+v", bank.Count(), bank.Entries)
	}
	if bank.Count() < 2 {
		t.Errorf("bt_evolve_island's IslandModel must be wired with deps.expBank so migration seeds cross-domain experiences; bank.Count() = %d, want >= 2 (original seed + migrated copy)", bank.Count())
	}
}

// TestBTEvolveIslandDomainsModeWritesFitnessBackPerDomain pins milestone 4/5
// of the production-safe island archive program: correct evolved-fitness
// attribution in domains mode. When each island is seeded from a registered
// domain tree, the evolved quality was earned by those domain trees — so each
// seeded domain island's own best elite fitness must be written back to its
// domain:<name> knowledge-graph entry through the evolved path
// (StructuralFitness + EvolvedCount, never the runtime-success EMA), and the
// base tree must NOT be credited with the single cross-island best. Crediting
// params.Tree for fitness that domain-seeded islands earned steers
// fitness-aware discovery toward a tree whose genome the elites never came
// from. Default (anonymous-islands) mode keeps the existing contract: the base
// tree seeded every island, so it alone receives the cross-island best.
func TestBTEvolveIslandDomainsModeWritesFitnessBackPerDomain(t *testing.T) {
	// Isolate the durable island archive so the run neither warm-starts from
	// nor persists test state into the real platform home.
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "godev", Name: "Go Dev", Category: "domain"})
	domainEntries := []struct {
		island  string
		kgID    string
		fitness float64
		runs    int
	}{
		{island: "code_review", kgID: "domain:code_review", fitness: 50, runs: 3},
		{island: "security_audit", kgID: "domain:security_audit", fitness: 60, runs: 4},
	}
	for _, d := range domainEntries {
		kg.Register(&knowledge.TreeMeta{
			ID: d.kgID, Name: d.island, Category: "domain",
			Fitness: d.fitness, RunCount: d.runs,
		})
	}

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{kg: kg})

	// Params stay tiny so the deterministic structural evolution remains
	// -short-safe.
	args := json.RawMessage(`{"tree":"godev","domains":"code_review,security_audit","population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`)
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

	for _, d := range domainEntries {
		best, isNum := perIsland[d.island].(float64)
		if !isNum || best <= 0 {
			t.Fatalf("per_island_best[%q] must be a positive number for this attribution pin to be meaningful; got %v", d.island, perIsland[d.island])
		}
		// The evolved write-back clamps into [0,100]; the entries start with a
		// zero StructuralFitness, so monotonicity cannot mask the credit.
		want := best
		if want > 100 {
			want = 100
		}
		tree := kg.Trees[d.kgID]
		if tree == nil {
			t.Fatalf("%s vanished from the knowledge graph after evolution", d.kgID)
		}
		if tree.EvolvedCount != 1 {
			t.Errorf("domains mode must write each seeded domain island's best back to its own KG entry exactly once; %s.EvolvedCount = %d, want 1", d.kgID, tree.EvolvedCount)
		}
		if diff := tree.StructuralFitness - want; diff < -1e-9 || diff > 1e-9 {
			t.Errorf("%s.StructuralFitness = %v, want its own island's best %v — each domain must be credited with its own elite fitness, not the cross-island best (and not nothing)", d.kgID, tree.StructuralFitness, want)
		}
		// The evolved path must leave genuine-execution telemetry alone.
		if tree.Fitness != d.fitness {
			t.Errorf("evolved write-back must not overwrite the runtime-success EMA; %s.Fitness = %v, want %v", d.kgID, tree.Fitness, d.fitness)
		}
		if tree.RunCount != d.runs {
			t.Errorf("evolved write-back must not increment RunCount; %s.RunCount = %d, want %d", d.kgID, tree.RunCount, d.runs)
		}
	}

	// The "instead" half of the contract: in domains mode the base tree seeded
	// nothing, so it must not be credited with the cross-island best.
	base := kg.Trees["godev"]
	if base == nil {
		t.Fatal("godev vanished from the knowledge graph after evolution")
	}
	if base.EvolvedCount != 0 || base.StructuralFitness != 0 {
		t.Errorf("domains mode must not credit the base tree with the cross-island best; godev EvolvedCount = %d (want 0), StructuralFitness = %v (want 0)", base.EvolvedCount, base.StructuralFitness)
	}

	// Default (anonymous-islands) mode is unchanged: the base tree seeded every
	// island, so it alone keeps the cross-island-best write-back. A fresh home
	// keeps this run's archive independent of the domains run above.
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	defaultKG := knowledge.NewKnowledgeGraph()
	defaultKG.Register(&knowledge.TreeMeta{ID: "godev", Name: "Go Dev", Category: "domain"})
	defaultServer := engine.NewServer("test-default")
	registerMCPTools(defaultServer, &mcpDeps{kg: defaultKG})
	defaultRes, ok := defaultServer.Invoke("bt_evolve_island", json.RawMessage(`{"tree":"godev","islands":2,"population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_island) reported the tool as unregistered on the default-mode run")
	}
	if defaultRes == nil || len(defaultRes.Content) == 0 {
		t.Fatal("bt_evolve_island returned no content on the default-mode run")
	}
	var defaultOut map[string]interface{}
	if err := json.Unmarshal([]byte(defaultRes.Content[0].Text), &defaultOut); err != nil {
		t.Fatalf("bt_evolve_island default-mode result is not valid JSON: %v (text=%q)", err, defaultRes.Content[0].Text)
	}
	if _, isErr := defaultOut["error"]; isErr {
		t.Fatalf("bt_evolve_island unexpectedly returned an error on the default-mode run: %v", defaultOut)
	}
	defaultBase := defaultKG.Trees["godev"]
	if defaultBase == nil {
		t.Fatal("godev vanished from the knowledge graph after the default-mode run")
	}
	if defaultBase.EvolvedCount != 1 || defaultBase.StructuralFitness <= 0 {
		t.Errorf("default (anonymous-islands) mode must keep crediting the base tree with the cross-island best; godev EvolvedCount = %d (want 1), StructuralFitness = %v (want > 0)", defaultBase.EvolvedCount, defaultBase.StructuralFitness)
	}
}

// TestBTEvolveIslandAccumulatesDurableArchive pins milestone 3/5 of the
// durable quality-diversity program: bt_evolve_island must persist its island
// model to island_archive.json under BT_AGENT_HOME after every run and
// warm-start from that archive on the next invocation, so illuminated behavior
// accumulates across runs instead of restarting from scratch each call. The
// result JSON must report the warm start honestly — "warm_started": false on a
// cold home, true once an archive exists — and the archive's generation
// counter must grow monotonically across invocations (proof the second run
// resumed from the first run's state rather than overwriting it).
func TestBTEvolveIslandAccumulatesDurableArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	// Identical args for both runs so warm-start island keys line up. Params
	// stay tiny so the deterministic structural evolution remains -short-safe.
	args := json.RawMessage(`{"tree":"godev","islands":2,"population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_island", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_island) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_island returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_island %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_island unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}
	readArchive := func(label string) (generation float64, islands map[string]json.RawMessage) {
		t.Helper()
		// Locate the durable archive by pattern rather than exact filename so
		// this accumulation pin holds across the per-base-tree archive scoping
		// (milestone 1/5): with a single base tree there must be exactly one
		// island_archive*.json file under BT_AGENT_HOME however the path embeds
		// the tree ID. The .json suffix keeps the flock sidecar (.json.lock)
		// out of the count.
		matches, err := filepath.Glob(filepath.Join(home, "island_archive*.json"))
		if err != nil {
			t.Fatalf("glob island archives after the %s run: %v", label, err)
		}
		if len(matches) != 1 {
			t.Fatalf("bt_evolve_island single-tree runs must persist exactly one durable island archive under BT_AGENT_HOME after the %s run; got %v", label, matches)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("bt_evolve_island must persist a durable island archive at %s after the %s run: %v", matches[0], label, err)
		}
		var snap struct {
			Generation float64                    `json:"generation"`
			Islands    map[string]json.RawMessage `json:"islands"`
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatalf("island archive after the %s run is not valid JSON: %v", label, err)
		}
		return snap.Generation, snap.Islands
	}

	first := invoke("first")
	if got, isBool := first["warm_started"].(bool); !isBool || got {
		t.Errorf(`first run on a cold home must report "warm_started": false; got %v`, first["warm_started"])
	}
	gen1, islands1 := readArchive("first")
	if gen1 < 2 {
		t.Errorf("archive generation after the first 2-generation run = %v, want >= 2", gen1)
	}
	if len(islands1) != 2 {
		t.Errorf("archive after the first run holds %d islands, want 2", len(islands1))
	}

	second := invoke("second")
	if got, isBool := second["warm_started"].(bool); !isBool || !got {
		t.Errorf(`second run must warm-start from the durable archive and report "warm_started": true; got %v`, second["warm_started"])
	}
	gen2, islands2 := readArchive("second")
	if gen2 <= gen1 {
		t.Errorf("archive generation must accumulate across runs; got %v after the second run, want > %v", gen2, gen1)
	}
	if len(islands2) != 2 {
		t.Errorf("archive after the second run holds %d islands, want 2", len(islands2))
	}
}

// TestBTEvolveIslandArchiveIsScopedPerBaseTree pins milestone 1/5 of the
// production-safe island archive program: the durable island archive must be
// scoped per base tree rather than shared through a single global
// island_archive.json. bt_evolve_island runs on two different base trees in
// the same BT_AGENT_HOME must not warm-start-merge each other's genomes: the
// second tree's first run has no archive of its own yet and must report
// "warm_started": false even though the first tree already persisted one.
// Within a single base tree the durable accumulation contract is unchanged —
// a repeat run on either tree warm-starts from that tree's own archive — and
// after both trees have run, two distinct archive files must exist.
func TestBTEvolveIslandArchiveIsScopedPerBaseTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	// Params stay tiny so the deterministic structural evolution remains
	// -short-safe; both trees use identical settings so any warm-start
	// difference can only come from archive scoping.
	invoke := func(label, tree string) bool {
		t.Helper()
		args := json.RawMessage(fmt.Sprintf(
			`{"tree":%q,"islands":2,"population":4,"generations":2,"migration_interval":1,"migration_rate":0.5}`, tree))
		res, ok := server.Invoke("bt_evolve_island", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_island) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_island returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_island %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_island unexpectedly returned an error on the %s run: %v", label, out)
		}
		warm, isBool := out["warm_started"].(bool)
		if !isBool {
			t.Fatalf(`bt_evolve_island %s-run result must report a boolean "warm_started"; got %v`, label, out["warm_started"])
		}
		return warm
	}

	// Cold home: the first base tree cold-starts its own archive.
	if invoke("godev first", "godev") {
		t.Errorf(`first run of base tree "godev" on a cold home must report "warm_started": false`)
	}

	// A different base tree in the same home must ALSO cold-start: it has no
	// archive of its own. Warm-starting here means the run loaded — and will
	// merge and re-persist — godev's genomes through a shared global archive,
	// which is exactly the cross-tree pollution per-tree scoping eliminates.
	if invoke("domain:code_review first", "domain:code_review") {
		t.Errorf(`first run of base tree "domain:code_review" must not warm-start from another tree's archive; want "warm_started": false`)
	}

	// Per-tree scoping must preserve within-tree durable accumulation: repeat
	// runs on each base tree warm-start from that tree's own archive.
	if !invoke("godev second", "godev") {
		t.Errorf(`second run of base tree "godev" must warm-start from its own archive; want "warm_started": true`)
	}
	if !invoke("domain:code_review second", "domain:code_review") {
		t.Errorf(`second run of base tree "domain:code_review" must warm-start from its own archive; want "warm_started": true`)
	}

	// The two base trees must persist distinct durable archives under
	// BT_AGENT_HOME — a single shared file cannot isolate them. The .json
	// suffix keeps flock sidecars (.json.lock) out of the count.
	matches, err := filepath.Glob(filepath.Join(home, "island_archive*.json"))
	if err != nil {
		t.Fatalf("glob island archives: %v", err)
	}
	if len(matches) < 2 {
		t.Errorf("two base trees must persist two distinct island archives under BT_AGENT_HOME; got %v", matches)
	}
}

// TestBTEvolveIslandCapsBoundDurableArchiveAcrossCalls pins milestone 3/4 of
// the "bound the durable island-model archive against runaway growth" program:
// bt_evolve_island must accept "population_cap" and "island_cap" request
// parameters and set them on the evolution.IslandModel (Cap and IslandCap)
// before im.Load, so the caps evolution.IslandModel already enforces
// (internal/evolution/island_model_test.go) actually take effect in
// production. Without wiring, every repeated call against the same base tree
// merges a freshly seeded population into whatever the archive already
// holds, so both the per-island individual count and the distinct
// island-key count grow without bound across calls.
func TestBTEvolveIslandCapsBoundDurableArchiveAcrossCalls(t *testing.T) {
	readIslands := func(t *testing.T, home string) map[string]json.RawMessage {
		t.Helper()
		matches, err := filepath.Glob(filepath.Join(home, "island_archive*.json"))
		if err != nil {
			t.Fatalf("glob island archives: %v", err)
		}
		if len(matches) != 1 {
			t.Fatalf("expected exactly one durable island archive under %s; got %v", home, matches)
		}
		data, err := os.ReadFile(matches[0])
		if err != nil {
			t.Fatalf("read island archive %s: %v", matches[0], err)
		}
		var snap struct {
			Islands map[string]json.RawMessage `json:"islands"`
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatalf("island archive %s is not valid JSON: %v", matches[0], err)
		}
		return snap.Islands
	}
	individualCount := func(t *testing.T, raw json.RawMessage) int {
		t.Helper()
		var pop struct {
			Individuals []json.RawMessage `json:"individuals"`
		}
		if err := json.Unmarshal(raw, &pop); err != nil {
			t.Fatalf("island population is not valid JSON: %v", err)
		}
		return len(pop.Individuals)
	}
	invoke := func(t *testing.T, server *engine.Server, label, args string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_island", json.RawMessage(args))
		if !ok {
			t.Fatalf("Invoke(bt_evolve_island) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_island returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_island %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_island unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}

	t.Run("population_cap bounds per-island individual count", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("BT_AGENT_HOME", home)

		// Pre-seed the durable archive for base tree "godev" with a
		// deliberately oversized "code_review" island (distinct Genome per
		// individual, so none collide-dedup away in the merge) — a stand-in
		// for what several prior uncapped calls would have accumulated.
		// hashTree only hashes the root node's Name/Type/child-count, so
		// letting real repeated invocations organically grow the archive is
		// unreliable (most mutations land on non-root nodes and collide back
		// to the same genome); seeding directly isolates the cap check from
		// that unrelated coarseness.
		const seedSize = 30
		seed := evolution.NewIslandModel(1, 0.5)
		seedPop := &evolution.Population{}
		for i := 0; i < seedSize; i++ {
			seedPop.Individuals = append(seedPop.Individuals, evolution.Individual{
				Tree:    &evolution.SerializableNode{Type: "Action", Name: fmt.Sprintf("seed-%d", i)},
				Genome:  fmt.Sprintf("seed-genome-%d", i),
				Fitness: float64(i),
			})
		}
		seedPop.BestFitness = float64(seedSize - 1)
		seed.AddIsland("code_review", seedPop)
		if err := seed.Save(islandArchivePath("godev")); err != nil {
			t.Fatalf("seed island archive: %v", err)
		}

		server := engine.NewServer("test")
		registerMCPTools(server, &mcpDeps{})

		// Same single domain every call: without population_cap wired, each
		// call's freshly seeded 4-individual population merges into whatever
		// the archive already accumulated (starting from the oversized
		// seed), growing without bound.
		const populationCap = 6
		args := fmt.Sprintf(`{"tree":"godev","domains":"code_review","population":4,"generations":1,"migration_interval":1,"migration_rate":0.5,"population_cap":%d}`, populationCap)
		for i := 0; i < 3; i++ {
			invoke(t, server, fmt.Sprintf("call %d", i+1), args)

			islands := readIslands(t, home)
			raw, present := islands["code_review"]
			if !present {
				t.Fatalf("archive after call %d is missing the 'code_review' island; got keys %v", i+1, islands)
			}
			if got := individualCount(t, raw); got > populationCap {
				t.Errorf("after call %d on the same tree, the 'code_review' island holds %d individuals, want <= population_cap=%d — population_cap must be threaded onto IslandModel.Cap before im.Load so repeated merges stay bounded", i+1, got, populationCap)
			}
		}
	})

	t.Run("island_cap bounds distinct island-key count", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("BT_AGENT_HOME", home)
		server := engine.NewServer("test")
		registerMCPTools(server, &mcpDeps{})

		// Each call introduces exactly one new domain never seeded before, so
		// every previously archived island becomes an "adopted" (non-reseeded)
		// candidate island_cap must evict down to the cap.
		const islandCap = 2
		newDomains := []string{"code_review", "devops_ci", "agent_monitor", "refactoring", "security_audit"}
		for i, domain := range newDomains {
			args := fmt.Sprintf(`{"tree":"godev","domains":%q,"population":4,"generations":1,"migration_interval":1,"migration_rate":0.5,"island_cap":%d}`, domain, islandCap)
			invoke(t, server, fmt.Sprintf("call %d (%s)", i+1, domain), args)
		}

		islands := readIslands(t, home)
		if got := len(islands); got > islandCap {
			t.Errorf("after 5 repeated calls on the same tree, each introducing a new domain, the archive holds %d distinct islands, want <= island_cap=%d — island_cap must be threaded onto IslandModel.IslandCap before im.Load so previously archived, non-reseeded islands get evicted", got, islandCap)
		}
	})
}

// TestBTEvolveIslandSurfacesEvictionCounters pins milestone 4/4 of the
// durable island-archive-bound program: bt_evolve_island's JSON result must
// report cumulative "evicted_individuals" and "evicted_islands" counters
// (evolution.IslandStats.EvictedIndividuals/EvictedIslands), mirroring the
// existing "migrations" key, so eviction activity triggered by
// population_cap/island_cap is observable without inspecting the archive
// file directly.
func TestBTEvolveIslandSurfacesEvictionCounters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	// Pre-seed an oversized "code_review" island (distinct Genome per
	// individual so none collide-dedup away in the merge) so the very first
	// population_cap-bounded call must evict individuals.
	const seedSize = 10
	seed := evolution.NewIslandModel(1, 0.5)
	seedPop := &evolution.Population{}
	for i := 0; i < seedSize; i++ {
		seedPop.Individuals = append(seedPop.Individuals, evolution.Individual{
			Tree:    &evolution.SerializableNode{Type: "Action", Name: fmt.Sprintf("seed-%d", i)},
			Genome:  fmt.Sprintf("seed-genome-%d", i),
			Fitness: float64(i),
		})
	}
	seedPop.BestFitness = float64(seedSize - 1)
	seed.AddIsland("code_review", seedPop)
	if err := seed.Save(islandArchivePath("godev")); err != nil {
		t.Fatalf("seed island archive: %v", err)
	}

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	const populationCap = 4
	args := fmt.Sprintf(`{"tree":"godev","domains":"code_review","population":4,"generations":1,"migration_interval":1,"migration_rate":0.5,"population_cap":%d}`, populationCap)
	res, ok := server.Invoke("bt_evolve_island", json.RawMessage(args))
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
		t.Fatalf("bt_evolve_island unexpectedly returned an error: %v", out)
	}
	for _, key := range []string{"evicted_individuals", "evicted_islands"} {
		if _, present := out[key]; !present {
			t.Errorf("bt_evolve_island result missing %q key; got keys %v", key, out)
		}
	}
	evicted, isNum := out["evicted_individuals"].(float64)
	if !isNum || evicted <= 0 {
		t.Errorf("bt_evolve_island 'evicted_individuals' = %v, want > 0 after a population_cap=%d call against an oversized %d-individual seeded island", out["evicted_individuals"], populationCap, seedSize)
	}
}

// TestBTEvolveIslandAdoptsLegacyGlobalArchiveOnce pins the one-time legacy
// island-archive migration: per-tree archive scoping (33f8c13) silently
// orphaned any pre-scoping GLOBAL island_archive.json — accumulated state
// cold-started away and the stale file lingered under BT_AGENT_HOME forever.
// When bt_evolve_island finds no per-tree archive but the legacy global file
// exists, it must merge it via im.Load BEFORE evolving, then rename it aside
// (.migrated) so it is consumed exactly once and can never re-pollute another
// tree's archive. A later run (per-tree archive now present) must not adopt,
// even if a global file reappears.
func TestBTEvolveIslandAdoptsLegacyGlobalArchiveOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	writeLegacy := func() string {
		t.Helper()
		legacy := evolution.NewIslandModel(3, 0.25)
		legacy.AddIsland("legacy_island", evolution.NewPopulation(2, evolution.DefaultTree()))
		legacy.Generation = 9
		path := filepath.Join(home, "island_archive.json")
		if err := legacy.Save(path); err != nil {
			t.Fatalf("write legacy archive: %v", err)
		}
		return path
	}
	legacyPath := writeLegacy()

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})
	args := json.RawMessage(`{"tree":"godev","islands":2,"population":4,"generations":1,"migration_interval":1,"migration_rate":0.5}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_island", args)
		if !ok || res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_island returned nothing on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_island %s result is not valid JSON: %v", label, err)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_island errored on the %s run: %v", label, out)
		}
		return out
	}

	first := invoke("first")
	if first["legacy_archive_adopted"] != true {
		t.Fatalf(`first run with a legacy global archive must report "legacy_archive_adopted": true; got %v`, first["legacy_archive_adopted"])
	}
	// Consumed exactly once: the original is renamed aside.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy archive must be renamed away after adoption; stat err = %v", err)
	}
	if _, err := os.Stat(legacyPath + ".migrated"); err != nil {
		t.Fatalf("adopted legacy archive must be kept aside as .migrated: %v", err)
	}
	// The adopted state is durable in the PER-TREE archive: its generation
	// counter carries the legacy high-water mark forward and the legacy
	// island's subpopulation survives.
	perTree, err := os.ReadFile(filepath.Join(home, "island_archive-godev.json"))
	if err != nil {
		t.Fatalf("per-tree archive must exist after the run: %v", err)
	}
	var snap struct {
		Generation float64                    `json:"generation"`
		Islands    map[string]json.RawMessage `json:"islands"`
	}
	if err := json.Unmarshal(perTree, &snap); err != nil {
		t.Fatalf("per-tree archive is not valid JSON: %v", err)
	}
	if snap.Generation < 9 {
		t.Errorf("per-tree archive generation = %v, want >= the legacy high-water mark 9", snap.Generation)
	}
	if _, adopted := snap.Islands["legacy_island"]; !adopted {
		t.Errorf("legacy island subpopulation missing from the per-tree archive; islands = %d", len(snap.Islands))
	}

	// Second run: no legacy file left → no adoption, normal warm start.
	second := invoke("second")
	if second["legacy_archive_adopted"] == true {
		t.Fatal("adoption must be one-time; the second run must not re-adopt")
	}
	if second["warm_started"] != true {
		t.Fatalf("second run must warm-start from the per-tree archive; got %v", second["warm_started"])
	}

	// A REAPPEARING global file must be ignored once this tree has its own
	// archive — adoption only fills a missing per-tree archive.
	writeLegacy()
	third := invoke("third")
	if third["legacy_archive_adopted"] == true {
		t.Fatal("a tree with an existing per-tree archive must not adopt a reappearing global file")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("un-adopted global file must be left untouched: %v", err)
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

	// bt_evolve_multiobjective is the last production Evolve variant with zero
	// self-healing observability: unlike bt_evolve_genetic, bt_evolve_bottlenecks,
	// and bt_evolve_selection_pressure (TestEvolveToolsSurfacePopulationHealthSnapshot),
	// its response never surfaces Population.HealthSnapshot(). The response must
	// expose a "health" object carrying the same three fields those sibling tools
	// report via evolveHealthProjection: "crisis_reasons" (a JSON array, present
	// even when empty), "resurrections" (a non-negative count), and
	// "last_mutation_rate" (the positive rate the run actually applied).
	health, healthPresent := out["health"]
	if !healthPresent {
		t.Fatal("bt_evolve_multiobjective response must surface Population.HealthSnapshot() under a 'health' object, matching the sibling evolve tools; it is absent")
	}
	healthObj, isObj := health.(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_multiobjective 'health' must be a JSON object projecting Population.HealthSnapshot(); got %T (%v)", health, health)
	}
	if reasons, hasReasons := healthObj["crisis_reasons"]; !hasReasons {
		t.Errorf("bt_evolve_multiobjective health object must report a 'crisis_reasons' key (an empty array when the run stayed healthy); got %v", healthObj)
	} else if _, isList := reasons.([]interface{}); !isList {
		t.Errorf("bt_evolve_multiobjective health 'crisis_reasons' must be a JSON array; got %T (%v)", reasons, reasons)
	}
	if res, isNum := healthObj["resurrections"].(float64); !isNum || res < 0 {
		t.Errorf("bt_evolve_multiobjective health object must report a non-negative 'resurrections' count; got %v", healthObj["resurrections"])
	}
	if rate, isNum := healthObj["last_mutation_rate"].(float64); !isNum || rate <= 0 {
		t.Errorf("bt_evolve_multiobjective health 'last_mutation_rate' must be the positive rate the run actually applied; got %v", healthObj["last_mutation_rate"])
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

// TestBTEvolveMultiObjectiveAccumulatesDurableArchive pins the missing
// durable-archive wiring for NSGA-II evolution (Q2 Evolvability milestone
// 4/5): evolution.NSGAIIPopulation already implements Save/Load (milestone
// 3/5, internal/evolution/multi_objective.go), wrapping its final
// non-dominated front (Fronts[0]) in a ParetoFront and delegating to its
// merge/cap-eviction persistence, but bt_evolve_multiobjective's handler
// (cmd/bt-agent/tools.go) never calls them, so NSGA-II's Pareto-optimal front
// resets on every invocation instead of accumulating across runs like its
// bt_evolve_pareto sibling (TestBTEvolveParetoAccumulatesDurableArchive).
// Mirroring that test and bt_evolve_qd, the result JSON must report the warm
// start honestly — "warm_started": false on a cold home, true once an
// archive exists — sharing the same durable-archive result fields
// (warm_started/archive_load_error/archive_save_error) the other four
// algorithms already report, and a single archive file must exist under
// BT_AGENT_HOME after the first run.
//
// This is orthogonal to the benchmark-suite gate
// (TestBTEvolveMultiObjectiveBenchmarkGateRejectsRegressedWinner): NSGA-II
// mutation uses unseeded math/rand, so the evolved winner occasionally
// regresses against the base tree on the real suite and the gate correctly
// skips the save — which would make this test flaky if left alone. Pin
// benchmarkRunSuiteFn to a constant non-regressing result so archive
// accumulation is exercised deterministically regardless of gate outcome.
func TestBTEvolveMultiObjectiveAccumulatesDurableArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	origFn := benchmarkRunSuiteFn
	benchmarkRunSuiteFn = func(tree *evolution.SerializableNode, suite benchmark.Suite, mock llm.LLM) *benchmark.RunMetrics {
		return &benchmark.RunMetrics{TotalTasks: 1, Successes: 1, SuccessRate: 1.0}
	}
	defer func() { benchmarkRunSuiteFn = origFn }()

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_multiobjective", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_multiobjective) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_multiobjective returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_multiobjective %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_multiobjective unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}

	first := invoke("first")
	if got, isBool := first["warm_started"].(bool); !isBool || got {
		t.Errorf(`first run on a cold home must report "warm_started": false; got %v`, first["warm_started"])
	}

	matches, err := filepath.Glob(filepath.Join(home, "*nsga*archive*.json"))
	if err != nil {
		t.Fatalf("glob NSGA-II archives after the first run: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("bt_evolve_multiobjective single-tree runs must persist exactly one durable NSGA-II archive under BT_AGENT_HOME after the first run; got %v", matches)
	}

	second := invoke("second")
	if got, isBool := second["warm_started"].(bool); !isBool || !got {
		t.Errorf(`second run must warm-start from the durable archive and report "warm_started": true; got %v`, second["warm_started"])
	}
}

// TestBTEvolveMultiObjectiveBenchmarkGateRejectsRegressedWinner pins the
// missing benchmark-suite gate on bt_evolve_multiobjective's durable-archive
// save (Q2 Evolvability, "gate the standalone evolution-algorithm tools'
// durable-archive winners through the benchmark suite, not structural fitness
// alone"). The handler picks the NSGA-II front's lead individual using only
// evolution.StructuralMultiFitness — success_rate/node_efficiency/stability
// computed from structural heuristics, never an actual tree execution — and
// unconditionally persists it via nsga.Save(archivePath). A mutation can look
// structurally elite while actually performing worse than the untouched base
// tree against the tree's real internal/benchmark suite (deterministic,
// sandboxed, mock-LLM RunSuite — no real LLM calls, so this stays -short-safe).
// benchmarkGateEvolvedWinner must run both the base tree and the winner
// through that suite and reject the save on regression, surfacing
// "benchmark_gate_rejected": true instead of silently archiving a worse tree.
//
// benchmarkRunSuiteFn is a package-level indirection over benchmark.RunSuite
// (mirroring the DelegateToA2AFn/AuctionDelegateFn test-seam pattern already
// used in this package) so this test can force a regression deterministically
// without depending on NSGA-II's unseeded math/rand mutation, which makes the
// evolved winner's actual structure unpredictable across runs.
func TestBTEvolveMultiObjectiveBenchmarkGateRejectsRegressedWinner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	origFn := benchmarkRunSuiteFn
	calls := 0
	benchmarkRunSuiteFn = func(tree *evolution.SerializableNode, suite benchmark.Suite, mock llm.LLM) *benchmark.RunMetrics {
		calls++
		if calls == 1 {
			// First call gates the base tree: a perfect score.
			return &benchmark.RunMetrics{TotalTasks: 1, Successes: 1, SuccessRate: 1.0}
		}
		// Every subsequent call gates the evolved winner: total regression.
		return &benchmark.RunMetrics{TotalTasks: 1, Successes: 0, SuccessRate: 0.0}
	}
	defer func() { benchmarkRunSuiteFn = origFn }()

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

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
		t.Fatalf("bt_evolve_multiobjective unexpectedly returned an error: %v", out)
	}

	if rejected, isBool := out["benchmark_gate_rejected"].(bool); !isBool || !rejected {
		t.Fatalf(`bt_evolve_multiobjective must report "benchmark_gate_rejected": true when the evolved winner regresses against the base tree on the real benchmark suite; got %v`, out["benchmark_gate_rejected"])
	}

	matches, err := filepath.Glob(filepath.Join(home, "*nsga*archive*.json"))
	if err != nil {
		t.Fatalf("glob NSGA-II archives: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("bt_evolve_multiobjective must skip persisting the durable NSGA-II archive when the benchmark gate rejects the winner; found %v", matches)
	}
}

// TestBTEvolveParetoRegisteredAndReturnsParetoMetrics pins the bt_evolve_pareto
// MCP tool: it must be registered by registerMCPTools and give
// evolution.ParetoPopulation.EvolvePareto (internal/evolution/pareto.go) a
// production entry point, mirroring the existing deterministic
// bt_evolve_qd/bt_evolve_island/bt_evolve_multiobjective tools in this file.
// Unlike bt_evolve_multiobjective (which drives NSGAIIPopulation.Evolve —
// full non-dominated sorting with crowding distance — and reports
// "dimension_bests"/"pareto_front_size"), this tool drives
// ParetoPopulation.EvolvePareto — front-elitism selection via SelectPareto —
// and must report ParetoFront.Stats() verbatim: "front_size",
// "diversity_score", and "best_per_dim". An unknown tree id must yield the
// shared {"error":"unknown tree"} shape rather than a partial/panicking
// result. Because EvolvePareto already wraps each generation in the same
// selfHealGeneration envelope Evolve and EvolveWithExperience use (see the
// doc comment on EvolvePareto), the response must also surface
// Population.HealthSnapshot() under a "health" object from the start,
// matching the sibling evolve tools.
func TestBTEvolveParetoRegisteredAndReturnsParetoMetrics(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_evolve_pareto") {
		t.Fatal("bt_evolve_pareto tool must be registered by registerMCPTools")
	}

	// Happy path: a real resolvable base tree with a small population/generations
	// (kept tiny so the deterministic Pareto evolution stays -short-safe).
	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	res, ok := server.Invoke("bt_evolve_pareto", args)
	if !ok {
		t.Fatal("Invoke(bt_evolve_pareto) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_pareto returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_pareto result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_pareto unexpectedly returned an error for a resolvable tree: %v", out)
	}

	// The best tree must have a non-zero node count.
	nodeCount, ok := out["node_count"].(float64)
	if !ok || nodeCount <= 0 {
		t.Errorf("bt_evolve_pareto must report a non-zero 'node_count'; got %v", out["node_count"])
	}

	// ParetoFront.Stats() must be reported verbatim: front_size (>=1),
	// diversity_score (present), and best_per_dim (a non-empty object).
	frontSize, ok := out["front_size"].(float64)
	if !ok || frontSize < 1 {
		t.Errorf("bt_evolve_pareto must report a 'front_size' >= 1; got %v", out["front_size"])
	}
	if _, present := out["diversity_score"]; !present {
		t.Errorf("bt_evolve_pareto result missing 'diversity_score' key; got keys %v", out)
	}
	bestPerDim, ok := out["best_per_dim"].(map[string]interface{})
	if !ok || len(bestPerDim) == 0 {
		t.Errorf("bt_evolve_pareto must report a non-empty 'best_per_dim' object; got %v", out["best_per_dim"])
	}

	// EvolvePareto already runs inside selfHealGeneration, so health must be
	// surfaced from the start (unlike bt_evolve_multiobjective, which needed a
	// dedicated milestone to add it).
	health, healthPresent := out["health"]
	if !healthPresent {
		t.Fatal("bt_evolve_pareto response must surface Population.HealthSnapshot() under a 'health' object, matching the sibling evolve tools; it is absent")
	}
	healthObj, isObj := health.(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_pareto 'health' must be a JSON object projecting Population.HealthSnapshot(); got %T (%v)", health, health)
	}
	if reasons, hasReasons := healthObj["crisis_reasons"]; !hasReasons {
		t.Errorf("bt_evolve_pareto health object must report a 'crisis_reasons' key (an empty array when the run stayed healthy); got %v", healthObj)
	} else if _, isList := reasons.([]interface{}); !isList {
		t.Errorf("bt_evolve_pareto health 'crisis_reasons' must be a JSON array; got %T (%v)", reasons, reasons)
	}
	if res, isNum := healthObj["resurrections"].(float64); !isNum || res < 0 {
		t.Errorf("bt_evolve_pareto health object must report a non-negative 'resurrections' count; got %v", healthObj["resurrections"])
	}
	if rate, isNum := healthObj["last_mutation_rate"].(float64); !isNum || rate <= 0 {
		t.Errorf("bt_evolve_pareto health 'last_mutation_rate' must be the positive rate the run actually applied; got %v", healthObj["last_mutation_rate"])
	}

	// Unknown tree: a known prefix with an unresolvable suffix resolves to nil,
	// which must surface the shared unknown-tree error shape.
	unknown, ok := server.Invoke("bt_evolve_pareto", json.RawMessage(`{"tree":"domain:__no_such_tree__"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_pareto) reported the tool as unregistered on the error path")
	}
	if unknown == nil || len(unknown.Content) == 0 {
		t.Fatal("bt_evolve_pareto returned no content for an unknown tree")
	}
	var errOut2 map[string]interface{}
	if err := json.Unmarshal([]byte(unknown.Content[0].Text), &errOut2); err != nil {
		t.Fatalf("bt_evolve_pareto unknown-tree result is not valid JSON: %v", err)
	}
	if errOut2["error"] != "unknown tree" {
		t.Fatalf("bt_evolve_pareto unknown tree should return {\"error\":\"unknown tree\"}; got %v", errOut2)
	}
}

// TestBTEvolveParetoAccumulatesDurableArchive pins the missing durable-archive
// wiring for Pareto front-elitism evolution (Q2 Evolvability milestone 2/5):
// bt_evolve_pareto currently builds a fresh evolution.ParetoFront on every
// call and discards it, so the Pareto-optimal front never survives across
// invocations even though ParetoFront already implements Save/Load/Cap
// (internal/evolution/pareto.go). Mirroring bt_evolve_qd, the front must
// warm-start from a durable per-tree archive and persist back to it after
// every run. The result JSON must report the warm start honestly —
// "warm_started": false on a cold home, true once an archive exists — and a
// single archive file must exist under BT_AGENT_HOME after the first run.
func TestBTEvolveParetoAccumulatesDurableArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	args := json.RawMessage(`{"tree":"godev","population":4,"generations":2}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_pareto", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_pareto) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_pareto returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_pareto %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_pareto unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}

	first := invoke("first")
	if got, isBool := first["warm_started"].(bool); !isBool || got {
		t.Errorf(`first run on a cold home must report "warm_started": false; got %v`, first["warm_started"])
	}

	matches, err := filepath.Glob(filepath.Join(home, "pareto_front_archive*.json"))
	if err != nil {
		t.Fatalf("glob pareto front archives after the first run: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("bt_evolve_pareto single-tree runs must persist exactly one durable Pareto front archive under BT_AGENT_HOME after the first run; got %v", matches)
	}

	second := invoke("second")
	if got, isBool := second["warm_started"].(bool); !isBool || !got {
		t.Errorf(`second run must warm-start from the durable archive and report "warm_started": true; got %v`, second["warm_started"])
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

// TestBTEvolveSelectionPressureRegisteredAndBreedsProvenUnderbredTrees pins the
// bt_evolve_selection_pressure MCP tool (analytics→action loop milestone 2/4,
// Q2 Evolvability): it gives Analytics.SelectionPressure a production consumer.
// The tool must be registered by registerMCPTools, read
// deps.kg.ComputeAnalytics().SelectionPressure (proven trees: Fitness >= 70 and
// RunCount < 5 — the same criteria ComputeAnalytics surfaces today only as
// human-readable SuggestedActions strings), run a deterministic (LLM-free)
// experience-grounded genetic evolution on each proven-but-underbred tree via
// evolution.NewPopulation(...).EvolveWithExperience, and — mirroring
// bt_evolve_bottlenecks — report per-tree before/after fitness as JSON.
//
// Proven-but-well-exercised trees (RunCount >= 5) must be left alone, and a
// pressure entry whose KG id does not resolve to a real behavior tree must be
// surfaced under "skipped" without aborting the rest of the report. Crucially,
// each evolved elite's fitness must be written back through the existing
// evolved-fitness path (recordEvolvedFitness → RecordRun "evolved"), landing in
// TreeMeta.StructuralFitness (never the runtime-success EMA) and bumping
// EvolvedCount — so fitness-driven discovery can later surface the bred winners.
// A nil experience bank (this test's bare mcpDeps) must degrade gracefully, with
// the consulted bank surfaced via "experience_bank_entries".
func TestBTEvolveSelectionPressureRegisteredAndBreedsProvenUnderbredTrees(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	// Proven (Fitness >= 70) and underbred (RunCount < 5), resolves to a real
	// domain behavior tree: must be evolved and get a fitness write-back.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 90, RunCount: 2,
	})
	// A second proven-but-underbred resolvable tree.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:alert_router", Name: "Alert Router", Category: "domain",
		Fitness: 78, RunCount: 4,
	})
	// Proven but WELL exercised (RunCount >= 5): the loop is already applying
	// selection pressure, so this must never appear in the report.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:security_audit", Name: "Security Audit", Category: "domain",
		Fitness: 85, RunCount: 10,
	})
	// Underbred but NOT proven (Fitness < 70): not selection pressure at all.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:data_pipeline", Name: "Data Pipeline", Category: "domain",
		Fitness: 40, RunCount: 1,
	})
	// Proven and underbred but whose id resolves to no behavior tree: must be
	// skipped, not abort the whole invocation.
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:__no_such_tree__", Name: "Ghost", Category: "domain",
		Fitness: 95, RunCount: 1,
	})

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{kg: kg})

	if !server.HasTool("bt_evolve_selection_pressure") {
		t.Fatal("bt_evolve_selection_pressure tool must be registered by registerMCPTools")
	}

	// Happy path: tiny population/generations so the deterministic structural
	// evolution stays -short-safe.
	res, ok := server.Invoke("bt_evolve_selection_pressure", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selection_pressure) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_selection_pressure returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_selection_pressure result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if _, isErr := out["error"]; isErr {
		t.Fatalf("bt_evolve_selection_pressure unexpectedly returned an error for a graph with selection pressure: %v", out)
	}

	// code_review, alert_router and __no_such_tree__ all satisfy Fitness>=70 &&
	// RunCount<5; security_audit (well-bred) and data_pipeline (not proven) do not.
	if n, isNum := out["selection_pressure"].(float64); !isNum || int(n) != 3 {
		t.Errorf("bt_evolve_selection_pressure must report 'selection_pressure' = 3 (proven+underbred trees); got %v", out["selection_pressure"])
	}

	// The per-tree report holds exactly the two resolvable pressure entries, with
	// the KG fitness echoed as before_fitness and a computed after_fitness.
	report, isList := out["report"].([]interface{})
	if !isList {
		t.Fatalf("bt_evolve_selection_pressure must return a 'report' JSON array; got %T (%v)", out["report"], out["report"])
	}
	if len(report) != 2 {
		t.Fatalf("bt_evolve_selection_pressure 'report' must hold exactly the 2 resolvable pressure entries; got %d entries: %v", len(report), report)
	}
	byTree := map[string]map[string]interface{}{}
	for _, e := range report {
		m, isMap := e.(map[string]interface{})
		if !isMap {
			t.Fatalf("bt_evolve_selection_pressure report entries must be JSON objects; got %T (%v)", e, e)
		}
		tree, _ := m["tree"].(string)
		byTree[tree] = m
	}
	if _, evolved := byTree["domain:security_audit"]; evolved {
		t.Errorf("bt_evolve_selection_pressure must not evolve the well-bred tree domain:security_audit; report: %v", report)
	}
	if _, evolved := byTree["domain:data_pipeline"]; evolved {
		t.Errorf("bt_evolve_selection_pressure must not evolve the unproven tree domain:data_pipeline; report: %v", report)
	}

	entry := byTree["domain:code_review"]
	if entry == nil {
		t.Fatalf("bt_evolve_selection_pressure report must include an entry for domain:code_review; got %v", report)
	}
	if before, isNum := entry["before_fitness"].(float64); !isNum || before != 90 {
		t.Errorf("bt_evolve_selection_pressure report entry must echo the KG fitness as 'before_fitness' = 90; got %v", entry["before_fitness"])
	}
	if after, isNum := entry["after_fitness"].(float64); !isNum || after <= 0 {
		t.Errorf("bt_evolve_selection_pressure report entry must carry a positive evolved 'after_fitness'; got %v", entry["after_fitness"])
	}
	if runs, isNum := entry["runs"].(float64); !isNum || int(runs) != 2 {
		t.Errorf("bt_evolve_selection_pressure report entry must echo 'runs' = 2; got %v", entry["runs"])
	}
	if entry["algorithm"] != "genetic" {
		t.Errorf("bt_evolve_selection_pressure must run experience-grounded genetic evolution and tag its entry 'algorithm' = %q; got %v", "genetic", entry["algorithm"])
	}

	// The unresolvable pressure entry is surfaced under 'skipped', not fatal.
	skipped, isList := out["skipped"].([]interface{})
	if !isList || len(skipped) != 1 || skipped[0] != "domain:__no_such_tree__" {
		t.Errorf("bt_evolve_selection_pressure must list the unresolvable pressure entry under 'skipped' = [\"domain:__no_such_tree__\"]; got %v", out["skipped"])
	}

	// Experience-grounded: the consulted bank is reported even when nil (0).
	if _, present := out["experience_bank_entries"]; !present {
		t.Errorf("bt_evolve_selection_pressure result missing 'experience_bank_entries' key; got keys %v", out)
	}

	// Fitness write-back: each evolved elite's fitness must land in
	// StructuralFitness (not the runtime EMA) via the "evolved" path, and bump
	// EvolvedCount without disturbing RunCount.
	cr := kg.Trees["domain:code_review"]
	if cr == nil {
		t.Fatal("domain:code_review vanished from the knowledge graph after evolution")
	}
	if cr.StructuralFitness <= 0 {
		t.Errorf("bt_evolve_selection_pressure must write each elite's fitness back through the evolved path; expected domain:code_review.StructuralFitness > 0, got %.2f", cr.StructuralFitness)
	}
	if cr.EvolvedCount != 1 {
		t.Errorf("bt_evolve_selection_pressure evolved write-back must bump EvolvedCount to 1 for domain:code_review; got %d", cr.EvolvedCount)
	}
	if cr.Fitness != 90 {
		t.Errorf("bt_evolve_selection_pressure must not overwrite the runtime-success EMA (Fitness); expected 90, got %.2f", cr.Fitness)
	}
	if cr.RunCount != 2 {
		t.Errorf("bt_evolve_selection_pressure evolved write-back must not increment RunCount; expected 2, got %d", cr.RunCount)
	}

	// No selection pressure: a graph of proven-but-well-bred trees yields an
	// empty report, not an error.
	noPressure := knowledge.NewKnowledgeGraph()
	noPressure.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 90, RunCount: 20,
	})
	server2 := engine.NewServer("test2")
	registerMCPTools(server2, &mcpDeps{kg: noPressure})
	empty, ok := server2.Invoke("bt_evolve_selection_pressure", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selection_pressure) reported the tool as unregistered on the no-pressure path")
	}
	if empty == nil || len(empty.Content) == 0 {
		t.Fatal("bt_evolve_selection_pressure returned no content for a no-pressure graph")
	}
	var emptyOut map[string]interface{}
	if err := json.Unmarshal([]byte(empty.Content[0].Text), &emptyOut); err != nil {
		t.Fatalf("bt_evolve_selection_pressure no-pressure result is not valid JSON: %v (text=%q)", err, empty.Content[0].Text)
	}
	if _, isErr := emptyOut["error"]; isErr {
		t.Fatalf("bt_evolve_selection_pressure must not error on a graph without selection pressure; got %v", emptyOut)
	}
	if n, isNum := emptyOut["selection_pressure"].(float64); !isNum || int(n) != 0 {
		t.Errorf("bt_evolve_selection_pressure must report 'selection_pressure' = 0 for a no-pressure graph; got %v", emptyOut["selection_pressure"])
	}
	if emptyReport, isList := emptyOut["report"].([]interface{}); !isList || len(emptyReport) != 0 {
		t.Errorf("bt_evolve_selection_pressure must return an empty 'report' array for a no-pressure graph; got %v", emptyOut["report"])
	}

	// Empty graph: no trees at all is a clean, empty result, not an error.
	emptyGraph := knowledge.NewKnowledgeGraph()
	server3 := engine.NewServer("test3")
	registerMCPTools(server3, &mcpDeps{kg: emptyGraph})
	blank, ok := server3.Invoke("bt_evolve_selection_pressure", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selection_pressure) reported the tool as unregistered on the empty-graph path")
	}
	if blank == nil || len(blank.Content) == 0 {
		t.Fatal("bt_evolve_selection_pressure returned no content for an empty graph")
	}
	var blankOut map[string]interface{}
	if err := json.Unmarshal([]byte(blank.Content[0].Text), &blankOut); err != nil {
		t.Fatalf("bt_evolve_selection_pressure empty-graph result is not valid JSON: %v", err)
	}
	if _, isErr := blankOut["error"]; isErr {
		t.Fatalf("bt_evolve_selection_pressure must not error on an empty graph; got %v", blankOut)
	}
	if n, isNum := blankOut["selection_pressure"].(float64); !isNum || int(n) != 0 {
		t.Errorf("bt_evolve_selection_pressure must report 'selection_pressure' = 0 for an empty graph; got %v", blankOut["selection_pressure"])
	}
}

// TestBTEvolveGeneticPersistsEvolvedWinnerTree pins the fix for the
// evolved-winner-discarded gap (Q2 Evolvability, Q1 Correctness):
// bt_evolve_genetic computes best := pop.EvolveWithExperience(...) only to
// read CountNodes(best) for the report, then drops the winner tree entirely —
// nothing about its actual structure survives the call. The tool must instead
// persist the winner through the existing persistGeneratedTree seam under a
// derived "<tree>-evolved" id (mirroring bt_evolve_selectors' "persisted"/
// "file" result keys), report that id, and register it in the knowledge graph
// so fitness-aware discovery and the gardener can find the bred winner on the
// next run instead of only its scalar fitness.
func TestBTEvolveGeneticPersistsEvolvedWinnerTree(t *testing.T) {
	dir := t.TempDir()
	treeStore, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "godev", Name: "Go Developer", Category: "core"})

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{treeStore: treeStore, kg: kg})

	res, ok := server.Invoke("bt_evolve_genetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_genetic) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_genetic returned no content")
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_genetic result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}

	const wantID = "godev-evolved"
	evolvedID, _ := out["evolved_tree_id"].(string)
	if evolvedID != wantID {
		t.Fatalf("bt_evolve_genetic must report the persisted evolved winner's id as 'evolved_tree_id' = %q instead of discarding the winner after computing fitness; got %v (keys %v)", wantID, out["evolved_tree_id"], out)
	}
	if persisted, _ := out["persisted"].(bool); !persisted {
		t.Errorf("bt_evolve_genetic must report persisted=true for the evolved winner when it validates and a tree store is configured; got %v", out["persisted"])
	}
	if file, _ := out["file"].(string); file == "" {
		t.Errorf("bt_evolve_genetic must report the on-disk 'file' path the evolved winner was persisted to; got %v", out["file"])
	}

	loaded, err := treeStore.LoadNamed(wantID)
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", wantID, err)
	}
	if loaded == nil {
		t.Fatalf("bt_evolve_genetic must persist the evolved winner tree under %q so it survives restarts and is resolvable by id; treeStore has nothing there", wantID)
	}

	meta := kg.Trees[wantID]
	if meta == nil {
		t.Fatalf("bt_evolve_genetic must register the evolved winner tree %q in the knowledge graph so discovery can surface it", wantID)
	}
	if meta.StructuralFitness <= 0 {
		t.Errorf("bt_evolve_genetic evolved winner %q must be registered with a positive StructuralFitness; got %v", wantID, meta.StructuralFitness)
	}

	related := kg.DiscoverRelated("godev")
	found := false
	for _, id := range related {
		if id == wantID {
			found = true
		}
	}
	if !found {
		t.Errorf("bt_evolve_genetic must connect the evolved winner %q back to its base tree 'godev' via a KG relationship; DiscoverRelated(godev)=%v", wantID, related)
	}
}

// TestBTEvolveBottlenecksPersistsEvolvedWinnerTree pins the same fix as
// TestBTEvolveGeneticPersistsEvolvedWinnerTree for the genetic-fallback path
// inside bt_evolve_bottlenecks: today pop.EvolveWithExperience(...)'s return
// value is discarded outright (line is a bare statement), leaving only
// pop.BestFitness in the report. domain:alert_router has no tunable
// parameters, so it deterministically routes to the genetic path (mirroring
// TestBTEvolveBottlenecksRegisteredAndReturnsBeforeAfterReport's fixture).
func TestBTEvolveBottlenecksPersistsEvolvedWinnerTree(t *testing.T) {
	dir := t.TempDir()
	treeStore, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:alert_router", Name: "Alert Router", Category: "domain",
		Fitness: 8, RunCount: 3,
	})

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{treeStore: treeStore, kg: kg})

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
	report, isList := out["report"].([]interface{})
	if !isList || len(report) != 1 {
		t.Fatalf("expected exactly one bt_evolve_bottlenecks report entry for the single genetic-path bottleneck; got %d: %v", len(report), out["report"])
	}
	entry, _ := report[0].(map[string]interface{})
	if entry["algorithm"] != "genetic" {
		t.Fatalf("fixture bottleneck must route to the genetic path; got algorithm=%v", entry["algorithm"])
	}

	const wantID = "domain:alert_router-evolved"
	evolvedID, _ := entry["evolved_tree_id"].(string)
	if evolvedID != wantID {
		t.Fatalf("bt_evolve_bottlenecks genetic-path report entry must carry 'evolved_tree_id' = %q instead of discarding the bred winner after computing fitness; got %v (entry %v)", wantID, entry["evolved_tree_id"], entry)
	}
	if persisted, _ := entry["persisted"].(bool); !persisted {
		t.Errorf("bt_evolve_bottlenecks report entry must report persisted=true for the evolved winner; got %v", entry["persisted"])
	}

	loaded, err := treeStore.LoadNamed(wantID)
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", wantID, err)
	}
	if loaded == nil {
		t.Fatalf("bt_evolve_bottlenecks must persist the genetic-path evolved winner tree under %q instead of discarding it after computing fitness", wantID)
	}

	if meta := kg.Trees[wantID]; meta == nil {
		t.Fatalf("bt_evolve_bottlenecks must register the evolved winner tree %q in the knowledge graph", wantID)
	}
}

// TestBTEvolveSelectionPressurePersistsEvolvedWinnerTree pins the same fix as
// TestBTEvolveGeneticPersistsEvolvedWinnerTree for bt_evolve_selection_pressure:
// today only pop.BestFitness survives via recordEvolvedFitness — the bred
// elite tree that earned that fitness is discarded. Uses the same fixture as
// TestBTEvolveSelectionPressureRegisteredAndBreedsProvenUnderbredTrees
// (domain:code_review, proven+underbred).
func TestBTEvolveSelectionPressurePersistsEvolvedWinnerTree(t *testing.T) {
	dir := t.TempDir()
	treeStore, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 90, RunCount: 2,
	})

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{treeStore: treeStore, kg: kg})

	res, ok := server.Invoke("bt_evolve_selection_pressure", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_selection_pressure) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_evolve_selection_pressure returned no content")
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_evolve_selection_pressure result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	report, isList := out["report"].([]interface{})
	if !isList || len(report) != 1 {
		t.Fatalf("expected exactly one bt_evolve_selection_pressure report entry for the single proven+underbred tree; got %d: %v", len(report), out["report"])
	}
	entry, _ := report[0].(map[string]interface{})

	const wantID = "domain:code_review-evolved"
	evolvedID, _ := entry["evolved_tree_id"].(string)
	if evolvedID != wantID {
		t.Fatalf("bt_evolve_selection_pressure report entry must carry 'evolved_tree_id' = %q instead of discarding the bred elite after computing fitness; got %v (entry %v)", wantID, entry["evolved_tree_id"], entry)
	}
	if persisted, _ := entry["persisted"].(bool); !persisted {
		t.Errorf("bt_evolve_selection_pressure report entry must report persisted=true for the evolved winner; got %v", entry["persisted"])
	}

	loaded, err := treeStore.LoadNamed(wantID)
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", wantID, err)
	}
	if loaded == nil {
		t.Fatalf("bt_evolve_selection_pressure must persist the bred elite tree under %q instead of discarding it after computing fitness", wantID)
	}

	if meta := kg.Trees[wantID]; meta == nil {
		t.Fatalf("bt_evolve_selection_pressure must register the evolved winner tree %q in the knowledge graph", wantID)
	}
}

// TestPersistEvolvedWinner_SkipsOverwriteWhenFitnessDoesNotImprove pins the
// gate this goal adds: persistEvolvedWinner (and the RegisterEvolved
// bookkeeping it drives) must only overwrite the persisted "<base>-evolved"
// tree file and its knowledge-graph metadata when the new winner's fitness
// actually beats what's already stored for that id. Today the file write via
// persistGeneratedTree is unconditional, so a later, weaker genetic-evolution
// pass silently clobbers a stronger winner already on disk — and
// RegisterEvolved's NodeCount write-back follows the clobbered structure even
// though its StructuralFitness field is already (correctly) monotone.
func TestPersistEvolvedWinner_SkipsOverwriteWhenFitnessDoesNotImprove(t *testing.T) {
	dir := t.TempDir()
	treeStore, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	kg := knowledge.NewKnowledgeGraph()
	deps := &mcpDeps{treeStore: treeStore, kg: kg}

	strong := &evolution.SerializableNode{Type: "Action", Name: "AddCitations"}
	weak := &evolution.SerializableNode{
		Type: "Sequence", Name: "weak-root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "AddMonitoring"}},
	}
	const wantID = "godev-evolved"

	first := map[string]interface{}{}
	persistEvolvedWinner(deps, "godev", strong, 90, first)
	if persisted, _ := first["persisted"].(bool); !persisted {
		t.Fatalf("first persistEvolvedWinner call must persist the initial winner; result=%v", first)
	}

	second := map[string]interface{}{}
	persistEvolvedWinner(deps, "godev", weak, 50, second)

	if persisted, _ := second["persisted"].(bool); persisted {
		t.Errorf("persistEvolvedWinner must not report persisted=true when the new winner's fitness (50) does not beat the stored fitness (90); result=%v", second)
	}

	loaded, err := treeStore.LoadNamed(wantID)
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", wantID, err)
	}
	if loaded == nil || loaded.Name != "AddCitations" {
		t.Errorf("a weaker later winner must not overwrite the stronger tree already persisted at %q; got %+v", wantID, loaded)
	}

	meta := kg.Trees[wantID]
	if meta == nil {
		t.Fatalf("expected evolved tree %q to be registered in the knowledge graph", wantID)
	}
	if meta.NodeCount != evolution.CountNodes(strong) {
		t.Errorf("RegisterEvolved must not overwrite NodeCount with a non-improving winner's structure; got %d, want %d (the stronger winner's node count)", meta.NodeCount, evolution.CountNodes(strong))
	}
	if meta.StructuralFitness != 90 {
		t.Errorf("StructuralFitness must stay at the stronger winner's 90 after a weaker winner is rejected; got %v", meta.StructuralFitness)
	}
}

// TestPersistEvolvedWinner_AtomicWithFailedDiskWrite pins that persistEvolvedWinner's
// knowledge-graph bookkeeping (RegisterEvolved) and its disk write
// (persistGeneratedTree) succeed or fail together. Today RegisterEvolved
// commits the higher fitness, bumped EvolvedCount, and the new NodeCount to
// the knowledge graph unconditionally before persistGeneratedTree even
// attempts engine.ValidateTreeFull — so when the winner tree fails
// validation and the disk write never happens, the knowledge graph is left
// claiming a better winner exists at evolvedID than what is actually
// persisted on disk. A later, genuinely weaker winner would then be silently
// rejected by RegisterEvolved's fitness gate because it can never beat the
// phantom fitness recorded for a tree that was never written.
func TestPersistEvolvedWinner_AtomicWithFailedDiskWrite(t *testing.T) {
	dir := t.TempDir()
	treeStore, err := evolution.NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	kg := knowledge.NewKnowledgeGraph()
	deps := &mcpDeps{treeStore: treeStore, kg: kg}

	strong := &evolution.SerializableNode{Type: "Action", Name: "AddCitations"}
	const wantID = "godev-evolved"

	first := map[string]interface{}{}
	persistEvolvedWinner(deps, "godev", strong, 90, first)
	if persisted, _ := first["persisted"].(bool); !persisted {
		t.Fatalf("first persistEvolvedWinner call must persist the initial winner; result=%v", first)
	}

	invalid := &evolution.SerializableNode{Type: "TotallyBogusNodeType", Name: "invalid-winner"}
	second := map[string]interface{}{}
	persistEvolvedWinner(deps, "godev", invalid, 95, second)

	if persisted, _ := second["persisted"].(bool); persisted {
		t.Fatalf("persistEvolvedWinner must not report persisted=true when the winner tree fails validation; result=%v", second)
	}

	loaded, err := treeStore.LoadNamed(wantID)
	if err != nil {
		t.Fatalf("LoadNamed(%q): %v", wantID, err)
	}
	if loaded == nil || loaded.Name != "AddCitations" {
		t.Fatalf("a winner that fails validation must not disturb the tree already persisted at %q; got %+v", wantID, loaded)
	}

	meta := kg.Trees[wantID]
	if meta == nil {
		t.Fatalf("expected evolved tree %q to still be registered in the knowledge graph", wantID)
	}
	if meta.StructuralFitness != 90 {
		t.Errorf("knowledge-graph bookkeeping must not record fitness 95 for a winner that was never written to disk; StructuralFitness got %v, want 90 (matching what is actually persisted)", meta.StructuralFitness)
	}
	if meta.NodeCount != evolution.CountNodes(strong) {
		t.Errorf("knowledge-graph bookkeeping must not record the failed winner's node count when its disk write never happened; NodeCount got %d, want %d (matching what is actually persisted)", meta.NodeCount, evolution.CountNodes(strong))
	}
	if meta.EvolvedCount != 1 {
		t.Errorf("EvolvedCount must not increment for a winner that failed to persist; got %d, want 1", meta.EvolvedCount)
	}
}

// TestEvolveToolsSurfacePopulationHealthSnapshot pins that the three production
// evolve tools that run a genetic Population — bt_evolve_genetic,
// bt_evolve_bottlenecks, and bt_evolve_selection_pressure — surface that
// population's Population.HealthSnapshot() in their JSON responses. The GA's
// self-healing signals (which population-level crises fired, how many extinct
// specialists were resurrected, and the mutation rate the generation actually
// applied — the emergency rate under crisis, otherwise the supervisor's
// recommendation) are computed on every one of these production evolve calls and
// then thrown away. Metrics and dashboard consumers cannot observe population
// health without reaching into Evolve internals.
//
// Each response must expose a "health" object carrying the three fields
// HealthSnapshot reports: "crisis_reasons" (a JSON array — empty, not omitted,
// when the run stayed healthy, so a consumer can always parse it),
// "resurrections" (a non-negative count), and "last_mutation_rate" (the positive
// rate the run actually applied). bt_evolve_genetic evolves a single population
// and surfaces its health at the top level; the per-tree bt_evolve_bottlenecks
// and bt_evolve_selection_pressure surface it on each evolved tree's report
// entry. Tiny population/generations keep the deterministic structural evolution
// -short-safe.
func TestEvolveToolsSurfacePopulationHealthSnapshot(t *testing.T) {
	// assertHealth verifies a report's "health" value is the JSON projection of
	// Population.HealthSnapshot() with all three self-healing fields present. It
	// uses Errorf (not Fatalf) so a single RED run reports every tool still
	// missing the health projection rather than stopping at the first.
	assertHealth := func(t *testing.T, tool string, health interface{}, present bool) {
		t.Helper()
		if !present {
			t.Errorf("%s response must surface Population.HealthSnapshot() under a 'health' object; it is absent", tool)
			return
		}
		h, isObj := health.(map[string]interface{})
		if !isObj {
			t.Errorf("%s 'health' must be a JSON object projecting Population.HealthSnapshot(); got %T (%v)", tool, health, health)
			return
		}
		if reasons, hasReasons := h["crisis_reasons"]; !hasReasons {
			t.Errorf("%s health object must report a 'crisis_reasons' key (an empty array when the run stayed healthy); got %v", tool, h)
		} else if _, isList := reasons.([]interface{}); !isList {
			t.Errorf("%s health 'crisis_reasons' must be a JSON array; got %T (%v)", tool, reasons, reasons)
		}
		if res, isNum := h["resurrections"].(float64); !isNum || res < 0 {
			t.Errorf("%s health object must report a non-negative 'resurrections' count; got %v", tool, h["resurrections"])
		}
		if rate, isNum := h["last_mutation_rate"].(float64); !isNum || rate <= 0 {
			t.Errorf("%s health 'last_mutation_rate' must be the positive rate the run actually applied; got %v", tool, h["last_mutation_rate"])
		}
	}

	// bt_evolve_genetic: a single population, so health is surfaced at the top
	// level of the response. No knowledge graph is needed — "godev" resolves to a
	// built-in tree and a nil experience bank degrades gracefully.
	genServer := engine.NewServer("health-genetic")
	registerMCPTools(genServer, &mcpDeps{})
	genRes, ok := genServer.Invoke("bt_evolve_genetic", json.RawMessage(`{"tree":"godev","population":4,"generations":2}`))
	if !ok || genRes == nil || len(genRes.Content) == 0 {
		t.Fatal("Invoke(bt_evolve_genetic) returned no content")
	}
	var genOut map[string]interface{}
	if err := json.Unmarshal([]byte(genRes.Content[0].Text), &genOut); err != nil {
		t.Fatalf("bt_evolve_genetic result is not valid JSON: %v (text=%q)", err, genRes.Content[0].Text)
	}
	if _, isErr := genOut["error"]; isErr {
		t.Fatalf("bt_evolve_genetic unexpectedly returned an error: %v", genOut)
	}
	genHealth, genPresent := genOut["health"]
	assertHealth(t, "bt_evolve_genetic", genHealth, genPresent)

	// bt_evolve_bottlenecks: per-tree health on each genetically evolved entry.
	// A parameterless bottleneck (RunCount>=3, Fitness<30) routes to the genetic
	// EvolveWithExperience path, which runs a full Population whose health must be
	// reported on its entry.
	bnKG := knowledge.NewKnowledgeGraph()
	bnKG.Register(&knowledge.TreeMeta{
		ID: "domain:alert_router", Name: "Alert Router", Category: "domain",
		Fitness: 8, RunCount: 3,
	})
	bnServer := engine.NewServer("health-bottlenecks")
	registerMCPTools(bnServer, &mcpDeps{kg: bnKG})
	bnRes, ok := bnServer.Invoke("bt_evolve_bottlenecks", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok || bnRes == nil || len(bnRes.Content) == 0 {
		t.Fatal("Invoke(bt_evolve_bottlenecks) returned no content")
	}
	var bnOut map[string]interface{}
	if err := json.Unmarshal([]byte(bnRes.Content[0].Text), &bnOut); err != nil {
		t.Fatalf("bt_evolve_bottlenecks result is not valid JSON: %v (text=%q)", err, bnRes.Content[0].Text)
	}
	bnReport, isList := bnOut["report"].([]interface{})
	if !isList || len(bnReport) != 1 {
		t.Fatalf("bt_evolve_bottlenecks must return a 'report' array holding the single genetic bottleneck; got %v", bnOut["report"])
	}
	bnEntry, isObj := bnReport[0].(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_bottlenecks report entry must be a JSON object; got %T (%v)", bnReport[0], bnReport[0])
	}
	if bnEntry["algorithm"] != "genetic" {
		t.Fatalf("bt_evolve_bottlenecks test fixture must route domain:alert_router to the genetic path; got algorithm %v", bnEntry["algorithm"])
	}
	bnHealth, bnPresent := bnEntry["health"]
	assertHealth(t, "bt_evolve_bottlenecks", bnHealth, bnPresent)

	// bt_evolve_selection_pressure: per-tree health on each bred entry (all
	// entries run the genetic path).
	spKG := knowledge.NewKnowledgeGraph()
	spKG.Register(&knowledge.TreeMeta{
		ID: "domain:code_review", Name: "Code Review", Category: "domain",
		Fitness: 90, RunCount: 2,
	})
	spServer := engine.NewServer("health-selection-pressure")
	registerMCPTools(spServer, &mcpDeps{kg: spKG})
	spRes, ok := spServer.Invoke("bt_evolve_selection_pressure", json.RawMessage(`{"population":4,"generations":2}`))
	if !ok || spRes == nil || len(spRes.Content) == 0 {
		t.Fatal("Invoke(bt_evolve_selection_pressure) returned no content")
	}
	var spOut map[string]interface{}
	if err := json.Unmarshal([]byte(spRes.Content[0].Text), &spOut); err != nil {
		t.Fatalf("bt_evolve_selection_pressure result is not valid JSON: %v (text=%q)", err, spRes.Content[0].Text)
	}
	spReport, isList := spOut["report"].([]interface{})
	if !isList || len(spReport) != 1 {
		t.Fatalf("bt_evolve_selection_pressure must return a 'report' array holding the single bred tree; got %v", spOut["report"])
	}
	spEntry, isObj := spReport[0].(map[string]interface{})
	if !isObj {
		t.Fatalf("bt_evolve_selection_pressure report entry must be a JSON object; got %T (%v)", spReport[0], spReport[0])
	}
	spHealth, spPresent := spEntry["health"]
	assertHealth(t, "bt_evolve_selection_pressure", spHealth, spPresent)
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

// TestBTEvolveQLearningAccumulatesDurableArchive pins milestone 2/4 of the
// durable Q-learning program (Q2 Evolvability): bt_evolve_qlearning must
// persist its QTable to a per-tree durable archive after every run and
// warm-start from that archive on the next invocation, so learned Q-values
// accumulate across runs instead of resetting to an empty table each call
// (today qt := evolution.NewQTable() is discarded on every call). The result
// JSON must report the warm start honestly — "warm_started": false on a cold
// home, true once an archive exists — and "learned_states_before" /
// "learned_states_after" must show the second run resuming from the first
// run's learned state count rather than starting from zero.
func TestBTEvolveQLearningAccumulatesDurableArchive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	// Identical args for both runs so warm-start states line up; epsilon=0
	// keeps action selection deterministic (pure greedy) once Q-values exist,
	// matching TestBTEvolveQLearningRegisteredAndLearnsGreedily.
	args := json.RawMessage(`{"tree":"godev","population":4,"generations":3,"epsilon":0}`)
	invoke := func(label string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_qlearning", args)
		if !ok {
			t.Fatalf("Invoke(bt_evolve_qlearning) reported the tool as unregistered on the %s run", label)
		}
		if res == nil || len(res.Content) == 0 {
			t.Fatalf("bt_evolve_qlearning returned no content on the %s run", label)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("bt_evolve_qlearning %s-run result is not valid JSON: %v (text=%q)", label, err, res.Content[0].Text)
		}
		if _, isErr := out["error"]; isErr {
			t.Fatalf("bt_evolve_qlearning unexpectedly returned an error on the %s run: %v", label, out)
		}
		return out
	}

	first := invoke("first")
	if got, isBool := first["warm_started"].(bool); !isBool || got {
		t.Errorf(`first run on a cold home must report "warm_started": false; got %v`, first["warm_started"])
	}
	if before, isNum := first["learned_states_before"].(float64); !isNum || before != 0 {
		t.Errorf("first run's 'learned_states_before' must be 0 on a cold home; got %v", first["learned_states_before"])
	}
	after1, isNum := first["learned_states_after"].(float64)
	if !isNum || after1 <= 0 {
		t.Fatalf("first run's 'learned_states_after' must be positive after learning generations; got %v", first["learned_states_after"])
	}

	matches, err := filepath.Glob(filepath.Join(home, "qtable_archive*.json"))
	if err != nil {
		t.Fatalf("glob qtable archives after the first run: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("bt_evolve_qlearning single-tree runs must persist exactly one durable qtable archive under BT_AGENT_HOME after the first run; got %v", matches)
	}

	second := invoke("second")
	if got, isBool := second["warm_started"].(bool); !isBool || !got {
		t.Errorf(`second run must warm-start from the durable archive and report "warm_started": true; got %v`, second["warm_started"])
	}
	if before2, isNum := second["learned_states_before"].(float64); !isNum || before2 != after1 {
		t.Errorf("second run must resume the first run's learned Q-values: 'learned_states_before' = %v, want %v (the first run's 'learned_states_after')", second["learned_states_before"], after1)
	}
}

// TestBTEvolveExpertSurfacesLearnedPatternFromQLearning pins milestone 2/2 of
// the durable Expert Knowledge program (Q2 Evolvability — "Give Expert
// Knowledge a durable, learning archive instead of a static hardcoded rule
// set"): bt_evolve_expert must warm-start from the same per-tree expert
// archive that bt_evolve_qlearning persists ExpertKnowledge.LearnedPatterns
// to (via Observe on every genuinely fitness-improving mutation, already
// wired into qLearnMutate at learning.go:921), so advisory calls surface
// accumulated cross-run learned patterns instead of only the hardcoded
// benchmark catalog. Today bt_evolve_expert always builds a fresh
// evolution.NewExpertKnowledge() (tools.go:1847) and bt_evolve_qlearning
// passes a nil ek to EvolveQLearning (tools.go:1288), so no learned pattern
// is ever produced, persisted, or read back.
func TestBTEvolveExpertSurfacesLearnedPatternFromQLearning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	// A large-enough population/generation budget across many mutation
	// attempts makes at least one genuinely fitness-improving mutation
	// overwhelmingly likely, so Observe records a learned pattern reliably in
	// practice while staying -short-safe (LLM-free structural fitness).
	qlArgs := json.RawMessage(`{"tree":"godev","population":10,"generations":8,"epsilon":0.2}`)
	qlRes, ok := server.Invoke("bt_evolve_qlearning", qlArgs)
	if !ok {
		t.Fatal("Invoke(bt_evolve_qlearning) reported the tool as unregistered")
	}
	if qlRes == nil || len(qlRes.Content) == 0 {
		t.Fatal("bt_evolve_qlearning returned no content")
	}
	var qlOut map[string]interface{}
	if err := json.Unmarshal([]byte(qlRes.Content[0].Text), &qlOut); err != nil {
		t.Fatalf("bt_evolve_qlearning result is not valid JSON: %v (text=%q)", err, qlRes.Content[0].Text)
	}
	if _, isErr := qlOut["error"]; isErr {
		t.Fatalf("bt_evolve_qlearning unexpectedly returned an error: %v", qlOut)
	}

	expRes, ok := server.Invoke("bt_evolve_expert", json.RawMessage(`{"tree":"godev"}`))
	if !ok {
		t.Fatal("Invoke(bt_evolve_expert) reported the tool as unregistered")
	}
	if expRes == nil || len(expRes.Content) == 0 {
		t.Fatal("bt_evolve_expert returned no content")
	}
	var expOut map[string]interface{}
	if err := json.Unmarshal([]byte(expRes.Content[0].Text), &expOut); err != nil {
		t.Fatalf("bt_evolve_expert result is not valid JSON: %v (text=%q)", err, expRes.Content[0].Text)
	}
	if _, isErr := expOut["error"]; isErr {
		t.Fatalf("bt_evolve_expert unexpectedly returned an error: %v", expOut)
	}

	learned, isArr := expOut["learned_patterns"].([]interface{})
	if !isArr || len(learned) == 0 {
		t.Fatalf("bt_evolve_expert must warm-start from the same expert archive bt_evolve_qlearning persisted to and surface a non-empty 'learned_patterns'; got %v (%T)", expOut["learned_patterns"], expOut["learned_patterns"])
	}
	for i, raw := range learned {
		entry, isObj := raw.(map[string]interface{})
		if !isObj {
			t.Fatalf("bt_evolve_expert 'learned_patterns'[%d] must be an object; got %T", i, raw)
		}
		action, _ := entry["action"].(string)
		category, _ := entry["category"].(string)
		gain, isNum := entry["gain"].(float64)
		if action == "" || category == "" || !isNum || gain <= 0 {
			t.Errorf("bt_evolve_expert 'learned_patterns'[%d] must carry a non-empty action/category and a positive gain (a genuine improvement observed during evolution); got %v", i, entry)
		}
	}
}

// TestBTEvolveQLearningStateCapBoundsDurableArchive pins milestone 4/4 of the
// durable Q-learning program (Q2 Evolvability): bt_evolve_qlearning must
// accept an optional "state_cap" request parameter and set it on
// evolution.QTable.Cap before qt.Load, so the eviction QTable.Update already
// enforces (internal/evolution/learning.go's enforceCap, pinned directly by
// TestQTable_UpdateEvictsLeastRecentlyUpdatedStateOnCapOverflow) also
// bounds the durable per-tree qtable archive in production. Without wiring,
// qt.Cap stays at its zero value (unbounded) regardless of how large the
// archive has grown across repeated warm-started calls. An omitted
// "state_cap" must derive a default of population*10, mirroring
// population_cap's population*3 default on bt_evolve_island (line ~1094).
// The result JSON must also surface cumulative "evicted_states"
// (evolution.QTable.EvictedStates), mirroring bt_evolve_island's
// "evicted_individuals", so eviction activity is observable without
// inspecting the archive file directly.
func TestBTEvolveQLearningStateCapBoundsDurableArchive(t *testing.T) {
	seedArchive := func(t *testing.T, path string, stateCount int) {
		t.Helper()
		values := make(map[string]map[string]float64, stateCount)
		for i := 0; i < stateCount; i++ {
			values[fmt.Sprintf("seed_state_%d", i)] = map[string]float64{"add_before": float64(i)}
		}
		data, err := json.Marshal(values)
		if err != nil {
			t.Fatalf("marshal seed qtable archive: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir seed qtable archive dir: %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write seed qtable archive: %v", err)
		}
	}
	readStateCount := func(t *testing.T, path string) int {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read qtable archive %s: %v", path, err)
		}
		var values map[string]map[string]float64
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatalf("qtable archive %s is not valid JSON: %v", path, err)
		}
		return len(values)
	}
	invoke := func(t *testing.T, server *engine.Server, args string) map[string]interface{} {
		t.Helper()
		res, ok := server.Invoke("bt_evolve_qlearning", json.RawMessage(args))
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
			t.Fatalf("bt_evolve_qlearning unexpectedly returned an error: %v", out)
		}
		return out
	}

	t.Run("default state_cap derives from population*10", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("BT_AGENT_HOME", home)
		path := qtableArchivePath("godev")
		const seedSize = 60 // exceeds the population=4 default cap of 40
		seedArchive(t, path, seedSize)

		server := engine.NewServer("test")
		registerMCPTools(server, &mcpDeps{})

		out := invoke(t, server, `{"tree":"godev","population":4,"generations":1,"epsilon":0}`)

		const wantCap = 40 // population(4) * 10
		if got := readStateCount(t, path); got > wantCap {
			t.Errorf("after a run with no explicit state_cap against a %d-state seeded archive, the durable archive holds %d states, want <= %d (population*10 default) — state_cap must default to population*10 and be set on QTable.Cap before Load", seedSize, got, wantCap)
		}
		evicted, isNum := out["evicted_states"].(float64)
		if !isNum || evicted <= 0 {
			t.Errorf(`bt_evolve_qlearning 'evicted_states' = %v, want > 0 after a default-capped run against an oversized %d-state seeded archive`, out["evicted_states"], seedSize)
		}
	})

	t.Run("explicit state_cap overrides the default", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("BT_AGENT_HOME", home)
		path := qtableArchivePath("godev")
		const seedSize = 20
		seedArchive(t, path, seedSize)

		server := engine.NewServer("test")
		registerMCPTools(server, &mcpDeps{})

		const stateCap = 5
		out := invoke(t, server, fmt.Sprintf(`{"tree":"godev","population":4,"generations":1,"epsilon":0,"state_cap":%d}`, stateCap))

		if got := readStateCount(t, path); got > stateCap {
			t.Errorf("after a run with explicit state_cap=%d against a %d-state seeded archive, the durable archive holds %d states, want <= %d — state_cap must be threaded onto QTable.Cap before Load", stateCap, seedSize, got, stateCap)
		}
		evicted, isNum := out["evicted_states"].(float64)
		if !isNum || evicted <= 0 {
			t.Errorf(`bt_evolve_qlearning 'evicted_states' = %v, want > 0 after an explicit state_cap=%d call against an oversized %d-state seeded archive`, out["evicted_states"], stateCap, seedSize)
		}
	})
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

// The DLQ's agent surface (c8094002 ms3): bt_dlq_list exposes retained entries
// and bt_dlq_replay requeue-flags one entry for drop-safe re-execution. With
// wait=true the replay runs synchronously through the configured executor so
// the caller sees the outcome; without an executor (an MCP sibling instance)
// the requeue flag alone is the deliverable — the daemon's scan consumes it.
func TestBTDLQToolsListAndReplay(t *testing.T) {
	prev := engine.TaskDLQ
	t.Cleanup(func() { engine.TaskDLQ = prev })

	dlq := reliability.NewDeadLetterQueue("")
	dlq.Push(reliability.DeadLetterEntry{ID: "dead-1", Agent: "agent-a", Task: "rebuild index", Error: "boom"})
	dlq.Push(reliability.DeadLetterEntry{ID: "dead-2", Agent: "agent-b", Task: "other work", Error: "kaput"})
	executed := 0
	dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
		executed++
		if e.ID != "dead-1" {
			t.Errorf("executor received wrong entry: %s", e.ID)
		}
		return nil
	})
	engine.TaskDLQ = dlq

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	for _, tool := range []string{"bt_dlq_list", "bt_dlq_replay"} {
		if !server.HasTool(tool) {
			t.Fatalf("%s must be registered by registerMCPTools", tool)
		}
	}

	res, ok := server.Invoke("bt_dlq_list", json.RawMessage(`{}`))
	if !ok || res == nil || len(res.Content) == 0 {
		t.Fatal("bt_dlq_list returned no content")
	}
	var listOut map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &listOut); err != nil {
		t.Fatalf("bt_dlq_list result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if n, isNum := listOut["count"].(float64); !isNum || int(n) != 2 {
		t.Fatalf("bt_dlq_list count = %v, want 2", listOut["count"])
	}
	if entries, isList := listOut["entries"].([]interface{}); !isList || len(entries) != 2 {
		t.Fatalf("bt_dlq_list entries = %v, want 2 entries", listOut["entries"])
	}

	// Synchronous replay of a live entry succeeds and removes it.
	rep, ok := server.Invoke("bt_dlq_replay", json.RawMessage(`{"id":"dead-1","wait":true}`))
	if !ok || rep == nil || len(rep.Content) == 0 {
		t.Fatal("bt_dlq_replay returned no content")
	}
	var repOut map[string]interface{}
	if err := json.Unmarshal([]byte(rep.Content[0].Text), &repOut); err != nil {
		t.Fatalf("bt_dlq_replay result is not valid JSON: %v", err)
	}
	if repOut["replayed"] != true {
		t.Fatalf("bt_dlq_replay must report replayed=true for a successful synchronous replay; got %v", repOut)
	}
	if executed != 1 {
		t.Fatalf("executor invocations = %d, want 1", executed)
	}
	if dlq.Len() != 1 {
		t.Fatalf("replayed entry must be removed; %d entries remain, want 1", dlq.Len())
	}

	// Unknown id: refused with a reason, nothing executed.
	unknownRep, _ := server.Invoke("bt_dlq_replay", json.RawMessage(`{"id":"no-such","wait":true}`))
	var unknownOut map[string]interface{}
	if err := json.Unmarshal([]byte(unknownRep.Content[0].Text), &unknownOut); err != nil {
		t.Fatalf("bt_dlq_replay unknown-id result is not valid JSON: %v", err)
	}
	if unknownOut["requeued"] != false {
		t.Fatalf("unknown id must not be requeued; got %v", unknownOut)
	}
	if executed != 1 {
		t.Fatalf("unknown id must not reach the executor; invocations = %d", executed)
	}
}

// bt_dlq_replay wait=true must surface the actual replay failure the queue
// persists (DeadLetterEntry.LastReplayAt/LastReplayError, stamped by a failed
// Replay) instead of only the canned reason string — otherwise the MCP caller
// cannot distinguish "executor missing in this instance" from "the task
// genuinely failed again", let alone see why it failed. The generic reason
// alone remains correct only for the executor-missing case, where no attempt
// was stamped.
func TestDLQReplayWaitReportsFailureOutcome(t *testing.T) {
	prev := engine.TaskDLQ
	t.Cleanup(func() { engine.TaskDLQ = prev })

	// Persisted queue: the failed-replay stamp round-trips through the file the
	// handler Reload()s, exactly as in the daemon topology.
	dlq := reliability.NewDeadLetterQueue(filepath.Join(t.TempDir(), "dlq.json"))
	dlq.Push(reliability.DeadLetterEntry{ID: "dead-1", Agent: "agent-a", Task: "rebuild index", Error: "boom"})
	dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
		return errors.New("tree run failed: timeout")
	})
	engine.TaskDLQ = dlq

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	rep, ok := server.Invoke("bt_dlq_replay", json.RawMessage(`{"id":"dead-1","wait":true}`))
	if !ok || rep == nil || len(rep.Content) == 0 {
		t.Fatal("bt_dlq_replay returned no content")
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(rep.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_dlq_replay result is not valid JSON: %v", err)
	}
	if out["replayed"] != false {
		t.Fatalf("failed synchronous replay must report replayed=false; got %v", out)
	}
	if got, _ := out["last_replay_error"].(string); got != "tree run failed: timeout" {
		t.Fatalf("bt_dlq_replay wait=true must surface the executor's error as last_replay_error; got %v (full result: %v)", out["last_replay_error"], out)
	}
	stamp, _ := out["last_replay_at"].(string)
	if stamp == "" {
		t.Fatalf("bt_dlq_replay wait=true must include last_replay_at for a stamped failure; got %v", out)
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Fatalf("last_replay_at must be RFC3339, got %q: %v", stamp, err)
	}

	// Executor missing (an MCP sibling with no tree runner): Replay refuses
	// before any attempt is stamped, so the generic reason stands and no
	// last_replay_error key may leak from a previous life of the entry.
	bare := reliability.NewDeadLetterQueue("")
	bare.Push(reliability.DeadLetterEntry{ID: "dead-2", Agent: "agent-b", Task: "other work", Error: "kaput"})
	engine.TaskDLQ = bare

	rep2, ok := server.Invoke("bt_dlq_replay", json.RawMessage(`{"id":"dead-2","wait":true}`))
	if !ok || rep2 == nil || len(rep2.Content) == 0 {
		t.Fatal("bt_dlq_replay (no executor) returned no content")
	}
	var out2 map[string]interface{}
	if err := json.Unmarshal([]byte(rep2.Content[0].Text), &out2); err != nil {
		t.Fatalf("bt_dlq_replay (no executor) result is not valid JSON: %v", err)
	}
	if out2["replayed"] != false {
		t.Fatalf("replay without an executor must report replayed=false; got %v", out2)
	}
	if reason, _ := out2["reason"].(string); reason == "" {
		t.Fatalf("replay without an executor must keep the generic reason; got %v", out2)
	}
	if _, present := out2["last_replay_error"]; present {
		t.Fatalf("no attempt was stamped, so last_replay_error must be absent; got %v", out2)
	}
	if _, present := out2["last_replay_at"]; present {
		t.Fatalf("no attempt was stamped, so last_replay_at must be absent; got %v", out2)
	}
}

// Production evolution passes must carry the specialist registry so crisis
// resurrection (f5f47894 milestone 4) is live outside tests: a population
// without Specialists silently skips archetype archival and resurrection.
func TestNewProductionPopulationAttachesSpecialists(t *testing.T) {
	pop := newProductionPopulation(4, evolution.DefaultTree())
	if pop == nil {
		t.Fatal("newProductionPopulation returned nil")
	}
	if pop.Specialists == nil || len(pop.Specialists.Archetypes) == 0 {
		t.Fatal("production populations must attach a seeded SpecialistRegistry")
	}
}

// Every population the MCP evolution tools build must go through the
// production helper — a raw evolution.NewPopulation call site silently drops
// the specialist wiring again.
func TestToolsBuildPopulationsViaProductionHelper(t *testing.T) {
	src, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	if n := strings.Count(string(src), "evolution.NewPopulation("); n != 1 {
		t.Fatalf("evolution.NewPopulation appears %d times in tools.go; every call site must route through newProductionPopulation (exactly 1 occurrence, inside the helper)", n)
	}
}

// evolution.NewNSGAIIPopulation seeds its own Specialists field, but only with
// NewSpecialistRegistry() — an empty registry with no resurrection material —
// unlike newProductionPopulation's evolution.SeedSpecialistRegistry(), which
// pre-loads the benchmark-validated archetypes. The bt_evolve_multiobjective
// handler must overwrite that empty registry with a seeded one so NSGA-II gets
// the same crisis-resurrection material every other production Evolve variant
// carries; a second evolution.SeedSpecialistRegistry() call site (the first is
// inside newProductionPopulation) is the only way to check that from source,
// since the handler builds its population inside a RegisterTool closure with
// no exported hook to inspect afterward.
func TestBTEvolveMultiObjectiveSeedsSpecialistRegistry(t *testing.T) {
	src, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	if n := strings.Count(string(src), "evolution.SeedSpecialistRegistry()"); n != 2 {
		t.Fatalf("evolution.SeedSpecialistRegistry() appears %d times in tools.go; want 2 (newProductionPopulation plus the bt_evolve_multiobjective NSGA-II population, overwriting NewNSGAIIPopulation's empty default registry)", n)
	}
}

// Without a queue (engine.TaskDLQ nil — bare test binaries), the DLQ tools
// degrade to an error shape instead of panicking.
func TestBTDLQToolsWithoutQueue(t *testing.T) {
	prev := engine.TaskDLQ
	engine.TaskDLQ = nil
	t.Cleanup(func() { engine.TaskDLQ = prev })

	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	for _, tool := range []string{"bt_dlq_list", "bt_dlq_replay"} {
		res, ok := server.Invoke(tool, json.RawMessage(`{"id":"x"}`))
		if !ok || res == nil || len(res.Content) == 0 {
			t.Fatalf("%s returned no content", tool)
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
			t.Fatalf("%s result is not valid JSON: %v", tool, err)
		}
		if _, hasErr := out["error"]; !hasErr {
			t.Fatalf("%s without a queue must return an error shape; got %v", tool, out)
		}
	}
}

// TestBTThinkTankAnalyzeSurfacesOrchestratorError pins milestone 3/4 of the
// Q1 Correctness program: bt_thinktank_analyze discards
// orch.RunFullAnalysis's returned error (`_ = orch.RunFullAnalysis(params.Topic)`)
// and always reports the (empty) findings as if analysis succeeded. With no
// llmClient wired (mcpDeps{} zero value — deps.llmClient is a nil llm.LLM),
// thinktank.ThinkTankOrchestrator.RunResearchRound returns a genuine
// "LLM is nil" error on its very first phase, which RunFullAnalysis wraps and
// returns. The handler must check that error and surface it via an explicit
// "error" field instead of silently returning zeroed-out findings/fellows
// counts as a 200-shaped success.
func TestBTThinkTankAnalyzeSurfacesOrchestratorError(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})

	if !server.HasTool("bt_thinktank_analyze") {
		t.Fatal("bt_thinktank_analyze tool must be registered by registerMCPTools")
	}

	args := json.RawMessage(`{"topic":"Should we ship feature X"}`)
	res, ok := server.Invoke("bt_thinktank_analyze", args)
	if !ok {
		t.Fatal("Invoke(bt_thinktank_analyze) reported the tool as unregistered")
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("bt_thinktank_analyze returned no content")
	}

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("bt_thinktank_analyze result is not valid JSON: %v", err)
	}

	errVal, hasErr := out["error"]
	if !hasErr || errVal == "" {
		t.Fatalf("bt_thinktank_analyze with a failing orchestrator (no llmClient) must surface an "+
			"explicit non-empty \"error\" field instead of a false-success response; got %v", out)
	}
}
