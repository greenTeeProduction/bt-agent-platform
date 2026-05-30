package evaluator

import (
	"fmt"
	"sort"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// CascadeLevel defines the tier in the evaluation cascade.
type CascadeLevel int

const (
	LevelSkip    CascadeLevel = iota // don't evaluate (rejected by previous tier)
	LevelQuick                       // structural checks only, no LLM, ~1s
	LevelBench                       // subset of benchmark tasks, ~30s
	LevelFull                        // complete benchmark suite, ~5min
)

func (l CascadeLevel) String() string {
	switch l {
	case LevelSkip:
		return "skip"
	case LevelQuick:
		return "quick"
	case LevelBench:
		return "bench"
	case LevelFull:
		return "full"
	default:
		return "unknown"
	}
}

// CascadeResult holds per-tier evaluation results for one tree.
type CascadeResult struct {
	Tree       *evolution.SerializableNode
	QuickScore float64 // 0-100, structural fitness
	BenchScore float64 // 0-100, benchmark subset fitness
	FullScore  float64 // 0-100, full benchmark fitness
	Level      CascadeLevel
	Rejected   bool   // true if filtered out at a tier
	RejectReason string
}

// CascadeConfig configures the evaluation cascade.
type CascadeConfig struct {
	QuickThreshold float64 // min QuickScore to advance to Bench (default: 30)
	BenchThreshold float64 // min BenchScore to advance to Full (default: 50)
	MaxBenchCandidates int // max candidates that reach Bench tier (default: 10)
	MaxFullCandidates  int // max candidates that reach Full tier (default: 3)
}

// DefaultCascadeConfig returns sensible defaults for Jetson.
func DefaultCascadeConfig() CascadeConfig {
	return CascadeConfig{
		QuickThreshold:     30,
		BenchThreshold:     50,
		MaxBenchCandidates: 10,
		MaxFullCandidates:  3,
	}
}

// CascadeEvaluator runs the tiered evaluation pipeline.
// 80%+ of candidates should be filtered at Quick tier (structural, no LLM).
type CascadeEvaluator struct {
	config  CascadeConfig
	quickFn QuickEvalFunc
	benchFn BenchEvalFunc
	fullFn  FullEvalFunc
}

// QuickEvalFunc evaluates a tree structurally — no LLM calls.
// Returns 0-100 score.
type QuickEvalFunc func(tree *evolution.SerializableNode) float64

// BenchEvalFunc runs a subset of benchmark tasks.
type BenchEvalFunc func(tree *evolution.SerializableNode) float64

// FullEvalFunc runs the complete benchmark suite.
type FullEvalFunc func(tree *evolution.SerializableNode) float64

// NewCascadeEvaluator creates a cascade evaluator with the given tier functions.
func NewCascadeEvaluator(cfg CascadeConfig, quick QuickEvalFunc, bench BenchEvalFunc, full FullEvalFunc) *CascadeEvaluator {
	return &CascadeEvaluator{
		config:  cfg,
		quickFn: quick,
		benchFn: bench,
		fullFn:  full,
	}
}

// EvaluatePopulation runs the cascade on all individuals and returns results sorted by best score.
func (ce *CascadeEvaluator) EvaluatePopulation(individuals []evolution.Individual) []CascadeResult {
	results := make([]CascadeResult, len(individuals))

	// ── Tier 1: Quick (structural) — everyone goes through this ──
	quickPassed := make([]int, 0, len(individuals))
	for i, ind := range individuals {
		score := ce.quickFn(ind.Tree)
		results[i] = CascadeResult{
			Tree:       ind.Tree,
			QuickScore: score,
			Level:      LevelQuick,
		}
		if score >= ce.config.QuickThreshold {
			quickPassed = append(quickPassed, i)
		} else {
			results[i].Rejected = true
			results[i].RejectReason = fmt.Sprintf("Quick score %.1f below threshold %.1f", score, ce.config.QuickThreshold)
		}
	}

	// ── Tier 2: Bench — top N from Quick tier ──
	if len(quickPassed) > ce.config.MaxBenchCandidates {
		// Sort by QuickScore descending, keep top N
		sort.Slice(quickPassed, func(a, b int) bool {
			return results[quickPassed[a]].QuickScore > results[quickPassed[b]].QuickScore
		})
		// Mark excess as rejected
		for _, idx := range quickPassed[ce.config.MaxBenchCandidates:] {
			results[idx].Rejected = true
			results[idx].RejectReason = fmt.Sprintf("Quick score rank %d exceeds Bench capacity %d",
				ce.config.MaxBenchCandidates+1, ce.config.MaxBenchCandidates)
		}
		quickPassed = quickPassed[:ce.config.MaxBenchCandidates]
	}

	benchPassed := make([]int, 0, len(quickPassed))
	for _, idx := range quickPassed {
		score := ce.benchFn(results[idx].Tree)
		results[idx].BenchScore = score
		results[idx].Level = LevelBench
		if score >= ce.config.BenchThreshold {
			benchPassed = append(benchPassed, idx)
		} else {
			results[idx].Rejected = true
			if results[idx].RejectReason == "" {
				results[idx].RejectReason = fmt.Sprintf("Bench score %.1f below threshold %.1f", score, ce.config.BenchThreshold)
			}
		}
	}

	// ── Tier 3: Full — top N from Bench tier ──
	if len(benchPassed) > ce.config.MaxFullCandidates {
		sort.Slice(benchPassed, func(a, b int) bool {
			return results[benchPassed[a]].BenchScore > results[benchPassed[b]].BenchScore
		})
		for _, idx := range benchPassed[ce.config.MaxFullCandidates:] {
			results[idx].Rejected = true
			results[idx].RejectReason = fmt.Sprintf("Bench score rank %d exceeds Full capacity %d",
				ce.config.MaxFullCandidates+1, ce.config.MaxFullCandidates)
		}
		benchPassed = benchPassed[:ce.config.MaxFullCandidates]
	}

	for _, idx := range benchPassed {
		score := ce.fullFn(results[idx].Tree)
		results[idx].FullScore = score
		results[idx].Level = LevelFull
		results[idx].Rejected = false
	}

	// Sort by best available score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].BestScore() > results[j].BestScore()
	})

	return results
}

// BestScore returns the highest-tier score available for this result.
func (cr *CascadeResult) BestScore() float64 {
	switch {
	case cr.FullScore > 0 || cr.Level >= LevelFull:
		return cr.FullScore
	case cr.BenchScore > 0 || cr.Level >= LevelBench:
		return cr.BenchScore
	default:
		return cr.QuickScore
	}
}

// Passed returns true if this candidate passed all tiers it entered.
func (cr *CascadeResult) Passed() bool { return !cr.Rejected }

// Summary returns a human-readable one-line summary.
func (cr *CascadeResult) Summary() string {
	if cr.Rejected {
		return fmt.Sprintf("REJECTED(%s) Q=%.1f", cr.RejectReason, cr.QuickScore)
	}
	return fmt.Sprintf("PASSED(%s) Q=%.1f B=%.1f F=%.1f",
		cr.Level, cr.QuickScore, cr.BenchScore, cr.FullScore)
}

// ─── Built-in Quick evaluator (structural, no LLM) ───

// StructuralQuickEval scores a tree on structural properties only.
// Dimensions: node count (in-range bonus), path coverage (children exist),
// max depth (not too deep), condition coverage (has conditions), action coverage (has actions).
func StructuralQuickEval(tree *evolution.SerializableNode) float64 {
	if tree == nil {
		return 0
	}

	score := 0.0
	nodeCount := evolution.CountNodes(tree)
	maxDepth := maxTreeDepthEval(tree, 0)
	hasConditions, hasActions := countConditionsActions(tree)

	// Node count: optimal 15-40, penalty outside
	if nodeCount >= 15 && nodeCount <= 40 {
		score += 25
	} else if nodeCount >= 5 && nodeCount <= 60 {
		score += 15
	} else {
		score += 5
	}

	// Max depth: optimal 3-6, penalty outside
	if maxDepth >= 3 && maxDepth <= 6 {
		score += 25
	} else if maxDepth >= 2 && maxDepth <= 8 {
		score += 15
	} else {
		score += 5
	}

	// Has conditions (routing capability)
	if hasConditions > 0 {
		condScore := float64(hasConditions)
		if condScore > 10 {
			condScore = 10
		}
		score += condScore * 2.5 // max 25
	}

	// Has actions (execution capability)
	if hasActions > 0 {
		actScore := float64(hasActions)
		if actScore > 10 {
			actScore = 10
		}
		score += actScore * 2.5 // max 25
	}

	return score
}

// countConditionsActions counts condition and action nodes in the tree.
func countConditionsActions(node *evolution.SerializableNode) (conditions, actions int) {
	if node == nil {
		return 0, 0
	}
	if node.Type == "Condition" {
		conditions++
	}
	if node.Type == "Action" {
		actions++
	}
	for i := range node.Children {
		c, a := countConditionsActions(&node.Children[i])
		conditions += c
		actions += a
	}
	return
}

// maxTreeDepthEval computes maximum tree depth.
func maxTreeDepthEval(node *evolution.SerializableNode, current int) int {
	if node == nil {
		return current
	}
	maxD := current
	for i := range node.Children {
		d := maxTreeDepthEval(&node.Children[i], current+1)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// CascadeStats tracks aggregate cascade performance across a run.
type CascadeStats struct {
	Total      int
	PassedQuick int
	PassedBench int
	PassedFull  int
}

func (cs *CascadeStats) FilterRate() float64 {
	if cs.Total == 0 {
		return 0
	}
	return float64(cs.PassedQuick-cs.PassedFull) / float64(cs.Total) * 100
}

func (cs *CascadeStats) Summary() string {
	return fmt.Sprintf("Cascade: %d→Quick %d→Bench %d→Full (%d%% filtered)",
		cs.Total, cs.PassedQuick, cs.PassedBench, int(cs.FilterRate()))
}
