package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

func withNlmEconomy(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	oldCache, oldUsage, oldStore := nlmQueryCachePath, nlmUsagePath, btFusionKnowledgePath
	nlmQueryCachePath = filepath.Join(dir, "cache.json")
	nlmUsagePath = filepath.Join(dir, "usage.json")
	btFusionKnowledgePath = filepath.Join(dir, "knowledge.json")
	t.Cleanup(func() {
		nlmQueryCachePath, nlmUsagePath, btFusionKnowledgePath = oldCache, oldUsage, oldStore
	})
}

func TestNlmExtractQueryTolratesFlagsAndConversation(t *testing.T) {
	q, ok := nlmExtractQuery([]string{"notebook", "query", "--json", "--timeout", "180", "nb-1", "what is missing?", "--conversation-id", "c9"})
	if !ok || q != "what is missing?" {
		t.Fatalf("got %q ok=%v", q, ok)
	}
	q, ok = nlmExtractQuery([]string{"notebook", "query", "nb-1", "plain question"})
	if !ok || q != "plain question" {
		t.Fatalf("plain form: got %q ok=%v", q, ok)
	}
	if _, ok := nlmExtractQuery([]string{"notebook", "get", "nb-1"}); ok {
		t.Fatal("non-query commands must not match")
	}
}

func TestNlmQueryCacheHitSkipsLiveCallAndIgnoresConversationID(t *testing.T) {
	withNlmEconomy(t)
	args1 := []string{"notebook", "query", "--json", "nb-1", "same question", "--conversation-id", "c1"}
	nlmPostflight(args1, `{"answer":"the answer body"}`)

	// Same question, different conversation id → still a hit.
	args2 := []string{"notebook", "query", "nb-1", "same question", "--conversation-id", "c2"}
	cached, deny, proceed := nlmPreflight(args2)
	if proceed || deny != "" {
		t.Fatalf("expected cache hit, got proceed=%v deny=%q", proceed, deny)
	}
	if !strings.Contains(cached, "the answer body") {
		t.Fatalf("cached answer must be the raw output: %q", cached)
	}

	// A different question misses.
	if _, _, proceed := nlmPreflight([]string{"notebook", "query", "nb-1", "different question"}); !proceed {
		t.Fatal("different question must proceed live")
	}
}

func TestNlmQueryBudgetRefusesOverCap(t *testing.T) {
	withNlmEconomy(t)
	t.Setenv("BT_NLM_QUERY_BUDGET", "1")
	nlmPostflight([]string{"notebook", "query", "nb-1", "q1"}, "answer1")
	_, deny, proceed := nlmPreflight([]string{"notebook", "query", "nb-1", "q2"})
	if proceed || deny == "" {
		t.Fatalf("over-budget query must be refused, got proceed=%v", proceed)
	}
	if !strings.HasPrefix(deny, "Error:") || !isGoapNotebookLMFailure(deny) {
		t.Fatalf("denial must classify as an nlm failure for fallbacks: %q", deny)
	}
	// The already-cached q1 must still be served despite the exhausted budget.
	cached, _, proceed := nlmPreflight([]string{"notebook", "query", "nb-1", "q1"})
	if proceed || cached != "answer1" {
		t.Fatalf("cache hits must survive budget exhaustion, got %q proceed=%v", cached, proceed)
	}
}

func TestNlmResearchBudgetRefusesOverCap(t *testing.T) {
	withNlmEconomy(t)
	t.Setenv("BT_NLM_RESEARCH_BUDGET", "1")
	nlmPostflight([]string{"research", "start", "topic", "--notebook-id", "nb-1"}, "task_id: abc")
	_, deny, proceed := nlmPreflight([]string{"research", "start", "another topic", "--notebook-id", "nb-1"})
	if proceed || deny == "" {
		t.Fatal("second research start must be refused at cap 1")
	}
	// status/import of an in-flight research task are never budget-gated.
	if _, _, proceed := nlmPreflight([]string{"research", "status", "nb-1", "--task-id", "abc"}); !proceed {
		t.Fatal("research status must not be budget-gated")
	}
}

func TestNlmPostflightRecordsAnswerInKnowledgeStore(t *testing.T) {
	withNlmEconomy(t)
	nlmPostflight([]string{"notebook", "query", "nb-1", "what about typed edges?"}, `{"answer":"typed edges need guards"}`)
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Known("typed edges need guards") {
		t.Fatal("live answers must be retained in the research knowledge store")
	}
}

func TestNlmPostflightIgnoresFailures(t *testing.T) {
	withNlmEconomy(t)
	nlmPostflight([]string{"notebook", "query", "nb-1", "q"}, "Error: RESOURCE_EXHAUSTED")
	if u := loadNlmUsage(); u.Queries != 0 {
		t.Fatalf("failed calls must not consume budget, used=%d", u.Queries)
	}
	if _, _, proceed := nlmPreflight([]string{"notebook", "query", "nb-1", "q"}); !proceed {
		t.Fatal("failures must not be cached")
	}
}

func TestNlmCacheExpiresAcrossPacificDays(t *testing.T) {
	withNlmEconomy(t)
	cache := loadNlmQueryCache()
	cache.Entries[research.Key("old question")] = nlmQueryCacheEntry{
		Question: "old question", Answer: "stale", Day: "2020-01-01", At: time.Now().UTC().Format(time.RFC3339),
	}
	// Save prunes other-day entries; a stale-day entry also misses on read.
	if _, _, proceed := nlmPreflight([]string{"notebook", "query", "nb-1", "old question"}); !proceed {
		t.Fatal("previous-day cache entries must not hit")
	}
}

func TestDeriveNotebookLMResearchQuery(t *testing.T) {
	withNlmEconomy(t)
	boiler := "Production NotebookLM researcher — domain:notebooklm tree with deterministic nlm CLI stubs + anti-fabrication evidence gate. Research → import sources → query with citations → save to vault. Runs every 2 hours to keep research fresh."
	got := deriveNotebookLMResearchQuery(boiler)
	if strings.Contains(got, "domain:") || strings.Contains(got, "anti-fabrication") {
		t.Fatalf("boilerplate must be replaced by a real topic: %q", got)
	}
	if passthru := deriveNotebookLMResearchQuery("auction protocols for task allocation"); passthru != "auction protocols for task allocation" {
		t.Fatalf("genuine questions must pass through: %q", passthru)
	}

	// With an active program, its next milestone drives the research.
	dir := t.TempDir()
	oldP := goapProgramsPath
	goapProgramsPath = filepath.Join(dir, "programs.json")
	t.Cleanup(func() { goapProgramsPath = oldP })
	ps, _ := research.OpenPrograms(goapProgramsPath)
	ps.Add("Auction allocation", "test", []string{"Define auction messages in internal/a2a/auction.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	got = deriveNotebookLMResearchQuery(boiler)
	if !strings.Contains(got, "auction messages") {
		t.Fatalf("active program milestone must drive derived research: %q", got)
	}
	_ = os.Remove(goapProgramsPath)
}
