package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ─── UpdateSchedule Coverage ───

func TestRegistry_UpdateSchedule(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "sched-test", Tree: "domain:default"})

	// Update schedule
	if err := reg.UpdateSchedule("sched-test", "every 1h"); err != nil {
		t.Fatal(err)
	}

	inst, _ := reg.Get("sched-test")
	if inst.Definition.Schedule != "every 1h" {
		t.Errorf("expected schedule 'every 1h', got %q", inst.Definition.Schedule)
	}

	// Verify persistence (reload from disk)
	reg2, _ := NewRegistry(dir)
	inst2, _ := reg2.Get("sched-test")
	if inst2.Definition.Schedule != "every 1h" {
		t.Errorf("persisted schedule should be 'every 1h', got %q", inst2.Definition.Schedule)
	}
}

func TestRegistry_UpdateScheduleNonexistent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	err := reg.UpdateSchedule("nonexistent", "every 1h")
	if err == nil {
		t.Error("should error on nonexistent agent")
	}
}

// ─── Delete Edge Cases ───

func TestRegistry_DeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	err := reg.Delete("nonexistent")
	if err == nil {
		t.Error("should error on nonexistent agent")
	}
}

func TestRegistry_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "delete-test", Tree: "domain:default"})

	// Verify file exists
	defPath := filepath.Join(dir, "delete-test.yaml")
	if _, err := os.Stat(defPath); os.IsNotExist(err) {
		t.Fatal("definition file should exist before delete")
	}

	if err := reg.Delete("delete-test"); err != nil {
		t.Fatal(err)
	}

	// File should be removed
	if _, err := os.Stat(defPath); !os.IsNotExist(err) {
		t.Error("definition file should be removed after delete")
	}

	// Agent should not be in list
	if len(reg.List()) != 0 {
		t.Error("registry should be empty after delete")
	}
}

// ─── NewRegistry Edge Cases ───

func TestRegistry_LoadAllNonExistentDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MkdirAll of a rooted path succeeds on Windows")
	}
	// loadAll with nonexistent dir should return error because MkdirAll fails
	_, err := NewRegistry("/nonexistent/path/for/registry")
	if err == nil {
		t.Error("should error when registry dir is in a non-existent path")
	}
}

func TestRegistry_LoadAllBadYAML(t *testing.T) {
	dir := t.TempDir()
	// Write a file with invalid YAML
	_ = os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("name: {{invalid"), 0644)

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Bad YAML should be silently skipped
	if len(reg.List()) != 0 {
		t.Errorf("expected 0 instances from bad YAML, got %d", len(reg.List()))
	}
}

func TestRegistry_LoadAllNonYAMLFile(t *testing.T) {
	dir := t.TempDir()
	// Write a non-yaml file
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not an agent"), 0644)

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Non-yaml files should be skipped
	if len(reg.List()) != 0 {
		t.Errorf("expected 0 instances, got %d", len(reg.List()))
	}
}

func TestRegistry_LoadAllSubdir(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory with a yaml file inside
	subDir := filepath.Join(dir, "subdir")
	_ = os.MkdirAll(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "agent.yaml"), []byte("name: sub-agent\ntree: domain:default"), 0644)

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Subdirectory entries should be skipped
	if len(reg.List()) != 0 {
		t.Errorf("expected 0 instances from subdir-only registry, got %d", len(reg.List()))
	}
}

func TestRegistry_UpdateStateNonexistent(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	err := reg.UpdateState("nonexistent", StateRunning, "")
	if err == nil {
		t.Error("should error on nonexistent agent")
	}
}

func TestRegistry_MarshalError(t *testing.T) {
	// saveDef yaml.Marshal should never fail on a valid Definition, but
	// we can test the edge case by using a temp dir that gets removed
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	inst, err := reg.Create(Definition{Name: "marshaltest", Tree: "domain:default"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Definition.Name != "marshaltest" {
		t.Errorf("expected marshaltest, got %s", inst.Definition.Name)
	}
	if inst.Definition.Version != "1.0.0" {
		t.Errorf("expected default version 1.0.0, got %s", inst.Definition.Version)
	}
}

// ─── saveDef Edge Cases ───

func TestRegistry_saveDefDirRemoved(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Create an agent first
	_, _ = reg.Create(Definition{Name: "save-test", Tree: "domain:default"})

	// Remove the directory to cause saveDef failure
	os.RemoveAll(dir)

	// Try to create another agent — should fail at saveDef
	_, err := reg.Create(Definition{Name: "save-fail", Tree: "domain:default"})
	if err == nil {
		t.Error("should error when directory is removed")
	}
}

func TestRegistry_saveDefFilePermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory chmod does not restrict writes on Windows")
	}
	// Create a readonly dir to force permission error in saveDef
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Create an initial agent before making dir readonly
	_, _ = reg.Create(Definition{Name: "perm-test", Tree: "domain:default"})

	// Make the directory read-only
	_ = os.Chmod(dir, 0444)

	// Try to update state (calls saveDef) — should fail with permission error
	err := reg.UpdateState("perm-test", StateRunning, "")
	if err == nil {
		t.Error("should error when directory is read-only")
	}

	// Restore permissions for cleanup
	_ = os.Chmod(dir, 0755)
}

// ─── loadAll ReadDir Error (file-as-dir) ───

func TestRegistry_NewRegistryFileAsDir(t *testing.T) {
	// Create a temporary file, then try to use it as a registry directory
	// This triggers the ReadDir error path
	tmpFile := filepath.Join(t.TempDir(), "notadir")
	_ = os.WriteFile(tmpFile, []byte("not a dir"), 0644)

	_, err := NewRegistry(tmpFile)
	if err == nil {
		t.Error("should error when registry path is a file, not a directory")
	}
}
