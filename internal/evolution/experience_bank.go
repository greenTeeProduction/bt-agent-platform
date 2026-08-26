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

const (
	// experienceBankCap bounds the bank. Without it every Add grows the slice
	// and rewrites the whole file forever (the bank is append-only otherwise).
	experienceBankCap = 500

	// experienceReuseProtection: entries reused at least this many times are
	// evicted only after every less-proven entry is gone — reuse count is the
	// strongest signal an experience keeps paying off.
	experienceReuseProtection = 3
)

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
	// Oversized legacy files (written by unbounded builds) are capped here so
	// the bound holds from the first tick, not only after the next Add.
	eb.enforceCapLocked()
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
// failureContext is an optional originating task/failure description (e.g.
// blackboard.LastFailureTask). When supplied, it is appended to entry.Context
// so a later Retrieve query built from that same failing task's text can
// match on task semantics rather than only on tree-type/operation boilerplate.
//
// Only mutations with positive fitness delta are stored (regressions are ignored).
func (eb *ExperienceBank) AddFromMutation(
	tree *SerializableNode,
	op MutationOp,
	beforeFitness, afterFitness float64,
	llmClient llm.LLM,
	failureContext ...string,
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

	if len(failureContext) > 0 && failureContext[0] != "" {
		entry.Context = fmt.Sprintf("%s; failing_task=%s", entry.Context, failureContext[0])
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

// MarkReused increments TimesReused for the entries with the given IDs and
// persists the bank, so reuse statistics survive restarts. Like addEntry it
// is a full-file rewrite, so it merges on-disk entries first under the
// sidecar file lock — otherwise a bank loaded at startup would blast its
// stale memory over everything the other writer persisted since load. The
// increments are applied AFTER the merge so they land on the merged view.
func (eb *ExperienceBank) MarkReused(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	release, lockErr := acquireExperienceLock(eb.PersistPath)
	if lockErr == nil {
		defer release()
	}
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.mergeFromDiskLocked()
	for i := range eb.Entries {
		if idSet[eb.Entries[i].ID] {
			eb.Entries[i].TimesReused++
		}
	}
	eb.enforceCapLocked()
	return eb.persistLocked()
}

// TransferExperiences finds experiences recorded against sourceTree that may
// apply to targetTree. Returns up to 5 entries sorted by quality score,
// filtered to sourceTree's own provenance — a caller seeding targetTree's
// pool (see IslandModel.Migrate) is responsible for re-tagging copies with
// the destination tree type.
func (eb *ExperienceBank) TransferExperiences(sourceTree, _ string) []ExperienceEntry {
	return eb.RetrieveByTreeType(sourceTree, 5)
}

// SeedDomain copies up to 5 top experiences transferred from sourceTree into
// targetTree's own experience pool, re-tagged with the target tree type so a
// later RetrieveByTreeType(targetTree, ...) surfaces them as native
// contributions — this is what closes the cross-domain feedback loop.
// sourceTree's own entries are left untouched; each copy gets a fresh ID so
// it never collides with (or is deduped against) the original. Returns the
// number of experiences successfully seeded.
func (eb *ExperienceBank) SeedDomain(sourceTree, targetTree string) int {
	seeded := 0
	for _, e := range eb.TransferExperiences(sourceTree, targetTree) {
		e.ID = fmt.Sprintf("transfer_%s_to_%s_%s_%d", sourceTree, targetTree, e.ID, time.Now().UnixNano())
		e.TreeType = targetTree
		e.TimesReused = 0
		e.CreatedAt = time.Now()
		if err := eb.addEntry(e); err == nil {
			seeded++
		}
	}
	return seeded
}

// Persist writes the experience bank to disk atomically. It runs the same
// lock→merge→write sequence as addEntry and MarkReused so no exported write
// path can rewrite the file from stale memory and drop a concurrent writer's
// entries. If the sidecar lock cannot be acquired, it falls back to the
// unlocked (still merged) path rather than failing the persist.
func (eb *ExperienceBank) Persist() error {
	release, lockErr := acquireExperienceLock(eb.PersistPath)
	if lockErr == nil {
		defer release()
	}
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.mergeFromDiskLocked()
	eb.enforceCapLocked()
	return eb.persistLocked()
}

// persistLocked marshals the current entries and atomically replaces the
// persisted file (write tmp + rename). Caller must hold eb.mu (read or
// write). Callers that merge from disk first must also hold the sidecar
// file lock across merge and rename, or a concurrent writer can rename its
// own snapshot into place in between and have it silently overwritten.
func (eb *ExperienceBank) persistLocked() error {
	// Serialize only the entries (not the mutex or path fields)
	data, err := json.MarshalIndent(struct {
		Entries []ExperienceEntry `json:"entries"`
	}{Entries: eb.Entries}, "", "  ")
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
func (eb *ExperienceBank) Stats() map[string]any {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := map[string]any{
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

// addEntry adds an entry, enforces the capacity cap, and persists the bank.
// Before the full-file rewrite, on-disk entries are merged back in so a
// concurrent writer's adds (daemon + gardener share experience.json) are
// never silently dropped. The sidecar file lock is held from the merge
// through the rename: without it a second process can rename its own
// snapshot into place inside that window and have it overwritten. If the
// lock cannot be acquired (e.g. read-only dir), fall back to the unlocked
// path rather than dropping the entry.
func (eb *ExperienceBank) addEntry(entry ExperienceEntry) error {
	release, lockErr := acquireExperienceLock(eb.PersistPath)
	if lockErr == nil {
		defer release()
	}
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.mergeFromDiskLocked()
	eb.Entries = append(eb.Entries, entry)
	eb.enforceCapLocked()
	return eb.persistLocked()
}

// mergeFromDiskLocked reloads the persisted file and merges its entries into
// memory by ID: disk-only entries are adopted, and for IDs present in both the
// higher TimesReused wins (reuse counts recorded by the other writer must
// survive this writer's rewrite). A missing or corrupt file leaves the
// in-memory state untouched. Caller must hold eb.mu.
func (eb *ExperienceBank) mergeFromDiskLocked() {
	data, err := os.ReadFile(eb.PersistPath)
	if err != nil {
		return
	}
	var wrapper struct {
		Entries []ExperienceEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}
	index := make(map[string]int, len(eb.Entries))
	for i, e := range eb.Entries {
		index[e.ID] = i
	}
	for _, d := range wrapper.Entries {
		if i, ok := index[d.ID]; ok {
			if d.TimesReused > eb.Entries[i].TimesReused {
				eb.Entries[i].TimesReused = d.TimesReused
			}
			continue
		}
		eb.Entries = append(eb.Entries, d)
	}
}

// enforceCapLocked evicts entries until the bank fits experienceBankCap.
// Eviction order: lowest QualityScore first, oldest first among equal quality.
// Entries with TimesReused >= experienceReuseProtection are protected — they
// are only evicted once every unprotected entry is gone, so the cap still
// holds even in a fully protected bank. Caller must hold eb.mu.
func (eb *ExperienceBank) enforceCapLocked() {
	excess := len(eb.Entries) - experienceBankCap
	if excess <= 0 {
		return
	}

	order := make([]int, len(eb.Entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ea, ebEntry := eb.Entries[order[a]], eb.Entries[order[b]]
		protA := ea.TimesReused >= experienceReuseProtection
		protB := ebEntry.TimesReused >= experienceReuseProtection
		if protA != protB {
			return !protA // unprotected entries are evicted first
		}
		if ea.QualityScore != ebEntry.QualityScore {
			return ea.QualityScore < ebEntry.QualityScore
		}
		return ea.CreatedAt.Before(ebEntry.CreatedAt)
	})

	evict := make(map[int]bool, excess)
	for _, idx := range order[:excess] {
		evict[idx] = true
	}
	kept := make([]ExperienceEntry, 0, experienceBankCap)
	for i, e := range eb.Entries {
		if !evict[i] {
			kept = append(kept, e)
		}
	}
	eb.Entries = kept
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
