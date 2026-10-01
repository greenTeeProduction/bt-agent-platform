package evolution

import (
	"encoding/json"
	"testing"
)

func TestTreeVersionRetainsPromptsAndRoundTrips(t *testing.T) {
	tree := &SerializableNode{Type: "ChainAction", Name: "llm_call", Metadata: map[string]any{"prompt": "complete this task", "max_tokens": 128}}
	version, err := TreeVersion(tree)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	var restored SerializableNode
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	got, err := TreeVersion(&restored)
	if err != nil || got != version {
		t.Fatalf("round trip changed version %q: %v", got, err)
	}
	restored.Metadata["prompt"] = "another task"
	got, _ = TreeVersion(&restored)
	if got == version {
		t.Fatal("prompt change did not change version")
	}
}

func TestExecutionEvidenceSeparatesCompilationFeedbackAndVersions(t *testing.T) {
	records := []Record{
		{TaskID: "compile", TreeName: "tree", User: "alice", TreeVersion: "v1", EvidenceKind: EvidenceCompilation},
		{TaskID: "feedback", TreeName: "tree", User: "alice", EvidenceKind: EvidenceFeedback, UserFeedback: FeedbackPositive},
		{TaskID: "run", TreeName: "tree", User: "alice", TreeVersion: "v1", EvidenceKind: EvidenceExecution, ExecutionVersions: []string{"v1"}},
		{TaskID: "old", TreeName: "tree", User: "alice", TreeVersion: "v0", EvidenceKind: EvidenceExecution, ExecutionVersions: []string{"v0"}},
		{TaskID: "other-owner", TreeName: "tree", User: "bob", TreeVersion: "v1", EvidenceKind: EvidenceExecution, ExecutionVersions: []string{"v1"}},
		{TaskID: "mixed", TreeName: "tree", User: "alice", TreeVersion: "v1", EvidenceKind: EvidenceExecution, ExecutionVersions: []string{"v1", "v2"}},
	}
	if got := ExecutionRecords(records); len(got) != 4 {
		t.Fatalf("synthetic evidence counted: %+v", got)
	}
	got := FilterByTreeVersion(records, "tree", "alice", "v1")
	if len(got) != 1 || got[0].TaskID != "run" {
		t.Fatalf("wrong version evidence: %+v", got)
	}
}
