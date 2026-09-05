package dashboard

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReviewTaskClaimsAtomic(t *testing.T) {
	s := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := s.Create(Task{ID: "task", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			tasks, err := s.ClaimApproved()
			if err != nil {
				t.Error(err)
			}
			claims.Add(int32(len(tasks)))
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claims=%d", claims.Load())
	}
}
func TestReviewTaskAdmissionDoesNotRevive(t *testing.T) {
	s := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := s.Create(Task{ID: "task", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(Task{ID: "task", Status: "approved"}); err == nil {
		t.Error("duplicate task admitted")
	}
	_ = s.Approve("task", "reviewer")
	if len(s.Approved()) != 0 {
		t.Error("approval retry revived completed work")
	}
}
func TestReviewTaskClaimPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	s := NewTaskStore(filepath.Join(root, "tasks.json"))
	if err := s.Create(Task{ID: "task", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.path+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if tasks, err := s.ClaimApproved(); err == nil || len(tasks) != 0 {
		t.Fatalf("failed write admitted work: %v %v", tasks, err)
	}
	if len(s.Approved()) != 1 {
		t.Fatal("failed claim changed memory")
	}
}
