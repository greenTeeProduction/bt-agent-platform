package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// registerGoalTools registers the per-user goal factory tools (ADR-010
// Phase 2): extracting grounded goap goals from free-form intent or mined
// habit patterns, and managing the persistent per-user goal queue.
func registerGoalTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_goal_add", "Extract a grounded GOAP goal from free-form intent and add it to the user's goal queue",
		map[string]engine.Property{
			"user":     {Type: "string", Description: "User ID (persona owner)"},
			"intent":   {Type: "string", Description: "Free-form description of what should be achieved"},
			"priority": {Type: "number", Description: "Override priority 0-1 (default: extracted from intent)"},
			"deadline": {Type: "number", Description: "Override step deadline, 0 = none"},
		},
		[]string{"user", "intent"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User     string   `json:"user"`
				Intent   string   `json:"intent"`
				Priority *float64 `json:"priority"`
				Deadline *int     `json:"deadline"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			store, res := userGoalStore(deps, params.User)
			if res != nil {
				return res
			}

			goal, report, err := goalFactory(deps).FromIntent(params.Intent)
			if err != nil {
				return goalError(err.Error())
			}
			if params.Priority != nil {
				goal.Priority = *params.Priority
			}
			if params.Deadline != nil && *params.Deadline >= 0 {
				goal.Deadline = *params.Deadline
			}
			if err := store.Add(goal); err != nil {
				return goalError(err.Error())
			}
			data, _ := json.Marshal(map[string]interface{}{
				"added":     true,
				"goal":      goal,
				"grounding": report,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_goal_list", "List a user's persisted goals in queue order (priority desc, earlier deadline first)",
		map[string]engine.Property{
			"user": {Type: "string", Description: "User ID (persona owner)"},
		},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User string `json:"user"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			store, res := userGoalStore(deps, params.User)
			if res != nil {
				return res
			}
			queue, err := store.Queue()
			if err != nil {
				return goalError(err.Error())
			}
			goals := queue.All()
			if goals == nil {
				goals = []*goap.Goal{}
			}
			var next string
			if selected := queue.SelectGoal(goap.WorldState{}); selected != nil {
				next = selected.Name
			}
			data, _ := json.Marshal(map[string]interface{}{
				"user":  params.User,
				"count": len(goals),
				"next":  next,
				"goals": goals,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_goal_remove", "Remove a goal by name from the user's goal queue",
		map[string]engine.Property{
			"user": {Type: "string", Description: "User ID (persona owner)"},
			"name": {Type: "string", Description: "Goal name as shown by bt_goal_list"},
		},
		[]string{"user", "name"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User string `json:"user"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			store, res := userGoalStore(deps, params.User)
			if res != nil {
				return res
			}
			removed, err := store.Remove(params.Name)
			if err != nil {
				return goalError(err.Error())
			}
			data, _ := json.Marshal(map[string]interface{}{"removed": removed, "name": params.Name})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_goal_compile", "Compile a stored goal into a persistent behavior tree: plan with GOAP, compile the plan to a guarded BT, validate, and persist it",
		map[string]engine.Property{
			"user": {Type: "string", Description: "User ID (persona owner)"},
			"name": {Type: "string", Description: "Goal name as shown by bt_goal_list"},
		},
		[]string{"user", "name"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User string `json:"user"`
				Name string `json:"name"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			store, res := userGoalStore(deps, params.User)
			if res != nil {
				return res
			}
			goals, err := store.Load()
			if err != nil {
				return goalError(err.Error())
			}
			var goal *goap.Goal
			for _, g := range goals {
				if g.Name == params.Name {
					goal = g
					break
				}
			}
			if goal == nil {
				return goalError(fmt.Sprintf("goal %q not found for user %q; run bt_goal_list", params.Name, params.User))
			}

			// Plan with the same action set the goal factory grounds against,
			// so a stored goal always has an action path.
			f := goalFactory(deps)
			planner := goap.NewPlanner(f.Actions, 50, 10000)
			plan := planner.Plan(f.InitialState.Clone(), goal)
			if plan == nil {
				return goalError(fmt.Sprintf("no plan reaches goal %q from the initial state", goal.Name))
			}
			if len(plan.Steps) == 0 {
				return goalError(fmt.Sprintf("goal %q is already satisfied by the initial state; nothing to compile", goal.Name))
			}

			treeID := "goal:" + goalTreeSlug(goal.Name)
			goapNode, err := goap.CompilePlanToTree(plan, goap.CompileOptions{
				TreeName:     treeID,
				InitialState: f.InitialState,
				KnownAction:  func(name string) bool { return engine.GetAction(name) != nil },
				StyleHints:   personaStyleHints(deps, params.User),
				Provenance: map[string]interface{}{
					"user":          params.User,
					"goal_priority": goal.Priority,
				},
			})
			if err != nil {
				return goalError(err.Error())
			}
			tree := evolution.FromGoapNode(goapNode)

			steps := make([]string, 0, len(plan.Steps))
			for _, s := range plan.Steps {
				steps = append(steps, s.Name)
			}
			result := map[string]interface{}{
				"tree_id":    treeID,
				"goal":       goal.Name,
				"plan":       steps,
				"plan_cost":  plan.Cost,
				"plan_hash":  goap.PlanHash(plan),
				"node_count": evolution.CountNodes(tree),
			}
			persistGeneratedTreeForUser(deps, params.User, treeID, tree, result)

			// Seed the tree's first reflection so the gardener's evidence
			// gate doesn't freeze it before its first real run (Phase 5).
			if result["persisted"] == true {
				seedCompileReflection(deps, params.User, treeID, goal.Name, steps)
				result["seeded_reflection"] = true
			}

			// Register in the knowledge graph so discovery and evolution see it.
			if result["persisted"] == true && deps.kg != nil {
				deps.kg.Register(&knowledge.TreeMeta{
					ID:          treeID,
					Name:        treeID,
					Category:    "goal",
					Description: "Compiled from GOAP plan for goal: " + goal.Name,
					NodeCount:   evolution.CountNodes(tree),
					Keywords:    strings.Fields(strings.ToLower(goal.Name)),
					Capabilities: []knowledge.Capability{
						{Action: "goal_automation", Domain: "goal", Strength: 0.7},
					},
				})
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_goal_from_pattern", "Mine the user's recurring task patterns and turn one into an automation goal",
		map[string]engine.Property{
			"user":            {Type: "string", Description: "User ID (persona owner)"},
			"pattern":         {Type: "number", Description: "Index into the mined pattern list (default 0 = most frequent)"},
			"min_occurrences": {Type: "number", Description: "Cluster size that makes a pattern recurring (default 3)"},
			"window_days":     {Type: "number", Description: "How many days back to consider (default 14)"},
		},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User           string  `json:"user"`
				Pattern        int     `json:"pattern"`
				MinOccurrences int     `json:"min_occurrences"`
				WindowDays     float64 `json:"window_days"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			store, res := userGoalStore(deps, params.User)
			if res != nil {
				return res
			}

			patterns, _, err := mineUserPatterns(deps, params.User, params.MinOccurrences, params.WindowDays, true)
			if err != nil {
				return goalError(err.Error())
			}
			if len(patterns) == 0 {
				return goalError("no recurring patterns found; run more tasks with this user or lower min_occurrences")
			}
			if params.Pattern < 0 || params.Pattern >= len(patterns) {
				return goalError(fmt.Sprintf("pattern index %d out of range (found %d patterns)", params.Pattern, len(patterns)))
			}
			pattern := patterns[params.Pattern]

			goal := goalFactory(deps).FromPattern(pattern.Representative, pattern.Count)
			if err := store.Add(goal); err != nil {
				return goalError(err.Error())
			}
			data, _ := json.Marshal(map[string]interface{}{
				"added":   true,
				"goal":    goal,
				"pattern": pattern,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}

// goalFactory builds a goal factory over the shared LLM client. When the
// health monitor reports Ollama as down the factory runs LLM-free and
// degrades to archetype templates instead of stalling on a dead backend.
func goalFactory(deps *mcpDeps) *goap.GoalFactory {
	var client goap.LLMClient
	if deps.llmClient != nil && (deps.llmHealth == nil || deps.llmHealth.IsHealthy()) {
		client = deps.llmClient
	}
	return goap.NewGoalFactory(client)
}

// userGoalStore opens the per-user goal store at users/<user>/goals/.
// Returns a non-nil ToolResult on error, ready to hand back to the caller.
func userGoalStore(deps *mcpDeps, user string) (*goap.GoalStore, *engine.ToolResult) {
	if deps.personaStore == nil {
		return nil, goalError("persona store not configured")
	}
	if strings.TrimSpace(user) == "" {
		return nil, goalError("user must not be empty")
	}
	store, err := goap.NewGoalStore(deps.personaStore.Workspace(user).GoalsDir())
	if err != nil {
		return nil, goalError(err.Error())
	}
	return store, nil
}

// personaStyleHints derives prompt style hints from the user's profile so
// compiled ChainAction prompts respect preferences (ADR-010 Phase 3
// preference-aware generation). Best-effort: no profile → no hints.
func personaStyleHints(deps *mcpDeps, user string) string {
	if deps.personaStore == nil || strings.TrimSpace(user) == "" {
		return ""
	}
	profile, err := deps.personaStore.Load(user)
	if err != nil {
		return ""
	}
	var hints []string
	if profile.PreferredStyle != "" {
		hints = append(hints, "Preferred output style: "+profile.PreferredStyle+".")
	}
	hints = append(hints, profile.PromptHints...)
	return strings.Join(hints, " ")
}

// goalTreeSlug compresses a goal name into a tree-ID fragment.
func goalTreeSlug(name string) string {
	fields := strings.Fields(strings.ToLower(name))
	if len(fields) > 5 {
		fields = fields[:5]
	}
	var b strings.Builder
	for _, f := range fields {
		var part strings.Builder
		for _, r := range f {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
				part.WriteRune(r)
			}
		}
		if part.Len() == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('_')
		}
		b.WriteString(part.String())
	}
	if b.Len() == 0 {
		return "goal"
	}
	return b.String()
}

func goalError(msg string) *engine.ToolResult {
	return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, msg)}}}
}
