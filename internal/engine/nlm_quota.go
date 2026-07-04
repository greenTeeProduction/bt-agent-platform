package engine

// NotebookLM quota economy: the free plan allows ~50 metered calls per
// Pacific day, but scheduled demand (grill 2×/hour, research query 2×/hour,
// the researcher's 5-call web-research pipeline every 2 hours) exceeded 150 —
// the quota exhausted by mid-morning every day and 1,100+ vault syntheses
// show the same questions asked again and again. Two mechanisms fix this at
// the nlmRun choke point:
//
//  1. Query cache: `notebook query` answers are cached for a Pacific day,
//     keyed by the question's content hash (conversation ids ignored — the
//     grill's three static round questions were burning ~48 calls/day for
//     three distinct answers). A cache hit returns the identical raw output
//     so downstream parsers behave exactly as on a live call.
//  2. Daily budget: metered ops (query cache-misses and `research start`)
//     count against per-day caps persisted under ADR-003; over budget the
//     call is refused with an "Error: …" string the existing failure
//     classifiers treat like any other nlm failure, so Claude fallbacks
//     engage instead of burning the tail of the quota.
//
// Every live answer is also recorded into the research knowledge store so
// the corpus of NotebookLM responses survives beyond the vault files.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

var (
	nlmQueryCachePath = defaultNlmEconomyPath("nlm-query-cache.json")
	nlmUsagePath      = defaultNlmEconomyPath("nlm-usage.json")
)

// Default budgets; env-overridable (BT_NLM_QUERY_BUDGET / BT_NLM_RESEARCH_BUDGET).
const (
	defaultNlmQueryBudget    = 30
	defaultNlmResearchBudget = 2
)

func defaultNlmEconomyPath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/home/nico"
	}
	return filepath.Join(home, ".go-bt-evolve", "research", name)
}

// nlmPacificDay is the quota accounting day: Google daily quotas reset at
// midnight America/Los_Angeles.
func nlmPacificDay(now time.Time) string {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return now.UTC().Format("2006-01-02")
	}
	return now.In(loc).Format("2006-01-02")
}

type nlmQueryCacheEntry struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
	Day      string `json:"day"`
	At       string `json:"at"`
}

type nlmQueryCache struct {
	Entries map[string]nlmQueryCacheEntry `json:"entries"`
}

type nlmUsage struct {
	Day      string `json:"day"`
	Queries  int    `json:"queries"`
	Research int    `json:"research"`
}

func loadNlmQueryCache() nlmQueryCache {
	c := nlmQueryCache{Entries: map[string]nlmQueryCacheEntry{}}
	if b, err := os.ReadFile(nlmQueryCachePath); err == nil {
		_ = json.Unmarshal(b, &c)
		if c.Entries == nil {
			c.Entries = map[string]nlmQueryCacheEntry{}
		}
	}
	return c
}

func saveNlmQueryCache(c nlmQueryCache) {
	// Drop entries from previous Pacific days: answers may go stale as the
	// notebook grows, and each new day has fresh budget to re-ask.
	day := nlmPacificDay(time.Now())
	for k, e := range c.Entries {
		if e.Day != day {
			delete(c.Entries, k)
		}
	}
	if err := os.MkdirAll(filepath.Dir(nlmQueryCachePath), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	tmp := nlmQueryCachePath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, nlmQueryCachePath)
	}
}

func loadNlmUsage() nlmUsage {
	var u nlmUsage
	if b, err := os.ReadFile(nlmUsagePath); err == nil {
		_ = json.Unmarshal(b, &u)
	}
	if day := nlmPacificDay(time.Now()); u.Day != day {
		u = nlmUsage{Day: day}
	}
	return u
}

func saveNlmUsage(u nlmUsage) {
	if err := os.MkdirAll(filepath.Dir(nlmUsagePath), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return
	}
	tmp := nlmUsagePath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, nlmUsagePath)
	}
}

func nlmBudgetFor(kind string) int {
	env, def := "BT_NLM_QUERY_BUDGET", defaultNlmQueryBudget
	if kind == "research" {
		env, def = "BT_NLM_RESEARCH_BUDGET", defaultNlmResearchBudget
	}
	if raw := os.Getenv(env); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

// nlmExtractQuery pulls the question text out of a `notebook query` arg list,
// tolerating flags in any position ("notebook query --json --timeout 180 <nb>
// <question> [--conversation-id X]"): the question is the last positional.
func nlmExtractQuery(args []string) (string, bool) {
	if len(args) < 2 || args[0] != "notebook" || args[1] != "query" {
		return "", false
	}
	flagsWithValue := map[string]bool{"--timeout": true, "--conversation-id": true, "--profile": true}
	var positionals []string
	for i := 2; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			if flagsWithValue[a] && i+1 < len(args) {
				i++
			}
			continue
		}
		positionals = append(positionals, a)
	}
	if len(positionals) < 2 {
		return "", false
	}
	return positionals[len(positionals)-1], true
}

func nlmIsResearchStart(args []string) bool {
	return len(args) >= 2 && args[0] == "research" && args[1] == "start"
}

// nlmPreflight decides how a metered nlm call proceeds:
// cached answer (hit), budget refusal (deny non-empty), or live (proceed).
func nlmPreflight(args []string) (cached string, deny string, proceed bool) {
	if q, ok := nlmExtractQuery(args); ok {
		key := research.Key(q)
		cache := loadNlmQueryCache()
		if e, hit := cache.Entries[key]; hit && e.Day == nlmPacificDay(time.Now()) {
			return e.Answer, "", false
		}
		u := loadNlmUsage()
		if budget := nlmBudgetFor("query"); u.Queries >= budget {
			return "", fmt.Sprintf("Error: nlm daily query budget exhausted (local cap %d, used %d) — falling back; budget resets midnight Pacific", budget, u.Queries), false
		}
		return "", "", true
	}
	if nlmIsResearchStart(args) {
		u := loadNlmUsage()
		if budget := nlmBudgetFor("research"); u.Research >= budget {
			return "", fmt.Sprintf("Error: nlm daily research budget exhausted (local cap %d, used %d) — skipping web research; budget resets midnight Pacific", budget, u.Research), false
		}
	}
	return "", "", true
}

// nlmPostflight records a successful live call: budget consumption, the
// query cache entry, and knowledge-store retention of the answer.
func nlmPostflight(args []string, out string) {
	if isGoapNotebookLMFailure(out) {
		return
	}
	if q, ok := nlmExtractQuery(args); ok {
		u := loadNlmUsage()
		u.Queries++
		saveNlmUsage(u)
		cache := loadNlmQueryCache()
		now := time.Now()
		cache.Entries[research.Key(q)] = nlmQueryCacheEntry{
			Question: q,
			Answer:   out,
			Day:      nlmPacificDay(now),
			At:       now.UTC().Format(time.RFC3339),
		}
		saveNlmQueryCache(cache)
		if store, err := research.Open(btFusionKnowledgePath); err == nil {
			title := q
			if len(title) > 100 {
				title = title[:100]
			}
			store.Record("nlm:answer", title, extractNotebookLMAnswer(out))
			_ = store.Save()
		}
		return
	}
	if nlmIsResearchStart(args) {
		u := loadNlmUsage()
		u.Research++
		saveNlmUsage(u)
	}
}

// deriveNotebookLMResearchQuery turns the scheduled researcher's task into a
// genuine research question. Agent boilerplate (the agent's own description,
// which the scheduler passes when no input is set) is replaced by, in order:
// the active program's next milestone, the newest unimplemented research
// goal, or a rotating platform topic — so web research always serves the
// platform's actual needs.
func deriveNotebookLMResearchQuery(task string) string {
	t := strings.TrimSpace(task)
	boilerplate := t == "" || len(t) > 220 ||
		strings.Contains(t, "domain:") || strings.Contains(t, "Runs every") ||
		strings.Contains(strings.ToLower(t), "anti-fabrication")
	if !boilerplate {
		return t
	}
	if ps, err := research.OpenPrograms(goapProgramsPath); err == nil {
		if p := ps.Active(); p != nil {
			if _, m := p.NextMilestone(); m != nil {
				return fmt.Sprintf("State of the art and reference implementations for: %s (context: %s)", m.Goal, p.Title)
			}
		}
	}
	topics := []string{
		"auction-based and market-based task allocation protocols for multi-agent systems",
		"behavior tree evolution and genetic programming for agent policies: recent methods",
		"skill libraries and failure-to-skill pipelines for continual agent learning",
		"LLM-driven code generation of executable behavior trees: validation and typing",
		"self-improving agent loops: progress metrics, circuit breakers, and safety gates",
		"multi-agent coordination benchmarks and evaluation for heterogeneous agent fleets",
	}
	return topics[time.Now().YearDay()%len(topics)]
}
