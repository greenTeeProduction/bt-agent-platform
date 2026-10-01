package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/util"
)

// promoteRunToAgentScope writes the latest successful run summary to agent-scoped keys.
func (d *RunDeps) promoteRunToAgentScope(ctx context.Context, agentName string, bb *engine.Blackboard, task, output string) error {
	if d == nil || bb == nil || bb.BB == nil || agentName == "" || output == "" {
		return nil
	}
	mgr := bb.BB.Mgr
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: agentName}
	summary := util.Truncate(output, 200)
	entries := []blackboard.Entry{
		{Key: "runs/latest/output", Value: output, Summary: summary, ContentType: "text"},
		{Key: "runs/latest/task", Value: task, Summary: util.Truncate(task, 120), ContentType: "text"},
		{Key: "runs/latest/run_id", Value: bb.RunID, ContentType: "text"},
	}
	if bb.BB.SessionID != "" {
		entries = append(entries, blackboard.Entry{Key: "runs/latest/session_id", Value: bb.BB.SessionID, ContentType: "text"})
	}
	entries = append(entries, blackboard.Entry{Key: "runs/latest/at", Value: time.Now().Format(time.RFC3339), ContentType: "text"})
	if err := mgr.SetEntriesWithContext(ctx, scope, entries); err != nil {
		return fmt.Errorf("promote completed run metadata: %w", err)
	}
	return nil
}
