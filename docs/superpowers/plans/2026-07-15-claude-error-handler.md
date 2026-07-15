# ClaudeErrorHandler Node Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `ClaudeErrorHandler` decorator node that, on subtree failure, asks Claude Code (one guarded read-only CLI call) to propose a recovery node composed of registered actions/conditions, validates it strictly, persists it, grafts it into the tree at build time, and ticks it immediately.

**Architecture:** New engine node type following the `BuildReviewCycle` closure pattern (`internal/engine/review_cycle.go`). Extensions persist as ADR-003 atomic JSON under `~/.go-bt-evolve/error_handler/` and are re-grafted at every tree build (scheduled runs rebuild trees from the compiled catalog each run — `internal/agentexec/deps.go:56-61` — so persistence must be node-local). Every `AllDomainTrees()` root gets wrapped.

**Tech Stack:** Go 1.26, go-bt (`github.com/rvitorper/go-bt`), existing `ClaudeRunner` interface, stdlib only (no new dependencies).

**Spec:** `docs/superpowers/specs/2026-07-15-claude-error-handler-design.md` — read it first.

## Global Constraints

- Go binary: `/usr/local/go/bin/go` — NOT on PATH. Prefix every command: `PATH=/usr/local/go/bin:$PATH go test ...`
- Work in this worktree: `/home/nico/go-bt-evolve/.claude/worktrees/claude-error-handler` (branch `worktree-claude-error-handler`). Never touch the bare main repo.
- `internal/engine` must not import higher-level packages (`internal/agent`, `internal/domains`, ...). `internal/domains` must not import `internal/engine` — the domains wrap in Task 6 is pure `evolution.SerializableNode` data.
- Recovery success must NEVER set `bb.OutcomeRefinement` — the runner's healthy-outcome whitelist is exactly `success/no_change/degraded` (`internal/agent/runner.go:349-359`); a novel refinement gets dead-lettered as an error.
- No new persistence formats: JSON via tmp-file + `os.Rename` (see `internal/evolution/mutate.go:104`).
- Env knobs (exact names): `BT_CLAUDE_ERROR_HANDLER` (`off` disables Claude calls + grafting), `BT_ERROR_HANDLER_COOLDOWN` (Go duration, default `6h`), `BT_ERROR_HANDLER_MAX_NODES` (default `5`).
- Limits (exact values): proposal ≤ 10 nodes, depth ≤ 4, extension auto-disable after 3 consecutive failures, Claude timeout 180 s, error-text excerpt 200 chars, subtree JSON in prompt truncated to 4000 chars.
- KNOWN PRE-EXISTING FAILURE: `make test` (-race) is red in `internal/llm` (data race in `acp.go:123`, unrelated, being fixed separately). Never "fix" it in this branch; verify feature packages individually with `-race`.
- A tracked PostToolUse hook gofmts every edited `.go` file — formatting churn in diffs is expected.
- Known flake: `TestCMAESOptimizer_Convergence` (`internal/evolution`) is stochastic; retry once in isolation if it is the sole failure.
- Commit messages end with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`

---

### Task 1: Extension + ledger store

**Files:**
- Create: `internal/engine/error_handler_store.go`
- Test: `internal/engine/error_handler_store_test.go`

**Interfaces:**
- Consumes: nothing (stdlib + `internal/evolution` only).
- Produces (used by Tasks 4–5):
  - `type ErrorHandlerExtension struct { Node evolution.SerializableNode; Signature string; CreatedAt time.Time; Successes int; ConsecutiveFailures int; Disabled bool }` (all fields JSON-tagged snake_case)
  - `type errorHandlerLedgerEntry struct { LastAttempt time.Time; Attempts int; LastVerdict string }`
  - `var errorHandlerDirOverride string` (tests set to `t.TempDir()`)
  - `func errorHandlerDir() string`
  - `func loadErrorHandlerExtensions(handlerName string) []ErrorHandlerExtension` (all, incl. disabled)
  - `func activeErrorHandlerExtensions(handlerName string) []ErrorHandlerExtension` (filters `Disabled`)
  - `func appendErrorHandlerExtension(handlerName string, ext ErrorHandlerExtension) error`
  - `func recordErrorHandlerResult(handlerName, nodeName string, success bool)`
  - `func errorHandlerLedgerGet(sig string) (errorHandlerLedgerEntry, bool)`
  - `func errorHandlerLedgerStamp(sig, verdict string)`
  - `func acquireErrorHandlerClaudeLock() (release func(), ok bool)`

- [ ] **Step 1: Write the failing test**

```go
// internal/engine/error_handler_store_test.go
package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func withTempErrorHandlerDir(t *testing.T) {
	t.Helper()
	old := errorHandlerDirOverride
	errorHandlerDirOverride = t.TempDir()
	t.Cleanup(func() { errorHandlerDirOverride = old })
}

func TestErrorHandlerStore_AppendLoadRoundTrip(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{
		Node:      evolution.SerializableNode{Type: "Sequence", Name: "Handle_testcat"},
		Signature: "abc123def456",
	}
	if err := appendErrorHandlerExtension("tree_ErrorHandler", ext); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := loadErrorHandlerExtensions("tree_ErrorHandler")
	if len(got) != 1 || got[0].Node.Name != "Handle_testcat" || got[0].Signature != "abc123def456" {
		t.Fatalf("round trip = %+v", got)
	}
	if len(loadErrorHandlerExtensions("other_handler")) != 0 {
		t.Fatal("extensions must be keyed by handler name")
	}
	// Backup written before append
	if _, err := os.Stat(filepath.Join(errorHandlerDir(), "extensions.json.bak")); err != nil {
		t.Fatalf("expected extensions.json.bak: %v", err)
	}
}

func TestErrorHandlerStore_ConsecutiveFailuresDisable(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", false)
	if len(activeErrorHandlerExtensions("h")) != 1 {
		t.Fatal("2 consecutive failures must not disable")
	}
	recordErrorHandlerResult("h", "n1", false)
	if len(activeErrorHandlerExtensions("h")) != 0 {
		t.Fatal("3 consecutive failures must disable the extension")
	}
	all := loadErrorHandlerExtensions("h")
	if len(all) != 1 || !all[0].Disabled || all[0].ConsecutiveFailures != 3 {
		t.Fatalf("persisted state = %+v", all)
	}
}

func TestErrorHandlerStore_SuccessResetsFailureStreak(t *testing.T) {
	withTempErrorHandlerDir(t)
	ext := ErrorHandlerExtension{Node: evolution.SerializableNode{Type: "Sequence", Name: "n1"}}
	if err := appendErrorHandlerExtension("h", ext); err != nil {
		t.Fatal(err)
	}
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", false)
	recordErrorHandlerResult("h", "n1", true)
	all := loadErrorHandlerExtensions("h")
	if all[0].ConsecutiveFailures != 0 || all[0].Successes != 1 || all[0].Disabled {
		t.Fatalf("after success: %+v", all[0])
	}
}

func TestErrorHandlerLedger_StampAndGet(t *testing.T) {
	withTempErrorHandlerDir(t)
	if _, ok := errorHandlerLedgerGet("sig1"); ok {
		t.Fatal("empty ledger must miss")
	}
	errorHandlerLedgerStamp("sig1", "unresolvable")
	entry, ok := errorHandlerLedgerGet("sig1")
	if !ok || entry.Attempts != 1 || entry.LastVerdict != "unresolvable" || entry.LastAttempt.IsZero() {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}
	errorHandlerLedgerStamp("sig1", "proposed")
	entry, _ = errorHandlerLedgerGet("sig1")
	if entry.Attempts != 2 || entry.LastVerdict != "proposed" {
		t.Fatalf("after 2nd stamp: %+v", entry)
	}
}

func TestErrorHandlerClaudeLock_ContentionSkips(t *testing.T) {
	withTempErrorHandlerDir(t)
	release, ok := acquireErrorHandlerClaudeLock()
	if !ok {
		t.Fatal("first acquire must succeed")
	}
	if _, ok2 := acquireErrorHandlerClaudeLock(); ok2 {
		t.Fatal("second acquire while held must fail")
	}
	release()
	release2, ok3 := acquireErrorHandlerClaudeLock()
	if !ok3 {
		t.Fatal("acquire after release must succeed")
	}
	release2()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandler(Store|Ledger|ClaudeLock)' -count=1`
Expected: FAIL — `undefined: errorHandlerDirOverride`, `undefined: ErrorHandlerExtension`, etc.

- [ ] **Step 3: Write the implementation**

```go
// internal/engine/error_handler_store.go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandler(Store|Ledger|ClaudeLock)' -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/error_handler_store.go internal/engine/error_handler_store_test.go
git commit -m "feat(engine): error handler extension + ledger store

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Note: the pre-commit hook runs gofmt/vet/lint/short-tests — if it rejects, fix the reported issue; never `--no-verify` a code commit.

---

### Task 2: Parameterized error-guard conditions

**Files:**
- Create: `internal/engine/error_handler_conditions.go`
- Modify: `internal/engine/tree.go:391-406` (`conditionForName`)
- Test: `internal/engine/error_handler_conditions_test.go`

**Interfaces:**
- Consumes: `ConditionFunc` (`registry.go:25`), ChainState keys `last_error_category` / `last_error_node` (written by `recordNodeFailure`, `reliability_decorators.go:44-57`).
- Produces (used by Tasks 4–5): `func errorHandlerConditionFor(name string) ConditionFunc` returning non-nil for names with prefixes `LastErrorCategoryIs:` / `LastErrorNodeIs:`, nil otherwise. Constants `errorCategoryCondPrefix = "LastErrorCategoryIs:"`, `errorNodeCondPrefix = "LastErrorNodeIs:"`.

- [ ] **Step 1: Write the failing test**

```go
// internal/engine/error_handler_conditions_test.go
package engine

import "testing"

func TestErrorHandlerConditionFor(t *testing.T) {
	cases := []struct {
		name  string
		state map[string]any
		want  bool
	}{
		{"LastErrorCategoryIs:rate_limit", map[string]any{"last_error_category": "rate_limit"}, true},
		{"LastErrorCategoryIs:rate_limit", map[string]any{"last_error_category": "timeout"}, false},
		{"LastErrorCategoryIs:rate_limit", nil, false},
		{"LastErrorCategoryIs:", map[string]any{"last_error_category": ""}, false}, // malformed spec must not pass
		{"LastErrorNodeIs:FetchData", map[string]any{"last_error_node": "FetchData"}, true},
		{"LastErrorNodeIs:FetchData", map[string]any{"last_error_node": "Other"}, false},
	}
	for _, tc := range cases {
		fn := errorHandlerConditionFor(tc.name)
		if fn == nil {
			t.Fatalf("%s: expected a condition func", tc.name)
		}
		bb := &Blackboard{ChainState: tc.state}
		if got := fn(bb); got != tc.want {
			t.Errorf("%s with %v = %v, want %v", tc.name, tc.state, got, tc.want)
		}
	}
	if errorHandlerConditionFor("SomeRegularCondition") != nil {
		t.Fatal("non-prefixed names must return nil")
	}
}

func TestConditionForName_ResolvesErrorHandlerPrefixes(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"last_error_category": "testcat"}}
	fn := bb.conditionForName("LastErrorCategoryIs:testcat")
	if !fn(bb) {
		t.Fatal("conditionForName must route LastErrorCategoryIs: to the error-handler resolver")
	}
	// Must NOT fall through to the permissive always-true default:
	if bb.conditionForName("LastErrorCategoryIs:other")(bb) {
		t.Fatal("mismatched category must be false, not permissive-true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandlerConditionFor|TestConditionForName_Resolves' -count=1`
Expected: FAIL — `undefined: errorHandlerConditionFor`

- [ ] **Step 3: Write the implementation**

```go
// internal/engine/error_handler_conditions.go
package engine

import "strings"

// Name-parameterized guard conditions for Claude-proposed recovery nodes,
// mirroring the compiled-GOAP "GoapStateMatches:k=v" pattern
// (goap_compiled_nodes.go). They read the classified error state that
// recordNodeFailure (reliability_decorators.go) stores on the blackboard.
const (
	errorCategoryCondPrefix = "LastErrorCategoryIs:"
	errorNodeCondPrefix     = "LastErrorNodeIs:"
)

func errorHandlerConditionFor(name string) ConditionFunc {
	chainStateEquals := func(key, want string) ConditionFunc {
		return func(b *Blackboard) bool {
			if want == "" || b == nil || b.ChainState == nil {
				return false // malformed spec must not silently pass
			}
			got, _ := b.ChainState[key].(string)
			return got == want
		}
	}
	switch {
	case strings.HasPrefix(name, errorCategoryCondPrefix):
		return chainStateEquals("last_error_category", strings.TrimSpace(strings.TrimPrefix(name, errorCategoryCondPrefix)))
	case strings.HasPrefix(name, errorNodeCondPrefix):
		return chainStateEquals("last_error_node", strings.TrimSpace(strings.TrimPrefix(name, errorNodeCondPrefix)))
	}
	return nil
}
```

In `internal/engine/tree.go`, `conditionForName` (currently lines 391-406), insert the new resolver between the compiled-GOAP branch and the permissive default:

```go
	// Name-parameterized compiled-GOAP guards ("GoapStateMatches:k=v"),
	// emitted by the plan→BT compiler (goap_compiled_nodes.go).
	if fn := compiledGoapConditionFor(name); fn != nil {
		return tracedCondition(name, fn)
	}
	// Name-parameterized error-handler guards ("LastErrorCategoryIs:<cat>",
	// "LastErrorNodeIs:<node>") used by Claude-proposed recovery nodes.
	if fn := errorHandlerConditionFor(name); fn != nil {
		return tracedCondition(name, fn)
	}
	// Default: always-true condition (permissive routing)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandlerConditionFor|TestConditionForName_Resolves' -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/error_handler_conditions.go internal/engine/error_handler_conditions_test.go internal/engine/tree.go
git commit -m "feat(engine): parameterized LastErrorCategoryIs/LastErrorNodeIs guard conditions

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Registry name listings

**Files:**
- Modify: `internal/engine/registry.go` (below `GetCondition`, ~line 202)
- Test: `internal/engine/error_handler_registry_test.go` (new file; `registry.go` has no dedicated test file)

**Interfaces:**
- Consumes: private `actionRegistry` / `conditionRegistry` maps + `regMu` (`registry.go:29-33`).
- Produces (used by Task 4): `func RegisteredActionNames() []string`, `func RegisteredConditionNames() []string` — sorted, snapshot copies.

- [ ] **Step 1: Write the failing test**

```go
// internal/engine/error_handler_registry_test.go
package engine

import (
	"sort"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegisteredNames_SortedAndComplete(t *testing.T) {
	RegisterAction("test_registered_names_probe_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	RegisterCondition("test_registered_names_probe_condition", func(*Blackboard) bool { return true })

	actions := RegisteredActionNames()
	if !sort.StringsAreSorted(actions) {
		t.Fatal("action names must be sorted")
	}
	conditions := RegisteredConditionNames()
	if !sort.StringsAreSorted(conditions) {
		t.Fatal("condition names must be sorted")
	}
	contains := func(names []string, want string) bool {
		i := sort.SearchStrings(names, want)
		return i < len(names) && names[i] == want
	}
	if !contains(actions, "test_registered_names_probe_action") {
		t.Fatal("registered action missing from listing")
	}
	if !contains(conditions, "test_registered_names_probe_condition") {
		t.Fatal("registered condition missing from listing")
	}
}
```

(Registrations are global and panic on duplicates — the probe names are unique to this test and registered exactly once.)

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestRegisteredNames_SortedAndComplete -count=1`
Expected: FAIL — `undefined: RegisteredActionNames`

- [ ] **Step 3: Write the implementation** (append after `GetCondition` in `registry.go`; add `"sort"` to imports)

```go
// RegisteredActionNames returns a sorted snapshot of all registered action
// names — the composition vocabulary offered to the ClaudeErrorHandler node's
// proposal prompt and checked by its strict validator.
func RegisteredActionNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(actionRegistry))
	for name := range actionRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisteredConditionNames returns a sorted snapshot of all registered
// condition names.
func RegisteredConditionNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(conditionRegistry))
	for name := range conditionRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestRegisteredNames_SortedAndComplete -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/registry.go internal/engine/error_handler_registry_test.go
git commit -m "feat(engine): sorted registry name listings for error-handler prompts

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Signature, prompt, proposal parsing, strict validation

**Files:**
- Create: `internal/engine/error_handler_claude.go`
- Test: `internal/engine/error_handler_claude_test.go`

**Interfaces:**
- Consumes: `ClaudeRunner`/`execClaudeRunner`/`CommandResult` (`superpowers_runner.go`), `resolvedSuperpowersClaudeModel` (implicit via `execClaudeRunner`), `goapFusionRepo` (`actions_goap_fusion.go:25`), `GetAction`/`GetCondition`, `RegisteredActionNames`/`RegisteredConditionNames` (Task 3), `errorHandlerConditionFor` (Task 2), `errorHandlerLedgerStamp` (Task 1), `evolution.CountNodes` (`mutate.go`).
- Produces (used by Task 5):
  - `type errorHandlerProposal struct { Resolvable bool; Reason string; Node *evolution.SerializableNode }` (JSON tags `resolvable`, `reason`, `node`)
  - `func errorHandlerSignatureFromBB(b *Blackboard, handlerName string) string`
  - `func requestErrorHandlerProposal(handlerName string, failing *evolution.SerializableNode, b *Blackboard, sig string) (errorHandlerProposal, error)` — stamps the ledger on every outcome (`proposed` / `unresolvable` / `error`)
  - `func parseErrorHandlerProposal(output string) (errorHandlerProposal, error)`
  - `func validateErrorHandlerProposal(node *evolution.SerializableNode, takenNames map[string]bool) error`
  - `func firstTickedLeaf(n *evolution.SerializableNode) *evolution.SerializableNode`
  - `var errorHandlerClaudeRunner ClaudeRunner` (swappable in tests)
  - `func errorHandlerEnabled() bool`, `func errorHandlerCooldown() time.Duration`, `func errorHandlerMaxNodes() int`

- [ ] **Step 1: Write the failing test**

```go
// internal/engine/error_handler_claude_test.go
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestErrorHandlerSignature_StableAndDigitInsensitive(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{
		"last_error_category": "rate_limit",
		"last_error_node":     "CallClaude",
		"last_error":          "429 after 17 attempts at 2026-07-15T17:32:37",
	}}
	sig1 := errorHandlerSignatureFromBB(bb, "h")
	bb.ChainState["last_error"] = "429 after 99 attempts at 2026-07-16T09:00:00"
	sig2 := errorHandlerSignatureFromBB(bb, "h")
	if sig1 != sig2 {
		t.Fatalf("digit-only differences must not change the signature: %s vs %s", sig1, sig2)
	}
	if len(sig1) != 12 {
		t.Fatalf("signature length = %d, want 12", len(sig1))
	}
	bb.ChainState["last_error_category"] = "timeout"
	if errorHandlerSignatureFromBB(bb, "h") == sig1 {
		t.Fatal("different category must change the signature")
	}
}

func TestErrorHandlerSignature_FallsBackToResult(t *testing.T) {
	bb := &Blackboard{Result: "boom failure text"}
	if errorHandlerSignatureFromBB(bb, "h") == "" {
		t.Fatal("must derive a signature from bb.Result when ChainState is empty")
	}
}

func TestParseErrorHandlerProposal(t *testing.T) {
	out := "Here is my analysis.\n```json\n{\"resolvable\": true, \"node\": {\"type\": \"Sequence\", \"name\": \"Handle_x\"}}\n```\n"
	p, err := parseErrorHandlerProposal(out)
	if err != nil || !p.Resolvable || p.Node == nil || p.Node.Name != "Handle_x" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	p, err = parseErrorHandlerProposal(`{"resolvable": false, "reason": "needs new Go code"}`)
	if err != nil || p.Resolvable || p.Reason == "" {
		t.Fatalf("unresolvable parse: p=%+v err=%v", p, err)
	}
	if _, err = parseErrorHandlerProposal("no json here"); err == nil {
		t.Fatal("garbage must error")
	}
	if _, err = parseErrorHandlerProposal(`{"resolvable": true}`); err == nil {
		t.Fatal("resolvable without node must error")
	}
}

func guardedSeq(guard, action string) *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "Handle_test", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: guard},
		{Type: "Action", Name: action},
	}}
}

func TestValidateErrorHandlerProposal(t *testing.T) {
	RegisterAction("eh_validate_known_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	valid := guardedSeq("LastErrorCategoryIs:testcat", "eh_validate_known_action")
	if err := validateErrorHandlerProposal(valid, map[string]bool{}); err != nil {
		t.Fatalf("valid proposal rejected: %v", err)
	}
	cases := []struct {
		desc string
		node *evolution.SerializableNode
	}{
		{"nil node", nil},
		{"unknown action", guardedSeq("LastErrorCategoryIs:x", "definitely_not_registered_xyz")},
		{"disallowed node type", &evolution.SerializableNode{Type: "HumanApprovalGate", Name: "n"}},
		{"unknown node type", &evolution.SerializableNode{Type: "Bogus", Name: "n"}},
		{"first leaf not a guard", &evolution.SerializableNode{Type: "Sequence", Name: "n", Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_validate_known_action"},
		}}},
		{"empty name", func() *evolution.SerializableNode { n := guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action"); n.Name = ""; return n }()},
		{"taken name", guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action")},
	}
	for _, tc := range cases {
		names := map[string]bool{}
		if tc.desc == "taken name" {
			names["Handle_test"] = true
		}
		if err := validateErrorHandlerProposal(tc.node, names); err == nil {
			t.Errorf("%s: expected rejection", tc.desc)
		}
	}
	// Size cap: a chain of 11 nested sequences exceeds maxProposalNodes=10.
	deep := &evolution.SerializableNode{Type: "Condition", Name: "LastErrorCategoryIs:x"}
	node := *deep
	for i := 0; i < 10; i++ {
		node = evolution.SerializableNode{Type: "Sequence", Name: "s" + strings.Repeat("x", i+1), Children: []evolution.SerializableNode{node}}
	}
	if err := validateErrorHandlerProposal(&node, map[string]bool{}); err == nil {
		t.Error("oversized/deep proposal must be rejected")
	}
}

func TestErrorHandlerConfigDefaults(t *testing.T) {
	if !errorHandlerEnabled() {
		t.Fatal("enabled by default")
	}
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	if errorHandlerEnabled() {
		t.Fatal("BT_CLAUDE_ERROR_HANDLER=off must disable")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "90m")
	if errorHandlerCooldown() != 90*time.Minute {
		t.Fatal("cooldown env override")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "bogus")
	if errorHandlerCooldown() != 6*time.Hour {
		t.Fatal("cooldown default on parse failure")
	}
	t.Setenv("BT_ERROR_HANDLER_MAX_NODES", "2")
	if errorHandlerMaxNodes() != 2 {
		t.Fatal("max nodes env override")
	}
}
```

(Imports for this test file: `strings`, `testing`, `time`, `github.com/nico/go-bt-evolve/internal/evolution`, and `btcore "github.com/rvitorper/go-bt/core"`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandlerSignature|TestParseErrorHandlerProposal|TestValidateErrorHandlerProposal|TestErrorHandlerConfigDefaults' -count=1`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Write the implementation**

```go
// internal/engine/error_handler_claude.go
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

const (
	// errorHandlerAllowedTools keeps the proposal run read-only: Claude
	// proposes a node as JSON; it never edits the repo (spec §4).
	errorHandlerAllowedTools  = "Read,Glob,Grep"
	errorHandlerClaudeTimeout = 180 * time.Second
	errorHandlerMaxProposal   = 10 // max nodes in a proposal
	errorHandlerMaxDepth      = 4
	errorHandlerErrExcerpt    = 200  // chars of error text in the signature
	errorHandlerSubtreeLimit  = 4000 // chars of subtree JSON in the prompt
)

// errorHandlerClaudeRunner is swappable in tests (same seam pattern as
// defaultSuperpowersClaudeRunner).
var errorHandlerClaudeRunner ClaudeRunner = execClaudeRunner{AllowedTools: errorHandlerAllowedTools}

func errorHandlerEnabled() bool {
	return !strings.EqualFold(os.Getenv("BT_CLAUDE_ERROR_HANDLER"), "off")
}

func errorHandlerCooldown() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("BT_ERROR_HANDLER_COOLDOWN")); err == nil && d > 0 {
		return d
	}
	return 6 * time.Hour
}

func errorHandlerMaxNodes() int {
	if n, err := strconv.Atoi(os.Getenv("BT_ERROR_HANDLER_MAX_NODES")); err == nil && n > 0 {
		return n
	}
	return 5
}

func stripDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return -1
		}
		return r
	}, s)
}

// errorHandlerSignatureFromBB identifies an error class: same tree + failing
// node + category + digit-stripped error text prefix ⇒ same signature, so
// timestamps/counters in messages don't defeat the cooldown ledger.
func errorHandlerSignatureFromBB(b *Blackboard, handlerName string) string {
	var cat, node, errText string
	if b.ChainState != nil {
		cat, _ = b.ChainState["last_error_category"].(string)
		node, _ = b.ChainState["last_error_node"].(string)
		errText, _ = b.ChainState["last_error"].(string)
	}
	if errText == "" {
		errText = b.Result
	}
	if len(errText) > errorHandlerErrExcerpt {
		errText = errText[:errorHandlerErrExcerpt]
	}
	sum := sha256.Sum256([]byte(handlerName + "|" + node + "|" + cat + "|" + stripDigits(errText)))
	return hex.EncodeToString(sum[:])[:12]
}

type errorHandlerProposal struct {
	Resolvable bool                        `json:"resolvable"`
	Reason     string                      `json:"reason"`
	Node       *evolution.SerializableNode `json:"node"`
}

// parseErrorHandlerProposal extracts the first parseable JSON object from
// Claude's output (which may wrap it in prose or ```json fences).
func parseErrorHandlerProposal(output string) (errorHandlerProposal, error) {
	rest := output
	for try := 0; try < 5; try++ {
		idx := strings.Index(rest, "{")
		if idx < 0 {
			break
		}
		rest = rest[idx:]
		var p errorHandlerProposal
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&p); err == nil {
			if p.Resolvable && p.Node == nil {
				return errorHandlerProposal{}, fmt.Errorf("claude proposal marked resolvable but has no node")
			}
			return p, nil
		}
		rest = rest[1:]
	}
	return errorHandlerProposal{}, fmt.Errorf("no parseable JSON proposal in claude output (%d bytes)", len(output))
}

// errorHandlerAllowedNodeTypes is the strict proposal vocabulary (spec §5).
// Deliberately a subset of evolution.KnownNodeTypes: no gates, no subtrees,
// no planners — a recovery node composes existing leaves under basic control
// flow. (MemSequence/MemSelector are absent from KnownNodeTypes, so they
// could never validate anyway.)
var errorHandlerAllowedNodeTypes = map[string]bool{
	"Sequence": true, "Selector": true,
	"Retry": true, "Timeout": true, "Inverter": true, "Succeeder": true,
	"Action": true, "Condition": true, "AlwaysSucceed": true,
}

// firstTickedLeaf follows first children down to the leaf a tick reaches
// first — the proposal's guard position.
func firstTickedLeaf(n *evolution.SerializableNode) *evolution.SerializableNode {
	if n == nil {
		return nil
	}
	switch n.Type {
	case "Action", "Condition", "AlwaysSucceed":
		return n
	}
	if len(n.Children) == 0 {
		return nil
	}
	return firstTickedLeaf(&n.Children[0])
}

func errorHandlerNodeDepth(n *evolution.SerializableNode) int {
	if n == nil {
		return 0
	}
	deepest := 0
	for i := range n.Children {
		if d := errorHandlerNodeDepth(&n.Children[i]); d > deepest {
			deepest = d
		}
	}
	return 1 + deepest
}

// validateErrorHandlerProposal is the strict gate before any graft: the
// engine's permissive unknown-name fallback (tree.go actionForName) is
// explicitly NOT acceptable for generated nodes — every leaf must resolve.
func validateErrorHandlerProposal(node *evolution.SerializableNode, takenNames map[string]bool) error {
	if node == nil {
		return fmt.Errorf("proposal node is nil")
	}
	if strings.TrimSpace(node.Name) == "" {
		return fmt.Errorf("proposal node must have a name")
	}
	if takenNames[node.Name] {
		return fmt.Errorf("proposal name %q already exists on this handler", node.Name)
	}
	if errs := node.Validate(); len(errs) > 0 {
		return fmt.Errorf("proposal failed tree validation: %v", errs)
	}
	if count := evolution.CountNodes(node); count > errorHandlerMaxProposal {
		return fmt.Errorf("proposal has %d nodes, max %d", count, errorHandlerMaxProposal)
	}
	if depth := errorHandlerNodeDepth(node); depth > errorHandlerMaxDepth {
		return fmt.Errorf("proposal depth %d exceeds max %d", depth, errorHandlerMaxDepth)
	}
	guard := firstTickedLeaf(node)
	if guard == nil || guard.Type != "Condition" {
		return fmt.Errorf("proposal's first-ticked leaf must be a Condition guard")
	}
	var walk func(n *evolution.SerializableNode) error
	walk = func(n *evolution.SerializableNode) error {
		if !errorHandlerAllowedNodeTypes[n.Type] {
			return fmt.Errorf("node type %q not allowed in proposals", n.Type)
		}
		switch n.Type {
		case "Action":
			if GetAction(n.Name) == nil {
				return fmt.Errorf("action %q is not registered", n.Name)
			}
		case "Condition":
			if GetCondition(n.Name) == nil && errorHandlerConditionFor(n.Name) == nil {
				return fmt.Errorf("condition %q is not registered", n.Name)
			}
		}
		for i := range n.Children {
			if err := walk(&n.Children[i]); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(node)
}

func buildErrorHandlerPrompt(handlerName string, failing *evolution.SerializableNode, b *Blackboard) string {
	var cat, errNode, errText string
	if b.ChainState != nil {
		cat, _ = b.ChainState["last_error_category"].(string)
		errNode, _ = b.ChainState["last_error_node"].(string)
		errText, _ = b.ChainState["last_error"].(string)
	}
	if errText == "" {
		errText = b.Result
	}
	subtree, _ := json.MarshalIndent(failing, "", "  ")
	subtreeStr := string(subtree)
	if len(subtreeStr) > errorHandlerSubtreeLimit {
		subtreeStr = subtreeStr[:errorHandlerSubtreeLimit] + "\n… (truncated)"
	}
	allowed := make([]string, 0, len(errorHandlerAllowedNodeTypes))
	for t := range errorHandlerAllowedNodeTypes {
		allowed = append(allowed, t)
	}
	return fmt.Sprintf(`You are the error handler for a Go behavior-tree agent platform. A subtree failed and you may propose ONE recovery node to handle this class of error in future runs.

## Failure context
- Handler: %s
- Failing node: %s
- Error category: %s
- Failure count this run: %d
- Error text:
%s

## Failing subtree (JSON)
%s

## Rules for your proposal
- Compose ONLY registered action/condition names listed below — you cannot invent new behavior.
- The node must be a guard-first composition: its first-ticked leaf MUST be a Condition, typically "LastErrorCategoryIs:%s" or "LastErrorNodeIs:%s", so it never fires on unrelated failures.
- Allowed node types: %s
- Max 10 nodes, max depth 4. Give the root and every composite a short unique descriptive name.
- Node JSON shape: {"type": "...", "name": "...", "children": [...], "max_retries": N (Retry only), "timeout_ms": N (Timeout only)}

## Registered actions
%s

## Registered conditions
%s
(Parameterized conditions also available: "LastErrorCategoryIs:<category>", "LastErrorNodeIs:<node-name>")

## Reply contract
Reply with ONLY one JSON object, no prose:
{"resolvable": true, "reason": "<why this handles the error>", "node": {…}}
or, if this error cannot be handled by composing the registered vocabulary:
{"resolvable": false, "reason": "<what capability is missing>"}`,
		handlerName, errNode, cat, b.FailureCount, errText, subtreeStr, cat, errNode,
		strings.Join(allowed, ", "),
		strings.Join(RegisteredActionNames(), ", "),
		strings.Join(RegisteredConditionNames(), ", "))
}

// requestErrorHandlerProposal makes the single guarded Claude call and stamps
// the ledger on EVERY outcome so the cooldown always engages.
func requestErrorHandlerProposal(handlerName string, failing *evolution.SerializableNode, b *Blackboard, sig string) (errorHandlerProposal, error) {
	ctx, cancel := context.WithTimeout(context.Background(), errorHandlerClaudeTimeout)
	defer cancel()
	res := errorHandlerClaudeRunner.RunClaude(ctx, goapFusionRepo, buildErrorHandlerPrompt(handlerName, failing, b))
	if res.Err != nil {
		errorHandlerLedgerStamp(sig, "error")
		return errorHandlerProposal{}, fmt.Errorf("claude error-handler call failed: %w", res.Err)
	}
	p, err := parseErrorHandlerProposal(res.Output)
	if err != nil {
		errorHandlerLedgerStamp(sig, "error")
		return errorHandlerProposal{}, err
	}
	if !p.Resolvable {
		errorHandlerLedgerStamp(sig, "unresolvable")
		Warn("claude error handler: error judged unresolvable with registered vocabulary",
			"handler", handlerName, "signature", sig, "reason", p.Reason)
		return p, nil
	}
	errorHandlerLedgerStamp(sig, "proposed")
	return p, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestErrorHandlerSignature|TestParseErrorHandlerProposal|TestValidateErrorHandlerProposal|TestErrorHandlerConfigDefaults' -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/error_handler_claude.go internal/engine/error_handler_claude_test.go
git commit -m "feat(engine): error-handler signature, prompt, proposal parsing and strict validation

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: The ClaudeErrorHandler node

**Files:**
- Create: `internal/engine/error_handler_node.go`
- Modify: `internal/evolution/node_types.go` (KnownNodeTypes map, after `"CheckpointVerifier": true`)
- Modify: `internal/engine/tree.go` build switch (add case next to `case "CheckpointVerifier":`)
- Test: `internal/engine/error_handler_node_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4 plus `buildNode` (`tree.go:225`), `btleaf.NewAction`, `Info`/`Warn` loggers.
- Produces: `func BuildClaudeErrorHandler(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard]`; `KnownNodeTypes["ClaudeErrorHandler"] = true`; ChainState key `error_handler_recovered` (signature string) + `## Error Handler Recovery` section appended to `bb.Result` on recovery.

- [ ] **Step 1: Write the failing test**

```go
// internal/engine/error_handler_node_test.go
package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// fakeClaudeRunner returns a canned proposal and counts invocations.
type fakeClaudeRunner struct {
	calls  atomic.Int64
	output string
	err    error
}

func (f *fakeClaudeRunner) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	f.calls.Add(1)
	return CommandResult{Output: f.output, Err: f.err}
}

func swapErrorHandlerRunner(t *testing.T, r ClaudeRunner) {
	t.Helper()
	old := errorHandlerClaudeRunner
	errorHandlerClaudeRunner = r
	t.Cleanup(func() { errorHandlerClaudeRunner = old })
}

var ehTestRecoverRan atomic.Int64

func init() {
	RegisterAction("eh_test_failing_action", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["last_error_category"] = "testcat"
		b.ChainState["last_error_node"] = "eh_test_failing_action"
		b.ChainState["last_error"] = "synthetic failure 42"
		return -1
	})
	RegisterAction("eh_test_recover_action", func(ctx *btcore.BTContext[Blackboard]) int {
		ehTestRecoverRan.Add(1)
		return 1
	})
}

func ehTestHandlerNode() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "ClaudeErrorHandler",
		Name: "eh_test_tree_ErrorHandler",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_test_failing_action"},
		},
	}
}

func ehTestProposalJSON(t *testing.T) string {
	t.Helper()
	prop := map[string]any{
		"resolvable": true,
		"reason":     "guarded recovery",
		"node": map[string]any{
			"type": "Sequence", "name": "Handle_testcat",
			"children": []map[string]any{
				{"type": "Condition", "name": "LastErrorCategoryIs:testcat"},
				{"type": "Action", "name": "eh_test_recover_action"},
			},
		},
	}
	data, err := json.Marshal(prop)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runHandler(t *testing.T, bb *Blackboard) int {
	t.Helper()
	cmd := BuildClaudeErrorHandler(ehTestHandlerNode(), bb)
	return cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb})
}

func TestClaudeErrorHandler_ProposalGraftedAndTicked(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	ehTestRecoverRan.Store(0)

	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != 1 {
		t.Fatalf("recovered run must return 1, got %d", code)
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("exactly one Claude call, got %d", fake.calls.Load())
	}
	if ehTestRecoverRan.Load() != 1 {
		t.Fatal("recovery action must have ticked")
	}
	if sig, _ := bb.ChainState["error_handler_recovered"].(string); sig == "" {
		t.Fatal("recovery must stamp error_handler_recovered")
	}
	if !strings.Contains(bb.Result, "## Error Handler Recovery") {
		t.Fatal("recovery must append a Result note")
	}
	if bb.OutcomeRefinement != "" {
		t.Fatal("recovery must NOT set OutcomeRefinement (runner dead-letters novel refinements)")
	}
	exts := loadErrorHandlerExtensions("eh_test_tree_ErrorHandler")
	if len(exts) != 1 || exts[0].Node.Name != "Handle_testcat" || exts[0].Successes != 1 {
		t.Fatalf("persisted extension = %+v", exts)
	}
}

func TestClaudeErrorHandler_GraftedExtensionHandlesNextRunWithoutClaude(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != 1 {
		t.Fatal("first run must recover")
	}
	// Fresh build (simulates the next scheduled run): extension grafted from
	// the store, error handled with ZERO further Claude calls.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != 1 {
		t.Fatal("second run must recover via the grafted extension")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("no second Claude call expected, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_UnresolvableStampsCooldownAndPassesFailureThrough(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": false, "reason": "needs new Go action"}`}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("unresolvable must pass the failure through")
	}
	// Same error again within cooldown: no second Claude call.
	bb2 := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb2); code != -1 {
		t.Fatal("still failing")
	}
	if fake.calls.Load() != 1 {
		t.Fatalf("cooldown must suppress the second call, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_InvalidProposalRejected(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: `{"resolvable": true, "node": {"type": "Action", "name": "not_registered_anywhere"}}`}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("invalid proposal must fail through")
	}
	if len(loadErrorHandlerExtensions("eh_test_tree_ErrorHandler")) != 0 {
		t.Fatal("rejected proposal must not be persisted")
	}
	if entry, ok := errorHandlerLedgerGet(errorHandlerSignatureFromBB(bb, "eh_test_tree_ErrorHandler")); !ok || entry.LastVerdict != "rejected" {
		t.Fatalf("ledger verdict = %+v ok=%v, want rejected", entry, ok)
	}
}

func TestClaudeErrorHandler_KillSwitch(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("kill switch must pass failure through")
	}
	if fake.calls.Load() != 0 {
		t.Fatal("kill switch must prevent Claude calls")
	}
}

func TestClaudeErrorHandler_CapReachedSkipsClaude(t *testing.T) {
	withTempErrorHandlerDir(t)
	t.Setenv("BT_ERROR_HANDLER_MAX_NODES", "1")
	// Pre-seed one active extension whose guard does NOT match this error, so
	// it can't recover — with the cap at 1, no Claude call may follow.
	seed := ErrorHandlerExtension{Node: evolution.SerializableNode{
		Type: "Sequence", Name: "Handle_othercat",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "LastErrorCategoryIs:othercat"},
			{Type: "Action", Name: "eh_test_recover_action"},
		},
	}, Signature: "seedsig000000"}
	if err := appendErrorHandlerExtension("eh_test_tree_ErrorHandler", seed); err != nil {
		t.Fatal(err)
	}
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{ChainState: map[string]any{}}
	if code := runHandler(t, bb); code != -1 {
		t.Fatal("cap reached with no matching recovery must fail through")
	}
	if fake.calls.Load() != 0 {
		t.Fatalf("cap must suppress the Claude call, got %d", fake.calls.Load())
	}
}

func TestClaudeErrorHandler_SandboxPassthrough(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	bb := &Blackboard{Sandbox: true, ChainState: map[string]any{}}
	code := runHandler(t, bb)
	if fake.calls.Load() != 0 {
		t.Fatal("sandbox mode must never call Claude")
	}
	if code != 1 { // sandbox stubs all actions to success
		t.Fatalf("sandbox passthrough code = %d", code)
	}
}

func TestClaudeErrorHandler_SuccessPassthroughUntouched(t *testing.T) {
	withTempErrorHandlerDir(t)
	fake := &fakeClaudeRunner{output: ehTestProposalJSON(t)}
	swapErrorHandlerRunner(t, fake)
	node := &evolution.SerializableNode{
		Type: "ClaudeErrorHandler", Name: "ok_ErrorHandler",
		Children: []evolution.SerializableNode{{Type: "AlwaysSucceed", Name: "ok"}},
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	cmd := BuildClaudeErrorHandler(node, bb)
	if code := cmd.Run(&btcore.BTContext[Blackboard]{Blackboard: bb}); code != 1 {
		t.Fatal("success must pass through")
	}
	if fake.calls.Load() != 0 {
		t.Fatal("no Claude on success")
	}
}

func TestClaudeErrorHandler_BuildSwitchAndValidation(t *testing.T) {
	withTempErrorHandlerDir(t)
	if !evolution.KnownNodeTypes["ClaudeErrorHandler"] {
		t.Fatal("ClaudeErrorHandler must be in KnownNodeTypes")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	if _, err := BuildAndValidate(ehTestHandlerNode(), bb); err != nil {
		t.Fatalf("BuildAndValidate must accept the node type: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestClaudeErrorHandler -count=1`
Expected: FAIL — `undefined: BuildClaudeErrorHandler`

- [ ] **Step 3: Write the implementation**

`internal/evolution/node_types.go` — inside the `KnownNodeTypes` literal, after `"CheckpointVerifier": true,`:

```go
	// ClaudeErrorHandler — self-extending recovery decorator: child 0 is the
	// protected subtree; further children are Claude-proposed recovery nodes
	// grafted at build time (engine/error_handler_node.go).
	"ClaudeErrorHandler": true,
```

`internal/engine/tree.go` — in the build switch, after `case "HumanApprovalGate":`:

```go
	case "ClaudeErrorHandler":
		return BuildClaudeErrorHandler(node, bb)
```

```go
// internal/engine/error_handler_node.go
package engine

import (
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildClaudeErrorHandler builds the self-extending recovery decorator
// (spec: docs/superpowers/specs/2026-07-15-claude-error-handler-design.md).
//
// Child 0 is the protected subtree. Persisted extensions (Claude-proposed
// recovery nodes) are re-grafted here on every build because scheduled runs
// rebuild trees from the compiled catalog each run. On child failure the
// handler ticks matching extensions guard-first; if none handle it, it makes
// ONE guarded read-only Claude call proposing a new recovery node, validates
// it strictly, persists it, ticks it immediately, and otherwise passes the
// failure through unchanged.
func BuildClaudeErrorHandler(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "ClaudeErrorHandler requires a protected child"
			return -1
		})
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	// Sandbox (gardener structural scoring) must never call Claude or touch
	// the store — behave as a transparent passthrough.
	if bb.Sandbox {
		return child
	}
	handlerName := node.Name
	protected := node.Children[0]

	type recovery struct {
		name  string
		guard string // first-ticked Condition name; evaluated before the tick
		cmd   btcore.Command[Blackboard]
	}
	buildRecovery := func(ext ErrorHandlerExtension) recovery {
		extNode := ext.Node
		guard := ""
		if leaf := firstTickedLeaf(&extNode); leaf != nil && leaf.Type == "Condition" {
			guard = leaf.Name
		}
		return recovery{name: extNode.Name, guard: guard, cmd: buildNode(&extNode, bb, handlerName)}
	}
	var recoveries []recovery
	takenNames := map[string]bool{protected.Name: true}
	for _, ext := range activeErrorHandlerExtensions(handlerName) {
		recoveries = append(recoveries, buildRecovery(ext))
		takenNames[ext.Node.Name] = true
	}

	markRecovered := func(b *Blackboard, nodeName, sig string) {
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState["error_handler_recovered"] = sig
		b.Result += fmt.Sprintf("\n\n## Error Handler Recovery\nHandler %s recovered via generated node %s (error signature %s).\n", handlerName, nodeName, sig)
		Info("claude error handler: recovered", "handler", handlerName, "node", nodeName, "signature", sig)
	}

	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		code := child.Run(ctx)
		if code >= 0 {
			return code
		}
		b := ctx.Blackboard
		sig := errorHandlerSignatureFromBB(b, handlerName)
		// 1. Existing recovery extensions, guard-first. The guard is evaluated
		// separately from the tick so a guard mismatch (expected on unrelated
		// errors) never counts as a recovery failure toward auto-disable.
		for _, r := range recoveries {
			if r.guard != "" && !b.conditionForName(r.guard)(b) {
				continue
			}
			if r.cmd.Run(ctx) == 1 {
				recordErrorHandlerResult(handlerName, r.name, true)
				markRecovered(b, r.name, sig)
				return 1
			}
			recordErrorHandlerResult(handlerName, r.name, false)
		}
		// 2. Maybe grow a new handler — every guard must pass first.
		if !errorHandlerEnabled() {
			return -1
		}
		if len(activeErrorHandlerExtensions(handlerName)) >= errorHandlerMaxNodes() {
			return -1
		}
		if entry, ok := errorHandlerLedgerGet(sig); ok && time.Since(entry.LastAttempt) < errorHandlerCooldown() {
			return -1
		}
		release, ok := acquireErrorHandlerClaudeLock()
		if !ok {
			return -1 // another agent is already consulting Claude — skip this run
		}
		defer release()
		prop, err := requestErrorHandlerProposal(handlerName, &protected, b, sig)
		if err != nil || !prop.Resolvable {
			return -1
		}
		if err := validateErrorHandlerProposal(prop.Node, takenNames); err != nil {
			errorHandlerLedgerStamp(sig, "rejected")
			Warn("claude error handler: proposal rejected", "handler", handlerName, "signature", sig, "err", err)
			return -1
		}
		ext := ErrorHandlerExtension{Node: *prop.Node, Signature: sig, CreatedAt: time.Now()}
		if err := appendErrorHandlerExtension(handlerName, ext); err != nil {
			Warn("claude error handler: persist failed", "handler", handlerName, "err", err)
			return -1
		}
		Info("claude error handler: tree extended with generated recovery node",
			"handler", handlerName, "node", prop.Node.Name, "signature", sig, "reason", prop.Reason)
		r := buildRecovery(ext)
		recoveries = append(recoveries, r)
		takenNames[r.name] = true
		if r.cmd.Run(ctx) == 1 {
			recordErrorHandlerResult(handlerName, r.name, true)
			markRecovered(b, r.name, sig)
			return 1
		}
		recordErrorHandlerResult(handlerName, r.name, false)
		return -1
	})
}
```

Implementation notes for this step:
- If `Info`/`Warn` don't exist with that signature in `internal/engine` (check `app_logger.go`), use whatever structured log helpers that file provides — same call sites, adjusted names.
- The `recoveries` slice is closure state per built tree — mutating it after a graft gives same-run reuse; cross-run reuse comes from the store re-graft.

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestClaudeErrorHandler -count=1 -race`
Expected: PASS

- [ ] **Step 5: Run the full engine + evolution packages**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine ./internal/evolution -short -count=1 -race`
Expected: PASS (retry `TestCMAESOptimizer_Convergence` once in isolation if it is the sole failure)

- [ ] **Step 6: Commit**

```bash
git add internal/engine/error_handler_node.go internal/engine/error_handler_node_test.go internal/engine/tree.go internal/evolution/node_types.go
git commit -m "feat(engine): ClaudeErrorHandler self-extending recovery node

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Wrap all domain trees

**Files:**
- Modify: `internal/domains/trees.go` (`AllDomainTrees()`, ~line 733)
- Test: `internal/domains/error_handler_wrap_test.go` (new)

**Interfaces:**
- Consumes: `evolution.SerializableNode` only — domains must NOT import engine (test-build cycle; see `goap_fusion_wire_seam_test.go` header comment).
- Produces: every `AllDomainTrees()` value has root `Type == "ClaudeErrorHandler"`, `Name == <treeName>+"_ErrorHandler"`, exactly one child (the previous root).

- [ ] **Step 1: Write the failing test**

```go
// internal/domains/error_handler_wrap_test.go
package domains

import "testing"

func TestAllDomainTreesWrappedInClaudeErrorHandler(t *testing.T) {
	for name, tree := range AllDomainTrees() {
		if tree == nil {
			t.Errorf("%s: nil tree", name)
			continue
		}
		if tree.Type != "ClaudeErrorHandler" {
			t.Errorf("%s: root type = %q, want ClaudeErrorHandler", name, tree.Type)
			continue
		}
		if want := name + "_ErrorHandler"; tree.Name != want {
			t.Errorf("%s: root name = %q, want %q", name, tree.Name, want)
		}
		if len(tree.Children) != 1 {
			t.Errorf("%s: wrapper must have exactly 1 child, got %d", name, len(tree.Children))
		}
		if len(tree.Children) == 1 && tree.Children[0].Type == "ClaudeErrorHandler" {
			t.Errorf("%s: double-wrapped", name)
		}
	}
}

func TestResolveDomainTreeIsWrapped(t *testing.T) {
	tree := ResolveTreeID("domain:goap_fusion_loop")
	if tree == nil || tree.Type != "ClaudeErrorHandler" {
		t.Fatalf("domain:goap_fusion_loop root = %+v, want ClaudeErrorHandler wrapper", tree)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/domains -run 'TestAllDomainTreesWrapped|TestResolveDomainTreeIsWrapped' -count=1`
Expected: FAIL — root types are Sequence/Selector etc.

- [ ] **Step 3: Write the implementation** — in `internal/domains/trees.go`, add the helper above `AllDomainTrees` and the wrap loop at its end (after the arc42 merge, before `return trees`):

```go
// wrapWithErrorHandler wraps a tree root in a ClaudeErrorHandler decorator so
// any failure that bubbles to the root can grow a Claude-proposed recovery
// node (engine/error_handler_node.go). Pure data — domains must not import
// engine (test-build cycle, see goap_fusion_wire_seam_test.go).
func wrapWithErrorHandler(name string, tree *evolution.SerializableNode) *evolution.SerializableNode {
	if tree == nil || tree.Type == "ClaudeErrorHandler" {
		return tree
	}
	return &evolution.SerializableNode{
		Type:     "ClaudeErrorHandler",
		Name:     name + "_ErrorHandler",
		Children: []evolution.SerializableNode{*tree},
	}
}
```

```go
	// Merge arc42 trees with qualified names (arc42:section1, etc.)
	for k, v := range Arc42Trees() {
		trees[k] = v
	}
	// Every domain tree root gets the self-extending error handler.
	for k, v := range trees {
		trees[k] = wrapWithErrorHandler(k, v)
	}
	return trees
```

- [ ] **Step 4: Run test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/domains -run 'TestAllDomainTreesWrapped|TestResolveDomainTreeIsWrapped' -count=1 -race`
Expected: PASS

- [ ] **Step 5: Run every catalog consumer and fix fallout**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/domains ./internal/agentexec ./internal/gardener ./internal/benchmark ./internal/knowledge ./internal/agent ./cmd/bt-agent -short -count=1`
Expected: mostly PASS. Known survey results (2026-07-15): `templates_resolve_test.go:88` and `domains_test.go:475` only nil/existence-check (unaffected); structural tests use direct constructors like `GoapFusionLoopTree()` (unaffected — constructors stay unwrapped). If a test DOES assert catalog-tree root structure, update the assertion to look at `tree.Children[0]` — never weaken the wrap to satisfy a test.

- [ ] **Step 6: Commit**

```bash
git add internal/domains/trees.go internal/domains/error_handler_wrap_test.go
git commit -m "feat(domains): wrap all domain tree roots in ClaudeErrorHandler

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: arc42 doc + full verification gate

**Files:**
- Modify: `docs/arc42/go-bt-evolve-arc42.md`

- [ ] **Step 1: Update arc42.** Locate the node-type / building-block coverage: `grep -n 'ReviewCycle\|CheckpointVerifier\|node type' docs/arc42/go-bt-evolve-arc42.md | head`. Add, in the same style and section as the existing decorator descriptions, a short entry:

> **ClaudeErrorHandler** — self-extending recovery decorator wrapped around every domain-tree root. On subtree failure it ticks previously generated recovery nodes (guard-first), then makes at most one read-only Claude Code call per error signature per cooldown (default 6 h) proposing a recovery node composed of registered actions/conditions; validated proposals persist under `~/.go-bt-evolve/error_handler/` and are re-grafted at every tree build. Guardrails: strict vocabulary validation, 5-extension cap per handler, auto-disable after 3 consecutive failures, `BT_CLAUDE_ERROR_HANDLER=off` kill switch.

- [ ] **Step 2: Doc drift + full gate**

Run: `PATH=/usr/local/go/bin:$PATH bash scripts/check-doc-drift.sh`
Expected: exit 0.

Run: `PATH=/usr/local/go/bin:$PATH make check-quick`
Expected: PASS (this is the CI-lint mirror).

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine ./internal/evolution ./internal/domains ./internal/agentexec -count=1 -race`
Expected: PASS. Do NOT run the whole-repo `-race` suite — `internal/llm` is red from the pre-existing acp.go race (Global Constraints).

Run: `PATH=/usr/local/go/bin:$PATH go build ./cmd/bt-agent ./cmd/bt-agent-cli`
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add docs/arc42/go-bt-evolve-arc42.md
git commit -m "docs(arc42): document ClaudeErrorHandler node

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Post-plan verification (before declaring done)

- Verify end-to-end behavior beyond unit tests: with `BT_CLAUDE_ERROR_HANDLER=off`, run one domain tree via the CLI or a `RunTask` harness and confirm passthrough; then, with the fake-store seeded by the Task 5 tests' JSON, confirm a fresh build grafts children (this is what the `GraftedExtensionHandlesNextRun` test proves at the package level).
- The branch is NOT merged by this plan — integration goes through the project's finishing flow (superpowers:finishing-a-development-branch), and the bare-repo master only fast-forwards.
