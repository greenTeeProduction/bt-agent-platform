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

// driftRestartFn restarts the daemon to adopt a rebuilt binary; driftSmokeTestFn
// validates the rebuilt binary first; restorePreviousBinaryFn rolls the swap
// back on a failed smoke test. Package vars so the restart handoff is testable
// without a real systemd unit or a real binary.
var (
	driftRestartFn          = defaultDriftRestart
	driftSmokeTestFn        = defaultDriftSmokeTest
	restorePreviousBinaryFn = restorePreviousBinary
)

// defaultDriftRestart asks systemd to restart the daemon's own unit. --no-block
// returns immediately so this process is not killed synchronously by the restart
// it requests; systemd then stops and starts the unit on the new binary.
func defaultDriftRestart(binary string) error {
	cmd := exec.Command("systemctl", "--user", "restart", "--no-block", binary+".service")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user restart --no-block %s.service: %w\n%s", binary, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultDriftSmokeTest runs the rebuilt binary with --version and requires a
// clean exit — a cheap guard that catches a binary broken enough to fail its
// own startup before it is adopted as the live daemon.
func defaultDriftSmokeTest(binPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--version")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s --version: %w\n%s", binPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// restorePreviousBinary copies <binPath>.previous back over binPath — the
// rollback for a rebuilt binary that failed its smoke test, so a later restart
// cannot adopt the broken build.
func restorePreviousBinary(binPath string) error {
	prev := binPath + ".previous"
	data, err := os.ReadFile(prev)
	if err != nil {
		return err
	}
	return os.WriteFile(binPath, data, 0o755)
}

// AutoRestartEnabled reports whether BT_AUTO_RESTART_ON_DRIFT opts into adopting
// a rebuilt binary by restarting the daemon. Default (unset) rebuilds only.
func AutoRestartEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BT_AUTO_RESTART_ON_DRIFT"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

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
	// Backoff, when set, throttles repeated rebuild attempts against the same
	// stale HEAD so a broken commit cannot retry-storm `go build` every watch
	// interval. Nil disables throttling (every stale tick attempts a rebuild).
	Backoff *RebuildBackoff
	// InFlightFn, when set, is consulted before a rebuild: a true result skips
	// the rebuild attempt entirely, the same way a backoff-blocked tick does.
	// Plugged with Scheduler.AnyInFlight so an out-of-place rebuild never
	// swaps the daemon's own binary out from under a mid-execution job. Nil
	// disables the guard (every stale tick may rebuild).
	InFlightFn func() bool
	// AutoRestart gates adopting the rebuilt binary by restarting the daemon.
	// It only applies AFTER a successful AutoRebuild (a rebuild without a
	// restart leaves the running process on the old code). Opt-in via
	// BT_AUTO_RESTART_ON_DRIFT=1: before restarting, the freshly-built self
	// binary is smoke-tested, and a failing smoke test rolls the swap back
	// (<bin>.previous) and skips the restart. Default (false) keeps the
	// pre-2026-07-13 behavior: rebuild then log "restart to adopt".
	AutoRestart bool
	// Kick, when set, lets a producer (the scheduler's OnCycleIdle hook)
	// trigger an immediate drift check outside the fixed interval. Without it
	// the watcher starves on busy daemons: every fixed tick lands inside an
	// in-flight cycle and an armed auto-redeploy never fires (observed
	// 2026-07-15: drift detected all day, zero rebuilds).
	Kick <-chan struct{}
}

// selfBinaryPath returns the OutPath of the target whose Name matches Binary —
// the daemon's own binary, the one to smoke-test and (via restart) adopt.
func (c DriftWatchConfig) selfBinaryPath() string {
	for _, t := range c.Targets {
		if t.Name == c.Binary {
			return t.OutPath
		}
	}
	return ""
}

// DriftResult is the outcome of one DriftWatchOnce check.
type DriftResult struct {
	Head      string
	Stale     bool
	Rebuilt   bool
	Restarted bool
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
	if cfg.Backoff != nil && !cfg.Backoff.Allow(head) {
		slog.Warn("deploy drift: rebuild attempt blocked by backoff guard",
			"binary", cfg.Binary, "head_revision", head)
		return res, nil
	}
	if cfg.InFlightFn != nil && cfg.InFlightFn() {
		slog.Warn("deploy drift: rebuild attempt skipped — a scheduled job is in-flight",
			"binary", cfg.Binary, "head_revision", head)
		return res, nil
	}
	if err := driftRebuildFn(cfg.RepoDir, cfg.Targets); err != nil {
		if cfg.Backoff != nil {
			cfg.Backoff.RecordFailure(head)
		}
		return res, fmt.Errorf("deploy-drift rebuild: %w", err)
	}
	if cfg.Backoff != nil {
		cfg.Backoff.RecordSuccess(head)
	}
	res.Rebuilt = true
	if !cfg.AutoRestart {
		slog.Warn("deploy drift: rebuilt binaries from repo HEAD — restart to adopt",
			"binary", cfg.Binary, "head_revision", head)
		return res, nil
	}
	// Adopt the rebuilt binary by restarting the daemon. RebuildBinaries has
	// already renamed <bin>.new over the live path (keeping <bin>.previous), so
	// the self binary path now points at the new build — smoke-test it before
	// committing to a restart that could otherwise crash-loop the service.
	if binPath := cfg.selfBinaryPath(); binPath != "" {
		if err := driftSmokeTestFn(binPath); err != nil {
			restoreErr := restorePreviousBinaryFn(binPath)
			slog.Error("deploy drift: rebuilt binary failed smoke test — rolled back, NOT restarting",
				"binary", cfg.Binary, "head_revision", head, "err", err, "rollback_err", restoreErr)
			return res, fmt.Errorf("deploy-drift smoke test failed for %s: %w", binPath, err)
		}
	}
	// A rebuild takes minutes; a scheduled job may have started in that window.
	// Re-check before restarting — killing a mid-execution cycle is worse than
	// deferring adoption (the rebuilt binary stays swapped in for the next
	// idle check).
	if cfg.InFlightFn != nil && cfg.InFlightFn() {
		slog.Warn("deploy drift: restart deferred — a job started during the rebuild",
			"binary", cfg.Binary, "head_revision", head)
		return res, nil
	}
	slog.Warn("deploy drift: restarting to adopt rebuilt binary",
		"binary", cfg.Binary, "head_revision", head)
	if err := driftRestartFn(cfg.Binary); err != nil {
		return res, fmt.Errorf("deploy-drift restart: %w", err)
	}
	res.Restarted = true
	return res, nil
}

// AdoptDriftOnIdle runs one synchronous drift check-rebuild-restart pass. It
// is the scheduler's OnCycleIdle hook body: called from the scheduler loop
// goroutine at the moment a cycle completes with nothing in flight, it BLOCKS
// the queue for the duration of the rebuild — which is the point. The async
// Kick variant lost a milliseconds race on saturated fleets: the tick loop
// started the next queued job before the watcher goroutine ran its InFlightFn
// check (observed live 2026-07-15 13:54/14:01). Synchronous adoption cannot
// be raced; queued jobs wait ~2-3 min for the rebuild, and a restart's
// crash-recovery + missed-slot catch-up preserves them. Errors are logged,
// never propagated — this runs inside the scheduler loop.
func AdoptDriftOnIdle(cfg DriftWatchConfig) {
	if _, err := DriftWatchOnce(cfg); err != nil {
		slog.Warn("deploy drift: idle adoption check failed", "binary", cfg.Binary, "err", err)
	}
}

// DefaultDriftCheckInterval is the cadence for the background drift watcher.
const DefaultDriftCheckInterval = 20 * time.Minute

// StartDriftWatcher runs DriftWatchOnce on an ~interval cadence until ctx is
// done, with a small per-process start offset so the daemons don't all check at
// the same instant. Detection-only unless cfg.AutoRebuild is set. Check errors
// are logged, never fatal; a panic in the loop is recovered.
// The returned stop func cancels the loop and blocks until it has exited —
// daemons may discard it; tests use it to join the goroutine.
func StartDriftWatcher(ctx context.Context, cfg DriftWatchConfig, interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = DefaultDriftCheckInterval
	}
	offset := time.Duration(os.Getpid()%10) * time.Minute
	if offset >= interval {
		offset = 0
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
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
			case <-cfg.Kick:
				// Scheduler-signaled idle window: check now instead of waiting
				// out the interval. The timer is reset so a kick also defers
				// the next fixed check.
				if _, err := DriftWatchOnce(cfg); err != nil {
					slog.Warn("deploy drift: kicked check failed", "binary", cfg.Binary, "err", err)
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(interval)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
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
