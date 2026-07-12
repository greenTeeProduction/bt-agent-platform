package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Deploy-drift detection (program 94b0b31, "Close the automated deploy-drift
// loop"). A long-lived daemon keeps running its old binary after a fix is
// committed to the repo it builds from; nothing tells it the repo HEAD has moved
// past the revision it was built at. DriftStatus makes that observable at zero
// risk; DriftWatchOnce turns it into a WARN (and, only when explicitly enabled,
// an out-of-place rebuild via rebuild.go).

// driftHeadFn resolves a repo's HEAD revision; a package var so tests can stub
// the git call.
var driftHeadFn = defaultDriftHead

// driftRebuildFn performs the actual rebuild+swap; a package var (defaults to
// RebuildBinaries) so DriftWatchOnce can be tested without a toolchain.
var driftRebuildFn = RebuildBinaries

// defaultDriftHead shells `git -C <repoDir> rev-parse HEAD`. It scrubs the
// inherited git plumbing env (GIT_DIR/GIT_WORK_TREE/…) so a hook or worktree
// context cannot redirect rev-parse at the wrong repository — the same class of
// leak that mis-authored a shared bare repo on 2026-07-10.
func defaultDriftHead(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	cmd.Env = scrubGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", repoDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DriftStatus reports the repo HEAD and whether the running build revision has
// fallen behind it. An unstamped build ("" or "unknown") cannot be compared and
// is never reported stale (avoids a false alarm that would churn a rebuild).
func DriftStatus(repoDir, runningRevision string) (head string, stale bool, err error) {
	head, err = driftHeadFn(repoDir)
	if err != nil {
		return "", false, err
	}
	runningRevision = strings.TrimSpace(runningRevision)
	if runningRevision == "" || runningRevision == "unknown" {
		return head, false, nil
	}
	return head, head != runningRevision, nil
}

// DriftWatchConfig configures a single drift check.
type DriftWatchConfig struct {
	RepoDir         string
	RunningRevision string
	// AutoRebuild gates the out-of-place rebuild. Default (false) is
	// detection-only: a drift logs a WARN and nothing is rebuilt. A daemon that
	// rebuilds and hot-swaps its own binaries is opt-in via
	// BT_AUTO_REBUILD_ON_DRIFT=1 at the call site.
	AutoRebuild bool
	Targets     []RebuildTarget
	// Binary names the process for the log line (e.g. "bt-agent").
	Binary string
}

// DriftResult is the outcome of one DriftWatchOnce check.
type DriftResult struct {
	Head    string
	Stale   bool
	Rebuilt bool
}

// DriftWatchOnce performs one drift check: it WARNs on drift and, only when
// AutoRebuild is set, rebuilds+swaps the targets. A rebuild failure is returned
// (and leaves Rebuilt false) but is not fatal to the caller's loop.
func DriftWatchOnce(cfg DriftWatchConfig) (DriftResult, error) {
	head, stale, err := DriftStatus(cfg.RepoDir, cfg.RunningRevision)
	if err != nil {
		return DriftResult{}, err
	}
	res := DriftResult{Head: head, Stale: stale}
	if !stale {
		return res, nil
	}
	slog.Warn("deploy drift: running binary is behind repo HEAD",
		"binary", cfg.Binary, "running_revision", cfg.RunningRevision,
		"head_revision", head, "auto_rebuild", cfg.AutoRebuild)
	if !cfg.AutoRebuild {
		return res, nil
	}
	if err := driftRebuildFn(cfg.RepoDir, cfg.Targets); err != nil {
		return res, fmt.Errorf("deploy-drift rebuild: %w", err)
	}
	res.Rebuilt = true
	slog.Warn("deploy drift: rebuilt binaries from repo HEAD — restart to adopt",
		"binary", cfg.Binary, "head_revision", head)
	return res, nil
}

// DefaultDriftCheckInterval is the cadence for the background drift watcher.
const DefaultDriftCheckInterval = 20 * time.Minute

// StartDriftWatcher runs DriftWatchOnce on an ~interval cadence until ctx is
// done, with a small per-process start offset so the daemons don't all check at
// the same instant. Detection-only unless cfg.AutoRebuild is set. Check errors
// are logged, never fatal; a panic in the loop is recovered.
func StartDriftWatcher(ctx context.Context, cfg DriftWatchConfig, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultDriftCheckInterval
	}
	offset := time.Duration(os.Getpid()%10) * time.Minute
	if offset >= interval {
		offset = 0
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("deploy drift: watcher panicked (recovered)", "binary", cfg.Binary, "panic", r)
			}
		}()
		timer := time.NewTimer(offset)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if _, err := DriftWatchOnce(cfg); err != nil {
					slog.Warn("deploy drift: check failed", "binary", cfg.Binary, "err", err)
				}
				timer.Reset(interval)
			}
		}
	}()
}

// AutoRebuildEnabled reports whether BT_AUTO_REBUILD_ON_DRIFT opts into the
// self-rebuild behavior. Default (unset) is detection-only.
func AutoRebuildEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BT_AUTO_REBUILD_ON_DRIFT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// scrubGitEnv returns the current environment minus the git plumbing variables
// that would redirect a `git` invocation at the wrong repo.
func scrubGitEnv() []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_PREFIX": true, "GIT_COMMON_DIR": true,
	}
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if drop[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
