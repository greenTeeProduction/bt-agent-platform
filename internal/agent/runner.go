package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// TreeResolver maps a tree ID string to a serializable behavior tree.
type TreeResolver func(treeID string) *evolution.SerializableNode

// UserTreeResolver maps a tree ID string to a serializable behavior tree,
// scoped to one requesting user's own workspace.
type UserTreeResolver func(user, treeID string) *evolution.SerializableNode

// RunDeps holds shared dependencies for agent execution.
type RunDeps struct {
	Registry    *Registry
	History     *History
	LLM         llm.LLM
	RefStore    *evolution.Store
	TreeStore   *evolution.TreeStore
	ResolveTree TreeResolver
	// ResolveTreeForUser scopes tree resolution to one requesting user's own
	// workspace (ADR-067 follow-up milestone). Scheduled personal automations
	// register under a deterministic slug tree ID (goal:automate_<slug>) that
	// carries no user identity of its own, so the unscoped ResolveTree could
	// otherwise hand one user's compiled automation tree to another user's
	// identically-slugged agent. RunOnce consults this resolver instead of
	// ResolveTree whenever the resolved agent's Definition.Metadata["user"] is
	// non-empty. Nil means no per-user resolution is wired, in which case
	// ResolveTree is used regardless of ownership.
	ResolveTreeForUser UserTreeResolver
	Blackboards        *blackboard.Manager
}

// RunOptions configures a single agent run.
type RunOptions struct {
	InjectMemory        bool
	PreviousRunLimit    int  // used when InjectMemory; default 2
	EnforceQuality      bool // fail run when QualitySpec gates miss
	RecordHistory       bool
	InputValues         map[string]string // named inputs from YAML inputs spec
	DisplayName         string            // history label; defaults to agentName
	SessionID           string            // pipeline session scope (future promote)
	DisableBlackboard   bool              // when true, skip run-scoped blackboard tools
	DisableAgentPromote bool              // when true, skip runs/latest/* on agent scope after success
}

// RunResult is the outcome of RunOnce.
type RunResult struct {
	AgentName      string             `json:"agent_name"`
	TreeID         string             `json:"tree_id"`
	Task           string             `json:"task"`
	Outcome        string             `json:"outcome"`
	Output         string             `json:"output"`
	Quality        float64            `json:"quality"`
	QualityPassed  bool               `json:"quality_passed"`
	QualityReasons []string           `json:"quality_reasons,omitempty"`
	OutputPassed   bool               `json:"output_passed"`
	OutputReasons  []string           `json:"output_reasons,omitempty"`
	RunID          string             `json:"run_id,omitempty"`
	SessionID      string             `json:"session_id,omitempty"`
	NodePaths      []string           `json:"node_paths,omitempty"` // BT nodes visited during execution (from bb.VisitedPaths)
	ChildTicks     []engine.ChildTick `json:"-"`                    // terminal child ticks (from bb.ChildTicks()), feeds knowledge.StepsFromChildTicks
	TraceID        string             `json:"trace_id,omitempty"`
	SpanID         string             `json:"span_id,omitempty"`
	Duration       time.Duration
	StartedAt      time.Time
	EndedAt        time.Time
}

// RunOnce executes an agent (registry name or tree ID) through its behavior tree.
func (d *RunDeps) RunOnce(ctx context.Context, agentName, task string, opts RunOptions) (*RunResult, error) {
	if d == nil || d.ResolveTree == nil {
		return nil, fmt.Errorf("RunDeps not configured")
	}
	if strings.TrimSpace(agentName) == "" {
		return nil, fmt.Errorf("agent name required")
	}
	if strings.TrimSpace(task) == "" {
		return nil, fmt.Errorf("task required")
	}

	start := time.Now()
	result := &RunResult{
		AgentName: agentName,
		Task:      task,
		StartedAt: start,
	}

	var def *Definition
	treeID := agentName
	if d.Registry != nil {
		if inst, err := d.Registry.Get(agentName); err == nil {
			def = &inst.Definition
			treeID = inst.Definition.Tree
		}
	}
	result.TreeID = treeID

	fullTask := task
	if def != nil && len(def.Inputs) > 0 {
		vals := opts.InputValues
		if vals == nil {
			vals = make(map[string]string)
		}
		if err := ValidateInputs(def.Inputs, vals); err != nil {
			result.Outcome = "failure"
			result.Output = err.Error()
			result.EndedAt = time.Now()
			result.Duration = result.EndedAt.Sub(start)
			return result, err
		}
		fullTask = BuildTaskFromInputs(def, fullTask, vals)
	}
	resolve := d.ResolveTree
	if def != nil && def.Metadata["user"] != "" && d.ResolveTreeForUser != nil {
		owner := def.Metadata["user"]
		resolve = func(id string) *evolution.SerializableNode {
			return d.ResolveTreeForUser(owner, id)
		}
	}
	tree := resolve(treeID)
	if tree == nil {
		tree = resolve(agentName)
		if tree != nil {
			result.TreeID = agentName
		}
	}
	if tree == nil {
		result.Outcome = "failure"
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(start)
		return result, fmt.Errorf("no tree found for agent %q (tree: %s)", agentName, treeID)
	}

	if ctx != nil {
		select {
		case <-ctx.Done():
			result.Outcome = "timeout"
			result.EndedAt = time.Now()
			result.Duration = result.EndedAt.Sub(start)
			return result, ctx.Err()
		default:
		}
	}

	bb := &engine.Blackboard{
		Reflections: d.RefStore,
		TreeStore:   d.TreeStore,
	}

	// Engine nodes flatten LLM errors into blackboard strings, severing the
	// error chain. The recorder preserves the typed error (e.g. RateLimitError
	// with Retry-After) so the failure return below can re-attach it with %w
	// for the caller's retry policy. Wrap only a non-nil LLM — engine nodes
	// guard on bb.LLM == nil.
	//
	// TracedLLM's context-less Generate/GenerateWithTimeout have no ctx param
	// to carry the caller's active span, so they're given a parent closure
	// that reads bb.TraceContext at call time — set below to the run's root
	// span and updated per node by engine.tracedAction — so LLM spans nest
	// under the active BT node span instead of always rooting at
	// context.Background().
	var llmRecorder *llm.ErrorRecorder
	if d.LLM != nil {
		llmRecorder = llm.NewErrorRecorder(d.LLM)
		bb.LLM = llm.NewTracedLLMWithParent(llmRecorder, "agent-llm", func() context.Context {
			if bb.TraceContext != nil {
				return bb.TraceContext
			}
			return context.Background()
		})
	}
	if !opts.DisableBlackboard {
		runID := blackboard.NewRunID()
		bb.RunID = runID
		bb.Logger = engine.L().With("run_id", runID, "agent", agentName, "tree", result.TreeID)
		bb.BB = blackboard.NewHandle(d.boardManager(), runID, opts.SessionID, agentName)
		result.RunID = runID
		result.SessionID = opts.SessionID
		engine.PrepareBlackboard(bb)
		if opts.InjectMemory {
			fullTask = d.seedMemoryToBlackboard(agentName, fullTask, opts.PreviousRunLimit, bb.BB)
		}
	} else if opts.InjectMemory {
		fullTask = d.injectMemoryContext(agentName, fullTask, opts.PreviousRunLimit)
	}
	bb.Task = fullTask
	bt, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		result.Outcome = "failure"
		result.Output = err.Error()
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(start)
		return result, err
	}
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	spanCtx, runSpan := tracing.StartSpan(runCtx, "agent.run/"+agentName)
	runSpan.SetAttribute("run_id", result.RunID)
	runSpan.SetAttribute("agent", agentName)
	runSpan.SetAttribute("tree", result.TreeID)
	runSpanCtx := runSpan.SpanContext()
	result.TraceID = runSpanCtx.TraceID
	result.SpanID = runSpanCtx.SpanID
	bb.TraceContext = spanCtx
	_ = engine.RunTask(bb, bt)
	runSpan.SetAttribute("outcome", bb.Outcome)
	if bb.Outcome != "success" && bb.Outcome != "" {
		runSpan.RecordError(fmt.Errorf("agent outcome: %s", bb.Outcome))
	}
	runSpan.End()

	result.Output = bb.Result
	result.Outcome = bb.Outcome
	result.NodePaths = bb.VisitedPaths
	result.ChildTicks = bb.ChildTicks()
	if result.Outcome == "" {
		result.Outcome = "failure"
	}

	var spec *QualitySpec
	if def != nil {
		spec = def.Quality
	}
	score, passed, reasons := ValidateQualitySpec(spec, result.Output)
	// Domain terminal actions may refine a "success" outcome into a healthy
	// no-code state (no_change/degraded) and assert an authoritative quality the
	// max()-with-estimate rule must not inflate. Absent those signals this is the
	// original max(estimate, bb.QualityScore) with the outcome untouched.
	result.Outcome, score = applyOutcomeRefinement(result.Outcome, score, bb.QualityScore, bb.QualityAuthoritative, bb.OutcomeRefinement)
	result.Quality = score
	result.QualityPassed = passed
	result.QualityReasons = reasons

	outputPassed, outputReasons := true, []string(nil)
	if def != nil && len(def.Outputs) > 0 {
		outputPassed, outputReasons = ValidateOutputs(def.Outputs, result.Output)
	}
	result.OutputPassed = outputPassed
	result.OutputReasons = outputReasons

	if opts.EnforceQuality && len(outputReasons) > 0 && !outputPassed && result.Outcome == "success" {
		result.Outcome = "failure"
	}

	if opts.EnforceQuality && spec != nil && !passed && result.Outcome == "success" {
		result.Outcome = "failure"
	}

	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(start)

	// Durable selector telemetry: merge this run's Selector-attributed child
	// outcomes into the tree's stats file (learned Selector ordering's writer).
	d.flushSelectorTelemetry(tree, result.TreeID, bb)

	if opts.RecordHistory && d.History != nil {
		errStr := ""
		if !passed && opts.EnforceQuality && spec != nil {
			errStr = strings.Join(reasons, "; ")
		}
		if !outputPassed && opts.EnforceQuality && len(outputReasons) > 0 {
			if errStr != "" {
				errStr += "; "
			}
			errStr += strings.Join(outputReasons, "; ")
		}
		historyName := agentName
		if opts.DisplayName != "" {
			historyName = opts.DisplayName
		}
		_ = d.History.Record(RunRecord{
			AgentName: historyName,
			Task:      task,
			Outcome:   result.Outcome,
			Output:    result.Output,
			Error:     errStr,
			Duration:  result.Duration.Truncate(time.Second).String(),
			Quality:   result.Quality,
			StartedAt: start,
			EndedAt:   result.EndedAt,
		})
	}

	if result.Outcome == "success" && !opts.DisableBlackboard && !opts.DisableAgentPromote && bb.BB != nil {
		d.promoteRunToAgentScope(agentName, bb, task, result.Output)
	}

	// no_change / degraded are healthy no-code terminal states (Item 1,
	// 2026-07-13): recorded honestly in history above, but they are not errors —
	// the scheduler must not retry or dead-letter them, and they are not
	// exemplary enough to promote. Return before the error path below.
	if isHealthyOutcome(result.Outcome) && result.Outcome != "success" {
		return result, nil
	}

	if result.Outcome != "success" {
		if opts.EnforceQuality && !outputPassed && len(outputReasons) > 0 {
			return result, fmt.Errorf("output contract failed: %s", strings.Join(outputReasons, "; "))
		}
		if opts.EnforceQuality && spec != nil && !passed {
			return result, fmt.Errorf("quality gate failed: %s", strings.Join(reasons, "; "))
		}
		if llmRecorder != nil {
			if llmErr := llmRecorder.LastError(); llmErr != nil {
				return result, fmt.Errorf("agent outcome: %s: %w", result.Outcome, llmErr)
			}
		}
		return result, fmt.Errorf("agent outcome: %s: %s", result.Outcome, OutcomeErrorDetail(result.Output))
	}

	return result, nil
}

// applyOutcomeRefinement folds the blackboard's honest-signal fields into the
// recorded (outcome, quality). A refinement string renames a "success" tree
// outcome into a healthy no-code state (no_change/degraded) so history stops
// counting analysis-only cycles as full successes. An authoritative quality is
// used verbatim; otherwise the original max(estimate, bbScore) rule stands.
func applyOutcomeRefinement(outcome string, estimate, bbScore float64, authoritative bool, refinement string) (string, float64) {
	quality := estimate
	if authoritative {
		quality = bbScore
	} else if bbScore > quality {
		quality = bbScore
	}
	if outcome == "success" && refinement != "" {
		outcome = refinement
	}
	return outcome, quality
}

// IsHealthyOutcome is the exported form of isHealthyOutcome for callers
// outside the package (cmd/bt-agent's scheduler attempt recording).
func IsHealthyOutcome(outcome string) bool { return isHealthyOutcome(outcome) }

// isHealthyOutcome reports whether an outcome is a healthy terminal state that
// the scheduler must neither retry nor dead-letter. no_change (analysis-only,
// nothing to change) and degraded (Claude path fell back to deterministic
// analysis) join success as non-error outcomes.
func isHealthyOutcome(outcome string) bool {
	switch outcome {
	case "success", "no_change", "degraded":
		return true
	}
	return false
}

// OutcomeErrorDetail distills the run output's tail into the outcome error so
// retry exhaustion and dead-letter records carry the actual failure instead
// of "last: unknown" — the 2026-07-04 applied_uncommitted runs were
// undiagnosable from the DLQ alone. Exported so callers outside internal/agent
// (the bt-agent scheduler closure) can produce the same DLQ-diagnosable detail.
func OutcomeErrorDetail(output string) string {
	out := strings.TrimSpace(output)
	if out == "" {
		return "no run output"
	}
	if len(out) > 400 {
		out = "…" + out[len(out)-400:]
	}
	return strings.ReplaceAll(out, "\n", " | ")
}

// BoardManager returns the shared blackboard manager (lazy default).
func (d *RunDeps) BoardManager() *blackboard.Manager {
	return d.boardManager()
}

func (d *RunDeps) boardManager() *blackboard.Manager {
	if d == nil {
		return blackboard.DefaultManager()
	}
	if d.Blackboards == nil {
		mgr := blackboard.DefaultManager()
		_ = mgr.EnablePersistence(BlackboardDir())
		d.Blackboards = mgr
	}
	return d.Blackboards
}

func (d *RunDeps) injectMemoryContext(agentName, task string, prevLimit int) string {
	if prevLimit <= 0 {
		prevLimit = 2
	}
	mem, err := NewMemoryStore(MemoryDir(), agentName, 100)
	if err != nil {
		return task
	}
	full := task
	if memCtx := mem.ContextBlock(); memCtx != "" {
		full = full + "\n\n" + memCtx
	}
	if d.History != nil {
		if prevCtx := mem.PreviousRunContext(d.History, agentName, prevLimit); prevCtx != "" {
			full = full + "\n\n" + prevCtx
		}
	}
	return full
}

// RateLimitCarryoverOutcome is the sentinel a goap cycle returns when it
// pauses gracefully on a Claude rate-limit backoff and carries its plan over
// to the next cycle. The literal is written by the engine's rate-limit branch
// (internal/engine, which cannot import this package) and consumed by the
// scheduler's attempt recording — it is an expected, healthy pause, never a
// retryable failure.
const RateLimitCarryoverOutcome = "goap_fusion_rate_limited"

// IsRateLimitCarryover reports whether outcome is the rate-limit-carryover
// sentinel — a healthy, expected backoff pause, never a genuine failure.
// Consolidates the `outcome == RateLimitCarryoverOutcome` comparison that was
// duplicated across scheduler.go, cmd/bt-agent/main.go, and
// dashboard/executor.go before the 2026-07-17 fix closed the gap; future call
// sites should use this helper instead of re-typing the raw comparison.
func IsRateLimitCarryover(outcome string) bool {
	return outcome == RateLimitCarryoverOutcome
}
