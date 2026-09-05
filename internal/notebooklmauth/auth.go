// Package notebooklmauth owns the background authentication policy shared by
// the daemon, raw tool and cron command. It never starts an interactive login.
package notebooklmauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const cooldown = 15 * time.Minute

// Result is an auth verdict, not a transcript (which can contain account data).
type Result struct {
	Status     string    `json:"status"`
	Detail     string    `json:"detail"`
	RetryAfter time.Time `json:"retry_after,omitzero"`
}

// OK requires positive evidence from a successful real CLI auth check.
func (r Result) OK() bool { return r.Status == "valid" }

func (r Result) String() string {
	if !r.RetryAfter.IsZero() {
		return fmt.Sprintf("%s: %s (retry after %s)", r.Status, r.Detail, r.RetryAfter.UTC().Format(time.RFC3339))
	}
	return r.Status + ": " + r.Detail
}

type runner func(context.Context, ...string) (string, error)

type policy struct {
	stateDir string
	cdpURL   string
	run      runner
	restore  func(context.Context, string) Result
}

// Ensure validates saved auth first. All callers use the same persistent state
// and lock. Environment overrides must be identical for daemon and cron.
func Ensure(ctx context.Context) Result {
	dir := os.Getenv("BT_NOTEBOOKLM_AUTH_STATE_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Result{Status: "auth_error", Detail: "cannot locate auth policy state directory"}
		}
		dir = filepath.Join(home, ".go-bt-evolve", "notebooklm-auth")
	}
	cdpURL := os.Getenv("BT_NOTEBOOKLM_CDP_URL")
	if cdpURL == "" {
		cdpURL = "http://localhost:9222"
	}
	p := policy{stateDir: dir, cdpURL: cdpURL, run: runCLI, restore: existingBrowserRestore}
	return p.ensure(ctx)
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := Command(ctx, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return string(out), err
}

// classify gives explicit network failures precedence over stale/auth words.
// Generic failures and empty output are never evidence that credentials expired.
func classify(out string, err error) string {
	s := strings.ToLower(out)
	for _, marker := range []string{"network_error", "could not reach", "timed out", "timeout", "connection refused", "connection reset", "temporary failure", "deadline exceeded", "unreachable", "name resolution", "no route to host", "http_5", "http 5", "status code 5"} {
		if strings.Contains(s, marker) {
			return "network_error"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "network_error"
	}
	for _, marker := range []string{"stale", "not_configured", "expired", "no saved credentials", "profile not found:", "no_tokens", "login required", "sign-in required"} {
		if strings.Contains(s, marker) {
			return "auth_required"
		}
	}
	if err == nil && !strings.Contains(s, "failed") && !strings.Contains(s, "error") &&
		(strings.Contains(s, "authentication valid!") || strings.Contains(s, "authenticated as ")) {
		return "valid"
	}
	return "auth_error"
}

func (p policy) check(ctx context.Context) Result {
	out, err := p.run(ctx, "login", "--check")
	status := classify(out, err)
	details := map[string]string{
		"valid":         "saved authentication confirmed by nlm login --check",
		"network_error": "NotebookLM auth check could not reach the service; saved credentials preserved",
		"auth_required": "saved authentication is missing or expired",
		"auth_error":    "auth check failed without evidence of expired credentials; saved credentials preserved",
	}
	return Result{Status: status, Detail: details[status]}
}

func (p policy) ensure(ctx context.Context) Result {
	if err := ctx.Err(); err != nil {
		return Result{Status: "auth_error", Detail: "auth check canceled"}
	}
	if err := os.MkdirAll(p.stateDir, 0700); err != nil {
		return stateError()
	}
	// Never unlink this lock: replacing its inode would permit overlapping owners.
	lock, err := os.OpenFile(filepath.Join(p.stateDir, "policy.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return stateError()
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return Result{Status: "in_progress", Detail: "another process is checking NotebookLM authentication"}
		}
		return stateError()
	}
	statePath := filepath.Join(p.stateDir, "cooldown.json")
	var previous Result
	b, err := os.ReadFile(statePath)
	if err == nil {
		if json.Unmarshal(b, &previous) != nil || previous.RetryAfter.IsZero() {
			return stateError()
		}
		if time.Now().Before(previous.RetryAfter) {
			return Result{Status: "cooldown", Detail: "previous " + previous.Status + "; automated auth retry suppressed", RetryAfter: previous.RetryAfter}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return stateError()
	}

	// Persist before any subprocess/CDP work so crashes also throttle retries.
	pending := Result{Status: "interrupted", Detail: "previous auth attempt interrupted", RetryAfter: time.Now().Add(cooldown)}
	if err := saveState(statePath, pending); err != nil {
		return stateError()
	}
	r := p.check(ctx)
	if r.Status == "auth_required" {
		r = p.restore(ctx, p.cdpURL)
		// A restore's own success text is insufficient. Recheck real saved auth.
		if r.OK() {
			r = p.check(ctx)
		}
	}
	if r.OK() {
		if err := os.Remove(statePath); err != nil {
			return stateError()
		}
		return r
	}
	r.RetryAfter = time.Now().Add(cooldown)
	if err := saveState(statePath, r); err != nil {
		return stateError()
	}
	return r
}

func stateError() Result {
	return Result{Status: "auth_error", Detail: "cannot safely read/write auth cooldown state; background restoration disabled"}
}

func saveState(path string, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cooldown-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	// Persist the renamed directory entry, not just the temporary file's data.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
