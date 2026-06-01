package agent

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
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
	ID         string      `json:"id"`
	AgentName  string      `json:"agent_name"`
	Schedule   string      `json:"schedule"` // "every 1h", "0 9 * * *", "on_demand"
	NextRun    time.Time   `json:"next_run"`
	LastRun    time.Time   `json:"last_run"`
	RunCount   int         `json:"run_count"`
	MaxRetries int         `json:"max_retries"` // 0 = unlimited
	RetryDelay string      `json:"retry_delay"` // "5m" between retries
	Timeout    string      `json:"timeout"`     // "2h" max run duration
	Active     bool        `json:"active"`
	InFlight   bool        `json:"in_flight"`            // true when currently executing (crash recovery)
	Checkpoint *Checkpoint `json:"checkpoint,omitempty"` // for long-running agents
}

// Checkpoint saves agent state for resumable long-running execution.
type Checkpoint struct {
	Step      int       `json:"step"`     // current step number
	Progress  string    `json:"progress"` // human-readable progress
	Data      string    `json:"data"`     // serialized state
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
		// Dedup: remove duplicate active jobs for the same agent (keeps most recent)
		s.dedupJobsLocked()
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

	// Dedup: if an active job for this agent already exists, update its schedule
	// instead of creating a duplicate. This prevents duplicate jobs from accumulating
	// across bt-agent restarts (where Restore loads persisted jobs, then auto-schedule
	// creates new ones for the same agents).
	for _, existing := range s.jobs {
		if existing.AgentName == agentName && existing.Active {
			existing.Schedule = schedule
			existing.NextRun = nextRun
			existing.Timeout = timeout
			existing.MaxRetries = maxRetries
			s.saveStateLocked()
			return existing, nil
		}
	}

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

// dedupJobsLocked removes duplicate active jobs for the same agent,
// keeping only the most recent one (highest run_count). Must be called
// with s.mu held. Used at startup to clean up accumulated duplicates
// from prior bt-agent restarts.
func (s *Scheduler) dedupJobsLocked() {
	seen := make(map[string]*ScheduledJob) // agentName -> best job
	for _, job := range s.jobs {
		if !job.Active {
			continue
		}
		existing, ok := seen[job.AgentName]
		if !ok || job.RunCount > existing.RunCount {
			seen[job.AgentName] = job
		}
	}
	// Delete all active jobs that aren't the best one for their agent
	for id, job := range s.jobs {
		if !job.Active {
			continue
		}
		best := seen[job.AgentName]
		if best != nil && id != best.ID {
			delete(s.jobs, id)
		}
	}
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

	// Mark in-flight and persist IMMEDIATELY for crash recovery.
	// If bt-agent crashes after this point, loadState() will detect
	// the in-flight job on restart and handle it gracefully.
	s.mu.Lock()
	job.InFlight = true
	s.mu.Unlock()
	s.saveState()

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

	// Clear in-flight flag now that execution has completed (or panicked).
	// Update job state
	s.mu.Lock()
	job.InFlight = false
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

	// Publish event to AgentBus (→ Hermes webhook bridge)
	if GlobalAgentBus != nil {
		tree := ""
		if inst != nil {
			tree = inst.Definition.Tree
		}
		eventType := "task_complete"
		if outcome == "panic" || outcome == "error" {
			eventType = "error_detected"
		}
		GlobalAgentBus.Publish(AgentEvent{
			Type:    eventType,
			Source:  job.AgentName,
			Message: fmt.Sprintf("%s: %s (%s)", job.AgentName, outcome, duration.Truncate(time.Second)),
			Data: map[string]interface{}{
				"tree":     tree,
				"task":     runCtx.Task,
				"outcome":  outcome,
				"duration": duration.Truncate(time.Second).String(),
			},
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
	case strings.Count(sched, " ") >= 4:
		// 5-field cron: "0 9 * * *", "15,37 * * * *", "8-59/15 * * * *"
		next, err := nextCronTime(sched, now)
		if err != nil {
			// Fall back to 1h if we can't parse — better than crashing
			log.Printf("Scheduler: cron parse error for %q: %v — falling back to +1h", sched, err)
			return now.Add(1 * time.Hour), nil
		}
		return next, nil
	}
	return now.Add(1 * time.Hour), nil
}

// matches calls a cron field matcher, handling nil gracefully.
func matches(fn func(int) bool, v int) bool {
	if fn == nil {
		return true
	}
	return fn(v)
}

// nextCronTime computes the next fire time for a 5-field cron expression.
// Fields: minute hour day-of-month month day-of-week
// Supports: *, N, N,M, */N, N-M, N-M/N
func nextCronTime(expr string, from time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("expected 5 cron fields, got %d in %q", len(fields), expr)
	}

	// Parse each field
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("minute field %q: %w", fields[0], err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("hour field %q: %w", fields[1], err)
	}
	dom, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("day-of-month field %q: %w", fields[2], err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("month field %q: %w", fields[3], err)
	}
	dow, err := parseCronField(fields[4], 0, 7) // 0 and 7 both mean Sunday
	if err != nil {
		return time.Time{}, fmt.Errorf("day-of-week field %q: %w", fields[4], err)
	}

	// Search forward from the current minute, up to 2 years ahead
	candidate := time.Date(from.Year(), from.Month(), from.Day(), from.Hour(), from.Minute(), 0, 0, from.Location())
	// Start from next minute so we don't re-trigger the current one
	candidate = candidate.Add(1 * time.Minute)
	deadline := from.AddDate(2, 0, 0)

	for candidate.Before(deadline) {
		if matches(minute, candidate.Minute()) &&
			matches(hour, candidate.Hour()) &&
			matches(dom, candidate.Day()) &&
			matches(month, int(candidate.Month())) &&
			matches(dow, int(candidate.Weekday())) {
			return candidate, nil
		}
		candidate = candidate.Add(1 * time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching time found for cron %q within 2 years", expr)
}

// parseCronField parses a single cron field into a matching function.
// Handles: * (all), N (specific), N,M (list), */N (step), N-M (range), N-M/N (ranged step)
func parseCronField(field string, min, max int) (func(int) bool, error) {
	if field == "*" {
		return func(v int) bool { return v >= min && v <= max }, nil
	}

	// Check for step pattern: */N, N-M/N
	if strings.Contains(field, "/") {
		parts := strings.SplitN(field, "/", 2)
		step, err := strconv.Atoi(parts[1])
		if err != nil || step < 1 {
			return nil, fmt.Errorf("invalid step in %q: %w", field, err)
		}
		if parts[0] == "*" {
			// */N: every Nth value
			return func(v int) bool { return v%step == 0 }, nil
		}
		// N-M/N: every Nth within range
		rangeParts := strings.SplitN(parts[0], "-", 2)
		start, err := strconv.Atoi(rangeParts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid range start in %q: %w", field, err)
		}
		end, err := strconv.Atoi(rangeParts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid range end in %q: %w", field, err)
		}
		return func(v int) bool {
			return v >= start && v <= end && (v-start)%step == 0
		}, nil
	}

	// Check for range: N-M
	if strings.Contains(field, "-") {
		parts := strings.SplitN(field, "-", 2)
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid range start in %q: %w", field, err)
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid range end in %q: %w", field, err)
		}
		return func(v int) bool { return v >= start && v <= end }, nil
	}

	// Check for list: N,M,O
	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		values := make(map[int]bool)
		for _, p := range parts {
			v, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return nil, fmt.Errorf("invalid list value %q in %q: %w", p, field, err)
			}
			values[v] = true
		}
		return func(v int) bool { return values[v] }, nil
	}

	// Single value
	v, err := strconv.Atoi(field)
	if err != nil {
		return nil, fmt.Errorf("invalid cron field %q: %w", field, err)
	}
	return func(v2 int) bool { return v2 == v }, nil
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
// Detects jobs that were in-flight when bt-agent crashed and
// marks them as "crashed" so they can be retried on startup.
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

	crashedCount := 0
	for i := range jobs {
		j := jobs[i] // copy
		if j.InFlight {
			// This job was running when bt-agent crashed.
			// Clear in-flight flag, reset NextRun to "now" so it
			// retries immediately on the next tick.
			log.Printf("Scheduler: recovered crashed job %q (agent=%s, run_count=%d)",
				j.ID, j.AgentName, j.RunCount)
			j.InFlight = false
			j.NextRun = time.Time{} // run immediately on next tick
			crashedCount++
		}
		s.jobs[j.ID] = &j
	}
	if crashedCount > 0 {
		log.Printf("Scheduler: recovered %d in-flight job(s) from crash", crashedCount)
	}
}
