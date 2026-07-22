package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ErrorHandlerExtension is one Claude-generated recovery node, persisted under
// ~/.go-bt-evolve/error_handler/ and re-grafted as a ClaudeErrorHandler child at
// every tree build (scheduled runs rebuild trees from the compiled catalog each
// run, so persistence must live with the node, not the tree).
type ErrorHandlerExtension struct {
	Node                evolution.SerializableNode `json:"node"`
	Signature           string                     `json:"signature"`
	CreatedAt           time.Time                  `json:"created_at"`
	Successes           int                        `json:"successes"`
	ConsecutiveFailures int                        `json:"consecutive_failures"`
	Disabled            bool                       `json:"disabled"`
}

type errorHandlerLedgerEntry struct {
	LastAttempt time.Time `json:"last_attempt"`
	Attempts    int       `json:"attempts"`
	LastVerdict string    `json:"last_verdict"`
	// ConsecutiveEscalateFailed counts back-to-back "escalate_failed" stamps.
	// escalate_failed is the only verdict that bypasses the cooldown (it means
	// a transient seed miss, not a real Claude verdict), but a PERSISTENTLY
	// broken self-fix store would otherwise bypass the cooldown forever. Reset
	// to 0 on any other verdict; capped against errorHandlerDisableAfter in
	// the cooldown check.
	ConsecutiveEscalateFailed int `json:"consecutive_escalate_failed"`
}

// errorHandlerDisableAfter is the consecutive-failure streak that disables a
// generated extension (matches the platform's PatchBoard 3-window convention).
const errorHandlerDisableAfter = 3

// errorHandlerLedgerMaxEntries bounds ledger.json: on overflow the entries
// with the oldest LastAttempt are evicted (deterministically) down to the cap.
const errorHandlerLedgerMaxEntries = 256

// errorHandlerDirOverride redirects the store in tests (same var-override
// pattern as goapFusionVaultDir).
var errorHandlerDirOverride string

// errorHandlerStoreMu guards in-process load-modify-write cycles of the error
// handler store JSON files. Cross-process read-modify-write cycles are guarded
// by the on-disk store.lock (fleet siblings share these files — see the
// job-table-wiper incident); counters remain advisory best-effort on lock
// contention.
var errorHandlerStoreMu sync.Mutex

func errorHandlerDir() string {
	if errorHandlerDirOverride != "" {
		return errorHandlerDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "error_handler")
}

func errorHandlerExtensionsPath() string { return filepath.Join(errorHandlerDir(), "extensions.json") }
func errorHandlerLedgerPath() string     { return filepath.Join(errorHandlerDir(), "ledger.json") }

func readErrorHandlerJSON(path string, out any) {
	_ = readErrorHandlerJSONStrict(path, out)
}

// readErrorHandlerJSONStrict distinguishes "no data yet" from "data exists but
// can't be read": nil on os.IsNotExist (out untouched — legitimately empty),
// the error otherwise. Read-modify-write callers MUST abort on error — treating
// a transient read failure as an empty store would rewrite the whole file from
// an empty map.
func readErrorHandlerJSONStrict(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return nil
}

// writeErrorHandlerJSON persists atomically per ADR-003: tmp file + rename.
// The tmp name is randomized (os.CreateTemp) — a fixed path+".tmp" collides
// across concurrent sibling processes sharing the store.
func writeErrorHandlerJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o644)
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func loadErrorHandlerExtensions(handlerName string) []ErrorHandlerExtension {
	errorHandlerStoreMu.Lock()
	defer errorHandlerStoreMu.Unlock()
	all := map[string][]ErrorHandlerExtension{}
	readErrorHandlerJSON(errorHandlerExtensionsPath(), &all)
	return all[handlerName]
}

func activeErrorHandlerExtensions(handlerName string) []ErrorHandlerExtension {
	var active []ErrorHandlerExtension
	for _, ext := range loadErrorHandlerExtensions(handlerName) {
		if !ext.Disabled {
			active = append(active, ext)
		}
	}
	return active
}

func appendErrorHandlerExtension(handlerName string, ext ErrorHandlerExtension) error {
	errorHandlerStoreMu.Lock()
	defer errorHandlerStoreMu.Unlock()
	release, ok := acquireErrorHandlerStoreLock()
	if !ok {
		return fmt.Errorf("error handler store is locked by another process")
	}
	defer release()
	path := errorHandlerExtensionsPath()
	all := map[string][]ErrorHandlerExtension{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if uerr := json.Unmarshal(data, &all); uerr != nil {
			return fmt.Errorf("error handler store unreadable, refusing to overwrite %s: %w", path, uerr)
		}
		// Simple rollback story: keep the pre-append state as .bak.
		_ = os.WriteFile(path+".bak", data, 0o644)
	case os.IsNotExist(err):
		// First-ever append: seed an empty .bak, but NEVER clobber an existing
		// good .bak with a "{}" placeholder — it is the only rollback copy.
		if _, bakErr := os.Stat(path + ".bak"); os.IsNotExist(bakErr) {
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
			_ = os.WriteFile(path+".bak", []byte("{}"), 0o644)
		}
	default:
		return fmt.Errorf("error handler store unreadable, refusing to overwrite %s: %w", path, err)
	}
	all[handlerName] = append(all[handlerName], ext)
	return writeErrorHandlerJSON(path, all)
}

// recordErrorHandlerResult updates success/failure counters for one extension
// and auto-disables it after errorHandlerDisableAfter consecutive failures.
// Best-effort: persistence errors and cross-process lock contention skip the
// update — counters are advisory. A store that EXISTS but cannot be read also
// skips: rewriting it from an empty map would destroy every extension.
func recordErrorHandlerResult(handlerName, nodeName string, success bool) {
	errorHandlerStoreMu.Lock()
	defer errorHandlerStoreMu.Unlock()
	release, ok := acquireErrorHandlerStoreLock()
	if !ok {
		return
	}
	defer release()
	path := errorHandlerExtensionsPath()
	all := map[string][]ErrorHandlerExtension{}
	if err := readErrorHandlerJSONStrict(path, &all); err != nil {
		Warn("claude error handler: store unreadable, skipping counter update", "path", path, "err", err)
		return
	}
	exts := all[handlerName]
	for i := range exts {
		if exts[i].Node.Name != nodeName {
			continue
		}
		if success {
			exts[i].Successes++
			exts[i].ConsecutiveFailures = 0
		} else {
			exts[i].ConsecutiveFailures++
			if exts[i].ConsecutiveFailures >= errorHandlerDisableAfter {
				exts[i].Disabled = true
				Warn("claude error handler: extension disabled after consecutive failures",
					"handler", handlerName, "node", nodeName, "failures", exts[i].ConsecutiveFailures)
			}
		}
		all[handlerName] = exts
		_ = writeErrorHandlerJSON(path, all)
		return
	}
}

func errorHandlerLedgerGet(sig string) (errorHandlerLedgerEntry, bool) {
	errorHandlerStoreMu.Lock()
	defer errorHandlerStoreMu.Unlock()
	ledger := map[string]errorHandlerLedgerEntry{}
	readErrorHandlerJSON(errorHandlerLedgerPath(), &ledger)
	entry, ok := ledger[sig]
	return entry, ok
}

// errorHandlerLedgerStamp records a proposal attempt for the cooldown ledger.
// Advisory: cross-process lock contention or an unreadable-but-existing ledger
// skips the stamp — rewriting from an empty map would erase every cooldown.
func errorHandlerLedgerStamp(sig, verdict string) {
	errorHandlerStoreMu.Lock()
	defer errorHandlerStoreMu.Unlock()
	release, ok := acquireErrorHandlerStoreLock()
	if !ok {
		return
	}
	defer release()
	ledger := map[string]errorHandlerLedgerEntry{}
	if err := readErrorHandlerJSONStrict(errorHandlerLedgerPath(), &ledger); err != nil {
		Warn("claude error handler: ledger unreadable, skipping stamp", "err", err)
		return
	}
	entry := ledger[sig]
	entry.LastAttempt = time.Now()
	entry.Attempts++
	entry.LastVerdict = verdict
	if verdict == "escalate_failed" {
		entry.ConsecutiveEscalateFailed++
	} else {
		entry.ConsecutiveEscalateFailed = 0
	}
	ledger[sig] = entry
	// Cap the ledger: without reliability wiring, signatures can churn — evict
	// the oldest attempts deterministically so ledger.json cannot grow forever.
	if len(ledger) > errorHandlerLedgerMaxEntries {
		type sigAge struct {
			sig string
			at  time.Time
		}
		entries := make([]sigAge, 0, len(ledger))
		for s, e := range ledger {
			entries = append(entries, sigAge{s, e.LastAttempt})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
		for _, e := range entries[:len(ledger)-errorHandlerLedgerMaxEntries] {
			delete(ledger, e.sig)
		}
	}
	_ = writeErrorHandlerJSON(errorHandlerLedgerPath(), ledger)
}

// errorHandlerClaudeLockStale bounds a crashed holder: the Claude call is
// capped at 180s, so anything older is abandoned and may be broken.
const errorHandlerClaudeLockStale = 10 * time.Minute

// errorHandlerStoreLockStale bounds a crashed store-lock holder: store writes
// are milliseconds, so anything older than this is a dead process.
const errorHandlerStoreLockStale = 10 * time.Second

// acquireErrorHandlerFileLock takes an O_CREATE|O_EXCL advisory lock file in
// the store dir, sweeping stale locks from crashed holders. On contention the
// caller skips its work this run — no waiting, no retries.
func acquireErrorHandlerFileLock(name string, stale time.Duration) (func(), bool) {
	dir := errorHandlerDir()
	if dir == "" {
		return nil, false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false
	}
	lockPath := filepath.Join(dir, name)
	if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > stale {
		_ = os.Remove(lockPath)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	_ = f.Close()
	return func() { _ = os.Remove(lockPath) }, true
}

// acquireErrorHandlerClaudeLock is the fleet-wide single-flight guard around a
// Claude proposal attempt (spec §3).
func acquireErrorHandlerClaudeLock() (func(), bool) {
	return acquireErrorHandlerFileLock("claude.lock", errorHandlerClaudeLockStale)
}

// acquireErrorHandlerStoreLock guards cross-process read-modify-write cycles
// on extensions.json/ledger.json — fleet siblings share the store, and an
// unguarded concurrent rewrite can clobber a just-appended extension or
// resurrect a Disabled flag.
func acquireErrorHandlerStoreLock() (func(), bool) {
	return acquireErrorHandlerFileLock("store.lock", errorHandlerStoreLockStale)
}
