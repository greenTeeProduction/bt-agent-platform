# Scheduler Throughput Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the scheduler's single-lane inline job execution with bounded concurrent dispatch, exclusion groups, and operator-tunable configuration that reloads without a restart.

**Architecture:** `Scheduler.tick()` stops calling `runJob` inline. It sorts due jobs oldest-first and admits each through a lane semaphore (`laneState`) guarded by four ordered rules — job in-flight, agent in-flight, exclusion group, circuit breaker last. Admitted jobs run in goroutines whose single `defer` releases the lane. Lane budget comes from `ThroughputConfig` (built-in default → env → hot-reloaded JSON file).

**Tech Stack:** Go 1.x, stdlib only (`sync`, `sort`, `encoding/json`, `time`, `log/slog`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-02-scheduler-throughput-design.md`

## Global Constraints

- Package `agent` (`internal/agent/`). Tests are in-package (`package agent`), matching the existing `scheduler_*_test.go` files.
- `max_concurrent` clamps to `[1, 12]`. **`1` must reproduce today's exact serial behavior** — this is the rollback path.
- Group limit default is `1` for any group not listed in `group_limits`; clamps to `[1, max_concurrent]`.
- Config precedence is **built-in default → env → file**. Absent file fields fall through to env; a deleted file reverts to env.
- Malformed config keeps the last-good values and logs WARN. It must never crash the scheduler and never fall back to unbounded concurrency.
- The circuit breaker's `Allowed()` consumes a half-open probe, so it must be called **only for a job that will actually run**.
- `runJob`'s internals are not modified except for deleting its in-flight *set* block (the clear stays).
- `AnyInFlight()` and `OnCycleIdle` semantics must not change — deploy-drift restart safety depends on them.
- Every test in this plan runs under `-race`.
- Test command: `/usr/local/go/bin/go test ./internal/agent/ -run <Name> -race -count=1`
- Never pipe test output to `tail`/`head` when checking exit status — pipes mask exit codes. Redirect to a file, or check the status directly.

---

### Task 1: Throughput config from env

**Files:**
- Create: `internal/agent/throughput.go`
- Test: `internal/agent/throughput_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces: `ThroughputConfig{MaxConcurrent int; TickInterval time.Duration; GroupLimits map[string]int}`, `throughputFromEnv() ThroughputConfig`, `clampThroughput(ThroughputConfig) ThroughputConfig`, `(ThroughputConfig).limitFor(group string) int`, `RepoMainGroup` constant.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/throughput_test.go`:

```go
package agent

import (
	"testing"
	"time"
)

func TestThroughputFromEnv_Defaults(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "")
	t.Setenv("BT_SCHEDULER_TICK_INTERVAL", "")

	c := throughputFromEnv()
	if c.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want 3", c.MaxConcurrent)
	}
	if c.TickInterval != time.Minute {
		t.Errorf("TickInterval = %s, want 1m", c.TickInterval)
	}
	if got := c.limitFor(RepoMainGroup); got != 1 {
		t.Errorf("limitFor(repo-main) = %d, want 1", got)
	}
}

func TestThroughputFromEnv_Overrides(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "5")
	t.Setenv("BT_SCHEDULER_TICK_INTERVAL", "15s")

	c := throughputFromEnv()
	if c.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", c.MaxConcurrent)
	}
	if c.TickInterval != 15*time.Second {
		t.Errorf("TickInterval = %s, want 15s", c.TickInterval)
	}
}

func TestThroughputFromEnv_IgnoresGarbage(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "not-a-number")
	t.Setenv("BT_SCHEDULER_TICK_INTERVAL", "banana")

	c := throughputFromEnv()
	if c.MaxConcurrent != 3 || c.TickInterval != time.Minute {
		t.Errorf("garbage env should fall back to defaults, got %+v", c)
	}
}

func TestClampThroughput(t *testing.T) {
	cases := []struct {
		name string
		in   ThroughputConfig
		want ThroughputConfig
	}{
		{
			name: "zero concurrency clamps to serial",
			in:   ThroughputConfig{MaxConcurrent: 0, TickInterval: time.Minute},
			want: ThroughputConfig{MaxConcurrent: 1, TickInterval: time.Minute},
		},
		{
			name: "over cap clamps to 12",
			in:   ThroughputConfig{MaxConcurrent: 99, TickInterval: time.Minute},
			want: ThroughputConfig{MaxConcurrent: 12, TickInterval: time.Minute},
		},
		{
			name: "non-positive interval falls back to 1m",
			in:   ThroughputConfig{MaxConcurrent: 2, TickInterval: 0},
			want: ThroughputConfig{MaxConcurrent: 2, TickInterval: time.Minute},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampThroughput(tc.in)
			if got.MaxConcurrent != tc.want.MaxConcurrent || got.TickInterval != tc.want.TickInterval {
				t.Errorf("clampThroughput(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestClampThroughput_GroupLimitBoundedByMaxConcurrent(t *testing.T) {
	c := clampThroughput(ThroughputConfig{
		MaxConcurrent: 2,
		TickInterval:  time.Minute,
		GroupLimits:   map[string]int{"repo-main": 0, "wide": 99},
	})
	if got := c.limitFor("repo-main"); got != 1 {
		t.Errorf("limitFor(repo-main) = %d, want 1", got)
	}
	if got := c.limitFor("wide"); got != 2 {
		t.Errorf("limitFor(wide) = %d, want 2 (bounded by MaxConcurrent)", got)
	}
	if got := c.limitFor("unlisted"); got != 1 {
		t.Errorf("limitFor(unlisted) = %d, want default 1", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run 'TestThroughput|TestClampThroughput' -race -count=1`
Expected: FAIL — `undefined: throughputFromEnv`, `undefined: ThroughputConfig`, `undefined: clampThroughput`, `undefined: RepoMainGroup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/throughput.go`:

```go
package agent

import (
	"os"
	"strconv"
	"time"
)

// RepoMainGroup is the exclusion group for agents that mutate the main
// checkout. Its limit is 1 by default, which is exactly the serial guarantee
// the scheduler provided before concurrent lanes existed: the goap-fusion
// materializer runs `git checkout -f HEAD -- .` in the shared repo, so two of
// those cycles overlapping would corrupt each other's working tree.
const RepoMainGroup = "repo-main"

const (
	defaultMaxConcurrent = 3
	defaultTickInterval  = time.Minute
	// maxAllowedConcurrent bounds operator error. The host has 12 cores; a
	// larger value could only oversubscribe it.
	maxAllowedConcurrent = 12
)

// ThroughputConfig is the scheduler's dispatch budget. Built from
// defaults, then env, then the on-disk file (see throughputLoader).
type ThroughputConfig struct {
	MaxConcurrent int
	TickInterval  time.Duration
	GroupLimits   map[string]int
}

// limitFor returns the in-flight cap for an exclusion group. Groups absent
// from GroupLimits default to 1 — the safe end, since an unknown group is
// most likely a new agent whose sharing behavior nobody has reasoned about.
func (c ThroughputConfig) limitFor(group string) int {
	if v, ok := c.GroupLimits[group]; ok {
		return v
	}
	return 1
}

// clampThroughput bounds operator input. MaxConcurrent of 1 is deliberately
// reachable: it reproduces the pre-concurrency serial scheduler and is the
// rollback path.
func clampThroughput(c ThroughputConfig) ThroughputConfig {
	if c.MaxConcurrent < 1 {
		c.MaxConcurrent = 1
	}
	if c.MaxConcurrent > maxAllowedConcurrent {
		c.MaxConcurrent = maxAllowedConcurrent
	}
	if c.TickInterval <= 0 {
		c.TickInterval = defaultTickInterval
	}
	for g, lim := range c.GroupLimits {
		if lim < 1 {
			c.GroupLimits[g] = 1
		} else if lim > c.MaxConcurrent {
			c.GroupLimits[g] = c.MaxConcurrent
		}
	}
	return c
}

// throughputFromEnv builds the boot-default config. Unparseable values are
// ignored rather than fatal: a typo in a unit file must not take the fleet
// down, and the logged default is recoverable.
func throughputFromEnv() ThroughputConfig {
	c := ThroughputConfig{
		MaxConcurrent: defaultMaxConcurrent,
		TickInterval:  defaultTickInterval,
		GroupLimits:   map[string]int{RepoMainGroup: 1},
	}
	if v := os.Getenv("BT_SCHEDULER_MAX_CONCURRENT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxConcurrent = n
		}
	}
	if v := os.Getenv("BT_SCHEDULER_TICK_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			c.TickInterval = d
		}
	}
	return clampThroughput(c)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run 'TestThroughput|TestClampThroughput' -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/throughput.go internal/agent/throughput_test.go
git commit -m "feat(agent): throughput config from env with clamps"
```

---

### Task 2: Hot-reloaded config file

**Files:**
- Modify: `internal/agent/paths.go` (add `ThroughputFile()` alongside the other path helpers)
- Modify: `internal/agent/throughput.go` (add loader)
- Test: `internal/agent/throughput_test.go` (append)

**Interfaces:**
- Consumes: `ThroughputConfig`, `throughputFromEnv`, `clampThroughput` (Task 1).
- Produces: `ThroughputFile() string`, `newThroughputLoader(path string) *throughputLoader`, `(*throughputLoader).Load() ThroughputConfig`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/throughput_test.go`:

```go
func writeThroughputFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// Loader caches on (mtime, size); coarse filesystem timestamps can make two
	// quick writes look identical. Push mtime forward so the reload is seen.
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestThroughputLoader_NoFileUsesEnv(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "4")
	l := newThroughputLoader(filepath.Join(t.TempDir(), "throughput.json"))

	if got := l.Load().MaxConcurrent; got != 4 {
		t.Errorf("MaxConcurrent = %d, want 4 from env", got)
	}
}

func TestThroughputLoader_FileOverridesEnv(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "4")
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"max_concurrent": 2, "tick_interval": "10s"}`)

	c := newThroughputLoader(path).Load()
	if c.MaxConcurrent != 2 {
		t.Errorf("MaxConcurrent = %d, want 2 from file", c.MaxConcurrent)
	}
	if c.TickInterval != 10*time.Second {
		t.Errorf("TickInterval = %s, want 10s from file", c.TickInterval)
	}
}

func TestThroughputLoader_AbsentFieldFallsThroughToEnv(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "7")
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"tick_interval": "30s"}`)

	c := newThroughputLoader(path).Load()
	if c.MaxConcurrent != 7 {
		t.Errorf("MaxConcurrent = %d, want 7 (env, since file omits it)", c.MaxConcurrent)
	}
	if c.TickInterval != 30*time.Second {
		t.Errorf("TickInterval = %s, want 30s", c.TickInterval)
	}
}

func TestThroughputLoader_ReloadsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"max_concurrent": 2}`)
	l := newThroughputLoader(path)

	if got := l.Load().MaxConcurrent; got != 2 {
		t.Fatalf("first Load = %d, want 2", got)
	}
	writeThroughputFile(t, path, `{"max_concurrent": 5}`)
	if got := l.Load().MaxConcurrent; got != 5 {
		t.Errorf("after rewrite Load = %d, want 5", got)
	}
}

func TestThroughputLoader_MalformedKeepsLastGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"max_concurrent": 5}`)
	l := newThroughputLoader(path)
	if got := l.Load().MaxConcurrent; got != 5 {
		t.Fatalf("first Load = %d, want 5", got)
	}

	writeThroughputFile(t, path, `{"max_concurrent": `) // truncated JSON
	if got := l.Load().MaxConcurrent; got != 5 {
		t.Errorf("after malformed rewrite Load = %d, want last-good 5", got)
	}
}

func TestThroughputLoader_DeletedFileRevertsToEnv(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "3")
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"max_concurrent": 8}`)
	l := newThroughputLoader(path)
	if got := l.Load().MaxConcurrent; got != 8 {
		t.Fatalf("first Load = %d, want 8", got)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := l.Load().MaxConcurrent; got != 3 {
		t.Errorf("after delete Load = %d, want env 3", got)
	}
}

func TestThroughputLoader_ClampsFileValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "throughput.json")
	writeThroughputFile(t, path, `{"max_concurrent": 500, "group_limits": {"repo-main": 99}}`)

	c := newThroughputLoader(path).Load()
	if c.MaxConcurrent != 12 {
		t.Errorf("MaxConcurrent = %d, want clamp to 12", c.MaxConcurrent)
	}
	if got := c.limitFor(RepoMainGroup); got != 12 {
		t.Errorf("limitFor(repo-main) = %d, want clamp to MaxConcurrent 12", got)
	}
}
```

Add `"os"`, `"path/filepath"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestThroughputLoader -race -count=1`
Expected: FAIL — `undefined: newThroughputLoader`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/agent/throughput.go` (add `"encoding/json"`, `"log/slog"`, `"sync"` to its imports):

```go
// throughputFileFormat is the on-disk shape. Every field is a pointer so an
// absent key falls through to the env layer instead of resetting it to zero.
type throughputFileFormat struct {
	MaxConcurrent *int           `json:"max_concurrent"`
	TickInterval  *string        `json:"tick_interval"`
	GroupLimits   map[string]int `json:"group_limits"`
}

// throughputLoader reads the config file at most once per change. The
// scheduler calls Load on every tick, so the steady-state cost must be a
// single stat, not a parse.
type throughputLoader struct {
	mu       sync.Mutex
	path     string
	lastMod  time.Time
	lastSize int64
	current  ThroughputConfig
	loaded   bool
}

func newThroughputLoader(path string) *throughputLoader {
	return &throughputLoader{path: path}
}

// Load returns the effective config: defaults, overlaid by env, overlaid by
// the file. A parse failure keeps the last good config — the fleet keeps the
// budget it was already running with rather than jumping to a default that
// nobody chose.
func (l *throughputLoader) Load() ThroughputConfig {
	l.mu.Lock()
	defer l.mu.Unlock()

	env := throughputFromEnv()

	st, err := os.Stat(l.path)
	if err != nil {
		// No file: env is the whole story. Reset the cache so a file created
		// later is picked up on the next call.
		l.lastMod, l.lastSize, l.loaded = time.Time{}, 0, false
		l.current = env
		return env
	}
	if l.loaded && st.ModTime().Equal(l.lastMod) && st.Size() == l.lastSize {
		return l.current
	}

	// Stamp the cache before parsing: bad bytes must not be re-read (and
	// re-warned) on every tick until someone fixes the file.
	l.lastMod, l.lastSize = st.ModTime(), st.Size()

	data, readErr := os.ReadFile(l.path)
	if readErr != nil {
		slog.Warn("scheduler: throughput config unreadable — keeping current budget",
			"path", l.path, "err", readErr)
		if !l.loaded {
			l.current, l.loaded = env, true
		}
		return l.current
	}

	var f throughputFileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("scheduler: throughput config parse failed — keeping current budget",
			"path", l.path, "err", err)
		if !l.loaded {
			l.current, l.loaded = env, true
		}
		return l.current
	}

	merged := env
	if f.MaxConcurrent != nil {
		merged.MaxConcurrent = *f.MaxConcurrent
	}
	if f.TickInterval != nil {
		if d, err := time.ParseDuration(*f.TickInterval); err == nil {
			merged.TickInterval = d
		} else {
			slog.Warn("scheduler: throughput tick_interval unparseable — keeping current",
				"value", *f.TickInterval, "err", err)
		}
	}
	if len(f.GroupLimits) > 0 {
		merged.GroupLimits = make(map[string]int, len(f.GroupLimits))
		for k, v := range f.GroupLimits {
			merged.GroupLimits[k] = v
		}
	}
	merged = clampThroughput(merged)

	if l.loaded && !sameThroughput(l.current, merged) {
		slog.Info("scheduler: throughput config changed",
			"old_max_concurrent", l.current.MaxConcurrent, "new_max_concurrent", merged.MaxConcurrent,
			"old_tick_interval", l.current.TickInterval, "new_tick_interval", merged.TickInterval)
	}
	l.current, l.loaded = merged, true
	return merged
}

func sameThroughput(a, b ThroughputConfig) bool {
	if a.MaxConcurrent != b.MaxConcurrent || a.TickInterval != b.TickInterval {
		return false
	}
	if len(a.GroupLimits) != len(b.GroupLimits) {
		return false
	}
	for k, v := range a.GroupLimits {
		if b.GroupLimits[k] != v {
			return false
		}
	}
	return true
}
```

Add to `internal/agent/paths.go`, next to `CircuitBreakersFile`:

```go
// ThroughputFile is the operator-tunable dispatch budget, re-read by the
// scheduler on every tick so lanes can be retuned without a restart (a
// restart kills whatever cycle is running).
func ThroughputFile() string {
	return filepath.Join(HomeDir(), "throughput.json")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run 'TestThroughput|TestClampThroughput' -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/throughput.go internal/agent/throughput_test.go internal/agent/paths.go
git commit -m "feat(agent): hot-reloaded throughput.json overlay"
```

---

### Task 3: Concurrency groups

**Files:**
- Modify: `internal/agent/registry.go` (add field to `Definition`)
- Create: `internal/agent/concurrency_group.go`
- Test: `internal/agent/concurrency_group_test.go`

**Interfaces:**
- Consumes: `RepoMainGroup` (Task 1), `Definition` (existing).
- Produces: `Definition.ConcurrencyGroup string`, `ConcurrencyGroupFor(def Definition) string`.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/concurrency_group_test.go`:

```go
package agent

import "testing"

func TestConcurrencyGroupFor(t *testing.T) {
	cases := []struct {
		name string
		def  Definition
		want string
	}{
		{
			name: "goap fusion tree defaults to repo-main",
			def:  Definition{Name: "goap-fusion-runner", Tree: "domain:goap_fusion"},
			want: RepoMainGroup,
		},
		{
			name: "goap fusion loop variant also repo-main",
			def:  Definition{Name: "goap-fusion-loop-runner", Tree: "domain:goap_fusion_loop"},
			want: RepoMainGroup,
		},
		{
			name: "superpowers tree defaults to repo-main",
			def:  Definition{Name: "superpowers-prod-runner", Tree: "domain:superpowers_prod"},
			want: RepoMainGroup,
		},
		{
			name: "self fix tree defaults to repo-main",
			def:  Definition{Name: "self-fixer", Tree: "domain:self_fix"},
			want: RepoMainGroup,
		},
		{
			name: "unrelated tree gets its own per-agent group",
			def:  Definition{Name: "notebooklm-researcher", Tree: "domain:research"},
			want: "agent:notebooklm-researcher",
		},
		{
			name: "explicit field wins over tree default",
			def:  Definition{Name: "goap-fusion-runner", Tree: "domain:goap_fusion", ConcurrencyGroup: "custom"},
			want: "custom",
		},
		{
			name: "whitespace-only field is treated as unset",
			def:  Definition{Name: "monitor", Tree: "domain:research", ConcurrencyGroup: "   "},
			want: "agent:monitor",
		},
		{
			name: "empty tree still yields a stable per-agent group",
			def:  Definition{Name: "bare"},
			want: "agent:bare",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConcurrencyGroupFor(tc.def); got != tc.want {
				t.Errorf("ConcurrencyGroupFor(%+v) = %q, want %q", tc.def, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestConcurrencyGroupFor -race -count=1`
Expected: FAIL — `undefined: ConcurrencyGroupFor` and `unknown field ConcurrencyGroup in struct literal`.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/registry.go`, add to the `Definition` struct immediately after the `Schedule` field:

```go
	// ConcurrencyGroup names the scheduler exclusion group this agent's runs
	// belong to; at most group_limits[group] runs from a group are in flight at
	// once. Empty means "derive from Tree" — see ConcurrencyGroupFor.
	ConcurrencyGroup string `yaml:"concurrency_group,omitempty" json:"concurrency_group,omitempty"`
```

Create `internal/agent/concurrency_group.go`:

```go
package agent

import "strings"

// repoMutatingTreePrefixes are the trees whose runs write to the main
// checkout: the goap-fusion materializer's `git checkout -f HEAD -- .`, the
// superpowers apply/commit path, and self-fix. Deriving the group from the
// tree — rather than requiring the YAML field — is the safety property: a new
// repo-mutating agent added without the field still lands in repo-main instead
// of silently racing the materializer.
var repoMutatingTreePrefixes = []string{
	"domain:goap_fusion",
	"domain:superpowers",
	"domain:self_fix",
}

// ConcurrencyGroupFor returns the exclusion group for an agent. An explicit
// ConcurrencyGroup always wins. Otherwise repo-mutating trees map to
// RepoMainGroup and everything else gets a group of its own, which means no
// cross-agent exclusion — only the scheduler's per-agent in-flight guard.
func ConcurrencyGroupFor(def Definition) string {
	if g := strings.TrimSpace(def.ConcurrencyGroup); g != "" {
		return g
	}
	tree := strings.TrimSpace(def.Tree)
	for _, prefix := range repoMutatingTreePrefixes {
		if strings.HasPrefix(tree, prefix) {
			return RepoMainGroup
		}
	}
	return "agent:" + def.Name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestConcurrencyGroupFor -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/concurrency_group.go internal/agent/concurrency_group_test.go internal/agent/registry.go
git commit -m "feat(agent): concurrency groups with tree-derived defaults"
```

---

### Task 4: Lane admission primitives

**Files:**
- Create: `internal/agent/lanes.go`
- Test: `internal/agent/lanes_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (pure data structure).
- Produces: `laneState` type, `newLaneState() *laneState`, `(*laneState).tryAcquire(jobID, agent, group string, maxConcurrent, groupLimit int) bool`, `(*laneState).release(jobID, agent, group string)`, `(*laneState).inUse() int`.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/lanes_test.go`:

```go
package agent

import (
	"sync"
	"testing"
)

func TestLaneState_RespectsMaxConcurrent(t *testing.T) {
	l := newLaneState()
	if !l.tryAcquire("j1", "a1", "g1", 2, 5) {
		t.Fatal("first acquire should succeed")
	}
	if !l.tryAcquire("j2", "a2", "g2", 2, 5) {
		t.Fatal("second acquire should succeed")
	}
	if l.tryAcquire("j3", "a3", "g3", 2, 5) {
		t.Fatal("third acquire should fail at max 2")
	}
	if got := l.inUse(); got != 2 {
		t.Errorf("inUse = %d, want 2", got)
	}
}

func TestLaneState_RejectsSameJobTwice(t *testing.T) {
	l := newLaneState()
	if !l.tryAcquire("j1", "a1", "g1", 4, 4) {
		t.Fatal("first acquire should succeed")
	}
	if l.tryAcquire("j1", "a1", "g1", 4, 4) {
		t.Fatal("same job must not be dispatched twice")
	}
}

func TestLaneState_RejectsSameAgentUnderDifferentJobIDs(t *testing.T) {
	l := newLaneState()
	if !l.tryAcquire("j1", "shared-agent", "g1", 4, 4) {
		t.Fatal("first acquire should succeed")
	}
	if l.tryAcquire("j2", "shared-agent", "g2", 4, 4) {
		t.Fatal("same agent must not run concurrently under a duplicate job")
	}
}

func TestLaneState_RespectsGroupLimit(t *testing.T) {
	l := newLaneState()
	if !l.tryAcquire("j1", "a1", RepoMainGroup, 4, 1) {
		t.Fatal("first repo-main acquire should succeed")
	}
	if l.tryAcquire("j2", "a2", RepoMainGroup, 4, 1) {
		t.Fatal("second repo-main acquire must fail at group limit 1")
	}
	if !l.tryAcquire("j3", "a3", "other", 4, 1) {
		t.Fatal("a different group should still get a lane")
	}
}

func TestLaneState_ReleaseFreesEverything(t *testing.T) {
	l := newLaneState()
	if !l.tryAcquire("j1", "a1", RepoMainGroup, 1, 1) {
		t.Fatal("acquire should succeed")
	}
	l.release("j1", "a1", RepoMainGroup)
	if got := l.inUse(); got != 0 {
		t.Fatalf("inUse after release = %d, want 0", got)
	}
	if !l.tryAcquire("j2", "a2", RepoMainGroup, 1, 1) {
		t.Fatal("lane, agent and group must all be free after release")
	}
}

func TestLaneState_DoubleReleaseIsNoop(t *testing.T) {
	l := newLaneState()
	l.tryAcquire("j1", "a1", "g1", 2, 2)
	l.release("j1", "a1", "g1")
	l.release("j1", "a1", "g1") // must not drive counters negative

	if got := l.inUse(); got != 0 {
		t.Fatalf("inUse = %d, want 0", got)
	}
	if !l.tryAcquire("j2", "a2", "g2", 2, 2) || !l.tryAcquire("j3", "a3", "g3", 2, 2) {
		t.Fatal("both lanes should be available after a double release")
	}
}

func TestLaneState_ConcurrentAcquireNeverExceedsMax(t *testing.T) {
	l := newLaneState()
	const max = 3
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			jobID := "job" + string(rune('a'+i%26)) + string(rune('a'+i/26))
			agent := "agent" + jobID
			if l.tryAcquire(jobID, agent, "grp"+jobID, max, max) {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if granted > max {
		t.Errorf("granted %d lanes, want at most %d", granted, max)
	}
	if got := l.inUse(); got != granted {
		t.Errorf("inUse = %d, want %d", got, granted)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestLaneState -race -count=1`
Expected: FAIL — `undefined: newLaneState`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/agent/lanes.go`:

```go
package agent

import "sync"

// laneState is the scheduler's admission control. It answers one question —
// may this job start right now — and owns the bookkeeping that makes the
// answer safe under concurrency. Use newLaneState; the zero value has nil maps.
type laneState struct {
	mu     sync.Mutex
	lanes  int             // total in-flight jobs
	jobs   map[string]bool // job ID -> in flight
	agents map[string]bool // agent name -> in flight
	groups map[string]int  // exclusion group -> in-flight count
}

func newLaneState() *laneState {
	return &laneState{
		jobs:   make(map[string]bool),
		agents: make(map[string]bool),
		groups: make(map[string]int),
	}
}

// tryAcquire takes a lane when every capacity rule allows it. It never blocks:
// a false return means the job stays due and the next tick retries it, which
// keeps backpressure visible in the job table instead of hidden in a queue.
//
// The rules, in order: global lane budget, this job already running, this
// agent already running (possible via duplicate jobs), and the exclusion group
// budget. The circuit breaker is deliberately NOT consulted here — see the
// caller: Allowed() consumes a half-open probe and must only be spent on a job
// that is actually about to run.
func (l *laneState) tryAcquire(jobID, agent, group string, maxConcurrent, groupLimit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lanes >= maxConcurrent {
		return false
	}
	if l.jobs[jobID] || l.agents[agent] {
		return false
	}
	if groupLimit > 0 && l.groups[group] >= groupLimit {
		return false
	}

	l.lanes++
	l.jobs[jobID] = true
	l.agents[agent] = true
	l.groups[group]++
	return true
}

// release frees a lane. It is idempotent: the caller releases from a defer, and
// a second release (e.g. a belt-and-braces cleanup path) must not drive the
// counters negative and hand out phantom lanes.
func (l *laneState) release(jobID, agent, group string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.jobs[jobID] {
		return
	}
	delete(l.jobs, jobID)
	delete(l.agents, agent)
	if l.groups[group] <= 1 {
		delete(l.groups, group)
	} else {
		l.groups[group]--
	}
	l.lanes--
}

func (l *laneState) inUse() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lanes
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestLaneState -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/lanes.go internal/agent/lanes_test.go
git commit -m "feat(agent): lane admission primitives for concurrent dispatch"
```

---

### Task 5: Concurrent dispatch in tick()

This is where behavior changes. Everything before it was inert.

**Files:**
- Modify: `internal/agent/scheduler.go` (struct fields, `NewScheduler`, `tick`, `runJob`)
- Test: `internal/agent/scheduler_dispatch_test.go`

**Interfaces:**
- Consumes: `newThroughputLoader`, `ThroughputFile`, `(*throughputLoader).Load` (Task 2); `ConcurrencyGroupFor` (Task 3); `newLaneState`, `tryAcquire`, `release`, `inUse` (Task 4).
- Produces: `(*Scheduler).groupFor(agentName string) string`, `(*Scheduler).markInFlight(job *ScheduledJob)`, `(*Scheduler).finishDispatch(job *ScheduledJob, group string)`; `Scheduler` gains fields `lanes *laneState`, `throughput *throughputLoader`.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/scheduler_dispatch_test.go`:

```go
package agent

import (
	"testing"
	"time"
)

// dueJob schedules an agent and back-dates NextRun so the next tick sees it.
func dueJob(t *testing.T, s *Scheduler, agentName string) *ScheduledJob {
	t.Helper()
	job, err := s.Schedule(agentName, "every 1h", "30m", 3)
	if err != nil {
		t.Fatalf("schedule %s: %v", agentName, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.jobs[job.ID]
	if !ok {
		t.Fatalf("job %s not stored", job.ID)
	}
	stored.NextRun = time.Now().Add(-time.Minute)
	return stored
}

func recvWithin(t *testing.T, ch <-chan string, d time.Duration) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timed out after %s waiting for an agent to start", d)
		return ""
	}
}

func mustNotRecv(t *testing.T, ch <-chan string, d time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected concurrent start: %s", v)
	case <-time.After(d):
	}
}

// blockingRunner reports each start on started and blocks until release closes.
func blockingRunner(started chan<- string, release <-chan struct{}) AgentRunner {
	return func(ctx RunContext) (string, string, *RunResult, error) {
		started <- ctx.AgentName
		<-release
		return "success", "", nil, nil
	}
}

func newDispatchScheduler(t *testing.T, defs ...Definition) *Scheduler {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BT_AGENT_HOME", dir) // keeps ThroughputFile() inside the temp dir
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	for _, d := range defs {
		if d.Version == "" {
			d.Version = "1.0.0"
		}
		if _, err := reg.Create(d); err != nil {
			t.Fatalf("create %s: %v", d.Name, err)
		}
	}
	return NewScheduler(SchedulerConfig{Registry: reg})
}

func TestTick_RunsDifferentAgentsConcurrently(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "3")
	s := newDispatchScheduler(t,
		Definition{Name: "alpha", Tree: "domain:research"},
		Definition{Name: "beta", Tree: "domain:research"},
	)
	dueJob(t, s, "alpha")
	dueJob(t, s, "beta")

	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)

	s.tick(blockingRunner(started, release))

	first := recvWithin(t, started, 2*time.Second)
	second := recvWithin(t, started, 2*time.Second)
	if first == second {
		t.Fatalf("same agent started twice: %s", first)
	}
}

func TestTick_SerialWhenMaxConcurrentIsOne(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "1")
	s := newDispatchScheduler(t,
		Definition{Name: "alpha", Tree: "domain:research"},
		Definition{Name: "beta", Tree: "domain:research"},
	)
	dueJob(t, s, "alpha")
	dueJob(t, s, "beta")

	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)

	s.tick(blockingRunner(started, release))

	recvWithin(t, started, 2*time.Second)
	mustNotRecv(t, started, 300*time.Millisecond)
}

func TestTick_RepoMainAgentsNeverOverlap(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "4")
	s := newDispatchScheduler(t,
		Definition{Name: "goap-a", Tree: "domain:goap_fusion"},
		Definition{Name: "goap-b", Tree: "domain:goap_fusion_loop"},
		Definition{Name: "monitor", Tree: "domain:research"},
	)
	dueJob(t, s, "goap-a")
	dueJob(t, s, "goap-b")
	dueJob(t, s, "monitor")

	started := make(chan string, 3)
	release := make(chan struct{})
	defer close(release)

	s.tick(blockingRunner(started, release))

	first := recvWithin(t, started, 2*time.Second)
	second := recvWithin(t, started, 2*time.Second)
	got := map[string]bool{first: true, second: true}
	if got["goap-a"] && got["goap-b"] {
		t.Fatalf("two repo-main agents ran concurrently: %s, %s", first, second)
	}
	if !got["monitor"] {
		t.Fatalf("the non-repo-main agent should have run: got %s, %s", first, second)
	}
	mustNotRecv(t, started, 300*time.Millisecond)
}

func TestTick_SameJobNotDispatchedTwiceAcrossTicks(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "3")
	s := newDispatchScheduler(t, Definition{Name: "alpha", Tree: "domain:research"})
	dueJob(t, s, "alpha")

	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)
	runner := blockingRunner(started, release)

	s.tick(runner)
	recvWithin(t, started, 2*time.Second)

	// The job is still "due" — NextRun is only advanced when the run completes.
	s.tick(runner)
	mustNotRecv(t, started, 300*time.Millisecond)
}

func TestTick_WithheldLaneDoesNotConsumeBreakerProbe(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "1")
	dir := t.TempDir()
	t.Setenv("BT_AGENT_HOME", dir)
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := reg.Create(Definition{Name: name, Tree: "domain:research", Version: "1.0.0"}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	cb := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 2, Cooldown: time.Hour})
	s := NewScheduler(SchedulerConfig{Registry: reg, CBStore: cb})
	dueJob(t, s, "alpha")
	dueJob(t, s, "beta")

	started := make(chan string, 2)
	release := make(chan struct{})
	defer close(release)

	s.tick(blockingRunner(started, release))
	ran := recvWithin(t, started, 2*time.Second)

	// The agent that never got a lane must still be allowed to run later: its
	// breaker must be untouched, not sitting on a consumed half-open probe.
	blocked := "beta"
	if ran == "beta" {
		blocked = "alpha"
	}
	if !cb.Allowed(blocked) {
		t.Errorf("breaker for %s was consumed by a job that never ran", blocked)
	}
}

func TestTick_PanicInOneLaneFreesItsLane(t *testing.T) {
	t.Setenv("BT_SCHEDULER_MAX_CONCURRENT", "1")
	s := newDispatchScheduler(t, Definition{Name: "alpha", Tree: "domain:research"})
	job := dueJob(t, s, "alpha")

	panicked := make(chan struct{})
	s.tick(func(ctx RunContext) (string, string, *RunResult, error) {
		defer close(panicked)
		panic("boom")
	})

	select {
	case <-panicked:
	case <-time.After(2 * time.Second):
		t.Fatal("runner never ran")
	}

	deadline := time.Now().Add(2 * time.Second)
	for s.lanes.inUse() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.lanes.inUse(); got != 0 {
		t.Fatalf("lanes in use after panic = %d, want 0", got)
	}
	s.mu.RLock()
	inFlight := s.jobs[job.ID].InFlight
	s.mu.RUnlock()
	if inFlight {
		t.Error("job left marked in-flight after a panicking run")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run 'TestTick_' -race -count=1`
Expected: FAIL — `s.lanes undefined`, and the concurrency assertions time out because `tick` still runs jobs inline.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/scheduler.go`:

(a) Add `"sort"` to the import block.

(b) Add fields to the `Scheduler` struct, after `onCycleIdle`:

```go
	lanes      *laneState        // admission control for concurrent dispatch
	throughput *throughputLoader // operator-tunable lane budget, reloaded per tick
	satMu      sync.Mutex        // guards lastSatLog
	lastSatLog time.Time         // throttles the lanes-saturated INFO line
```

(c) In `NewScheduler`, immediately after the `s := &Scheduler{...}` literal, initialize them:

```go
	s.lanes = newLaneState()
	s.throughput = newThroughputLoader(ThroughputFile())
	// An explicit SchedulerConfig.TickInterval (tests, embedders) pins the
	// interval; production leaves it zero, so the file/env value governs and can
	// be retuned live.
	if cfg.TickInterval == 0 {
		s.tickInterval = s.throughput.Load().TickInterval
	}
```

Note the existing `if cfg.TickInterval == 0 { cfg.TickInterval = 1 * time.Minute }` at the top of `NewScheduler` still runs first; the block above then replaces that default with the configured one. Leave the original line alone so `cfg` stays internally consistent.

(d) Replace the whole body of `tick`:

```go
func (s *Scheduler) tick(runner AgentRunner) {
	cfg := s.throughput.Load()

	s.mu.RLock()
	var due []*ScheduledJob
	now := time.Now()
	for _, j := range s.jobs {
		if j.Active && (j.NextRun.IsZero() || now.After(j.NextRun)) {
			due = append(due, j)
		}
	}
	s.mu.RUnlock()

	// Oldest-first. Map iteration order is random, so under a lane cap the same
	// unlucky job could be skipped tick after tick while newer ones take the
	// lanes.
	sort.Slice(due, func(i, j int) bool { return due[i].NextRun.Before(due[j].NextRun) })

	deferred := 0
	for _, job := range due {
		group := s.groupFor(job.AgentName)
		if !s.lanes.tryAcquire(job.ID, job.AgentName, group, cfg.MaxConcurrent, cfg.limitFor(group)) {
			deferred++
			slog.Debug("scheduler: no lane available, job stays due",
				"agent", job.AgentName, "group", group,
				"lanes_in_use", s.lanes.inUse(), "max_concurrent", cfg.MaxConcurrent)
			continue
		}

		// Breaker last: Allowed() consumes a half-open probe, and a probe spent
		// on a job we then decline to run wedges the breaker in HalfOpen forever
		// (Allow() in HalfOpen always refuses afterwards).
		if s.cbStore != nil && !s.cbStore.Allowed(job.AgentName) {
			cb := s.cbStore.Get(job.AgentName)
			slog.Warn("scheduler: skipping agent — circuit breaker open",
				"agent", job.AgentName, "state", cb.State(),
				"failures", cb.FailureCount(), "cooldown", cb.Cooldown())
			s.lanes.release(job.ID, job.AgentName, group)
			continue
		}

		s.markInFlight(job)
		go func(job *ScheduledJob, group string) {
			// First deferred call, so it runs last on the way out and survives any
			// panic that escapes runJob's own recover.
			defer s.finishDispatch(job, group)
			s.runJob(job, runner)
		}(job, group)
	}

	if deferred > 0 {
		s.logSaturation(deferred, cfg.MaxConcurrent)
	}
}

// groupFor resolves an agent's exclusion group, falling back to a per-agent
// group when the registry cannot answer — an unknown agent gets no sharing
// semantics, never an accidental repo-main lane.
func (s *Scheduler) groupFor(agentName string) string {
	if s.reg == nil {
		return "agent:" + agentName
	}
	inst, err := s.reg.Get(agentName)
	if err != nil || inst == nil {
		return "agent:" + agentName
	}
	return ConcurrencyGroupFor(inst.Definition)
}

// markInFlight flags the job before its goroutine starts. Dispatch owns the
// set (not runJob) because the flag is what stops the next tick — which fires
// while the run is still going — from dispatching the same job again.
func (s *Scheduler) markInFlight(job *ScheduledJob) {
	s.mu.Lock()
	job.InFlight = true
	s.mu.Unlock()
	s.saveState()
}

// finishDispatch frees the lane and clears the in-flight flag. runJob already
// clears the flag on its normal path; repeating it here is the panic-path
// safety net and is idempotent.
func (s *Scheduler) finishDispatch(job *ScheduledJob, group string) {
	s.lanes.release(job.ID, job.AgentName, group)
	s.mu.Lock()
	job.InFlight = false
	s.mu.Unlock()
}

// logSaturation reports lane starvation at most once a minute. Unthrottled, a
// saturated fleet writes one line per deferred job per tick.
func (s *Scheduler) logSaturation(deferred, maxConcurrent int) {
	s.satMu.Lock()
	defer s.satMu.Unlock()
	if time.Since(s.lastSatLog) < time.Minute {
		return
	}
	s.lastSatLog = time.Now()
	slog.Info("scheduler: lanes saturated",
		"deferred_jobs", deferred, "lanes_in_use", s.lanes.inUse(), "max_concurrent", maxConcurrent)
}
```

(e) In `runJob`, delete the in-flight *set* block (dispatch owns it now):

```go
	// DELETE these lines — markInFlight does this before the goroutine starts:
	//	s.mu.Lock()
	//	job.InFlight = true
	//	s.mu.Unlock()
	//	s.saveState()
```

Leave the comment above it about crash recovery attached to `markInFlight` instead, and leave `job.InFlight = false` in the completion block untouched.

This move is safe, verified before writing this plan: `runJob` has exactly one
production call site (`tick`, everything else is tests), and
`TestScheduler_CrashRecovery_InFlightReset` sets `job.InFlight = true` by hand
rather than relying on `runJob` to set it. Tests that call `sched.runJob(...)`
directly bypass dispatch and keep passing — none of them assert that the flag is
set during the run.

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run 'TestTick_' -race -count=1`
Expected: PASS

Then run the whole package to catch regressions in the existing scheduler tests:

Run: `/usr/local/go/bin/go test ./internal/agent/ -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/scheduler.go internal/agent/scheduler_dispatch_test.go
git commit -m "feat(agent): bounded concurrent job dispatch with exclusion groups"
```

---

### Task 6: Tick-interval hot reload

**Files:**
- Modify: `internal/agent/scheduler.go` (`Start`)
- Test: `internal/agent/scheduler_dispatch_test.go` (append)

**Interfaces:**
- Consumes: `(*throughputLoader).Load` (Task 2), `Scheduler.throughput` (Task 5).
- Produces: `(*Scheduler).currentTickInterval() time.Duration`, `(*Scheduler).applyTickInterval(d time.Duration) bool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/scheduler_dispatch_test.go`:

```go
func TestApplyTickInterval(t *testing.T) {
	s := newDispatchScheduler(t)
	s.applyTickInterval(2 * time.Second)

	if got := s.currentTickInterval(); got != 2*time.Second {
		t.Fatalf("currentTickInterval = %s, want 2s", got)
	}
	if changed := s.applyTickInterval(2 * time.Second); changed {
		t.Error("re-applying the same interval should report no change")
	}
	if changed := s.applyTickInterval(5 * time.Second); !changed {
		t.Error("a new interval should report changed")
	}
	if got := s.currentTickInterval(); got != 5*time.Second {
		t.Errorf("currentTickInterval = %s, want 5s", got)
	}
}

func TestApplyTickInterval_IgnoresNonPositive(t *testing.T) {
	s := newDispatchScheduler(t)
	s.applyTickInterval(3 * time.Second)

	if changed := s.applyTickInterval(0); changed {
		t.Error("zero interval must be ignored, not applied")
	}
	if got := s.currentTickInterval(); got != 3*time.Second {
		t.Errorf("currentTickInterval = %s, want unchanged 3s", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestApplyTickInterval -race -count=1`
Expected: FAIL — `s.applyTickInterval undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/agent/scheduler.go`, add next to the other small helpers:

```go
func (s *Scheduler) currentTickInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tickInterval
}

// applyTickInterval stores a new tick cadence and reports whether it changed,
// so the caller only resets the ticker when it has to.
func (s *Scheduler) applyTickInterval(d time.Duration) bool {
	if d <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tickInterval == d {
		return false
	}
	s.tickInterval = d
	return true
}
```

Then, in `Start`, replace the ticker construction and the tick case:

```go
	ticker := time.NewTicker(s.currentTickInterval())
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("scheduler: tick panicked (recovered)", "panic", r)
					}
				}()
				s.SyncFromRegistry()
				s.tick(runner)
			}()
			// Retuning the cadence must not require a restart — a restart kills
			// whatever cycle is running.
			if d := s.throughput.Load().TickInterval; s.applyTickInterval(d) {
				slog.Info("scheduler: tick interval changed", "new", d)
				ticker.Reset(d)
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestApplyTickInterval -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/scheduler.go internal/agent/scheduler_dispatch_test.go
git commit -m "feat(agent): hot-reload the scheduler tick interval"
```

---

### Task 7: Cycle-complete observability

**Files:**
- Modify: `internal/agent/scheduler.go` (`runJob` log args)
- Test: `internal/agent/scheduler_dispatch_test.go` (append)

**Interfaces:**
- Consumes: `(*Scheduler).groupFor` (Task 5), `(*laneState).inUse` (Task 4).
- Produces: no new exported API; `scheduler: cycle complete` gains `group` and `lanes_in_use` fields.

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/scheduler_dispatch_test.go`:

```go
func TestCycleCompleteLogIncludesGroupAndLanes(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := newDispatchScheduler(t, Definition{Name: "goap-a", Tree: "domain:goap_fusion"})
	dueJob(t, s, "goap-a")

	done := make(chan struct{})
	s.tick(func(ctx RunContext) (string, string, *RunResult, error) {
		defer close(done)
		return "success", "ok", nil, nil
	})
	<-done

	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), "cycle complete") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	out := buf.String()
	if !strings.Contains(out, "cycle complete") {
		t.Fatalf("no cycle-complete line logged: %s", out)
	}
	if !strings.Contains(out, `"group":"repo-main"`) {
		t.Errorf("cycle-complete line missing group=repo-main: %s", out)
	}
	if !strings.Contains(out, `"lanes_in_use"`) {
		t.Errorf("cycle-complete line missing lanes_in_use: %s", out)
	}
}
```

Add `"bytes"`, `"log/slog"`, `"strings"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestCycleCompleteLog -race -count=1`
Expected: FAIL — the line has neither `group` nor `lanes_in_use`.

- [ ] **Step 3: Write minimal implementation**

In `runJob`, extend the `logArgs` slice built for the cycle-complete line:

```go
	logArgs := []any{
		"agent", job.AgentName,
		"outcome", outcome,
		"duration", duration.Truncate(time.Second).String(),
		"quality", quality,
		"run_count", job.RunCount,
		// Throughput diagnostics: shows whether the fleet is lane-bound or
		// genuinely idle without cross-referencing the job table.
		"group", s.groupFor(job.AgentName),
		"lanes_in_use", s.lanes.inUse(),
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `/usr/local/go/bin/go test ./internal/agent/ -run TestCycleCompleteLog -race -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/scheduler.go internal/agent/scheduler_dispatch_test.go
git commit -m "feat(agent): log group and lane usage on cycle complete"
```

---

### Task 8: Ship it — full verification, docs, safe rollout default

**Files:**
- Modify: `CHANGELOG.md`
- Create: `docs/scheduler-throughput.md`
- Modify: `~/.config/systemd/user/bt-agent.service` (operator step, outside the repo)

**Interfaces:**
- Consumes: everything above.
- Produces: no code API.

- [ ] **Step 1: Run the full package suite plus a build**

Run: `/usr/local/go/bin/go test ./internal/agent/ -race -count=1 > /tmp/agent-tests.txt 2>&1; echo "exit=$?"`
Expected: `exit=0`

Run: `/usr/local/go/bin/go build -buildvcs=false ./... > /tmp/build.txt 2>&1; echo "exit=$?"`
Expected: `exit=0` (`-buildvcs=false` is required — this repo's `.git` is bare-configured and VCS stamping fails with exit 128)

Run: `/usr/local/go/bin/go vet -buildvcs=false ./internal/agent/ > /tmp/vet.txt 2>&1; echo "exit=$?"`
Expected: `exit=0`

- [ ] **Step 2: Write the operator doc**

Create `docs/scheduler-throughput.md`:

```markdown
# Scheduler Throughput

The scheduler dispatches due jobs into a bounded pool of concurrent lanes.

## Knobs

Precedence: built-in default → env → file.

| Setting | Env | File key | Default |
|---|---|---|---|
| Lane budget | `BT_SCHEDULER_MAX_CONCURRENT` | `max_concurrent` | 3 |
| Tick cadence | `BT_SCHEDULER_TICK_INTERVAL` | `tick_interval` | 1m |
| Per-group cap | — | `group_limits` | 1 per group |

File: `~/.go-bt-evolve/throughput.json`, re-read every tick. No restart needed.

```json
{
  "max_concurrent": 3,
  "tick_interval": "1m",
  "group_limits": { "repo-main": 1 }
}
```

Malformed JSON keeps the last-good budget and logs a WARN.

## Exclusion groups

At most `group_limits[group]` runs from a group are in flight at once. An agent
declares its group with `concurrency_group:` in its YAML. When the field is
absent the group is derived from the tree: `domain:goap_fusion*`,
`domain:superpowers*`, and `domain:self_fix*` map to `repo-main`; everything
else gets `agent:<name>` (no cross-agent exclusion).

`repo-main` must stay at 1 until coding cycles get isolated checkouts: the
goap-fusion materializer runs `git checkout -f HEAD -- .` in the shared main
checkout, so two overlapping cycles would corrupt each other.

## Rollback

Write `{"max_concurrent": 1}` to `~/.go-bt-evolve/throughput.json`. That
reproduces the pre-concurrency serial scheduler on the next tick — no restart,
no redeploy.

## Reading the logs

- `scheduler: cycle complete` carries `group` and `lanes_in_use`.
- `scheduler: lanes saturated` (max once a minute) means jobs are waiting on lanes.
- `scheduler: throughput config changed` records every applied retune.
```

- [ ] **Step 3: Add the CHANGELOG entry**

Under `## [Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
- **(agents):** Concurrent scheduler lanes — `tick()` now dispatches due jobs into a bounded pool instead of running them inline, so a 37-minute goap cycle no longer blocks all nine agents (measured baseline: 85 cycles / 22.4h busy / 93% duty on a single lane while the 12-core host idled at load 0.6). Admission is guarded by job in-flight, agent in-flight, exclusion group, and finally the circuit breaker (last, because `Allowed()` spends a half-open probe). Agents declare `concurrency_group:`; repo-mutating trees default to `repo-main` at limit 1, preserving the existing main-checkout guarantee. Budget comes from `BT_SCHEDULER_MAX_CONCURRENT` / `BT_SCHEDULER_TICK_INTERVAL` and the hot-reloaded `~/.go-bt-evolve/throughput.json`; `max_concurrent: 1` reproduces the old serial behavior as a no-restart rollback. Spec: `docs/superpowers/specs/2026-08-02-scheduler-throughput-design.md`.
```

- [ ] **Step 4: Commit the code and docs**

```bash
git add CHANGELOG.md docs/scheduler-throughput.md
git commit -m "docs(agents): scheduler throughput operator guide"
```

- [ ] **Step 5: Deploy at serial parity, then open the lanes**

Deploying is a separate, operator-visible step. Rebuild is mandatory: an env-only
change cannot introduce a flag the deployed binary does not have (this exact
trap cost a false "done" on 2026-08-01, when a stale 8-day-old `bin/bt-agent`
silently ignored a new env var).

```bash
cd /home/nico/go-bt-evolve
cp -p bin/bt-agent bin/bt-agent.pre-lanes-backup
/usr/local/go/bin/go build -buildvcs=false -o bin/bt-agent ./cmd/bt-agent
printf '{"max_concurrent": 1}\n' > ~/.go-bt-evolve/throughput.json
```

Restart only when no fleet claude run is in flight — a restart kills the running
cycle (`KillMode=control-group`):

```bash
systemctl --user restart bt-agent.service
```

Verify serial parity for one cycle, then open the lanes with no restart:

```bash
printf '{"max_concurrent": 3, "group_limits": {"repo-main": 1}}\n' > ~/.go-bt-evolve/throughput.json
journalctl --user -u bt-agent.service -f | grep -E "lanes_in_use|lanes saturated|throughput config changed"
```

Watch for a day: `lanes_in_use` > 1, no new rate-limit backoff, cycle outcomes
unchanged. Those numbers size the follow-up per-cycle-checkout spec.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| Bounded non-blocking lane dispatch | 5 |
| Oldest-first ordering | 5 |
| Guard 1–2 (job / agent in-flight) | 4, 5 |
| Guard 3 (exclusion group) | 3, 4, 5 |
| Guard 4 (breaker last, probe not spent) | 5 |
| `concurrency_group` field + tree-derived default | 3 |
| Env knobs | 1 |
| File overlay, mtime reload, last-good | 2 |
| Clamps, `max_concurrent: 1` rollback | 1, 8 |
| Reload affects future dispatch only | 2, 6 |
| Tick-interval reset | 6 |
| Single-defer lane release, panic safety | 4, 5 |
| `Stop()` does not drain | unchanged — no task needed |
| Throttled saturation log | 5 |
| `lanes_in_use` / `group` on cycle complete | 7 |
| Test matrix rows 1–10 | 1–7 |
| Rollout | 8 |

**Placeholders:** none — every step carries runnable code or an exact command.

**Type consistency:** `ThroughputConfig`, `throughputLoader`, `laneState`,
`ConcurrencyGroupFor`, `groupFor`, `markInFlight`, `finishDispatch`,
`applyTickInterval`, and `currentTickInterval` are spelled identically wherever
they appear across tasks. `tryAcquire`/`release`/`inUse` keep the same argument
order (`jobID, agent, group`) in the definition and all call sites.
