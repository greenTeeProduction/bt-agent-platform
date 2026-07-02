package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// banditLocks is a package-level per-stats-path mutex registry, mirroring
// the `semaphores` registry in semaphore_guard.go. Every load-modify-save
// cycle for a given stats path holds that path's mutex (acquired via
// banditLockFor), so two same-named BanditSelectors built for different
// trees, or concurrent goroutines each ticking their own instance (e.g.
// mcp_server.go dispatches tool calls to per-request goroutines), cannot
// interleave a read with another's write and drop an outcome — in
// particular it prevents concurrent writers from stepping on the same
// "<path>.tmp" staging file used by saveBanditStats.
//
// This registry only covers races within a single process: it is in-memory,
// so it cannot serialize two separate OS processes writing to the same
// stats path. The atomic tmp+rename write (ADR-003) still guarantees the
// file itself is never left half-written under a cross-process race, but a
// lost update (one process's save overwriting another's) remains possible.
// That's accepted: bandit stats are advisory ordering hints, not a
// consistency-critical ledger.
var banditLocks = struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}{m: map[string]*sync.Mutex{}}

func banditLockFor(path string) *sync.Mutex {
	banditLocks.mu.Lock()
	defer banditLocks.mu.Unlock()
	if l, ok := banditLocks.m[path]; ok {
		return l
	}
	l := &sync.Mutex{}
	banditLocks.m[path] = l
	return l
}

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
//
// Concurrency: every load-modify-save cycle holds this node's path-scoped
// lock from banditLockFor — see that registry's doc comment for exactly what
// is (same-process races) and isn't (cross-process races) covered.
//
// Caching: stats are loaded once and cached in this closure; subsequent
// ticks of the *same built command* reuse the cache instead of re-reading
// the file (mutation of the cache is guarded by the same lock). This means
// writes another closure makes to the same stats path after this one's
// first load are not observed for the rest of this closure's lifetime —
// acceptable, since these stats are advisory ordering hints, not ground
// truth that selection correctness depends on.
//
// Resuming a RUNNING child: when enabled, a child that returns RUNNING is
// pinned via a ChainState cursor ("bandit/<name>/running") and is resumed
// first on the next tick regardless of what a fresh UCB1 recompute would
// rank first — otherwise a re-tick could reorder arms out from under an
// in-flight child and abandon it. The cursor is cleared once that child
// reaches a terminal result; on failure, selection continues from the
// remaining order in the same tick.
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

	lock := banditLockFor(banditStatsPath(node.Name))
	var cached *banditStats // guarded by lock; loaded once, then reused (Finding 2)
	runningKey := "bandit/" + node.Name + "/running"

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		lock.Lock()
		defer lock.Unlock()

		if cached == nil {
			cached = loadBanditStats(node.Name)
		}
		stats := cached
		dirty := false

		// Resume a pinned RUNNING child first (Finding 3), bypassing
		// whatever the current recompute would otherwise rank first.
		resumed := -1
		if idx, ok := chainStateInt(ctx.Blackboard, runningKey); ok && idx >= 0 && idx < len(children) {
			resumed = idx
			switch code := children[idx].Run(ctx); {
			case code == 0:
				ctx.Blackboard.ChainState[runningKey] = idx
				return 0
			case code > 0:
				recordBanditOutcome(stats, keys[idx], true, window)
				delete(ctx.Blackboard.ChainState, runningKey)
				saveBanditStats(node.Name, stats)
				return 1
			default:
				recordBanditOutcome(stats, keys[idx], false, window)
				delete(ctx.Blackboard.ChainState, runningKey)
				dirty = true
				// fall through: continue selection from the remaining order
			}
		}

		order := make([]int, len(children))
		for i := range order {
			order[i] = i
		}
		if enabled {
			order = banditUCB1Order(keys, stats)
		}

		result := -1
		for _, idx := range order {
			if idx == resumed {
				continue // already tried (and failed) earlier this tick
			}
			code := children[idx].Run(ctx)
			if code == 0 {
				ctx.Blackboard.ChainState[runningKey] = idx
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
