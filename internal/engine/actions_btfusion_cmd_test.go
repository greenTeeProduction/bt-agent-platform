package engine

import (
	"strings"
	"testing"
)

// TestFusionCodebaseFitCmdReadsGitNotWorkingTree pins that the codebase-fit
// evidence comes from git HEAD content, not on-disk source files: the main
// repo is bare and its pre-conversion source was removed 2026-07-03, so a
// filesystem grep of internal/domains/trees.go fails every bt-fusion run.
func TestFusionCodebaseFitCmdReadsGitNotWorkingTree(t *testing.T) {
	if !strings.Contains(fusionCodebaseFitCmd, "git grep") {
		t.Fatalf("codebase-fit must use git grep against HEAD, got:\n%s", fusionCodebaseFitCmd)
	}
	if strings.Contains(fusionCodebaseFitCmd, "grep -R") {
		t.Fatalf("codebase-fit must not grep the working tree:\n%s", fusionCodebaseFitCmd)
	}
	for _, want := range []string{"bt_fusion", "hermes_update", "trees.go"} {
		if !strings.Contains(fusionCodebaseFitCmd, want) {
			t.Fatalf("codebase-fit cmd missing %q:\n%s", want, fusionCodebaseFitCmd)
		}
	}
}
