package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestSchedulerTerminalDeadLetterSurvivesRestart(t *testing.T) {
	if phase := os.Getenv("BT_DLQ_ORIGIN_PHASE"); phase != "" {
		root := os.Getenv("BT_DLQ_ORIGIN_ROOT")
		q := reliability.NewDeadLetterQueue(filepath.Join(root, "dlq.json"))
		if phase == "origin" {
			// The action happened before the scheduler received a terminal diagnostic.
			if err := os.WriteFile(filepath.Join(root, "actions"), []byte("action\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var executionErr error = &reliability.ExecutionUncertainError{Err: errors.New("fixture outcome lost")}
			if os.Getenv("BT_DLQ_ORIGIN_KIND") == "stopped" {
				executionErr = &reliability.ExecutionStoppedError{Outcome: "failure", Err: errors.New("fixture completed prefix")}
			}
			if err := q.PushExecutionFailureWithError(schedulerDeadLetter("fixture-agent", "fixture-task", executionErr, 1, "fixture-revision"), executionErr); err != nil {
				t.Fatal(err)
			}
		} else {
			entries := q.List()
			if len(entries) != 1 || !entries[0].RecoveryRequired {
				t.Fatal("origin disposition not preserved")
			}
			q.SetReplayExecutor(func(reliability.DeadLetterEntry) error {
				file, err := os.OpenFile(filepath.Join(root, "actions"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					return err
				}
				defer file.Close()
				_, err = file.WriteString("duplicate\n")
				return err
			})
			runDLQReplayScanOnce(q)
			if _, err := q.RequeueWithError(entries[0].ID); !errors.Is(err, reliability.ErrReplayRecovery) {
				t.Fatalf("requeue: %v", err)
			}
			if _, err := q.ReplayWithError(entries[0].ID); !errors.Is(err, reliability.ErrReplayRecovery) {
				t.Fatalf("direct replay: %v", err)
			}
		}
		return
	}
	for _, kind := range []string{"uncertain", "stopped"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"origin", "recover-one", "recover-two"} {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				cmd := exec.CommandContext(ctx, bin, "-test.run=^TestSchedulerTerminalDeadLetterSurvivesRestart$")
				cmd.Env = append(os.Environ(), "BT_DLQ_ORIGIN_ROOT="+root, "BT_DLQ_ORIGIN_PHASE="+phase, "BT_DLQ_ORIGIN_KIND="+kind)
				output, err := cmd.CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("%s: %v %s", phase, err, output)
				}
			}
			actions, err := os.ReadFile(filepath.Join(root, "actions"))
			if err != nil || string(actions) != "action\n" {
				t.Fatalf("replayed origin: %q %v", actions, err)
			}
		})
	}
}
