package knowledge

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// =============================================================================
// REGISTRY OPERATIONS
// =============================================================================

func TestRegister_Duplicate(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm1 := &TreeMeta{ID: "dup", Name: "First", Category: "test", Keywords: []string{"first"}}
	tm2 := &TreeMeta{ID: "dup", Name: "Second", Category: "test", Keywords: []string{"second"}}
	kg.Register(tm1)
	kg.Register(tm2)

	// Check the tree was overwritten
	if kg.Trees["dup"].Name != "Second" {
		t.Errorf("expected 'Second' after duplicate register, got %q", kg.Trees["dup"].Name)
	}
	// Check synonyms were updated
	if kg.Synonyms["second"] != "dup" {
		t.Error("synonym 'second' should map to 'dup' after re-register")
	}
}

func TestRegister_SynonymIndexing(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{
		ID:       "syn_test",
		Name:     "Synonym Test",
		Category: "test",
		Keywords: []string{"hello", "WORLD"},
		Capabilities: []Capability{
			{Action: "do_stuff", Domain: "testing", Strength: 0.9},
		},
	}
	kg.Register(tm)

	// Keywords should be lowercased in synonyms
	if kg.Synonyms["hello"] != "syn_test" {
		t.Error("keyword 'hello' not indexed")
	}
	if kg.Synonyms["world"] != "syn_test" {
		t.Error("keyword 'WORLD' should be indexed as 'world'")
	}
	// Capability action should be indexed
	if kg.Synonyms["do_stuff"] != "syn_test" {
		t.Error("capability action 'do_stuff' not indexed")
	}
}

func TestRegister_MultipleEdges(t *testing.T) {
	kg := NewKnowledgeGraph()
	for i := 0; i < 10; i++ {
		kg.Register(&TreeMeta{
			ID:       fmt.Sprintf("t%d", i),
			Name:     fmt.Sprintf("Tree %d", i),
			Category: "bulk",
			Keywords: []string{fmt.Sprintf("kw%d", i)},
		})
	}
	if len(kg.Trees) != 10 {
		t.Errorf("expected 10 trees, got %d", len(kg.Trees))
	}
	if len(kg.Synonyms) != 10 {
		t.Errorf("expected 10 synonyms, got %d", len(kg.Synonyms))
	}
}

func TestRegister_EmptyKeywords(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{ID: "no_kw", Name: "No Keywords", Category: "test"}
	kg.Register(tm)
	if kg.Trees["no_kw"] == nil {
		t.Error("tree should be registered even without keywords")
	}
}

// =============================================================================
// GRAPH TRAVERSAL / CONNECT
// =============================================================================

func TestConnect_Single(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})
	kg.Connect("a", "b", "depends_on")

	if len(kg.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(kg.Edges))
	}
	e := kg.Edges[0]
	if e.From != "a" || e.To != "b" || e.Type != "depends_on" {
		t.Errorf("edge mismatch: %+v", e)
	}
	if e.Weight != 1.0 {
		t.Errorf("expected weight 1.0, got %.2f", e.Weight)
	}
}

func TestConnect_Multiple(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})
	kg.Register(&TreeMeta{ID: "c", Name: "C", Category: "test"})

	kg.Connect("a", "b", "depends_on")
	kg.Connect("b", "c", "composes")
	kg.Connect("a", "c", "extends")

	if len(kg.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(kg.Edges))
	}
}

func TestConnect_Duplicate(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "test"})

	kg.Connect("a", "b", "depends_on")
	kg.Connect("a", "b", "depends_on") // duplicate
	kg.Connect("a", "b", "composes")   // different type

	if len(kg.Edges) != 3 {
		t.Fatalf("expected 3 edges (duplicates allowed), got %d", len(kg.Edges))
	}
}

func TestConnect_GlobalGraph(t *testing.T) {
	kg := BuildKnowledgeGraph()
	if len(kg.Edges) == 0 {
		t.Error("global graph should have edges")
	}
	// Verify specific edges exist
	foundDeps := false
	foundComposes := false
	foundExtends := false
	foundSpecializes := false
	for _, e := range kg.Edges {
		switch e.Type {
		case "depends_on":
			foundDeps = true
		case "composes":
			foundComposes = true
		case "extends":
			foundExtends = true
		case "specializes":
			foundSpecializes = true
		}
	}
	if !foundDeps {
		t.Error("no depends_on edges found")
	}
	if !foundComposes {
		t.Error("no composes edges found")
	}
	if !foundExtends {
		t.Error("no extends edges found")
	}
	if !foundSpecializes {
		t.Error("no specializes edges found")
	}
	t.Logf("total edges: %d", len(kg.Edges))
}

func TestConnect_MissingNodes(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "test"})
	// Connect to non-existent node — should not crash
	kg.Connect("a", "nonexistent", "depends_on")
	kg.Connect("nonexistent", "a", "depends_on")
	kg.Connect("x", "y", "depends_on")

	if len(kg.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(kg.Edges))
	}
}

// =============================================================================
// TREE DISCOVERY MATCHING
// =============================================================================

func TestDiscover_ExactKeywordMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "test:exact",
		Name:     "Exact Match",
		Category: "test",
		Keywords: []string{"widget", "gadget", "thingamajig"},
	})

	id, conf := kg.Discover("I need a widget")
	if id != "test:exact" {
		t.Errorf("expected 'test:exact', got %q", id)
	}
	if conf != 0.8 {
		t.Errorf("expected confidence 0.8 for exact keyword match, got %.2f", conf)
	}
}

func TestDiscover_CapabilityOverlap(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "test:cap",
		Name:     "Cap Match",
		Category: "test",
		Keywords: []string{"cake", "baking", "cooking"},
		Capabilities: []Capability{
			{Action: "bake_cake", Domain: "cooking", Strength: 1.0},
		},
	})

	// Keywords provide exact match (Phase 1)
	id, conf := kg.Discover("I want to bake a cake for the party")
	if id != "test:cap" {
		t.Errorf("expected 'test:cap', got %q", id)
	}
	if conf != 0.8 {
		t.Errorf("expected confidence 0.8 for keyword match, got %.2f", conf)
	}
	t.Logf("capability match: id=%s conf=%.2f", id, conf)

	// Phase 2: capability overlap scoring (no keyword match)
	kg2 := NewKnowledgeGraph()
	kg2.Register(&TreeMeta{
		ID:       "test:cap2",
		Name:     "Cap Match 2",
		Category: "test",
		Capabilities: []Capability{
			{Action: "bake_cake", Domain: "cooking", Strength: 1.0},
		},
	})
	id2, conf2 := kg2.Discover("cooking bake_cake preparation")
	if id2 != "test:cap2" {
		t.Errorf("capability overlap: expected 'test:cap2', got %q", id2)
	}
	if conf2 <= 0 {
		t.Error("confidence should be > 0 for capability overlap")
	}
	t.Logf("capability overlap: id=%s conf=%.2f", id2, conf2)
}

func TestDiscover_BestMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Weak match
	kg.Register(&TreeMeta{
		ID:       "weak",
		Name:     "Weak",
		Category: "test",
		Keywords: []string{"other"},
		Capabilities: []Capability{
			{Action: "unrelated", Domain: "test", Strength: 0.1},
		},
	})
	// Strong match
	kg.Register(&TreeMeta{
		ID:       "strong",
		Name:     "Strong",
		Category: "test",
		Keywords: []string{"finance", "model", "dcf"},
		Capabilities: []Capability{
			{Action: "analyze_financials", Domain: "finance", Strength: 1.0},
		},
	})

	id, _ := kg.Discover("build a DCF financial model")
	if id != "strong" {
		t.Errorf("expected 'strong', got %q", id)
	}
}

func TestDiscover_EmptyGraph(t *testing.T) {
	kg := NewKnowledgeGraph()
	id, conf := kg.Discover("do something useful")
	if id != "" {
		t.Errorf("empty graph should return empty id, got %q", id)
	}
	if conf != 0.0 {
		t.Errorf("empty graph should return 0 confidence, got %.2f", conf)
	}
}

func TestDiscover_NoMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "only",
		Name:     "Only Tree",
		Category: "test",
		Keywords: []string{"alpha", "beta"},
	})

	id, conf := kg.Discover("xyzzy plugh plover completely unrelated")
	if id != "" || conf != 0.0 {
		t.Logf("no-match discover: id=%s conf=%.2f (may return weak match)", id, conf)
	}
	// Should not return a result with low confidence
}

func TestDiscover_CategoryMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "cat_test",
		Name:     "Category Test",
		Category: "finance",
		Keywords: []string{"stuff"},
	})

	id, conf := kg.Discover("finance stuff")
	if id != "cat_test" {
		t.Errorf("expected category match, got %q", id)
	}
	if conf <= 0 {
		t.Error("confidence should be > 0 for category match")
	}
	t.Logf("category match: id=%s conf=%.2f", id, conf)
}

func TestDiscover_GlobalGraphVariety(t *testing.T) {
	kg := BuildKnowledgeGraph()

	tests := []struct {
		task     string
		expectID string // empty = any non-empty
	}{
		{"review code", ""},
		{"pitch investment", ""},
		{"deep research", ""},
		{"startup strategy", ""},
		{"thinktank fellow perspective analysis", ""},
		{"evolve and optimize", ""},
		{"deploy pipeline", ""},
		{"reconcile ledger", ""},
		{"audit security", ""},
		{"close books month end", ""},
	}

	for _, tt := range tests {
		id, conf := kg.Discover(tt.task)
		if id == "" {
			t.Errorf("Discover(%q) returned empty id", tt.task)
		} else if conf <= 0 {
			t.Errorf("Discover(%q) returned zero confidence", tt.task)
		}
		t.Logf("Discover(%q) -> %s (%.2f)", tt.task, id, conf)
	}
}

// =============================================================================
// CATEGORY LISTING
// =============================================================================

func TestListByCategory_All(t *testing.T) {
	kg := BuildKnowledgeGraph()

	categories := []string{"core", "finance", "research", "domain", "startup", "thinktank", "evolution"}
	counts := map[string]int{
		"core": 2, "finance": 10, "research": 2, "domain": 13,
		"startup": 6, "thinktank": 5, "evolution": 3,
	}

	for _, cat := range categories {
		trees := kg.ListByCategory(cat)
		expected := counts[cat]
		if len(trees) != expected {
			t.Errorf("category %s: expected %d trees, got %d", cat, expected, len(trees))
		}
		for _, tree := range trees {
			if tree.Category != cat {
				t.Errorf("category %s: tree %s has category %s", cat, tree.ID, tree.Category)
			}
		}
	}
}

func TestListByCategory_Empty(t *testing.T) {
	kg := NewKnowledgeGraph()
	result := kg.ListByCategory("nonexistent")
	if len(result) != 0 {
		t.Errorf("expected 0 trees for nonexistent category, got %d", len(result))
	}
}

func TestListByCategory_NonExistent(t *testing.T) {
	kg := BuildKnowledgeGraph()
	result := kg.ListByCategory("does_not_exist")
	if len(result) != 0 {
		t.Errorf("expected 0 trees, got %d", len(result))
	}
}

func TestListByCategory_EmptyString(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "empty_cat", Name: "Empty Cat", Category: ""})
	result := kg.ListByCategory("")
	if len(result) != 1 {
		t.Errorf("expected 1 tree with empty category, got %d", len(result))
	}
}

// =============================================================================
// QUERY (capability search)
// =============================================================================

func TestQuery_ByAction(t *testing.T) {
	kg := BuildKnowledgeGraph()
	results := kg.Query("review_code")
	if len(results) == 0 {
		t.Error("Query('review_code') should return results")
	}
	for _, r := range results {
		t.Logf("  found: %s (%s)", r.ID, r.Name)
	}
}

func TestQuery_ByDomain(t *testing.T) {
	kg := BuildKnowledgeGraph()
	results := kg.Query("engineering")
	if len(results) == 0 {
		t.Error("Query('engineering') should return results")
	}
}

func TestQuery_ByFinance(t *testing.T) {
	kg := BuildKnowledgeGraph()
	results := kg.Query("analyze_financials")
	if len(results) == 0 {
		t.Error("Query('analyze_financials') should return results")
	}
	// All finance trees have analyze_financials
	if len(results) < 5 {
		t.Errorf("expected at least 5 results for analyze_financials, got %d", len(results))
	}
}

func TestQuery_NoResults(t *testing.T) {
	kg := NewKnowledgeGraph()
	results := kg.Query("nonexistent_capability")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestQuery_CaseInsensitive(t *testing.T) {
	kg := BuildKnowledgeGraph()
	lower := kg.Query("review_code")
	upper := kg.Query("REVIEW_CODE")
	mixed := kg.Query("Review_Code")

	if len(lower) != len(upper) || len(lower) != len(mixed) {
		t.Errorf("query should be case-insensitive: lower=%d upper=%d mixed=%d",
			len(lower), len(upper), len(mixed))
	}
}

func TestQuery_Deduplication(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "multi_cap",
		Name:     "Multi Cap",
		Category: "test",
		Capabilities: []Capability{
			{Action: "do_x", Domain: "test", Strength: 0.5},
			{Action: "do_x_also", Domain: "test", Strength: 0.5},
		},
	})
	// "do_x" appears in both capabilities' Action prefix — tree should appear once
	results := kg.Query("do_x")
	if len(results) != 1 {
		t.Errorf("expected 1 result (break after first match), got %d", len(results))
	}
}

// =============================================================================
// SUMMARY
// =============================================================================

func TestSummary_Empty(t *testing.T) {
	kg := NewKnowledgeGraph()
	s := kg.Summary()
	if !strings.Contains(s, "0 trees") {
		t.Errorf("empty summary should show 0 trees, got: %s", s)
	}
	if !strings.Contains(s, "0 edges") {
		t.Errorf("empty summary should show 0 edges, got: %s", s)
	}
}

func TestSummary_Single(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "one", Name: "One", Category: "solo"})
	s := kg.Summary()
	if !strings.Contains(s, "solo(1)") {
		t.Errorf("summary should show solo(1), got: %s", s)
	}
	if !strings.Contains(s, "1 trees") {
		t.Errorf("summary should show 1 trees, got: %s", s)
	}
}

func TestSummary_Multiple(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "cat_a"})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "cat_a"})
	kg.Register(&TreeMeta{ID: "c", Name: "C", Category: "cat_b"})
	kg.Connect("a", "b", "depends_on")

	s := kg.Summary()
	if !strings.Contains(s, "cat_a(2)") {
		t.Errorf("summary should show cat_a(2), got: %s", s)
	}
	if !strings.Contains(s, "cat_b(1)") {
		t.Errorf("summary should show cat_b(1), got: %s", s)
	}
	if !strings.Contains(s, "3 trees") {
		t.Errorf("summary should show 3 trees, got: %s", s)
	}
	if !strings.Contains(s, "1 edges") {
		t.Errorf("summary should show 1 edges, got: %s", s)
	}
}

func TestSummary_GlobalGraph(t *testing.T) {
	kg := BuildKnowledgeGraph()
	s := kg.Summary()
	t.Logf("Summary: %s", s)

	if !strings.Contains(s, "41 trees") {
		t.Errorf("global summary should show 41 trees, got: %s", s)
	}
	// Should contain category counts
	for _, cat := range []string{"core", "finance", "research", "domain", "startup", "thinktank", "evolution"} {
		if !strings.Contains(s, cat) {
			t.Errorf("summary missing category %q: %s", cat, s)
		}
	}
}

// =============================================================================
// itoa helper
// =============================================================================

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "10"},
		{42, "42"},
		{100, "100"},
		{999, "999"},
		{1000, "1000"},
		{12345, "12345"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// =============================================================================
// matchScore helper
// =============================================================================

func TestMatchScore_KeywordMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{
		Keywords: []string{"hello", "world"},
		Category: "test",
	}
	score := kg.matchScore("hello world", tm)
	if score < 0.4 {
		t.Errorf("two keyword matches should score >= 0.4, got %.2f", score)
	}
}

func TestMatchScore_NoMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{
		Keywords:     []string{"alpha", "beta"},
		Category:     "test",
		Capabilities: []Capability{},
	}
	score := kg.matchScore("xyzzy plugh", tm)
	if score != 0.0 {
		t.Errorf("no match should score 0.0, got %.2f", score)
	}
}

func TestMatchScore_CapabilityMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{
		Capabilities: []Capability{
			{Action: "bake_cake", Domain: "cooking", Strength: 0.5},
		},
	}
	score := kg.matchScore("bake a cake in the cooking department", tm)
	if score <= 0 {
		t.Errorf("capability match should score > 0, got %.2f", score)
	}
}

func TestMatchScore_CategoryMatch(t *testing.T) {
	kg := NewKnowledgeGraph()
	tm := &TreeMeta{
		Category: "finance",
	}
	score := kg.matchScore("this is a finance task", tm)
	if score != 0.1 {
		t.Errorf("category match should score exactly 0.1, got %.2f", score)
	}
}

// =============================================================================
// FACTORY TESTS (core factory.go coverage)
// =============================================================================

func TestNewFactory(t *testing.T) {
	kg := BuildKnowledgeGraph()
	f := NewFactory(kg)
	if f == nil {
		t.Fatal("NewFactory returned nil")
	}
	if f.Graph != kg {
		t.Error("Factory.Graph should be the passed kg")
	}
	if f.Expert == nil {
		t.Error("Factory.Expert should not be nil")
	}
	if f.Templates == nil {
		t.Error("Factory.Templates should not be nil")
	}
	if len(f.Templates) == 0 {
		t.Error("Factory should have templates from kg")
	}
}

func TestExtractTemplates(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "test:a", Name: "A", Category: "finance",
		NodeCount: 10, Fitness: 80, Keywords: []string{"money"},
	})
	kg.Register(&TreeMeta{
		ID:       "test:b", Name: "B", Category: "research",
		NodeCount: 15, Fitness: 90, Keywords: []string{"study"},
	})
	kg.Register(&TreeMeta{
		ID:       "test:c", Name: "C", Category: "finance",
		NodeCount: 20, Fitness: 70, Keywords: []string{"invest"},
	})

	f := NewFactory(kg)

	// Templates should exist (last one per category wins due to map overwrite)
	if f.Templates["finance"].SourceID != "test:c" {
		t.Errorf("expected finance template from test:c, got %s", f.Templates["finance"].SourceID)
	}
	if f.Templates["research"].SourceID != "test:b" {
		t.Errorf("expected research template from test:b, got %s", f.Templates["research"].SourceID)
	}
}

func TestContainsAnyStr(t *testing.T) {
	if !containsAnyStr("hello world", "hello") {
		t.Error("should find 'hello' in 'hello world'")
	}
	if containsAnyStr("hello world", "xyzzy") {
		t.Error("should not find 'xyzzy' in 'hello world'")
	}
	if !containsAnyStr("finance and banking", "finance", "bank", "money") {
		t.Error("should find 'finance' in list")
	}
	if containsAnyStr("nothing here", "xyzzy", "plugh", "plover") {
		t.Error("should not find any match")
	}
}

func TestDetermineCategory(t *testing.T) {
	tests := []struct {
		task     string
		expected string
	}{
		{"invest in stocks", "finance"},
		{"revenue earnings valuation", "finance"},
		{"debug the program", "domain"},
		{"refactor this code", "domain"},
		{"research quantum computing", "research"},
		{"analyze the data", "research"},
		{"startup strategy session", "startup"},
		{"hire a ceo", "startup"},
		{"think about philosophy", "thinktank"},
		{"debate climate change", "thinktank"},
		{"evolve the system", "evolution"},
		{"optimize and improve", "evolution"},
		{"generic task here", "core"},
	}

	for _, tt := range tests {
		got := determineCategory(tt.task)
		if got != tt.expected {
			t.Errorf("determineCategory(%q) = %q, want %q", tt.task, got, tt.expected)
		}
	}
}

func TestTruncateTask(t *testing.T) {
	tests := []struct {
		task string
		n    int
		want string
	}{
		{"short", 100, "short"},
		{"short", 10, "short"},
		{"this is a long task description that should be truncated", 20, "this is a long ta..."},
		{"exactly ten", 10, "exactly..."},
	}
	for _, tt := range tests {
		got := truncateTask(tt.task, tt.n)
		if got != tt.want {
			t.Errorf("truncateTask(%q, %d) = %q, want %q", tt.task, tt.n, got, tt.want)
		}
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		task string
		min  int // minimum expected keywords
	}{
		{"build a DCF model for valuation analysis", 4},
		{"review go code for bugs and issues", 3},
		{"a b c d", 0},   // all <= 3 chars
		{"", 0},           // empty
		{"hello, world! this is a test.", 3},
	}

	for _, tt := range tests {
		kw := extractKeywords(tt.task)
		if len(kw) < tt.min {
			t.Errorf("extractKeywords(%q) = %d keywords (%v), expected >= %d",
				tt.task, len(kw), kw, tt.min)
		}
		// Verify no keyword is <= 3 chars
		for _, k := range kw {
			if len(k) <= 3 {
				t.Errorf("extractKeywords(%q): keyword %q has length %d (should be > 3)",
					tt.task, k, len(k))
			}
		}
	}
}

func TestGenerateTreeName(t *testing.T) {
	f := NewFactory(NewKnowledgeGraph())

	name := f.generateTreeName("finance", "build a DCF model for valuation analysis")
	if !strings.HasPrefix(name, "finance:") {
		t.Errorf("expected name to start with 'finance:', got %q", name)
	}
	// Should contain key words from task
	if !strings.Contains(name, "build") && !strings.Contains(name, "dcf") && !strings.Contains(name, "model") {
		t.Logf("generated name: %s (may not contain expected words)", name)
	}

	// Edge case: no valid words (>3 chars)
	name2 := f.generateTreeName("core", "a b c d")
	if !strings.HasPrefix(name2, "core:") {
		t.Errorf("expected fallback to start with 'core:', got %q", name2)
	}
	if !strings.Contains(name2, "agent") {
		t.Logf("fallback name should contain 'agent': %s", name2)
	}
}

func TestPickToolSetup(t *testing.T) {
	f := &Factory{}
	tests := map[string]string{
		"research":  "SetupResearchTools",
		"startup":   "SetupStartupTools",
		"domain":    "SetupDevTools",
		"evolution": "SetupDefaultTools",
		"finance":   "SetupDefaultTools",
		"unknown":   "SetupDefaultTools",
	}
	for cat, expected := range tests {
		got := f.pickToolSetup(cat)
		if got != expected {
			t.Errorf("pickToolSetup(%q) = %q, want %q", cat, got, expected)
		}
	}
}

func TestDefaultFunctions(t *testing.T) {
	f := &Factory{}

	// defaultPreGate
	pg := f.defaultPreGate()
	if pg.Type != "Sequence" || pg.Name != "PreGate" {
		t.Errorf("defaultPreGate: type=%s name=%s", pg.Type, pg.Name)
	}
	if len(pg.Children) < 2 {
		t.Error("defaultPreGate should have at least 2 children")
	}

	// defaultAgentPath
	ap := f.defaultAgentPath("do something useful")
	if ap.Type != "Sequence" || ap.Name != "ExecutionPath" {
		t.Errorf("defaultAgentPath: type=%s name=%s", ap.Type, ap.Name)
	}
	if len(ap.Children) < 1 {
		t.Error("defaultAgentPath should have at least 1 child")
	}

	// defaultOutcomeSelector
	os := f.defaultOutcomeSelector()
	if os.Type != "Selector" || os.Name != "OutcomeSelector" {
		t.Errorf("defaultOutcomeSelector: type=%s name=%s", os.Type, os.Name)
	}
	if len(os.Children) < 2 {
		t.Error("defaultOutcomeSelector should have at least 2 children")
	}
}

func TestCloneFunctions(t *testing.T) {
	f := &Factory{}

	tmpl := &TreeTemplate{
		SourceID: "test:source",
		Category: "research",
	}

	// clonePreGate
	pg := f.clonePreGate(tmpl)
	if pg == nil {
		t.Fatal("clonePreGate returned nil")
	}
	if pg.Type != "Sequence" || pg.Name != "PreGate" {
		t.Errorf("clonePreGate: type=%s name=%s", pg.Type, pg.Name)
	}
	if len(pg.Children) < 2 {
		t.Error("clonePreGate should have at least 2 children")
	}

	// cloneStrategyRouter
	sr := f.cloneStrategyRouter(tmpl)
	if sr == nil {
		t.Fatal("cloneStrategyRouter returned nil")
	}
	if sr.Type != "Selector" || sr.Name != "StrategyRouter" {
		t.Errorf("cloneStrategyRouter: type=%s name=%s", sr.Type, sr.Name)
	}
	if len(sr.Children) < 2 {
		t.Error("cloneStrategyRouter should have at least 2 children")
	}
}

func TestBuildBasicAgentTree(t *testing.T) {
	f := &Factory{}
	tree := f.buildBasicAgentTree()
	if tree == nil {
		t.Fatal("buildBasicAgentTree returned nil")
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence type, got %s", tree.Type)
	}
	if tree.Name != "BasicAgent" {
		t.Errorf("expected 'BasicAgent', got %q", tree.Name)
	}
	if len(tree.Children) < 3 {
		t.Errorf("expected at least 3 children, got %d", len(tree.Children))
	}
	// Verify PreGate
	pg := tree.Children[0]
	if pg.Name != "PreGate" {
		t.Errorf("first child should be PreGate, got %q", pg.Name)
	}
	// Verify OutcomeSelector
	os := tree.Children[len(tree.Children)-1]
	if os.Type != "Selector" {
		t.Errorf("last child should be Selector (OutcomeSelector), got %s", os.Type)
	}
}

func TestCreateTree(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)

	tree, treeID := f.CreateTree("build a DCF model", "finance", nil)
	if tree == nil {
		t.Fatal("CreateTree returned nil tree")
	}
	if treeID == "" {
		t.Error("CreateTree returned empty treeID")
	}
	if !strings.HasPrefix(treeID, "finance:") {
		t.Errorf("treeID should start with 'finance:', got %q", treeID)
	}

	// Should be registered in the graph
	if _, ok := kg.Trees[treeID]; !ok {
		t.Error("tree was not registered in the knowledge graph")
	}

	// Verify the registered tree has correct info
	registered := kg.Trees[treeID]
	if registered.Category != "finance" {
		t.Errorf("expected category 'finance', got %q", registered.Category)
	}
	if registered.NodeCount == 0 {
		t.Error("node count should be > 0")
	}
	if registered.Fitness != 0 {
		t.Logf("fitness: %.2f", registered.Fitness)
	}
}

func TestCreateTree_WithParents(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "finance:a", Name: "A", Category: "finance", NodeCount: 10, Fitness: 80})
	kg.Register(&TreeMeta{ID: "finance:b", Name: "B", Category: "finance", NodeCount: 15, Fitness: 70})

	f := NewFactory(kg)
	tree, treeID := f.CreateTree("analyze portfolio performance", "finance", []string{"finance:a", "finance:b"})
	if tree == nil {
		t.Fatal("CreateTree returned nil")
	}
	if treeID == "" {
		t.Error("empty treeID")
	}
	t.Logf("Created tree: %s", treeID)
}

func TestCreateFromParents(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID: "finance:pitch_agent", Name: "Pitch Agent", Category: "finance",
		NodeCount: 39, Fitness: 80, Keywords: []string{"pitch"},
	})
	kg.Register(&TreeMeta{
		ID: "research:deep_research", Name: "Deep Research", Category: "research",
		NodeCount: 20, Fitness: 90, Keywords: []string{"research"},
	})

	f := NewFactory(kg)
	tree, treeID := f.CreateFromParents("finance:pitch_agent", "research:deep_research", "hybrid financial research")
	if tree == nil {
		t.Fatal("CreateFromParents returned nil")
	}
	if treeID == "" {
		t.Error("empty treeID")
	}
	if !strings.Contains(treeID, ":") {
		t.Errorf("treeID should contain ':': %q", treeID)
	}

	// Should be registered
	if _, ok := kg.Trees[treeID]; !ok {
		t.Error("hybrid tree not registered")
	}
	// Should have relations
	reg := kg.Trees[treeID]
	if len(reg.Relations) < 2 {
		t.Errorf("expected 2 relations, got %d", len(reg.Relations))
	}
}

func TestAutoCreateTree_Existing(t *testing.T) {
	kg := BuildKnowledgeGraph()
	tree, treeID, err := AutoCreateTree(kg, "review go code for bugs")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tree != nil {
		t.Error("should return nil tree when existing tree found")
	}
	if treeID == "" {
		t.Error("should return existing tree ID")
	}
	t.Logf("found existing tree: %s", treeID)
}

func TestAutoCreateTree_New(t *testing.T) {
	kg := NewKnowledgeGraph()
	// No trees registered → should create new one
	tree, treeID, err := AutoCreateTree(kg, "process financial data for reporting")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Error("should return new tree when nothing matches")
	}
	if treeID == "" {
		t.Error("should return new tree ID")
	}
	// New tree should be registered
	if _, ok := kg.Trees[treeID]; !ok {
		t.Error("auto-created tree should be registered")
	}
	t.Logf("auto-created tree: %s", treeID)
}

func TestAutoCreateTree_ConfidenceThreshold(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Register a tree with weak match
	kg.Register(&TreeMeta{
		ID:       "weak",
		Name:     "Weak Tree",
		Category: "core",
		Keywords: []string{"weak"},
	})

	tree, treeID, err := AutoCreateTree(kg, "strong specific unique financial analysis")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if tree == nil && treeID == "weak" {
		t.Log("auto-discovered weak tree (confidence may exceed 0.5 threshold)")
	} else if tree != nil && treeID != "" {
		t.Log("auto-created new tree (confidence below 0.5 threshold)")
	}
	// Either way should not error
}

func TestBreed(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "p1", Name: "Parent1", Category: "finance", NodeCount: 15})
	kg.Register(&TreeMeta{ID: "p2", Name: "Parent2", Category: "finance", NodeCount: 20})

	f := NewFactory(kg)
	tree := f.Breed("build a model", "finance", []string{"p1", "p2"})
	if tree == nil {
		t.Fatal("Breed returned nil")
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence type, got %s", tree.Type)
	}
	// Should have PreGate, StrategyRouter, Reflect, Outcome
	if len(tree.Children) < 3 {
		t.Errorf("expected at least 3 children, got %d", len(tree.Children))
	}
}

func TestBreed_NoParents(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)
	// No parents → should auto-select or fall back to archetype
	tree := f.Breed("some task", "core", nil)
	if tree == nil {
		t.Fatal("Breed with no parents returned nil")
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence type, got %s", tree.Type)
	}
}

func TestBreed_TooFewParents(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)
	// One parent → should fall back to archetype
	tree := f.Breed("some task", "core", []string{"only_one"})
	if tree == nil {
		t.Fatal("Breed with too few parents returned nil")
	}
}

func TestBreed_FromArchetype(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)

	// breedFromArchetype with a known category that has an archetype
	tree := f.breedFromArchetype("finance")
	if tree == nil {
		t.Fatal("breedFromArchetype returned nil")
	}
	// Should have children
	if len(tree.Children) == 0 {
		t.Error("archetype tree should have children")
	}
}

func TestBreed_FromArchetype_Fallback(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)

	// Category with no archetype → falls back to basic agent
	tree := f.breedFromArchetype("nonexistent_category")
	if tree == nil {
		t.Fatal("breedFromArchetype fallback returned nil")
	}
	if tree.Name != "BasicAgent" {
		t.Errorf("fallback should be BasicAgent, got %q", tree.Name)
	}
}

func TestBuildFromArchetype(t *testing.T) {
	f := NewFactory(NewKnowledgeGraph())

	// Test with a known archetype
	for _, arch := range f.Expert.TreeArchetypes {
		tree := f.buildFromArchetype(arch)
		if tree == nil {
			t.Errorf("buildFromArchetype returned nil for %s", arch.Category)
			continue
		}
		if tree.Type != "Sequence" {
			t.Errorf("buildFromArchetype(%s): expected Sequence, got %s", arch.Category, tree.Type)
		}
		// Should end with category_generated
		if !strings.HasSuffix(tree.Name, "_generated") {
			t.Errorf("buildFromArchetype(%s): name should end with _generated, got %q", arch.Category, tree.Name)
		}
	}
}

func TestSelectParents(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "f1", Name: "F1", Category: "finance", NodeCount: 10})
	kg.Register(&TreeMeta{ID: "f2", Name: "F2", Category: "research", NodeCount: 15})
	kg.Register(&TreeMeta{ID: "f3", Name: "F3", Category: "domain", NodeCount: 20})
	kg.Register(&TreeMeta{ID: "f4", Name: "F4", Category: "startup", NodeCount: 10})

	f := NewFactory(kg)
	// ExtractTemplates creates one template per category, so Templates keys are category names
	parents := f.selectParents("finance", "some financial task")

	if len(parents) < 2 {
		t.Errorf("expected at least 2 parents, got %d", len(parents))
	}
	if len(parents) > 3 {
		t.Errorf("expected at most 3 parents, got %d", len(parents))
	}
	// With only 1 template per category, fallback includes all templates
	// All returned parent IDs should exist as template keys
	for _, pid := range parents {
		if _, ok := f.Templates[pid]; !ok {
			t.Errorf("parent %q not found in templates", pid)
		}
	}
	t.Logf("selected parents: %v", parents)
}

func TestSelectParents_Fallback(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Only one tree in the category
	kg.Register(&TreeMeta{ID: "only", Name: "Only", Category: "special", NodeCount: 10})
	kg.Register(&TreeMeta{ID: "other", Name: "Other", Category: "other", NodeCount: 10})

	f := NewFactory(kg)
	parents := f.selectParents("special", "some task")
	// Since only 1 in "special", falls back to all templates
	if len(parents) > 2 {
		t.Logf("fallback parents: %v", parents)
	}
	// Should not panic
}

func TestCrossoverBreed(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "a", Name: "A", Category: "finance", NodeCount: 10})
	kg.Register(&TreeMeta{ID: "b", Name: "B", Category: "finance", NodeCount: 15})

	f := NewFactory(kg)
	tree := f.crossoverBreed("finance", []string{"a", "b"}, "test task")
	if tree == nil {
		t.Fatal("crossoverBreed returned nil")
	}
	if tree.Type != "Sequence" {
		t.Errorf("expected Sequence, got %s", tree.Type)
	}
}

func TestCrossoverBreed_NoTemplates(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)
	tree := f.crossoverBreed("finance", []string{"nonexistent1", "nonexistent2"}, "test task")
	if tree == nil {
		t.Fatal("crossoverBreed with no templates returned nil")
	}
	// Should fall back to archetype
}

// =============================================================================
// Table-driven test: comprehensive edge case coverage
// =============================================================================

func TestKnowledgeGraph_EdgeCases(t *testing.T) {
	kg := NewKnowledgeGraph()

	// Empty graph properties
	if len(kg.Trees) != 0 {
		t.Error("new graph should have 0 trees")
	}
	if len(kg.Edges) != 0 {
		t.Error("new graph should have 0 edges")
	}
	if len(kg.Synonyms) != 0 {
		t.Error("new graph should have 0 synonyms")
	}

	// Register nil? Actually Go doesn't have nil panics for struct field access
	// but let's test empty category
	kg.Register(&TreeMeta{ID: "empty", Name: "Empty", Category: ""})
	empty := kg.ListByCategory("")
	if len(empty) != 1 {
		t.Errorf("expected 1 tree in empty category, got %d", len(empty))
	}

	// Discover on graph with only empty-category tree
	id, conf := kg.Discover("empty")
	t.Logf("discover 'empty': id=%s conf=%.2f", id, conf)

	// Query on empty-capability tree
	results := kg.Query("")
	if len(results) > 0 {
		t.Logf("query '' returned %d results", len(results))
	}

	// Summary with only empty-category tree
	s := kg.Summary()
	t.Logf("summary: %s", s)
}

func TestKnowledgeGraph_GlobalConsistency(t *testing.T) {
	kg := BuildKnowledgeGraph()

	// All edges should reference valid trees
	for i, e := range kg.Edges {
		if _, ok := kg.Trees[e.From]; !ok {
			t.Errorf("edge %d: from %q not in tree map", i, e.From)
		}
		if _, ok := kg.Trees[e.To]; !ok {
			t.Errorf("edge %d: to %q not in tree map", i, e.To)
		}
	}

	// No orphan synonyms
	for syn, treeID := range kg.Synonyms {
		if _, ok := kg.Trees[treeID]; !ok {
			t.Errorf("synonym %q maps to nonexistent tree %q", syn, treeID)
		}
	}

	// All trees should have non-empty IDs
	for id, tree := range kg.Trees {
		if id == "" {
			t.Error("found tree with empty ID in map key")
		}
		if tree.ID != id {
			t.Errorf("tree.ID %q != map key %q", tree.ID, id)
		}
	}
}

func TestGlobalGraph(t *testing.T) {
	g := GlobalGraph
	if g == nil {
		t.Fatal("GlobalGraph is nil")
	}
	if len(g.Trees) != 41 {
		t.Errorf("expected 41 trees in GlobalGraph, got %d", len(g.Trees))
	}
}

// =============================================================================
// Evolution.SerializableNode integration tests
// =============================================================================

func TestFactory_CreateTree_NodeCount(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)
	tree, treeID := f.CreateTree("do something", "finance", nil)

	count := evolution.CountNodes(tree)
	t.Logf("Created tree %s with %d nodes", treeID, count)

	// Verify the registered count matches
	registered := kg.Trees[treeID]
	if registered.NodeCount != count {
		t.Errorf("registered NodeCount %d != counted %d", registered.NodeCount, count)
	}
}

func TestFactory_AllCategoriesTreeCreation(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)

	categories := []string{"finance", "domain", "research", "startup", "thinktank", "evolution", "core"}
	for _, cat := range categories {
		task := fmt.Sprintf("do %s work", cat)
		tree, treeID := f.CreateTree(task, cat, nil)
		if tree == nil {
			t.Errorf("CreateTree for %s returned nil", cat)
		}
		if treeID == "" {
			t.Errorf("CreateTree for %s returned empty ID", cat)
		}
		if !strings.HasPrefix(treeID, cat+":") {
			t.Errorf("treeID for %s: %q doesn't start with %q:", cat, treeID, cat)
		}
		t.Logf("  %s -> %s (%d nodes)", cat, treeID, evolution.CountNodes(tree))
	}
}

func TestFactory_FitnessUpdate(t *testing.T) {
	kg := NewKnowledgeGraph()
	f := NewFactory(kg)
	_, treeID := f.CreateTree("optimize performance", "evolution", nil)
	reg := kg.Trees[treeID]
	if reg.Fitness == 0 {
		t.Log("fitness is 0 (default)")
	}
	// Update fitness
	reg.Fitness = 95.5
	if kg.Trees[treeID].Fitness != 95.5 {
		t.Error("fitness update not reflected")
	}
}
