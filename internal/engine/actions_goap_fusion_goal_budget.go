package engine

import (
	"strings"

	"github.com/nico/go-bt-evolve/internal/research"
)

// Research-goal failure budget: program milestones block after repeated failed
// attempts (goapProgramMaxMilestoneAttempts), but notebooklm-lane P0 goals had
// no budget at all — on 2026-07-10 one goal burned 11 blind implementation
// attempts on the same lint failure. PrioritizeGoapGoals stamps the head
// research goal it queues; a GENUINE implementation failure (never an
// infrastructure one — see isGoapInfraCycleFailure) charges that goal's
// durable budget, the recorded failure tail steers the next attempt via a
// parse-safe note in the rebuilt plan, and the queue abandons the goal once
// the budget is exhausted. Landing the goal clears its budget.

// goapGoalMaxAttempts bounds how many genuine implementation failures a
// research goal may accumulate before the queue abandons it.
const goapGoalMaxAttempts = 3

// goapGoalAttemptsPath locates the durable goal-attempt budgets (test seam).
var goapGoalAttemptsPath = research.DefaultGoalAttemptsPath()

// goapGoalFailureNoteMarker introduces the failure annotation appended to a
// retried goal line. goapResearchGoalKey strips it, so an annotated goal keys
// identically to its clean form.
const goapGoalFailureNoteMarker = "[PREVIOUS-ATTEMPT-FAILURE:"

// goapResearchGoalKey normalizes a research goal line to its budget key: the
// queue prefixes ("[Pn] ", "NotebookLM research: ") and any failure note are
// stripped so the gaps list, the queue, the plan, and the landed objective all
// key the same goal identically.
func goapResearchGoalKey(line string) string {
	t := strings.TrimSpace(line)
	if i := strings.Index(t, goapGoalFailureNoteMarker); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	for _, p := range []string{"[P0]", "[P1]", "[P2]"} {
		if strings.HasPrefix(t, p) {
			t = strings.TrimSpace(strings.TrimPrefix(t, p))
		}
	}
	t = strings.TrimSpace(strings.TrimPrefix(t, "NotebookLM research:"))
	return research.Key(t)
}

// chargeGoapResearchGoalFailure records one genuine implementation failure
// against the head research goal PrioritizeGoapGoals queued this cycle.
// Idempotent per cycle so a doubled failure path cannot charge twice; a cycle
// that queued no research goal is a no-op. Reports whether a charge landed.
func chargeGoapResearchGoalFailure(bb *Blackboard) bool {
	if bb == nil || bb.ChainState == nil {
		return false
	}
	if done, _ := bb.ChainState["goap_fusion_research_goal_charge_done"].(string); done == "true" {
		return false
	}
	key, _ := bb.ChainState["goap_fusion_research_goal_charged"].(string)
	if strings.TrimSpace(key) == "" {
		return false
	}
	s, err := research.OpenGoalAttempts(goapGoalAttemptsPath)
	if err != nil {
		return false
	}
	attempts := s.RecordFailure(key, strings.TrimSpace(bb.Result))
	if err := s.Save(); err != nil {
		return false
	}
	setGoapState(bb, "research_goal_charge_done", "true")
	if attempts >= goapGoalMaxAttempts {
		Info("goap fusion: research goal abandoned after repeated implementation failures",
			"goal_key", key, "attempts", attempts)
	}
	return true
}

// goapGoalFailureNote returns the parse-safe, single-line failure annotation
// for a goal whose previous attempt failed, or "" for a clean goal. Appended
// to the goal text inside its task section, it steers the retry ("fix this
// explicitly") instead of letting the agent resubmit the same rejected code.
func goapGoalFailureNote(goal string) string {
	s, err := research.OpenGoalAttempts(goapGoalAttemptsPath)
	if err != nil {
		return ""
	}
	fail := strings.TrimSpace(s.LastFailure(goapResearchGoalKey(goal)))
	if fail == "" {
		return ""
	}
	return goapGoalFailureNoteMarker + " " + collapseToSingleLine(truncateGoapTail(fail, 500)) +
		" — fix this explicitly; do not resubmit the same approach]"
}

// goapAbandonedResearchGoal reports whether a research goal's budget is
// exhausted; store read errors degrade to "not abandoned" so a corrupt budget
// file can never silence the whole research lane.
func goapAbandonedResearchGoal(s *research.GoalAttemptStore, goal string) bool {
	if s == nil {
		return false
	}
	return s.Count(goapResearchGoalKey(goal)) >= goapGoalMaxAttempts
}
