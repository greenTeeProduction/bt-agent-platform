package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
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
}

// errorHandlerDisableAfter is the consecutive-failure streak that disables a
// generated extension (matches the platform's PatchBoard 3-window convention).
const errorHandlerDisableAfter = 3

// errorHandlerDirOverride redirects the store in tests (same var-override
// pattern as goapFusionVaultDir).
var errorHandlerDirOverride string

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
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, out)
}

// writeErrorHandlerJSON persists atomically per ADR-003: tmp file + rename.
func writeErrorHandlerJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadErrorHandlerExtensions(handlerName string) []ErrorHandlerExtension {
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
	path := errorHandlerExtensionsPath()
	all := map[string][]ErrorHandlerExtension{}
	readErrorHandlerJSON(path, &all)
	// Simple rollback story: keep the pre-append state as .bak.
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", data, 0o644)
	} else {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path+".bak", []byte("{}"), 0o644)
	}
	all[handlerName] = append(all[handlerName], ext)
	return writeErrorHandlerJSON(path, all)
}

// recordErrorHandlerResult updates success/failure counters for one extension
// and auto-disables it after errorHandlerDisableAfter consecutive failures.
// Best-effort: persistence errors are swallowed — counters are advisory.
func recordErrorHandlerResult(handlerName, nodeName string, success bool) {
	path := errorHandlerExtensionsPath()
	all := map[string][]ErrorHandlerExtension{}
	readErrorHandlerJSON(path, &all)
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
	ledger := map[string]errorHandlerLedgerEntry{}
	readErrorHandlerJSON(errorHandlerLedgerPath(), &ledger)
	entry, ok := ledger[sig]
	return entry, ok
}

func errorHandlerLedgerStamp(sig, verdict string) {
	ledger := map[string]errorHandlerLedgerEntry{}
	readErrorHandlerJSON(errorHandlerLedgerPath(), &ledger)
	entry := ledger[sig]
	entry.LastAttempt = time.Now()
	entry.Attempts++
	entry.LastVerdict = verdict
	ledger[sig] = entry
	_ = writeErrorHandlerJSON(errorHandlerLedgerPath(), ledger)
}

// errorHandlerClaudeLockStale bounds a crashed holder: the Claude call is
// capped at 180s, so anything older is abandoned and may be broken.
const errorHandlerClaudeLockStale = 10 * time.Minute

// acquireErrorHandlerClaudeLock is the fleet-wide single-flight guard around a
// Claude proposal attempt. On contention the caller skips the attempt this run
// (spec §3) — no waiting, no retries.
func acquireErrorHandlerClaudeLock() (func(), bool) {
	dir := errorHandlerDir()
	if dir == "" {
		return nil, false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false
	}
	lockPath := filepath.Join(dir, "claude.lock")
	if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > errorHandlerClaudeLockStale {
		_ = os.Remove(lockPath)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, false
	}
	_ = f.Close()
	return func() { _ = os.Remove(lockPath) }, true
}
