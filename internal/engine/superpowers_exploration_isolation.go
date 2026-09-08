package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// isolateProductionExploration runs at the actual CLI boundary, not at a late
// implementation-tree node. Design/grill/brainstorm/seed calls can already
// Write/Edit and run tests before a Superpowers worktree exists. Both providers
// (including failover and read-only reviews that run tests) must pass this gate.
// Existing implementation worktrees are left alone: their edits must survive
// across RED/GREEN/review calls. This is cwd isolation, not an OS sandbox.
func isolateProductionExploration(ctx context.Context, dir, prompt string) (string, string, func(), error) {
	noop := func() {}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, prompt, noop, err
	}
	canonical := func(p string) string {
		p, _ = filepath.Abs(p)
		if real, e := filepath.EvalSymlinks(p); e == nil {
			return real
		}
		return p
	}
	abs = canonical(abs)
	for _, configured := range []string{superpowersRepoDir, goapFusionRepo} {
		if configured == "" {
			continue
		}
		root := canonical(configured)
		rel, e := filepath.Rel(root, abs)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		// A linked worktree/standalone clone may live under .claude/worktrees
		// inside production. Its own Git top-level is already isolated; do not
		// discard its in-progress implementation edits by cloning production.
		setupCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		git := execCommandRunner{}
		top := git.Run(setupCtx, abs, "git", "rev-parse", "--show-toplevel")
		if top.Err == nil && canonical(strings.TrimSpace(top.Output)) != root {
			return dir, prompt, noop, nil
		}
		base := canonical(superpowersWorktreeBase)
		baseRel, e := filepath.Rel(root, base)
		if e != nil || (baseRel != ".." && !strings.HasPrefix(baseRel, ".."+string(filepath.Separator))) {
			return dir, prompt, noop, fmt.Errorf("exploration isolation base must be outside production checkout")
		}
		head := git.Run(setupCtx, root, "git", "rev-parse", "HEAD")
		if head.Err != nil {
			return dir, prompt, noop, fmt.Errorf("exploration isolation HEAD: %w", head.Err)
		}
		if err := os.MkdirAll(base, 0700); err != nil {
			return dir, prompt, noop, err
		}
		clone, err := os.MkdirTemp(base, "exploration-")
		if err != nil {
			return dir, prompt, noop, err
		}
		// --no-local avoids shared objects/hardlinks and never writes worktree
		// registration into the production repository. No fetch/pull/reset there.
		res := git.Run(setupCtx, base, "git", "clone", "--no-local", "--no-checkout", "--", root, clone)
		if res.Err == nil {
			res = git.Run(setupCtx, clone, "git", "checkout", "--detach", strings.TrimSpace(head.Output))
		}
		if res.Err == nil {
			res = git.Run(setupCtx, clone, "git", "remote", "remove", "origin")
		}
		if res.Err != nil {
			_ = os.RemoveAll(clone) // No agent has run: no user/agent edits to lose.
			return dir, prompt, noop, fmt.Errorf("exploration isolation: %w: %s", res.Err, res.Output)
		}
		isolatedDir := filepath.Join(clone, rel)
		if info, err := os.Stat(isolatedDir); err != nil || !info.IsDir() {
			_ = os.RemoveAll(clone)
			return dir, prompt, noop, fmt.Errorf("exploration isolation: cwd %q absent from committed snapshot", rel)
		}
		refs := git.Run(setupCtx, clone, "git", "for-each-ref", "--format=%(refname) %(objectname)")
		cleanup := func() {
			// Keep all evidence on failures to inspect, dirty/ignored files, or agent
			// commits. The returned CommandResult.Dir identifies the retained clone.
			c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			status := git.Run(c, clone, "git", "status", "--porcelain=v1", "--untracked-files=all", "--ignored")
			tip := git.Run(c, clone, "git", "rev-parse", "HEAD")
			currentRefs := git.Run(c, clone, "git", "for-each-ref", "--format=%(refname) %(objectname)")
			if status.Err == nil && status.Output == "" && tip.Err == nil && tip.Output == head.Output &&
				refs.Err == nil && currentRefs.Err == nil && refs.Output == currentRefs.Output {
				_ = os.RemoveAll(clone)
			}
		}
		prompt = strings.ReplaceAll(prompt, configured, clone)
		prompt = strings.ReplaceAll(prompt, root, clone)
		if dir != "" && dir != "." && filepath.IsAbs(dir) {
			prompt = strings.ReplaceAll(prompt, dir, isolatedDir)
		}
		prompt = "Explore and run probes/tests only in this isolated checkout: " + isolatedDir + ". Do not access or modify the original production checkout.\n\n" + prompt
		return isolatedDir, prompt, cleanup, nil
	}
	return dir, prompt, noop, nil
}
