package cicd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoCommittedRootBinaries guards against compiled binaries landing in the
// repository: on 2026-07-15 an autonomous cycle re-committed a rebuilt 19.8MB
// `benchcmp` (and `bt-ci-doctor` was tracked alongside it) — every rebuild
// then bloats history by tens of MB. Any tracked ELF executable at the repo
// root fails this test; build outputs belong in bin/ (gitignored) or nowhere.
func TestNoCommittedRootBinaries(t *testing.T) {
	top, err := gitOutput(".", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Skipf("not in a git checkout: %v", err)
	}
	top = strings.TrimSpace(top)

	tracked, err := gitOutput(top, "ls-files")
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	var offenders []string
	for _, f := range strings.Split(strings.TrimSpace(tracked), "\n") {
		if f == "" || strings.Contains(f, "/") {
			continue // root-level files only
		}
		head := make([]byte, 4)
		fh, err := os.Open(filepath.Join(top, f))
		if err != nil {
			continue // deleted-but-tracked etc. — not this test's concern
		}
		n, _ := fh.Read(head)
		fh.Close()
		if n == 4 && bytes.Equal(head, []byte{0x7f, 'E', 'L', 'F'}) {
			offenders = append(offenders, f)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("tracked ELF binaries at the repo root: %v — `git rm --cached` them and add .gitignore entries; compiled outputs must not be committed", offenders)
	}
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
