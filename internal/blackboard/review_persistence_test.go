package blackboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// A fresh persistent scope must not hold the manager's map lock during I/O.
func TestReviewBlackboardSlowLoadDoesNotStallRun(t *testing.T) {
	m := DefaultManager()
	if err := m.EnablePersistence(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeAgent, ID: "slow"}
	path := m.persistFile(scope)
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	loaded := make(chan error, 1)
	go func() { _, err := m.List(scope, "", 0); loaded <- err }()
	// Opening the writer synchronizes with the loader entering ReadFile.
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- m.Set(Scope{Kind: ScopeRun, ID: "free"}, "k", "v", "", "text") }()
	select {
	case err := <-runDone:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("run scope blocked behind another scope's disk read")
	}
	// Replace the FIFO before finishing the read, including for implementations
	// that redundantly reload a new store a second time.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"entries":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write([]byte(`{"entries":{}}`))
	_ = writer.Close()
	if err := <-loaded; err != nil {
		t.Fatal(err)
	}
}

func TestReviewBlackboardReconfigureDoesNotCopyOldRoot(t *testing.T) {
	m := DefaultManager()
	a, b := t.TempDir(), t.TempDir()
	scope := Scope{Kind: ScopeAgent, ID: "switch"}
	if err := m.EnablePersistence(a); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(scope, "old", "private", "", "text"); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePersistence(b); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(scope, "new", "value", "", "text"); err != nil {
		t.Fatal(err)
	}
	fresh := DefaultManager()
	if err := fresh.EnablePersistence(b); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Get(scope, "old"); err == nil {
		t.Fatal("old root data copied into new root")
	}
}

func TestReviewBlackboardQueuedOperationUsesCurrentRoot(t *testing.T) {
	m := DefaultManager()
	a, b := t.TempDir(), t.TempDir()
	scope := Scope{Kind: ScopeAgent, ID: "queued"}
	if err := m.EnablePersistence(a); err != nil {
		t.Fatal(err)
	}
	id, _ := m.scopeID(scope)
	gate := m.acquireGate(id)
	done := make(chan error, 1)
	go func() { done <- m.Set(scope, "key", "value", "", "text") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.gatesMu.Lock()
		queued := gate.refs == 2
		m.gatesMu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			m.releaseGate(id, gate)
			t.Fatal("operation did not queue")
		}
		time.Sleep(time.Millisecond)
	}
	if err := m.EnablePersistence(b); err != nil {
		m.releaseGate(id, gate)
		t.Fatal(err)
	}
	m.releaseGate(id, gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	fresh := DefaultManager()
	if err := fresh.EnablePersistence(b); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Get(scope, "key"); err != nil {
		t.Fatalf("queued operation used obsolete root: %v", err)
	}
}

func TestReviewBlackboardConcurrentReconfiguration(t *testing.T) {
	m := DefaultManager()
	roots := []string{t.TempDir(), t.TempDir()}
	scope := Scope{Kind: ScopeAgent, ID: "changing"}
	for _, root := range roots {
		fresh := DefaultManager()
		if err := fresh.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		if err := fresh.Set(scope, "marker", root, "", "text"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.EnablePersistence(roots[0]); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			if err := m.EnablePersistence(roots[i%2]); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 3 {
		wg.Go(func() {
			for range 30 {
				if _, err := m.Append(scope, "log", "x", "", "text"); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	total := 0
	for _, root := range roots {
		fresh := DefaultManager()
		if err := fresh.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		if e, err := fresh.Get(scope, "marker"); err != nil || e.Value != root {
			t.Fatalf("root mixed during reconfiguration: %v %v", e, err)
		}
		if e, err := fresh.Get(scope, "log"); err == nil {
			total += len(e.Value)
		}
	}
	if total != 90 {
		t.Fatalf("lost appends across roots: got %d want 90", total)
	}
	m.gatesMu.Lock()
	defer m.gatesMu.Unlock()
	if len(m.gates) != 0 {
		t.Fatalf("leaked %d gates", len(m.gates))
	}
}

func TestReviewBlackboardSharedWrites(t *testing.T) {
	root := t.TempDir()
	scope := Scope{Kind: ScopeAgent, ID: "shared"}
	managers := make([]*Manager, 0, 3)
	for range 3 {
		m := DefaultManager()
		if err := m.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		managers = append(managers, m)
	}
	var wg sync.WaitGroup
	for j, m := range managers {
		for i := range 30 {
			wg.Go(func() {
				if err := m.Set(scope, fmt.Sprintf("%d-%d", j, i), "value", "", "text"); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	entries, err := m.List(scope, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 90 {
		t.Fatalf("persisted %d want 90", len(entries))
	}
}
func TestReviewBlackboardScopeCollisions(t *testing.T) {
	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a/b", "a_b"} {
		if err := m.Set(Scope{Kind: ScopeAgent, ID: id}, "key", id, "", "text"); err != nil {
			t.Fatal(err)
		}
	}
	m = DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a/b", "a_b"} {
		e, err := m.Get(Scope{Kind: ScopeAgent, ID: id}, "key")
		if err != nil {
			t.Fatal(err)
		}
		if e.Value != id {
			t.Errorf("scope %q contains %q", id, e.Value)
		}
	}
	ids, err := m.ListPersistedScopeIDs(ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("ids=%v", ids)
	}
}
func TestReviewBlackboardProcesses(t *testing.T) {
	if root := os.Getenv("REVIEW_BB_ROOT"); root != "" {
		m := DefaultManager()
		if err := m.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		for range 20 {
			if _, err := m.Append(Scope{Kind: ScopeAgent, ID: "process"}, "all", "x", "", "text"); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	root := t.TempDir()
	cmds := make([]*exec.Cmd, 0, 3)
	for range 3 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReviewBlackboardProcesses$")
		cmd.Env = append(os.Environ(), "REVIEW_BB_ROOT="+root)
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
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	e, err := m.Get(Scope{Kind: ScopeAgent, ID: "process"}, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Value) != 60 {
		t.Fatalf("lost process appends: %d of 60", len(e.Value))
	}
}
func TestReviewBlackboardLegacyMigration(t *testing.T) {
	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"entries":{"key":{"value":"legacy"}}}`)
	for _, id := range []string{"legacy", "ambiguous_name"} {
		if err := os.WriteFile(filepath.Join(root, "agent", id+".json"), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if e, err := m.Get(Scope{Kind: ScopeAgent, ID: "legacy"}, "key"); err != nil || e.Value != "legacy" {
		t.Fatalf("safe legacy read %v %v", e, err)
	}
	if _, err := m.Get(Scope{Kind: ScopeAgent, ID: "ambiguous_name"}, "key"); err == nil {
		t.Error("ambiguous legacy payload attributed without proof")
	}
	if err := m.Set(Scope{Kind: ScopeAgent, ID: "legacy"}, "new", "new", "", "text"); err != nil {
		t.Fatal(err)
	}
	ids, err := m.ListPersistedScopeIDs(ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate migrated scope %q", id)
		}
		seen[id] = true
	}
}
func TestReviewRunScopeCardinalityBound(t *testing.T) {
	m := DefaultManager()
	accepted := 0
	for i := range 1100 {
		if err := m.Set(Scope{Kind: ScopeRun, ID: fmt.Sprint(i)}, "key", "value", "", "text"); err == nil {
			accepted++
		}
	}
	if accepted > 1024 {
		t.Fatalf("retained %d ephemeral scopes, want bounded at 1024", accepted)
	}
}

// Engine tools share one Manager across scopes (RunScope ListRecent and
// ScopeSession reads), so a session scope stalled on a peer process's sidecar
// flock must not wedge unrelated run-scope traffic: run scopes are never
// persisted and need no file lock at all.
func TestReviewBlackboardPeerFileLockDoesNotStallRunScope(t *testing.T) {
	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	session := Scope{Kind: ScopeSession, ID: "wedged"}
	run := Scope{Kind: ScopeRun, ID: "unrelated"}
	if err := m.Set(session, "key", "session", "", "text"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(run, "key", "run", "", "text"); err != nil {
		t.Fatal(err)
	}

	// flock attaches to the open file description, so a second open of the
	// sidecar excludes this manager exactly like a peer process holding it.
	unlock, err := reliability.AcquireFileLock(m.persistFile(session))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	started := make(chan struct{})
	sessionDone := make(chan struct{})
	go func() {
		defer close(sessionDone)
		close(started)
		_, _ = m.Get(session, "key")
	}()
	<-started
	time.Sleep(200 * time.Millisecond) // let the session read reach the lock wait

	runDone := make(chan error, 1)
	go func() {
		_, err := m.Get(run, "key")
		runDone <- err
	}()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("run scope Get: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("run scope Get stalled behind a session scope waiting on a peer file lock")
	}

	select {
	case <-sessionDone:
		t.Fatal("session Get returned; the peer file lock never held, so the test proved nothing")
	default:
	}
	unlock()
	<-sessionDone
}

// A peer that wedges while holding a scope's sidecar flock must surface as a
// failed operation. The engine's tool path has no way to abandon a blackboard
// call, so an unbounded flock wait there is an unrecoverable stall.
func TestReviewBlackboardWedgedPeerFileLockTimesOut(t *testing.T) {
	prev := scopeLockTimeout
	scopeLockTimeout = 150 * time.Millisecond
	t.Cleanup(func() { scopeLockTimeout = prev })

	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeAgent, ID: "wedged"}
	if err := m.Set(scope, "key", "first", "", "text"); err != nil {
		t.Fatal(err)
	}
	unlock, err := reliability.AcquireFileLock(m.persistFile(scope))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	done := make(chan error, 1)
	go func() { done <- m.Set(scope, "key", "second", "", "text") }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wedged peer lock: got %v, want a deadline error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Set hung on a wedged peer's file lock instead of failing")
	}

	// The failed wait must not have leaked the scope's gate: once the peer
	// lets go, the very next operation proceeds.
	unlock()
	if err := m.Set(scope, "key", "second", "", "text"); err != nil {
		t.Fatalf("after the peer released: %v", err)
	}
	if e, err := m.Get(scope, "key"); err != nil || e.Value != "second" {
		t.Fatalf("post-release read: %v %v", e, err)
	}
}
