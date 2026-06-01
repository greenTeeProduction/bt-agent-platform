package factory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/util"
)

// SkillSpec is the extracted essence of a skill, used to generate a behavior tree.
type SkillSpec struct {
	Name        string   `json:"name"`
	Purpose     string   `json:"purpose"`
	Checks      []string `json:"checks"`       // conditions / decision points
	Actions     []string `json:"actions"`      // things the agent should do
	Pitfalls    []string `json:"pitfalls"`     // things to avoid (guard conditions)
	Fallbacks   []string `json:"fallbacks"`    // what to do on failure
	RetryPolicy string   `json:"retry_policy"` // "none", "retry", "retry_with_escalation"
}

// TreeSpec is the LLM-generated behavior tree structure, directly serializable.
type TreeSpec struct {
	RootType     string     `json:"root_type"` // "Sequence" or "Selector"
	RootName     string     `json:"root_name"`
	PreChecks    []TreeNode `json:"pre_checks"`    // validate before execution
	StrategyPath []TreeNode `json:"strategy_path"` // main execution
	SelfCorrect  *TreeNode  `json:"self_correct"`  // optional self-correction
	Fallback     *TreeNode  `json:"fallback"`      // escalation path
}

// TreeNode is a single node in the LLM-generated tree.
type TreeNode struct {
	Type        string `json:"type"` // "Condition" or "Action"
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Analyzer uses the LLM interface to analyze a skill and produce a TreeSpec.
type Analyzer struct {
	client  llm.LLM
	timeout time.Duration
}

// NewAnalyzer creates a skill analyzer backed by the shared LLM client.
func NewAnalyzer(client llm.LLM) *Analyzer {
	return &Analyzer{
		client:  client,
		timeout: 120 * time.Second,
	}
}

// analyzePrompt is the system prompt that instructs the LLM to convert a skill into a BT spec.
const analyzePrompt = `You are a behavior tree architect. Given a skill definition (markdown file describing how an AI agent should behave), produce a behavior tree specification in JSON format.

A behavior tree has:
- Sequence nodes: run children in order, fail if any child fails
- Selector nodes: try children in order, succeed on first success (like if/else)
- Condition nodes: check something, return success or failure
- Action nodes: do something
- Retry decorators: retry a child N times on failure

Output ONLY valid JSON with this exact structure:
{
  "root_type": "Sequence",
  "root_name": "MainSequence",
  "pre_checks": [
    {"type": "Condition", "name": "CheckName", "description": "what it checks"}
  ],
  "strategy_path": [
    {"type": "Condition", "name": "DetectPattern", "description": "..."},
    {"type": "Action", "name": "DoAction", "description": "..."}
  ],
  "self_correct": {"type": "Action", "name": "SelfCorrect", "description": "..."},
  "fallback": {"type": "Action", "name": "Escalate", "description": "..."}
}

Rules:
- pre_checks are conditions that must ALL pass (they become part of a Sequence)
- strategy_path lists conditions+actions in order. The first condition that matches triggers its following actions.
- If the skill mentions retrying, set self_correct. Otherwise leave it null.
- If the skill mentions escalation or fallback behavior, set fallback. Otherwise leave it null.
- Node names should be CamelCase, descriptive, and derived from the skill content.
- Limit to 3-5 pre_checks and 4-8 strategy_path items.`

// Analyze reads a skill's markdown content and returns a TreeSpec.
func (a *Analyzer) Analyze(skillContent string) (*TreeSpec, error) {
	prompt := fmt.Sprintf("%s\n\n--- SKILL CONTENT ---\n%s\n--- END SKILL ---\n\nJSON:", analyzePrompt, skillContent)

	result, err := a.client.Generate(prompt)
	if err != nil {
		return nil, fmt.Errorf("llm analyze: %w", err)
	}

	// Extract JSON from the response (may be wrapped in markdown)
	jsonStr := extractJSON(result)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in LLM response: %s", truncate(result, 200))
	}

	var spec TreeSpec
	if err := json.Unmarshal([]byte(jsonStr), &spec); err != nil {
		return nil, fmt.Errorf("parse tree spec: %w\nRaw JSON: %s", err, truncate(jsonStr, 500))
	}

	// Validate
	if spec.RootType == "" {
		spec.RootType = "Sequence"
	}
	if spec.RootName == "" {
		spec.RootName = "MainSequence"
	}
	if len(spec.StrategyPath) == 0 {
		return nil, fmt.Errorf("tree spec has no strategy_path nodes")
	}

	return &spec, nil
}

// extractJSON finds the first complete JSON object in a string.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

func truncate(s string, n int) string { return util.Truncate(s, n) }
