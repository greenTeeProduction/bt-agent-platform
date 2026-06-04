// Package evolution — ExperienceBank stores successful mutation experiences for
// cross-generation reuse. Adapted from EvoRepair (arXiv:2605.30105) which achieves
// 90.46% repair rate via experience-based self-evolution with 5-dimension entries,
// LLM-as-judge scoring, and retrieval-augmented mutation guidance.
//
// EvoRepair shows that the experience bank is the SINGLE highest-impact improvement
// after quality gates: it makes every mutation smarter by retrieving past successes
// before applying new mutations, enabling cross-tree transfer and progressive
// refinement.
//
// Key integration: Population.EvolveWithExperience() in learning.go.
package evolution

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
)

// ─── Experience Bank ────────────────────────────────────────────────────────

// ExperienceBank stores successful mutation experiences for cross-generation
// and cross-tree reuse. Every mutation that improves fitness is recorded with
// EvoRepair-style 5-dimension context and retrieved before subsequent mutations.
//
// Thread-safe via sync.RWMutex. Persists to disk on every Add.
type ExperienceBank struct {
	mu          sync.RWMutex      `json:"-"`
	Entries     []ExperienceEntry `json:"entries"`
	PersistPath string            `json:"-"` // path to experience.json
}

// ExperienceEntry records one successful mutation with EvoRepair-style
// 5-dimension (ABCDE) analysis. Each entry captures not just WHAT worked
// but WHY, HOW, and in WHAT context — enabling intelligent retrieval.
type ExperienceEntry struct {
	ID         string `json:"id"`
	TreeType   string `json:"tree_type"`   // GoDev, Merged, Default, etc.
	MutationOp string `json:"mutation_op"` // add_before, wrap_retry, etc.
	TargetNode string `json:"target_node"` // name of the mutated node

	// EvoRepair 5 dimensions — ABCD+E
	Context    string `json:"context"`    // A: why this mutation? (problem context)
	Strategy   string `json:"strategy"`   // B: what approach? (mutation strategy)
	Trajectory string `json:"trajectory"` // C: what happened? (execution trace)
	Summary    string `json:"summary"`    // D: prescriptive rules (what to do)
	Reflection string `json:"reflection"` // E: what could be better? (meta-cognition)

	FitnessDelta float64   `json:"fitness_delta"` // after - before
	QualityScore float64   `json:"quality_score"` // LLM-as-judge, 0.0–1.0
	CreatedAt    time.Time `json:"created_at"`
	TimesReused  int       `json:"times_reused"`
}

// NewExperienceBank creates a new experience bank with persistence at the
// given directory (~/.go-bt-evolve/experience/). If the path already exists
// on disk, it loads existing entries.
func NewExperienceBank(dir string) (*ExperienceBank, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create experience dir: %w", err)
	}
	eb := &ExperienceBank{
		PersistPath: filepath.Join(dir, "experience.json"),
	}
	// Load existing entries if present
	data, err := os.ReadFile(eb.PersistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return eb, nil // fresh bank
		}
		return nil, fmt.Errorf("read experience file: %w", err)
	}
	// Read wrapper struct (matching Persist format)
	var wrapper struct {
		Entries []ExperienceEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		// Corrupt file? Return empty bank but preserve the file for debugging.
		eb.Entries = nil
		return eb, fmt.Errorf("unmarshal experience file (starting fresh): %w", err)
	}
	eb.Entries = wrapper.Entries
	return eb, nil
}

// Count returns the number of stored experience entries.
func (eb *ExperienceBank) Count() int {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	return len(eb.Entries)
}

// AddFromMutation records a successful mutation as an experience entry.
// If an LLM is provided, it generates the 5-dimension summary and quality score.
// If llm is nil, the entry is stored with minimal metadata (just the raw data).
//
// Only mutations with positive fitness delta are stored (regressions are ignored).
func (eb *ExperienceBank) AddFromMutation(
	tree *SerializableNode,
	op MutationOp,
	beforeFitness, afterFitness float64,
	llmClient llm.LLM,
) error {
	fitnessDelta := afterFitness - beforeFitness
	if fitnessDelta <= 0 {
		return nil // don't store regressions
	}

	entry := ExperienceEntry{
		ID:           fmt.Sprintf("%s_%s_%d", extractTreeType(tree), op.Operation, time.Now().UnixNano()),
		TreeType:     extractTreeType(tree),
		MutationOp:   op.Operation,
		TargetNode:   op.Target,
		FitnessDelta: fitnessDelta,
		CreatedAt:    time.Now(),
	}

	// If LLM available, enrich with 5-dimension analysis
	if llmClient != nil {
		eb.enrichEntry(&entry, llmClient)
	} else {
		// Minimal context without LLM
		entry.Context = fmt.Sprintf("tree=%s, nodes=%d, fitness_before=%.3f", entry.TreeType, CountNodes(tree), beforeFitness)
		entry.Strategy = fmt.Sprintf("operation=%s on node=%s", entry.MutationOp, entry.TargetNode)
		entry.Trajectory = fmt.Sprintf("fitness %.3f → %.3f (Δ=%.3f)", beforeFitness, afterFitness, fitnessDelta)
		entry.Summary = fmt.Sprintf("Apply %s to %s nodes in %s trees for +%.3f fitness gain", entry.MutationOp, entry.TargetNode, entry.TreeType, fitnessDelta)
		entry.Reflection = "No LLM available for deeper analysis"
		entry.QualityScore = math.Min(fitnessDelta/0.2, 1.0) // normalize delta to 0–1
	}

	return eb.addEntry(entry)
}

// Retrieve finds the top-K most similar experience entries for a query.
// Uses Jaccard token similarity to find semantically relevant experiences,
// then reranks by μ*similarity + (1-μ)*quality_score.
//
// The query is typically the tree type + mutation context (e.g., "GoDev add_before").
func (eb *ExperienceBank) Retrieve(query string, topK int) []ExperienceEntry {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	if len(eb.Entries) == 0 {
		return nil
	}

	type scored struct {
		entry ExperienceEntry
		score float64
	}

	const mu = 0.6 // weight of similarity vs quality score
	queryTokens := tokenize(query)

	candidates := make([]scored, 0, 16)
	for _, e := range eb.Entries {
		// Build search text from entry fields
		searchText := fmt.Sprintf("%s %s %s %s %s %s",
			e.TreeType, e.MutationOp, e.TargetNode,
			e.Context, e.Strategy, e.Summary)
		searchTokens := tokenize(searchText)

		sim := jaccardSimilarity(queryTokens, searchTokens)
		score := mu*sim + (1-mu)*e.QualityScore

		candidates = append(candidates, scored{entry: e, score: score})
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Return top-K
	if topK > len(candidates) {
		topK = len(candidates)
	}
	result := make([]ExperienceEntry, topK)
	for i := 0; i < topK; i++ {
		result[i] = candidates[i].entry
	}
	return result
}

// RetrieveByTreeType returns entries filtered by tree type, sorted by quality score.
func (eb *ExperienceBank) RetrieveByTreeType(treeType string, topK int) []ExperienceEntry {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	var matching []ExperienceEntry
	for _, e := range eb.Entries {
		if strings.EqualFold(e.TreeType, treeType) {
			matching = append(matching, e)
		}
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i].QualityScore > matching[j].QualityScore
	})

	if topK > len(matching) {
		topK = len(matching)
	}
	return matching[:topK]
}

// TransferExperiences finds experiences from sourceTree that may apply to targetTree.
// Returns entries sorted by quality score — cross-tree transfer relies on the LLM
// or similarity matching to determine applicability.
func (eb *ExperienceBank) TransferExperiences(_, targetTree string) []ExperienceEntry {
	return eb.Retrieve(targetTree, 5)
}

// Persist writes the experience bank to disk atomically.
func (eb *ExperienceBank) Persist() error {
	eb.mu.RLock()
	// Serialize only the entries (not the mutex or path fields)
	data, err := json.MarshalIndent(struct {
		Entries []ExperienceEntry `json:"entries"`
	}{Entries: eb.Entries}, "", "  ")
	eb.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal experience bank: %w", err)
	}
	tmp := eb.PersistPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, eb.PersistPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Stats returns a summary of the experience bank.
func (eb *ExperienceBank) Stats() map[string]interface{} {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := map[string]interface{}{
		"total_entries": len(eb.Entries),
	}

	if len(eb.Entries) == 0 {
		return stats
	}

	// Count by tree type
	treeTypes := make(map[string]int)
	mutationOps := make(map[string]int)
	var totalQuality, totalDelta float64
	var mostRecent time.Time

	for _, e := range eb.Entries {
		treeTypes[e.TreeType]++
		mutationOps[e.MutationOp]++
		totalQuality += e.QualityScore
		totalDelta += e.FitnessDelta
		if e.CreatedAt.After(mostRecent) {
			mostRecent = e.CreatedAt
		}
	}

	stats["by_tree_type"] = treeTypes
	stats["by_mutation_op"] = mutationOps
	stats["avg_quality_score"] = totalQuality / float64(len(eb.Entries))
	stats["avg_fitness_delta"] = totalDelta / float64(len(eb.Entries))
	stats["most_recent"] = mostRecent.Format(time.RFC3339)

	return stats
}

// ─── Internal helpers ───────────────────────────────────────────────────────

// addEntry adds an entry and persists the bank.
func (eb *ExperienceBank) addEntry(entry ExperienceEntry) error {
	eb.mu.Lock()
	eb.Entries = append(eb.Entries, entry)
	eb.mu.Unlock()
	return eb.Persist()
}

// enrichEntry uses the LLM to generate 5-dimension analysis and quality score.
func (eb *ExperienceBank) enrichEntry(entry *ExperienceEntry, llmClient llm.LLM) {
	prompt := fmt.Sprintf(`Analyze this successful behavior tree mutation:

Tree Type: %s
Mutation: %s on node %s
Fitness Change: +%.3f

Provide a structured analysis in exactly 5 labeled sections:

A|CONTEXT: Why was this mutation needed? (1 sentence)
B|STRATEGY: What approach was used? (1 sentence)
C|TRAJECTORY: What happened during execution? (1 sentence)
D|SUMMARY: What prescriptive rule should future mutations follow? (1 sentence)
E|REFLECTION: What could be improved? (1 sentence)

QUALITY_SCORE: Rate this mutation's quality 0.0-1.0 (single number at end)`,
		entry.TreeType, entry.MutationOp, entry.TargetNode, entry.FitnessDelta)

	response, err := llmClient.Generate(prompt)
	if err != nil || response == "" {
		// LLM unavailable — use defaults
		entry.Context = fmt.Sprintf("Mutation %s on %s in %s tree", entry.MutationOp, entry.TargetNode, entry.TreeType)
		entry.Strategy = "Standard mutation operator"
		entry.Trajectory = fmt.Sprintf("Fitness improved by +%.3f", entry.FitnessDelta)
		entry.Summary = fmt.Sprintf("Consider %s on %s nodes in %s context", entry.MutationOp, entry.TargetNode, entry.TreeType)
		entry.Reflection = "LLM unavailable — no meta-analysis"
		entry.QualityScore = math.Min(entry.FitnessDelta/0.2, 1.0)
		return
	}

	// Parse the 5 dimensions from LLM response
	eb.parseDimensions(entry, response)
}

// parseDimensions extracts the ABCDE sections and quality score from LLM output.
func (eb *ExperienceBank) parseDimensions(entry *ExperienceEntry, response string) {
	lines := strings.Split(response, "\n")
	currentSection := ""
	var sections = make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "A|CONTEXT:") || strings.HasPrefix(line, "A|Context:") {
			currentSection = "A"
			sections["A"] = strings.TrimPrefix(strings.TrimPrefix(line, "A|CONTEXT:"), "A|Context:")
			sections["A"] = strings.TrimSpace(sections["A"])
		} else if strings.HasPrefix(line, "B|STRATEGY:") || strings.HasPrefix(line, "B|Strategy:") {
			currentSection = "B"
			sections["B"] = strings.TrimPrefix(strings.TrimPrefix(line, "B|STRATEGY:"), "B|Strategy:")
			sections["B"] = strings.TrimSpace(sections["B"])
		} else if strings.HasPrefix(line, "C|TRAJECTORY:") || strings.HasPrefix(line, "C|Trajectory:") {
			currentSection = "C"
			sections["C"] = strings.TrimPrefix(strings.TrimPrefix(line, "C|TRAJECTORY:"), "C|Trajectory:")
			sections["C"] = strings.TrimSpace(sections["C"])
		} else if strings.HasPrefix(line, "D|SUMMARY:") || strings.HasPrefix(line, "D|Summary:") {
			currentSection = "D"
			sections["D"] = strings.TrimPrefix(strings.TrimPrefix(line, "D|SUMMARY:"), "D|Summary:")
			sections["D"] = strings.TrimSpace(sections["D"])
		} else if strings.HasPrefix(line, "E|REFLECTION:") || strings.HasPrefix(line, "E|Reflection:") {
			currentSection = "E"
			sections["E"] = strings.TrimPrefix(strings.TrimPrefix(line, "E|REFLECTION:"), "E|Reflection:")
			sections["E"] = strings.TrimSpace(sections["E"])
		} else if strings.HasPrefix(line, "QUALITY_SCORE:") || strings.HasPrefix(line, "Quality_Score:") {
			scoreStr := strings.TrimPrefix(strings.TrimPrefix(line, "QUALITY_SCORE:"), "Quality_Score:")
			scoreStr = strings.TrimSpace(scoreStr)
			if score, err := parseFloat(scoreStr); err == nil {
				entry.QualityScore = math.Max(0, math.Min(1.0, score))
			}
		} else if currentSection != "" && line != "" {
			// Continuation line for current section
			sections[currentSection] += " " + line
		}
	}

	// Fill in parsed values
	if v, ok := sections["A"]; ok {
		entry.Context = v
	} else {
		entry.Context = fmt.Sprintf("%s tree mutation", entry.TreeType)
	}
	if v, ok := sections["B"]; ok {
		entry.Strategy = v
	} else {
		entry.Strategy = entry.MutationOp
	}
	if v, ok := sections["C"]; ok {
		entry.Trajectory = v
	} else {
		entry.Trajectory = fmt.Sprintf("fitness_delta=%.3f", entry.FitnessDelta)
	}
	if v, ok := sections["D"]; ok {
		entry.Summary = v
	} else {
		entry.Summary = fmt.Sprintf("Use %s on %s in %s trees", entry.MutationOp, entry.TargetNode, entry.TreeType)
	}
	if v, ok := sections["E"]; ok {
		entry.Reflection = v
	} else {
		entry.Reflection = "No reflection available"
	}

	// Default quality score if not parsed
	if entry.QualityScore == 0 {
		entry.QualityScore = 0.5
	}
}

// ─── Similarity helpers ─────────────────────────────────────────────────────

// tokenize splits text into lowercase word tokens for Jaccard similarity.
func tokenize(text string) []string {
	words := strings.Fields(strings.ToLower(text))
	seen := make(map[string]bool)
	var tokens []string
	for _, w := range words {
		// Clean punctuation
		w = strings.Trim(w, ".,;:!?\"'()[]{}\n\t")
		if len(w) > 1 && !seen[w] {
			seen[w] = true
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// jaccardSimilarity computes Jaccard index between two token sets.
// Returns 0.0 (no overlap) to 1.0 (identical sets).
func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	setA := make(map[string]bool, len(a))
	for _, t := range a {
		setA[t] = true
	}
	intersection := 0
	for _, t := range b {
		if setA[t] {
			intersection++
		}
	}
	union := len(setA)
	for _, t := range b {
		if !setA[t] {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// parseFloat is a simple wrapper that returns error if s is empty.
func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// extractTreeType derives a tree type label from the tree's name.
func extractTreeType(tree *SerializableNode) string {
	if tree == nil {
		return "Unknown"
	}
	name := strings.ToLower(tree.Name)
	switch {
	case strings.Contains(name, "godev"):
		return "GoDev"
	case strings.Contains(name, "merged"):
		return "Merged"
	case strings.Contains(name, "mainsequence") || strings.Contains(name, "default"):
		return "Default"
	case strings.Contains(name, "stockfish"):
		return "Stockfish"
	case strings.Contains(name, "kanban"):
		return "Kanban"
	case strings.Contains(name, "goap"):
		return "GOAP"
	default:
		// Use first word of name as type
		parts := strings.Split(tree.Name, "_")
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
		return tree.Name
	}
}
