package agent

import (
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/util"
)

// promoteRunToAgentScope writes the latest successful run summary to agent-scoped keys.
func (d *RunDeps) promoteRunToAgentScope(agentName string, bb *engine.Blackboard, task, output string) {
	if d == nil || bb == nil || bb.BB == nil || agentName == "" || output == "" {
		return
	}
	mgr := d.boardManager()
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: agentName}
	summary := util.Truncate(output, 200)
	_ = mgr.Set(scope, "runs/latest/output", output, summary, "text")
	_ = mgr.Set(scope, "runs/latest/task", task, util.Truncate(task, 120), "text")
	_ = mgr.Set(scope, "runs/latest/run_id", bb.RunID, "", "text")
	if bb.BB.SessionID != "" {
		_ = mgr.Set(scope, "runs/latest/session_id", bb.BB.SessionID, "", "text")
	}
	_ = mgr.Set(scope, "runs/latest/at", time.Now().Format(time.RFC3339), "", "text")
}
