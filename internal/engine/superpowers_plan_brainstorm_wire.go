package engine

import (
	"context"
	"time"
)

// brainstormMaxWait bounds the plan-expansion LLM call. Plan expansion is a
// best-effort enrichment: if it does not return promptly the deterministic
// plan is used, so the cycle is never blocked on it.
const brainstormMaxWait = 6 * time.Minute

// WireGoalPlanBrainstorm installs the Claude-backed plan-expansion seam. It is
// called from the production wiring layer (internal/agentexec), NOT from an
// engine init(), so engine unit tests keep goalPlanBrainstormFn nil (offline,
// deterministic) and never invoke a real LLM.
func WireGoalPlanBrainstorm() {
	goalPlanBrainstormFn = func(task string, goals []string, deterministicPlan string) string {
		ctx, cancel := context.WithTimeout(context.Background(), brainstormMaxWait)
		defer cancel()
		prompt := buildGoalBrainstormPrompt(task, goals, deterministicPlan)
		res := defaultSuperpowersClaudeRunner.RunClaude(ctx, goapFusionRepo, prompt)
		if res.Err != nil {
			return ""
		}
		return res.Output
	}
}

// GoalPlanBrainstormWired reports whether the plan-expansion seam is
// installed — used by the binary-level wiring test to guard against the
// seam being dropped from the production link.
func GoalPlanBrainstormWired() bool { return goalPlanBrainstormFn != nil }
