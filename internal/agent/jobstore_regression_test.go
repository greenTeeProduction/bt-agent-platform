package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFileJobStoreIndependentConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			jobs := []ScheduledJob{{ID: fmt.Sprint(i), AgentName: strings.Repeat("x", 20000)}}
			if err := NewFileJobStore(path).Save(jobs); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	jobs, err := NewFileJobStore(path).Load()
	if err != nil || len(jobs) != 1 || len(jobs[0].AgentName) != 20000 {
		t.Fatalf("incomplete committed snapshot: jobs=%d, err=%v", len(jobs), err)
	}
}

func TestFileJobStoreIgnoresPreplantedTempSymlink(t *testing.T) {
	dir := t.TempDir()
	path, victim := filepath.Join(dir, "jobs.json"), filepath.Join(dir, "unrelated")
	if err := os.WriteFile(victim, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := NewFileJobStore(path).Save([]ScheduledJob{{ID: "one"}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != "keep me" {
		t.Fatalf("unrelated file overwritten: %q, %v", got, err)
	}
}
