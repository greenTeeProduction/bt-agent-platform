package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// BFCLV3Turn represents a single message turn in a BFCL V3 multi-turn conversation.
type BFCLV3Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BFCLV3Entry represents a multi-turn BFCL V3 benchmark entry.
// Each entry has multiple conversation turns and expected tool calls per turn.
type BFCLV3Entry struct {
	ID            string         `json:"id"`
	Category      string         `json:"category"`
	Turns         []BFCLV3Turn   `json:"turns"`          // conversation turns
	InitialConfig map[string]any `json:"initial_config"` // initial system state
	ExpectedTools []string       `json:"expected_tools"` // expected tool per turn
}

// BFCLV3Metrics aggregates BFCL V3 multi-turn evaluation results.
type BFCLV3Metrics struct {
	TotalEntries         int            `json:"total_entries"`
	CorrectTurns         int            `json:"correct_turns"`
	TotalTurns           int            `json:"total_turns"`
	TurnAccuracy         float64        `json:"turn_accuracy"`
	MultiStepSuccessRate float64        `json:"multi_step_success_rate"`
	FullyCorrect         int            `json:"fully_correct"`
	Results              []BFCLV3Result `json:"results,omitempty"`
}

// BFCLV3Result holds the outcome for a single multi-turn entry.
type BFCLV3Result struct {
	EntryID        string `json:"entry_id"`
	Category       string `json:"category"`
	NumTurns       int    `json:"num_turns"`
	CorrectInTurns int    `json:"correct_in_turns"`
	AllCorrect     bool   `json:"all_correct"`
	TurnSuccess    []bool `json:"turn_success,omitempty"`
}

// LoadBFCLV3MultiTurn reads a BFCL V3 multi-turn JSON file.
// The JSON file has top-level keys for each category (multi_turn_base, multi_turn_composite, etc.)
// each containing an array of BFCLV3Entry.
func LoadBFCLV3MultiTurn(path string) (map[string][]BFCLV3Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bfcl v3 file: %w", err)
	}
	var categories map[string][]BFCLV3Entry
	if err := json.Unmarshal(data, &categories); err != nil {
		return nil, fmt.Errorf("unmarshal bfcl v3: %w", err)
	}
	return categories, nil
}

// LoadBFCLV3Entries reads a BFCL V3 multi-turn JSON file and returns all entries flattened.
func LoadBFCLV3Entries(path string) ([]BFCLV3Entry, error) {
	categories, err := LoadBFCLV3MultiTurn(path)
	if err != nil {
		return nil, err
	}
	var all []BFCLV3Entry
	for _, entries := range categories {
		all = append(all, entries...)
	}
	return all, nil
}

// EvaluateBFCLV3 runs multi-turn BFCL V3 entries through a tree statefully.
// The same Blackboard is reused across turns of a single entry to simulate
// stateful multi-turn conversation. A turn is considered correct if the
// output or detected path matches the expected tool for that turn.
// A multi-step entry is fully correct only if ALL turns match.
func EvaluateBFCLV3(tree *evolution.SerializableNode, entries []BFCLV3Entry, llmClient llm.LLM) *BFCLV3Metrics {
	results := make([]BFCLV3Result, 0, 32)
	totalTurns := 0
	correctTurns := 0
	fullyCorrect := 0

	for _, entry := range entries {
		// Stateful blackboard reused across turns
		bb := &engine.Blackboard{
			LLM: llmClient,
		}

		turnSuccess := make([]bool, len(entry.Turns))
		correctInEntry := 0

		for i, turn := range entry.Turns {
			bb.Task = turn.Content
			bt := engine.BuildTree(tree, bb)
			output := engine.RunTask(bb, bt)

			// Determine if this turn matches the expected tool
			expected := ""
			if i < len(entry.ExpectedTools) {
				expected = entry.ExpectedTools[i]
			}

			path := detectPath(output, bb)
			isCorrect := isToolMatch(output, path, expected)

			totalTurns++
			if isCorrect {
				correctTurns++
				correctInEntry++
			}
			turnSuccess[i] = isCorrect
		}

		allCorrect := correctInEntry == len(entry.Turns) && len(entry.Turns) > 0
		if allCorrect {
			fullyCorrect++
		}

		results = append(results, BFCLV3Result{
			EntryID:        entry.ID,
			Category:       entry.Category,
			NumTurns:       len(entry.Turns),
			CorrectInTurns: correctInEntry,
			AllCorrect:     allCorrect,
			TurnSuccess:    turnSuccess,
		})
	}

	n := len(entries)
	turnAcc := 0.0
	multiStepRate := 0.0
	if totalTurns > 0 {
		turnAcc = float64(correctTurns) / float64(totalTurns)
	}
	if n > 0 {
		multiStepRate = float64(fullyCorrect) / float64(n)
	}

	return &BFCLV3Metrics{
		TotalEntries:         n,
		CorrectTurns:         correctTurns,
		TotalTurns:           totalTurns,
		TurnAccuracy:         turnAcc,
		MultiStepSuccessRate: multiStepRate,
		FullyCorrect:         fullyCorrect,
		Results:              results,
	}
}

// isToolMatch checks whether a tree output/path matches an expected tool name.
func isToolMatch(output, path, expected string) bool {
	if expected == "" {
		// No expected tool specified — consider any non-empty output as correct
		return len(output) > 0
	}
	// Case-insensitive path match
	if strings.EqualFold(path, expected) {
		return true
	}
	// Check if path contains expected or expected contains path
	lowPath := strings.ToLower(path)
	lowExp := strings.ToLower(expected)
	if strings.Contains(lowPath, lowExp) || strings.Contains(lowExp, lowPath) {
		return true
	}
	// Check output for expected tool mention
	if strings.Contains(strings.ToLower(output), lowExp) {
		return true
	}
	return false
}

// BuiltinBFCLV3 returns 8 representative BFCL V3 multi-turn entries
// from different categories: 2 base, 2 composite, 2 long_context, 1 miss_func, 1 miss_param.
func BuiltinBFCLV3() []BFCLV3Entry {
	return []BFCLV3Entry{
		// --- multi_turn_base (2 entries) ---
		{
			ID:       "base-001",
			Category: "multi_turn_base",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "review this Go code for bugs"},
				{Role: "assistant", Content: "I'll review the code. Let me scan for issues."},
				{Role: "user", Content: "focus on error handling patterns"},
			},
			InitialConfig: map[string]any{"context": "code_review"},
			ExpectedTools: []string{"BugDetection", "BugDetection"},
		},
		{
			ID:       "base-002",
			Category: "multi_turn_base",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "build the Go project"},
				{Role: "assistant", Content: "Building... Found 0 compilation errors."},
				{Role: "user", Content: "now run the tests"},
			},
			InitialConfig: map[string]any{"context": "devops"},
			ExpectedTools: []string{"BuildPath", "TestPath"},
		},
		// --- multi_turn_composite (2 entries) ---
		{
			ID:       "composite-001",
			Category: "multi_turn_composite",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "scan for security vulnerabilities in this code"},
				{Role: "assistant", Content: "Running security scan..."},
				{Role: "user", Content: "also check the code style and formatting"},
				{Role: "assistant", Content: "Checking style..."},
				{Role: "user", Content: "now suggest fixes for all issues found"},
			},
			InitialConfig: map[string]any{"context": "composite_review"},
			ExpectedTools: []string{"SecurityReview", "StyleReview", "ExecutionPath"},
		},
		{
			ID:       "composite-002",
			Category: "multi_turn_composite",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "reconcile the general ledger entries"},
				{Role: "assistant", Content: "Starting GL reconciliation..."},
				{Role: "user", Content: "trace the root cause of any breaks found"},
				{Role: "assistant", Content: "Root cause analysis complete."},
				{Role: "user", Content: "route the reconciliation for sign-off"},
			},
			InitialConfig: map[string]any{"context": "finance"},
			ExpectedTools: []string{"ReconPath", "ReconPath", "ReconPath"},
		},
		// --- multi_turn_long_context (2 entries) ---
		{
			ID:       "longctx-001",
			Category: "multi_turn_long_context",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "research how AI agents compare to traditional software architectures"},
				{Role: "assistant", Content: "Broad research launched. Mapping the landscape..."},
				{Role: "user", Content: "focus on autonomous decision-making capabilities"},
				{Role: "assistant", Content: "Refined search. Processing 15+ sources..."},
				{Role: "user", Content: "add a section on failure modes and recovery strategies"},
				{Role: "assistant", Content: "Deep diving into resilience patterns..."},
				{Role: "user", Content: "synthesize everything into a structured research report"},
			},
			InitialConfig: map[string]any{"context": "research"},
			ExpectedTools: []string{
				"SynthesisPhase", "SynthesisPhase", "SynthesisPhase", "SynthesisPhase", "SynthesisPhase",
			},
		},
		{
			ID:       "longctx-002",
			Category: "multi_turn_long_context",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "build a DCF model with WACC of 10%"},
				{Role: "assistant", Content: "DCF model template created."},
				{Role: "user", Content: "add three scenarios: bear, base, bull"},
				{Role: "assistant", Content: "Scenarios added with growth rate ranges."},
				{Role: "user", Content: "build sensitivity tables for WACC vs terminal growth"},
				{Role: "assistant", Content: "5x5 sensitivity tables generated."},
				{Role: "user", Content: "verify the model integrity and check for errors"},
				{Role: "assistant", Content: "Model verified. Balance sheet ties. No #REF! errors."},
				{Role: "user", Content: "assemble everything into the pitch deck"},
			},
			InitialConfig: map[string]any{"context": "finance"},
			ExpectedTools: []string{
				"DCFPath", "DCFPath", "DCFPath", "DCFPath", "DeckAssemblyPath",
			},
		},
		// --- multi_turn_miss_func (1 entry) ---
		{
			ID:       "missfunc-001",
			Category: "multi_turn_miss_func",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "deploy the application to staging"},
				{Role: "assistant", Content: "Deployment started."},
				{Role: "user", Content: "verify the deployment is healthy"},
			},
			InitialConfig: map[string]any{"context": "devops"},
			ExpectedTools: []string{"DeployPath", "DeployPath"},
		},
		// --- multi_turn_miss_param (1 entry) ---
		{
			ID:       "missparam-001",
			Category: "multi_turn_miss_param",
			Turns: []BFCLV3Turn{
				{Role: "user", Content: "run KYC screening for a new client"},
				{Role: "assistant", Content: "Onboarding docs parsed. Running rules engine..."},
				{Role: "user", Content: "I need the report generated with risk rating"},
			},
			InitialConfig: map[string]any{"context": "finance"},
			ExpectedTools: []string{"KYCPath", "KYCPath"},
		},
	}
}
