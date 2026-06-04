package domains

import "github.com/nico/go-bt-evolve/internal/evolution"

// BTManagerTree is a meta-cognitive behavior tree that runs after other BT agents
// to analyze failures, detect degradation, and apply targeted improvements.
//
// Strategy paths:
//  1. DegradedPerformance: agent success rate dropped below threshold → diagnose + fix
//  2. NewAgentBootstrap: agent is new (< 5 runs) → analyze early patterns + tune
//  3. Idle: everything healthy → report status, nothing to fix
//
// The tree is designed to be fast (< 2s) with zero LLM calls — all analysis
// is rule-based using reflection records and fitness metrics.
func BTManagerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "BTManager_Main",
		Children: []evolution.SerializableNode{
			// ── PreGate: ensure we have data to work with ──
			seq("BTManager_PreGate",
				cond("ValidateInput", "Task must be non-empty"),
				cond("HasReflectionStore", "Reflection store must be available"),
			),

			// ── Strategy Router ──
			sel("BTManager_StrategyRouter",
				// Path 1: Degraded Performance — success rate below threshold
				seq("DegradedPerformancePath",
					cond("IsDegradedAgent", "Success rate < 0.7 OR 3+ consecutive failures OR circuit breaker open"),
					act("AnalyzeFailurePatterns", "Parse reflection records to identify dominant failure mode: timeout, LLM error, parse error, empty response, tool error"),
					act("ApplyTargetedMutation", "Apply the right fix: increase retries for timeouts, add fallback for LLM errors, add precondition gate for parse errors, increase tool timeout for tool errors"),
					act("RecordIntervention", "Log the intervention to reflection store with agent name, failure mode, mutation applied"),
				),

				// Path 2: New agent — fewer than 5 runs, needs bootstrapping
				seq("NewAgentBootstrapPath",
					cond("IsNewAgent", "Agent has < 5 total runs"),
					act("CheckInitialQuality", "Verify first runs have quality > 0.3"),
					act("BootstrapRetryConfig", "Set conservative defaults: 3 retries, 60s timeout, fallback enabled"),
					act("RecordBootstrap", "Log bootstrap configuration to reflection store"),
				),

				// Path 3: Everything OK — nothing to fix
				seq("HealthyReportPath",
					cond("IsHealthy", "Success rate ≥ 0.7 and no circuit breakers open"),
					act("ReportHealth", "Report agent health summary: N agents OK, 0 interventions needed"),
				),
			),

			// ── Outcome ──
			outcome(),
		},
	}
}
