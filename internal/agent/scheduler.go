package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// Scheduler runs agents on cron-like schedules. Supports one-shot, recurring,
// and long-running agents with checkpoint/resume capability.
type Scheduler struct {
	mu            sync.RWMutex
	reg           *Registry
	history       *History
	jobs          map[string]*ScheduledJob
	stopCh        chan struct{}
	running       bool
	tickInterval  time.Duration
	jobStore      JobStore                  // optional: persists job state across restarts
	cbStore       *AgentCircuitBreakerStore // per-agent circuit breakers (nil = disabled)
	buildRevision string                    // running binary's VCS revision (deploy-drift diagnosis)
	onCycleIdle   func()                    // optional: fired after a cycle completes with no job in flight
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
	// Context carries the timeout/deadline context so runners can propagate
	// cancellation to downstream operations (e.g., RetryPolicy.ExecuteContext).
	// Never nil — always a real context (timeout or background).
	Context context.Context
}

// AgentRunner is the function that actually executes an agent. Injected for testability.
// Returns (outcome, output, result, error). res carries per-run detail (node
// trace etc.) and may be nil when the run produced no result.
// For long-running agents, the runner should periodically update the checkpoint.
type AgentRunner func(ctx RunContext) (outcome, output string, res *RunResult, err error)

// SchedulerConfig configures a new scheduler.
type SchedulerConfig struct {
	Registry     *Registry
	History      *History
	TickInterval time.Duration             // how often to check for due jobs (default: 1m)
	JobStore     JobStore                  // optional: persists jobs across restarts (nil = in-memory only)
	CBStore      *AgentCircuitBreakerStore // optional: per-agent circuit breakers (nil = disabled)

	// FeedbackPath, when set, points at the on-disk knowledge-graph feedback
	// snapshot. On startup the scheduler re-hydrates prior Fitness/RunCount/
	// tool-edges from it into knowledge.GlobalGraph and arms the debounced writer
	// so later feedback lands back on disk. Empty = feedback persistence disabled.
	FeedbackPath string
	// FeedbackFlushInterval is the minimum interval between throttled feedback
	// writes. Defaults to 30s when zero (and FeedbackPath is set).
	FeedbackFlushInterval time.Duration

	// BuildRevision is the running binary's VCS revision (dashboard.
	// ReadBuildIdentity().Revision). It is stamped onto every AgentBus/webhook
	// event and used by the cycle-complete deploy-drift check (program 94b0b31):
	// when set, runJob compares it against the repo HEAD and WARNs if the
	// running binary has fallen behind. Empty disables the drift check.
	BuildRevision string

	// OnCycleIdle, when set, is invoked (non-blocking contract: keep it cheap
	// or hand off to a channel) after a scheduled cycle completes while no
	// other job is in flight — the idle window in which a deploy-drift
	// rebuild/restart is safe. Wired by the daemon to the drift watcher's Kick
	// so adoption no longer depends on a fixed tick landing in a gap between
	// back-to-back cycles.
	OnCycleIdle func()
}

// NewScheduler creates a new agent scheduler.
// If cfg.JobStore is set, persisted jobs are loaded on startup.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 1 * time.Minute
	}
	s := &Scheduler{
		reg:           cfg.Registry,
		history:       cfg.History,
		jobs:          make(map[string]*ScheduledJob),
		stopCh:        make(chan struct{}),
		tickInterval:  cfg.TickInterval,
		jobStore:      cfg.JobStore,
		cbStore:       cfg.CBStore,
		buildRevision: cfg.BuildRevision,
		onCycleIdle:   cfg.OnCycleIdle,
	}
	// Restore persisted jobs, then reconcile them against the registry YAML.
	// The registry definition is the source of truth: stale jobs for deleted
	// agents are removed, on_demand agents cannot keep active recurring jobs,
	// duplicate jobs are collapsed, and missing YAML-scheduled jobs are created.
	if cfg.JobStore != nil {
		s.loadState()
		s.ReconcileWithRegistry()
	}
	// Restore persisted circuit breaker state so a restart doesn't forget that
	// an agent was open (missing file is not an error — first-boot case).
	if s.cbStore != nil {
		if err := s.cbStore.Load(CircuitBreakersFile()); err != nil {
			slog.Warn("scheduler: restore circuit breaker state failed", "path", CircuitBreakersFile(), "err", err)
		}
	}
	// Read/startup half of feedback persistence: re-hydrate prior feedback from
	// disk (log, don't fail, on error — matches the missing-file-no-error
	// contract), then arm the debounced writer for subsequent runs.
	if cfg.FeedbackPath != "" {
		interval := cfg.FeedbackFlushInterval
		if interval == 0 {
			interval = 30 * time.Second
		}
		if err := knowledge.GlobalGraph.LoadFeedback(cfg.FeedbackPath); err != nil {
			slog.Warn("scheduler: restore feedback snapshot failed", "path", cfg.FeedbackPath, "err", err)
		}
		knowledge.GlobalGraph.ConfigureFeedbackPersistence(cfg.FeedbackPath, interval)
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

	// Scheduling is an operator-visible state change. Persist it to the
	// registry YAML too, otherwise restart reconciliation can resurrect or
	// remove jobs based on stale metadata.
	if s.reg != nil {
		if err := s.reg.UpdateSchedule(agentName, schedule); err != nil {
			return nil, fmt.Errorf("persist schedule for %q: %w", agentName, err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// on_demand means explicitly paused: remove all jobs for this agent.
	if schedule == "" || schedule == "on_demand" {
		for id, existing := range s.jobs {
			if existing.AgentName == agentName {
				slog.Warn("scheduler: deleting job (agent set to on_demand via Schedule)",
					"job_id", id, "agent", agentName, "run_count", existing.RunCount)
				delete(s.jobs, id)
			}
		}
		s.saveStateLocked()
		return &ScheduledJob{ID: fmt.Sprintf("job_%s_on_demand", agentName), AgentName: agentName, Schedule: "on_demand", Active: false}, nil
	}

	// Dedup: if any job for this agent already exists, update the best one and
	// delete the rest instead of creating another duplicate.
	var keep *ScheduledJob
	for id, existing := range s.jobs {
		if existing.AgentName != agentName {
			continue
		}
		if keep == nil || betterScheduledJob(existing, keep) {
			if keep != nil {
				delete(s.jobs, keep.ID)
			}
			keep = existing
		} else {
			delete(s.jobs, id)
		}
	}
	if keep != nil {
		// Catch-up preservation (2026-07-15): when the schedule string is
		// unchanged, keep a zero NextRun (loadState's crash-recovery "run
		// immediately" marker) and keep a past-due NextRun (a slot missed while
		// the daemon was down or the queue was busy) so the first tick fires the
		// missed run. Unconditionally overwriting NextRun here is how the
		// startup auto-schedule loop silently dropped hermes-daily-updater's
		// 06:00 slot for a full day and defeated crash recovery.
		sameSchedule := keep.Schedule == schedule
		missedSlot := !keep.NextRun.IsZero() && keep.NextRun.Before(time.Now())
		switch {
		case sameSchedule && keep.NextRun.IsZero():
			// preserve immediate-run marker
		case sameSchedule && missedSlot:
			slog.Info("scheduler: preserving missed slot for catch-up",
				"agent", agentName, "missed_next_run", keep.NextRun)
		default:
			keep.NextRun = nextRun
		}
		keep.Schedule = schedule
		keep.Timeout = timeout
		keep.MaxRetries = maxRetries
		keep.Active = true
		s.saveStateLocked()
		return keep, nil
	}

	job := &ScheduledJob{
		ID:         fmt.Sprintf("job_%s_%d", agentName, time.Now().UnixNano()),
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
		Context:   ctx,
	}

	start := time.Now()
	var res *RunResult
	outcome, output, res, err = runner(runCtx)
	duration := time.Since(start)

	// Record history
	if s.history != nil {
		quality := recordedQuality(inst, outcome, output, res)
		_ = s.history.Record(RunRecord{
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
		s.persistRunFeedback()
		// Record decision trace for failure explainability
		runID := fmt.Sprintf("%s-%d", inst.Definition.Tree, start.UnixNano())
		var steps []knowledge.TraceStep
		if res != nil {
			steps = stepsFromChildTicks(res.ChildTicks)
		}
		knowledge.GlobalTraceStore.Record(knowledge.DecisionTrace{
			RunID:     runID,
			TreeID:    inst.Definition.Tree,
			Task:      task,
			Steps:     steps,
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
						slog.Error("scheduler: tick panicked (recovered)", "panic", r)
					}
				}()
				s.SyncFromRegistry()
				s.tick(runner)
			}()
		}
	}
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	// Force a final feedback flush so any pending (throttled) run feedback is
	// durably written even inside the throttle window. No-op when persistence is
	// not configured or nothing is dirty.
	if err := knowledge.GlobalGraph.FlushFeedback(true); err != nil {
		slog.Warn("scheduler: final feedback flush failed", "err", err)
	}
	close(s.stopCh)
}

// persistRunFeedback marks the knowledge graph dirty after a run recorded
// feedback and attempts a throttled, best-effort flush. Bursty in-window calls
// are suppressed by FlushFeedback and captured by the forced flush in Stop().
func (s *Scheduler) persistRunFeedback() {
	knowledge.GlobalGraph.MarkFeedbackDirty()
	if err := knowledge.GlobalGraph.FlushFeedback(false); err != nil {
		slog.Warn("scheduler: feedback flush failed", "err", err)
	}
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

// GetCBStore returns the circuit breaker store for operator inspection.
// Returns nil if circuit breakers are not configured.
func (s *Scheduler) GetCBStore() *AgentCircuitBreakerStore {
	return s.cbStore
}

// AnyInFlight reports whether any scheduled job is currently executing. The
// deploy-drift rebuild guardrail (program 94b0b31 milestone 5) consults this
// before swapping the daemon's own binary, since doing so out from under a
// mid-run job is the hazard the out-of-place rebuild exists to avoid.
func (s *Scheduler) AnyInFlight() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, j := range s.jobs {
		if j.InFlight {
			return true
		}
	}
	return false
}

// RemoveJob removes a scheduled job.
func (s *Scheduler) RemoveJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	slog.Warn("scheduler: deleting job (RemoveJob call)",
		"job_id", jobID, "agent", job.AgentName, "run_count", job.RunCount)
	delete(s.jobs, jobID)
	s.saveStateLocked()
	return nil
}

func betterScheduledJob(candidate, current *ScheduledJob) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	if candidate.Active != current.Active {
		return candidate.Active
	}
	if candidate.RunCount != current.RunCount {
		return candidate.RunCount > current.RunCount
	}
	if !candidate.LastRun.Equal(current.LastRun) {
		return candidate.LastRun.After(current.LastRun)
	}
	return candidate.ID > current.ID
}

// ApplySchedule persists an agent schedule to registry YAML and scheduler-jobs.json.
// Use from CLI tools; a running bt-agent picks up YAML changes on the next scheduler sync tick.
func ApplySchedule(reg *Registry, agentName, schedule, timeout string, maxRetries int) (*ScheduledJob, error) {
	if timeout == "" {
		timeout = "2h"
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	if err := os.MkdirAll(JobsDir(), 0755); err != nil {
		return nil, fmt.Errorf("create jobs dir: %w", err)
	}
	store := NewFileJobStore(SchedulerJobsFile())
	sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	return sched.Schedule(agentName, schedule, timeout, maxRetries)
}

// RemoveAgentJobs drops persisted scheduler jobs for an agent name.
func RemoveAgentJobs(agentName string) error {
	store := NewFileJobStore(SchedulerJobsFile())
	jobs, err := store.Load()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	filtered := make([]ScheduledJob, 0, len(jobs))
	for _, job := range jobs {
		if job.AgentName != agentName {
			filtered = append(filtered, job)
		}
	}
	if len(filtered) == len(jobs) {
		return nil
	}
	return store.Save(filtered)
}

// DeleteRegisteredAgent removes an agent from the registry and clears scheduler jobs.
func DeleteRegisteredAgent(reg *Registry, name string) error {
	if reg == nil {
		return fmt.Errorf("registry required")
	}
	if _, err := reg.Get(name); err != nil {
		return err
	}
	if _, err := ApplySchedule(reg, name, "on_demand", "2h", 3); err != nil {
		return fmt.Errorf("clear schedule for %q: %w", name, err)
	}
	if err := reg.Delete(name); err != nil {
		return err
	}
	return RemoveAgentJobs(name)
}

// SyncFromRegistry reloads registry YAML from disk and reconciles in-memory jobs.
func (s *Scheduler) SyncFromRegistry() {
	if s.reg != nil {
		if err := s.reg.ReloadFromDisk(); err != nil {
			slog.Warn("scheduler: registry reload failed", "error", err)
		}
	}
	s.ReconcileWithRegistry()
}

// ReconcileWithRegistry canonicalizes scheduler state using agent YAML as the
// source of truth. It removes jobs for deleted agents, removes all jobs for
// on_demand agents, collapses duplicates for scheduled agents, forces job
// schedules to match YAML, and creates a missing active job for every YAML
// recurring schedule.
func (s *Scheduler) ReconcileWithRegistry() {
	if s.reg == nil {
		return
	}

	agents := s.reg.List()
	if len(agents) == 0 {
		// An empty registry at reconcile time is far more likely a failed or
		// partial registry construction (main() tolerates NewRegistry errors)
		// than a genuine zero-agent deployment. Proceeding would drop every
		// persisted job and its run history. Abort: stale-job cleanup can wait
		// until the registry is actually readable.
		slog.Warn("scheduler: reconcile sees EMPTY registry — keeping persisted jobs untouched")
		return
	}
	defs := make(map[string]Definition, len(agents))
	for _, inst := range agents {
		defs[inst.Definition.Name] = inst.Definition
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	bestByAgent := make(map[string]*ScheduledJob)
	for id, job := range s.jobs {
		def, ok := defs[job.AgentName]
		if !ok {
			// Loud drop: if the registry is unexpectedly empty or partial at
			// startup, this path silently discards run history (last_run,
			// run_count) for every persisted job — make that visible.
			slog.Warn("scheduler: dropping job for agent not in registry",
				"job_id", id, "agent", job.AgentName, "run_count", job.RunCount)
			delete(s.jobs, id)
			continue
		}
		sched := def.Schedule
		if sched == "on_demand" {
			slog.Warn("scheduler: deleting job (registry YAML says on_demand)",
				"job_id", id, "agent", job.AgentName, "run_count", job.RunCount)
			delete(s.jobs, id)
			continue
		}
		if sched != "" {
			applyJobSchedule(job, sched)
		}
		job.Active = true
		if existing, ok := bestByAgent[job.AgentName]; !ok || betterScheduledJob(job, existing) {
			if ok {
				delete(s.jobs, existing.ID)
			}
			bestByAgent[job.AgentName] = job
		} else {
			delete(s.jobs, id)
		}
	}

	for name, def := range defs {
		sched := def.Schedule
		if sched == "" || sched == "on_demand" {
			continue
		}
		if _, ok := bestByAgent[name]; ok {
			continue
		}
		next, err := parseSchedule(sched)
		if err != nil {
			slog.Warn("scheduler: skipping invalid YAML schedule", "agent", name, "schedule", sched, "error", err)
			continue
		}
		job := &ScheduledJob{
			ID:         fmt.Sprintf("job_%s_%d", name, time.Now().UnixNano()),
			AgentName:  name,
			Schedule:   sched,
			NextRun:    next,
			MaxRetries: 3,
			Timeout:    "2h",
			Active:     true,
		}
		s.jobs[job.ID] = job
		bestByAgent[name] = job
	}

	s.saveStateLocked()
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
		// Check circuit breaker before starting the job.
		// If the circuit is open, skip the run entirely instead of
		// wasting resources on a known-broken agent.
		if s.cbStore != nil {
			if !s.cbStore.Allowed(job.AgentName) {
				cb := s.cbStore.Get(job.AgentName)
				slog.Warn("scheduler: skipping agent — circuit breaker open",
					"agent", job.AgentName, "state", cb.State(), "failures", cb.FailureCount(), "cooldown", cb.cooldown)
				continue
			}
		}
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
	ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
	defer cancel()

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
		Context:    ctx,
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
	var runRes *RunResult
	var runErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("scheduler: agent panicked in runJob (recovered)", "agent", job.AgentName, "panic", r)
				outcome = "panic"
				runErr = fmt.Errorf("agent panicked: %v", r)
			}
		}()
		outcome, output, runRes, runErr = runner(runCtx)
	}()
	duration := time.Since(start)

	// A retry-exhausted runner whose attempts never produced a result returns
	// a zero-valued outcome (observed live 2026-07-15 00:08: the Telegram
	// notification rendered "Outcome:  in 4s"). Stamp it a failure so history,
	// the circuit breaker, and task_complete consumers see an honest outcome —
	// runErr already carries the detail.
	if outcome == "" && runErr != nil {
		outcome = "failure"
	}

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
	// Program throughput: a successful cycle whose multi-cycle program still
	// has pending milestones requeues after a short cooldown instead of
	// idling until the next cron slot (the run output carries the marker).
	job.NextRun = fastRequeueAfterSuccess(outcome, output, job.NextRun, time.Now())
	s.mu.Unlock()

	// Persist updated job state
	s.saveState()

	// Record history
	quality := recordedQuality(inst, outcome, output, runRes)
	errStr := ""
	if runErr != nil {
		errStr = runErr.Error()
	}
	if s.history != nil {
		_ = s.history.Record(RunRecord{
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

	// One structured INFO line per scheduled cycle — the operationally useful
	// event that previously had to be reconstructed from run.json/history by
	// hand. `journalctl … | grep "scheduler: cycle complete"` is now the
	// cycle-outcome history, and the extracted facts (commit, apply status,
	// milestone, seeding, brainstorm) surface what each cycle actually did.
	logArgs := []any{
		"agent", job.AgentName,
		"outcome", outcome,
		"duration", duration.Truncate(time.Second).String(),
		"quality", quality,
		"run_count", job.RunCount,
	}
	for k, v := range extractCycleFacts(output) {
		logArgs = append(logArgs, k, v)
	}
	if errStr != "" {
		logArgs = append(logArgs, "error", truncateForLog(errStr, 200))
	}
	slog.Info("scheduler: cycle complete", logArgs...)

	// Deploy-drift diagnosis (program 94b0b31): on every cycle — not just the
	// slower background watcher (StartDriftWatcher) — compare the running
	// binary's revision against repo HEAD so a stale daemon is visible without
	// cross-referencing dlq entries. Skipped when BuildRevision is unset
	// (detection is opt-in per SchedulerConfig.BuildRevision's doc).
	if s.buildRevision != "" {
		if repoDir, err := os.Getwd(); err == nil {
			if head, stale, err := DriftStatus(repoDir, s.buildRevision); err == nil && stale {
				slog.Warn("scheduler: deploy drift detected",
					"head_revision", head, "running_revision", s.buildRevision)
			}
		}
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
		// Raw values — consumers (Hermes webhook templates) do the labeling.
		// Empty on success / when unavailable. RateLimitCarryoverOutcome is a
		// healthy, expected backoff pause (see cycleBreakerSuccess), not a
		// genuine failure, so it must not alarm the Hermes webhook/Telegram
		// template either.
		failureReason := ""
		if runErr != nil {
			failureReason = runErr.Error()
		} else if outcome != "success" && !IsRateLimitCarryover(outcome) {
			failureReason = fmt.Sprintf("agent outcome: %s", outcome)
		}

		nodesStr := ""
		if runRes != nil && len(runRes.NodePaths) > 0 {
			nodesStr = strings.Join(runRes.NodePaths, " → ")
		}

		webhookParent := runCtx.Context
		if runRes != nil && runRes.TraceID != "" {
			webhookParent = tracing.ContextWithTraceParentHeader(runCtx.Context, "00-"+runRes.TraceID+"-"+runRes.SpanID+"-01")
		}
		_, whSpan := tracing.StartSpan(webhookParent, "agent.webhook_publish")
		whSpan.SetAttribute("agent", job.AgentName)
		whSpan.SetAttribute("event_type", eventType)
		GlobalAgentBus.Publish(AgentEvent{
			Type:    eventType,
			Source:  job.AgentName,
			Message: fmt.Sprintf("%s: %s (%s)", job.AgentName, outcome, duration.Truncate(time.Second)),
			Data: map[string]interface{}{
				"tree":           tree,
				"task":           runCtx.Task,
				"outcome":        outcome,
				"duration":       duration.Truncate(time.Second).String(),
				"failure_reason": failureReason,
				"nodes":          nodesStr,
				"build_revision": s.buildRevision,
				// Rendered verbatim by the bt-task-complete Telegram template
				// as {data.summary} — the operator-facing "what did this run
				// actually do" digest (headline, run/commit facts, step trail).
				"summary": buildRunActivitySummary(output, failureReason, nodesStr),
			},
		})
		whSpan.End()
	}

	// Feed back into knowledge graph
	if inst.Definition.Tree != "" {
		knowledge.GlobalGraph.RecordRun(knowledge.RunRecord{
			TreeID:   inst.Definition.Tree,
			Task:     runCtx.Task,
			Outcome:  outcome,
			Duration: duration,
		})
		s.persistRunFeedback()
		// Record decision trace for failure explainability
		runID := fmt.Sprintf("%s-sched-%d", inst.Definition.Tree, start.UnixNano())
		var steps []knowledge.TraceStep
		if runRes != nil {
			steps = stepsFromChildTicks(runRes.ChildTicks)
		}
		knowledge.GlobalTraceStore.Record(knowledge.DecisionTrace{
			RunID:     runID,
			TreeID:    inst.Definition.Tree,
			Task:      runCtx.Task,
			Steps:     steps,
			Outcome:   outcome,
			StartedAt: start,
			EndedAt:   time.Now(),
		})
	}

	// Report outcome to circuit breaker store. Healthy no-code outcomes
	// (no_change, degraded) count as breaker success: a stretch of
	// analysis-only cycles must not open an agent's breaker with nothing
	// actually broken.
	reportAgentOutcome(s.cbStore, job.AgentName, cycleBreakerSuccess(outcome, runErr))
	if s.cbStore != nil {
		if err := s.cbStore.Save(CircuitBreakersFile()); err != nil {
			slog.Warn("scheduler: persist circuit breaker state failed", "path", CircuitBreakersFile(), "err", err)
		}
	}

	// Idle window: nothing else is mid-execution right now, so a deploy-drift
	// rebuild/restart is safe. Let the watcher check immediately instead of
	// waiting for a fixed tick that back-to-back cycles starve forever.
	if s.onCycleIdle != nil && !s.AnyInFlight() {
		s.onCycleIdle()
	}
}

// cycleBreakerSuccess reports whether a completed cycle counts as a success
// for the agent's circuit breaker. Healthy terminal outcomes — success,
// no_change (analysis-only), degraded (deterministic fallback), and the
// rate-limit carryover (an expected pause; a long backoff window must not
// walk the breaker open) — keep the breaker closed; genuine failures and
// errored runs count against it.
func cycleBreakerSuccess(outcome string, runErr error) bool {
	if IsRateLimitCarryover(outcome) {
		return true
	}
	return runErr == nil && isHealthyOutcome(outcome)
}

// stepsFromChildTicks converts a run's terminal child ticks (engine.Blackboard.
// ChildTicks(), surfaced on RunResult) into knowledge.TraceSteps so the two
// production DecisionTrace sites below give ExplainLastFailure a real path
// instead of an empty one.
func stepsFromChildTicks(ticks []engine.ChildTick) []knowledge.TraceStep {
	converted := make([]knowledge.ChildTick, len(ticks))
	for i, t := range ticks {
		converted[i] = knowledge.ChildTick{Parent: t.Parent, Child: t.Child, Status: t.Status}
	}
	return knowledge.StepsFromChildTicks(converted)
}

// historyQualityScore combines heuristic and YAML QualitySpec scoring for
// history records. The rate-limit carryover pause is exempted from the
// 0.0-quality convention below: it is a healthy, expected state (see
// cycleBreakerSuccess), not a genuine failure, so it must not be persisted to
// scheduler history looking indistinguishable from one.
func historyQualityScore(inst *Instance, outcome, output string) float64 {
	quality := 0.0
	if outcome != "success" && !IsRateLimitCarryover(outcome) {
		return quality
	}
	quality = estimateQuality(output)
	if inst != nil && inst.Definition.Quality != nil {
		specScore, _, _ := ValidateQualitySpec(inst.Definition.Quality, output)
		if specScore > quality {
			quality = specScore
		}
	}
	return quality
}

// recordedQuality is the quality persisted for a scheduled run. RunOnce's
// RunResult already carries applyOutcomeRefinement's score — the authoritative
// committed=0.9 and the refined no_change=0.5 / degraded=0.3 signals — so prefer
// it for healthy outcomes instead of recomputing the text-shape estimate, which
// discards those signals (committed runs otherwise land as 0.75/0.9/1.0 by
// output length, and no_change/degraded as 0.0). The rate-limit carryover pause
// is healthy too (see cycleBreakerSuccess) even though isHealthyOutcome doesn't
// cover it, so it also prefers the RunResult's authoritative quality. Non-healthy
// outcomes (failure/timeout/partial) keep the historyQualityScore 0.0
// convention, and a nil RunResult (e.g. a panicked run) falls back to the
// estimate.
func recordedQuality(inst *Instance, outcome, output string, res *RunResult) float64 {
	if res != nil && (isHealthyOutcome(outcome) || IsRateLimitCarryover(outcome)) {
		return res.Quality
	}
	return historyQualityScore(inst, outcome, output)
}

// estimateQuality is a fast quality heuristic for output text.
func estimateQuality(output string) float64 {
	trimmed := strings.TrimSpace(output)
	lower := strings.ToLower(trimmed)
	if len(trimmed) < 10 {
		return 0.2
	}

	score := 0.35
	if len(trimmed) >= 30 {
		score = 0.5
	}
	if len(trimmed) > 200 {
		score += 0.15
	}
	if len(trimmed) > 500 {
		score += 0.15
	}
	if strings.Contains(trimmed, "## ") || strings.Contains(trimmed, "**") || strings.Contains(trimmed, "- ") {
		score += 0.1
	}

	// Deterministic production agents are often concise. Score them by evidence
	// fields instead of raw length so future runs stop being marked low-quality
	// when they emit compact but complete reports.
	evidenceTerms := []string{
		"status:", "severity:", "route:", "target:", "timestamp:", "threshold:",
		"symbols:", "delta:", "citation", "source", "artifact", "processed:",
		"errors:", "idempotency", "decision:", "rationale:", "auth:", "quota:",
	}
	evidence := 0
	for _, term := range evidenceTerms {
		if strings.Contains(lower, term) {
			evidence++
		}
	}
	if evidence >= 3 {
		score += 0.25
	}
	if evidence >= 5 {
		score += 0.15
	}

	badPatterns := []string{"not implemented", "i don't know", "i cannot", "i can't", "unable to", "failed to", "error:"}
	for _, p := range badPatterns {
		if strings.Contains(lower, p) {
			score -= 0.4
			break
		}
	}
	if score < 0 {
		return 0
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

func applyJobSchedule(job *ScheduledJob, sched string) {
	prev := job.Schedule
	job.Schedule = sched
	// A zero NextRun is loadState's crash-recovery "run immediately" marker. A
	// schedule change (or format normalization) during ReconcileWithRegistry
	// must not push a recovered-crashed job to its next cron slot — that would
	// defeat the immediate re-run after restart. Only a job with a real future
	// NextRun is rescheduled on a schedule change.
	if sched != prev && !job.NextRun.IsZero() {
		if next, err := parseSchedule(sched); err == nil {
			job.NextRun = next
		}
	}
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
			slog.Warn("scheduler: cron parse error, falling back to +1h", "schedule", sched, "error", err)
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
func parseCronField(field string, minVal, maxVal int) (func(int) bool, error) {
	if field == "*" {
		return func(v int) bool { return v >= minVal && v <= maxVal }, nil
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
		slog.Warn("scheduler: failed to persist jobs", "error", err)
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
		slog.Warn("scheduler: failed to persist jobs", "error", err)
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
		slog.Warn("scheduler: failed to load persisted jobs", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Loud restore accounting: on 2026-07-02 scheduler-jobs.json came back
	// with zeroed last_run/run_count after daemon kills, and the silent
	// restore made the loss undiagnosable. If the state file exists but no
	// jobs were restored, say so — that is the clobber signature.
	if len(jobs) == 0 {
		if fi, statErr := os.Stat(SchedulerJobsFile()); statErr == nil && fi.Size() > 4 {
			slog.Warn("scheduler: state file exists but restored zero jobs — possible clobbered or truncated state",
				"path", SchedulerJobsFile(), "size", fi.Size())
		} else {
			// Not silent: a missing state file on a host with run history is
			// itself the anomaly (observed 08:27 2026-07-03 — file vanished
			// across a restart and every job lost last_run/run_count).
			slog.Warn("scheduler: no persisted job state found — starting with fresh jobs",
				"path", SchedulerJobsFile())
		}
	} else {
		slog.Info("scheduler: restored persisted jobs", "count", len(jobs))
	}

	crashedCount := 0
	for i := range jobs {
		j := jobs[i] // copy
		if j.InFlight {
			// This job was running when bt-agent crashed.
			// Clear in-flight flag, reset NextRun to "now" so it
			// retries immediately on the next tick.
			slog.Warn("scheduler: recovered crashed job",
				"job_id", j.ID, "agent", j.AgentName, "run_count", j.RunCount)
			j.InFlight = false
			j.NextRun = time.Time{} // run immediately on next tick
			crashedCount++
		}
		s.jobs[j.ID] = &j
	}
	if crashedCount > 0 {
		slog.Warn("scheduler: recovered in-flight jobs from crash", "count", crashedCount)
	}
}

// fastRequeueAfterSuccess returns an accelerated next-run time when a
// successful run's output carries the PROGRAM-CONTINUE marker (an active
// multi-cycle program has pending milestones); otherwise the cron-derived
// time is kept. The 2-minute cooldown lets the apply/push settle and keeps
// a marker bug from producing a hot loop.
func fastRequeueAfterSuccess(outcome, output string, cronNext, now time.Time) time.Time {
	if outcome != "success" || !strings.Contains(output, "PROGRAM-CONTINUE") {
		return cronNext
	}
	fast := now.Add(2 * time.Minute)
	if fast.Before(cronNext) {
		return fast
	}
	return cronNext
}
