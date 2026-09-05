package benchmark

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/llm"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestBFCLV3_MultiTurn_Basic(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use 2 base entries from BuiltinBFCLV3
	all := BuiltinBFCLV3()
	var baseEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_base" {
			baseEntries = append(baseEntries, e)
		}
	}
	if len(baseEntries) < 2 {
		t.Fatalf("expected at least 2 base entries, got %d", len(baseEntries))
	}
	baseEntries = baseEntries[:2]

	// Run against GoDeveloperTree
	tree := evolution.GoDeveloperTree()
	llmClient := RealLLM(t)
	metrics := EvaluateBFCLV3(tree, baseEntries, llmClient)

	fmt.Printf("\nBFCL V3 Multi-Turn Basic: %d/%d correct turns (%.0f%%), %d/%d fully correct (%.0f%%)\n",
		metrics.CorrectTurns, metrics.TotalTurns, metrics.TurnAccuracy*100,
		metrics.FullyCorrect, metrics.TotalEntries, metrics.MultiStepSuccessRate*100)

	for _, r := range metrics.Results {
		s := "✗"
		if r.AllCorrect {
			s = "✓"
		}
		fmt.Printf("  %s %-20s [%s]: %d/%d turns correct\n",
			s, r.EntryID, r.Category, r.CorrectInTurns, r.NumTurns)
	}

	if metrics.TurnAccuracy < 0.3 {
		t.Errorf("BFCL V3 multi-turn base accuracy too low: %.0f%% (expected > 30%%)", metrics.TurnAccuracy*100)
	}
}

func TestBFCLV3_MultiTurn_Composite(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use 2 composite entries
	all := BuiltinBFCLV3()
	var compEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_composite" {
			compEntries = append(compEntries, e)
		}
	}
	if len(compEntries) < 2 {
		t.Fatalf("expected at least 2 composite entries, got %d", len(compEntries))
	}
	compEntries = compEntries[:2]

	// Run against CodeReviewTree for first entry, Finance tree for second
	// Composite-001 uses SecurityReview+StyleReview → CodeReviewTree
	// Composite-002 uses ReconPath → use a general tree
	var allResults []BFCLV3Result
	totalCorrect := 0
	totalTurns := 0
	totalFullyCorrect := 0

	// First entry: code review domain
	{
		tree := domains.CodeReviewTree()
		llmClient := RealLLM(t)
		metrics := EvaluateBFCLV3(tree, compEntries[:1], llmClient)
		allResults = append(allResults, metrics.Results...)
		totalCorrect += metrics.CorrectTurns
		totalTurns += metrics.TotalTurns
		totalFullyCorrect += metrics.FullyCorrect
	}

	// Second entry: finance domain
	{
		tree := evolution.PitchAgentTree()
		llmClient := RealLLM(t)
		metrics := EvaluateBFCLV3(tree, compEntries[1:], llmClient)
		allResults = append(allResults, metrics.Results...)
		totalCorrect += metrics.CorrectTurns
		totalTurns += metrics.TotalTurns
		totalFullyCorrect += metrics.FullyCorrect
	}

	turnAcc := 0.0
	if totalTurns > 0 {
		turnAcc = float64(totalCorrect) / float64(totalTurns)
	}

	fmt.Printf("\nBFCL V3 Multi-Turn Composite: %d/%d correct turns (%.0f%%), %d/%d fully correct\n",
		totalCorrect, totalTurns, turnAcc*100,
		totalFullyCorrect, len(compEntries))

	for _, r := range allResults {
		s := "✗"
		if r.AllCorrect {
			s = "✓"
		}
		fmt.Printf("  %s %-20s: %d/%d turns correct\n",
			s, r.EntryID, r.CorrectInTurns, r.NumTurns)
	}

	// Composite tasks are harder; just verify the evaluation runs
	if turnAcc == 0 && totalTurns > 0 {
		t.Log("composite multi-turn accuracy is 0% — expected for complex tasks with real LLM")
	}
}

func TestBFCLV3_LongContext(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use long_context entries to verify tree handles long conversations
	all := BuiltinBFCLV3()
	var longCtxEntries []BFCLV3Entry
	for _, e := range all {
		if e.Category == "multi_turn_long_context" {
			longCtxEntries = append(longCtxEntries, e)
		}
	}
	if len(longCtxEntries) == 0 {
		t.Fatal("expected at least 1 long_context entry")
	}

	// Run against DeepResearchTree (handles research-type long context)
	tree := evolution.DeepResearchTree()
	llmClient := RealLLM(t)
	metrics := EvaluateBFCLV3(tree, longCtxEntries, llmClient)

	fmt.Printf("\nBFCL V3 Long Context: %d/%d correct turns (%.0f%%), %d/%d fully correct (%.0f%%)\n",
		metrics.CorrectTurns, metrics.TotalTurns, metrics.TurnAccuracy*100,
		metrics.FullyCorrect, metrics.TotalEntries, metrics.MultiStepSuccessRate*100)

	for _, r := range metrics.Results {
		fmt.Printf("  %-20s: %d/%d turns correct, all=%v\n",
			r.EntryID, r.CorrectInTurns, r.NumTurns, r.AllCorrect)
	}

	// Verify that the tree didn't crash on long multi-turn input
	if metrics.TotalTurns == 0 {
		t.Error("no turns were processed for long context entries")
	}
	// All entries should have been processed
	if metrics.TotalEntries != len(longCtxEntries) {
		t.Errorf("expected %d entries processed, got %d", len(longCtxEntries), metrics.TotalEntries)
	}
}

func TestSWEVerified_Evaluation(t *testing.T) {
	llm.SkipUnlessIntegration(t)
	// Use a sample of 5 entries from BuiltinSWEVerifiedSample
	all := BuiltinSWEVerifiedSample()
	entries := all[:minInt(5, len(all))]

	tree := evolution.GoDeveloperTree()
	llmClient := RealLLM(t)
	metrics := EvaluateSWEVerified(tree, entries, llmClient)

	fmt.Printf("\nSWE-bench Verified: %d/%d resolved (%.0f%% resolve rate)\n",
		metrics.Resolved, metrics.TotalEntries, metrics.ResolveRate*100)

	for _, r := range metrics.Results {
		s := "✗"
		if r.Resolved {
			s = "✓"
		}
		fmt.Printf("  %s %-35s [%s]: outcome=%s, output=%d chars\n",
			s, r.Entry.InstanceID, r.Entry.Repo, r.Outcome, len(r.Output))
	}

	// No hard threshold — success depends on LLM quality
	// Just verify the evaluation runs without error and processes all entries
	if metrics.TotalEntries != len(entries) {
		t.Errorf("expected %d entries processed, got %d", len(entries), metrics.TotalEntries)
	}

	t.Logf("resolve rate: %.0f%% (depends on LLM, no hard threshold enforced)", metrics.ResolveRate*100)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestDetectPath_SecurityKeywordFallback guards a genuine routing inconsistency:
// eval_suites.go declares SecurityPath as the ExpectedPath for security-audit
// tasks (the Security() suite and the codeReviewPaths PossiblePaths reference it
// 12 times), yet detectPath's keyword fallback has NO branch that can ever emit
// "SecurityPath". When a tree run leaves no bb.CurrentPath/VisitedPaths (the
// backward-compat scoring path), an unmistakably security-audit task —
// "scan for SQL injection vulnerabilities", "check for hardcoded credentials",
// "full penetration test" — is silently classified as GeneralPath (or, when it
// happens to contain "audit", CodeReviewPath), never SecurityPath. That means the
// fallback classifier disagrees with the suites' own declared expectations for an
// entire domain, deflating security-suite scoring in offline/backward-compat runs.
// A security-audit task must route to SecurityPath, while a generic "review this
// code for security bugs" must remain CodeReviewPath (pinned as a control so any
// fix does not over-capture the code-review domain).
func TestDetectPath_SecurityKeywordFallback(t *testing.T) {
	tests := []struct {
		name, task, expected string
	}{
		// Must route to SecurityPath (currently misrouted — the defect under test).
		{"sql injection", "scan for SQL injection vulnerabilities in the API handlers", "SecurityPath"},
		{"hardcoded credentials", "check for hardcoded credentials and API keys in the codebase", "SecurityPath"},
		{"penetration test", "full penetration test: recon, vulnerability scanning, privilege escalation, remediation with CVSS scores", "SecurityPath"},
		{"threat model", "threat model the platform: trust boundaries, attack surfaces, STRIDE analysis", "SecurityPath"},

		// Control: generic security-flavored code review must stay CodeReviewPath
		// (matches the existing TestDetectPath_KeywordFallback expectation).
		{"security bugs review stays code review", "review this code for security bugs", "CodeReviewPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task}
			if got := detectPath("result", bb); got != tt.expected {
				t.Errorf("detectPath(task=%q) = %q, want %q", tt.task, got, tt.expected)
			}
		})
	}
}

// TestDetectPath_DataPipelineKeywordFallback guards the same class of routing
// inconsistency as TestDetectPath_SecurityKeywordFallback, for a different domain:
// eval_suites.go declares an entire DataPipeline() suite (wired into AllSuites())
// whose 9 tasks all name "DataPipelinePath" as their ExpectedPath, yet detectPath's
// keyword fallback has NO branch that can ever emit "DataPipelinePath". When a tree
// run leaves no bb.CurrentPath/VisitedPaths (the backward-compat scoring path), an
// unmistakable ETL/data-engineering task — "design an ETL pipeline", "transform JSON
// event streams into parquet", "build a data lake/mesh" — is silently misclassified:
// the generic "pipeline" keyword sends it to DevOpsPath, "build" sends it to
// BuildPath, and the rest fall through to GeneralPath. The fallback classifier thus
// disagrees with the suite's own declared expectations for the whole data-pipeline
// domain. A data-pipeline task must route to DataPipelinePath, while a generic CI/CD
// "pipeline" task must remain DevOpsPath (pinned as a control so any fix keys on
// data-specific signals and does not over-capture the DevOps domain).
func TestDetectPath_DataPipelineKeywordFallback(t *testing.T) {
	tests := []struct {
		name, task, expected string
	}{
		// Must route to DataPipelinePath (currently misrouted — the defect under test).
		{"etl pipeline", "design an ETL pipeline to ingest CSV logs with schema validation", "DataPipelinePath"},
		{"streaming to parquet", "transform JSON event streams into parquet format for analytics", "DataPipelinePath"},
		{"data lake", "build a data lake: raw/bronze/silver/gold layers, schema evolution, catalog integration, lineage tracking", "DataPipelinePath"},
		{"data mesh", "build a data mesh: domain ownership, data products with SLAs, federated governance, cross-domain lineage", "DataPipelinePath"},

		// Controls: generic CI/CD "pipeline" tasks must stay DevOpsPath so a fix keys
		// on data-specific signals rather than the bare "pipeline" keyword.
		{"ci pipeline stays devops", "configure the CI pipeline for Go lint, vet, and test", "DevOpsPath"},
		{"deploy task stays devops", "deploy to staging environment and run smoke tests", "DevOpsPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task}
			if got := detectPath("result", bb); got != tt.expected {
				t.Errorf("detectPath(task=%q) = %q, want %q", tt.task, got, tt.expected)
			}
		})
	}
}

// TestIsToolMatch_EmptyPath guards a correctness defect in isToolMatch: when the
// detected path is empty, the bidirectional substring check strings.Contains(expected, path)
// degenerates to strings.Contains(expected, "") which is ALWAYS true, so a turn that
// executed no path (and whose output does not mention the tool) is scored as a correct
// match against any expected tool — silently inflating turn accuracy. An empty path must
// never count as correctly invoking a named expected tool.
func TestIsToolMatch_EmptyPath(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		path     string
		expected string
		want     bool
	}{
		// The defect: empty path + expected tool not present in output must NOT match.
		{name: "empty path, empty output", output: "", path: "", expected: "BuildPath", want: false},
		{name: "empty path, unrelated output", output: "did something unrelated", path: "", expected: "BuildPath", want: false},

		// Control cases that must stay correct after any fix.
		{name: "no expected tool, non-empty output", output: "some output", path: "", expected: "", want: true},
		{name: "exact path match", output: "x", path: "BuildPath", expected: "BuildPath", want: true},
		{name: "path is substring of expected", output: "x", path: "Build", expected: "BuildPath", want: true},
		{name: "expected is substring of path", output: "x", path: "BuildPath", expected: "Build", want: true},
		{name: "expected mentioned in output", output: "ran the BuildPath step", path: "GeneralPath", expected: "BuildPath", want: true},
		{name: "genuine mismatch", output: "x", path: "GeneralPath", expected: "BuildPath", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isToolMatch(tt.output, tt.path, tt.expected)
			if got != tt.want {
				t.Errorf("isToolMatch(%q, %q, %q) = %v, want %v", tt.output, tt.path, tt.expected, got, tt.want)
			}
		})
	}
}
