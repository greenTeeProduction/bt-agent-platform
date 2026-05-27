// Package eval provides a comprehensive platform evaluation runner that
// executes all 20 use case suites against the merged behavior tree and
// produces a maturity scorecard.
package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// PlatformEvalResult is the comprehensive result of evaluating all suites.
type PlatformEvalResult struct {
	Timestamp    string                 `json:"timestamp"`
	TotalSuites  int                    `json:"total_suites"`
	TotalTasks   int                    `json:"total_tasks"`
	Passed       int                    `json:"passed"`
	Failed       int                    `json:"failed"`
	SuccessRate  float64                `json:"success_rate"`
	AvgDurationMs float64               `json:"avg_duration_ms"`
	BySuite      []SuiteEvalResult      `json:"by_suite"`
	Scorecard    PlatformScorecard      `json:"scorecard"`
}

// SuiteEvalResult is the result for a single suite.
type SuiteEvalResult struct {
	Name        string  `json:"name"`
	TotalTasks  int     `json:"total_tasks"`
	Passed      int     `json:"passed"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
	AvgDuration float64 `json:"avg_duration_ms"`
}

// PlatformScorecard maps use cases to scores.
type PlatformScorecard struct {
	UseCases map[string]UseCaseScore `json:"use_cases"`
}

// UseCaseScore scores a single use case on automation readiness.
type UseCaseScore struct {
	Name           string  `json:"name"`
	SuitePass      float64 `json:"suite_pass_rate"` // routing success rate
	AutomationFit  float64 `json:"automation_fit"`  // 0-100: how well it fits automation
	Frequency      string  `json:"frequency"`        // how often it runs
	Status         string  `json:"status"`           // optimized | ready | partial | gap
}

// EvalMockLLM is a mock that returns sufficiently long output to pass quality gates.
type EvalMockLLM struct{}

func (m *EvalMockLLM) Generate(prompt string) (string, error) {
	return "EVAL_OUTPUT: Comprehensive response with details, examples, and actionable recommendations. This output meets the minimum length requirement for quality validation gates.", nil
}
func (m *EvalMockLLM) AnalyzeComplexity(task string) string    { return "medium" }
func (m *EvalMockLLM) GeneratePlan(task, complexity string) string { return "plan: " + task }
func (m *EvalMockLLM) Reflect(task, outcome, plan string) (string, string) { return "good", "none" }

// treeForSuite maps suite names to their optimal behavior trees.
func treeForSuite(name string) *evolution.SerializableNode {
	switch name {
	case "godev":
		return evolution.GoDeveloperTree()
	case "code_review":
		return evolution.MergedTree() // Uses CodeReviewPath
	case "devops_ci":
		return evolution.MergedTree() // Uses DevOpsPath
	case "security_audit":
		return evolution.MergedTree() // Uses SecurityPath
	case "data_pipeline":
		return evolution.MergedTree() // Uses DataPipelinePath
	case "deep_research":
		return evolution.MergedTree() // Uses ResearchPath
	case "think_tank":
		return evolution.MergedTree() // Uses ThinkTankPath
	case "refactoring":
		return evolution.MergedTree() // Uses RefactoringPath
	case "knowledge_qa":
		return evolution.MergedTree() // Uses KnowledgePath
	case "kanban_workflow":
		return evolution.MergedTree() // Uses WorkflowPath
	case "incident_investigation":
		return evolution.MergedTree() // Uses IncidentPath
	case "finance":
		return evolution.MergedTree() // Uses FinancePath
	case "health_monitoring":
		return evolution.MergedTree() // Uses GeneralPath (no specific handler yet)
	case "cron_management":
		return evolution.MergedTree() // General
	case "self_evolution":
		return evolution.MergedTree() // General
	case "meeting_notes":
		return evolution.MergedTree() // General
	case "startup_simulation":
		return evolution.MergedTree() // General
	case "notebooklm_research":
		return evolution.MergedTree() // Research
	case "vault_management":
		return evolution.MergedTree() // General
	case "platform_eval":
		return evolution.MergedTree() // General
	default:
		return evolution.MergedTree()
	}
}

// RunPlatformEval runs all 20 eval suites against their optimal trees.
func RunPlatformEval() *PlatformEvalResult {
	result := &PlatformEvalResult{
		Timestamp: time.Now().Format(time.RFC3339),
	}
	mock := &EvalMockLLM{}
	suites := benchmark.AllSuites()

	var allResults []SuiteEvalResult
	totalPassed := 0
	totalTasks := 0
	totalDuration := int64(0)

	for _, suite := range suites {
		// Skip empty-task-only suites
		realTasks := 0
		for _, tc := range suite.Tasks {
			if tc.Task != "" {
				realTasks++
			}
		}
		if realTasks == 0 {
			continue
		}

		tree := treeForSuite(suite.Name)
		metrics := benchmark.RunSuite(tree, suite, mock)

		passed := metrics.Successes
		totalPassed += passed
		totalTasks += metrics.TotalTasks
		totalDuration += int64(metrics.AvgDurationMs) * int64(metrics.TotalTasks)

		failed := metrics.TotalTasks - metrics.Successes
		rate := 0.0
		if metrics.TotalTasks > 0 {
			rate = float64(passed) / float64(metrics.TotalTasks) * 100
		}

		allResults = append(allResults, SuiteEvalResult{
			Name:        suite.Name,
			TotalTasks:  metrics.TotalTasks,
			Passed:      passed,
			Failed:      failed,
			SuccessRate: rate,
			AvgDuration: metrics.AvgDurationMs,
		})
	}

	result.TotalSuites = len(allResults)
	result.TotalTasks = totalTasks
	result.Passed = totalPassed
	result.Failed = totalTasks - totalPassed
	if totalTasks > 0 {
		result.SuccessRate = float64(totalPassed) / float64(totalTasks) * 100
		result.AvgDurationMs = float64(totalDuration) / float64(totalTasks)
	}
	result.BySuite = allResults

	// Build scorecard
	result.Scorecard = buildScorecard(allResults)

	return result
}

// buildScorecard maps suite results to use case automation scores.
func buildScorecard(results []SuiteEvalResult) PlatformScorecard {
	cases := map[string]UseCaseScore{
		"platform_eval":          {Name: "Platform Self-Evaluation", AutomationFit: 85, Frequency: "weekly"},
		"godev":                  {Name: "Go Development", AutomationFit: 90, Frequency: "on-demand"},
		"code_review":            {Name: "Code Review", AutomationFit: 95, Frequency: "per-PR"},
		"devops_ci":              {Name: "DevOps CI/CD", AutomationFit: 85, Frequency: "per-commit"},
		"security_audit":         {Name: "Security Audit", AutomationFit: 80, Frequency: "weekly"},
		"data_pipeline":          {Name: "Data Pipeline", AutomationFit: 75, Frequency: "daily"},
		"deep_research":          {Name: "Deep Research", AutomationFit: 70, Frequency: "daily"},
		"think_tank":             {Name: "Think Tank Analysis", AutomationFit: 65, Frequency: "weekly"},
		"refactoring":            {Name: "Refactoring", AutomationFit: 75, Frequency: "on-demand"},
		"knowledge_qa":           {Name: "Knowledge QA", AutomationFit: 95, Frequency: "on-demand"},
		"kanban_workflow":        {Name: "Kanban Workflow", AutomationFit: 90, Frequency: "continuous"},
		"incident_investigation": {Name: "Incident Investigation", AutomationFit: 70, Frequency: "on-alert"},
		"finance":                {Name: "Financial Analysis", AutomationFit: 80, Frequency: "quarterly"},
		"health_monitoring":      {Name: "Health Monitoring", AutomationFit: 95, Frequency: "every 5m"},
		"cron_management":        {Name: "Cron Job Management", AutomationFit: 90, Frequency: "every 30m"},
		"self_evolution":         {Name: "Self-Evolution", AutomationFit: 60, Frequency: "continuous"},
		"meeting_notes":          {Name: "Meeting Notes", AutomationFit: 85, Frequency: "per-meeting"},
		"startup_simulation":     {Name: "Startup Simulation", AutomationFit: 50, Frequency: "quarterly"},
		"notebooklm_research":    {Name: "NotebookLM Research", AutomationFit: 80, Frequency: "daily"},
		"vault_management":       {Name: "Vault Management", AutomationFit: 85, Frequency: "daily"},
	}

	for _, r := range results {
		if c, ok := cases[r.Name]; ok {
			c.SuitePass = r.SuccessRate
			if r.SuccessRate >= 90 {
				c.Status = "optimized"
			} else if r.SuccessRate >= 70 {
				c.Status = "ready"
			} else if r.SuccessRate >= 40 {
				c.Status = "partial"
			} else {
				c.Status = "gap"
			}
			cases[r.Name] = c
		}
	}

	// Sort by automation fit
	sorted := make([]UseCaseScore, 0, len(cases))
	for _, v := range cases {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].AutomationFit > sorted[j].AutomationFit
	})

	sc := PlatformScorecard{UseCases: make(map[string]UseCaseScore)}
	for _, s := range sorted {
		key := strings.ToLower(strings.ReplaceAll(s.Name, " ", "_"))
		// Map display names back to suite keys
		for k, v := range cases {
			if v.Name == s.Name {
				key = k
				break
			}
		}
		sc.UseCases[key] = s
	}
	return sc
}

// FormatReport produces a human-readable report string.
func (r *PlatformEvalResult) FormatReport() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== BT Platform Evaluation Report ===\n"))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n", r.Timestamp))
	sb.WriteString(fmt.Sprintf("Suites: %d | Tasks: %d | Passed: %d | Failed: %d\n",
		r.TotalSuites, r.TotalTasks, r.Passed, r.Failed))
	sb.WriteString(fmt.Sprintf("Overall Success Rate: %.1f%% | Avg Duration: %.0fms\n\n",
		r.SuccessRate, r.AvgDurationMs))

	sb.WriteString("--- Suite Results ---\n")
	for _, s := range r.BySuite {
		icon := "✓"
		if s.SuccessRate < 70 {
			icon = "⚠"
		}
		if s.SuccessRate < 40 {
			icon = "✗"
		}
		sb.WriteString(fmt.Sprintf("%s %-24s %2d/%2d (%.0f%%) %5.0fms\n",
			icon, s.Name, s.Passed, s.TotalTasks, s.SuccessRate, s.AvgDuration))
	}

	sb.WriteString("\n--- Top 20 Use Cases (ranked by automation fit) ---\n")
	for _, uc := range r.Scorecard.UseCases {
		icon := "🟢"
		switch uc.Status {
		case "ready":
			icon = "🟡"
		case "partial":
			icon = "🟠"
		case "gap":
			icon = "🔴"
		}
		sb.WriteString(fmt.Sprintf("%s %-28s fit=%2d%% pass=%.0f%% freq=%-12s [%s]\n",
			icon, uc.Name, int(uc.AutomationFit), uc.SuitePass, uc.Frequency, uc.Status))
	}

	return sb.String()
}

// JSON returns the result as indented JSON.
func (r *PlatformEvalResult) JSON() string {
	b, _ := json.MarshalIndent(r, "", "  ")
	return string(b)
}
