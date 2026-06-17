package agent

import (
	"os"
	"path/filepath"
)

// HomeDir returns the BT agent platform data root (~/.go-bt-evolve by default).
// Override with BT_AGENT_HOME (preferred) or BT_HOME (legacy).
func HomeDir() string {
	if v := os.Getenv("BT_AGENT_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("BT_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("BT_AGENT_DEFS_DIR"); v != "" {
		return filepath.Dir(v)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".go-bt-evolve")
}

func RegistryDir() string   { return filepath.Join(HomeDir(), "agents") }
func TemplatesDir() string  { return filepath.Join(HomeDir(), "agents", "templates") }
func WorkflowsDir() string  { return filepath.Join(HomeDir(), "agents", "workflows") }
func MemoryDir() string     { return filepath.Join(HomeDir(), "memory") }
func BlackboardDir() string { return filepath.Join(HomeDir(), "blackboard") }
func HistoryDir() string    { return filepath.Join(HomeDir(), "history") }
func JobsDir() string       { return filepath.Join(HomeDir(), "jobs") }
func DLQFile() string       { return filepath.Join(HomeDir(), "dead_letter_queue.json") }
func SchedulerJobsFile() string {
	return filepath.Join(JobsDir(), "scheduler-jobs.json")
}
func CircuitBreakersFile() string {
	return filepath.Join(HomeDir(), "circuit_breakers.json")
}
func LogsDir() string { return filepath.Join(HomeDir(), "logs") }
