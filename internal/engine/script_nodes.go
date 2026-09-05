package engine

import (
	"fmt"
	"os/exec"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

// registerScriptNodes registers BT action nodes that wrap existing Python
// scripts as BT actions, replacing Hermes cron jobs.
func registerScriptNodes() {
	// IndexSessions runs the session indexer script.
	actionRegistry["IndexSessions"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cmd := exec.Command("python3", "/mnt/ssd/.hermes/scripts/session_indexer.py", "index", "4", "500")
		output, err := cmd.CombinedOutput()
		if err != nil {
			b.Result = fmt.Sprintf("session indexer failed: %v\n%s", err, string(output))
			b.Outcome = "failure"
			return -1
		}
		b.Result = fmt.Sprintf("sessions indexed: %s", strings.TrimSpace(string(output)))
		b.Outcome = "success"
		return 1
	}

	// ExtractMemories runs the memory extractor script.
	actionRegistry["ExtractMemories"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cmd := exec.Command("python3", "/mnt/ssd/.hermes/scripts/memory_extractor.py", "run")
		output, err := cmd.CombinedOutput()
		if err != nil {
			b.Result = fmt.Sprintf("memory extractor failed: %v\n%s", err, string(output))
			b.Outcome = "failure"
			return -1
		}
		b.Result = fmt.Sprintf("memory extraction: %s", strings.TrimSpace(string(output)))
		b.Outcome = "success"
		return 1
	}
}
