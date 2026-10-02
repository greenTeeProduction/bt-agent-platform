package agent

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type failedCleanupJobStore struct{ err error }

func (s failedCleanupJobStore) Load() ([]ScheduledJob, error) { return nil, nil }
func (s failedCleanupJobStore) Save([]ScheduledJob) error     { return s.err }

func TestScheduleAndRemoveReportPersistenceFailure(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "job-persistence-failure"
	if _, err := reg.Create(Definition{Name: name, Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(SchedulerConfig{Registry: reg})
	sentinel := errors.New("disk unavailable")
	sched.jobStore = failedCleanupJobStore{err: sentinel}
	job, err := sched.Schedule(name, "every 1h", "30s", 0)
	if !errors.Is(err, sentinel) || job == nil {
		t.Fatalf("job=%v err=%v", job, err)
	}
	if err := sched.RemoveJob(job.ID); !errors.Is(err, sentinel) {
		t.Fatalf("remove err=%v", err)
	}
	if _, err := sched.Schedule(name, "on_demand", "30s", 0); !errors.Is(err, sentinel) {
		t.Fatalf("pause err=%v", err)
	}
}

type orderedCleanupJobStore struct {
	mu      sync.Mutex
	first   sync.Once
	entered chan struct{}
	release chan struct{}
	last    []ScheduledJob
}

func (s *orderedCleanupJobStore) Load() ([]ScheduledJob, error) { return nil, nil }
func (s *orderedCleanupJobStore) Save(jobs []ScheduledJob) error {
	s.first.Do(func() { close(s.entered); <-s.release })
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = append([]ScheduledJob(nil), jobs...)
	return nil
}

func TestSchedulerSaveCannotOverwriteLaterScheduleWithStaleSnapshot(t *testing.T) {
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"save-before", "save-after"} {
		if _, err := reg.Create(Definition{Name: name, Tree: "domain:default", Version: "1.0.0"}); err != nil {
			t.Fatal(err)
		}
	}
	sched := NewScheduler(SchedulerConfig{Registry: reg})
	if _, err := sched.Schedule("save-before", "every 1h", "30s", 0); err != nil {
		t.Fatal(err)
	}
	store := &orderedCleanupJobStore{entered: make(chan struct{}), release: make(chan struct{})}
	sched.jobStore = store
	saved := make(chan struct{})
	go func() { sched.saveState(); close(saved) }()
	<-store.entered
	changed := make(chan error, 1)
	go func() { _, err := sched.Schedule("save-after", "every 1h", "30s", 0); changed <- err }()
	select {
	case err := <-changed:
		close(store.release)
		t.Fatalf("schedule committed ahead of older snapshot: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(store.release)
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("save stuck")
	}
	select {
	case err := <-changed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("schedule stuck")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.last) != 2 {
		t.Fatalf("stale save lost newly scheduled job: %+v", store.last)
	}
}
