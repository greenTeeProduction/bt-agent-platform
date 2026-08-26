package goap

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// LLMClient is the minimal LLM surface the goal factory needs. internal/llm's
// client satisfies it structurally, keeping goap free of platform imports.
type LLMClient interface {
	Generate(prompt string) (string, error)
}

// GoalArchetype is a parameterized goal template. Archetypes are the
// LLM-free degradation path: they always produce vocabulary-grounded,
// plannable goals.
type GoalArchetype string

const (
	ArchetypeAutomateRecurring GoalArchetype = "automate-recurring-task"
	ArchetypeImproveQuality    GoalArchetype = "improve-output-quality"
	ArchetypeReduceTurnaround  GoalArchetype = "reduce-turnaround"
	ArchetypeWatchAndAlert     GoalArchetype = "watch-and-alert"
	ArchetypeCompleteTask      GoalArchetype = "complete-task"
)

// GroundingReport explains how a goal was derived and what the vocabulary
// grounding changed, so MCP callers can surface it to the user.
type GroundingReport struct {
	Source      string            `json:"source"`                 // "llm", "llm_repaired", "archetype"
	Archetype   GoalArchetype     `json:"archetype,omitempty"`    // set when an archetype produced the goal
	MappedKeys  map[string]string `json:"mapped_keys,omitempty"`  // raw key -> canonical key
	DroppedKeys []string          `json:"dropped_keys,omitempty"` // keys rejected as ungrounded
	PlanPreview []string          `json:"plan_preview,omitempty"` // validation plan action names
}

// GoalFactory derives plannable goap.Goal values from free-form user intent
// (LLM-backed) or from mined habit patterns (deterministic). Every emitted
// goal is grounded in the Vocab and verified reachable by Actions from
// InitialState.
type GoalFactory struct {
	LLM               LLMClient // nil → archetype-only mode
	Vocab             *Vocabulary
	Actions           []Action
	InitialState      WorldState
	MaxRepairAttempts int
}

// NewGoalFactory creates a factory over the standard + automation action
// sets. llm may be nil, in which case FromIntent degrades to archetypes.
func NewGoalFactory(llm LLMClient) *GoalFactory {
	actions := append(StandardActions(), AutomationActions()...)
	return &GoalFactory{
		LLM:               llm,
		Vocab:             StandardVocabulary(),
		Actions:           actions,
		InitialState:      DefaultInitialState(),
		MaxRepairAttempts: 2,
	}
}

// DefaultInitialState is the neutral world state plannability is checked
// from: no result yet, general task.
func DefaultInitialState() WorldState {
	return WorldState{"task_type": "general", "has_result": false}
}

// FromIntent extracts a goal from free-form user text. With an LLM it asks
// for structured conditions restricted to the vocabulary, grounds them, and
// retries with feedback when everything was rejected. Without an LLM (or
// when extraction keeps failing) it falls back to a keyword-selected
// archetype. The returned goal is always plannable from InitialState.
func (f *GoalFactory) FromIntent(userText string) (*Goal, *GroundingReport, error) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return nil, nil, fmt.Errorf("goalfactory: empty intent")
	}

	if f.LLM != nil {
		if goal, report, err := f.fromIntentLLM(userText); err == nil {
			return goal, report, nil
		}
	}

	archetype := DetectArchetype(userText)
	goal := f.ArchetypeGoal(archetype, userText, 0.5)
	report := &GroundingReport{Source: "archetype", Archetype: archetype}
	if err := f.validate(goal, report); err != nil {
		return nil, nil, fmt.Errorf("goalfactory: archetype goal not plannable: %w", err)
	}
	return goal, report, nil
}

// FromPattern converts a mined recurring pattern (persona.RecurringPattern
// fields) into an automation goal: the pattern's task should end up running
// as a scheduled automation. Priority grows with the occurrence count and is
// capped below 1.0 so explicit user goals can always outrank habit-derived
// ones.
func (f *GoalFactory) FromPattern(representative string, occurrences int) *Goal {
	priority := math.Round(40+10*float64(occurrences)) / 100
	if priority > 0.9 {
		priority = 0.9
	}
	return &Goal{
		Name:       "automate: " + goalSlug(representative),
		Priority:   priority,
		Conditions: WorldState{"task_automated": true},
	}
}

// ArchetypeGoal instantiates a parameterized goal template. subject is only
// used for naming; priority is clamped to [0,1].
func (f *GoalFactory) ArchetypeGoal(archetype GoalArchetype, subject string, priority float64) *Goal {
	priority = clamp01(priority)
	name := string(archetype) + ": " + goalSlug(subject)
	switch archetype {
	case ArchetypeAutomateRecurring:
		return &Goal{Name: name, Priority: priority, Conditions: WorldState{"task_automated": true}}
	case ArchetypeImproveQuality:
		return &Goal{Name: name, Priority: priority, Conditions: WorldState{"quality_improved": true, "has_result": true}}
	case ArchetypeReduceTurnaround:
		return &Goal{Name: name, Priority: priority, Conditions: WorldState{"turnaround_optimized": true}}
	case ArchetypeWatchAndAlert:
		return &Goal{Name: name, Priority: priority, Conditions: WorldState{"alerts_enabled": true}}
	default:
		return &Goal{Name: name, Priority: priority, Conditions: WorldState{"task_status": "completed", "has_verification": true}}
	}
}

// DetectArchetype picks the archetype template that best matches the intent
// text via keyword scan. Defaults to plain task completion.
func DetectArchetype(text string) GoalArchetype {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "automat", "recurring", "every day", "every week", "daily", "weekly", "schedule", "routine"):
		return ArchetypeAutomateRecurring
	case containsAny(lower, "quality", "better output", "improve", "more accurate", "fewer mistakes"):
		return ArchetypeImproveQuality
	case containsAny(lower, "faster", "turnaround", "speed up", "quicker", "less time"):
		return ArchetypeReduceTurnaround
	case containsAny(lower, "watch", "monitor", "alert", "notify", "keep an eye"):
		return ArchetypeWatchAndAlert
	default:
		return ArchetypeCompleteTask
	}
}

// --- LLM extraction ---

// llmGoal is the JSON shape requested from the model.
type llmGoal struct {
	Name       string         `json:"name"`
	Priority   float64        `json:"priority"`
	Deadline   int            `json:"deadline"`
	Conditions map[string]any `json:"conditions"`
}

func (f *GoalFactory) fromIntentLLM(userText string) (*Goal, *GroundingReport, error) {
	feedback := ""
	attempts := max(f.MaxRepairAttempts, 1)
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		raw, err := f.LLM.Generate(f.extractionPrompt(userText, feedback))
		if err != nil {
			return nil, nil, fmt.Errorf("goalfactory: llm: %w", err)
		}
		parsed, err := parseLLMGoal(raw)
		if err != nil {
			lastErr = err
			feedback = "Your previous reply was not valid JSON: " + err.Error()
			continue
		}

		report := &GroundingReport{Source: "llm"}
		if attempt > 0 {
			report.Source = "llm_repaired"
		}
		conditions := f.groundConditions(parsed.Conditions, report)
		if len(conditions) == 0 {
			lastErr = fmt.Errorf("no grounded conditions (rejected: %s)", strings.Join(report.DroppedKeys, ", "))
			feedback = fmt.Sprintf(
				"Your previous conditions used unknown keys (%s). Use ONLY keys from the allowed list.",
				strings.Join(report.DroppedKeys, ", "))
			continue
		}

		goal := &Goal{
			Name:       parsed.Name,
			Priority:   clamp01(parsed.Priority),
			Conditions: conditions,
			Deadline:   parsed.Deadline,
		}
		if goal.Name == "" {
			goal.Name = "goal: " + goalSlug(userText)
		}
		if goal.Priority == 0 {
			goal.Priority = 0.5
		}
		if goal.Deadline < 0 {
			goal.Deadline = 0
		}
		if err := f.validate(goal, report); err != nil {
			lastErr = err
			feedback = fmt.Sprintf(
				"Your previous conditions (%v) have no achievable action path. Choose conditions an action can produce.",
				conditions)
			continue
		}
		return goal, report, nil
	}
	return nil, nil, fmt.Errorf("goalfactory: extraction failed after %d attempts: %w", attempts, lastErr)
}

func (f *GoalFactory) extractionPrompt(userText, feedback string) string {
	var b strings.Builder
	b.WriteString("Extract a planning goal from the user's intent.\n\n")
	b.WriteString("User intent: ")
	b.WriteString(userText)
	b.WriteString("\n\nAllowed condition keys (use ONLY these):\n")
	b.WriteString(f.Vocab.PromptBlock())
	if feedback != "" {
		b.WriteString("\nFeedback on your previous attempt: ")
		b.WriteString(feedback)
		b.WriteString("\n")
	}
	b.WriteString("\nRespond with ONLY a JSON object, no prose:\n")
	b.WriteString(`{"name": "short goal name", "priority": 0.5, "deadline": 0, "conditions": {"key": value}}`)
	b.WriteString("\npriority is 0-1 (importance). deadline is a step budget, 0 for none. ")
	b.WriteString("conditions are the desired world state (typically booleans or the listed enum strings).")
	return b.String()
}

// groundConditions keeps only conditions whose keys resolve against the
// vocabulary, normalizing bool-ish string values on the way.
func (f *GoalFactory) groundConditions(raw map[string]any, report *GroundingReport) WorldState {
	grounded := make(WorldState, len(raw))
	for key, value := range raw {
		canonical, ok := f.Vocab.Canonical(key)
		if !ok {
			report.DroppedKeys = append(report.DroppedKeys, key)
			continue
		}
		if canonical != key {
			if report.MappedKeys == nil {
				report.MappedKeys = make(map[string]string)
			}
			report.MappedKeys[key] = canonical
		}
		grounded[canonical] = normalizeValue(value)
	}
	return grounded
}

// validate confirms the goal is reachable from InitialState with the
// factory's actions and records the plan preview in the report.
func (f *GoalFactory) validate(goal *Goal, report *GroundingReport) error {
	planner := NewPlanner(f.Actions, 50, 10000)
	plan := planner.Plan(f.InitialState.Clone(), goal)
	if plan == nil {
		return fmt.Errorf("no plan reaches conditions %v", goal.Conditions)
	}
	for _, step := range plan.Steps {
		report.PlanPreview = append(report.PlanPreview, step.Name)
	}
	return nil
}

// parseLLMGoal extracts the first JSON object from an LLM reply (tolerating
// prose and code fences) and unmarshals it.
func parseLLMGoal(raw string) (*llmGoal, error) {
	obj := extractJSONObject(raw)
	if obj == "" {
		return nil, fmt.Errorf("no JSON object in reply")
	}
	var parsed llmGoal
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return nil, fmt.Errorf("invalid goal JSON: %w", err)
	}
	if len(parsed.Conditions) == 0 {
		return nil, fmt.Errorf("goal JSON has no conditions")
	}
	return &parsed, nil
}

// extractJSONObject returns the first balanced {...} block in s, or "".
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// normalizeValue coerces LLM output quirks: "true"/"false" strings become
// bools, integral floats stay floats (JSON default), everything else passes
// through.
func normalizeValue(v any) any {
	if s, ok := v.(string); ok {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "yes":
			return true
		case "false", "no":
			return false
		}
	}
	return v
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0.5
	}
	return math.Min(1, math.Max(0, v))
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// goalSlug compresses free text into a short lowercase identifier fragment.
func goalSlug(text string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) > 6 {
		fields = fields[:6]
	}
	slug := strings.Join(fields, "_")
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "goal"
	}
	return b.String()
}
