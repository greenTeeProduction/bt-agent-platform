package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/security"
	"github.com/nico/go-bt-evolve/internal/util"
	btcore "github.com/rvitorper/go-bt/core"
)

const restartFixtureRevision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func waitRestartFixtureFile(t *testing.T, root, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart fixture did not reach %s", name)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDashboardRestartOwnerAcrossProcesses(t *testing.T) {
	if phase := os.Getenv("BT_RESTART_CONTROL_FIXTURE_PHASE"); phase != "" {
		runDashboardRestartOwnerFixture(t, phase, os.Getenv("BT_RESTART_CONTROL_FIXTURE_ROOT"))
		return
	}
	root := t.TempDir()
	binary := filepath.Join(root, "fixture-version")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' 'bt-dashboard revision="+restartFixtureRevision+" vcs_time=unknown dirty=false'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	start := func(phase string) (*exec.Cmd, *bytes.Buffer) {
		t.Helper()
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		t.Cleanup(cancel)
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestDashboardRestartOwnerAcrossProcesses$", "-test.count=1")
		cmd.Env = append(os.Environ(), "BT_RESTART_CONTROL_FIXTURE_PHASE="+phase, "BT_RESTART_CONTROL_FIXTURE_ROOT="+root)
		var log bytes.Buffer
		cmd.Stdout, cmd.Stderr = &log, &log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// Clean up only this known child, including assertion failures.
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		return cmd, &log
	}
	child, log := start("busy-owner")
	waitRestartFixtureFile(t, root, "busy-ready.json")
	if err := agent.RequestOwnedRestart(root, "bt-dashboard", restartFixtureRevision); err == nil {
		t.Fatal("other-process owner restarted during accepted sprint work")
	}
	if _, err := os.Stat(filepath.Join(root, "restart.json")); !os.IsNotExist(err) {
		t.Fatal("busy owner invoked restart")
	}
	if err := util.SaveJSONAtomic(filepath.Join(root, "release.json"), true); err != nil {
		t.Fatal(err)
	}
	waitRestartFixtureFile(t, root, "idle.json")
	if err := agent.RequestOwnedRestart(root, "bt-dashboard", restartFixtureRevision); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("owner fixture: %v\n%s", err, log.String())
	}
	var proof struct {
		Status   int
		Admitted string
		Count    int
		OwnerPID int
	}
	data, err := os.ReadFile(filepath.Join(root, "proof.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	if proof.Status != 503 || proof.Admitted != "false" || proof.Count != 1 || proof.OwnerPID != child.Process.Pid {
		t.Fatalf("owner failed to seal real admission: %+v", proof)
	}
	if err := agent.RequestOwnedRestart(root, "bt-dashboard", restartFixtureRevision); err == nil {
		t.Fatal("dead owner accepted restart")
	}
	second, log := start("current-owner")
	waitRestartFixtureFile(t, root, "current-ready.json")
	if err := agent.RequestOwnedRestart(root, "bt-dashboard", restartFixtureRevision); err != nil {
		t.Fatal(err)
	}
	if err := util.SaveJSONAtomic(filepath.Join(root, "current-done.json"), true); err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("replacement fixture: %v\n%s", err, log.String())
	}
	var marker struct{ PID int }
	data, err = os.ReadFile(filepath.Join(root, "restart.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.PID != child.Process.Pid {
		t.Fatal("already-current replacement repeated the restart")
	}
}

func runDashboardRestartOwnerFixture(t *testing.T, phase, root string) {
	t.Helper()
	var beginCalls atomic.Int64
	restarted := make(chan struct{}, 1)
	cfg := agent.RestartControlConfig{Home: root, Unit: "bt-dashboard", Revision: strings.Repeat("a", 40), BinaryPath: filepath.Join(root, "fixture-version"), Enabled: true, VerifyOwner: func(string) error { return nil },
		BeginRestart: func() (func(bool), bool) { beginCalls.Add(1); return dashActivity.beginRestart() },
		Restart: func(unit string) error {
			if unit != "bt-dashboard" {
				return fmt.Errorf("wrong target")
			}
			if err := util.SaveJSONAtomic(filepath.Join(root, "restart.json"), map[string]int{"PID": os.Getpid()}); err != nil {
				return err
			}
			restarted <- struct{}{}
			return nil
		},
	}
	if phase == "current-owner" {
		cfg.Revision = restartFixtureRevision
		stop, err := agent.StartRestartControl(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		if err := util.SaveJSONAtomic(filepath.Join(root, "current-ready.json"), true); err != nil {
			t.Fatal(err)
		}
		waitRestartFixtureFile(t, root, "current-done.json")
		if beginCalls.Load() != 0 {
			t.Fatal("current replacement admitted another restart")
		}
		return
	}
	isolateSprintPersistence(t)
	oldSessions := sessionStore
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{})
	t.Cleanup(func() { sessionStore.Stop(); sessionStore = oldSessions })
	taskStore = dashboard.NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := taskStore.Create(dashboard.Task{ID: "restart-control", Title: "local fixture", Assignee: "fixture", Status: "approved"}); err != nil {
		t.Fatal(err)
	}
	started, stopAction := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(stopAction) })
	defer release()
	t.Cleanup(func() { release(); dashWorkerPool.Shutdown() })
	var count atomic.Int64
	name := fmt.Sprintf("OwnedRestartAction%d", admissionActionID.Add(1))
	engine.RegisterAction(name, func(ctx *btcore.BTContext[engine.Blackboard]) int {
		count.Add(1)
		close(started)
		<-stopAction
		ctx.Blackboard.Result = `{"status":"ok","result":"Completed real local restart-owner fixture action without provider invocation."}`
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: name}
	}}
	mux := inFlightMiddleware(dashboardMux("restart-fixture-key"))
	rr := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sprint/execute", strings.NewReader(`{}`))
	request.Header.Set("X-API-Key", "restart-fixture-key")
	mux.ServeHTTP(rr, request)
	if rr.Code != 200 {
		t.Fatalf("real sprint admission: %d %s", rr.Code, rr.Body.String())
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("local action did not start")
	}
	stop, err := agent.StartRestartControl(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if err := util.SaveJSONAtomic(filepath.Join(root, "busy-ready.json"), true); err != nil {
		t.Fatal(err)
	}
	waitRestartFixtureFile(t, root, "release.json")
	release()
	dashWorkerPool.Shutdown()
	waitSprintFixture(t)
	waitDashboardIdle(t)
	if err := util.SaveJSONAtomic(filepath.Join(root, "idle.json"), true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("target did not request own restart")
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if _, err := executeDashboardAgent(context.Background(), "fixture", "must not execute", "fixture"); err != errDashboardRestarting {
		t.Fatalf("sealed execution admitted: %v", err)
	}
	proof := map[string]any{"Status": rr.Code, "Admitted": rr.Header().Get("X-BT-Execution-Admitted"), "Count": count.Load(), "OwnerPID": os.Getpid()}
	if err := util.SaveJSONAtomic(filepath.Join(root, "proof.json"), proof); err != nil {
		t.Fatal(err)
	}
}
