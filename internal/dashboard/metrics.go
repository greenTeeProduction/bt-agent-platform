// Package dashboard provides live metrics collection and SSE streaming for the BT Dashboard.
package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Metrics holds a snapshot of live dashboard data.
type Metrics struct {
	Timestamp int64            `json:"timestamp"`
	System    SystemMetrics    `json:"system"`
	Trees     TreeMetrics      `json:"trees"`
	Gardener  *GardenerMetrics `json:"gardener,omitempty"`
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
	Cycles       int     `json:"cycles"`
	Trees        int     `json:"trees"`
	Improvements int     `json:"improvements"`
	BestFitness  float64 `json:"best_fitness"`
	LastRun      string  `json:"last_run"`
}

var (
	mu       sync.RWMutex
	lastSnap *Metrics
	snapTime time.Time
)

// Collect gathers live system and platform metrics.
func Collect(treeCount int, categories map[string]int) Metrics {
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
	}

	// System health via shell commands
	m.System = collectSystem()

	// Gardener metrics from file
	m.Gardener = loadGardenerMetrics()

	mu.Lock()
	lastSnap = &m
	snapTime = time.Now()
	mu.Unlock()

	return m
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
			for _, line := range strings.Split(string(data), "\n") {
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
		used := total - avail
		if used < 0 {
			used = 0
		}
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
			s.Processes = 0
		}
	}

	// Uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			secs, _ := strconv.ParseFloat(parts[0], 64)
			hours := int(secs) / 3600
			days := hours / 24
			hours = hours % 24
			s.Uptime = fmt.Sprintf("%dd %dh", days, hours)
		}
	}

	return s
}

func loadGardenerMetrics() *GardenerMetrics {
	path := os.Getenv("HOME") + "/.go-bt-gardener/gardener-metrics.json"
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Cycles      int     `json:"total_cycles"`
		Trees       int     `json:"active_trees"`
		BestFitness float64 `json:"best_fitness"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if raw.Cycles == 0 {
		return nil
	}
	return &GardenerMetrics{
		Cycles:       raw.Cycles,
		Trees:        raw.Trees,
		Improvements: 0, // computed later
		BestFitness:  raw.BestFitness,
		LastRun:      "recent",
	}
}

// ToJSON serializes metrics to JSON bytes.
func (m *Metrics) ToJSON() []byte {
	b, _ := json.Marshal(m)
	return b
}
