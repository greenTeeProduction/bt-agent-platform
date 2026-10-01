package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDefinitionUsesDiskIdentityAcrossRegistries(t *testing.T) {
	dir := t.TempDir()
	first, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := Definition{Name: "auto-report", Description: "Read report\nand summarize.", Tree: "goal:report", Schedule: "0 9 * * *", Metadata: map[string]string{"user": "alice"}}
	inst, err := first.Create(expected)
	if err != nil {
		t.Fatal(err)
	}
	again, err := stale.EnsureDefinition(expected)
	if err != nil || again.ID != inst.ID {
		t.Fatalf("exact retry not idempotent: %+v %v", again, err)
	}
	for _, field := range []string{"user", "task", "tree", "schedule", "inputs"} {
		t.Run(field, func(t *testing.T) {
			different := cloneDefinition(expected)
			switch field {
			case "user":
				different.Metadata["user"] = "bob"
			case "task":
				different.Description += " changed"
			case "tree":
				different.Tree = "goal:other"
			case "schedule":
				different.Schedule = "* * * * *"
			case "inputs":
				different.Inputs = []InputSpec{{Name: "override"}}
			}
			if _, err := stale.EnsureDefinition(different); err == nil {
				t.Fatal("accepted conflicting definition")
			}
		})
	}
	before, err := os.ReadFile(filepath.Join(dir, expected.Name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	stale.instances = map[string]*Instance{}
	if _, err := stale.Create(expected); err == nil {
		t.Fatal("stale registry overwrote existing definition")
	}
	after, err := os.ReadFile(filepath.Join(dir, expected.Name+".yaml"))
	if err != nil || string(after) != string(before) {
		t.Fatal("failed creation changed definition")
	}
}
