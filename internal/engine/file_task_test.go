package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Filesystem lifecycle fixtures do not evaluate model quality. The end-to-end
// automation benchmark uses RealLLM and the production task factory/runner.
func TestFileTaskRejectsWrongOutputAndChangedInputBeforeWriting(t *testing.T) {
	for _, mode := range []string{"wrong_result", "changed_input", "valid"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			prev := ArtifactRootFn
			ArtifactRootFn = func(string) (string, error) { return root, nil }
			t.Cleanup(func() { ArtifactRootFn = prev })
			if err := os.WriteFile(filepath.Join(root, "input.json"), []byte(`{"a":17,"b":25}`), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			name := "FileTaskFilesystemFixture"
			previous := actionRegistry[name]
			actionRegistry[name] = func(ctx *btcore.BTContext[Blackboard]) int {
				calls++
				ctx.Blackboard.Result = `{"total":42}`
				if mode == "wrong_result" {
					ctx.Blackboard.Result = `{"total":0}`
				}
				if mode == "changed_input" {
					if err := os.WriteFile(filepath.Join(root, "input.json"), []byte(`{"a":99}`), 0600); err != nil {
						t.Error(err)
					}
				}
				return 1
			}
			t.Cleanup(func() {
				if previous == nil {
					delete(actionRegistry, name)
				} else {
					actionRegistry[name] = previous
				}
			})
			tree := &evolution.SerializableNode{Type: "FileTask", Name: "file-task", Metadata: map[string]any{"user": "alice", "task": "Compute the total", "file_task": evolution.FileTaskSpec{Input: "input.json", Output: "reports/total.json"}, "result_contract": json.RawMessage(`{"json_fields":{"total":42}}`)}, Children: []evolution.SerializableNode{{Type: "Action", Name: name}}}
			bb := &Blackboard{User: "alice", Task: "Compute the total"}
			command, err := BuildAndValidate(tree, bb)
			if err != nil {
				t.Fatal(err)
			}
			RunTask(bb, command)
			receipts := bb.EvidenceEffects()
			data, err := os.ReadFile(filepath.Join(root, "reports/total.json"))
			if mode != "valid" {
				if err == nil || bb.Outcome == "success" || len(receipts) != 0 {
					t.Fatalf("invalid task wrote output: %s %s %+v", data, bb.Outcome, receipts)
				}
				return
			}
			if err != nil || bb.Outcome != "success" || len(receipts) != 1 || !receipts[0].Verified || !receipts[0].WriteCommitted {
				t.Fatalf("missing verified effect: %s %s %+v %v", data, bb.Outcome, receipts, err)
			}
			command.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
			if calls != 1 || len(bb.EvidenceEffects()) != 1 {
				t.Fatal("same-run tick replayed completed work")
			}
		})
	}
}

func TestArtifactReadbackRejectsMissingWrongAndEscapingFiles(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	spec := evolution.FileTaskSpec{Output: "result.json"}
	contract := &evolution.ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}
	expected := []byte("{\"total\":42}\n")
	if verifyTaskArtifact(root, spec, expected, contract) == nil {
		t.Fatal("missing effect accepted")
	}
	if err := writeTaskArtifact(root, spec.Output, []byte(`{"total":0}`)); err != nil {
		t.Fatal(err)
	}
	if verifyTaskArtifact(root, spec, expected, contract) == nil {
		t.Fatal("different effect accepted")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := writeTaskArtifact(root, "escape/result.json", expected); err == nil {
		t.Fatal("write escaped artifact root")
	}
	if _, err := readTaskArtifact(root, "escape/result.json"); err == nil {
		t.Fatal("read escaped artifact root")
	}
	if err := writeTaskArtifact(root, spec.Output, expected); err != nil {
		t.Fatal(err)
	}
	if err := verifyTaskArtifact(root, spec, expected, contract); err != nil {
		t.Fatal(err)
	}
}
