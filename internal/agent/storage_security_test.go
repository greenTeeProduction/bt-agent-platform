package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityRegistryNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../outside", "a/b", `a\b`, "/absolute", `C:\absolute`, "bad name", "bad:name", "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agents")
			reg, err := NewRegistry(dir)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(filepath.Dir(dir), "outside.yaml")
			if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Create(Definition{Name: name}); err == nil {
				t.Errorf("Create accepted %q", name)
			}
			if err := reg.saveDef(Definition{Name: name}); err == nil {
				t.Errorf("saveDef accepted %q", name)
			}
			reg.instances[name] = &Instance{Definition: Definition{Name: name}}
			if err := reg.Delete(name); err == nil {
				t.Errorf("Delete accepted %q", name)
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "unchanged" {
				t.Errorf("outside changed: %q %v", b, err)
			}
		})
	}
}
func TestSecurityRegistrySymlinksAndValidNames(t *testing.T) {
	for _, op := range []string{"create", "save", "delete", "load"} {
		t.Run(op, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "agents")
			reg, err := NewRegistry(dir)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside.yaml")
			original := []byte("name: linked\ntree: unused\n")
			if err := os.WriteFile(outside, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "linked.yaml")); err != nil {
				t.Fatal(err)
			}
			switch op {
			case "create":
				_, err = reg.Create(Definition{Name: "linked"})
			case "save":
				err = reg.saveDef(Definition{Name: "linked"})
			case "delete":
				reg.instances["linked"] = &Instance{}
				err = reg.Delete("linked")
			case "load":
				err = reg.ReloadFromDisk()
				if _, getErr := reg.Get("linked"); getErr == nil {
					t.Error("loaded symlink outside registry")
				}
			}
			if op != "load" && err == nil {
				t.Error("symlink operation accepted")
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != string(original) {
				t.Errorf("outside changed: %q %v", b, err)
			}
		})
	}
	for _, name := range []string{"code-reviewer", "Agent_2", "agent.v2", "123", "équipe"} {
		reg, err := NewRegistry(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Create(Definition{Name: name}); err != nil {
			t.Fatal(err)
		}
		if err := reg.Delete(name); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(reg.dir, name+".yaml")); !os.IsNotExist(err) {
			t.Errorf("definition still exists: %v", err)
		}
	}
}

func TestSecurityRegistryIgnoresInvalidStoredName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "innocent.yaml"), []byte("name: ../outside\ntree: unused\n"), 0644); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.List()) != 0 {
		t.Fatal("invalid name loaded from YAML")
	}
}

func TestSecurityRegistryWritePreservesHardlinkTarget(t *testing.T) {
	base := t.TempDir()
	reg, err := NewRegistry(filepath.Join(base, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.yaml")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(reg.dir, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "linked"}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "unchanged" {
		t.Fatalf("hardlink target changed: %q %v", got, err)
	}
}
