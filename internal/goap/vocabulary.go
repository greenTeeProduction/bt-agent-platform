package goap

import (
	"maps"
	"slices"
	"strings"
)

// Vocabulary is the world-state key registry that grounds goal extraction
// (ADR-133 Phase 2): a goal condition is only accepted when its key is a
// known world-state key some action can actually affect, so every goal the
// factory emits is plannable rather than hallucinated.
type Vocabulary struct {
	descriptions map[string]string
}

// NewVocabulary creates an empty vocabulary.
func NewVocabulary() *Vocabulary {
	return &Vocabulary{descriptions: make(map[string]string)}
}

// Add registers a world-state key with a short description used in prompts.
// Adding an existing key overwrites its description.
func (v *Vocabulary) Add(key, description string) {
	key = normalizeKey(key)
	if key == "" {
		return
	}
	v.descriptions[key] = description
}

// AddFromActions registers every precondition and effect key of the actions,
// so the vocabulary always covers exactly what the planner can manipulate.
func (v *Vocabulary) AddFromActions(actions ...Action) {
	for _, a := range actions {
		for key := range a.Preconditions {
			if _, exists := v.descriptions[normalizeKey(key)]; !exists {
				v.Add(key, "precondition of "+a.Name)
			}
		}
		for key := range a.Effects {
			if _, exists := v.descriptions[normalizeKey(key)]; !exists {
				v.Add(key, "effect of "+a.Name)
			}
		}
	}
}

// Has reports whether key (after normalization) is registered.
func (v *Vocabulary) Has(key string) bool {
	_, ok := v.descriptions[normalizeKey(key)]
	return ok
}

// Canonical maps a raw key to its registered form: exact match after
// normalization, else a unique substring/superstring match ("automated" →
// "task_automated"). Ambiguous or unknown keys return ok=false — grounding
// rejects them rather than guessing between candidates.
func (v *Vocabulary) Canonical(raw string) (string, bool) {
	key := normalizeKey(raw)
	if key == "" {
		return "", false
	}
	if _, ok := v.descriptions[key]; ok {
		return key, true
	}
	var candidates []string
	for known := range v.descriptions {
		if strings.Contains(known, key) || strings.Contains(key, known) {
			candidates = append(candidates, known)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return "", false
}

// Keys returns all registered keys, sorted.
func (v *Vocabulary) Keys() []string {
	keys := slices.Sorted(maps.Keys(v.descriptions))
	return keys
}

// PromptBlock renders the vocabulary as a prompt fragment listing every
// allowed condition key with its description.
func (v *Vocabulary) PromptBlock() string {
	var b strings.Builder
	for _, key := range v.Keys() {
		b.WriteString("- ")
		b.WriteString(key)
		if desc := v.descriptions[key]; desc != "" {
			b.WriteString(": ")
			b.WriteString(desc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// StandardVocabulary covers the standard task actions plus the automation
// pipeline actions, with curated descriptions for the load-bearing keys.
func StandardVocabulary() *Vocabulary {
	v := NewVocabulary()
	v.Add("task_type", `kind of task: "build", "test", "deploy", "research", "fix", or "general"`)
	v.Add("task_status", `lifecycle state; goals usually want "completed"`)
	v.Add("has_result", "a concrete task result exists (bool)")
	v.Add("has_verification", "the result passed verification (bool)")
	v.Add("task_automated", "a recurring task runs as a scheduled automation (bool)")
	v.Add("quality_improved", "output quality was actively improved (bool)")
	v.Add("turnaround_optimized", "task turnaround time was reduced (bool)")
	v.Add("alerts_enabled", "a watcher raises alerts on relevant changes (bool)")
	v.AddFromActions(StandardActions()...)
	v.AddFromActions(AutomationActions()...)
	return v
}

// normalizeKey lowercases and converts separators so LLM variants like
// "Task-Automated" or "task automated" ground to "task_automated".
func normalizeKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, "-", "_")
	return key
}

// AutomationActions model the personalization pipeline (ADR-133 Phases 3–5)
// as planner operators, giving goals like task_automated=true a real action
// path: detect pattern → compile tree → HITL approval → schedule agent.
// Quality/turnaround/watcher operators back the remaining goal archetypes.
func AutomationActions() []Action {
	return []Action{
		{
			Name:          "detect_recurring_pattern",
			Cost:          0.5,
			Preconditions: WorldState{},
			Effects:       WorldState{"pattern_detected": true},
		},
		{
			Name:          "compile_automation_tree",
			Cost:          2.0,
			Preconditions: WorldState{"pattern_detected": true},
			Effects:       WorldState{"automation_tree_ready": true},
		},
		{
			Name:          "propose_automation_hitl",
			Cost:          1.0,
			Preconditions: WorldState{"automation_tree_ready": true},
			Effects:       WorldState{"automation_approved": true},
		},
		{
			Name:          "schedule_automation_agent",
			Cost:          1.0,
			Preconditions: WorldState{"automation_approved": true},
			Effects:       WorldState{"task_automated": true},
		},
		{
			Name:          "improve_output_quality",
			Cost:          1.5,
			Preconditions: WorldState{"has_result": true},
			Effects:       WorldState{"quality_improved": true},
		},
		{
			Name:          "optimize_turnaround",
			Cost:          1.5,
			Preconditions: WorldState{"has_result": true},
			Effects:       WorldState{"turnaround_optimized": true},
		},
		{
			Name:          "setup_watcher",
			Cost:          1.0,
			Preconditions: WorldState{},
			Effects:       WorldState{"watcher_active": true},
		},
		{
			Name:          "enable_alerts",
			Cost:          0.5,
			Preconditions: WorldState{"watcher_active": true},
			Effects:       WorldState{"alerts_enabled": true},
		},
	}
}
