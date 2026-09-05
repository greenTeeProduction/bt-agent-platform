package hitl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestReviewHITLProcesses(t *testing.T) {
	if root := os.Getenv("REVIEW_HITL_ROOT"); root != "" {
		s, err := InitStore(root)
		if err != nil {
			t.Fatal(err)
		}
		for i := range 30 {
			if err := s.Create(&Request{ID: fmt.Sprintf("%s-%d", os.Getenv("REVIEW_CHILD"), i), Status: StatusPending}); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	root := t.TempDir()
	s, err := InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	for i := range 3 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReviewHITLProcesses$")
		cmd.Env = append(os.Environ(), "REVIEW_HITL_ROOT="+root, fmt.Sprintf("REVIEW_CHILD=%d", i))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.ListPending()); got != 90 {
		t.Fatalf("visible requests=%d want 90", got)
	}
}
func TestReviewHITLPollAndPreserve(t *testing.T) {
	root := t.TempDir()
	a, err := InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Create(&Request{ID: "first", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Approve("first", "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Create(&Request{ID: "second", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req, err := a.WaitForRequest(ctx, "first", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != StatusApproved {
		t.Fatal(req.Status)
	}
}
