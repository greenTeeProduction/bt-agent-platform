package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Fail every result save while preserving the actual admission bytes on disk.
// A new process uses the real store without this injected failure.
type restartFaultJobStore struct {
	JobStore
	failed *bool
}

func (s restartFaultJobStore) Save(jobs []ScheduledJob) error {
	if *s.failed {
		return errors.New("fixture result storage unavailable")
	}
	return s.JobStore.Save(jobs)
}

func TestSchedulerRestartDoesNotReplayUnrecordedExecution(t *testing.T) {
	if root := os.Getenv("BT_RESTART_SAFETY_ROOT"); root != "" {
		runSchedulerRestartChild(t, root, os.Getenv("BT_RESTART_SAFETY_PHASE"), os.Getenv("BT_RESTART_SAFETY_FAULT"))
		return
	}
	for _, fault := range []string{"result-save", "uncertain-result-save", "history", "exit-after-action", "manual-result-save", "manual-history", "manual-exit-after-action"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			for _, phase := range []string{"execute", "restart", "restart"} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestSchedulerRestartDoesNotReplayUnrecordedExecution$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "BT_RESTART_SAFETY_ROOT="+root, "BT_RESTART_SAFETY_PHASE="+phase, "BT_RESTART_SAFETY_FAULT="+fault, "BT_AGENT_HOME="+filepath.Join(root, "home"))
				out, err := cmd.CombinedOutput()
				if phase == "execute" {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 23 {
						t.Fatalf("execution process did not reach controlled exit: %v\n%s", err, out)
					}
				} else if err != nil {
					t.Fatalf("restart process: %v\n%s", err, out)
				}
			}
			data, err := os.ReadFile(filepath.Join(root, "actions.log"))
			if err != nil || string(data) != "action\n" {
				t.Fatalf("side effect repeated across processes: %q, %v", data, err)
			}
		})
	}
}

func runSchedulerRestartChild(t *testing.T, root, phase, fault string) {
	t.Helper()
	manual := strings.HasPrefix(fault, "manual-")
	fault = strings.TrimPrefix(fault, "manual-")
	reg, err := NewRegistry(filepath.Join(root, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewFileJobStore(filepath.Join(root, "jobs.json"))
	if phase == "execute" {
		if _, err := reg.Create(Definition{Name: "restart-action", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
			t.Fatal(err)
		}
		history, err := NewHistory(filepath.Join(root, "history"))
		if err != nil {
			t.Fatal(err)
		}
		failed := false
		s := NewScheduler(SchedulerConfig{Registry: reg, History: history, JobStore: restartFaultJobStore{JobStore: store, failed: &failed}})
		runner := func(RunContext) (string, string, *RunResult, error) {
			if err := os.WriteFile(filepath.Join(root, "actions.log"), []byte("action\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if fault == "exit-after-action" {
				os.Exit(23) // kill boundary before any result or cleanup can run
			}
			if fault == "history" {
				if err := os.Mkdir(filepath.Join(root, "history", "restart-action.jsonl"), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				failed = true
			}
			if fault == "uncertain-result-save" {
				return "", "", nil, &reliability.ExecutionUncertainError{Err: errors.New("fixture lost acknowledgement")}
			}
			return "success", "completed fixture action", nil, nil
		}
		if manual {
			_, _, _ = s.RunNow("restart-action", "controlled action", runner, "10s")
		} else {
			job, err := s.Schedule("restart-action", "every 1h", "10s", 0)
			if err != nil {
				t.Fatal(err)
			}
			s.runJob(job, runner)
		}
		os.Exit(23) // no in-process repair, Stop, or deferred persistence
	}
	s := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	if _, err := s.Schedule("restart-action", "every 1h", "10s", 0); err == nil {
		t.Fatal("ordinary Schedule silently released unresolved execution")
	}
	s.SyncFromRegistry()
	// Even a changed registry schedule cannot release the execution hold.
	if err := reg.UpdateSchedule("restart-action", "every 1ms"); err != nil {
		t.Fatal(err)
	}
	s.SyncFromRegistry()
	jobs := s.ListJobs()
	held := false
	for _, job := range jobs {
		if job.Active {
			t.Fatalf("unresolved job was not held: %+v", jobs)
		}
		held = held || job.RecoveryRequired
	}
	if !held {
		t.Fatalf("unresolved job was not held: %+v", jobs)
	}
	s.tick(func(RunContext) (string, string, *RunResult, error) {
		f, err := os.OpenFile(filepath.Join(root, "actions.log"), os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return "failure", "", nil, err
		}
		_, _ = f.WriteString("replayed\n")
		_ = f.Close()
		return "success", "replayed", nil, nil
	})
	s.workers.Wait()
	if _, _, err := s.RunNow("restart-action", "same task", func(RunContext) (string, string, *RunResult, error) {
		t.Error("manual admission bypassed recovery hold")
		return "success", "", nil, nil
	}, "1s"); err == nil || !strings.Contains(err.Error(), "reconciliation") {
		t.Fatalf("manual recovery admission: %v", err)
	}
}

func TestSchedulerFailedAdmissionDoesNotExecute(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "no-admission", Tree: "domain:default"}); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(SchedulerConfig{Registry: reg})
	job, err := s.Schedule("no-admission", "every 1h", "1s", 0)
	if err != nil {
		t.Fatal(err)
	}
	s.jobStore = failedCleanupJobStore{err: errors.New("fixture admission storage unavailable")}
	calls := 0
	s.runJob(job, func(RunContext) (string, string, *RunResult, error) {
		calls++
		return "success", "", nil, nil
	})
	if calls != 0 || job.RunCount != 0 || job.InFlight {
		t.Fatalf("uncommitted admission dispatched: calls=%d job=%+v", calls, job)
	}
}

func TestSchedulerUnreadableStateFailsClosed(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(filepath.Join(root, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "unreadable", Tree: "domain:default", Schedule: "every 1ms"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "jobs.json")
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(SchedulerConfig{Registry: reg, JobStore: NewFileJobStore(path)})
	if _, err := s.Schedule("unreadable", "every 1ms", "1s", 0); err == nil {
		t.Fatal("unreadable state silently replaced by fresh schedule")
	}
	time.Sleep(5 * time.Millisecond)
	s.tick(func(RunContext) (string, string, *RunResult, error) {
		t.Error("unreadable state dispatched")
		return "success", "", nil, nil
	})
	s.workers.Wait()
	data, _ := os.ReadFile(path)
	if string(data) != "{broken" {
		t.Fatalf("unreadable recovery evidence clobbered: %q", data)
	}
}

func TestSchedulerRecoveryResolutionCommitsBeforeRelease(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(filepath.Join(root, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "held", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
		t.Fatal(err)
	}
	store := NewFileJobStore(filepath.Join(root, "jobs.json"))
	if err := store.Save([]ScheduledJob{{ID: "held-job", AgentName: "held", Schedule: "every 1h", InFlight: true, Active: true}}); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	if err := s.RemoveJob("held-job"); err == nil {
		t.Fatal("ordinary removal discarded recovery evidence")
	}
	s.jobStore = failedCleanupJobStore{err: errors.New("fixture resolution storage unavailable")}
	if err := s.ResolveRecovery("held-job", "operator", "completed"); err == nil || !s.ListJobs()[0].RecoveryRequired {
		t.Fatal("failed reconciliation released execution")
	}
	s.jobStore = store
	if err := s.ResolveRecovery("held-job", "", "completed"); err == nil {
		t.Fatal("missing reviewer accepted")
	}
	if err := s.ResolveRecovery("held-job", "operator", "retry"); err == nil {
		t.Fatal("automatic interrupted-slot replay accepted")
	}
	if err := s.ResolveRecovery("held-job", "operator", "completed"); err != nil {
		t.Fatal(err)
	}
	fresh := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	job := fresh.ListJobs()[0]
	if job.RecoveryRequired || !job.NextRun.After(time.Now()) || job.RecoveryReason != "resolved completed by operator" {
		t.Fatalf("resolved slot replayable or attribution lost: %+v", job)
	}
}

func TestSchedulerRecoveryHoldSurvivesDuplicateAndRegistryRemoval(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(filepath.Join(root, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "held", Tree: "domain:default", Schedule: "every 1ms"}); err != nil {
		t.Fatal(err)
	}
	store := NewFileJobStore(filepath.Join(root, "jobs.json"))
	if err := store.Save([]ScheduledJob{
		{ID: "held", AgentName: "held", Schedule: "every 1ms", InFlight: true, Active: true},
		{ID: "clean", AgentName: "held", Schedule: "every 1ms", Active: true},
	}); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	if err := reg.Delete("held"); err != nil {
		t.Fatal(err)
	}
	s.SyncFromRegistry()
	for _, job := range s.ListJobs() {
		if job.Active {
			t.Fatalf("clean duplicate bypassed hold: %+v", job)
		}
	}
	s.tick(func(RunContext) (string, string, *RunResult, error) {
		t.Error("held duplicate dispatched")
		return "success", "", nil, nil
	})
	s.workers.Wait()
	jobs, err := store.Load()
	if err != nil || len(jobs) == 0 {
		t.Fatalf("registry removal discarded unresolved execution: %+v, %v", jobs, err)
	}
}
