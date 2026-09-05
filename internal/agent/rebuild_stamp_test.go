package agent

import (
	"strings"
	"testing"
)

// TestBuildStampLdflagsNonGitDir: outside a git repo, no stamp is produced (the
// build proceeds unstamped rather than failing).
func TestBuildStampLdflagsNonGitDir(t *testing.T) {
	if got := buildStampLdflags(t.TempDir()); got != "" {
		t.Fatalf("buildStampLdflags(non-git) = %q, want empty", got)
	}
}

// TestBuildStampLdflagsInRepo: inside this checkout, the stamp names the
// dashboard.stampedRevision symbol and a 40-hex HEAD.
func TestBuildStampLdflagsInRepo(t *testing.T) {
	got := buildStampLdflags(".")
	if got == "" {
		t.Skip("not a git checkout; skipping positive stamp assertion")
	}
	const prefix = "-X github.com/nico/go-bt-evolve/internal/dashboard.stampedRevision="
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("stamp = %q, want prefix %q", got, prefix)
	}
	rev := strings.TrimPrefix(got, prefix)
	if len(rev) != 40 {
		t.Fatalf("stamped revision %q length = %d, want 40", rev, len(rev))
	}
}
