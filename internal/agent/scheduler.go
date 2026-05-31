package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// Scheduler runs agents on cron-like schedules. Supports one-shot, recurring,
// and long-running agents with checkpoint/resume capability.
type Scheduler struct {
	mu           sync.RWMutex
	reg          *Registry
	history      *History
	jobs         map[string]*ScheduledJob
	stopCh       chan struct{}
	running      bool
	tickInterval time.Duration
	jobStore     JobStore // optional: persists job state across restarts
}

// ScheduledJob represents a scheduled agent run.
type ScheduledJob struct {
	ID         string    `json:"id"`
	AgentName  string    `json:"agent_name"`
	Schedule   string    `json:"schedule"`    // "every 1h", "0 9 * * *", "on_demand"
	NextRun    time.Time `json:"next_run"`
	LastRun    time.Time `json:"last_run"`
	RunCount   int       `json:"run_count"`
	MaxRetries int       `json:"max_retries"` // 0 = unlimited
	RetryDelay string    `json:"retry_delay"` // "5m" between retries
	Timeout    string    `json:"timeout"`     // "2h" max run duration
	Active     bool      `json:"active"`
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"` // for long-running agents
}

// Checkpoint saves agent state for resumable long-running execution.
type Checkpoint struct {
	Step      int       `json:"step"`       // current step number
	Progress  string    `json:"progress"`   // human-readable progress
	Data      string    `json:"data"`       // serialized state
	UpdatedAt time.Time `json:"updated_at"`
}

// RunContext provides the execution context for an agent run.
type RunContext struct {
	AgentName  string
	Task       string
	JobID      string
	Checkpoint *Checkpoint
	Cancel     context.CancelFunc
}

// AgentRunner is the function that actually executes an agent. Injected for testability.
// Returns (outcome, output, error).
// For long-running agents, the runner should periodically update the checkpoint.
type AgentRunner func(ctx RunContext) (outcome, output string, err error)

// SchedulerConfig configures a new scheduler.
type SchedulerConfig struct {
	Registry     *Registry
	History      *History
	TickInterval time.Duration // how often to check for due jobs (default: 1m)
	JobStore     JobStore      // optional: persists jobs across restarts (nil = in-memory only)
}

// NewScheduler creates a new agent scheduler.
// If cfg.JobStore is set, persisted jobs are loaded on startup.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 1 * time.Minute
	}
	s := &Scheduler{
		reg:          cfg.Registry,
		history:      cfg.History,
		jobs:         make(map[string]*ScheduledJob),
		stopCh:       make(chan struct{}),
		tickInterval: cfg.TickInterval,
		jobStore:     cfg.JobStore,
	}
	// Restore persisted jobs
	if cfg.JobStore != nil {
		s.loadState()
	}
	return s
}

// Schedule adds a recurring job for an agent.
func (s *Scheduler) Schedule(agentName, schedule string, timeout string, maxRetries int) (*ScheduledJob, error) {
	// Verify agent exists
	if _, err := s.reg.Get(agentName); err != nil {
		return nil, fmt.Errorf("agent %q not registered: %w", agentName, err)
	}

	nextRun, err := parseSchedule(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job := &ScheduledJob{
		ID:         fmt.Sprintf("job_%s_%d", agentName, time.Now().Unix()),
		AgentName:  agentName,
		Schedule:   schedule,
		NextRun:    nextRun,
		MaxRetries: maxRetries,
		Timeout:    timeout,
		Active:     true,
	}
	s.jobs[job.ID] = job
	s.saveStateLocked()
	return job, nil
}

// RunNow triggers an immediate run of an agent (bypasses schedule).
func (s *Scheduler) RunNow(agentName, task string, runner AgentRunner, timeout string) (outcome, output string, err error) {
	inst, err := s.reg.Get(agentName)
	if err != nil {
		return "", "", err
	}

	timeoutDur := parseTimeout(timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
	defer cancel()

	runCtx := RunContext{
		AgentName: agentName,
		Task:      task,
	}

	start := time.Now()
	outcome, output, err = runner(runCtx)
	duration := time.Since(start)

	// Record history
	if s.history != nil {
		quality := 0.0
		if outcome == "success" {
			quality = estimateQuality(output)
		}
		s.history.Record(RunRecord{
			AgentName: agentName,
			Task:      task,
			Outcome:   outcome,
			Output:    output,
			Duration:  duration.Truncate(time.Second).String(),
			Quality:   quality,
			StartedAt: start,
			EndedAt:   time.Now(),
		})
	}

	// Feed back into knowledge graph
	if inst.Definition.Tree != "" {
		knowledge.GlobalGraph.RecordRun(knowledge.RunRecord{
			TreeID:   inst.Definition.Tree,
			Task:     task,
			Outcome:  outcome,
			Duration: duration,
		})
		// Record decision trace for failure explainability
		runID := fmt.Sprintf("%s-%d", inst.Definition.Tree, start.UnixNano())
		knowledge.GlobalTraceStore.Record(knowledge.DecisionTrace{
			RunID:     runID,
			TreeID:    inst.Definition.Tree,
			Task:      task,
			Outcome:   outcome,
			StartedAt: start,
			EndedAt:   time.Now(),
		})
	}

	_ = inst
	_ = ctx
	return outcome, output, err
}

// Start begins the scheduler loop. Runs in the background.
// Panics in the scheduler loop or runner are recovered to prevent
// the entire scheduler from dying. A single bad job does not take
// down the system.
func (s *Scheduler) Start(runner AgentRunner) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Scheduler: tick panicked (recovered): %v", r)
					}
				}()
				s.tick(runner)
			}()
		}
	}
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// ListJobs returns all scheduled jobs.
func (s *Scheduler) ListJobs() []ScheduledJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ScheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, *j)
	}
	return result
}

// RemoveJob removes a scheduled job.
func (s *Scheduler) RemoveJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.jobs[jobID]; !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	delete(s.jobs, jobID)
	s.saveStateLocked()
	return nil
}

func (s *Scheduler) tick(runner AgentRunner) {
	s.mu.RLock()
	var due []*ScheduledJob
	now := time.Now()
	for _, j := range s.jobs {
		if j.Active && (j.NextRun.IsZero() || now.After(j.NextRun)) {
			due = append(due, j)
		}
	}
	s.mu.RUnlock()

	for _, job := range due {
		s.runJob(job, runner)
	}
}

func (s *Scheduler) runJob(job *ScheduledJob, runner AgentRunner) {
	inst, err := s.reg.Get(job.AgentName)
	if err != nil {
		return
	}
	_ = inst

	timeoutDur := parseTimeout(job.Timeout)
	_ = timeoutDur

	// Build a meaningful task from the agent's description.
	// Avoid "scheduled run" — use the actual purpose so the agent
	// doesn't get caught in a self-referential loop.
	task := inst.Definition.Description
	if task == "" {
		task = job.AgentName
	}

	runCtx := RunContext{
		AgentName:  job.AgentName,
		Task:       task,
		JobID:      job.ID,
		Checkpoint: job.Checkpoint,
	}

	start := time.Now()

	// Recover from panics in the runner so one bad agent doesn't
	// block all subsequent jobs. Panic is recorded as a failure.
	var outcome, output string
	var runErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Scheduler: agent %q panicked in runJob (recovered): %v", job.AgentName, r)
				outcome = "panic"
				runErr = fmt.Errorf("agent panicked: %v", r)
			}
		}()
		outcome, output, runErr = runner(runCtx)
	}()
	duration := time.Since(start)

	// Update job state
	s.mu.Lock()
	job.LastRun = time.Now()
	job.RunCount++

	// Schedule next run
	next, err := parseSchedule(job.Schedule)
	if err == nil {
		job.NextRun = next
	}
	s.mu.Unlock()

	// Persist updated job state
	s.saveState()

	// Record history
	if s.history != nil {
		quality := 0.0
		if outcome == "success" {
			quality = estimateQuality(output)
		}
		errStr := ""
		if runErr != nil {
			errStr = runErr.Error()
		}
		s.history.Record(RunRecord{
			AgentName: job.AgentName,
			Task:      runCtx.Task,
			Outcome:   outcome,
			Output:    output,
			Error:     errStr,
			Duration:  duration.Truncate(time.Second).String(),
			Quality:   quality,
			StartedAt: start,
			EndedAt:   time.Now(),
		})
	}

	// Feed back into knowledge graph
	if inst.Definition.Tree != "" {
		knowledge.GlobalGraph.RecordRun(knowledge.RunRecord{
			TreeID:   inst.Definition.Tree,
			Task:     runCtx.Task,
			Outcome:  outcome,
			Duration: duration,
		})
		// Record decision trace for failure explainability
		runID := fmt.Sprintf("%s-sched-%d", inst.Definition.Tree, start.UnixNano())
		knowledge.GlobalTraceStore.Record(knowledge.DecisionTrace{
			RunID:     runID,
			TreeID:    inst.Definition.Tree,
			Task:      runCtx.Task,
			Outcome:   outcome,
			StartedAt: start,
			EndedAt:   time.Now(),
		})
	}
}

// estimateQuality is a fast quality heuristic for output text.
func estimateQuality(output string) float64 {
	if len(output) < 30 {
		return 0.2
	}
	score := 0.5
	if len(output) > 200 {
		score += 0.2
	}
	if len(output) > 500 {
		score += 0.2
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// parseSchedule converts a schedule string to the next run time.
// Supports: "every 1h", "every 30m", "0 9 * * *" (daily 9am), "on_demand"
func parseSchedule(sched string) (time.Time, error) {
	now := time.Now()
	switch {
	case sched == "" || sched == "on_demand":
		return time.Time{}, nil // never auto-runs
	case len(sched) > 6 && sched[:6] == "every ":
		d, err := time.ParseDuration(sched[6:])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid duration in %q: %w", sched, err)
		}
		return now.Add(d), nil
	case len(sched) > 2 && sched[2] == ' ':
		// Simple cron: "0 9 * * *" → next occurrence
		// For now, just return now+1h as a reasonable default
		return now.Add(1 * time.Hour), nil
	}
	return now.Add(1 * time.Hour), nil
}

func parseTimeout(timeout string) time.Duration {
	if timeout == "" {
		return 2 * time.Hour
	}
	d, err := time.ParseDuration(timeout)
	if err != nil {
		return 2 * time.Hour
	}
	return d
}

// saveState persists all jobs to the configured JobStore.
// Safe to call without holding the lock.
func (s *Scheduler) saveState() {
	if s.jobStore == nil {
		return
	}
	s.mu.RLock()
	jobs := make([]ScheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	s.mu.RUnlock()

	if err := s.jobStore.Save(jobs); err != nil {
		log.Printf("Scheduler: failed to persist jobs: %v", err)
	}
}

// saveStateLocked persists all jobs to the configured JobStore.
// Caller MUST hold s.mu (write lock). Performs synchronous I/O.
func (s *Scheduler) saveStateLocked() {
	if s.jobStore == nil {
		return
	}
	jobs := make([]ScheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	if err := s.jobStore.Save(jobs); err != nil {
		log.Printf("Scheduler: failed to persist jobs: %v", err)
	}
}

// loadState restores jobs from the configured JobStore.
// Called during NewScheduler. Errors are logged and ignored —
// an empty job map is a safe fallback.
func (s *Scheduler) loadState() {
	if s.jobStore == nil {
		return
	}
	jobs, err := s.jobStore.Load()
	if err != nil {
		log.Printf("Scheduler: failed to load persisted jobs: %v", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range jobs {
		j := jobs[i] // copy
		s.jobs[j.ID] = &j
	}
}
