package evolution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Store transaction tests use declared records, not LLM substitutes. The
// separate live gardener test is required to establish inference qualification.
func releaseFixture(t *testing.T) (*SerializableNode, *SerializableNode, *RuntimeQualification) {
	t.Helper()
	base := &SerializableNode{Type: "Sequence", Name: "factory:sum", Metadata: map[string]any{"user": "alice", "factory_kind": "response", "task": "Compute seventeen plus twenty-five."}, Children: []SerializableNode{{Type: "Condition", Name: "ValidateInput"}, {Type: "QualityGate", Name: "Sum", Metadata: map[string]any{"result_contract": json.RawMessage(`{"json_fields":{"total":42}}`)}, Children: []SerializableNode{{Type: "ChainAction", Name: "llm_call:Compute seventeen plus twenty-five and return JSON with wrong_total.", Metadata: map[string]any{"max_tokens": 128}}}}}}
	candidate := cloneTree(base)
	if ApplyMutations(candidate, []MutationOp{{Operation: "add_contract_recovery", Target: "Sum"}}) != 1 {
		t.Fatal("recovery mutation not applied")
	}
	bv, _ := TreeVersion(base)
	cv, _ := TreeVersion(candidate)
	q := &RuntimeQualification{TreeID: base.Name, User: "alice", BaselineVersion: bv, CandidateVersion: cv, Backend: "ollama", Model: "store-unit-fixture", BaselineCalls: 3, CandidateCalls: 6, MeasuredAt: time.Now().UTC()}
	for range 3 {
		q.Trials = append(q.Trials, TaskTrial{Task: "Compute seventeen plus twenty-five.", Contract: ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}, BeforeOutput: `{"wrong_total":42}`, BeforeOutcome: "failure", AfterOutput: `{"total":42}`, AfterOutcome: "success", BeforeDurationMs: 1, AfterDurationMs: 2})
	}
	return base, candidate, q
}

func TestRuntimeReleasePreservesDefinitionsAndRollsBack(t *testing.T) {
	base, candidate, q := releaseFixture(t)
	store := NewRuntimeReleaseStore(t.TempDir())
	release, err := store.Promote(t.Context(), base, candidate, q)
	if err != nil {
		t.Fatal(err)
	}
	loaded, active, err := store.Resolve(q.TreeID, q.User)
	if err != nil || active.Version != q.CandidateVersion || loaded == nil || release.Generation != 1 {
		t.Fatalf("bad activation: %+v %v", active, err)
	}
	if other, _, err := store.Resolve(q.TreeID, "bob"); err != nil || other != nil {
		t.Fatal("cross-owner release")
	}
	if _, err = store.Promote(t.Context(), base, candidate, q); err == nil {
		t.Fatal("stale predecessor replaced the active version")
	}
	rollback, err := store.Rollback(t.Context(), q.TreeID, q.User, release.Version, "regression investigation")
	if err != nil || rollback.Version != q.BaselineVersion || rollback.Generation != 2 {
		t.Fatalf("rollback=%+v err=%v", rollback, err)
	}
	loaded, _, err = store.Resolve(q.TreeID, q.User)
	if version, _ := TreeVersion(loaded); err != nil || version != q.BaselineVersion {
		t.Fatal("rollback did not restore exact predecessor")
	}
	dir, _ := store.directory(q.TreeID, q.User)
	if _, err = readVersion(dir, q.CandidateVersion); err != nil {
		t.Fatal("rollback discarded candidate evidence")
	}
}

func TestRuntimeReleaseRejectsInvalidEvidenceAndTampering(t *testing.T) {
	for _, change := range []func(*RuntimeQualification){
		func(q *RuntimeQualification) { q.BaselineVersion = "wrong" },
		func(q *RuntimeQualification) { q.User = "bob" },
		func(q *RuntimeQualification) { q.CandidateCalls = 0 },
		func(q *RuntimeQualification) { q.Trials = q.Trials[:1] },
		func(q *RuntimeQualification) { q.MeasuredAt = time.Now().Add(-time.Hour) },
		func(q *RuntimeQualification) { q.Trials[0].AfterOutput = `{"total":17}` },
		func(q *RuntimeQualification) {
			for i := range q.Trials {
				q.Trials[i].BeforeOutcome = "success"
				q.Trials[i].BeforeOutput = q.Trials[i].AfterOutput
			}
		},
	} {
		base, candidate, q := releaseFixture(t)
		change(q)
		if _, err := NewRuntimeReleaseStore(t.TempDir()).Promote(t.Context(), base, candidate, q); err == nil {
			t.Fatal("invalid qualification accepted")
		}
	}
	base, candidate, q := releaseFixture(t)
	store := NewRuntimeReleaseStore(t.TempDir())
	release, err := store.Promote(t.Context(), base, candidate, q)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := store.directory(q.TreeID, q.User)
	proof := filepath.Join(dir, "qualifications", release.Qualification+".json")
	if err = os.WriteFile(proof, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.Resolve(q.TreeID, q.User); err == nil {
		t.Fatal("tampered proof silently accepted")
	}
}

func TestRuntimeReleaseConcurrentPromotionAndBoundedLock(t *testing.T) {
	base, candidate, q := releaseFixture(t)
	store := NewRuntimeReleaseStore(t.TempDir())
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := store.Promote(context.Background(), base, candidate, q); results <- err })
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent commits=%d", wins)
	}
	dir, _ := store.directory(q.TreeID, q.User)
	unlock, err := reliability.AcquireFileLockWithContext(t.Context(), filepath.Join(dir, "active.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err = store.Rollback(ctx, q.TreeID, q.User, q.CandidateVersion, "test"); err == nil {
		t.Fatal("ignored lock deadline")
	}
}

func TestRuntimeReleaseRetainsRejectionWithoutChangingActiveVersion(t *testing.T) {
	base, candidate, q := releaseFixture(t)
	store := NewRuntimeReleaseStore(t.TempDir())
	release, err := store.Promote(t.Context(), base, candidate, q)
	if err != nil {
		t.Fatal(err)
	}
	q.Trials[0].AfterOutput = `{"total":37}`
	rejection := q.Validate(base, candidate)
	if rejection == nil {
		t.Fatal("incorrect output passed")
	}
	if err = store.RecordAttempt(q, rejection); err != nil {
		t.Fatal(err)
	}
	_, active, err := store.Resolve(q.TreeID, q.User)
	if err != nil || active == nil || active.Version != release.Version || active.Generation != release.Generation || active.Qualification != release.Qualification {
		t.Fatalf("active release changed: %+v %v", active, err)
	}
	dir, _ := store.directory(q.TreeID, q.User)
	paths, err := filepath.Glob(filepath.Join(dir, "attempts", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("rejection evidence missing: %v %v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var attempt struct {
		Qualification RuntimeQualification `json:"qualification"`
		Rejection     string               `json:"rejection"`
	}
	if err = json.Unmarshal(data, &attempt); err != nil || attempt.Rejection != rejection.Error() || attempt.Qualification.Trials[0].AfterOutput != q.Trials[0].AfterOutput {
		t.Fatalf("rejection output not retained: %s %v", data, err)
	}
}

func TestContractRecoveryMutationPreservesOracleAndIsBounded(t *testing.T) {
	base, candidate, _ := releaseFixture(t)
	t.Logf("governance before=%+v after=%+v", AssessGovernance(base), AssessGovernance(candidate))
	if !PreservesGovernance(base, candidate) || len(ContractRecoveryTargets(candidate)) != 0 {
		t.Fatal("recovery changed controls or remains repeatable")
	}
	if ApplyMutations(candidate, []MutationOp{{Operation: "add_contract_recovery", Target: "Sum"}}) != 0 {
		t.Fatal("unbounded recovery added")
	}
}
