package gardener

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func collapsedRecoveryTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "WrongTree_Main", Children: []evolution.SerializableNode{
		{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "WasSuccessful"},
			{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect"}}},
			{Type: "Action", Name: "EscalateToDeepSeek"},
		}},
	}}
}

func TestRecoverCollapsedBuiltinsPreservesEvidenceAndReloads(t *testing.T) {
	dir := t.TempDir()
	catalog := &Registry{}
	catalog.addCatalogBuiltins()
	bad, _ := json.Marshal(collapsedRecoveryTree())
	for _, e := range catalog.entries {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(e.FilePath)), bad, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Unknown and legacy active files are outside the exact catalog mapping.
	for _, name := range []string{"tree-custom.json", "tree.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), bad, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewRegistry(dir)
	for _, e := range reg.List() {
		if e.Name != "custom" && (e.Active || !e.RecoveryRequired) {
			t.Fatalf("collapsed builtin admitted: %s", e.Name)
		}
	}
	plan, err := PlanTreeRecovery(dir, "test-authored-revision")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != len(catalog.entries) {
		t.Fatalf("incomplete mapping: %d/%d", len(plan.Items), len(catalog.entries))
	}
	for _, item := range plan.Items {
		data, _ := os.ReadFile(filepath.Join(dir, item.File))
		if !bytes.Equal(data, bad) {
			t.Fatal("planning wrote state")
		}
	}
	if err := ApplyTreeRecovery(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Applied || plan.Restored != len(catalog.entries) {
		t.Fatalf("missing recovery acknowledgement: %+v", plan)
	}
	for _, item := range plan.Items {
		data, err := os.ReadFile(item.Backup)
		if err != nil || !bytes.Equal(data, bad) {
			t.Fatalf("original bytes lost: %s %v", item.File, err)
		}
	}
	reloaded := NewRegistry(dir)
	for _, e := range reloaded.List() {
		if e.Name == "custom" {
			continue
		}
		if !e.Active || e.RecoveryRequired || outcomeRecoveryOnly(e.Tree) {
			t.Fatalf("recovery not adopted by registry: %s", e.Name)
		}
		var expected *evolution.SerializableNode
		for _, builtin := range catalog.entries {
			if builtin.Name == e.Name {
				expected = builtin.Tree
				break
			}
		}
		actual, _ := evolution.TreeVersion(e.Tree)
		wanted, _ := evolution.TreeVersion(expected)
		if actual != wanted {
			t.Fatalf("wrong authored definition for %s", e.Name)
		}
	}
	for _, name := range []string{"tree-custom.json", "tree.json"} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		if !bytes.Equal(data, bad) {
			t.Fatalf("unmapped tree overwritten: %s", name)
		}
	}
	again, err := PlanTreeRecovery(dir, "test-authored-revision")
	if err != nil || len(again.Items) != 0 {
		t.Fatalf("recovery is not idempotent: %+v %v", again, err)
	}
}

func TestRecoveryRefusesStalePlanAndBoundedLock(t *testing.T) {
	dir := t.TempDir()
	bad, _ := json.Marshal(collapsedRecoveryTree())
	path := filepath.Join(dir, "tree-default.json")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTreeRecovery(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"type":"Action","name":"ValidateInput"}`)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyTreeRecovery(t.Context(), plan); err == nil {
		t.Fatal("stale plan overwrote newer state")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, changed) {
		t.Fatal("changed file lost")
	}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = PlanTreeRecovery(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := reliability.AcquireFileLockWithContext(t.Context(), filepath.Join(dir, ".tree-recovery"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := ApplyTreeRecovery(ctx, plan); err == nil {
		t.Fatal("recovery ignored lock deadline")
	}
	data, _ = os.ReadFile(path)
	if !bytes.Equal(data, bad) {
		t.Fatal("blocked recovery wrote state")
	}
}

func TestRecoveryRetiresRemovedTreeAndRefusesChangedPlan(t *testing.T) {
	dir := t.TempDir()
	name := "tree-domain_arc42:assemble.json"
	bad, _ := json.Marshal(collapsedRecoveryTree())
	if err := os.WriteFile(filepath.Join(dir, name), bad, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTreeRecovery(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Operation != "quarantine_retired" {
		t.Fatalf("removed workflow was not recognized: %+v", plan)
	}
	plan.Items[0].File = "tree-default.json"
	if err := ApplyTreeRecovery(t.Context(), plan); err == nil {
		t.Fatal("altered plan admitted")
	}
	plan, err = PlanTreeRecovery(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyTreeRecovery(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if plan.Restored != 0 || plan.Quarantined != 1 {
		t.Fatal("retirement claimed restoration")
	}
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Fatal("retired tree remains discoverable")
	}
	for _, path := range []string{plan.Items[0].Backup, plan.Items[0].Archived} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, bad) {
			t.Fatalf("retired evidence lost: %v", err)
		}
	}
}

func TestRecoveryBackupFailurePreservesOriginal(t *testing.T) {
	dir := t.TempDir()
	bad, _ := json.Marshal(collapsedRecoveryTree())
	path := filepath.Join(dir, "tree-default.json")
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "recovery"), []byte("blocked backup parent"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTreeRecovery(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyTreeRecovery(t.Context(), plan); err == nil {
		t.Fatal("write proceeded without a backup")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, bad) || plan.Restored != 0 {
		t.Fatal("backup failure lost original")
	}
}
