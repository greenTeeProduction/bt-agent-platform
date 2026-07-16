package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// registerPersonaTools registers the per-user personalization tools
// (ADR-010 Phase 1): profile inspection, preference updates, and habit
// pattern mining over the user's interaction log.
func registerPersonaTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_persona_get", "Get a user's personalization profile and workspace layout",
		map[string]engine.Property{"user": {Type: "string", Description: "User ID (persona owner)"}},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User string `json:"user"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return personaError(err.Error())
			}
			if deps.personaStore == nil {
				return personaError("persona store not configured")
			}
			profile, err := deps.personaStore.Load(params.User)
			if err != nil {
				return personaError(err.Error())
			}
			ws := deps.personaStore.Workspace(params.User)
			data, _ := json.Marshal(map[string]interface{}{
				"profile": profile,
				"workspace": map[string]string{
					"root":         ws.Root,
					"trees":        ws.TreesDir(),
					"goals":        ws.GoalsDir(),
					"memory":       ws.MemoryDir(),
					"reflections":  ws.ReflectionsDir(),
					"experience":   ws.ExperienceDir(),
					"interactions": ws.InteractionsPath(),
				},
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_persona_set_preference", "Update a user's profile: add a preference tag, set the output style, or add a prompt hint",
		map[string]engine.Property{
			"user":        {Type: "string", Description: "User ID (persona owner)"},
			"tag":         {Type: "string", Description: "Preference tag to add (e.g. golang, finance)"},
			"style":       {Type: "string", Description: "Preferred output style: visual, minimal, or detailed"},
			"prompt_hint": {Type: "string", Description: "Style hint injected into generated prompts (e.g. 'answer in German')"},
		},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User       string `json:"user"`
				Tag        string `json:"tag"`
				Style      string `json:"style"`
				PromptHint string `json:"prompt_hint"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return personaError(err.Error())
			}
			if deps.personaStore == nil {
				return personaError("persona store not configured")
			}
			if params.Tag == "" && params.Style == "" && params.PromptHint == "" {
				return personaError("provide at least one of tag, style, prompt_hint")
			}
			profile, err := deps.personaStore.Update(params.User, func(p *persona.Profile) {
				if tag := strings.TrimSpace(params.Tag); tag != "" {
					exists := false
					for _, existing := range p.PreferenceTags {
						if strings.EqualFold(existing, tag) {
							exists = true
							break
						}
					}
					if !exists {
						p.PreferenceTags = append(p.PreferenceTags, tag)
					}
				}
				if style := strings.TrimSpace(params.Style); style != "" {
					p.PreferredStyle = style
				}
				if hint := strings.TrimSpace(params.PromptHint); hint != "" {
					p.PromptHints = append(p.PromptHints, hint)
				}
			})
			if err != nil {
				return personaError(err.Error())
			}
			data, _ := json.Marshal(map[string]interface{}{"updated": true, "profile": profile})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})

	server.RegisterTool("bt_persona_patterns", "Mine a user's interaction log for recurring task patterns (automation candidates)",
		map[string]engine.Property{
			"user":            {Type: "string", Description: "User ID (persona owner)"},
			"min_occurrences": {Type: "number", Description: "Cluster size that makes a pattern recurring (default 3)"},
			"window_days":     {Type: "number", Description: "How many days back to consider (default 14)"},
		},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User           string  `json:"user"`
				MinOccurrences int     `json:"min_occurrences"`
				WindowDays     float64 `json:"window_days"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return personaError(err.Error())
			}
			if deps.personaStore == nil {
				return personaError("persona store not configured")
			}
			patterns, interactionCount, err := mineUserPatterns(deps, params.User, params.MinOccurrences, params.WindowDays, true)
			if err != nil {
				return personaError(err.Error())
			}
			data, _ := json.Marshal(map[string]interface{}{
				"user":         params.User,
				"interactions": interactionCount,
				"patterns":     patterns,
			})
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}

// mineUserPatterns runs the habit miner over a user's interaction log.
// Shared by bt_persona_patterns, bt_goal_from_pattern, and the automation
// autopilot. useEmbeddings=false forces keyword clustering — the autopilot
// runs inline after bt_run_task and must not block on Ollama.
func mineUserPatterns(deps *mcpDeps, user string, minOccurrences int, windowDays float64, useEmbeddings bool) ([]persona.RecurringPattern, int, error) {
	log, err := persona.NewLog(deps.personaStore.Workspace(user))
	if err != nil {
		return nil, 0, err
	}
	interactions, err := log.All()
	if err != nil {
		return nil, 0, err
	}

	miner := persona.NewHabitMiner()
	if minOccurrences > 0 {
		miner.MinOccurrences = minOccurrences
	}
	if windowDays > 0 {
		miner.Window = time.Duration(windowDays * 24 * float64(time.Hour))
	}
	// Embedding-based clustering when Ollama is reachable; the miner
	// falls back to keyword similarity on any embedding failure.
	if useEmbeddings && (deps.llmHealth == nil || deps.llmHealth.IsHealthy()) {
		miner.Embed = personaEmbedder(deps)
	}

	patterns := miner.Mine(interactions, time.Now())
	if patterns == nil {
		patterns = []persona.RecurringPattern{}
	}
	return patterns, len(interactions), nil
}

// personaEmbedder adapts the knowledge-layer Ollama embedding client to the
// habit miner's vector interface.
func personaEmbedder(deps *mcpDeps) func(string) ([]float64, error) {
	client := &knowledge.EmbeddingClient{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	}
	if deps.cfg != nil && deps.cfg.OllamaHost != "" {
		client.BaseURL = deps.cfg.OllamaHost
	}
	return func(text string) ([]float64, error) {
		emb, err := client.GetEmbedding(text)
		if err != nil {
			return nil, err
		}
		return []float64(emb), nil
	}
}

// injectPersonaContextLocked seeds ChainState["persona_context"] with the
// user's profile block so ChainAction prompts can reference
// {{.ChainState.persona_context}}, and ChainState["persona_user"] so
// in-tree hooks (ConsiderTreeCompile) know who the run belongs to. Always
// resets the keys first: the shared blackboard survives across bt_run_task
// calls, and one user's preferences must never leak into another user's
// (or an anonymous) run.
//
// Callers must be registered via server.RegisterBlackboardTool so the whole
// call runs under the Server-wide blackboard lock (internal/engine/mcp_server.go).
func injectPersonaContextLocked(deps *mcpDeps, user string) {
	if deps.bb.ChainState == nil {
		deps.bb.ChainState = map[string]any{}
	}
	delete(deps.bb.ChainState, "persona_context")
	delete(deps.bb.ChainState, "persona_user")
	if deps.personaStore == nil || strings.TrimSpace(user) == "" {
		return
	}
	deps.bb.ChainState["persona_user"] = user
	profile, err := deps.personaStore.Load(user)
	if err != nil {
		engine.Warn("persona: profile load failed", "user", user, "error", err)
		return
	}
	if block := profile.ContextBlock(); block != "" {
		deps.bb.ChainState["persona_context"] = block
	}
}

// recordPersonaInteraction appends a bt_run_task execution to the user's
// interaction log (ADR-010 Phase 1: every run with a user feeds habit
// mining). Best-effort: logging must never fail the task itself.
func recordPersonaInteraction(deps *mcpDeps, user, task, treeID, outcome string, durationMs int64) {
	if deps.personaStore == nil || strings.TrimSpace(user) == "" {
		return
	}
	log, err := persona.NewLog(deps.personaStore.Workspace(user))
	if err != nil {
		engine.Warn("persona: interaction log unavailable", "user", user, "error", err)
		return
	}
	if err := log.Append(persona.Interaction{
		Task:       task,
		TreeID:     treeID,
		Outcome:    outcome,
		DurationMs: durationMs,
	}); err != nil {
		engine.Warn("persona: interaction append failed", "user", user, "error", err)
	}
}

func personaError(msg string) *engine.ToolResult {
	return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: fmt.Sprintf(`{"error": %q}`, msg)}}}
}
