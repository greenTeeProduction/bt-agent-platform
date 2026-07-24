package engine

import "testing"

func TestResolveSuperpowersWorktreeBaseEnvOverride(t *testing.T) {
	t.Setenv("BT_WORKTREE_BASE", "/mnt/ssd/worktrees")
	if got := resolveSuperpowersWorktreeBase(); got != "/mnt/ssd/worktrees" {
		t.Errorf("expected BT_WORKTREE_BASE override, got %q", got)
	}
}

func TestResolveSuperpowersWorktreeBaseDefault(t *testing.T) {
	t.Setenv("BT_WORKTREE_BASE", "  ")
	if got := resolveSuperpowersWorktreeBase(); got != "/tmp/worktrees" {
		t.Errorf("expected /tmp/worktrees default for blank env, got %q", got)
	}
}

func TestShepherdFixWorktreePathUsesBase(t *testing.T) {
	old := superpowersWorktreeBase
	superpowersWorktreeBase = "/custom/base"
	defer func() { superpowersWorktreeBase = old }()
	if got := shepherdFixWorktreePath("pr-fix-abc12345"); got != "/custom/base/pr-fix-abc12345" {
		t.Errorf("expected worktree-base-joined path, got %q", got)
	}
}
