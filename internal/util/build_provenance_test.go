package util

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildProvenanceRequiresNativeCleanMetadata(t *testing.T) {
	base := debug.BuildInfo{Main: debug.Module{Path: "github.com/nico/go-bt-evolve"}, Settings: []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: strings.Repeat("a", 40)}, {Key: "vcs.modified", Value: "false"}}}
	if !BuildProvenanceFromInfo(&base).QualifiesCodeIdentity() {
		t.Fatal("complete clean native metadata rejected")
	}
	for name, change := range map[string]func(*debug.BuildInfo){
		"dirty":         func(b *debug.BuildInfo) { b.Settings[2].Value = "true" },
		"unknown-dirty": func(b *debug.BuildInfo) { b.Settings = b.Settings[:2] },
		"display-stamp": func(b *debug.BuildInfo) {
			b.Settings = []debug.BuildSetting{{Key: "-ldflags", Value: "-X revision=" + strings.Repeat("a", 40)}}
		},
		"other-module": func(b *debug.BuildInfo) { b.Main.Path = "example.com/other" },
		"local-replacement": func(b *debug.BuildInfo) {
			b.Deps = []*debug.Module{{Path: "example.com/dep", Replace: &debug.Module{Path: "../dep"}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := base
			b.Settings = append([]debug.BuildSetting(nil), base.Settings...)
			change(&b)
			if BuildProvenanceFromInfo(&b).QualifiesCodeIdentity() {
				t.Fatal("unqualified build accepted")
			}
		})
	}
}

func TestBuildProvenanceReadsRealBuiltExecutable(t *testing.T) {
	root := t.TempDir()
	run := func(name string, args ...string) string {
		t.Helper()
		c := exec.CommandContext(t.Context(), name, args...)
		c.Dir = root
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v %s", name, args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	for name, body := range map[string]string{"go.mod": "module github.com/nico/go-bt-evolve\n\ngo 1.26.5\n", "main.go": "package main\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-q")
	run("git", "config", "user.name", "Build provenance fixture")
	run("git", "config", "user.email", "build@example.invalid")
	run("git", "config", "core.hooksPath", "/dev/null")
	run("git", "add", ".")
	run("git", "-c", "commit.gpgsign=false", "commit", "-qm", "metadata fixture")
	revision := run("git", "rev-parse", "HEAD")
	bin := filepath.Join(t.TempDir(), "fixture")
	toolchainRoot := run("go", "env", "GOROOT")
	build := func() BuildProvenance {
		t.Helper()
		run(filepath.Join(toolchainRoot, "bin", "go"), "build", "-buildvcs=true", "-o", bin, ".")
		bi, err := buildinfo.ReadFile(bin)
		if err != nil {
			t.Fatal(err)
		}
		return BuildProvenanceFromInfo(bi)
	}
	clean := build()
	if !clean.QualifiesCodeIdentity() || clean.Revision != revision {
		t.Fatalf("wrong native metadata: %+v", clean)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() { println(\"modified\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dirty := build()
	if !dirty.Known || !dirty.Dirty || dirty.QualifiesCodeIdentity() {
		t.Fatalf("dirty binary accepted: %+v", dirty)
	}
}
