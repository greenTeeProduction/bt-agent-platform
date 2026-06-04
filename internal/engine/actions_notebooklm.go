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

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerNotebookLMActions()
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

		// Extract research query from task
		query := bb.Task
		report.WriteString(fmt.Sprintf("**Query:** %s\n\n", query))

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
		report.WriteString(fmt.Sprintf("**Task ID:** `%s`\n\n", taskID))

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

		// Step 6: Save to vault
		dateStr := time.Now().Format("2006-01-02")
		savePath := fmt.Sprintf("/mnt/ssd/clawd/wiki/bt-research/syntheses/nlm-research-%s.md", dateStr)
		saveErr := writeString(savePath, report.String())
		if saveErr != nil {
			report.WriteString(fmt.Sprintf("⚠ Save error: %v\n", saveErr))
		} else {
			report.WriteString(fmt.Sprintf("✅ Saved to `%s`\n", savePath))
		}

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

	// CheckNotebookLMAuthAndRefresh — runs auth check, refreshes if stale.
	RegisterAction("CheckNotebookLMAuthAndRefresh", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out := nlmRun(30*time.Second, "login", "--check")
		if strings.Contains(out, "stale") || strings.Contains(out, "not_configured") {
			refreshOut := nlmRun(120*time.Second, "login")
			out = "Auth check: " + out + "\nRefresh: " + refreshOut
		}
		bb.Result = "## NotebookLM Auth\n\n```\n" + out + "\n```\n"
		bb.ChainState["nlm_auth"] = out
		bb.Outcome = "success"
		if strings.Contains(out, "error") || strings.Contains(out, "failed") {
			bb.Outcome = "failure"
			return -1
		}
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
