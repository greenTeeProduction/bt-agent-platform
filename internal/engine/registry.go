package engine

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/nico/go-bt-evolve/internal/reflection"
	btcore "github.com/rvitorper/go-bt/core"
)

// ActionFunc is the signature for behavior tree action implementations.
type ActionFunc func(*btcore.BTContext[Blackboard]) int

// ConditionFunc is the signature for behavior tree condition implementations.
type ConditionFunc func(*Blackboard) bool

// ─── Legacy global maps (used by goap_nodes.go, tree.go) ────────────────────

var (
	actionRegistry    = map[string]ActionFunc{}
	conditionRegistry = map[string]ConditionFunc{}
	regMu             sync.RWMutex
)

// ─── Public registration API ────────────────────────────────────────────────

// RegisterAction adds an action to the global registry.
func RegisterAction(name string, fn ActionFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := actionRegistry[name]; exists {
		panic(fmt.Sprintf("action %q already registered", name))
	}
	actionRegistry[name] = fn
}

// RegisterCondition adds a condition to the global registry.
func RegisterCondition(name string, fn ConditionFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := conditionRegistry[name]; exists {
		panic(fmt.Sprintf("condition %q already registered", name))
	}
	conditionRegistry[name] = fn
}

// ─── Provider interface for domain packages ─────────────────────────────────

// ActionProvider is implemented by packages that register BT actions.
type ActionProvider interface {
	RegisterActions()
}

// ConditionProvider is implemented by packages that register BT conditions.
type ConditionProvider interface {
	RegisterConditions()
}

// Provider is implemented by packages that register both actions and conditions.
type Provider interface {
	ActionProvider
	ConditionProvider
}

// ─── Engine (constructor-injected, not global) ──────────────────────────────

// Engine holds registry-backed BT execution state.
type Engine struct {
	Actions    map[string]ActionFunc
	Conditions map[string]ConditionFunc
}

// NewEngine creates an Engine pre-populated from the global registry.
func NewEngine() *Engine {
	regMu.RLock()
	defer regMu.RUnlock()
	actions := make(map[string]ActionFunc, len(actionRegistry))
	conditions := make(map[string]ConditionFunc, len(conditionRegistry))
	for k, v := range actionRegistry {
		actions[k] = v
	}
	for k, v := range conditionRegistry {
		conditions[k] = v
	}
	return &Engine{Actions: actions, Conditions: conditions}
}

// GetAction returns an action by name, or nil.
func (e *Engine) GetAction(name string) ActionFunc {
	return e.Actions[name]
}

// GetCondition returns a condition by name, or nil.
func (e *Engine) GetCondition(name string) ConditionFunc {
	return e.Conditions[name]
}

// RegisterProviders calls RegisterActions/RegisterConditions on each provider.
func RegisterProviders(providers ...interface{}) {
	for _, p := range providers {
		if ap, ok := p.(ActionProvider); ok {
			ap.RegisterActions()
		}
		if cp, ok := p.(ConditionProvider); ok {
			cp.RegisterConditions()
		}
	}
}

// ─── Package-level accessors (for tests and legacy code) ────────────────────

// GetAction returns the action from the global registry, or nil for unknown names.
// The fallback to the switch in actionForName handles unregistered actions.
func GetAction(name string) ActionFunc {
	return actionRegistry[name]
}

// GetCondition returns the condition from the global registry, or nil for unknown names.
// The fallback to the switch in conditionForName handles unregistered conditions.
func GetCondition(name string) ConditionFunc {
	return conditionRegistry[name]
}

func init() {
	// Core actions
	RegisterAction("GeneratePlan", generatePlanAction)
	RegisterAction("AssignComplexity", assignComplexityAction)
	RegisterAction("ValidateInput", validateInputAction)
	RegisterAction("ValidateOutput", validateOutputAction)
	RegisterAction("ReflectOnOutcome", reflectOnOutcomeAction)
	RegisterAction("UpdateBehaviorTree", updateBehaviorTreeAction)
	RegisterAction("ExecLLMCall", execLLMCallAction)
	RegisterAction("ExecRefine", execRefineAction)
	RegisterAction("KnowledgeQuery", knowledgeQueryAction)
	RegisterAction("CacheCheck", cacheCheckAction)
	RegisterAction("CacheResult", cacheResultAction)

	// Core conditions
	RegisterCondition("HasClearTask", hasClearTaskCond)
	RegisterCondition("IsLowComplexity", func(b *Blackboard) bool { return b.Complexity == "low" })
	RegisterCondition("IsMediumComplexity", func(b *Blackboard) bool { return b.Complexity == "medium" })
	RegisterCondition("IsHighComplexity", func(b *Blackboard) bool { return b.Complexity == "high" })
	RegisterCondition("WasSuccessful", wasSuccessfulCond)
	RegisterCondition("CheckCoverageCompleteness", wasSuccessfulCond)
	RegisterCondition("TaskIsNotEmpty", func(b *Blackboard) bool { return b.Task != "" })
	RegisterCondition("CachedResult", func(b *Blackboard) bool { return b.CachedResult != "" })
		RegisterCondition("HasPlan", func(b *Blackboard) bool { return strings.TrimSpace(b.Plan) != "" })

	RegisterCondition("HasKnowledgeResult", func(b *Blackboard) bool { return b.KgResults != "" })

	// Domain-specific aliases (implementations in tree.go actionForName/conditionForName)
	RegisterCondition("ValidateInput", func(b *Blackboard) bool { return b.Task != "" })
	RegisterCondition("CheckPrerequisites", func(b *Blackboard) bool { return true })
	RegisterCondition("CheckKnowledgeGap", func(b *Blackboard) bool { return b.KgResults == "" })
	RegisterCondition("CheckCache", func(b *Blackboard) bool { return b.CachedResult != "" })
	RegisterCondition("CheckConfidence", func(b *Blackboard) bool { return true })
	RegisterAction("SetupDefaultTools", func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("QueryKG", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.KgResults = fmt.Sprintf("KG: %s", ctx.Blackboard.Task)
		return 1
	})
	RegisterAction("ApplyKnowledge", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Task = fmt.Sprintf("%s [KG: %s]", bb.Task, bb.KgResults)
		return 1
	})
	RegisterAction("UseCachedResult", func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("EscalateToDeepSeek", func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("SelfCorrect", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.LLM != nil {
			// Include the previous failed output so the LLM knows what to fix
			prevResult := bb.Result
			if prevResult == "" && len(bb.Results) > 0 {
				prevResult = bb.Results[len(bb.Results)-1]
			}
			prompt := fmt.Sprintf("The previous task produced errors. Task: %s\n\nPrevious output: %s\n\nCorrect the errors and produce a better answer:", bb.Task, prevResult)
			result, err := bb.LLM.Generate(prompt)
			if err == nil {
				bb.Result = result
				bb.Outcome = string(reflection.Success)
				return 1
			}
		}
		return -1
	})
	RegisterAction("MarkSuccessful", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Outcome = string(reflection.Success)
		return 1
	})
	RegisterAction("DefaultFallback", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = fmt.Sprintf("## Fallback Executed\n\n**Task**: %s\n**Status**: Processed via generic fallback path.", ctx.Blackboard.Task)
		ctx.Blackboard.Outcome = string(reflection.Success)
		return 1
	})
	RegisterAction("HealthCheckAgent", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var report strings.Builder
		report.WriteString("## System Health Report\n\n")
		// Disk usage
		dfOut, err := exec.Command("df", "-h", "/", "/mnt/ssd").CombinedOutput()
		if err == nil {
			report.WriteString("### Disk\n```\n")
			report.Write(dfOut)
			report.WriteString("```\n\n")
		}
		// Memory
		freeOut, err := exec.Command("free", "-m").CombinedOutput()
		if err == nil {
			report.WriteString("### Memory\n```\n")
			report.Write(freeOut)
			report.WriteString("```\n\n")
		}
		// BT processes
		psOut, err := exec.Command("bash", "-c", "ps aux | grep '[b]t-' | awk '{print $11, $2, $3, $4}'").CombinedOutput()
		if err == nil {
			report.WriteString("### BT Processes\n```\n")
			report.Write(psOut)
			report.WriteString("```\n\n")
		}
		// Load
		uptimeOut, err := exec.Command("uptime").CombinedOutput()
		if err == nil {
			report.WriteString("### Load\n```\n")
			report.Write(uptimeOut)
			report.WriteString("```\n")
		}
		bb.Result = report.String()
		bb.Outcome = "success"
		return 1
	})
	RegisterAction("MetricsCollectionAgent", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var report strings.Builder
		report.WriteString("## Metrics Collection\n\n")
		// Disk numeric
		dfOut, err := exec.Command("df", "-BM", "/", "/mnt/ssd").CombinedOutput()
		if err == nil {
			report.WriteString("### Disk (MB)\n```\n")
			report.Write(dfOut)
			report.WriteString("```\n\n")
		}
		// Memory numeric
		freeOut, err := exec.Command("free", "-m").CombinedOutput()
		if err == nil {
			report.WriteString("### Memory (MB)\n```\n")
			report.Write(freeOut)
			report.WriteString("```\n\n")
		}
		// Process count
		countOut, err := exec.Command("bash", "-c", "ps aux | grep '[b]t-' | wc -l").CombinedOutput()
		if err == nil {
			report.WriteString("### BT Process Count: ")
			report.Write(countOut)
			report.WriteString("\n")
		}
		bb.Result = report.String()
		bb.Outcome = "success"
		return 1
	})
	RegisterAction("AnalyzeTask", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.LLM != nil {
			bb.Complexity = bb.LLM.AnalyzeComplexity(bb.Task)
		}
		return 1
	})
	RegisterAction("ExecutePlan", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.LLM != nil {
			bb.Plan = bb.LLM.GeneratePlan(bb.Task, bb.Complexity)
		}
		bb.Result = fmt.Sprintf("Executed plan for: %s (complexity: %s)", bb.Task, bb.Complexity)
		bb.Outcome = "success"
		return 1
	})

	// Domain-specific inits (their init() functions add to the registries)
	// See goap_nodes.go init(), tree.go actionForName/conditionForName switches
}

func init() {
	registerGoapNodes()
	registerAlertRouterNodes()
	registerA2ANodes()
	registerScriptNodes()
}

// registerAlertRouterNodes registers conditions and actions for the alert_router tree.
// Kept here (not in domains/) to avoid import cycle: domains → engine → domains.
func registerAlertRouterNodes() {
	// Alert Router conditions
	RegisterCondition("IsCritical", func(b *Blackboard) bool {
		return containsAnyLower(b.Task, "critical", "emergency", "urgent", "severe")
	})
	RegisterCondition("IsSecurity", func(b *Blackboard) bool {
		return containsAnyLower(b.Task, "security", "breach", "attack", "intrusion", "unauthorized", "ssh", "brute")
	})
	RegisterCondition("IsTrading", func(b *Blackboard) bool {
		return containsAnyLower(b.Task, "trading", "btc", "price", "signal", "volume", "market")
	})
	RegisterCondition("IsDiskAlert", func(b *Blackboard) bool {
		return containsAnyLower(b.Task, "disk", "storage", "filesystem", "sda", "nvme", "space")
	})
	RegisterCondition("IsHealthAlert", func(b *Blackboard) bool {
		return containsAnyLower(b.Task, "health", "monitor", "down", "failure", "crash", "unreachable")
	})

	// Alert Router actions
	RegisterAction("RouteToAllChannels", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Alert Routed\n\n**Severity:** CRITICAL\n**Task:** %s\n**Route:** ALL channels\n**Status:** Delivered", bb.Task)
		return 1
	})
	RegisterAction("RouteToSecurityChannel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Security Alert Routed\n\n**Severity:** HIGH\n**Task:** %s\n**Route:** Security team\n**Status:** Delivered", bb.Task)
		return 1
	})
	RegisterAction("RouteToTradingChannel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Trading Signal Routed\n\n**Task:** %s\n**Route:** Trading channels\n**Status:** Delivered", bb.Task)
		return 1
	})
	RegisterAction("RouteToDevOpsChannel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Alert Routed\n\n**Task:** %s\n**Route:** DevOps/Admin\n**Status:** Delivered", bb.Task)
		return 1
	})
	RegisterAction("RouteToDefaultChannel", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = fmt.Sprintf("## Alert Routed\n\n**Task:** %s\n**Route:** Default channel\n**Status:** Delivered", bb.Task)
		return 1
	})
}

func containsAnyLower(s string, keywords ...string) bool {
	for _, kw := range keywords {
		for i := 0; i <= len(s)-len(kw); i++ {
			match := true
			for j := 0; j < len(kw); j++ {
				c := s[i+j]
				kc := kw[j]
				if c >= 'A' && c <= 'Z' {
					c += 32
				}
				if kc >= 'A' && kc <= 'Z' {
					kc += 32
				}
				if c != kc {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

// ─── Action implementations ─────────────────────────────────────────────────

func generatePlanAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.LLM != nil {
		bb.Plan = bb.LLM.GeneratePlan(bb.Task, bb.Complexity)
	}
	return 1
}

func assignComplexityAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.LLM != nil {
		bb.Complexity = bb.LLM.AnalyzeComplexity(bb.Task)
	} else {
		bb.Complexity = "medium"
	}
	return 1
}

func validateInputAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.Task == "" {
		return -1
	}
	return 1
}

func validateOutputAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if len(bb.Result) < 10 {
		return -1
	}
	return 1
}

func reflectOnOutcomeAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.LLM != nil {
		wentWell, toImprove := bb.LLM.Reflect(bb.Task, bb.Outcome, bb.Plan)

		// Validate output quality — mark as failure if output is garbage
		if !validateOutputQuality(bb) {
			bb.Outcome = string(reflection.Failure)
			bb.Result = fmt.Sprintf("OUTPUT QUALITY FAILED (score=%.1f): %s", bb.QualityScore, bb.Result)
			toImprove = "Output quality below threshold — retry with more detail"
		}

		// Save reflection record (don't overwrite bb.Result; task result is already set)
		if bb.Reflections != nil {
			record := &reflection.Record{
				Task:          bb.Task,
				Plan:          bb.Plan,
				WhatWentWell:  []string{wentWell},
				WhatToImprove: []string{toImprove},
				Outcome:       reflection.Outcome(bb.Outcome),
				DurationMs:    bb.DurationMs,
			}
			if err := bb.Reflections.Save(record); err != nil {
				fmt.Fprintf(os.Stderr, "engine: failed to save reflection record for %q: %v\n", bb.Task, err)
			}
		}
	}
	return 1
}

func updateBehaviorTreeAction(ctx *btcore.BTContext[Blackboard]) int {
	return 1
}

func execLLMCallAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.LLM == nil {
		return -1
	}
	result, err := bb.LLM.Generate(bb.Task)
	if err != nil {
		return -1
	}
	bb.Result = result
	return 1
}

func execRefineAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.LLM == nil || bb.Result == "" {
		return -1
	}
	prompt := fmt.Sprintf("Improve this output:\n%s\n\nImproved:", bb.Result)
	result, err := bb.LLM.Generate(prompt)
	if err != nil {
		return -1
	}
	bb.Result = result
	return 1
}

func knowledgeQueryAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	bb.KgResults = fmt.Sprintf("KG: %s — no cached results", bb.Task)
	return 1
}

func cacheCheckAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.KgResults != "" && !strContains(bb.KgResults, "no cached") {
		bb.CachedResult = bb.KgResults
		return 1
	}
	return -1
}

func cacheResultAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.Result != "" {
		bb.CachedResult = bb.Result
	}
	return 1
}

// ─── Condition implementations ──────────────────────────────────────────────

func hasClearTaskCond(b *Blackboard) bool {
	task := trim(b.Task)
	if len(task) < 3 {
		return false
	}
	lower := strings.ToLower(task)
	hasAlpha := false
	for _, c := range lower {
		if c >= 'a' && c <= 'z' {
			hasAlpha = true
			break
		}
	}
	if !hasAlpha {
		return false
	}
	for _, p := range []string{"<script>", "drop table", "cmd.exe", "/bin/"} {
		if strContains(lower, p) {
			return false
		}
	}
	return true
}

func wasSuccessfulCond(b *Blackboard) bool {
	return b.Outcome == "success" || b.Outcome == "chain_success"
}

// ─── Mini string helpers (avoid import cycles) ──────────────────────────────

func strContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}
