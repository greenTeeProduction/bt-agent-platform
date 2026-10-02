package agent

import (
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/util"
)

func rebuildGitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":  "module github.com/nico/go-bt-evolve\n\ngo 1.26.5\n",
		"main.go": "package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"committed\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rebuildGit(t, root, "init", "-q")
	rebuildGit(t, root, "config", "user.name", "Native rebuild fixture")
	rebuildGit(t, root, "config", "user.email", "build@example.invalid")
	rebuildGit(t, root, "config", "core.hooksPath", "/dev/null")
	rebuildGit(t, root, "add", ".")
	rebuildGit(t, root, "-c", "commit.gpgsign=false", "commit", "-qm", "committed input")
	return root, rebuildGit(t, root, "rev-parse", "HEAD")
}

func rebuildGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir, cmd.Env = dir, scrubGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRebuildNativeProvenanceAcrossRepositoryLayouts(t *testing.T) {
	for _, layout := range []string{"ordinary", "bare", "linked"} {
		t.Run(layout, func(t *testing.T) {
			root, revision := rebuildGitFixture(t)
			source := root
			switch layout {
			case "bare":
				source = filepath.Join(t.TempDir(), "bare.git")
				rebuildGit(t, root, "clone", "--bare", "--quiet", root, source)
			case "linked":
				source = filepath.Join(t.TempDir(), "linked")
				rebuildGit(t, root, "worktree", "add", "--detach", source, revision)
			}
			if layout != "bare" {
				if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("uncommitted and deliberately unbuildable"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := rebuildGit(t, source, "worktree", "list", "--porcelain")
			binary := filepath.Join(t.TempDir(), "fixture")
			if err := RebuildBinaries(source, []RebuildTarget{{Name: "fixture", Pkg: ".", OutPath: binary}}); err != nil {
				t.Fatal(err)
			}
			info, err := buildinfo.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			proof := util.BuildProvenanceFromInfo(info)
			if !proof.QualifiesCodeIdentity() || proof.Revision != revision || proof.CommitTime == "" {
				t.Fatalf("automatic rebuild lost native identity: %+v", proof)
			}
			out, err := exec.CommandContext(t.Context(), binary).CombinedOutput()
			if err != nil || string(out) != "committed\n" {
				t.Fatalf("rebuilt uncommitted input: %q %v", out, err)
			}
			if after := rebuildGit(t, source, "worktree", "list", "--porcelain"); after != before {
				t.Fatal("rebuild altered source worktree registrations")
			}
			if layout != "bare" {
				data, _ := os.ReadFile(filepath.Join(source, "main.go"))
				if string(data) != "uncommitted and deliberately unbuildable" {
					t.Fatal("rebuild modified the source working tree")
				}
			}
		})
	}
}

func TestRebuildNativeBuildScrubsInheritedGitEnvironment(t *testing.T) {
	root, revision := rebuildGitFixture(t)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "missing.git"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	binary := filepath.Join(t.TempDir(), "fixture")
	if err := RebuildBinaries(root, []RebuildTarget{{Name: "fixture", Pkg: ".", OutPath: binary}}); err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if proof := util.BuildProvenanceFromInfo(info); !proof.QualifiesCodeIdentity() || proof.Revision != revision {
		t.Fatalf("inherited Git environment redirected build identity: %+v", proof)
	}
}

func TestRebuildNativeBuildRejectsDirtyCheckoutBeforeReplacement(t *testing.T) {
	root, _ := rebuildGitFixture(t)
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	prior := rebuildMaterializeFn
	rebuildMaterializeFn = func(string) (string, func(), error) { return root, func() {}, nil }
	t.Cleanup(func() { rebuildMaterializeFn = prior })
	binary := filepath.Join(t.TempDir(), "deployed")
	if err := os.WriteFile(binary, []byte("previous executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := RebuildBinaries(root, []RebuildTarget{{Name: "fixture", Pkg: ".", OutPath: binary}}); err == nil {
		t.Fatal("dirty executable replaced a deployed binary")
	}
	data, _ := os.ReadFile(binary)
	if string(data) != "previous executable" {
		t.Fatal("failed provenance check changed the deployed executable")
	}
}

func TestRebuildTargetsUseInstalledCLIPath(t *testing.T) {
	for _, target := range DefaultRebuildTargets("/repo") {
		if target.Name == "bt-agent-cli" {
			if target.OutPath != filepath.Join("/repo", "bin", "bt-agent-cli") {
				t.Fatal("automatic rebuild leaves the installed CLI stale")
			}
			return
		}
	}
	t.Fatal("CLI is missing from the release targets")
}
