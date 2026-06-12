package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// AgentWithStatus extends AgentInfo with circuit breaker status.
type AgentWithStatus struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Tree        string  `json:"tree"`
	Status      string  `json:"status"`
	Schedule    string  `json:"schedule"`
	SuccessRate float64 `json:"success_rate"`
	TotalRuns   int     `json:"total_runs"`
	AvgQuality  float64 `json:"avg_quality"`
	LastRun     string  `json:"last_run"`
	LastOutcome string  `json:"last_outcome"`
	CBStatus    string  `json:"cb_status,omitempty"` // circuit breaker: "open", "closed", "half_open", "unknown"
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

// CircuitBreakerEntry holds circuit breaker state for a single agent.
type CircuitBreakerEntry struct {
	Status      string `json:"status"`       // open, closed, half_open
	Failures    int    `json:"failures"`     // consecutive failure count
	LastFailure string `json:"last_failure"` // ISO 8601
}

// CircuitBreakers is the circuit breaker state file.
type CircuitBreakers struct {
	Breakers map[string]CircuitBreakerEntry `json:"breakers"`
}

// ListAgents reads agent definitions from YAML templates and combines with scheduler state.
func ListAgents() []AgentInfo {
	// Try current directory first for local development, fallback to home directory
	templatesDir := filepath.Join("agents", "templates")
	if _, err := os.Stat(templatesDir); err != nil {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = os.Getenv("HOME")
		}
		templatesDir = filepath.Join(home, "go-bt-evolve", "agents", "templates")
	}

	schedulerPath := filepath.Join(".go-bt-evolve", "jobs", "scheduler-jobs.json")
	if _, err := os.Stat(schedulerPath); err != nil {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = os.Getenv("HOME")
		}
		schedulerPath = filepath.Join(home, ".go-bt-evolve", "jobs", "scheduler-jobs.json")
	}

	// Load scheduler state for live stats
	sched := loadScheduler(schedulerPath)

	// Read agent YAML definitions
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil
	}

	agents := make([]AgentInfo, 0, 16)
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

// ListAgentsWithCB reads agent definitions and includes circuit breaker status.
func ListAgentsWithCB() []AgentWithStatus {
	home := os.Getenv("HOME")
	templatesDir := filepath.Join(home, "go-bt-evolve", "agents", "templates")
	schedulerPath := filepath.Join(home, ".go-bt-evolve", "jobs", "scheduler-jobs.json")
	cbPath := filepath.Join(home, ".go-bt-evolve", "circuit_breakers.json")

	// Load scheduler state for live stats
	sched := loadScheduler(schedulerPath)

	// Load circuit breaker state
	cbMap := loadCircuitBreakers(cbPath)

	// Read agent YAML definitions
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil
	}

	var agents []AgentWithStatus
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

		info := AgentWithStatus{
			Name:        cfg.Name,
			Description: cfg.Description,
			Tree:        cfg.Tree,
			Status:      "created",
			Schedule:    cfg.Schedule,
			CBStatus:    "unknown",
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

		// Merge circuit breaker status
		if cb, ok := cbMap[cfg.Name]; ok {
			info.CBStatus = cb.Status
		}

		agents = append(agents, info)
	}

	// Sort alphabetically
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Name < agents[j].Name
	})

	return agents
}

// CreateAgent writes a new agent YAML template to disk.
func CreateAgent(info AgentYAMLConfig) error {
	if info.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if info.Tree == "" {
		return fmt.Errorf("tree is required")
	}

	home := os.Getenv("HOME")
	templatesDir := filepath.Join(home, "go-bt-evolve", "agents", "templates")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		return fmt.Errorf("failed to create templates directory: %w", err)
	}

	// Set defaults
	if info.Schedule == "" {
		info.Schedule = "on_demand"
	}

	// Marshal to YAML
	data, err := yaml.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal agent YAML: %w", err)
	}

	// Write the file
	filePath := filepath.Join(templatesDir, info.Name+".yaml")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent file: %w", err)
	}

	return nil
}

// DeleteAgent removes an agent YAML template from disk.
func DeleteAgent(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}

	home := os.Getenv("HOME")
	filePath := filepath.Join(home, "go-bt-evolve", "agents", "templates", name+".yaml")

	// Also try .yml extension
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		filePath = filepath.Join(home, "go-bt-evolve", "agents", "templates", name+".yml")
	}

	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent %q not found", name)
		}
		return fmt.Errorf("failed to delete agent %q: %w", name, err)
	}

	return nil
}

func loadScheduler(path string) []ScheduledJob {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Try wrapped {jobs: [...]} format first
	var store SchedulerJobs
	if err := json.Unmarshal(data, &store); err == nil {
		return store.Jobs
	}
	// Fall back to bare array format [...]
	var jobs []ScheduledJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil
	}
	return jobs
}

func loadCircuitBreakers(path string) map[string]CircuitBreakerEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	// Try wrapped {breakers: {...}} format
	var store CircuitBreakers
	if err := json.Unmarshal(data, &store); err == nil {
		return store.Breakers
	}
	// Fall back to flat map format {"agent_name": {"status": "open", ...}}
	var flat map[string]CircuitBreakerEntry
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil
	}
	return flat
}
