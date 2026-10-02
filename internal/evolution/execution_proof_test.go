package evolution

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutionProofRecomputesFinalOracle(t *testing.T) {
	version := "sha256:" + strings.Repeat("a", 64)
	r := Record{TaskID: "task", RunID: "run", TreeName: "tree", TreeVersion: version, ExecutionVersions: []string{version}, EvidenceKind: EvidenceExecution, StartedAt: time.Now().Add(-time.Second), Timestamp: time.Now().UnixMilli(), Outcome: Success, Result: `{"total":42}`}
	r.ResultChecks = []ResultCheck{{Passed: true, OutputDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(r.Result))), Contract: &ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}}}
	if !r.VerifiedFinalResult() {
		t.Fatal("valid executed oracle rejected")
	}
	for name, change := range map[string]func(*Record){
		"wrong-result":     func(r *Record) { r.Result = `{"total":41}` },
		"failed":           func(r *Record) { r.Outcome = Failure },
		"legacy":           func(r *Record) { r.EvidenceKind = "" },
		"feedback":         func(r *Record) { r.UserFeedback = "positive" },
		"mixed-version":    func(r *Record) { r.ExecutionVersions = append(r.ExecutionVersions, "other") },
		"flag-only":        func(r *Record) { r.ResultChecks = []ResultCheck{{Passed: true}} },
		"uncertain-effect": func(r *Record) { r.Effects = []EffectReceipt{{WriteCommitted: true, Verified: false}} },
		"missing-checks":   func(r *Record) { r.ResultChecksDropped = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			record := r
			change(&record)
			if record.VerifiedFinalResult() {
				t.Fatal("unqualified result accepted")
			}
		})
	}
}

func TestStrictReflectionReadRejectsCorruptionAndIdentityMismatch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir(), "reflection-expected.json")
	for _, body := range []string{"", "{broken", `{"task_id":"other"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadAllStrict(); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}
