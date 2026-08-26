// Package dashboard provides live metrics collection and SSE streaming for the BT Dashboard.
package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics holds a snapshot of live dashboard data.
type Metrics struct {
	Timestamp     int64            `json:"timestamp"`
	System        SystemMetrics    `json:"system"`
	Trees         TreeMetrics      `json:"trees"`
	Gardener      *GardenerMetrics `json:"gardener,omitempty"`
	TopWinners    []TreeSnapshot   `json:"top_winners,omitempty"`
	DLQCategories map[string]int   `json:"dlq_categories,omitempty"`
}

// DLQCategoriesFn, when set, is consulted by Collect to surface a per-error-
// category dead-letter-queue rollup in dashboard metrics, mirroring the
// DiscoverTreeFn / KGAnalyticsRefreshFn package-var injection-hook pattern
// (see executor.go and metrics_utils.go). main.go wires this to
// dlq.CategoryCounts at startup.
var DLQCategoriesFn func() map[string]int

// TreeSnapshot is a lightweight per-tree view of live KnowledgeGraph state
// (structural fitness, evolution lineage, evolved count) used to rank the
// top evolved winners in dashboard metrics.
type TreeSnapshot struct {
	ID                string  `json:"id"`
	StructuralFitness float64 `json:"structural_fitness"`
	EvolvedCount      int     `json:"evolved_count"`
	BaseID            string  `json:"base_id,omitempty"`
}

// SystemMetrics holds system health data.
type SystemMetrics struct {
	DiskRoot  DiskInfo `json:"disk_root"`
	DiskSSD   DiskInfo `json:"disk_ssd"`
	Memory    MemInfo  `json:"memory"`
	Processes int      `json:"processes"`
	Uptime    string   `json:"uptime"`
}

// DiskInfo holds disk usage for a mount point.
type DiskInfo struct {
	MountPoint  string `json:"mount"`
	TotalGB     int    `json:"total_gb"`
	UsedGB      int    `json:"used_gb"`
	PercentUse  int    `json:"percent_use"`
	AvailableGB int    `json:"available_gb"`
	OK          bool   `json:"ok"`
}

// MemInfo holds memory usage.
type MemInfo struct {
	TotalGB     int `json:"total_gb"`
	UsedGB      int `json:"used_gb"`
	AvailableGB int `json:"available_gb"`
	PercentUse  int `json:"percent_use"`
}

// TreeMetrics holds knowledge graph stats.
type TreeMetrics struct {
	Total      int            `json:"total"`
	Categories map[string]int `json:"categories"`
}

// GardenerMetrics holds gardener stats from its metrics file.
type GardenerMetrics struct {
	Cycles              int                `json:"cycles"`
	Trees               int                `json:"trees"`
	Improvements        int                `json:"improvements"`
	CrisisInterventions int                `json:"crisis_interventions"`
	Rollbacks           int                `json:"rollbacks"`
	BestFitness         float64            `json:"best_fitness"`
	LastRun             string             `json:"last_run"`
	SLOs                map[string]float64 `json:"slos,omitempty"`
}

var (
	mu       sync.RWMutex
	lastSnap *Metrics
	snapTime time.Time
)

// Collect gathers live system and platform metrics.
func Collect(treeCount int, categories map[string]int, trees []TreeSnapshot) Metrics {
	mu.RLock()
	if lastSnap != nil && time.Since(snapTime) < 2*time.Second {
		snap := *lastSnap
		mu.RUnlock()
		return snap
	}
	mu.RUnlock()

	m := Metrics{
		Timestamp: time.Now().Unix(),
		Trees: TreeMetrics{
			Total:      treeCount,
			Categories: categories,
		},
		TopWinners: rankTopWinners(trees),
	}

	// System health via shell commands
	m.System = collectSystem()

	// Gardener metrics from file
	m.Gardener = loadGardenerMetrics()

	if DLQCategoriesFn != nil {
		m.DLQCategories = DLQCategoriesFn()
	}

	mu.Lock()
	lastSnap = &m
	snapTime = time.Now()
	mu.Unlock()

	return m
}

// rankTopWinners ranks tree snapshots descending by StructuralFitness so the
// Evolution tab can render live top-evolved-winners instead of the static
// "Algorithms Active" panel.
func rankTopWinners(trees []TreeSnapshot) []TreeSnapshot {
	winners := make([]TreeSnapshot, len(trees))
	copy(winners, trees)
	sort.SliceStable(winners, func(i, j int) bool {
		return winners[i].StructuralFitness > winners[j].StructuralFitness
	})
	return winners
}

func collectSystem() SystemMetrics {
	s := SystemMetrics{}

	// Disk usage via df
	for _, mp := range []string{"/", "/mnt/ssd"} {
		out, err := exec.Command("df", "-BG", mp).Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 2 {
			continue
		}
		fields := strings.Fields(lines[1])
		if len(fields) < 4 {
			continue
		}
		total, _ := strconv.Atoi(strings.TrimSuffix(fields[1], "G"))
		used, _ := strconv.Atoi(strings.TrimSuffix(fields[2], "G"))
		avail, _ := strconv.Atoi(strings.TrimSuffix(fields[3], "G"))
		pct := 0
		if total > 0 {
			pct = (used * 100) / total
		}
		d := DiskInfo{
			MountPoint:  mp,
			TotalGB:     total,
			UsedGB:      used,
			AvailableGB: avail,
			PercentUse:  pct,
			OK:          pct < 90,
		}
		if mp == "/" {
			s.DiskRoot = d
		} else {
			s.DiskSSD = d
		}
	}

	// Memory via /proc/meminfo
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		parseMem := func(key string) int {
			for line := range strings.SplitSeq(string(data), "\n") {
				if strings.HasPrefix(line, key+":") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						v, _ := strconv.Atoi(parts[1])
						return v / 1024 // kB → MB
					}
				}
			}
			return 0
		}
		total := parseMem("MemTotal") / 1024 // MB → GB
		avail := parseMem("MemAvailable") / 1024
		used := max(total-avail, 0)
		pct := 0
		if total > 0 {
			pct = (used * 100) / total
		}
		s.Memory = MemInfo{
			TotalGB:     total,
			UsedGB:      used,
			AvailableGB: avail,
			PercentUse:  pct,
		}
	}

	// Process count (bt-* only)
	out, err := exec.Command("sh", "-c", "ps aux | grep -c '[b]t-'").Output()
	if err == nil {
		s.Processes, err = strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			// retain previous known-good count instead of silently defaulting to 0
			_ = err
		}
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			secs, _ := strconv.ParseFloat(parts[0], 64)
			hours := int(secs) / 3600
			days := hours / 24
			hours %= 24
			s.Uptime = fmt.Sprintf("%dd %dh", days, hours)
		}
	}

	return s
}

func loadGardenerMetrics() *GardenerMetrics {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	metricsPath := filepath.Join(home, ".go-bt-gardener", "gardener-metrics.json")
	sloPath := filepath.Join(home, ".go-bt-gardener", "slo-metrics.json")

	data, err := os.ReadFile(metricsPath)
	if err != nil {
		return nil
	}
	var raw struct {
		Cycles              int     `json:"total_cycles"`
		Trees               int     `json:"active_trees"`
		Improvements        int     `json:"total_improvements"`
		CrisisInterventions int     `json:"total_crisis_interventions"`
		Rollbacks           int     `json:"total_rollbacks"`
		BestFitness         float64 `json:"best_fitness"`
		LastRun             int64   `json:"last_run"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if raw.Cycles == 0 {
		return nil
	}

	// Legacy documents predate the last_run unix timestamp.
	lastRun := "recent"
	if raw.LastRun > 0 {
		lastRun = time.Unix(raw.LastRun, 0).UTC().Format(time.RFC3339)
	}

	gm := &GardenerMetrics{
		Cycles:              raw.Cycles,
		Trees:               raw.Trees,
		Improvements:        raw.Improvements,
		CrisisInterventions: raw.CrisisInterventions,
		Rollbacks:           raw.Rollbacks,
		BestFitness:         raw.BestFitness,
		LastRun:             lastRun,
	}

	// Load SLO metrics if available
	if sloData, err := os.ReadFile(sloPath); err == nil {
		var slos map[string]float64
		if json.Unmarshal(sloData, &slos) == nil {
			gm.SLOs = slos
		}
	}

	return gm
}

// ToJSON serializes metrics to JSON bytes.
func (m *Metrics) ToJSON() []byte {
	b, _ := json.Marshal(m)
	return b
}
