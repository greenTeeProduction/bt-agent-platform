package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// banditStats is the persisted outcome store for a BanditSelector node, keyed
// by child name (fallback "child_<i>" for unnamed children). Each slice is
// trimmed to the node's configured window on write.
type banditStats struct {
	Outcomes map[string][]bool `json:"outcomes"`
}

// banditStatsDir is the writable directory for bandit outcome files, override
// via BT_BANDIT_DIR (used by tests to avoid touching the real home directory).
func banditStatsDir() string {
	if d := os.Getenv("BT_BANDIT_DIR"); d != "" {
		return d
	}
	return filepath.Join(homeDir(), ".go-bt-evolve", "data", "bandit")
}

func banditStatsPath(nodeName string) string {
	return filepath.Join(banditStatsDir(), nodeName+".json")
}

// loadBanditStats loads a node's outcome stats, tolerating a missing file or
// corrupt JSON by starting empty — persistence problems must never fail the
// node itself.
func loadBanditStats(nodeName string) *banditStats {
	data, err := os.ReadFile(banditStatsPath(nodeName))
	if err != nil {
		return &banditStats{Outcomes: map[string][]bool{}}
	}
	var stats banditStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return &banditStats{Outcomes: map[string][]bool{}}
	}
	if stats.Outcomes == nil {
		stats.Outcomes = map[string][]bool{}
	}
	return &stats
}

// saveBanditStats atomically persists stats via tmp file + rename (ADR-003,
// mirroring SaveSLOMetrics in slo_persistence.go). Errors are swallowed:
// recording outcomes must never fail the tree.
func saveBanditStats(nodeName string, stats *banditStats) {
	dir := banditStatsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return
	}
	path := banditStatsPath(nodeName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// banditChildKey returns the stats key for child i: its Name, or "child_<i>"
// when unnamed.
func banditChildKey(child *evolution.SerializableNode, i int) string {
	if strings.TrimSpace(child.Name) != "" {
		return child.Name
	}
	return fmt.Sprintf("child_%d", i)
}

// recordBanditOutcome appends a terminal outcome for key and trims the slice
// to the last `window` entries.
func recordBanditOutcome(stats *banditStats, key string, success bool, window int) {
	outcomes := append(stats.Outcomes[key], success)
	if window > 0 && len(outcomes) > window {
		outcomes = outcomes[len(outcomes)-window:]
	}
	stats.Outcomes[key] = outcomes
}

// banditUCB1Order returns child indices ordered for selection: untried
// children (n=0) first in declaration order, then descending
// mean + sqrt(2*ln(total)/n), where mean is the success ratio over the
// sliding window and total is the sum of all children's window counts.
func banditUCB1Order(keys []string, stats *banditStats) []int {
	n := len(keys)
	counts := make([]int, n)
	means := make([]float64, n)
	total := 0
	for i, k := range keys {
		outcomes := stats.Outcomes[k]
		counts[i] = len(outcomes)
		total += counts[i]
		if counts[i] > 0 {
			successes := 0
			for _, o := range outcomes {
				if o {
					successes++
				}
			}
			means[i] = float64(successes) / float64(counts[i])
		}
	}

	score := func(i int) float64 {
		return means[i] + math.Sqrt(2*math.Log(float64(total))/float64(counts[i]))
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if (counts[i] == 0) != (counts[j] == 0) {
			return counts[i] == 0 // untried arms sort first
		}
		if counts[i] == 0 { // both untried: keep declaration order
			return i < j
		}
		return score(i) > score(j)
	})
	return order
}

// BuildBanditSelector builds a flag-gated UCB1 Selector.
//
// Disabled (default: metadata.enabled absent or false) it behaves exactly
// like a plain Selector — no cursor, no memory across ticks, first child to
// return SUCCESS wins, RUNNING propagates immediately — but additionally
// records each tried child's terminal outcome (SUCCESS/FAILURE; RUNNING is
// not recorded) into a per-node JSON stats file.
//
// Enabled it orders children by UCB1 before trying them: untried children
// first (declaration order), then descending mean + sqrt(2*ln(total)/n) over
// the metadata.window sliding window of recorded outcomes. It then runs a
// Selector over that order, recording outcomes the same way.
//
// Stats persist to <BT_BANDIT_DIR or ~/.go-bt-evolve/data/bandit>/<node
// name>.json, written atomically (tmp + rename, ADR-003). A missing or
// corrupt stats file starts empty rather than failing the node. A malformed
// config (zero children) yields a failing action instead of panicking.
func BuildBanditSelector(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "BanditSelector requires at least one child: " + node.Name
			return -1
		})
	}

	enabled, _ := node.Metadata["enabled"].(bool)
	window := 50
	switch v := node.Metadata["window"].(type) {
	case int:
		window = v
	case float64:
		window = int(v)
	}

	children := make([]btcore.Command[Blackboard], len(node.Children))
	keys := make([]string, len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
		keys[i] = banditChildKey(&node.Children[i], i)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		stats := loadBanditStats(node.Name)

		order := make([]int, len(children))
		for i := range order {
			order[i] = i
		}
		if enabled {
			order = banditUCB1Order(keys, stats)
		}

		dirty := false
		result := -1
		for _, idx := range order {
			code := children[idx].Run(ctx)
			if code == 0 {
				if dirty {
					saveBanditStats(node.Name, stats)
				}
				return 0 // RUNNING propagates immediately; not recorded
			}
			recordBanditOutcome(stats, keys[idx], code > 0, window)
			dirty = true
			if code > 0 {
				result = 1
				break
			}
		}
		if dirty {
			saveBanditStats(node.Name, stats)
		}
		return result
	})
}
