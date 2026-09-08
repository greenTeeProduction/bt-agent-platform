package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func isolationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func isolationRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	isolationGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "tracked"), []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	isolationGit(t, dir, "add", ".")
	isolationGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture")
	oldGoap, oldBase := goapFusionRepo, superpowersWorktreeBase
	goapFusionRepo, superpowersWorktreeBase = dir, t.TempDir()
	t.Cleanup(func() { goapFusionRepo, superpowersWorktreeBase = oldGoap, oldBase })
	return dir
}

func TestExplorationCLIIsolatedBeforeProbe(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			repo := isolationRepo(t)
			// Existing production edits must neither block exploration nor be reset.
			if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("operator edit\n"), 0600); err != nil {
				t.Fatal(err)
			}
			before := isolationGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all")
			bin := filepath.Join(t.TempDir(), provider)
			script := "#!/bin/sh\nprintf 'probe' > zz_probe_test.go\nprintf '%s\\n' \"$PWD\"\n"
			if provider == "codex" {
				script += "while [ $# -gt 0 ]; do if [ \"$1\" = --output-last-message ]; then shift; printf 'done' > \"$1\"; break; fi; shift; done\n"
			}
			if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			var res CommandResult
			if provider == "claude" {
				res = (execClaudeRunner{Bin: bin}).RunClaude(context.Background(), repo, "Explore and test")
			} else {
				res = (execCodexRunner{Bin: bin}).RunCodex(context.Background(), repo, "Explore and test")
			}
			if res.Err != nil {
				t.Fatalf("run: %v: %s", res.Err, res.Output)
			}
			if res.Dir == repo {
				t.Fatal("writable exploratory subprocess ran in production checkout")
			}
			if after := isolationGit(t, repo, "status", "--porcelain=v1", "--untracked-files=all"); after != before {
				t.Fatalf("production changed: %s -> %s", before, after)
			}
			if _, err := os.Stat(filepath.Join(res.Dir, "zz_probe_test.go")); err != nil {
				t.Fatalf("probe evidence lost: %v", err)
			}
			if out := isolationGit(t, res.Dir, "remote"); out != "" {
				t.Fatalf("exploration clone has remote: %s", out)
			}
			data, err := os.ReadFile(filepath.Join(repo, "tracked"))
			if err != nil || string(data) != "operator edit\n" {
				t.Fatal("operator edit lost")
			}
		})
	}
}

func TestExplorationIsolationLifecycle(t *testing.T) {
	for _, change := range []string{"clean", "ignored", "commit", "branch"} {
		t.Run(change, func(t *testing.T) {
			repo := isolationRepo(t)
			isolated, prompt, cleanup, err := isolateProductionExploration(context.Background(), repo, "Inspect "+repo)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(prompt, repo) || !strings.Contains(prompt, isolated) {
				t.Fatalf("prompt retains production cwd: %s", prompt)
			}
			switch change {
			case "ignored":
				if err := os.WriteFile(filepath.Join(isolated, ".git", "info", "exclude"), []byte("evidence\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(isolated, "evidence"), []byte("probe\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "branch":
				isolationGit(t, isolated, "branch", "probe-evidence")
			case "commit":
				isolationGit(t, isolated, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "probe evidence")
			}
			cleanup()
			_, err = os.Stat(isolated)
			if change == "clean" && !os.IsNotExist(err) {
				t.Fatal("clean exploration clone leaked")
			}
			if change != "clean" && err != nil {
				t.Fatalf("%s evidence deleted: %v", change, err)
			}
		})
	}
}

func TestExplorationIsolationAliasAndImplementationWorktree(t *testing.T) {
	repo := isolationRepo(t)
	alias := filepath.Join(t.TempDir(), "production-alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	isolated, _, cleanup, err := isolateProductionExploration(context.Background(), alias, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if isolated == repo || isolated == alias {
		t.Fatal("production symlink bypassed isolation")
	}
	cleanup()
	// Claude also creates implementation worktrees nested beneath production.
	wt := filepath.Join(repo, ".claude", "worktrees", "implementation")
	isolationGit(t, repo, "worktree", "add", "--detach", wt, "HEAD")
	actual, _, cleanup, err := isolateProductionExploration(context.Background(), wt, "implement")
	if err != nil || actual != wt {
		t.Fatalf("implementation worktree was redirected: %s %v", actual, err)
	}
	cleanup()
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("implementation worktree deleted")
	}
}

func TestExplorationIsolationCancelledBeforeAgent(t *testing.T) {
	repo := isolationRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := isolateProductionExploration(ctx, repo, "probe")
	if err == nil {
		t.Fatal("cancelled exploration must not execute")
	}
}

func TestExplorationIsolationFailsClosed(t *testing.T) {
	repo := isolationRepo(t)
	if err := os.RemoveAll(filepath.Join(repo, ".git")); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntouch should-not-run\n"), 0700); err != nil {
		t.Fatal(err)
	}
	res := (execClaudeRunner{Bin: bin}).RunClaude(context.Background(), repo, "probe")
	if res.Err == nil {
		t.Fatal("expected isolation failure before executing agent")
	}
	if _, err := os.Stat(filepath.Join(repo, "should-not-run")); !os.IsNotExist(err) {
		t.Fatal("agent ran after isolation failure")
	}
}
