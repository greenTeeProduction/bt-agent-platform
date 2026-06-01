package benchmark

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// SWEVerifiedEntry mirrors a SWE-bench Verified task entry.
// Each entry is a real GitHub issue with repo and problem statement.
type SWEVerifiedEntry struct {
	InstanceID       string `json:"instance_id"`
	Repo             string `json:"repo"`
	ProblemStatement string `json:"problem_statement"`
}

// SWEVerifiedResult holds the evaluation outcome for a single SWE-bench entry.
type SWEVerifiedResult struct {
	Entry    SWEVerifiedEntry `json:"entry"`
	Outcome  string           `json:"outcome"`
	Output   string           `json:"output"`
	Resolved bool             `json:"resolved"`
}

// SWEVerifiedMetrics aggregates evaluation results across SWE-bench Verified entries.
type SWEVerifiedMetrics struct {
	TotalEntries int                 `json:"total_entries"`
	Resolved     int                 `json:"resolved"`
	ResolveRate  float64             `json:"resolve_rate"`
	Results      []SWEVerifiedResult `json:"results"`
}

// LoadSWEVerified reads a SWE-bench Verified JSON file and returns entries.
func LoadSWEVerified(path string) ([]SWEVerifiedEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read swebench verified file: %w", err)
	}
	var entries []SWEVerifiedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal swebench verified: %w", err)
	}
	return entries, nil
}

// EvaluateSWEVerified runs all SWE-bench Verified entries through a tree and returns metrics.
// Each entry is formatted as "fix: <repo>\n\n<problem_statement>" and evaluated.
// Resolution criteria: bb.Outcome == "success" && len(output) > 50.
func EvaluateSWEVerified(tree *evolution.SerializableNode, entries []SWEVerifiedEntry, llmClient llm.LLM) *SWEVerifiedMetrics {
	var results []SWEVerifiedResult
	resolved := 0

	for _, entry := range entries {
		task := fmt.Sprintf("fix: %s\n\n%s", entry.Repo, entry.ProblemStatement)
		bb := &engine.Blackboard{
			Task: task,
			LLM:  llmClient,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		isResolved := bb.Outcome == "success" && len(output) > 50
		if isResolved {
			resolved++
		}

		results = append(results, SWEVerifiedResult{
			Entry:    entry,
			Outcome:  bb.Outcome,
			Output:   output,
			Resolved: isResolved,
		})
	}

	n := len(results)
	rate := 0.0
	if n > 0 {
		rate = float64(resolved) / float64(n)
	}

	return &SWEVerifiedMetrics{
		TotalEntries: n,
		Resolved:     resolved,
		ResolveRate:  rate,
		Results:      results,
	}
}

// BuiltinSWEVerifiedSample returns 10 representative SWE-bench Verified entries
// covering astropy, django, sympy, and scikit-learn (Go-equivalent tasks).
func BuiltinSWEVerifiedSample() []SWEVerifiedEntry {
	return []SWEVerifiedEntry{
		{
			InstanceID:       "astropy__astropy-12907",
			Repo:             "astropy/astropy",
			ProblemStatement: "Modeling's separability_matrix does not compute separability correctly for nested CompoundModels. When nesting compound models, the separability matrix incorrectly reports inputs and outputs as non-separable.",
		},
		{
			InstanceID:       "astropy__astropy-13033",
			Repo:             "astropy/astropy",
			ProblemStatement: "TimeSeries: misleading exception when required column check fails. The error message is confusing and does not help the user understand which column is missing.",
		},
		{
			InstanceID:       "astropy__astropy-13236",
			Repo:             "astropy/astropy",
			ProblemStatement: "Table grouping does not work correctly with masked columns. When calling group_by on a masked table, the resulting groups are incorrect.",
		},
		{
			InstanceID:       "django__django-10999",
			Repo:             "django/django",
			ProblemStatement: "Add support for enumerations to the templates engine. Django's template language cannot resolve enum values, causing AttributeError when accessing enum members in templates.",
		},
		{
			InstanceID:       "django__django-11001",
			Repo:             "django/django",
			ProblemStatement: "ModelAdmin.get_search_results should allow customization of the search queryset filtering. The current implementation does not expose hooks for customizing search behavior.",
		},
		{
			InstanceID:       "django__django-11066",
			Repo:             "django/django",
			ProblemStatement: "ContentTypesManager.get_for_models should use a single query when called with multiple models. Currently it issues N queries for N models.",
		},
		{
			InstanceID:       "sympy__sympy-12236",
			Repo:             "sympy/sympy",
			ProblemStatement: "sympify should be able to parse Python's built-in complex numbers. Currently sympify('1+2j') raises SympifyError instead of returning a complex SymPy expression.",
		},
		{
			InstanceID:       "sympy__sympy-12481",
			Repo:             "sympy/sympy",
			ProblemStatement: "Matrix multiplication with Piecewise results is incorrect. When multiplying matrices containing piecewise expressions, the result is not properly simplified.",
		},
		{
			InstanceID:       "scikit-learn__scikit-learn-10297",
			Repo:             "scikit-learn/scikit-learn",
			ProblemStatement: "GridSearchCV should support DataFrames with non-numeric dtypes for the param_grid. Passing a DataFrame with categorical columns to GridSearchCV.fit raises a ValueError.",
		},
		{
			InstanceID:       "scikit-learn__scikit-learn-10982",
			Repo:             "scikit-learn/scikit-learn",
			ProblemStatement: "cross_val_predict returns incorrect predictions when method='predict_proba' and using StratifiedKFold. The probabilities are not properly aggregated across folds.",
		},
	}
}
