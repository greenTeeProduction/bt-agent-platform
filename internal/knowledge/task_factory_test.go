package knowledge_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func taskFactory() *knowledge.Factory {
	f := knowledge.NewFactory(nil)
	f.Validate = func(tree *evolution.SerializableNode) error {
		info := engine.ValidateTreeFull(tree)
		if !info.Valid() {
			return fmt.Errorf("%v", info.Errors)
		}
		return nil
	}
	return f
}

func TestTaskFactoryBuildsUnpublishedGovernedWork(t *testing.T) {
	f := taskFactory()
	request := knowledge.TaskRequest{Task: strings.Repeat("Use the provided input carefully. ", 8) + "Calculate seventeen plus twenty-five.", User: "alice", ResultContract: json.RawMessage(`{"json_fields":{"total":42},"required_keys":["explanation"]}`), Steps: []knowledge.TaskStep{{Instruction: "Extract the two input numbers.", ResultContract: json.RawMessage(`{"json_fields":{"first":17,"second":25}}`)}}}
	tree, id, err := f.BuildTask(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Graph.Trees) != 0 {
		t.Fatal("unpersisted draft became discoverable")
	}
	if tree.Name != id || tree.Metadata["user"] != "alice" || len(tree.Children) != 3 {
		t.Fatalf("bad task tree: %+v", tree)
	}
	for _, gate := range tree.Children[1:] {
		if gate.Type != "QualityGate" || len(gate.Children) != 2 {
			t.Fatalf("unbounded or absent gate: %+v", gate)
		}
		for _, worker := range gate.Children {
			if !strings.Contains(worker.Name, request.Task) || strings.Contains(worker.Name, "42") || worker.Metadata["max_tokens"] != 2048 {
				t.Fatalf("truncated task, leaked oracle or insufficient budget: %+v", worker)
			}
		}
	}
	_, second, err := f.BuildTask(request)
	if err != nil || second == id {
		t.Fatalf("tree identity collision: %s %v", second, err)
	}
}

func TestTaskFactoryRejectsMissingControls(t *testing.T) {
	for _, raw := range []string{"", `null`, `{}`, `{"min_length":10}`, `{"required_keys":[""]}`, `{"typo":true}`} {
		f := taskFactory()
		if _, _, err := f.BuildTask(knowledge.TaskRequest{Task: "Summarize input", ResultContract: json.RawMessage(raw)}); err == nil {
			t.Fatalf("accepted invalid contract %s", raw)
		}
		if len(f.Graph.Trees) != 0 {
			t.Fatal("invalid task registered")
		}
	}
	f := taskFactory()
	f.Validate = nil
	if _, _, err := f.BuildTask(knowledge.TaskRequest{Task: "Summarize", ResultContract: json.RawMessage(`{"required_keys":["summary"]}`)}); err == nil {
		t.Fatal("accepted missing validator")
	}
}
