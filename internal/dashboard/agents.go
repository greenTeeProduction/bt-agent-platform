package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// AgentInfo holds displayed agent data.
type AgentInfo struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Tree        string  `json:"tree"`
	Schedule    string  `json:"schedule"`
	TotalRuns   int     `json:"total_runs"`
	SuccessRate float64 `json:"success_rate"`
	AvgQuality  float64 `json:"avg_quality"`
	LastRun     string  `json:"last_run"`
	LastOutcome string  `json:"last_outcome"`
}

// AgentRunRecord is a single run from the JSONL history.
type AgentRunRecord struct {
	RunID     string  `json:"id"`
	AgentName string  `json:"agent_name"`
	Task      string  `json:"task"`
	Outcome   string  `json:"outcome"`
	Duration  string  `json:"duration"`
	Quality   float64 `json:"quality"`
	StartedAt string  `json:"started_at"`
}

// AgentYAMLConfig is the structure of an agent YAML file.
type AgentYAMLConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Tree        string `yaml:"tree"`
	Schedule    string `yaml:"schedule"`
}

// ListAgents reads agent configs and history to build agent info.
func ListAgents() []AgentInfo {
	home := os.Getenv("HOME")
	agentsDir := filepath.Join(home, ".go-bt-evolve", "agents")
	historyDir := filepath.Join(home, ".go-bt-evolve", "history")

	var agents []AgentInfo

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return agents
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(agentsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg AgentYAMLConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if cfg.Name != "" {
			name = cfg.Name
		}

		agent := AgentInfo{
			Name:        name,
			Description: cfg.Description,
			Tree:        cfg.Tree,
			Schedule:    cfg.Schedule,
		}

		// Parse history JSONL
		histPath := filepath.Join(historyDir, strings.TrimSuffix(entry.Name(), ".yaml")+".jsonl")
		if histData, err := os.ReadFile(histPath); err == nil {
			lines := strings.Split(strings.TrimSpace(string(histData)), "\n")
			var totalQuality float64
			successes := 0
			for _, line := range lines {
				var rec AgentRunRecord
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					continue
				}
				agent.TotalRuns++
				totalQuality += rec.Quality
				if rec.Outcome == "success" {
					successes++
				}
				// Last record wins
				agent.LastRun = rec.StartedAt
				agent.LastOutcome = rec.Outcome
			}
			if agent.TotalRuns > 0 {
				agent.SuccessRate = float64(successes) / float64(agent.TotalRuns)
				agent.AvgQuality = totalQuality / float64(agent.TotalRuns)
			}
		}

		// Format last run time
		if agent.LastRun != "" {
			if t, err := time.Parse(time.RFC3339Nano, agent.LastRun); err == nil {
				agent.LastRun = t.Format("2006-01-02 15:04")
			}
		}

		agents = append(agents, agent)
	}
	return agents
}
