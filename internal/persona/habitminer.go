package persona

import (
	"math"
	"sort"
	"strings"
	"time"
)

// RecurringPattern is a cluster of similar tasks the user keeps asking for —
// the trigger for proposing an automation (ADR-133: a task repeated ≥3 times
// within the window yields a proposal).
type RecurringPattern struct {
	// Representative is the most recent task text in the cluster.
	Representative string   `json:"representative"`
	Examples       []string `json:"examples"`
	Count          int      `json:"count"`
	FirstSeen      int64    `json:"first_seen"`
	LastSeen       int64    `json:"last_seen"`
	// TreeIDs are the trees that handled the clustered tasks (dominant first).
	TreeIDs []string `json:"tree_ids,omitempty"`
	// SuggestedGoal is a human-readable automation goal for the goal factory
	// (Phase 2 turns this into a grounded goap.Goal).
	SuggestedGoal string `json:"suggested_goal"`
}

// HabitMiner clusters a user's interactions into recurring patterns.
//
// Similarity is embedding-based when Embed is set and succeeds (cosine over
// per-task vectors, mirroring knowledge's discovery pipeline) and falls back
// to keyword Jaccard when Ollama is unavailable — habit mining must keep
// working offline.
type HabitMiner struct {
	// Embed converts a task to a vector. Optional; nil → keyword similarity.
	Embed func(text string) ([]float64, error)
	// Threshold is the minimum similarity for two tasks to share a cluster.
	Threshold float64
	// MinOccurrences is the cluster size that makes a pattern "recurring".
	MinOccurrences int
	// Window bounds how far back interactions are considered.
	Window time.Duration
}

// NewHabitMiner returns a miner with the ADR-133 defaults: ≥3 similar tasks
// within 14 days.
func NewHabitMiner() *HabitMiner {
	return &HabitMiner{
		Threshold:      0.5,
		MinOccurrences: 3,
		Window:         14 * 24 * time.Hour,
	}
}

// Mine clusters the interactions (greedy single-pass over recency-filtered
// tasks) and returns recurring patterns, most frequent first.
func (m *HabitMiner) Mine(interactions []Interaction, now time.Time) []RecurringPattern {
	threshold := m.Threshold
	if threshold <= 0 {
		threshold = 0.5
	}
	minOcc := m.MinOccurrences
	if minOcc <= 0 {
		minOcc = 3
	}
	window := m.Window
	if window <= 0 {
		window = 14 * 24 * time.Hour
	}

	cutoff := now.Add(-window).Unix()
	var recent []Interaction
	for _, rec := range interactions {
		if rec.Timestamp >= cutoff && strings.TrimSpace(rec.Task) != "" {
			recent = append(recent, rec)
		}
	}
	if len(recent) < minOcc {
		return nil
	}

	sim := m.similarityFn(recent)

	// Greedy clustering: each interaction joins the first cluster whose seed
	// is similar enough, else starts a new cluster. Deterministic given the
	// chronological input order.
	type cluster struct {
		seedIdx int
		members []int
	}
	var clusters []*cluster
	for i := range recent {
		placed := false
		for _, c := range clusters {
			if sim(c.seedIdx, i) >= threshold {
				c.members = append(c.members, i)
				placed = true
				break
			}
		}
		if !placed {
			clusters = append(clusters, &cluster{seedIdx: i, members: []int{i}})
		}
	}

	var patterns []RecurringPattern
	for _, c := range clusters {
		if len(c.members) < minOcc {
			continue
		}
		p := RecurringPattern{Count: len(c.members)}
		treeCounts := map[string]int{}
		for _, idx := range c.members {
			rec := recent[idx]
			p.Examples = append(p.Examples, rec.Task)
			if p.FirstSeen == 0 || rec.Timestamp < p.FirstSeen {
				p.FirstSeen = rec.Timestamp
			}
			if rec.Timestamp >= p.LastSeen {
				p.LastSeen = rec.Timestamp
				p.Representative = rec.Task
			}
			if rec.TreeID != "" {
				treeCounts[rec.TreeID]++
			}
		}
		p.TreeIDs = sortedByCount(treeCounts)
		p.SuggestedGoal = "Automate recurring task: " + p.Representative
		patterns = append(patterns, p)
	}

	sort.Slice(patterns, func(i, j int) bool {
		if patterns[i].Count != patterns[j].Count {
			return patterns[i].Count > patterns[j].Count
		}
		return patterns[i].LastSeen > patterns[j].LastSeen
	})
	return patterns
}

// similarityFn returns an index-based similarity over the recent slice,
// pre-computing embeddings once per task when Embed is available. Any
// embedding failure downgrades the whole run to keyword similarity so a
// mid-run Ollama outage cannot split clusters across two metrics.
func (m *HabitMiner) similarityFn(recent []Interaction) func(i, j int) float64 {
	if m.Embed != nil {
		vecs := make([][]float64, len(recent))
		ok := true
		for i, rec := range recent {
			v, err := m.Embed(rec.Task)
			if err != nil || len(v) == 0 {
				ok = false
				break
			}
			vecs[i] = v
		}
		if ok {
			return func(i, j int) float64 { return cosine(vecs[i], vecs[j]) }
		}
	}
	tokens := make([]map[string]bool, len(recent))
	for i, rec := range recent {
		tokens[i] = keywordSet(rec.Task)
	}
	return func(i, j int) float64 { return jaccard(tokens[i], tokens[j]) }
}

// keywordSet tokenizes a task into its significant lowercase words.
func keywordSet(task string) map[string]bool {
	set := make(map[string]bool)
	for w := range strings.FieldsSeq(strings.ToLower(task)) {
		w = strings.Trim(w, ",.!?;:\"'()[]{}")
		if len(w) > 3 && !stopwords[w] {
			set[w] = true
		}
	}
	return set
}

var stopwords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "what": true,
	"when": true, "where": true, "please": true, "then": true, "into": true,
	"about": true, "some": true, "have": true, "them": true, "they": true,
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func sortedByCount(counts map[string]int) []string {
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if counts[ids[i]] != counts[ids[j]] {
			return counts[ids[i]] > counts[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}
