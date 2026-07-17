# Runtime Tree Mutation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let behavior trees be mutated (nodes added/removed) while they are being ticked, via a per-run mutation queue applied copy-on-write at tick boundaries, per spec `docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md`.

**Architecture:** The serializable tree stays the source of truth. A live-run registry (RunID → queue/journal) accepts `MutationOp`s from action nodes (`bb.EnqueueMutation`) and MCP tools. `RunTask`'s tick loop gains two nil-checked hook calls that drain the queue at quiescent points, apply ops per-op-atomically to a clone, validate, rebuild the compiled tree, migrate pointer-keyed node state by clone correspondence, and swap. `Persist: true` snapshots the whole live tree through an injection hook to an override store consulted by tree resolution.

**Tech Stack:** Go 1.26 (at `/usr/local/go/bin/go`, NOT on PATH — prefix every command), go-bt v0.1.0 (vendored semantics: composites hold private child slices; `BTContext` state maps keyed by `Command` pointer), stdlib only.

## Global Constraints

- Prefix every go/make command: `PATH=/usr/local/go/bin:$PATH ...`
- `internal/engine` must not import higher-level packages (`internal/agent`, `internal/domains`, `cmd/*`). Cross-layer calls use nil-checked injection-hook vars wired from `cmd/bt-agent` (pattern: `engine.DelegateToTreeFn`).
- Persistence is atomic JSON under `~/.go-bt-evolve/` (tmp + rename). Reuse `evolution.TreeStore.SaveTo`.
- `Blackboard` is shallow-copied by `forkBlackboard` (reactive_parallel.go) — every new Blackboard field must be a pointer or map so copies share it; no mutexes by value on Blackboard.
- New engine files go in `internal/engine/` flat, tests alongside as `_test.go`, `package engine`.
- Per-run mutation cap: 100 ops. Origins: `"operator"`, `"tree"`, `"llm"`. Only llm-origin adds run the error-handler proposal policy (`validateErrorHandlerProposal`).
- A run's live tree is always a private clone — never mutate the tree returned by a resolver in place (built-in domain trees may be shared package state).
- The repo pre-commit hook runs gofmt → vet → golangci-lint → mod tidy → doc drift → ci-doctor → short tests on every commit; a PostToolUse hook gofmts edited `.go` files.
- Known-ignore: builds/tests from `.claude/worktrees` fail with "VCS status exit 128" — run with `-buildvcs=false` there, or work in the main checkout.
- Spec deltas locked here (both strictly safer): (1) the spec's leaf-parent add-rejection generalizes to a per-type max-children guard, because single-child decorators (`Retry`, `Inverter`, …) silently ignore extra children just like leaves do; (2) instead of a duplicated tick loop in `RunTaskMutable`, `RunTask` itself gets two nil-checked hook lines — zero behavior change when the run isn't mutable, no drift between two loops.

---

### Task 1: Mutation ops on the serializable tree

**Files:**
- Create: `internal/engine/tree_mutation.go`
- Test: `internal/engine/tree_mutation_test.go`

**Interfaces:**
- Consumes: `evolution.SerializableNode`, `ValidateTreeFull` (validate_suite.go), `validateErrorHandlerProposal(node, takenNames)` (error_handler_claude.go), `treeHasSubTreeRefs` (tree_expand.go).
- Produces (used by Tasks 2–4):
  - `type MutationOp struct { Kind, ParentPath string; Index int; Path, ExpectName string; Subtree *evolution.SerializableNode; Persist bool; Origin string }`
  - `const OriginOperator = "operator"; OriginTree = "tree"; OriginLLM = "llm"`
  - `cloneNode(n *evolution.SerializableNode) *evolution.SerializableNode` — plain deep copy
  - `mapCorrespondence(oldN, newN, mutParent *evolution.SerializableNode, kind string, at int, corr map[*evolution.SerializableNode]*evolution.SerializableNode)` — dual-walk pairing old→new computed AFTER the op applied (a slice insert can reallocate the parent's children array and move sibling addresses, so correspondence must never be captured during the clone)
  - `applyMutationOp(root *evolution.SerializableNode, op MutationOp) (*evolution.SerializableNode, int, error)` — in-place on `root`, returns (directly-mutated parent, child index the op touched, error)
  - `validateMutatedTree(root *evolution.SerializableNode, op MutationOp) error`
  - `maxChildrenForType(t string) int`

- [ ] **Step 1: Write the failing tests**

```go
// internal/engine/tree_mutation_test.go
package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func mkTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "a"},
			{Type: "Selector", Name: "sel", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "b"},
				{Type: "Action", Name: "c"},
			}},
		},
	}
}

func TestCloneNode(t *testing.T) {
	src := mkTree()
	dst := cloneNode(src)
	if dst == src || &dst.Children[1] == &src.Children[1] {
		t.Fatal("clone must not alias source")
	}
	dst.Children[1].Children[0].Name = "changed"
	if src.Children[1].Children[0].Name != "b" {
		t.Fatal("mutating clone must not touch source")
	}
	// Metadata map must be copied, not aliased.
	src2 := &evolution.SerializableNode{Type: "Action", Name: "m",
		Metadata: map[string]any{"k": "v"}}
	dst2 := cloneNode(src2)
	dst2.Metadata["k"] = "w"
	if src2.Metadata["k"] != "v" {
		t.Fatal("metadata map aliased between source and clone")
	}
}

func TestMapCorrespondenceIdentityWalk(t *testing.T) {
	old := mkTree()
	nw := cloneNode(old)
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, nil, "", 0, corr)
	if corr[old] != nw || corr[&old.Children[1]] != &nw.Children[1] ||
		corr[&old.Children[1].Children[0]] != &nw.Children[1].Children[0] {
		t.Fatal("identity walk must pair every node 1:1")
	}
}

func TestMapCorrespondenceAfterAddSurvivesRealloc(t *testing.T) {
	// The regression this guards: correspondence captured during the clone
	// goes stale when applyMutationOp's insert reallocates the parent's
	// children backing array. Correspondence must therefore be computed
	// AFTER the op, via this dual walk with shift arithmetic.
	old := mkTree()
	nw := cloneNode(old)
	parent, at, err := applyMutationOp(nw, MutationOp{
		Kind: "add", ParentPath: "1", Index: 0,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, parent, "add", at, corr)
	// Shifted siblings b (old 1.0 → new 1.1) and c (old 1.1 → new 1.2)
	// must pair with their POST-reallocation addresses.
	if corr[&old.Children[1].Children[0]] != &nw.Children[1].Children[1] {
		t.Fatal("sibling b must pair across the insert shift")
	}
	if corr[&old.Children[1].Children[1]] != &nw.Children[1].Children[2] {
		t.Fatal("sibling c must pair across the insert shift")
	}
	// The inserted node has no old counterpart.
	for _, nwNode := range corr {
		if nwNode == &nw.Children[1].Children[0] {
			t.Fatal("inserted node must not be paired")
		}
	}
}

func TestMapCorrespondenceAfterRemove(t *testing.T) {
	old := mkTree()
	nw := cloneNode(old)
	parent, at, err := applyMutationOp(nw, MutationOp{Kind: "remove", Path: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, parent, "remove", at, corr)
	if corr[&old.Children[1].Children[1]] != &nw.Children[1].Children[0] {
		t.Fatal("sibling c must pair across the removal shift")
	}
	if corr[&old.Children[1].Children[0]] != nil {
		t.Fatal("removed node must not be paired")
	}
}

func TestApplyMutationOpAdd(t *testing.T) {
	root := mkTree()
	parent, idx, err := applyMutationOp(root, MutationOp{
		Kind: "add", ParentPath: "1", Index: 1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Name != "sel" || idx != 1 {
		t.Fatalf("want parent sel idx 1, got %s idx %d", parent.Name, idx)
	}
	got := []string{root.Children[1].Children[0].Name, root.Children[1].Children[1].Name, root.Children[1].Children[2].Name}
	if got[0] != "b" || got[1] != "x" || got[2] != "c" {
		t.Fatalf("insertion order wrong: %v", got)
	}
	// Append with Index -1.
	if _, idx, err = applyMutationOp(root, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "tail"}}); err != nil || idx != 2 {
		t.Fatalf("append: idx=%d err=%v", idx, err)
	}
	if root.Children[2].Name != "tail" {
		t.Fatal("append did not land at end of root children")
	}
}

func TestApplyMutationOpRemove(t *testing.T) {
	root := mkTree()
	parent, idx, err := applyMutationOp(root, MutationOp{Kind: "remove", Path: "1.0", ExpectName: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Name != "sel" || idx != 0 || len(root.Children[1].Children) != 1 || root.Children[1].Children[0].Name != "c" {
		t.Fatal("remove 1.0 must delete node b")
	}
}

func TestApplyMutationOpRejections(t *testing.T) {
	cases := []struct {
		name string
		op   MutationOp
		want string
	}{
		{"remove root", MutationOp{Kind: "remove", Path: ""}, "root"},
		{"bad path", MutationOp{Kind: "remove", Path: "9.9"}, "resolve"},
		{"expect name mismatch", MutationOp{Kind: "remove", Path: "0", ExpectName: "zzz"}, "expect"},
		{"add under leaf", MutationOp{Kind: "add", ParentPath: "0", Index: -1,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"}}, "children"},
		{"nil subtree", MutationOp{Kind: "add", ParentPath: "", Index: -1}, "subtree"},
		{"subtree with SubTreeRef", MutationOp{Kind: "add", ParentPath: "", Index: -1,
			Subtree: &evolution.SerializableNode{Type: "SubTreeRef", Name: "blk"}}, "SubTreeRef"},
		{"unknown kind", MutationOp{Kind: "replace", Path: "0"}, "kind"},
		{"index out of range", MutationOp{Kind: "add", ParentPath: "", Index: 7,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"}}, "index"},
	}
	for _, tc := range cases {
		root := mkTree()
		if _, _, err := applyMutationOp(root, tc.op); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: want error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestApplyMutationOpDecoratorCap(t *testing.T) {
	root := &evolution.SerializableNode{Type: "Retry", Name: "r", MaxRetries: 2,
		Children: []evolution.SerializableNode{{Type: "Action", Name: "a"}}}
	if _, _, err := applyMutationOp(root, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"}}); err == nil {
		t.Fatal("adding a second child to a single-child decorator must be rejected")
	}
}

func TestValidateMutatedTreeLLMAllowlist(t *testing.T) {
	root := mkTree()
	// Operator-origin: a plain action subtree is fine.
	if err := validateMutatedTree(root, MutationOp{Kind: "add", Origin: OriginOperator,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"}}); err != nil {
		t.Fatalf("operator origin should pass: %v", err)
	}
	// LLM-origin: same subtree fails the proposal policy (no Condition guard).
	if err := validateMutatedTree(root, MutationOp{Kind: "add", Origin: OriginLLM,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "x"}}); err == nil {
		t.Fatal("llm origin without guard-first structure must be rejected")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestCloneNode|TestMapCorrespondence|TestApplyMutationOp|TestValidateMutatedTree' -count=1`
Expected: FAIL to compile — `undefined: cloneNode`, `undefined: MutationOp`, etc.

- [ ] **Step 3: Implement tree_mutation.go**

```go
// internal/engine/tree_mutation.go
// Structural mutation ops applied to a live run's serializable tree
// (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Mutation origins. Only llm-origin grafts pass the error-handler proposal
// policy; the MCP entry point stamps operator, in-engine callers declare
// tree or llm.
const (
	OriginOperator = "operator"
	OriginTree     = "tree"
	OriginLLM      = "llm"
)

// MutationOp is one structural edit to a live tree.
type MutationOp struct {
	Kind       string                      `json:"kind"`                  // "add" | "remove"
	ParentPath string                      `json:"parent_path,omitempty"` // add: index path of parent ("" = root, "0.2" = root.Children[0].Children[2])
	Index      int                         `json:"index,omitempty"`       // add: insertion position; -1 = append
	Path       string                      `json:"path,omitempty"`        // remove: index path of the node to remove
	ExpectName string                      `json:"expect_name,omitempty"` // optional: resolved node's Name must match
	Subtree    *evolution.SerializableNode `json:"subtree,omitempty"`     // add: subtree to graft
	Persist    bool                        `json:"persist,omitempty"`     // snapshot resulting tree to the store
	Origin     string                      `json:"origin,omitempty"`
}

// cloneNode deep-copies n. Correspondence for state migration is computed
// separately by mapCorrespondence AFTER mutation ops run: applyMutationOp's
// slice insert can reallocate a parent's children backing array and move
// sibling addresses, so pairings captured during the clone would go stale.
func cloneNode(n *evolution.SerializableNode) *evolution.SerializableNode {
	if n == nil {
		return nil
	}
	cp := *n
	if n.Metadata != nil {
		cp.Metadata = make(map[string]any, len(n.Metadata))
		for k, v := range n.Metadata {
			cp.Metadata[k] = v
		}
	}
	if n.Edges != nil {
		cp.Edges = append([]evolution.TypedEdge(nil), n.Edges...)
	}
	if len(n.Children) > 0 {
		cp.Children = make([]evolution.SerializableNode, len(n.Children))
		for i := range n.Children {
			cp.Children[i] = *cloneNode(&n.Children[i])
		}
	}
	return &cp
}

// mapCorrespondence pairs every node of the old tree with its counterpart in
// the new tree (old → new in corr), walking both in lockstep. mutParent, when
// non-nil, is the node IN THE NEW TREE whose direct child list was changed by
// one (kind, at) op; its children pair with the appropriate index shift and
// the inserted/removed child stays unpaired. Everywhere else the shapes are
// identical by construction; a length mismatch stops pairing that subtree
// (defensive — unpaired nodes just start with fresh state).
func mapCorrespondence(oldN, newN, mutParent *evolution.SerializableNode, kind string, at int, corr map[*evolution.SerializableNode]*evolution.SerializableNode) {
	if oldN == nil || newN == nil {
		return
	}
	corr[oldN] = newN
	if newN == mutParent {
		switch kind {
		case "add":
			for i := range oldN.Children {
				ni := i
				if i >= at {
					ni = i + 1
				}
				if ni < len(newN.Children) {
					mapCorrespondence(&oldN.Children[i], &newN.Children[ni], mutParent, kind, at, corr)
				}
			}
		case "remove":
			for i := range oldN.Children {
				if i == at {
					continue // the removed child has no counterpart
				}
				ni := i
				if i > at {
					ni = i - 1
				}
				if ni < len(newN.Children) {
					mapCorrespondence(&oldN.Children[i], &newN.Children[ni], mutParent, kind, at, corr)
				}
			}
		}
		return
	}
	if len(oldN.Children) != len(newN.Children) {
		return
	}
	for i := range oldN.Children {
		mapCorrespondence(&oldN.Children[i], &newN.Children[i], mutParent, kind, at, corr)
	}
}

// maxChildrenForType bounds how many children a node type meaningfully
// executes: 0 for leaves, 1 for single-child decorators, -1 for unbounded
// composites. buildNodeInner silently ignores extra children on leaf and
// single-child types, so an add beyond the cap would graft dead structure.
func maxChildrenForType(t string) int {
	switch t {
	case "Action", "Condition", "ChainAction", "AlwaysSucceed", "SubTreeRef":
		return 0
	case "Retry", "Inverter", "Succeeder", "Repeater", "Runner", "Timeout",
		"Budget", "RateLimit", "CircuitBreaker", "Monitor", "QualityGate",
		"CheckpointVerifier", "SemaphoreGuard", "CachedCondition",
		"ClaudeErrorHandler", "HumanApprovalGate":
		return 1
	default:
		return -1
	}
}

// parseIndexPath turns "0.2.1" into []int{0, 2, 1}; "" is the root.
func parseIndexPath(p string) ([]int, error) {
	if p == "" {
		return nil, nil
	}
	parts := strings.Split(p, ".")
	idx := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("bad index path %q: segment %q", p, s)
		}
		idx[i] = n
	}
	return idx, nil
}

// resolveIndexPath walks root by child indices.
func resolveIndexPath(root *evolution.SerializableNode, idx []int) (*evolution.SerializableNode, error) {
	n := root
	for depth, i := range idx {
		if i >= len(n.Children) {
			return nil, fmt.Errorf("cannot resolve path: index %d at depth %d exceeds %d children of %s %q",
				i, depth, len(n.Children), n.Type, n.Name)
		}
		n = &n.Children[i]
	}
	return n, nil
}

// applyMutationOp applies op to root in place. It returns the directly-mutated
// parent node and the child index the op touched (for MemSequence cursor
// arithmetic in the apply loop). Structural guards live here; whole-tree
// validation is validateMutatedTree.
func applyMutationOp(root *evolution.SerializableNode, op MutationOp) (*evolution.SerializableNode, int, error) {
	switch op.Kind {
	case "add":
		if op.Subtree == nil {
			return nil, 0, fmt.Errorf("add: subtree is nil")
		}
		if treeHasSubTreeRefs(op.Subtree) {
			return nil, 0, fmt.Errorf("add: subtree contains SubTreeRef nodes — expand before grafting")
		}
		idx, err := parseIndexPath(op.ParentPath)
		if err != nil {
			return nil, 0, err
		}
		parent, err := resolveIndexPath(root, idx)
		if err != nil {
			return nil, 0, err
		}
		if op.ExpectName != "" && parent.Name != op.ExpectName {
			return nil, 0, fmt.Errorf("add: expect_name %q does not match parent %q", op.ExpectName, parent.Name)
		}
		if maxN := maxChildrenForType(parent.Type); maxN >= 0 && len(parent.Children)+1 > maxN {
			if maxN == 0 {
				return nil, 0, fmt.Errorf("add: %s %q is a leaf type and cannot take children", parent.Type, parent.Name)
			}
			return nil, 0, fmt.Errorf("add: %s %q executes at most %d children", parent.Type, parent.Name, maxN)
		}
		at := op.Index
		if at < 0 {
			at = len(parent.Children)
		}
		if at > len(parent.Children) {
			return nil, 0, fmt.Errorf("add: index %d out of range (parent has %d children)", at, len(parent.Children))
		}
		sub := cloneNode(op.Subtree) // never alias caller-owned memory
		parent.Children = append(parent.Children, evolution.SerializableNode{})
		copy(parent.Children[at+1:], parent.Children[at:])
		parent.Children[at] = *sub
		return parent, at, nil
	case "remove":
		idx, err := parseIndexPath(op.Path)
		if err != nil {
			return nil, 0, err
		}
		if len(idx) == 0 {
			return nil, 0, fmt.Errorf("remove: cannot remove the root node")
		}
		parent, err := resolveIndexPath(root, idx[:len(idx)-1])
		if err != nil {
			return nil, 0, err
		}
		at := idx[len(idx)-1]
		if at >= len(parent.Children) {
			return nil, 0, fmt.Errorf("cannot resolve path %q: index %d exceeds %d children", op.Path, at, len(parent.Children))
		}
		target := &parent.Children[at]
		if op.ExpectName != "" && target.Name != op.ExpectName {
			return nil, 0, fmt.Errorf("remove: expect_name %q does not match node %q", op.ExpectName, target.Name)
		}
		parent.Children = append(parent.Children[:at], parent.Children[at+1:]...)
		return parent, at, nil
	default:
		return nil, 0, fmt.Errorf("unknown mutation kind %q", op.Kind)
	}
}

// validateMutatedTree gates the whole post-op tree, plus the llm-origin
// allowlist policy on the grafted subtree (the same security boundary that
// guards ClaudeErrorHandler's auto-executed proposals).
func validateMutatedTree(root *evolution.SerializableNode, op MutationOp) error {
	info := ValidateTreeFull(root)
	if !info.Valid() {
		return fmt.Errorf("mutated tree fails validation: %v", info.Errors)
	}
	if op.Kind == "add" && op.Origin == OriginLLM {
		if err := validateErrorHandlerProposal(op.Subtree, map[string]bool{}); err != nil {
			return fmt.Errorf("llm-origin subtree rejected by proposal policy: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestCloneNode|TestMapCorrespondence|TestApplyMutationOp|TestValidateMutatedTree' -count=1 -race`
Expected: PASS (all 9 test functions)

Note: if `ValidateTreeFull` is stricter than expected on `mkTree` (e.g. name rules), check its rules in `internal/engine/validate_suite.go` and adjust `mkTree` names — do NOT weaken the production code.

- [ ] **Step 5: Commit**

```bash
cd /home/nico/go-bt-evolve && git add internal/engine/tree_mutation.go internal/engine/tree_mutation_test.go && git commit -m "feat(engine): serializable-tree mutation ops with tracked clone and guards"
```

---

### Task 2: Live-run registry, mutation queue, journal, build capture

**Files:**
- Create: `internal/engine/live_run.go`
- Modify: `internal/engine/tree.go` (Blackboard fields ~line 168; buildNode ~line 225)
- Test: `internal/engine/live_run_test.go`

**Interfaces:**
- Consumes: Task 1 (`MutationOp`), `blackboard.NewRunID()` (already imported by engine).
- Produces (used by Tasks 3–4):
  - `type LiveRunInfo struct{ Agent, TreeID string }`
  - `type MutationRecord struct { OpID string; Op MutationOp; Status, Error string; Generation int; At time.Time }` (Status: `"applied"` | `"rejected"`)
  - `type LiveRunStatus struct { RunID, Agent, TreeID string; Generation, Pending, JournalLen int; StartedAt time.Time }`
  - `registerLiveRun(bb *Blackboard, info LiveRunInfo) *liveRun`, `deregisterLiveRun(runID string)`
  - `(*liveRun).enqueue(op MutationOp) (string, error)`, `(*liveRun).drain() []queuedOp`, `(*liveRun).record(rec MutationRecord)`
  - `ListLiveRuns() []LiveRunStatus`, `EnqueueLiveMutation(runID string, op MutationOp) (string, error)`, `LiveMutationJournal(runID string) ([]MutationRecord, error)`
  - `(*Blackboard).EnqueueMutation(op MutationOp) (string, error)`
  - Blackboard gains unexported `liveRun *liveRun` and `buildCapture map[*evolution.SerializableNode]btcore.Command[Blackboard]`; `buildNode` records the INNER command per source node when `buildCapture != nil`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/engine/live_run_test.go
package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestLiveRunRegistryLifecycle(t *testing.T) {
	bb := &Blackboard{RunID: "run-lifecycle-1"}
	lr := registerLiveRun(bb, LiveRunInfo{Agent: "agentX", TreeID: "treeY"})
	defer deregisterLiveRun(lr.runID)
	if bb.liveRun != lr {
		t.Fatal("registerLiveRun must attach the run to the blackboard")
	}
	found := false
	for _, s := range ListLiveRuns() {
		if s.RunID == "run-lifecycle-1" && s.Agent == "agentX" && s.TreeID == "treeY" {
			found = true
		}
	}
	if !found {
		t.Fatal("registered run must be listed")
	}
	deregisterLiveRun(lr.runID)
	if _, err := EnqueueLiveMutation("run-lifecycle-1", MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("enqueue to a deregistered run must error")
	}
}

func TestLiveRunEnqueueAndDrain(t *testing.T) {
	bb := &Blackboard{RunID: "run-q-1"}
	lr := registerLiveRun(bb, LiveRunInfo{})
	defer deregisterLiveRun(lr.runID)
	id1, err := bb.EnqueueMutation(MutationOp{Kind: "remove", Path: "0"})
	if err != nil || id1 == "" {
		t.Fatalf("bb enqueue: id=%q err=%v", id1, err)
	}
	id2, err := EnqueueLiveMutation("run-q-1", MutationOp{Kind: "remove", Path: "1"})
	if err != nil || id2 == id1 {
		t.Fatalf("registry enqueue: id=%q err=%v", id2, err)
	}
	ops := lr.drain()
	if len(ops) != 2 || ops[0].id != id1 || ops[1].id != id2 {
		t.Fatalf("drain must return queued ops in order, got %+v", ops)
	}
	if len(lr.drain()) != 0 {
		t.Fatal("second drain must be empty")
	}
}

func TestLiveRunOpCap(t *testing.T) {
	bb := &Blackboard{RunID: "run-cap-1"}
	lr := registerLiveRun(bb, LiveRunInfo{})
	defer deregisterLiveRun(lr.runID)
	for i := 0; i < maxMutationsPerRun; i++ {
		if _, err := lr.enqueue(MutationOp{Kind: "remove", Path: "0"}); err != nil {
			t.Fatalf("enqueue %d unexpectedly failed: %v", i, err)
		}
	}
	if _, err := lr.enqueue(MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("enqueue beyond maxMutationsPerRun must error")
	}
}

func TestEnqueueWithoutLiveRun(t *testing.T) {
	bb := &Blackboard{}
	if _, err := bb.EnqueueMutation(MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("EnqueueMutation on a non-mutable run must error")
	}
}

func TestBuildNodeCapture(t *testing.T) {
	ser := &evolution.SerializableNode{Type: "MemSequence", Name: "memroot",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "s1"},
			{Type: "Action", Name: "s2"},
		}}
	bb := &Blackboard{buildCapture: map[*evolution.SerializableNode]btcore.Command[Blackboard]{}}
	cmd := buildNode(ser, bb, "")
	if cmd == nil {
		t.Fatal("buildNode returned nil")
	}
	if len(bb.buildCapture) != 3 {
		t.Fatalf("capture must record every built node, got %d entries", len(bb.buildCapture))
	}
	// The captured command must be the INNER command — the pointer the
	// library keys MemSequenceState by — not the observeNode wrapper that
	// buildNode returns.
	if bb.buildCapture[ser] == cmd {
		t.Fatal("captured command must be the inner command, not the observeNode wrapper")
	}
	if bb.buildCapture[&ser.Children[0]] == nil || bb.buildCapture[&ser.Children[1]] == nil {
		t.Fatal("children must be captured by their in-tree addresses")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestLiveRun|TestEnqueueWithout|TestBuildNodeCapture' -count=1`
Expected: FAIL to compile — `undefined: registerLiveRun`, `bb.liveRun undefined`, etc.

- [ ] **Step 3: Implement live_run.go and the tree.go edits**

```go
// internal/engine/live_run.go
// Live-run registry: mutable runs register here so action nodes and MCP
// callers can enqueue MutationOps against them by RunID
// (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// maxMutationsPerRun caps enqueued ops per run so a self-growing tree cannot
// mutate without bound. The 1000-tick cap and tree timeout still apply.
const maxMutationsPerRun = 100

// LiveRunInfo names the run for registry listing and persistence.
type LiveRunInfo struct{ Agent, TreeID string }

// MutationRecord is one journal entry: an op that was applied or rejected.
type MutationRecord struct {
	OpID       string      `json:"op_id"`
	Op         MutationOp  `json:"op"`
	Status     string      `json:"status"` // "applied" | "rejected"
	Error      string      `json:"error,omitempty"`
	Generation int         `json:"generation"`
	At         time.Time   `json:"at"`
}

// LiveRunStatus is the externally visible summary of a registered run.
type LiveRunStatus struct {
	RunID      string    `json:"run_id"`
	Agent      string    `json:"agent"`
	TreeID     string    `json:"tree_id"`
	Generation int       `json:"generation"`
	Pending    int       `json:"pending"`
	JournalLen int       `json:"journal_len"`
	StartedAt  time.Time `json:"started_at"`
}

type queuedOp struct {
	id string
	op MutationOp
}

// liveRun is one mutable run's mutation state. The tree fields (cur, capture)
// are owned by the run goroutine and only touched at tick boundaries; the
// queue, journal, and counters are mutex-guarded because MCP handlers read
// and write them from other goroutines.
type liveRun struct {
	runID     string
	info      LiveRunInfo
	startedAt time.Time

	mu         sync.Mutex
	queue      []queuedOp
	journal    []MutationRecord
	generation int
	opSeq      int
	total      int // lifetime enqueued count, enforces maxMutationsPerRun

	// Run-goroutine-owned (no lock): current serializable tree and the
	// source-node → inner-command capture of its latest build.
	cur     *evolution.SerializableNode
	capture map[*evolution.SerializableNode]btcore.Command[Blackboard]
}

var liveRuns sync.Map // runID → *liveRun

// registerLiveRun creates and registers the run's mutation state and attaches
// it to bb. A missing RunID gets a generated one so registry lookup works.
func registerLiveRun(bb *Blackboard, info LiveRunInfo) *liveRun {
	if bb.RunID == "" {
		bb.RunID = blackboard.NewRunID()
	}
	lr := &liveRun{runID: bb.RunID, info: info, startedAt: time.Now()}
	liveRuns.Store(lr.runID, lr)
	bb.liveRun = lr
	return lr
}

func deregisterLiveRun(runID string) { liveRuns.Delete(runID) }

func (lr *liveRun) enqueue(op MutationOp) (string, error) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.total >= maxMutationsPerRun {
		return "", fmt.Errorf("run %s: mutation cap (%d) reached", lr.runID, maxMutationsPerRun)
	}
	lr.total++
	lr.opSeq++
	id := fmt.Sprintf("%s-op%d", lr.runID, lr.opSeq)
	lr.queue = append(lr.queue, queuedOp{id: id, op: op})
	return id, nil
}

func (lr *liveRun) drain() []queuedOp {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	ops := lr.queue
	lr.queue = nil
	return ops
}

func (lr *liveRun) record(rec MutationRecord) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.journal = append(lr.journal, rec)
}

func (lr *liveRun) status() LiveRunStatus {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return LiveRunStatus{
		RunID: lr.runID, Agent: lr.info.Agent, TreeID: lr.info.TreeID,
		Generation: lr.generation, Pending: len(lr.queue),
		JournalLen: len(lr.journal), StartedAt: lr.startedAt,
	}
}

// ListLiveRuns returns a snapshot of all registered mutable runs.
func ListLiveRuns() []LiveRunStatus {
	var out []LiveRunStatus
	liveRuns.Range(func(_, v any) bool {
		out = append(out, v.(*liveRun).status())
		return true
	})
	return out
}

// EnqueueLiveMutation queues op against a registered run by RunID.
func EnqueueLiveMutation(runID string, op MutationOp) (string, error) {
	v, ok := liveRuns.Load(runID)
	if !ok {
		return "", fmt.Errorf("no live mutable run %q", runID)
	}
	return v.(*liveRun).enqueue(op)
}

// LiveMutationJournal returns a copy of the run's applied/rejected records.
func LiveMutationJournal(runID string) ([]MutationRecord, error) {
	v, ok := liveRuns.Load(runID)
	if !ok {
		return nil, fmt.Errorf("no live mutable run %q", runID)
	}
	lr := v.(*liveRun)
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return append([]MutationRecord(nil), lr.journal...), nil
}

// EnqueueMutation queues a mutation against this run's own tree, effective at
// the next tick boundary. Callable from action implementations during a tick.
func (bb *Blackboard) EnqueueMutation(op MutationOp) (string, error) {
	if bb.liveRun == nil {
		return "", fmt.Errorf("this run is not mutable (not started via RunTaskMutable)")
	}
	return bb.liveRun.enqueue(op)
}
```

Modify `internal/engine/tree.go` — add two fields to `Blackboard` directly after the `childTicks` field (~line 155), keeping the pointer/map-only rule for fork safety:

```go
	// liveRun and buildCapture support runtime tree mutation
	// (tree_mutation.go / live_run.go). Pointer + map so forkBlackboard's
	// shallow copies share them. buildCapture, when non-nil, makes buildNode
	// record each source node's INNER command — the pointer the go-bt library
	// keys per-node state by — enabling state migration across rebuilds.
	liveRun      *liveRun
	buildCapture map[*evolution.SerializableNode]btcore.Command[Blackboard]
```

Modify `buildNode` (tree.go ~line 225) to capture the inner command:

```go
func buildNode(node *evolution.SerializableNode, bb *Blackboard, parentName string) btcore.Command[Blackboard] {
	inner := buildNodeInner(node, bb, parentName)
	if bb != nil && bb.buildCapture != nil {
		bb.buildCapture[node] = inner
	}
	return observeNode(node, parentName, inner)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestLiveRun|TestEnqueueWithout|TestBuildNodeCapture' -count=1 -race`
Expected: PASS (5 test functions)

Then run the whole engine short suite to catch fallout from the tree.go edit:
Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -short -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/nico/go-bt-evolve && git add internal/engine/live_run.go internal/engine/live_run_test.go internal/engine/tree.go && git commit -m "feat(engine): live-run registry, mutation queue/journal, build capture"
```

---

### Task 3: Tick-boundary apply, state migration, RunTaskMutable

**Files:**
- Create: `internal/engine/run_task_mutable.go`
- Modify: `internal/engine/tree.go` (`RunTask`, two hook lines at ~line 575 and inside the ~line 584 loop)
- Test: `internal/engine/run_task_mutable_test.go`

**Interfaces:**
- Consumes: Tasks 1–2; `prepareTreeForBuild`, `ValidateTreeFull`, `buildNode`, `RunTask`; `audit.Append` (`internal/audit` — leaf utility, stdlib-only, safe for engine to import); `bb.EventBus.Publish` (event_bus.go, `EventMessage{Source, Timestamp, Type, Data, Priority}`).
- Produces (used by Task 4):
  - `var PersistMutatedTreeFn func(info LiveRunInfo, tree *evolution.SerializableNode) error` — nil-checked injection hook
  - `func RunTaskMutable(bb *Blackboard, serTree *evolution.SerializableNode, info LiveRunInfo) (string, error)`
  - `(*liveRun).applyPending(ctx *btcore.BTContext[Blackboard], bb *Blackboard, tree btcore.Command[Blackboard]) btcore.Command[Blackboard]`

- [ ] **Step 1: Write the failing tests**

```go
// internal/engine/run_task_mutable_test.go
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Deterministic external-mutation harness — NO timing races:
//   1. The tree contains a muttest_gate action that returns Running until a
//      channel placed in ChainState["gate_ch"] before the run is closed.
//   2. A helper goroutine waits for the run to appear in the registry,
//      enqueues the op, then POLLS THE JOURNAL until the op is applied or
//      rejected — only then closes the gate channel.
//   3. The gate sees the closed channel on its next tick, which is strictly
//      after the boundary that applied the op. No sleeps, no tick counting.
func init() {
	RegisterAction("muttest_mark_a", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "a")
		return 1
	})
	RegisterAction("muttest_mark_b", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "b")
		return 1
	})
	RegisterAction("muttest_grafted", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "grafted")
		ctx.Blackboard.Result = "grafted node executed in the same run and did a bunch of useful work here"
		return 1
	})
	// Grafts a sibling action after itself on first execution, then returns
	// Running once so the next tick boundary applies the graft.
	RegisterAction("muttest_self_graft", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		if b.ChainState["grafted"] != true {
			b.ChainState["grafted"] = true
			_, err := b.EnqueueMutation(MutationOp{
				Kind: "add", ParentPath: "", Index: -1, Origin: OriginTree,
				Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_grafted"},
			})
			if err != nil {
				b.Result = "enqueue failed: " + err.Error()
				return -1
			}
			return 0 // Running → RunTask loops → boundary applies the graft
		}
		return 1
	})
	// Running until ChainState["gate_ch"] (chan struct{}) is closed.
	RegisterAction("muttest_gate", func(ctx *btcore.BTContext[Blackboard]) int {
		ch, _ := ctx.Blackboard.ChainState["gate_ch"].(chan struct{})
		if ch == nil {
			return -1 // misconfigured test
		}
		select {
		case <-ch:
			return 1
		default:
			return 0
		}
	})
}

func muttestMark(b *Blackboard, s string) {
	// Actions run only on the run goroutine in these tests — no lock needed.
	b.Results = append(b.Results, "mark:"+s)
}

func marksOf(b *Blackboard) []string {
	var out []string
	for _, r := range b.Results {
		if strings.HasPrefix(r, "mark:") {
			out = append(out, strings.TrimPrefix(r, "mark:"))
		}
	}
	return out
}

// enqueueWhenLiveThenRelease finds the run by treeID, enqueues op, polls the
// journal until the op lands (applied or rejected), then closes gate. The
// t.Deadline-free 10s cap only guards a hung engine — the happy path never
// waits on wall time.
func enqueueWhenLiveThenRelease(t *testing.T, treeID string, op MutationOp, gate chan struct{}) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(gate)
		deadline := time.Now().Add(10 * time.Second)
		var runID, opID string
		for time.Now().Before(deadline) && opID == "" {
			for _, s := range ListLiveRuns() {
				if s.TreeID == treeID {
					runID = s.RunID
					id, err := EnqueueLiveMutation(runID, op)
					if err != nil {
						t.Errorf("enqueue: %v", err)
						return
					}
					opID = id
				}
			}
		}
		for time.Now().Before(deadline) {
			recs, err := LiveMutationJournal(runID)
			if err != nil {
				return // run ended already; gate close is a no-op then
			}
			for _, r := range recs {
				if r.OpID == opID {
					return // applied or rejected — release the gate
				}
			}
		}
		t.Error("op never appeared in the journal within 10s")
	}()
	return done
}

func newGateBB(task string) (*Blackboard, chan struct{}) {
	gate := make(chan struct{})
	return &Blackboard{Task: task, ChainState: map[string]any{"gate_ch": gate}}, gate
}

func TestRunTaskMutableGraftExecutesSameRun(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "muttest_self_graft"}}}
	bb := &Blackboard{Task: "graft test"}
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "graftcase"}); err != nil {
		t.Fatal(err)
	}
	marks := marksOf(bb)
	if len(marks) == 0 || marks[len(marks)-1] != "grafted" {
		t.Fatalf("grafted action must execute within the same run, marks=%v outcome=%s result=%q", marks, bb.Outcome, bb.Result)
	}
	if _, err := LiveMutationJournal(bb.RunID); err == nil {
		t.Fatal("run must deregister from the live-run registry after completion")
	}
	if len(tree.Children) != 1 {
		t.Fatal("caller-owned tree must stay untouched; run mutates a private clone")
	}
}

func TestRunTaskMutableRemoveSkipsBranch(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "muttest_gate"},   // Running until removal applied
			{Type: "Action", Name: "muttest_mark_b"}, // removed before it can run
			{Type: "Action", Name: "muttest_mark_a"},
		}}
	bb, gate := newGateBB("remove test")
	done := enqueueWhenLiveThenRelease(t, "removecase",
		MutationOp{Kind: "remove", Path: "1", ExpectName: "muttest_mark_b", Origin: OriginOperator}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "removecase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	marks := marksOf(bb)
	for _, m := range marks {
		if m == "b" {
			t.Fatalf("removed branch must not execute, marks=%v", marks)
		}
	}
	if len(marks) == 0 || marks[len(marks)-1] != "a" {
		t.Fatalf("surviving sibling must still execute, marks=%v", marks)
	}
}

func TestRunTaskMutableMemSequenceKeepsPlace(t *testing.T) {
	// Load-bearing migration case: a MemSequence completes side-effectful
	// step a, then blocks in the gate. An unrelated graft lands at ROOT while
	// the cursor sits at the gate. After the rebuild the MemSequence must NOT
	// re-run step a, and the grafted node must run.
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "MemSequence", Name: "memphase", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "muttest_mark_a"},
				{Type: "Action", Name: "muttest_gate"},
			}},
		}}
	bb, gate := newGateBB("memseq migration")
	done := enqueueWhenLiveThenRelease(t, "memcase",
		MutationOp{Kind: "add", ParentPath: "", Index: -1, Origin: OriginOperator,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_mark_b"}}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "memcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	countA, sawB := 0, false
	for _, m := range marksOf(bb) {
		if m == "a" {
			countA++
		}
		if m == "b" {
			sawB = true
		}
	}
	if countA != 1 {
		t.Fatalf("MemSequence must keep its cursor across an unrelated graft; step a ran %d times (marks=%v)", countA, marksOf(bb))
	}
	if !sawB {
		t.Fatalf("grafted root child must execute after the memseq completes, marks=%v", marksOf(bb))
	}
}

func TestRunTaskMutableRejectionKeepsRunHealthy(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "muttest_gate"},
			{Type: "Action", Name: "muttest_mark_a"},
		}}
	bb, gate := newGateBB("reject test")
	// Root removal is always rejected; the goroutine sees the rejected
	// journal record and releases the gate — proving the run survived.
	done := enqueueWhenLiveThenRelease(t, "rejectcase",
		MutationOp{Kind: "remove", Path: "", Origin: OriginOperator}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "rejectcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("rejected op must not fail the run, outcome=%s result=%q", bb.Outcome, bb.Result)
	}
}

func TestRunTaskMutablePersistHookInvoked(t *testing.T) {
	var persisted *evolution.SerializableNode
	var persistedInfo LiveRunInfo
	old := PersistMutatedTreeFn
	PersistMutatedTreeFn = func(info LiveRunInfo, tr *evolution.SerializableNode) error {
		persistedInfo, persisted = info, tr
		return nil
	}
	defer func() { PersistMutatedTreeFn = old }()
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "muttest_gate"}}}
	bb, gate := newGateBB("persist test")
	done := enqueueWhenLiveThenRelease(t, "persistcase",
		MutationOp{Kind: "add", ParentPath: "", Index: -1, Persist: true, Origin: OriginOperator,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_mark_a"}}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "persistcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	if persisted == nil || persistedInfo.TreeID != "persistcase" {
		t.Fatal("persist hook must receive the mutated tree and run info")
	}
	if len(persisted.Children) != 2 {
		t.Fatalf("persisted tree must include the graft, has %d children", len(persisted.Children))
	}
}
```

Notes for the implementer: the persist-hook test swaps a package-level var, so these tests must not run `t.Parallel()`. `muttest_gate` reads `ChainState["gate_ch"]` — placed before the run starts, so there is no concurrent map write. The journal-poll release guarantees the gate's success tick happens strictly after the boundary that applied the op — no sleeps or tick counting anywhere.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestRunTaskMutable' -count=1`
Expected: FAIL to compile — `undefined: RunTaskMutable`, `undefined: PersistMutatedTreeFn`.

- [ ] **Step 3: Implement run_task_mutable.go**

```go
// internal/engine/run_task_mutable.go
// RunTaskMutable: RunTask with a live mutation queue applied at tick
// boundaries (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// PersistMutatedTreeFn persists a mutated live tree so future runs inherit it.
// Nil-checked injection hook wired from cmd/bt-agent (engine must not import
// higher layers). Persist snapshots the ENTIRE current live tree — including
// earlier ephemeral ops from the same run (documented spec semantic).
var PersistMutatedTreeFn func(info LiveRunInfo, tree *evolution.SerializableNode) error

// RunTaskMutable validates, builds, and runs serTree with runtime mutation
// support. It mirrors BuildAndValidate's build semantics, always operating on
// a private clone (resolver-returned trees may be shared package state), and
// registers the run so bb.EnqueueMutation and EnqueueLiveMutation reach it.
func RunTaskMutable(bb *Blackboard, serTree *evolution.SerializableNode, info LiveRunInfo) (string, error) {
	if bb == nil {
		return "", fmt.Errorf("nil blackboard")
	}
	if bb.childTicks == nil {
		bb.childTicks = &childTickLog{}
	}
	expanded, err := prepareTreeForBuild(serTree)
	if err != nil {
		return "", err
	}
	if serTree.TimeoutMs > 0 {
		bb.TreeTimeoutMs = serTree.TimeoutMs
	}
	vinfo := ValidateTreeFull(expanded)
	if !vinfo.Valid() {
		return "", fmt.Errorf("tree validation failed: %v", vinfo.Errors)
	}
	cur := cloneNode(expanded)
	bb.buildCapture = map[*evolution.SerializableNode]btcore.Command[Blackboard]{}
	cmd := buildNode(cur, bb, "")
	lr := registerLiveRun(bb, info)
	lr.cur, lr.capture = cur, bb.buildCapture
	defer deregisterLiveRun(lr.runID)
	_ = RunTask(bb, cmd)
	return bb.Result, nil
}

// applyPending drains and applies queued mutations. Called from RunTask at
// tick boundaries — quiescent points where no node is executing (parallel
// composites join within their tick), so the tree needs no lock. Returns the
// command tree to keep ticking: the old one when nothing applied, the rebuilt
// one otherwise.
func (lr *liveRun) applyPending(ctx *btcore.BTContext[Blackboard], bb *Blackboard, tree btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	ops := lr.drain()
	if len(ops) == 0 {
		return tree
	}
	type appliedShift struct {
		parent *evolution.SerializableNode // node in the FINAL tree coordinates
		kind   string
		index  int
	}
	// corrTotal maps nodes of the CURRENT live tree (lr.cur) to their
	// counterparts in the evolving candidate. Correspondence is ALWAYS
	// computed by mapCorrespondence AFTER an op runs — never captured during
	// a clone — because applyMutationOp's insert can reallocate a parent's
	// children array and move sibling addresses.
	working := cloneNode(lr.cur)
	corrTotal := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(lr.cur, working, nil, "", 0, corrTotal)
	var shifts []appliedShift
	applied := 0
	persistWanted := false
	for _, q := range ops {
		candidate := cloneNode(working)
		parent, at, err := applyMutationOp(candidate, q.op)
		if err == nil {
			err = validateMutatedTree(candidate, q.op)
		}
		if err != nil {
			lr.record(MutationRecord{OpID: q.id, Op: q.op, Status: "rejected",
				Error: err.Error(), Generation: lr.generation, At: time.Now()})
			bb.Log().Warn("tree mutation rejected", "run", lr.runID, "op", q.id, "kind", q.op.Kind, "err", err)
			continue
		}
		// Accept: pair working → candidate around this op's shift, then
		// compose into corrTotal and re-point recorded shifts.
		corrStep := map[*evolution.SerializableNode]*evolution.SerializableNode{}
		mapCorrespondence(working, candidate, parent, q.op.Kind, at, corrStep)
		for old, mid := range corrTotal {
			corrTotal[old] = corrStep[mid] // nil when mid was removed — pairing ends there
		}
		for i := range shifts {
			shifts[i].parent = corrStep[shifts[i].parent]
		}
		working = candidate
		shifts = append(shifts, appliedShift{parent: parent, kind: q.op.Kind, index: at})
		applied++
		persistWanted = persistWanted || q.op.Persist
		lr.record(MutationRecord{OpID: q.id, Op: q.op, Status: "applied",
			Generation: lr.generation + 1, At: time.Now()})
	}
	if applied == 0 {
		return tree
	}
	// Rebuild and migrate pointer-keyed library state old → new for every
	// node of the final tree that has a counterpart in the previous tree.
	oldCapture := lr.capture
	newCapture := map[*evolution.SerializableNode]btcore.Command[Blackboard]{}
	bb.buildCapture = newCapture
	newCmd := buildNode(working, bb, "")
	inv := map[*evolution.SerializableNode]*evolution.SerializableNode{} // new → old
	for old, nw := range corrTotal {
		if nw != nil {
			inv[nw] = old
		}
	}
	var migrate func(n *evolution.SerializableNode)
	migrate = func(n *evolution.SerializableNode) {
		if old := inv[n]; old != nil {
			if oldCmd, ok := oldCapture[old]; ok {
				if newCmdN, ok2 := newCapture[n]; ok2 {
					migrateNodeState(ctx, oldCmd, newCmdN)
				}
			}
		}
		for i := range n.Children {
			migrate(&n.Children[i])
		}
	}
	migrate(working)
	// Cursor arithmetic for directly-mutated MemSequence parents: keep the
	// cursor pointing at the same child despite the index shift.
	for _, s := range shifts {
		if s.parent == nil || s.parent.Type != "MemSequence" {
			continue
		}
		cmdP, ok := newCapture[s.parent]
		if !ok {
			continue
		}
		cursor, ok := ctx.MemSequenceState[cmdP]
		if !ok {
			continue
		}
		switch {
		case s.kind == "add" && s.index <= cursor:
			ctx.MemSequenceState[cmdP] = cursor + 1
		case s.kind == "remove" && s.index < cursor:
			ctx.MemSequenceState[cmdP] = cursor - 1
		}
	}
	lr.mu.Lock()
	lr.generation++
	gen := lr.generation
	lr.mu.Unlock()
	lr.cur, lr.capture = working, newCapture
	bb.Log().Info("tree mutated at tick boundary", "run", lr.runID, "applied", applied, "generation", gen)
	if bb.EventBus != nil {
		bb.EventBus.Publish("tree_mutated", EventMessage{
			Source: "engine.applyPending", Timestamp: time.Now(), Type: "tree_mutated",
			Data: map[string]any{"run_id": lr.runID, "applied": applied, "generation": gen},
		})
	}
	_ = audit.Append(audit.Entry{
		Timestamp: time.Now(), Agent: lr.info.Agent, Action: "tree_mutation",
		Detail: fmt.Sprintf("applied %d op(s), generation %d, tree %s", applied, gen, lr.info.TreeID),
	})
	if persistWanted {
		if PersistMutatedTreeFn == nil {
			bb.Log().Warn("tree mutation persist requested but no persistence hook wired", "run", lr.runID)
		} else if err := PersistMutatedTreeFn(lr.info, cloneNode(working)); err != nil {
			// Persist failure is journaled, never fails the mutation or run.
			lr.record(MutationRecord{OpID: lr.runID + "-persist", Status: "rejected",
				Error: "persist: " + err.Error(), Generation: gen, At: time.Now()})
			bb.Log().Warn("tree mutation persist failed", "run", lr.runID, "err", err)
		}
	}
	return newCmd
}

// migrateNodeState moves every go-bt per-node state entry keyed by the old
// command pointer to the new one. MemSequence cursors, Repeat/Retry counters,
// and Timeout/Sleep stamps survive rebuilds for unchanged nodes.
func migrateNodeState(ctx *btcore.BTContext[Blackboard], oldCmd, newCmd btcore.Command[Blackboard]) {
	if oldCmd == nil || newCmd == nil || oldCmd == newCmd {
		return
	}
	if v, ok := ctx.MemSequenceState[oldCmd]; ok {
		ctx.MemSequenceState[newCmd] = v
		delete(ctx.MemSequenceState, oldCmd)
	}
	if v, ok := ctx.RepeaterState[oldCmd]; ok {
		ctx.RepeaterState[newCmd] = v
		delete(ctx.RepeaterState, oldCmd)
	}
	if v, ok := ctx.RetryState[oldCmd]; ok {
		ctx.RetryState[newCmd] = v
		delete(ctx.RetryState, oldCmd)
	}
	if v, ok := ctx.RetryTimeState[oldCmd]; ok {
		ctx.RetryTimeState[newCmd] = v
		delete(ctx.RetryTimeState, oldCmd)
	}
	if v, ok := ctx.TimeoutState[oldCmd]; ok {
		ctx.TimeoutState[newCmd] = v
		delete(ctx.TimeoutState, oldCmd)
	}
	if v, ok := ctx.SleepState[oldCmd]; ok {
		ctx.SleepState[newCmd] = v
		delete(ctx.SleepState, oldCmd)
	}
}
```

Modify `RunTask` in `internal/engine/tree.go`: replace the first-run call and the multi-tick loop (currently ~lines 575–586) with the hook-aware version. The ONLY changes are the two `applyPending` lines — everything else stays byte-identical:

```go
	if bb.liveRun != nil {
		tree = bb.liveRun.applyPending(btCtx, bb, tree)
	}
	code := tree.Run(btCtx)

	// Multi-tick loop: Repeat and other decorators return 0 (Running) between
	// ticks. Keep ticking until a terminal status is reached. A HITL gate
	// awaiting an external human (bb.Outcome == "pending_approval") is also
	// Running, but nothing inside this synchronous loop can change that
	// status — re-ticking would just burn maxTicks iterations for no effect —
	// so stop immediately and let RunTask return that outcome to the caller.
	// Mutable runs (bb.liveRun set) apply queued tree mutations at each tick
	// boundary — a quiescent point — and keep ticking the rebuilt tree.
	const maxTicks = 1000
	for tick := 1; code == 0 && bb.Outcome != "pending_approval" && tick < maxTicks; tick++ {
		if bb.liveRun != nil {
			tree = bb.liveRun.applyPending(btCtx, bb, tree)
		}
		code = tree.Run(btCtx)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestRunTaskMutable' -count=1 -race`
Expected: PASS (5 test functions). The `-race` flag is mandatory here — the enqueue goroutines exercise the cross-goroutine queue/journal path.

Then the full engine short suite:
Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -short -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/nico/go-bt-evolve && git add internal/engine/run_task_mutable.go internal/engine/run_task_mutable_test.go internal/engine/tree.go && git commit -m "feat(engine): tick-boundary mutation apply with state migration and RunTaskMutable"
```

---

### Task 4: Runner switch, persistence wiring, MCP tools

**Files:**
- Create: `internal/agent/mutated_trees.go`, `internal/agent/mutated_trees_test.go`
- Modify: `internal/agent/runner.go` (~lines 198–218), `cmd/bt-agent/main.go` (resolveTree ~line 33; deps wiring ~line 488), `cmd/bt-agent/tools.go` (append three RegisterTool calls in the function that registers the other `bt_*` tools)

**Interfaces:**
- Consumes: Task 3 (`engine.RunTaskMutable`, `engine.LiveRunInfo`, `engine.PersistMutatedTreeFn`), Task 2 (`engine.ListLiveRuns`, `engine.EnqueueLiveMutation`, `engine.LiveMutationJournal`), Task 1 (`engine.MutationOp`, `engine.OriginOperator`), `evolution.TreeStore.SaveTo` (atomic tmp+rename).
- Produces:
  - `agent.SaveMutatedTree(treeID string, tree *evolution.SerializableNode) error`
  - `agent.LoadMutatedTreeOverride(treeID string) *evolution.SerializableNode` (nil when no override)
  - MCP tools `bt_live_runs`, `bt_live_mutate`, `bt_live_mutations`

- [ ] **Step 1: Write the failing tests for the override store**

```go
// internal/agent/mutated_trees_test.go
package agent

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestMutatedTreeOverrideRoundTrip(t *testing.T) {
	t.Setenv("BT_MUTATED_TREES_DIR", t.TempDir())
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "x"}}}
	if err := SaveMutatedTree("goal:automate_demo", tree); err != nil {
		t.Fatal(err)
	}
	got := LoadMutatedTreeOverride("goal:automate_demo")
	if got == nil || got.Name != "root" || len(got.Children) != 1 || got.Children[0].Name != "x" {
		t.Fatalf("override round-trip failed: %+v", got)
	}
	if LoadMutatedTreeOverride("no_such_tree") != nil {
		t.Fatal("missing override must return nil")
	}
}

func TestMutatedTreeFilenameSanitized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_MUTATED_TREES_DIR", dir)
	if err := SaveMutatedTree("../../etc/passwd", &evolution.SerializableNode{Type: "Sequence", Name: "r",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if LoadMutatedTreeOverride("../../etc/passwd") == nil {
		t.Fatal("sanitized ID must still round-trip")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/agent/ -run 'TestMutatedTree' -count=1`
Expected: FAIL to compile — `undefined: SaveMutatedTree`.

- [ ] **Step 3: Implement the override store**

```go
// internal/agent/mutated_trees.go
// Persisted runtime tree mutations (ADR-003): the full mutated tree is
// snapshotted per tree ID under ~/.go-bt-evolve/mutated_trees/ and consulted
// override-first by cmd/bt-agent's tree resolution, so persisted runtime
// mutations survive restarts even for code-defined domain trees.
package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func mutatedTreesDir() string {
	if d := os.Getenv("BT_MUTATED_TREES_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "mutated_trees")
}

// sanitizeTreeID makes a tree ID filesystem-safe (IDs contain ':' and '/').
func sanitizeTreeID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
}

// SaveMutatedTree atomically snapshots tree as the persisted override for
// treeID. Wired into engine.PersistMutatedTreeFn from cmd/bt-agent.
func SaveMutatedTree(treeID string, tree *evolution.SerializableNode) error {
	dir := mutatedTreesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ts, err := evolution.NewTreeStore(dir)
	if err != nil {
		return err
	}
	return ts.SaveTo(tree, filepath.Join(dir, sanitizeTreeID(treeID)+".json"))
}

// LoadMutatedTreeOverride returns the persisted override for treeID, or nil
// when none exists (callers fall through to normal resolution).
func LoadMutatedTreeOverride(treeID string) *evolution.SerializableNode {
	dir := mutatedTreesDir()
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, sanitizeTreeID(treeID)+".json"))
	if err != nil {
		return nil
	}
	var tree evolution.SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil
	}
	return &tree
}
```

(Add `"encoding/json"` to the imports.)

- [ ] **Step 4: Run override-store tests**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go test ./internal/agent/ -run 'TestMutatedTree' -count=1 -race`
Expected: PASS (2 test functions)

- [ ] **Step 5: Switch the runner to RunTaskMutable**

In `internal/agent/runner.go`, replace lines 198–218 (the `BuildAndValidate` call through `_ = engine.RunTask(bb, bt)`). The tracing span moves BEFORE the call because RunTaskMutable builds and runs in one step; a build/validation error now ends the span before returning, preserving today's failure shape:

```go
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	spanCtx, runSpan := tracing.StartSpan(runCtx, "agent.run/"+agentName)
	runSpan.SetAttribute("run_id", result.RunID)
	runSpan.SetAttribute("agent", agentName)
	runSpan.SetAttribute("tree", result.TreeID)
	runSpanCtx := runSpan.SpanContext()
	result.TraceID = runSpanCtx.TraceID
	result.SpanID = runSpanCtx.SpanID
	bb.TraceContext = spanCtx
	if _, err := engine.RunTaskMutable(bb, tree, engine.LiveRunInfo{Agent: agentName, TreeID: result.TreeID}); err != nil {
		runSpan.RecordError(err)
		runSpan.End()
		result.Outcome = "failure"
		result.Output = err.Error()
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(start)
		return result, err
	}
	runSpan.SetAttribute("outcome", bb.Outcome)
	if bb.Outcome != "success" && bb.Outcome != "" {
		runSpan.RecordError(fmt.Errorf("agent outcome: %s", bb.Outcome))
	}
	runSpan.End()
```

Note: `RunTaskMutable` registers the run under `bb.RunID` (generating one only if empty — the runner set it at line 185 when blackboard management is on), so MCP callers can target scheduler-driven runs.

- [ ] **Step 6: Wire persistence + override-first resolution in cmd/bt-agent/main.go**

Change `resolveTree` (line 33) to consult the override store first:

```go
func resolveTree(id string) *evolution.SerializableNode {
	if t := agent.LoadMutatedTreeOverride(id); t != nil {
		return t
	}
	return domains.ResolveTreeID(id)
}
```

(User-scoped resolution `resolveTreeForUser` intentionally skips overrides — persisted mutations are unscoped for now, matching the spec's out-of-scope list.)

Next to the `RunDeps` construction (~line 488), wire the hook:

```go
	engine.PersistMutatedTreeFn = func(info engine.LiveRunInfo, tree *evolution.SerializableNode) error {
		return agent.SaveMutatedTree(info.TreeID, tree)
	}
```

(Add imports for `engine` if not already present in main.go — check the import block.)

- [ ] **Step 7: Register the three MCP tools in cmd/bt-agent/tools.go**

Append inside the function that registers the existing `bt_*` tools (same one containing `bt_get_tree` at line ~479), mirroring its result-building style — if the file has a text-result helper, use it; otherwise construct `engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: s}}}` directly:

```go
	server.RegisterTool("bt_live_runs", "List live mutable behavior-tree runs (run_id, agent, tree, generation, pending ops)",
		map[string]engine.Property{}, nil,
		func(args json.RawMessage) *engine.ToolResult {
			data, _ := json.MarshalIndent(engine.ListLiveRuns(), "", "  ")
			return textToolResult(string(data))
		})

	server.RegisterTool("bt_live_mutate", "Enqueue an add/remove node mutation against a live run's behavior tree; applied at the next tick boundary. Returns an op ID (fire-and-forget — poll bt_live_mutations for the result).",
		map[string]engine.Property{
			"run_id":      {Type: "string", Description: "target run (from bt_live_runs)"},
			"kind":        {Type: "string", Description: "add | remove"},
			"parent_path": {Type: "string", Description: "add: index path of parent ('' = root, '0.2' = second grandchild)"},
			"index":       {Type: "number", Description: "add: insertion position, -1 appends"},
			"path":        {Type: "string", Description: "remove: index path of the node to remove"},
			"expect_name": {Type: "string", Description: "optional: resolved node's Name must match"},
			"subtree":     {Type: "string", Description: "add: SerializableNode JSON to graft"},
			"persist":     {Type: "boolean", Description: "snapshot the mutated tree so future runs inherit it"},
		}, []string{"run_id", "kind"},
		func(args json.RawMessage) *engine.ToolResult {
			var in struct {
				RunID      string  `json:"run_id"`
				Kind       string  `json:"kind"`
				ParentPath string  `json:"parent_path"`
				Index      *int    `json:"index"`
				Path       string  `json:"path"`
				ExpectName string  `json:"expect_name"`
				Subtree    string  `json:"subtree"`
				Persist    bool    `json:"persist"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return textToolResult("error: " + err.Error())
			}
			op := engine.MutationOp{
				Kind: in.Kind, ParentPath: in.ParentPath, Index: -1,
				Path: in.Path, ExpectName: in.ExpectName, Persist: in.Persist,
				Origin: engine.OriginOperator, // MCP entry point always stamps operator
			}
			if in.Index != nil {
				op.Index = *in.Index
			}
			if in.Subtree != "" {
				var sub evolution.SerializableNode
				if err := json.Unmarshal([]byte(in.Subtree), &sub); err != nil {
					return textToolResult("error: bad subtree JSON: " + err.Error())
				}
				op.Subtree = &sub
			}
			id, err := engine.EnqueueLiveMutation(in.RunID, op)
			if err != nil {
				return textToolResult("error: " + err.Error())
			}
			return textToolResult(fmt.Sprintf("queued op %s (applies at next tick boundary)", id))
		})

	server.RegisterTool("bt_live_mutations", "Mutation journal for a live run: applied/rejected ops with errors and generations",
		map[string]engine.Property{
			"run_id": {Type: "string", Description: "target run"},
		}, []string{"run_id"},
		func(args json.RawMessage) *engine.ToolResult {
			var in struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return textToolResult("error: " + err.Error())
			}
			recs, err := engine.LiveMutationJournal(in.RunID)
			if err != nil {
				return textToolResult("error: " + err.Error())
			}
			data, _ := json.MarshalIndent(recs, "", "  ")
			return textToolResult(string(data))
		})
```

If `tools.go` has no `textToolResult` helper, add one near the top of the file (match the file's existing style first — an equivalent helper may exist under another name; use that instead if so):

```go
func textToolResult(s string) *engine.ToolResult {
	return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: s}}}
}
```

- [ ] **Step 8: Build and run the touched packages' tests**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH go build ./... && PATH=/usr/local/go/bin:$PATH go test ./internal/agent/ ./internal/engine/ -short -count=1 -race`
Expected: builds clean, tests PASS. If `ContentItem` has no `Text` field, read `internal/engine/mcp_server.go` for the actual field name and adjust.

- [ ] **Step 9: Commit**

```bash
cd /home/nico/go-bt-evolve && git add internal/agent/mutated_trees.go internal/agent/mutated_trees_test.go internal/agent/runner.go cmd/bt-agent/main.go cmd/bt-agent/tools.go && git commit -m "feat: runner mutable runs, persisted-override store, live-mutation MCP tools"
```

---

### Task 5: Full gate, changelog, docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top, matching the file's existing entry format)

- [ ] **Step 1: Run the pre-commit-equivalent gate**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH make check-quick`
Expected: gofmt, vet, golangci-lint, mod tidy, doc drift, ci-doctor, short tests all green. Known flake exemption: `TestCMAESOptimizer_Convergence` may fail stochastically — retry it once in isolation ONLY if it is the sole failure.

- [ ] **Step 2: Run the full engine+agent race suite**

Run: `cd /home/nico/go-bt-evolve && PATH=/usr/local/go/bin:$PATH make test`
Expected: PASS.

- [ ] **Step 3: Add CHANGELOG entry**

Open `CHANGELOG.md`, match the existing entry style, and add at the top:

```markdown
## 2026-07-17

- Runtime tree mutation: behavior trees can now be mutated (nodes added or
  removed) while they are being ticked. Mutations queue per live run (from
  action nodes via `bb.EnqueueMutation`, or externally via the new
  `bt_live_runs` / `bt_live_mutate` / `bt_live_mutations` MCP tools) and apply
  copy-on-write at tick boundaries with validation, llm-origin allowlisting,
  and pointer-state migration (an unrelated graft never restarts an
  in-progress MemSequence). `persist: true` snapshots the mutated tree to
  `~/.go-bt-evolve/mutated_trees/` where tree resolution consults it
  override-first. Spec: `docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md`.
```

- [ ] **Step 4: Commit**

```bash
cd /home/nico/go-bt-evolve && git add CHANGELOG.md && git commit -m "docs: changelog for runtime tree mutation"
```

---

## Plan Self-Review Notes (already applied)

- Spec coverage: ops/addressing (Task 1), queue/registry/journal/cap/entry points (Task 2), tick-boundary apply + migration + cursor arithmetic + eventbus/audit/logging + persist hook + defensive paths (Task 3), runner switch + ADR-003 persistence + override-first resolution + MCP tools (Task 4), gates (Task 5). Out-of-scope items from the spec remain out.
- The spec's "leaf-parent add rejection" is implemented as the generalized `maxChildrenForType` guard (Global Constraints, delta 1).
- The spec's `RunTaskMutable` exists; its tick loop is RunTask's own with two nil-checked hook lines (Global Constraints, delta 2) — no duplicated terminal-outcome semantics.
- Type consistency: `MutationOp`/`LiveRunInfo`/`MutationRecord`/`LiveRunStatus` field names match across Tasks 1–4; `applyPending(ctx, bb, tree)` signature consistent between Task 3 code and RunTask edit.
- Correctness fix applied during review: correspondence is computed by `mapCorrespondence` AFTER each op (dual walk with shift arithmetic), never captured during the clone — `applyMutationOp`'s slice insert can reallocate the parent's children backing array and move sibling addresses, which would silently orphan migrated state. `TestMapCorrespondenceAfterAddSurvivesRealloc` guards the regression.
- Determinism fix applied during review: the external-mutation tests gate on a channel released only after the enqueue goroutine SEES the op in the journal — no sleeps, no tick counting, no scheduling races under `-race` or slow CI.
