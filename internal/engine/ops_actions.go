package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/metrics"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
	btcore "github.com/rvitorper/go-bt/core"
)

// TaskDLQ is set from cmd/bt-agent for PushToDLQ actions.
var TaskDLQ *reliability.DeadLetterQueue

func init() {
	registerOpsActions()
}

func registerOpsActions() {
	RegisterAction("FitnessProbe", fitnessProbeAction)
	RegisterAction("TraceCheckpoint", traceCheckpointAction)
	RegisterAction("AuditLog", auditLogAction)
	RegisterAction("PushToDLQ", pushToDLQAction)
}

func fitnessProbeAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	success := bb.Outcome == "success" || bb.Outcome == "completed"
	score := fitnessScoreFromBB(bb.Outcome, bb.QualityScore, success)
	bb.ChainState["block_fitness"] = score
	agent, _ := bb.ChainState["agent_name"].(string)
	metrics.RecordBlockFitness("probe", agent, score)
	return 1
}

func traceCheckpointAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	label := "checkpoint"
	if bb != nil && bb.ChainState != nil {
		if l, ok := bb.ChainState["trace_checkpoint"].(string); ok && l != "" {
			label = l
		}
	}
	parent := ctx.Context
	if bb != nil && bb.TraceContext != nil {
		parent = bb.TraceContext
	}
	_, span := tracing.StartSpan(parent, "bt.checkpoint/"+label)
	defer span.End()
	span.AddEvent("trace_checkpoint", tracing.StringAttr("label", label))
	if bb != nil {
		snap := map[string]any{
			"task":    truncateStr(bb.Task, 200),
			"outcome": bb.Outcome,
			"plan":    truncateStr(bb.Plan, 200),
			"result":  truncateStr(bb.Result, 400),
		}
		if data, err := json.Marshal(snap); err == nil {
			span.AddEvent("blackboard_snapshot", tracing.StringAttr("json", string(data)))
		}
	}
	return 1
}

func auditLogAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if audit.TaskLogPath() == "" {
		home := os.Getenv("HOME")
		if home != "" {
			audit.Init(filepath.Join(home, ".go-bt-evolve"))
		}
	}
	agent := ""
	if bb != nil && bb.ChainState != nil {
		if a, ok := bb.ChainState["agent_name"].(string); ok {
			agent = a
		}
	}
	detail := ""
	if bb != nil {
		detail = truncateStr(bb.Result, 500)
	}
	_ = audit.Append(audit.Entry{
		Action: "bt_run",
		Agent:  agent,
		Task:   truncateStr(bb.Task, 300),
		Detail: detail,
		Metadata: map[string]string{
			"outcome": bb.Outcome,
		},
	})
	return 1
}

func pushToDLQAction(ctx *btcore.BTContext[Blackboard]) int {
	if TaskDLQ == nil {
		return 1
	}
	bb := ctx.Blackboard
	agent := "agent"
	if bb.ChainState != nil {
		if a, ok := bb.ChainState["agent_name"].(string); ok && a != "" {
			agent = a
		}
	}
	errMsg := bb.Result
	if errMsg == "" {
		errMsg = "persistent failures — escalated to DLQ"
	}
	TaskDLQ.Push(reliability.DeadLetterEntry{
		Task:     bb.Task,
		Agent:    agent,
		Error:    errMsg,
		Attempts: bb.FailureCount,
		Category: "hitl_exhausted",
	})
	bb.Result += "\n\nTask escalated to dead letter queue."
	return 1
}

func fitnessScoreFromBB(outcome string, qualityScore float64, success bool) float64 {
	score := qualityScore * 100
	if score <= 0 {
		if success || strings.EqualFold(outcome, "success") || strings.EqualFold(outcome, "completed") {
			score = 75
		} else {
			score = 25
		}
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

func truncateStr(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
