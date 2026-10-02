package agentexec

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Transaction fixtures test resolver authority only. Real inference and measured
// improvement are exercised by the separate live factory-to-runtime test.
func TestRuntimeVersionResolvesSharedAndOwnedAuthority(t *testing.T) {
	t.Setenv("BT_REFLECTIONS_DIR", t.TempDir())
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	store, err := RuntimeReleaseStore()
	if err != nil {
		t.Fatal(err)
	}
	const id = "default"
	publish := func(user string) string {
		t.Helper()
		base := &evolution.SerializableNode{Type: "Sequence", Name: id, Metadata: map[string]any{"user": user}, Children: []evolution.SerializableNode{{Type: "AlwaysSucceed", Name: "fixture"}}}
		candidate := *base
		candidate.Description = "resolver transaction fixture"
		bv, _ := evolution.TreeVersion(base)
		cv, _ := evolution.TreeVersion(&candidate)
		q := &evolution.RuntimeQualification{TreeID: id, User: user, BaselineVersion: bv, CandidateVersion: cv, Backend: "ollama", Model: "store-unit-fixture", BaselineCalls: 3, CandidateCalls: 3, MeasuredAt: time.Now().UTC()}
		for range 3 {
			q.Trials = append(q.Trials, evolution.TaskTrial{Task: "resolver fixture", Contract: evolution.ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`42`)}}, BeforeOutcome: "failure", AfterOutcome: "success", AfterOutput: `{"total":42}`})
		}
		if _, err := store.Promote(t.Context(), base, &candidate, q); err != nil {
			t.Fatal(err)
		}
		return cv
	}
	shared := publish("")
	owned := publish("alice")
	for user, want := range map[string]string{"": shared, "bob": shared, "alice": owned} {
		tree, err := ResolveRuntimeVersion(user, id)
		version, _ := evolution.TreeVersion(tree)
		if err != nil || version != want {
			t.Fatalf("owner %q resolved %s; want %s: %v", user, version, want, err)
		}
	}
	root, _ := ReflectionsPath()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("alice\x00"+id)))
	if err := os.WriteFile(filepath.Join(root, "runtime-versions", key, "active.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRuntimeVersion("alice", id); err == nil {
		t.Fatal("corrupt personal authority fell back to shared runtime")
	}
}
