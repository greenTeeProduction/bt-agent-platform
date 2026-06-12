// Package engine provides the behavior tree runtime for the BT platform.
//
// It implements tree building, execution, action/condition registration, and
// the Blackboard context that carries task state through tree execution.
// The package also defines 10 chain types (llm_call, agent, refine, map_reduce,
// rag_query, structured_output, retrieval_qa, conversation, tool_call, tool_action)
// that integrate langchaingo workflows directly into behavior tree nodes.
//
// Key types:
//   - Blackboard — shared state (Task, Plan, Result, Outcome, ChainTools, ChainMemory)
//   - SerializableNode — JSON-serializable tree node used across all domain trees
//
// Key functions:
//   - RunTask(bb, tree) — executes a tree to completion with 1000-tick safety limit
//   - BuildTree(tree, bb) — converts a SerializableNode into a runnable go-bt tree
//   - actionForName / conditionForName — registry of 175+ engine nodes
package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/tracing"
	"github.com/nico/go-bt-evolve/internal/util"

	btcomp "github.com/rvitorper/go-bt/composite"
	btcore "github.com/rvitorper/go-bt/core"
	btdec "github.com/rvitorper/go-bt/decorators"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// toolStub is a lightweight tool implementation for bt.ChainTools.
// It implements Name(), Description(), and Call(string)string.
// When a real tool isn't available, Call falls back to LLM simulation
// via executeAgentTool in chains.go.
type toolStub struct {
	name string
	desc string
}

func (t toolStub) Name() string        { return t.name }
func (t toolStub) Description() string { return t.desc }
func (t toolStub) Call(_ string) string {
	return fmt.Sprintf("STUB_ERROR: tool '%s' is a stub with no real implementation. Do not fabricate output — report that this tool is unavailable and proceed with available tools only.", t.name)
}

// Blackboard is the shared state passed through the behavior tree.
type Blackboard struct {
	Task         string
	Complexity   string
	Plan         string
	Result       string
	Outcome      string
	DurationMs   int64
	KgResults    string
	CachedResult string
	FailureCount int
	Reflections  *evolution.Store
	TreeStore    *evolution.TreeStore
	LLM          llm.LLM

	// Langchain integration — chain primitives accessible from BT nodes.
	// Use interface{} to avoid circular imports; chain runners cast to concrete types.
	ChainMemory  any            // langchaingo memory (ConversationBuffer, etc.)
	ChainTools   []any          // langchaingo tools available to chains
	ChainState   map[string]any // arbitrary chain execution state
	Results      []string       // accumulated results from all chain actions
	QualityScore float64        // 0.0-1.0 output quality score
	CurrentPath  string         // currently executing strategy path (set by tree traversal)
	VisitedPaths []string       // all strategy paths visited during execution
	EventBus     *EventBus      // inter-node event bus (Plan #3: AbortOnEvent, ReactiveParallel)

	// Budget tracking (Budget decorator / agent limits)
	TokensUsed int
	TickBudget int
	TreeTicks  int

	TraceContext context.Context `json:"-"`
}

// BuildTree constructs a go-bt Command from a SerializableNode tree definition.
// Invalid trees produce a failing command instead of silently executing an unsafe
// or unknown structure. Use BuildAndValidate when the caller needs the error.
func BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	cmd, err := BuildAndValidate(serTree, bb)
	if err != nil {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			msg := fmt.Sprintf("tree validation failed: %v", err)
			ctx.Blackboard.Outcome = msg
			ctx.Blackboard.Result = msg
			return -1
		})
	}
	return cmd
}

// BuildAndValidate constructs a tree and validates it before execution.
// SubTreeRef nodes are expanded first when a tree expander is registered (internal/blocks).
// Returns an error if validation fails; on success the tree is still built.
func BuildAndValidate(serTree *evolution.SerializableNode, bb *Blackboard) (btcore.Command[Blackboard], error) {
	expanded, err := prepareTreeForBuild(serTree)
	if err != nil {
		return nil, err
	}
	info := ValidateTreeFull(expanded)
	if !info.Valid() {
		return nil, fmt.Errorf("tree validation failed: %v", info.Errors)
	}
	return buildNode(expanded, bb, ""), nil
}

// buildNode recursively builds a go-bt Command from a SerializableNode.
// parentName tracks the parent node's name for path-tracking in StrategyRouters.
func buildNode(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	// If this Sequence is inside a StrategyRouter, record its name as the active path
	if parentName == "StrategyRouter" && node.Type == "Sequence" && node.Name != "" {
		origChildren := node.Children
		// Prepend a path-recording action before the sequence's children
		pathRecordAction := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.CurrentPath = node.Name
			ctx.Blackboard.VisitedPaths = append(ctx.Blackboard.VisitedPaths, node.Name)
			return 1
		})
		children := make([]btcore.Command[Blackboard], len(origChildren)+1)
		children[0] = pathRecordAction
		for i := range origChildren {
			children[i+1] = buildNode(&origChildren[i], bb, node.Name)
		}
		return btcomp.NewSequence(children...)
	}

	switch node.Type {
	case "Sequence":
		if len(node.Edges) > 0 {
			return buildSequenceWithEdges(node, bb)
		}
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		return btcomp.NewSequence(children...)
	case "Selector":
		if len(node.Edges) > 0 {
			return buildSelectorWithEdges(node, bb)
		}
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		return btcomp.NewSelector(children...)
	case "Parallel":
		return BuildParallel(node, bb)
	case "Budget":
		return BuildBudget(node, bb)
	case "RateLimit":
		return BuildRateLimit(node, bb)
	case "Timeout":
		return BuildTimeout(node, bb)
	case "CircuitBreaker":
		return BuildCircuitBreaker(node, bb)
	case "Inverter":
		return BuildInverter(node, bb)
	case "Succeeder":
		return BuildSucceeder(node, bb)
	case "Repeater":
		return BuildRepeater(node, bb)
	case "Runner":
		return BuildRunner(node, bb)
	case "Monitor":
		return BuildMonitor(node, bb)
	case "QualityGate":
		return BuildQualityGate(node, bb)
	case "Retry":
		if len(node.Children) == 0 {
			return btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 })
		}
		child := buildNode(&node.Children[0], bb, node.Name)
		times := node.MaxRetries
		if times <= 0 {
			times = 1
		}
		return btdec.NewRepeat(child, times)
	case "Action":
		return btleaf.NewAction(bb.actionForName(node.Name))
	case "ChainAction":
		// Langchain chain node — reads ChainConfig from node metadata
		cfg := parseChainConfig(node)
		return BuildChainAction(cfg, bb)
	case "Condition":
		return btleaf.NewCondition(bb.conditionForName(node.Name))
	case "UtilitySelector":
		return BuildUtilitySelector(node, bb)
	case "DecisionTree":
		return BuildDecisionTree(node, bb)
	case "PlannerNode":
		// PlannerNode extends UtilitySelector with GOAP goal management
		return BuildPlannerNode(node, bb)
	case "AbortOnEvent":
		return BuildEventDrivenAbort(node, bb)
	case "ReactiveParallel":
		return BuildReactiveParallel(node, bb)
	case "CheckpointVerifier":
		if len(node.Children) == 0 {
			return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
		}
		child := buildNode(&node.Children[0], bb, node.Name)
		postconditions := readPostconditions(node)
		return NewCheckpointVerifier(child, node.MaxRetries, postconditions)
	case "HumanApprovalGate":
		return buildHumanApprovalGate(node, bb, parentName)
	case "SubTreeRef":
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "SubTreeRef not expanded — run BuildAndValidate with tree expander"
			return -1
		})
	case "AlwaysSucceed":
		return btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int {
			return 1
		})
	default:
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = fmt.Sprintf("unsupported node type %q", node.Type)
			return -1
		})
	}
}

func (bb *Blackboard) actionForName(name string) func(*btcore.BTContext[Blackboard]) int {
	// Registry-first: packages register via engine.RegisterAction() in init().
	// GetAction returns the zero-value ActionFunc (nil) for unknown names.
	if fn := GetAction(name); fn != nil {
		return fn
	}
	// Fallback: unknown actions succeed silently (permissive, same as original default)
	return func(ctx *btcore.BTContext[Blackboard]) int {
		return 1
	}
}

func (bb *Blackboard) conditionForName(name string) func(*Blackboard) bool {
	// Registry-first: packages register via engine.RegisterCondition() in init().
	// GetCondition returns nil for unknown names.
	if fn := GetCondition(name); fn != nil {
		return fn
	}
	// Default: always-true condition (permissive routing)
	return func(b *Blackboard) bool {
		return true
	}
}

// RunTask executes a task through the behavior tree to completion.
// Multi-tick decorators (Repeat) return 0 (Running) between ticks, so we loop
// until the tree reaches a terminal state (1=Success or -1=Failure).
// validateOutputQuality checks if the agent's output meets minimum quality standards.
// Returns true if the output is acceptable; false if it appears to be garbage.
// This prevents agents reporting "success" with truncated/garbage output
// (e.g., max_tokens=10 producing a few words).
func validateOutputQuality(b *Blackboard) bool {
	result := b.Result
	if b.Result == "" && len(b.Results) > 0 {
		// Use accumulated results if Result is empty
		result = b.Results[len(b.Results)-1]
	}

	// 0. Structured zero-LLM output detection — short but valid structured output
	// from trees like alert_router, agent_monitor that produce markdown-formatted
	// routing/status results without LLM calls.
	lowerResult := strings.ToLower(result)
	isStructured := strings.HasPrefix(strings.TrimSpace(result), "## ") ||
		strings.Contains(lowerResult, "route:") ||
		strings.Contains(lowerResult, "status:") ||
		strings.Contains(lowerResult, "delivered")
	minLen := 30
	if isStructured {
		minLen = 15 // structured zero-LLM output is intentionally compact
	}

	// 1. Minimum length check
	if len(result) < minLen {
		b.QualityScore = 0.0
		return false
	}

	// 2. Error pattern check
	errorPatterns := []string{
		"output quality failed", "i cannot", "i can't", "unable to", "error:", "failed to",
		"i don't know", "i'm not sure", "not implemented",
	}
	for _, p := range errorPatterns {
		if strings.Contains(lowerResult, p) {
			b.QualityScore = 0.1
			return false
		}
	}

	// 3. Structure check (bonus for structured output)
	score := 0.5 // baseline for meeting minimum length + no errors
	if strings.Contains(result, "#") || strings.Contains(result, "**") {
		score += 0.2 // has markdown structure
	}
	if strings.Contains(result, "- ") || strings.Contains(result, "* ") {
		score += 0.1 // has bullet points
	}
	if len(result) > 200 {
		score += 0.1 // substantive length
	}
	if strings.Contains(result, "```") {
		score += 0.1 // contains code blocks
	}
	// Bonus for zero-LLM routing output (alert_router, etc.)
	if isStructured && len(result) < 100 {
		score += 0.2 // compact but valid structured output
	}
	if score > 1.0 {
		score = 1.0
	}
	b.QualityScore = score
	return score >= 0.5
}

func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string {
	start := time.Now()

	// ── Tracing: wrap tree execution in a span ──
	taskName := bb.Task
	if len(taskName) > 50 {
		taskName = taskName[:50]
	}
	_, span := tracing.StartSpan(context.Background(), "RunTask:"+taskName)
	defer span.End()
	span.SetAttribute("task", util.Truncate(bb.Task, 80))

	// Panic recovery at the tree level — if the entire BT crashes, capture it.
	defer func() {
		if r := recover(); r != nil {
			bb.Outcome = string(evolution.Failure)
			bb.Result = fmt.Sprintf("TREE PANIC: %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	btCtx := btcore.NewBTContext(ctx, bb)

	code := tree.Run(btCtx)

	// Multi-tick loop: Repeat and other decorators return 0 (Running) between
	// ticks. Keep ticking until a terminal status is reached.
	const maxTicks = 1000
	for tick := 1; code == 0 && tick < maxTicks; tick++ {
		code = tree.Run(btCtx)
	}

	bb.DurationMs = time.Since(start).Milliseconds()

	if code == 1 {
		bb.Outcome = string(evolution.Success)
	} else if code == -1 {
		bb.Outcome = string(evolution.Failure)
	} else {
		bb.Outcome = string(evolution.Partial)
	}

	span.SetAttribute("outcome", bb.Outcome)
	span.SetAttribute("duration_ms", fmt.Sprintf("%d", bb.DurationMs))

	// Always validate output quality — some trees (agent_monitor, alert_router)
	// don't include ReflectOnOutcome which is where quality scoring normally runs.
	// Without this, zero-LLM trees report quality=0 even with valid structured output.
	validateOutputQuality(bb)

	return bb.Result
}
