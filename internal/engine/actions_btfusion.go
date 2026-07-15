package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

const btFusionRepo = "/home/nico/go-bt-evolve"
const btFusionReport = "/mnt/ssd/clawd/wiki/bt-research/bt-fusion-latest.md"

// btFusionMaxNewNotes bounds how many newly discovered vault notes one cycle
// quotes in the report; further new notes are still recorded, just counted.
const btFusionMaxNewNotes = 5

// Knowledge-store seams (package vars for test override). The store is the
// persistent dedup index every research action consults before reporting:
// content already recorded by an earlier cycle is never re-reported as new.
var (
	btFusionKnowledgePath = research.DefaultPath()
	btFusionVaultDirs     = []string{goapFusionVaultDir, goapFusionSynthesesDir}
)

// fusionCodebaseFitCmd gathers codebase-fit evidence from git HEAD content
// (`git grep … HEAD`), never the working tree: the main repo is bare and its
// pre-conversion source files were removed 2026-07-03 — a filesystem grep of
// internal/domains/trees.go fails every bt-fusion run.
const fusionCodebaseFitCmd = `printf 'trees='; git grep -n '"bt_fusion"\|"hermes_update"\|"notebooklm_pipeline_monitor"' HEAD -- internal/domains/trees.go; printf '\nagents='; ls ~/.go-bt-evolve/agents/*fusion* ~/.go-bt-evolve/agents/*manager* 2>/dev/null; printf '\nservice='; systemctl --user show bt-agent.service -p ActiveState,SubState,Restart --no-pager 2>/dev/null`

// fusionCodebaseFitRun is the seam CheckCodebaseFit uses to run the
// diagnostic-only probe — a package var so tests can force a nonzero exit
// deterministically (the real command's exit code hinges on live daemon
// state that already exits 0 in dev sandboxes).
var fusionCodebaseFitRun = runFusionShell

func init() {
	registerBTFusionActions()
}

// nlmFusionResearchRun is the nlm invocation used by bt_fusion pattern
// research — a var so tests can stub the live NotebookLM call.
var nlmFusionResearchRun = nlmRun

// btFusionPatternQuestion derives the day's pattern-research question from
// the arc42 quality goals, rotating one goal per day. Empty when the goals
// are unavailable (the action then reports only its static seed findings).
func btFusionPatternQuestion() string {
	topics := arc42ResearchTopics()
	if len(topics) == 0 {
		return ""
	}
	return topics[time.Now().YearDay()%len(topics)]
}

// btFusionResearchFindings extracts bullet findings from a NotebookLM answer.
func btFusionResearchFindings(answer string) []string {
	var out []string
	for _, line := range strings.Split(answer, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") {
			t = strings.TrimSpace(t[2:])
		} else {
			continue
		}
		if len(t) >= 20 && len(out) < 8 {
			out = append(out, t)
		}
	}
	return out
}

func registerBTFusionActions() {
	RegisterAction("SearchForBTPatterns", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		findings := []string{
			"LLM-supervised BT evolution: use an LLM as meta-controller over GP-style mutations, but gate runtime/core edits behind review.",
			"Skill-library expansion: extract successful subtrees/actions into reusable domain trees and agent YAMLs.",
			"Telemetry-driven self-improvement: prioritize candidates from run history, trace latency, success rate, and failure modes.",
			"Typed-edge validation: preserve guard/effect/recovery/approval semantics when generating new trees.",
			"Checkpoint verification: generated trees should include deterministic postcondition checks before reporting success.",
		}
		// Goal-anchored live research: one arc42-derived question per day.
		// The static seeds above went stale after the first cycle recorded
		// them (383 consecutive "0 new findings" runs); the daily question
		// keeps new knowledge flowing while the nlm query cache collapses
		// this hourly action to at most one live call per day.
		if question := btFusionPatternQuestion(); question != "" {
			out := nlmFusionResearchRun(200*time.Second, "notebook", "query", "--json", "--timeout", "180", defaultNotebook, question)
			if !isGoapNotebookLMFailure(out) {
				findings = append(findings, btFusionResearchFindings(extractNotebookLMAnswer(out))...)
			}
		}
		store, err := research.Open(btFusionKnowledgePath)
		if err != nil {
			bb.Outcome = "fusion_knowledge_store_failed"
			bb.Result = "## BT Fusion Research Findings\n\nKnowledge store unavailable: " + err.Error()
			return -1
		}
		var fresh []string
		known := 0
		for _, f := range findings {
			if store.Record("bt_fusion:pattern", fusionTitle(f), f) {
				fresh = append(fresh, f)
			} else {
				known++
			}
		}
		if err := store.Save(); err != nil {
			bb.Outcome = "fusion_knowledge_store_failed"
			bb.Result = "## BT Fusion Research Findings\n\nFailed persisting knowledge store: " + err.Error()
			return -1
		}
		addFusionNewCount(bb, len(fresh))
		addFusionNewItems(bb, fresh)
		setFusionState(bb, "research_findings", strings.Join(fresh, "\n- "))
		bb.Result = "## BT Fusion Research Findings\n\n"
		if len(fresh) > 0 {
			bb.Result += "- " + strings.Join(fresh, "\n- ") + "\n\n"
		}
		bb.Result += fmt.Sprintf("%d new pattern finding(s); %d already recorded in the knowledge store (`%s`), not re-reported.", len(fresh), known, btFusionKnowledgePath)
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("QueryNotebookLMResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		store, err := research.Open(btFusionKnowledgePath)
		if err != nil {
			bb.Outcome = "fusion_knowledge_store_failed"
			bb.Result += "\n\n## Vault Research Delta\n\nKnowledge store unavailable: " + err.Error()
			return -1
		}
		notes := listFusionVaultNotes()
		var surfaced []string
		newCount, knownCount, skipped := 0, 0, 0
		for _, n := range notes {
			b, err := os.ReadFile(n.path)
			if err != nil {
				continue
			}
			content := string(b)
			if isFusionQuotaGarbage(content) {
				skipped++
				continue
			}
			if store.Record("vault:"+n.name, n.name, content) {
				newCount++
				if len(surfaced) < btFusionMaxNewNotes {
					surfaced = append(surfaced, fmt.Sprintf("**%s**: %s", n.name, truncateFusion(strings.Join(strings.Fields(content), " "), 400)))
				}
				addFusionNewItems(bb, []string{"vault note " + n.name})
			} else {
				knownCount++
			}
		}
		if err := store.Save(); err != nil {
			bb.Outcome = "fusion_knowledge_store_failed"
			bb.Result += "\n\n## Vault Research Delta\n\nFailed persisting knowledge store: " + err.Error()
			return -1
		}
		addFusionNewCount(bb, newCount)
		summary := fmt.Sprintf("%d new vault note(s) since the last cycle; %d already recorded, %d skipped as quota-error garbage.", newCount, knownCount, skipped)
		if len(surfaced) > 0 {
			summary += "\n\n- " + strings.Join(surfaced, "\n- ")
			if extra := newCount - len(surfaced); extra > 0 {
				summary += fmt.Sprintf("\n- …plus %d more new note(s) recorded in the knowledge store.", extra)
			}
		}
		setFusionState(bb, "notebooklm_context", summary)
		bb.Result += "\n\n## Vault Research Delta\n\n" + summary
		return 1
	})

	RegisterAction("SynthesizeFindings", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var synthesis string
		if n := fusionNewCount(bb); n == 0 {
			synthesis = fmt.Sprintf("No new research this cycle: every candidate finding and vault note is already recorded in the knowledge store (`%s`). The duplicate fusion report will be skipped.", btFusionKnowledgePath)
		} else {
			items, _ := bb.ChainState["bt_fusion_research_new_items"].(string)
			synthesis = fmt.Sprintf("%d new knowledge entr%s this cycle to evaluate for fusion targets:\n%s", n, pluralYIes(n), items)
		}
		setFusionState(bb, "synthesis", synthesis)
		bb.Result += "\n\n## Synthesis\n\n" + synthesis
		return 1
	})

	// Routing: a cycle that produced zero new knowledge must not rewrite and
	// re-broadcast the previous report — it reports the no-op briefly instead.
	RegisterCondition("HasNewResearch", func(bb *Blackboard) bool { return fusionNewCount(bb) > 0 })
	RegisterCondition("NoNewResearch", func(bb *Blackboard) bool { return fusionNewCount(bb) == 0 })

	RegisterAction("ReportNoNewResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		entries := "?"
		if store, err := research.Open(btFusionKnowledgePath); err == nil {
			entries = strconv.Itoa(store.Len())
		}
		bb.Result += fmt.Sprintf("\n\n## No New Research\n\nAll findings this cycle were already recorded in the research knowledge store (%s entries at `%s`). Skipped the duplicate fusion report and verification.", entries, btFusionKnowledgePath)
		bb.Outcome = string(evolution.Success)
		// Honest signal: zero new knowledge is a healthy no-op, not a full
		// success — refine to the SLO-deferred no_change state with the same
		// authoritative quality the goap analysis-only path stamps, so the
		// notification throttle and stats can tell a quiet cycle from real work.
		bb.OutcomeRefinement = "no_change"
		bb.QualityScore = 0.5
		bb.QualityAuthoritative = true
		return 1
	})

	RegisterAction("CheckCodebaseFit", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, code := fusionCodebaseFitRun(fusionCodebaseFitCmd)
		setFusionState(bb, "codebase_fit", out)
		bb.Result += fmt.Sprintf("\n\n## Codebase Fit Evidence (exit=%d)\n\n```\n%s\n```", code, truncateFusion(out, 2500))
		// Diagnostic-only evidence gathering: the probe's exit code hinges on
		// live external state (systemctl daemon status, agent YAML presence)
		// this action doesn't control. A nonzero exit is logged but must not
		// hard-fail the whole bt_fusion cycle.
		if code != 0 {
			Warn("bt fusion: codebase-fit probe exited nonzero (best-effort evidence, continuing)", "exit_code", code)
		}
		return 1
	})

	RegisterAction("AssessFusionComplexity", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		assessment := `Complexity assessment:
- Deterministic BT Fusion actions: DONE / low risk / additive engine action file.
- --no-mcp daemon stability: DONE / medium risk / main.go signal wait path.
- Gardener pool expansion to 36 trees: medium risk / requires gardener config/state migration.
- Typed-edge generator for new trees: medium risk / should be added as a new domain-tree template first.
- Runtime interface changes: high risk / HITL required.`
		setFusionState(bb, "complexity", assessment)
		bb.Result += "\n\n## Complexity\n\n" + assessment
		return 1
	})

	RegisterAction("PrioritizeFusionTargets", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		priorities := `Priority order:
1. Stabilize bt-agent daemon in --no-mcp mode (required for scheduled agents).
2. Replace generic/no-op BT Fusion actions with deterministic repo-grounded actions.
3. Persist BT Fusion report to vault for compounding research memory.
4. Run domain tests + build every fusion cycle.
5. Next cycle: expand gardener pool from 32 to all 36 registered domain trees.`
		setFusionState(bb, "priorities", priorities)
		bb.Result += "\n\n## Priorities\n\n" + priorities
		return 1
	})

	RegisterAction("ApplyFusion", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		_ = os.MkdirAll(filepath.Dir(btFusionReport), 0755)
		report := fusionMarkdown(bb)
		if err := os.WriteFile(btFusionReport, []byte(report), 0644); err != nil {
			bb.Outcome = "fusion_write_failed"
			bb.Result += "\n\n## ApplyFusion\n\nFailed writing report: " + err.Error()
			return -1
		}
		bb.Result += "\n\n## ApplyFusion\n\nWrote fusion report: `" + btFusionReport + "`"
		return 1
	})

	RegisterAction("VerifyFusionBuild", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		// A bare main repo has no working tree to build: bt-fusion only wrote
		// a vault report, on-disk source is gone, VCS stamping dies on git
		// status — and `go build -o bt-agent` here would overwrite the LIVE
		// daemon binary in place. Same delegation contract as VerifyGoapBuild.
		if out, err := runGoapShell("git rev-parse --is-bare-repository"); err == nil && strings.TrimSpace(out) == "true" {
			bb.Result += "\n\n## Verification Delegated\n\nMain repo is bare (no working tree to build); bt-fusion changed no repo source. Skipping stale-tree build."
			setFusionState(bb, "verification", "delegated (bare main repo)")
			return 1
		}
		testOut, testCode := runFusionShell("/usr/local/go/bin/go test ./internal/domains/ -run TestAllDomainTrees -count=1 -timeout 180s")
		buildOut, buildCode := runFusionShell("/usr/local/go/bin/go build -o bt-agent ./cmd/bt-agent")
		verification := fmt.Sprintf("go test exit=%d\n%s\n\ngo build exit=%d\n%s", testCode, testOut, buildCode, buildOut)
		setFusionState(bb, "verification", verification)
		bb.Result += fmt.Sprintf("\n\n## Verification\n\n```\n%s\n```", truncateFusion(verification, 3000))
		if testCode != 0 || buildCode != 0 {
			bb.Outcome = "fusion_verify_failed"
			return -1
		}
		return 1
	})

	RegisterAction("ReportFusionStatus", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Outcome = string(evolution.Success)
		bb.Result += "\n\n## Fusion Status\n\nApplied deterministic BT Fusion reporting and verification path. Next target: include all 36 registered domain trees in the gardener/evolution pool and add checkpoint gates to generated trees."
		return 1
	})
}

func setFusionState(bb *Blackboard, key, value string) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["bt_fusion_"+key] = value
}

// fusionNewCount reads this cycle's accumulated count of new knowledge
// entries. Stored as a string: ChainState round-trips through JSON, which
// would silently turn ints into float64.
func fusionNewCount(bb *Blackboard) int {
	v, _ := bb.ChainState["bt_fusion_research_new_count"].(string)
	n, _ := strconv.Atoi(v)
	return n
}

func addFusionNewCount(bb *Blackboard, delta int) {
	setFusionState(bb, "research_new_count", strconv.Itoa(fusionNewCount(bb)+delta))
}

func addFusionNewItems(bb *Blackboard, items []string) {
	if len(items) == 0 {
		return
	}
	existing, _ := bb.ChainState["bt_fusion_research_new_items"].(string)
	for _, it := range items {
		existing += "- " + fusionTitle(it) + "\n"
	}
	setFusionState(bb, "research_new_items", existing)
}

// fusionTitle derives a short stable title from a finding's leading words.
func fusionTitle(finding string) string {
	words := strings.Fields(finding)
	if len(words) > 10 {
		words = words[:10]
	}
	return strings.Join(words, " ")
}

type fusionVaultNote struct {
	path string
	name string
	mod  time.Time
}

// listFusionVaultNotes returns the vault's markdown notes newest-first,
// excluding bt_fusion's own report so it never feeds back into research.
func listFusionVaultNotes() []fusionVaultNote {
	ownReport := filepath.Base(btFusionReport)
	var notes []fusionVaultNote
	for _, dir := range btFusionVaultDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == ownReport {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			notes = append(notes, fusionVaultNote{path: filepath.Join(dir, e.Name()), name: e.Name(), mod: info.ModTime()})
		}
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].mod.After(notes[j].mod) })
	return notes
}

// isFusionQuotaGarbage detects vault syntheses that captured NotebookLM
// quota-error output instead of research (the pre-bd8c5b6 failure mode).
func isFusionQuotaGarbage(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "error code 8") ||
		strings.Contains(lower, "google rejected the query")
}

func pluralYIes(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func runFusionShell(command string) (string, int) {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = btFusionRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return string(out), exit.ExitCode()
		}
		return string(out) + "\n" + err.Error(), 1
	}
	return string(out), 0
}

func fusionMarkdown(bb *Blackboard) string {
	// Strip injected blackboard context from both the task and the result.
	// The blackboard seeder appends a standardized hint to bb.Task;
	// the report should show the clean task and result only.
	result := bb.Result
	const fusionHeader = "## BT Fusion Research Findings"
	if idx := strings.Index(result, fusionHeader); idx >= 0 {
		result = result[idx:]
	}
	task := bb.Task
	if idx := strings.Index(task, "\n\nBLACKBOARD CONTEXT"); idx >= 0 {
		task = task[:idx]
	}
	return fmt.Sprintf(`# BT Fusion Report

Generated: %s
Task: %s

%s
`, time.Now().Format(time.RFC3339), task, result)
}

func truncateFusion(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...<truncated>"
}
