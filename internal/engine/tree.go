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
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
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

// ChildTick is one terminal (success/failure) child tick under a named parent
// composite, recorded by the observability wrapper. The agent runner flushes
// Selector-attributed ticks into the durable per-tree selector telemetry at
// run end, which is what feeds learned Selector ordering.
type ChildTick struct {
	Parent string
	Child  string
	Status string
}

// maxChildTicks bounds the per-run tick record so a runaway re-ticking tree
// cannot grow the run's memory without limit.
const maxChildTicks = 1024

// childTickLog is the shared, mutex-guarded tick record. It hangs off the
// Blackboard as a POINTER so the shallow Blackboard copies some composites
// make (reactive parallel) share the same log instead of tripping copylocks —
// and their children's ticks still land in the run's record.
type childTickLog struct {
	mu    sync.Mutex
	ticks []ChildTick
}

// recordChildTick appends one terminal child outcome, dropping ticks beyond
// maxChildTicks. Guarded because Parallel composites tick children
// concurrently. BuildTree pre-initializes the log; the lazy init here only
// serves single-threaded direct callers (tests).
func (bb *Blackboard) recordChildTick(parent, child, status string) {
	if bb.childTicks == nil {
		bb.childTicks = &childTickLog{}
	}
	l := bb.childTicks
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ticks) >= maxChildTicks {
		return
	}
	l.ticks = append(l.ticks, ChildTick{Parent: parent, Child: child, Status: status})
}

// ChildTicks returns a copy of the run's recorded terminal child ticks.
func (bb *Blackboard) ChildTicks() []ChildTick {
	if bb.childTicks == nil {
		return nil
	}
	bb.childTicks.mu.Lock()
	defer bb.childTicks.mu.Unlock()
	out := slices.Clone(bb.childTicks.ticks)
	return out
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
	// OutcomeRefinement lets a domain's terminal action refine the recorded
	// run outcome beyond the tree's success/failure/partial code (which tree.Run
	// sets from the root node's return status and would otherwise flatten a
	// healthy-but-no-code cycle into a plain "success"). The agent runner applies
	// it only when the tree outcome is "success", so it can name a healthy
	// terminal state ("no_change", "degraded") without turning it into a failure.
	OutcomeRefinement string
	// QualityAuthoritative makes QualityScore the recorded quality verbatim
	// instead of max(estimateQuality, QualityScore). A terminal action sets it
	// when it knows the true quality (e.g. a no-code cycle must score below the
	// text-shape estimate, which the max() rule would otherwise inflate).
	QualityAuthoritative bool
	CurrentPath          string    // currently executing strategy path (set by tree traversal)
	VisitedPaths         []string  // all strategy paths visited during execution
	EventBus             *EventBus // inter-node event bus (Plan #3: AbortOnEvent, ReactiveParallel)
	TreeTimeoutMs        int64     // custom tree timeout in ms (0 = use default 120s)

	// Budget tracking (Budget decorator / agent limits)
	TokensUsed int
	TickBudget int
	TreeTicks  int

	// childTicks records terminal child outcomes with their parent composite
	// (appended by the observability wrapper, bounded by maxChildTicks; read
	// via ChildTicks()). The agent runner filters these to Selector parents
	// and merges them into the durable per-tree selector telemetry at run end.
	// A pointer so shallow Blackboard copies share the same log (copylocks).
	childTicks *childTickLog

	// liveRun and buildCapture support runtime tree mutation
	// (tree_mutation.go / live_run.go). Pointer + map so forkBlackboard's
	// shallow copies share them. buildCapture, when non-nil, makes buildNode
	// record each source node's INNER command — the pointer the go-bt library
	// keys per-node state by — enabling state migration across rebuilds.
	liveRun      *liveRun
	buildCapture map[*evolution.SerializableNode]btcore.Command[Blackboard]

	// Sandbox disables real action side effects: when true, actionForName
	// returns a simulated success for every action instead of dispatching to
	// the registered implementation. Used by benchmark/evolution harnesses so
	// tree evaluation can never spawn subprocesses, hit the network, or burn
	// external API quotas. Conditions still run (routing stays observable).
	Sandbox bool

	TraceContext context.Context `json:"-"`
	Logger       *slog.Logger    `json:"-"` // run-scoped logger (run_id/agent/tree bound); use Log()

	// Blackboard management (Phase 1): scoped key-value store for context offloading.
	BB    *blackboard.Handle
	RunID string
}

// Log returns the run-scoped logger when bound, else the global logger.
func (bb *Blackboard) Log() *slog.Logger {
	if bb.Logger != nil {
		return bb.Logger
	}
	return L()
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
	// Pre-initialize the shared tick log so shallow Blackboard copies made
	// during execution (reactive parallel) share this run's log rather than
	// lazily creating divergent ones.
	if bb != nil && bb.childTicks == nil {
		bb.childTicks = &childTickLog{}
	}
	expanded, err := prepareTreeForBuild(serTree)
	if err != nil {
		return nil, err
	}
	if serTree != nil && serTree.TimeoutMs > 0 {
		bb.TreeTimeoutMs = serTree.TimeoutMs
	}
	info := ValidateTreeFull(expanded)
	if !info.Valid() {
		return nil, fmt.Errorf("tree validation failed: %v", info.Errors)
	}
	return buildNode(expanded, bb, ""), nil
}

// buildNode builds the node and wraps it with the per-node observability
// command (tracing span, RecordNodeTickFn metrics hook, terminal child-tick
// recording for selector telemetry). The wrapper existed but was never
// applied — node metrics, node spans, and selector telemetry all silently
// produced nothing until this wiring.
func buildNode(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	inner := buildNodeInner(node, bb, parentName)
	if bb != nil && bb.buildCapture != nil {
		bb.buildCapture[node] = inner
	}
	return observeNode(node, parentName, inner)
}

// buildNodeInner recursively builds a go-bt Command from a SerializableNode.
// parentName tracks the parent node's name for path-tracking in StrategyRouters.
func buildNodeInner(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
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
	case "MemSequence":
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, node.Name)
		}
		return btcomp.NewMemSequence(children...)
	case "MemSelector":
		return BuildMemSelector(node, bb)
	case "PersistentMemSequence":
		return BuildPersistentMemSequence(node, bb)
	case "CachedCondition":
		return BuildCachedCondition(node, bb)
	case "SemaphoreGuard":
		return BuildSemaphoreGuard(node, bb)
	case "ForEachTask":
		return BuildForEachTask(node, bb)
	case "ReviewCycle":
		return BuildReviewCycle(node, bb)
	case "BanditSelector":
		return BuildBanditSelector(node, bb)
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
	case "ClaudeErrorHandler":
		return BuildClaudeErrorHandler(node, bb)
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
	// Sandbox mode: never dispatch to real implementations — simulate success
	// so structural evaluation can tick trees without side effects.
	if bb.Sandbox {
		return func(ctx *btcore.BTContext[Blackboard]) int {
			b := ctx.Blackboard
			b.Results = append(b.Results, "[sandbox] "+name)
			return 1
		}
	}
	// Registry-first: packages register via engine.RegisterAction() in init().
	// GetAction returns the zero-value ActionFunc (nil) for unknown names.
	if fn := GetAction(name); fn != nil {
		return fn
	}
	// Name-parameterized compiled-GOAP effect writes ("ApplyGoapEffects:k=v"),
	// emitted by the plan→BT compiler (goap_compiled_nodes.go).
	if fn := compiledGoapActionFor(name); fn != nil {
		return tracedAction(name, fn)
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
	// Name-parameterized compiled-GOAP guards ("GoapStateMatches:k=v"),
	// emitted by the plan→BT compiler (goap_compiled_nodes.go).
	if fn := compiledGoapConditionFor(name); fn != nil {
		return tracedCondition(name, fn)
	}
	// Name-parameterized error-handler guards ("LastErrorCategoryIs:<cat>",
	// "LastErrorNodeIs:<node>") used by Claude-proposed recovery nodes.
	if fn := errorHandlerConditionFor(name); fn != nil {
		return tracedCondition(name, fn)
	}
	// Default: always-true condition (permissive routing)
	return func(b *Blackboard) bool {
		return true
	}
}

// resolvedResult returns b.Result, falling back to the most recent entry in
// b.Results when b.Result is empty (e.g. a Sequence of actions that only
// append to Results without ever setting the final result field). Shared by
// validateOutputQuality and RunTask's terminal backstop so both judge the
// same resolved string.
func resolvedResult(b *Blackboard) string {
	if b.Result != "" || len(b.Results) == 0 {
		return b.Result
	}
	return b.Results[len(b.Results)-1]
}

// isStructuredOutput reports whether result looks like short-but-valid
// zero-LLM structured output (e.g. alert_router, agent_monitor routing/status
// text) rather than truncated or garbage output.
func isStructuredOutput(result string) bool {
	lowerResult := strings.ToLower(result)
	return strings.HasPrefix(strings.TrimSpace(result), "## ") ||
		strings.Contains(lowerResult, "route:") ||
		strings.Contains(lowerResult, "status:") ||
		strings.Contains(lowerResult, "delivered")
}

// RunTask executes a task through the behavior tree to completion.
// Multi-tick decorators (Repeat) return 0 (Running) between ticks, so we loop
// until the tree reaches a terminal state (1=Success or -1=Failure).
// validateOutputQuality checks if the agent's output meets minimum quality standards.
// Returns true if the output is acceptable; false if it appears to be garbage.
// This prevents agents reporting "success" with truncated/garbage output
// (e.g., max_tokens=10 producing a few words).
func validateOutputQuality(b *Blackboard) bool {
	result := resolvedResult(b)

	// 0. Structured zero-LLM output detection — short but valid structured output
	// from trees like alert_router, agent_monitor that produce markdown-formatted
	// routing/status results without LLM calls.
	isStructured := isStructuredOutput(result)
	minLen := 30
	if isStructured {
		minLen = 15 // structured zero-LLM output is intentionally compact
	}

	// 1. Minimum length check
	if len(result) < minLen {
		setHeuristicQuality(b, 0.0)
		return false
	}

	// 2. Error/incomplete pattern check. These markers indicate the agent is
	// explicitly reporting unfinished or unverified work, so they must not be
	// allowed to score as successful structured output.
	errorPatterns := []string{
		"output quality failed", "i cannot", "i can't", "unable to", "error:", "failed to",
		"i don't know", "i'm not sure", "not implemented", "incomplete", "step limit",
		"could not be determined", "could not be verified", "not verified", "unverified",
		// Refusal/apology/empty-turn patterns: an agent answering with an
		// apology or a request for more input did no work and must not score
		// as success (observed: alert-router runs whose entire output was
		// "I'm sorry, but there is no previous task recorded…" scored 0.5).
		"i'm sorry", "i am sorry", "no previous task", "please provide",
		"if you have a specific task", "as an ai language model", "i'd be happy to",
		"let me know what", "there is nothing to",
	}
	// Scan outside fenced code blocks only: reports legitimately embed CLI
	// transcripts (nlm output, git output) whose error words describe the
	// tool's state, not the run's. The 2026-07-15 researcher treadmill was a
	// budget-skip line inside a fence failing 17KB of genuine research.
	scanned := strings.ToLower(stripFencedBlocks(result))
	for _, p := range errorPatterns {
		if strings.Contains(scanned, p) {
			setHeuristicQuality(b, 0.1)
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
	setHeuristicQuality(b, score)
	return score >= 0.5
}

// setHeuristicQuality records the text-shape heuristic score UNLESS a domain
// classifier already asserted an authoritative one (bb.QualityAuthoritative).
// validateOutputQuality runs after tree completion, so an unconditional write
// here silently clobbered authoritative scores — 2026-07-15's degraded goap
// cycles recorded 0.8999999999999999 (the heuristic sum) instead of their
// classifier-stamped 0.3.
func setHeuristicQuality(b *Blackboard, score float64) {
	if b.QualityAuthoritative {
		return
	}
	b.QualityScore = score
}

// stripFencedBlocks removes ``` fenced regions from s so error-pattern scans
// only see the report's own prose, not embedded tool transcripts.
func stripFencedBlocks(s string) string {
	var out strings.Builder
	inFence := false
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string {
	start := time.Now()

	// Production Blackboard-construction sites (a2a Execute, bt_run_task MCP
	// tool) leave ChainState nil; dozens of engine nodes write
	// bb.ChainState[k]=v unguarded, which panics on a nil map. Guard here,
	// the single choke point every caller goes through.
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}

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

	treeTimeout := 120 * time.Second
	if bb.TreeTimeoutMs > 0 {
		treeTimeout = time.Duration(bb.TreeTimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), treeTimeout)
	defer cancel()
	btCtx := btcore.NewBTContext(ctx, bb)

	if bb.liveRun != nil {
		tree = bb.liveRun.applyPending(btCtx, bb, tree)
	}
	code := tree.Run(btCtx)

	// Multi-tick loop: Repeat and other decorators return 0 (Running) between
	// ticks. Keep ticking until a terminal status is reached. A HITL gate
	// awaiting an external human (bb.Outcome == "pending_approval") is also
	// Running, but nothing inside this synchronous loop can change that
	// status — re-ticking would just burn maxTicks iterations for no effect —
	// so stop immediately and let RunTask return that outcome to the caller.
	// Mutable runs (bb.liveRun set) apply queued tree mutations at each tick
	// boundary — a quiescent point — and keep ticking the rebuilt tree.
	const maxTicks = 1000
	for tick := 1; code == 0 && bb.Outcome != "pending_approval" && tick < maxTicks; tick++ {
		if bb.liveRun != nil {
			tree = bb.liveRun.applyPending(btCtx, bb, tree)
		}
		code = tree.Run(btCtx)
	}

	bb.DurationMs = time.Since(start).Milliseconds()

	switch {
	case bb.Outcome == "goap_fusion_rate_limited":
		// Deliberate graceful-degrade carryover set by a leaf (e.g. an active
		// Claude rate-limit backoff) — preserve it instead of collapsing the
		// tree's generic failure code to Failure, so the scheduler can defer
		// rather than dead-letter this attempt.
	case code == 1:
		bb.Outcome = string(evolution.Success)
	case code == -1:
		bb.Outcome = string(evolution.Failure)
	case bb.Outcome == "pending_approval":
		// Non-terminal HITL outcome — preserve it rather than collapsing to Partial.
	default:
		bb.Outcome = string(evolution.Partial)
	}

	// Backstop: a leaf that terminates the tree without success and without
	// ever writing to bb.Result leaves every downstream consumer (DLQ
	// records, OutcomeErrorDetail, dashboards) undiagnosable about which
	// task failed and how. Populate a message naming the task and terminal
	// outcome instead of leaving it blank.
	if bb.Outcome != string(evolution.Success) && bb.Result == "" {
		bb.Result = fmt.Sprintf("task %q produced no output (terminal outcome: %s, code: %d)", bb.Task, bb.Outcome, code)
	}

	span.SetAttribute("outcome", bb.Outcome)
	span.SetAttribute("duration_ms", fmt.Sprintf("%d", bb.DurationMs))

	// Always validate output quality — some trees (agent_monitor, alert_router)
	// don't include ReflectOnOutcome which is where quality scoring normally runs.
	// Without this, zero-LLM trees report quality=0 even with valid structured output.
	//
	// Terminal backstop: trees that terminate without ever routing through
	// outcome() (e.g. compiled GOAP fusion trees — a bare leaf reporting code
	// 1) never get their garbage output caught by OutcomeSelector's quality
	// gate. Flip the outcome here too, unless the result is a recognized
	// zero-LLM structured result (same exemption validateOutputQuality
	// applies), this is a Sandbox structural-evaluation run (whose action
	// stubs intentionally write short non-content placeholders — "[sandbox]
	// <name>" — that quality heuristics have nothing valid to judge), or the
	// tree produced no output at all: a deliberate no-op success marker (the
	// "AlwaysSucceed" node used by guard/preflight composition) has nothing
	// to judge either, unlike a leaf that wrote actual truncated/garbage
	// content.
	qualityOK := validateOutputQuality(bb)
	resolved := resolvedResult(bb)
	if !bb.Sandbox && !qualityOK && bb.Outcome == string(evolution.Success) && resolved != "" && !isStructuredOutput(resolved) {
		bb.Outcome = string(evolution.Failure)
	}

	return bb.Result
}
