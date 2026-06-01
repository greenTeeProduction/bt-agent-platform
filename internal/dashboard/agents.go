package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentInfo holds the dashboard-facing agent summary.
type AgentInfo struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Tree        string  `json:"tree"`
	Status      string  `json:"status"`       // running, scheduled, created, error
	Schedule    string  `json:"schedule"`     // cron expression or "on_demand"
	SuccessRate float64 `json:"success_rate"` // 0.0-1.0
	TotalRuns   int     `json:"total_runs"`
	AvgQuality  float64 `json:"avg_quality"`
	LastRun     string  `json:"last_run"`     // ISO 8601
	LastOutcome string  `json:"last_outcome"` // success, failure, timeout
}

// AgentYAMLConfig mirrors the agent YAML template format.
type AgentYAMLConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Tree        string `yaml:"tree"`
	Schedule    string `yaml:"schedule"`
}

// SchedulerJobs is the scheduler state file.
type SchedulerJobs struct {
	Jobs []ScheduledJob `json:"jobs"`
}

// ScheduledJob mirrors the bt-agent scheduler job entry.
type ScheduledJob struct {
	AgentName   string  `json:"agent_name"`
	Status      string  `json:"status"`
	SuccessRate float64 `json:"success_rate"`
	TotalRuns   int     `json:"total_runs"`
	AvgQuality  float64 `json:"avg_quality"`
	LastRun     string  `json:"last_run"`
	LastOutcome string  `json:"last_outcome"`
}

// AgentHistoryEntry mirrors a single run record.
type AgentHistoryEntry struct {
	Outcome   string  `json:"outcome"`
	Quality   float64 `json:"quality"`
	StartedAt string  `json:"started_at"`
}

// ListAgents reads agent definitions from YAML templates and combines with scheduler state.
func ListAgents() []AgentInfo {
	home := os.Getenv("HOME")
	templatesDir := filepath.Join(home, "go-bt-evolve", "agents", "templates")
	schedulerPath := filepath.Join(home, ".go-bt-evolve", "jobs", "scheduler-jobs.json")

	// Load scheduler state for live stats
	sched := loadScheduler(schedulerPath)

	// Read agent YAML definitions
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil
	}

	var agents []AgentInfo
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(templatesDir, entry.Name()))
		if err != nil {
			continue
		}
		var cfg AgentYAMLConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if cfg.Name == "" {
			cfg.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}

		info := AgentInfo{
			Name:        cfg.Name,
			Description: cfg.Description,
			Tree:        cfg.Tree,
			Status:      "created",
			Schedule:    cfg.Schedule,
		}

		// Merge scheduler state
		for _, job := range sched {
			if job.AgentName == cfg.Name {
				info.SuccessRate = job.SuccessRate
				info.TotalRuns = job.TotalRuns
				info.AvgQuality = job.AvgQuality
				info.LastRun = job.LastRun
				info.LastOutcome = job.LastOutcome
				info.Status = job.Status
				if info.Status == "" {
					info.Status = "scheduled"
				}
			}
		}

		agents = append(agents, info)
	}

	// Sort alphabetically
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return agents
}

func loadScheduler(path string) []ScheduledJob {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var store SchedulerJobs
	if err := json.Unmarshal(data, &store); err != nil {
		return nil
	}
	return store.Jobs
}

// GetAgentHistory returns the last N run records for an agent.
func GetAgentHistory(agentName string, limit int) []AgentHistoryEntry {
	home := os.Getenv("HOME")
	historyDir := filepath.Join(home, ".go-bt-evolve", "history")
	pattern := filepath.Join(historyDir, agentName+"*.json")

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	var all []AgentHistoryEntry
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		var entries []AgentHistoryEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			continue
		}
		all = append(all, entries...)
	}

	// Sort by most recent first
	sort.Slice(all, func(i, j int) bool {
		// Parse ISO timestamps for sorting
		ti, _ := time.Parse(time.RFC3339, all[i].StartedAt)
		tj, _ := time.Parse(time.RFC3339, all[j].StartedAt)
		return ti.After(tj)
	})

	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}
