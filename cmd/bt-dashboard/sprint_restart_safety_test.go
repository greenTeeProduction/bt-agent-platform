package main

import (
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestSprintRestartDoesNotReplayUnrecordedAction(t *testing.T) {
	if root := os.Getenv("BT_SPRINT_RESTART_ROOT"); root != "" {
		runSprintRestartChild(t, root, os.Getenv("BT_SPRINT_RESTART_PHASE"), os.Getenv("BT_SPRINT_RESTART_FAULT"))
		return
	}
	for _, fault := range []string{"result-save", "exit-after-action"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			for _, phase := range []string{"execute", "restart", "restart"} {
				cmd := exec.Command(os.Args[0], "-test.run=^TestSprintRestartDoesNotReplayUnrecordedAction$", "-test.timeout=20s")
				cmd.Env = append(os.Environ(), "BT_SPRINT_RESTART_ROOT="+root, "BT_SPRINT_RESTART_PHASE="+phase, "BT_SPRINT_RESTART_FAULT="+fault, "BT_AGENT_HOME="+filepath.Join(root, "home"))
				out, err := cmd.CombinedOutput()
				if phase == "execute" {
					var exit *exec.ExitError
					if !errors.As(err, &exit) || exit.ExitCode() != 23 {
						t.Fatalf("execution did not reach controlled exit: %v\n%s", err, out)
					}
				} else if err != nil {
					t.Fatalf("restart: %v\n%s", err, out)
				}
			}
			data, err := os.ReadFile(filepath.Join(root, "actions.log"))
			if err != nil || string(data) != "action\n" {
				t.Fatalf("action repeated across processes: %q, %v", data, err)
			}
		})
	}
}

func runSprintRestartChild(t *testing.T, root, phase, fault string) {
	t.Helper()
	isolateSprintPersistence(t)
	path := filepath.Join(root, "tasks.json")
	if phase == "restart" && fault == "result-save" {
		// Restore result storage availability without touching admission bytes.
		if err := os.Remove(path + ".lock"); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	taskStore = dashboard.NewTaskStore(path)
	if phase == "execute" {
		if err := taskStore.Create(dashboard.Task{ID: "restart-owned", Title: "controlled action", Status: "approved", TreeID: "fixture", Assignee: "restart-fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	engine.RegisterAction("SprintRestartAction", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		f, err := os.OpenFile(filepath.Join(root, "actions.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			panic(err)
		}
		_, err = f.WriteString("action\n")
		_ = f.Close()
		if err != nil {
			panic(err)
		}
		if phase == "execute" && fault == "exit-after-action" {
			os.Exit(23)
		}
		ctx.Blackboard.Result = `{"status":"ok","result":"Completed durable fixture side effect before the task result save failed."}`
		if phase == "execute" {
			if err := os.Remove(path + ".lock"); err != nil && !os.IsNotExist(err) {
				panic(err)
			}
			if err := os.Mkdir(path+".lock", 0700); err != nil {
				panic(err)
			}
		}
		return 1
	})
	dashAgentRunner = &agent.RunDeps{Blackboards: blackboard.DefaultManager(), ResolveTree: func(string) *evolution.SerializableNode {
		return &evolution.SerializableNode{Type: "Action", Name: "SprintRestartAction"}
	}}
	rr := sprintFixtureHTTP(t, http.MethodPost, "/api/sprint/execute")
	if rr.Code != 200 {
		t.Fatalf("sprint HTTP %d: %s", rr.Code, rr.Body.String())
	}
	waitSprintFixture(t)
	if phase == "execute" {
		sprintState.Lock()
		valid := len(sprintState.Diagnostics) == 1 && !sprintState.Diagnostics[0].TaskCommitted && sprintState.Diagnostics[0].Outcome == "success"
		diagnostics := append([]sprintTaskDiagnostic(nil), sprintState.Diagnostics...)
		sprintState.Unlock()
		if !valid {
			t.Fatalf("did not observe completed action with failed result recording: %+v", diagnostics)
		}
		os.Exit(23) // lose all in-process diagnostics, without repair or cleanup
	}
	if !strings.Contains(rr.Body.String(), "no_approved_tasks") {
		t.Fatalf("restart admitted unresolved task: %s", rr.Body.String())
	}
	if got, ok := taskStore.Get("restart-owned"); !ok || got.Status != "in_progress" {
		t.Fatalf("durable unresolved claim lost: %+v", got)
	}
	if err := taskStore.Approve("restart-owned", "fixture-operator"); !errors.Is(err, dashboard.ErrTaskInvalidStatus) {
		t.Fatalf("ordinary approval released an unresolved claim: %v", err)
	}
}
