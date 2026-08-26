package dashboard

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nico/go-bt-evolve/internal/agent"
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
	AgentInfo
	CBStatus string `json:"cb_status,omitempty"` // circuit breaker: "open", "closed", "half_open", "unknown"
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

// ListAgents reads installed agents from the registry and merges scheduler/history stats.
func ListAgents() []AgentInfo {
	out := make([]AgentInfo, 0, 16)
	for _, a := range listRegistryAgents(false) {
		out = append(out, a.AgentInfo)
	}
	return out
}

// ListAgentsWithCB reads registry agents and includes circuit breaker status.
func ListAgentsWithCB() []AgentWithStatus {
	return listRegistryAgents(true)
}

func listRegistryAgents(withCB bool) []AgentWithStatus {
	reg, err := agent.NewRegistry(agent.RegistryDir())
	if err != nil {
		return nil
	}
	sched := loadScheduler(agent.SchedulerJobsFile())
	cbMap := loadCircuitBreakers(agent.CircuitBreakersFile())
	hist, _ := agent.NewHistory(agent.HistoryDir())

	agents := make([]AgentWithStatus, 0, len(reg.List()))
	for _, inst := range reg.List() {
		def := inst.Definition
		info := AgentWithStatus{
			AgentInfo: AgentInfo{
				Name:        def.Name,
				Description: def.Description,
				Tree:        def.Tree,
				Schedule:    def.Schedule,
				Status:      agentStatus(def, inst),
				SuccessRate: inst.SuccessRate,
				TotalRuns:   inst.RunCount,
			},
			CBStatus: "unknown",
		}
		if withCB {
			if cb, ok := cbMap[def.Name]; ok {
				info.CBStatus = cb.Status
			}
		}

		mergeSchedulerJob(&info.AgentInfo, sched)
		if hist != nil {
			mergeHistoryStats(&info.AgentInfo, hist.Stats(def.Name))
		}
		if !inst.LastRun.IsZero() && info.LastRun == "" {
			info.LastRun = inst.LastRun.Format(time.RFC3339)
		}
		agents = append(agents, info)
	}

	slices.SortFunc(agents, func(a, b AgentWithStatus) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return agents
}

func agentStatus(def agent.Definition, inst *agent.Instance) string {
	if inst.State == agent.StateRunning {
		return "running"
	}
	if inst.State == agent.StateError {
		return "error"
	}
	if def.Schedule != "" && def.Schedule != "on_demand" {
		return "scheduled"
	}
	return "created"
}

func mergeSchedulerJob(info *AgentInfo, sched []ScheduledJob) {
	for _, job := range sched {
		if job.AgentName != info.Name {
			continue
		}
		if job.TotalRuns > info.TotalRuns {
			info.TotalRuns = job.TotalRuns
		}
		if job.SuccessRate > 0 {
			info.SuccessRate = job.SuccessRate
		}
		if job.AvgQuality > 0 {
			info.AvgQuality = job.AvgQuality
		}
		if job.LastRun != "" {
			info.LastRun = job.LastRun
		}
		if job.LastOutcome != "" {
			info.LastOutcome = job.LastOutcome
		}
		if job.Status != "" {
			info.Status = job.Status
		} else if info.Status == "created" {
			info.Status = "scheduled"
		}
	}
}

func mergeHistoryStats(info *AgentInfo, stats agent.RunStats) {
	if stats.TotalRuns == 0 {
		return
	}
	if stats.TotalRuns >= info.TotalRuns {
		info.TotalRuns = stats.TotalRuns
		info.SuccessRate = stats.SuccessRate
		info.AvgQuality = stats.AvgQuality
		if !stats.LastRun.IsZero() {
			info.LastRun = stats.LastRun.Format(time.RFC3339)
		}
		if stats.LastOutcome != "" {
			info.LastOutcome = stats.LastOutcome
		}
	}
}

// DefaultAgentTask returns task text when the caller did not supply one.
// Uses the agent description from registry YAML, then the agent name.
func DefaultAgentTask(agentName string) string {
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return "Execute agent task"
	}
	reg, err := agent.NewRegistry(agent.RegistryDir())
	if err != nil {
		return agentName
	}
	inst, err := reg.Get(agentName)
	if err != nil {
		return agentName
	}
	if desc := strings.TrimSpace(inst.Definition.Description); desc != "" {
		return desc
	}
	return agentName
}

// CreateAgent writes a new agent YAML to the registry.
func CreateAgent(info AgentYAMLConfig) error {
	if info.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if info.Tree == "" {
		return fmt.Errorf("tree is required")
	}

	if err := os.MkdirAll(agent.RegistryDir(), 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	if info.Schedule == "" {
		info.Schedule = "on_demand"
	}

	data, err := yaml.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal agent YAML: %w", err)
	}

	filePath := filepath.Join(agent.RegistryDir(), info.Name+".yaml")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent file: %w", err)
	}

	if info.Schedule != "" && info.Schedule != "on_demand" {
		reg, err := agent.NewRegistry(agent.RegistryDir())
		if err != nil {
			return fmt.Errorf("created agent but failed to open registry: %w", err)
		}
		if _, err := agent.ApplySchedule(reg, info.Name, info.Schedule, "2h", 3); err != nil {
			return fmt.Errorf("created agent but failed to register schedule: %w", err)
		}
	}

	return nil
}

// DeleteAgent removes an agent from the registry and clears its scheduler jobs.
func DeleteAgent(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}

	reg, err := agent.NewRegistry(agent.RegistryDir())
	if err != nil {
		return fmt.Errorf("open registry: %w", err)
	}

	if err := agent.DeleteRegisteredAgent(reg, name); err == nil {
		return nil
	}

	filePath := filepath.Join(agent.RegistryDir(), name+".yaml")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		filePath = filepath.Join(agent.RegistryDir(), name+".yml")
	}
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent %q not found", name)
		}
		return fmt.Errorf("failed to delete agent %q: %w", name, err)
	}
	return agent.RemoveAgentJobs(name)
}

func loadScheduler(path string) []ScheduledJob {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var store SchedulerJobs
	if err := json.Unmarshal(data, &store); err == nil && len(store.Jobs) > 0 {
		return store.Jobs
	}
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
	var store CircuitBreakers
	if err := json.Unmarshal(data, &store); err == nil && store.Breakers != nil {
		return store.Breakers
	}
	var flat map[string]CircuitBreakerEntry
	if err := json.Unmarshal(data, &flat); err != nil {
		return nil
	}
	return flat
}
