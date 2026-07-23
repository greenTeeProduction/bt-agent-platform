// Package engine — NotebookLM zero-LLM action nodes.
// These directly exec the nlm CLI via nlmRun(), producing real output
// without any LLM call. The tree becomes deterministic and anti-fabrication
// by design — no ChainAction/agent nodes needed.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerNotebookLMActions()
}

// nlmAuthRun is the nlm invocation used by auth actions — a var so tests can
// script check/login/re-check sequences without the real CLI.
var nlmAuthRun = nlmRun

// nlmResearchSynthesesDir is where ResearchNotebookLM writes its per-run
// synthesis report; a var so tests never touch the live research vault.
var nlmResearchSynthesesDir = "/mnt/ssd/clawd/wiki/bt-research/syntheses"

// nlmResearchQuerySeenWindow is the novelty-gate recency window: the same
// research query re-derived within it (the 2-hour scheduled cadence hitting
// one topic 4×/day, 2026-07-23 review gap 7) is skipped instead of burning
// web-research quota on a near-identical synthesis. Just under a day, so a
// topic legitimately re-arms on its next daily rotation.
const nlmResearchQuerySeenWindow = 20 * time.Hour

// nlmResearchQueryKeyContent is the knowledge-store content identity of one
// research query — distinct from the report content Record'ed after a run,
// so gating never depends on report wording.
func nlmResearchQueryKeyContent(query string) string {
	return "nlm research query: " + query
}

// nlmResearchQueryRecentlySeen reports whether query completed a research run
// within the novelty-gate window.
func nlmResearchQueryRecentlySeen(query string) bool {
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		return false
	}
	e, ok := store.Entries[research.Key(nlmResearchQueryKeyContent(query))]
	return ok && nlmResearchNowFn().UTC().Sub(e.LastSeen) < nlmResearchQuerySeenWindow
}

// nlmMarkResearchQueryDone records a completed research run's query, arming
// the novelty gate for the window. Best-effort: a store failure only costs a
// duplicate run later.
func nlmMarkResearchQueryDone(query string) {
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		return
	}
	store.Record("nlm:research-query", query, nlmResearchQueryKeyContent(query))
	_ = store.Save()
}

// nlmAuthNeedsRefresh reports whether an auth-check output warrants an
// `nlm login` attempt. "expired" matters: a stored Chrome profile often
// re-authenticates non-interactively, and failing without trying left the
// notebooklm-researcher dead-lettering every window (2026-07-03).
func nlmAuthNeedsRefresh(out string) bool {
	for _, marker := range []string{"stale", "not_configured", "expired", "failed", "error"} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}

// nlmAuthUnhealthy reports whether an auth-check output is a hard failure.
func nlmAuthUnhealthy(out string) bool {
	for _, marker := range []string{"expired", "failed", "error", "not_configured"} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}

func registerNotebookLMActions() {
	// CheckNotebookLMAuth — runs nlm login --check and reports status.
	RegisterAction("CheckNotebookLMAuth", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out := nlmRun(30*time.Second, "login", "--check")
		bb.Result = "## NotebookLM Auth\n\n```\n" + out + "\n```\n"
		if strings.Contains(out, "not_configured") || strings.Contains(out, "stale") || strings.Contains(out, "error") {
			bb.Result += "\n⚠ Auth issue detected."
			bb.Outcome = "failure"
			return -1
		}
		bb.Outcome = "success"
		return 1
	})

	// ListNotebookLMNotebooks — runs nlm notebook list --json.
	RegisterAction("ListNotebookLMNotebooks", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out := nlmRun(30*time.Second, "notebook", "list", "--json")
		bb.Result = "## NotebookLM Notebooks\n\n```json\n" + out + "\n```\n"
		bb.ChainState["nlm_notebook_list"] = out
		bb.Outcome = "success"
		return 1
	})

	// GetNotebookLMNotebook — runs nlm notebook get <id> --json.
	// Uses default notebook 463ca402-... unless task specifies another.
	RegisterAction("GetNotebookLMNotebook", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		nbID := defaultNotebook
		out := nlmRun(30*time.Second, "notebook", "get", nbID, "--json")
		bb.Result = "## BT Platform Research Notebook\n\n```json\n" + out + "\n```\n"
		bb.ChainState["nlm_notebook_get"] = out
		bb.ChainState["nlm_notebook_id"] = nbID
		bb.Outcome = "success"
		return 1
	})

	// ResearchNotebookLM — runs the full research pipeline:
	//   research start → poll status → import sources → get notebook → save to vault.
	RegisterAction("ResearchNotebookLM", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		nbID := defaultNotebook
		var report strings.Builder
		report.WriteString("## NotebookLM Research\n\n")

		// Extract research query from task; agent boilerplate is replaced by
		// a derived platform question (the scheduled researcher's task is its
		// own description — it spent weeks web-researching the sentence
		// "Production NotebookLM researcher — domain:notebooklm tree…" and
		// importing the junk results as notebook sources).
		query := deriveNotebookLMResearchQuery(bb.Task)
		fmt.Fprintf(&report, "**Query:** %s\n\n", query)

		// Novelty gate (2026-07-23 review gap 7): a query researched within
		// the recency window burns web-research quota to produce a
		// near-identical synthesis — the notebook already holds this run's
		// sources and the knowledge store its findings. Skip healthily
		// BEFORE any nlm invocation.
		if nlmResearchQueryRecentlySeen(query) {
			bb.Result = fmt.Sprintf("## NotebookLM Research Skipped (novelty gate)\n\n**Query:** %s\n\nAlready researched within the last %s — web-research quota preserved.", query, nlmResearchQuerySeenWindow)
			bb.Outcome = "no_change"
			return 1
		}

		// Step 1: Get current notebook state
		beforeOut := nlmRun(30*time.Second, "notebook", "get", nbID, "--json")
		report.WriteString("### Before\n```json\n" + beforeOut + "\n```\n\n")

		// Step 2: Start research
		researchOut := nlmRun(60*time.Second,
			"research", "start", query,
			"--notebook-id", nbID,
			"--mode", "fast",
			"--source", "web",
		)
		report.WriteString("### Research Started\n```\n" + researchOut + "\n```\n\n")

		// Extract task_id from research output
		taskID := extractTaskID(researchOut)
		if taskID == "" {
			bb.Result = report.String() + "\n⚠ Could not extract task_id from research output."
			bb.Outcome = "failure"
			return -1
		}
		fmt.Fprintf(&report, "**Task ID:** `%s`\n\n", taskID)

		// Step 3: Poll research status (with longer timeout)
		statusOut := nlmRun(360*time.Second,
			"research", "status", nbID,
			"--task-id", taskID,
			"--compact",
			"--max-wait", "300",
		)
		report.WriteString("### Research Status\n```\n" + statusOut + "\n```\n\n")

		// Step 4: Import sources (cited only if available, otherwise all)
		importArgs := []string{"research", "import", nbID, taskID}
		if strings.Contains(statusOut, "cited") {
			importArgs = append(importArgs, "--cited-only")
		}
		importOut := nlmRun(300*time.Second, importArgs...)
		report.WriteString("### Import\n```\n" + importOut + "\n```\n\n")

		// Step 5: Get after state
		afterOut := nlmRun(30*time.Second, "notebook", "get", nbID, "--json")
		report.WriteString("### After\n```json\n" + afterOut + "\n```\n\n")

		// Step 6: Save to vault — timestamped: the old per-day filename made
		// every run overwrite the previous run's results.
		dateStr := time.Now().Format("2006-01-02T150405")
		savePath := fmt.Sprintf("%s/nlm-research-%s.md", nlmResearchSynthesesDir, dateStr)
		saveErr := writeString(savePath, report.String())
		if saveErr != nil {
			fmt.Fprintf(&report, "⚠ Save error: %v\n", saveErr)
		} else {
			fmt.Fprintf(&report, "✅ Saved to `%s`\n", savePath)
		}

		// Retain in the research knowledge store so the result survives even
		// if the vault file is pruned, and so future cycles see it as known.
		if store, serr := research.Open(btFusionKnowledgePath); serr == nil {
			store.Record("nlm:research", query, report.String())
			_ = store.Save()
		}
		// Arm the novelty gate: this query is done for the window.
		nlmMarkResearchQueryDone(query)

		bb.Result = report.String()
		bb.ChainState["nlm_task_id"] = taskID
		bb.ChainState["nlm_save_path"] = savePath
		bb.Outcome = "success"
		return 1
	})

	// QueryNotebookLM — runs nlm notebook query <id> <question>.
	RegisterAction("QueryNotebookLM", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		nbID := defaultNotebook
		out := nlmRun(180*time.Second, "notebook", "query", nbID, bb.Task)
		bb.Result = "## NotebookLM Query\n\n" + out + "\n"
		bb.Outcome = "success"
		return 1
	})

	// SaveNotebookLMFindings — writes the accumulated results to vault.
	RegisterAction("SaveNotebookLMFindings", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		dateStr := time.Now().Format("2006-01-02")
		savePath := fmt.Sprintf("/mnt/ssd/clawd/wiki/bt-research/syntheses/nlm-findings-%s.md", dateStr)
		content := fmt.Sprintf("# NotebookLM Findings — %s\n\n## Task\n%s\n\n## Results\n%s\n",
			dateStr, bb.Task, bb.Result)
		saveErr := writeString(savePath, content)
		if saveErr != nil {
			bb.Result += fmt.Sprintf("\n⚠ Save error: %v\n", saveErr)
			bb.Outcome = "failure"
			return -1
		}
		bb.Result += fmt.Sprintf("\n\n✅ Saved to `%s`\n", savePath)
		bb.ChainState["nlm_save_path"] = savePath
		bb.Outcome = "success"
		return 1
	})

	// CheckNotebookLMAuthAndRefresh — runs auth check; on any unhealthy
	// status (stale, not_configured, expired, failed) attempts `nlm login`
	// and re-checks. The verdict comes from the RE-CHECK only — grepping the
	// concatenated transcript let the original failure text poison a
	// successful refresh.
	RegisterAction("CheckNotebookLMAuthAndRefresh", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		check := nlmAuthRun(30*time.Second, "login", "--check")
		status := check
		detail := check
		if nlmAuthNeedsRefresh(check) {
			refresh := nlmAuthRun(120*time.Second, "login")
			recheck := nlmAuthRun(30*time.Second, "login", "--check")
			detail = "Auth check: " + check + "\nRefresh: " + refresh + "\nRe-check: " + recheck
			status = recheck
		}
		bb.Result = "## NotebookLM Auth\n\n```\n" + detail + "\n```\n"
		bb.ChainState["nlm_auth"] = detail
		if nlmAuthUnhealthy(status) {
			// Still unhealthy after the refresh attempt: nlm login needs an
			// interactive browser only the user can drive. Failing here just
			// dead-letters the guardian every schedule tick (3 retries each)
			// without changing anything — degrade with a loud instruction
			// instead so the report reaches the user without DLQ noise.
			bb.Result += "\nDEGRADED: automated refresh cannot recover this session — run `nlm login` interactively."
			bb.Outcome = "nlm_auth_needs_user"
			return 1
		}
		bb.Outcome = "success"
		return 1
	})
}

// extractTaskID extracts a UUID-like task ID from research output.
func extractTaskID(output string) string {
	// The nlm research start output typically contains a task_id field
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "task_id") || strings.Contains(line, "task_id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				id := strings.TrimSpace(strings.Trim(parts[1], `"',`))
				if id != "" {
					return id
				}
			}
		}
	}
	// Fallback: find any UUID in the output
	for _, word := range strings.Fields(output) {
		word = strings.Trim(word, `"':{},`)
		if len(word) >= 36 && strings.Count(word, "-") >= 4 {
			return word
		}
	}
	return ""
}

// writeString writes content to a file, creating directories as needed.
func writeString(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
