package evolution

import (
	"encoding/json"
	"testing"
)

func TestFileTaskContractAndGovernancePreservation(t *testing.T) {
	for _, path := range []string{"", "/tmp/report", "../report", "a/../report", ".", "a\\b", "a//b"} {
		if (FileTaskSpec{Output: path}).Validate() == nil {
			t.Fatalf("unsafe destination accepted: %q", path)
		}
	}
	base := &SerializableNode{Type: "FileTask", Name: "report", Metadata: map[string]any{"user": "alice", "task": "Create report", "file_task": FileTaskSpec{Output: "reports/summary.json"}, "result_contract": json.RawMessage(`{"json_fields":{"total":25}}`)}, Children: []SerializableNode{{Type: "ChainAction", Name: "llm_call:Calculate the total", Metadata: map[string]any{"max_tokens": 256}}}}
	if _, err := ParseFileTask(base); err != nil {
		t.Fatal(err)
	}
	if !PreservesGovernance(base, base) {
		t.Fatal("unchanged file controls rejected")
	}
	if PreservesGovernance(base, &base.Children[0]) {
		t.Fatal("removing file effect retained governance")
	}
	clone := *base
	clone.Metadata = map[string]any{"user": "alice", "task": "Create report", "file_task": FileTaskSpec{Output: "another.json"}, "result_contract": json.RawMessage(`{"json_fields":{"total":25}}`)}
	if PreservesGovernance(base, &clone) {
		t.Fatal("redirected output retained governance")
	}
	clone.Metadata["file_task"] = map[string]any{"output": "reports/summary.json", "ignored_typo": true}
	if _, err := ParseFileTask(&clone); err == nil {
		t.Fatal("unknown file task key accepted")
	}
}
