package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// Exercise the registered action against disposable repositories and an inert
// Hermes executable; never invoke the installed updater or touch the real HOME.
func TestHermesUpdateAgentVerification(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantOK       bool
	}{
		{"updated", "git reset --hard origin/main", true},
		{"unchanged", ":", false},
		{"residual behind", "git reset --hard origin/main~1", false},
		{"unreadable HEAD", "rm .git/HEAD", false},
		{"unknown residual", "git reset --hard origin/main; git update-ref -d refs/remotes/origin/main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// Git configuration and identity are local to each disposable fixture.
			git := func(dir string, args ...string) string {
				t.Helper()
				cmd := exec.Command("/usr/bin/git", append([]string{"-C", dir}, args...)...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			seed := filepath.Join(home, "seed")
			if err := os.MkdirAll(seed, 0700); err != nil {
				t.Fatal(err)
			}
			git(seed, "init", "-b", "main")
			git(seed, "config", "user.name", "Fixture")
			git(seed, "config", "user.email", "fixture@example.invalid")
			git(seed, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "first")
			repo := filepath.Join(home, ".hermes", "hermes-agent")
			if err := os.MkdirAll(filepath.Dir(repo), 0700); err != nil {
				t.Fatal(err)
			}
			git(home, "clone", seed, repo)
			git(seed, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "second")
			git(seed, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "third")
			bin := filepath.Join(home, ".local", "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nset -e\nif [ \"$1\" = --version ]; then printf 'Hermes fixture version\\n'; exit 0; fi\n" + tc.script + "\n"
			if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			bb := newTestBlackboard()
			got := GetAction("HermesUpdateAgent")(&btcore.BTContext[Blackboard]{Blackboard: bb})
			if (got == 1) != tc.wantOK || (bb.Outcome == "success") != tc.wantOK {
				t.Fatalf("result=%d outcome=%s wantOK=%v report=%s", got, bb.Outcome, tc.wantOK, bb.Result)
			}
			if !tc.wantOK && !strings.Contains(bb.Result, "FAILED") {
				t.Fatalf("missing failure report: %s", bb.Result)
			}
		})
	}
}
