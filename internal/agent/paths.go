package agent

import (
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/util"
)

// HomeDir returns the BT agent platform data root (~/.go-bt-evolve by default).
// Override with BT_AGENT_HOME (preferred) or BT_HOME (legacy).
func HomeDir() string {
	return util.RuntimePlatformHome()
}

func RegistryDir() string   { return util.PlatformAgentDefinitionsDir() }
func TemplatesDir() string  { return filepath.Join(HomeDir(), "agents", "templates") }
func WorkflowsDir() string  { return filepath.Join(HomeDir(), "agents", "workflows") }
func MemoryDir() string     { return filepath.Join(HomeDir(), "memory") }
func BlackboardDir() string { return filepath.Join(HomeDir(), "blackboard") }
func HistoryDir() string    { return util.PlatformHistoryDir() }
func JobsDir() string       { return filepath.Join(HomeDir(), "jobs") }
func TasksFile() string     { return filepath.Join(HomeDir(), "tasks.json") }
func TaskQueueFile() string { return filepath.Join(HomeDir(), "task_queue.json") }
func DLQFile() string       { return filepath.Join(HomeDir(), "dead_letter_queue.json") }
func SchedulerJobsFile() string {
	return filepath.Join(JobsDir(), "scheduler-jobs.json")
}
func CircuitBreakersFile() string {
	return filepath.Join(HomeDir(), "circuit_breakers.json")
}
func FeedbackFile() string { return filepath.Join(HomeDir(), "feedback.json") }
func NotificationThrottleFile() string {
	return filepath.Join(HomeDir(), "notification_throttle.json")
}
func LogsDir() string { return util.PlatformLogDir() }

// SLOMetricsFile is the cross-process SLO evidence file the gardener's
// validation gate falls back to (internal/gardener/validation_gate.go
// EvidencePath) when its own in-memory metrics are empty. Matches the path
// cmd/bt-agent/main.go's daemon builds from platformHome.
func SLOMetricsFile() string { return filepath.Join(HomeDir(), "slo", "slo-metrics.json") }

// UsersDir is the root of per-user personalization workspaces (ADR-133):
// users/<user>/{profile.json, interactions.jsonl, trees, goals, memory,
// reflections, experience}. Layout inside is owned by internal/persona.
func UsersDir() string { return filepath.Join(HomeDir(), "users") }
