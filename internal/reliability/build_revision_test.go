package reliability

import (
	"encoding/json"
	"testing"
)

// Milestone 1 (program 94b0b31, "deploy-drift loop"): a dead letter records the
// build revision of the process that failed, so a drift diagnosis can tell
// "this failed on a stale binary" from "this failed on current code". The field
// is optional and must round-trip through the DLQ's JSON persistence.
func TestDeadLetterEntry_BuildRevisionRoundTrips(t *testing.T) {
	e := DeadLetterEntry{ID: "agent-1", Task: "t", BuildRevision: "8105ae2deadbeef"}

	blob, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got DeadLetterEntry
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.BuildRevision != "8105ae2deadbeef" {
		t.Fatalf("BuildRevision = %q, want %q", got.BuildRevision, "8105ae2deadbeef")
	}

	// Unset revision omits the field (omitempty) — old entries stay clean.
	if blob, _ := json.Marshal(DeadLetterEntry{ID: "x"}); string(blob) == "" ||
		contains(string(blob), "build_revision") {
		t.Fatalf("empty BuildRevision must be omitted from JSON, got %s", blob)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
