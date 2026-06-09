# BT Platform Gap Closure — Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Close the 5 critical gaps identified by NotebookLM research: scaling complexity, intent drift, GOAP contexts, formal SLOs, decentralized coordination.

**Architecture:** Add `PlannerNode` (GOBT A* planning) and `CheckpointVerifier` decorator to the engine; parameterized GOAP actions; SLO telemetry node; gardener validation gate. All built on existing `Blackboard`, `BuildTree()`, and evolution pipeline.

**Tech Stack:** Go 1.21+, go-bt library, existing engine/evolution/gardener modules.

**Research Basis:** NotebookLM "BT Platform Research" notebook (93 sources, conversation 9c3acc28-3a2d-4ff1-a000-1cc28e1acd32).

---

## Sprint 1: P1 — Scaling Complexity + Intent Drift (2 critical gaps)

### Gap 1: Scaling Complexity — PlannerNode (GOBT)

**Objective:** Replace static selector-based planning with dynamic A* GOAP planning via a `PlannerNode` that searches action space at runtime.

**Files:**
- Create: `internal/engine/planner_node.go`
- Create: `internal/engine/planner_node_test.go`
- Modify: `internal/engine/registry.go` (register PlannerNode)
- Modify: `internal/engine/tree.go` (BuildTree support)

---

### Task 1.1: Define PlannerNode struct and A* search

**Objective:** Core PlannerNode type that performs backward A* search through GOAP actions given a goal and world state.

**Step 1: Create `internal/engine/planner_node.go`**

```go
package engine

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"
)

// GOAPAction represents a single action in the GOAP planning domain.
type GOAPAction struct {
	Name         string
	Cost         float64
	Preconditions map[string]bool  // what must be true to execute
	Effects      map[string]bool   // what changes after execution
	ActionFunc   string            // registered engine action name
}

// GOAPGoal represents a desired world state.
type GOAPGoal struct {
	Name       string
	Priority   float64
	Conditions map[string]bool // desired world state
}

// PlannerNode implements Goal-Oriented Behavior Tree (GOBT) planning.
// Instead of a static selector subtree, it runs A* search at runtime
// to find the optimal action sequence from current world state to goal.
type PlannerNode struct {
	Goal      GOAPGoal
	Actions   []GOAPAction
	MaxDepth  int           // max plan depth (default 5)
	Mode      string        // "greedy" or "search" (A*)
}

// Plan represents a computed action sequence.
type Plan struct {
	Actions  []string
	Cost     float64
	Depth    int
	Complete bool
}

// plannerState is an A* search state.
type plannerState struct {
	WorldState map[string]bool
	Actions    []string
	Cost       float64
	Depth      int
	Heuristic  float64
	index      int // for heap
}

type plannerHeap []*plannerState

func (h plannerHeap) Len() int           { return len(h) }
func (h plannerHeap) Less(i, j int) bool { return h[i].Cost+h[i].Heuristic < h[j].Cost+h[j].Heuristic }
func (h plannerHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *plannerHeap) Push(x any)        { n := len(*h); item := x.(*plannerState); item.index = n; *h = append(*h, item) }
func (h *plannerHeap) Pop() any          { old := *h; n := len(old); item := old[n-1]; old[n-1] = nil; item.index = -1; *h = old[:n-1]; return item }

// Plan searches for an action sequence from the current world state to the goal.
// In "greedy" mode, it picks the cheapest satisfiable action at each step.
// In "search" mode, it uses A* with heuristic = unsatisfied goal condition count.
func (p *PlannerNode) Plan(worldState map[string]bool) Plan {
	if p.MaxDepth == 0 {
		p.MaxDepth = 5
	}
	if p.Mode == "" {
		p.Mode = "search"
	}

	if p.Mode == "greedy" {
		return p.greedyPlan(worldState)
	}
	return p.aStarPlan(worldState)
}

func (p *PlannerNode) goalSatisfied(state map[string]bool) bool {
	for k, v := range p.Goal.Conditions {
		if state[k] != v {
			return false
		}
	}
	return true
}

func (p *PlannerNode) applicableActions(state map[string]bool) []GOAPAction {
	var applicable []GOAPAction
	for _, a := range p.Actions {
		ok := true
		for k, v := range a.Preconditions {
			if state[k] != v {
				ok = false
				break
			}
		}
		if ok {
			applicable = append(applicable, a)
		}
	}
	sort.Slice(applicable, func(i, j int) bool { return applicable[i].Cost < applicable[j].Cost })
	return applicable
}

func (p *PlannerNode) applyAction(state map[string]bool, action GOAPAction) map[string]bool {
	newState := make(map[string]bool, len(state)+len(action.Effects))
	for k, v := range state {
		newState[k] = v
	}
	for k, v := range action.Effects {
		newState[k] = v
	}
	return newState
}

func (p *PlannerNode) heuristic(state map[string]bool) float64 {
	unsatisfied := 0.0
	for k, v := range p.Goal.Conditions {
		if state[k] != v {
			unsatisfied++
		}
	}
	return unsatisfied
}

func (p *PlannerNode) greedyPlan(worldState map[string]bool) Plan {
	state := p.copyState(worldState)
	var actions []string
	cost := 0.0

	for depth := 0; depth < p.MaxDepth; depth++ {
		if p.goalSatisfied(state) {
			return Plan{Actions: actions, Cost: cost, Depth: depth, Complete: true}
		}
		applicable := p.applicableActions(state)
		if len(applicable) == 0 {
			break
		}
		best := applicable[0]
		state = p.applyAction(state, best)
		actions = append(actions, best.ActionFunc)
		cost += best.Cost
	}

	return Plan{Actions: actions, Cost: cost, Depth: len(actions), Complete: p.goalSatisfied(state)}
}

func (p *PlannerNode) aStarPlan(worldState map[string]bool) Plan {
	h := &plannerHeap{}
	heap.Init(h)

	start := &plannerState{
		WorldState: p.copyState(worldState),
		Cost:       0,
		Depth:      0,
		Heuristic:  p.heuristic(worldState),
	}
	heap.Push(h, start)

	visited := make(map[string]bool)
	for h.Len() > 0 {
		current := heap.Pop(h).(*plannerState)

		stateKey := p.stateKey(current.WorldState)
		if visited[stateKey] {
			continue
		}
		visited[stateKey] = true

		if p.goalSatisfied(current.WorldState) {
			return Plan{Actions: current.Actions, Cost: current.Cost, Depth: current.Depth, Complete: true}
		}
		if current.Depth >= p.MaxDepth {
			continue
		}

		for _, action := range p.applicableActions(current.WorldState) {
			newState := p.applyAction(current.WorldState, action)
			newActions := make([]string, len(current.Actions)+1)
			copy(newActions, current.Actions)
			newActions[len(current.Actions)] = action.ActionFunc

			next := &plannerState{
				WorldState: newState,
				Actions:    newActions,
				Cost:       current.Cost + action.Cost,
				Depth:      current.Depth + 1,
				Heuristic:  p.heuristic(newState),
			}
			heap.Push(h, next)
		}
	}

	return Plan{Complete: false}
}

func (p *PlannerNode) copyState(state map[string]bool) map[string]bool {
	out := make(map[string]bool, len(state))
	for k, v := range state {
		out[k] = v
	}
	return out
}

func (p *PlannerNode) stateKey(state map[string]bool) string {
	keys := make([]string, 0, len(state))
	for k, v := range state {
		keys = append(keys, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(keys)
	return strings.Join(keys, "|")
}
```

**Step 2: Verify compilation**

```bash
cd /home/nico/go-bt-evolve && go build ./internal/engine/
```

Expected: PASS (no errors)

---

### Task 1.2: Write tests for PlannerNode

**Objective:** Unit tests covering greedy mode, A* mode, max depth, and unsatisfiable goals.

**Step 1: Create `internal/engine/planner_node_test.go`**

```go
package engine

import (
	"testing"
)

func TestPlannerNode_Greedy_DirectPath(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name: "door_open",
			Conditions: map[string]bool{"door_open": true},
		},
		Actions: []GOAPAction{
			{
				Name: "open_door", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{"door_open": true},
				ActionFunc:    "open_door",
			},
		},
		MaxDepth: 3,
		Mode:     "greedy",
	}

	plan := p.Plan(map[string]bool{"door_open": false})
	if !plan.Complete {
		t.Errorf("expected complete plan, got incomplete")
	}
	if len(plan.Actions) != 1 || plan.Actions[0] != "open_door" {
		t.Errorf("expected [open_door], got %v", plan.Actions)
	}
}

func TestPlannerNode_AStar_MultiStep(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name: "room_entered",
			Conditions: map[string]bool{"in_room": true},
		},
		Actions: []GOAPAction{
			{
				Name: "unlock_door", Cost: 1.0,
				Preconditions: map[string]bool{"has_key": true},
				Effects:       map[string]bool{"door_unlocked": true},
				ActionFunc:    "unlock_door",
			},
			{
				Name: "pickup_key", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{"has_key": true},
				ActionFunc:    "pickup_key",
			},
			{
				Name: "enter_room", Cost: 1.0,
				Preconditions: map[string]bool{"door_unlocked": true},
				Effects:       map[string]bool{"in_room": true},
				ActionFunc:    "enter_room",
			},
		},
		MaxDepth: 5,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if !plan.Complete {
		t.Errorf("expected complete plan, got incomplete")
	}
	if len(plan.Actions) != 3 {
		t.Errorf("expected 3 actions, got %d: %v", len(plan.Actions), plan.Actions)
	}
}

func TestPlannerNode_MaxDepth_Exceeded(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name: "impossible",
			Conditions: map[string]bool{"done": true},
		},
		Actions: []GOAPAction{
			{
				Name: "do_nothing", Cost: 1.0,
				Preconditions: map[string]bool{},
				Effects:       map[string]bool{},
				ActionFunc:    "do_nothing",
			},
		},
		MaxDepth: 2,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if plan.Complete {
		t.Errorf("expected incomplete plan (goal unreachable), got complete")
	}
	if plan.Depth > 2 {
		t.Errorf("expected max depth 2, got %d", plan.Depth)
	}
}

func TestPlannerNode_Unsatisfiable_NoActions(t *testing.T) {
	p := &PlannerNode{
		Goal: GOAPGoal{
			Name: "impossible",
			Conditions: map[string]bool{"magic": true},
		},
		Actions:  []GOAPAction{},
		MaxDepth: 5,
		Mode:     "search",
	}

	plan := p.Plan(map[string]bool{})
	if plan.Complete {
		t.Errorf("expected incomplete plan")
	}
}
```

**Step 2: Run tests**

```bash
cd /home/nico/go-bt-evolve && go test ./internal/engine/ -run TestPlannerNode -v
```

Expected: 4 PASS

**Step 3: Commit**

```bash
git add internal/engine/planner_node.go internal/engine/planner_node_test.go
git commit -m "feat: add PlannerNode with greedy and A* GOAP planning"
```

---

### Task 1.3: Register PlannerNode in BuildTree and action registry

**Objective:** Wire PlannerNode into the tree builder so it can be used in domain trees.

**Step 1: Add PlannerNode support to `internal/engine/tree.go` BuildTree**

After the existing node type switch, add:

```go
case "PlannerNode":
    // Build a PlannerNode from the SerializableNode's GOAP configuration.
    planner := &PlannerNode{
        Goal: GOAPGoal{
            Name:       node.Properties["goal_name"],
            Conditions: parseBoolMap(node.Properties["goal_conditions"]),
        },
        MaxDepth: intProp(node, "max_depth", 5),
        Mode:     strProp(node, "mode", "search"),
    }
    // Parse actions from children
    for _, child := range node.Children {
        if child.Type == "GOAPAction" {
            planner.Actions = append(planner.Actions, GOAPAction{
                Name:          child.Properties["name"],
                Cost:          floatProp(child, "cost", 1.0),
                Preconditions: parseBoolMap(child.Properties["preconditions"]),
                Effects:       parseBoolMap(child.Properties["effects"]),
                ActionFunc:    child.Properties["action"],
            })
        }
    }
    // The PlannerNode doesn't execute directly — it computes a plan
    // and injects it into the Blackboard. The subtree then reads the plan
    // and executes each action sequentially.
    return btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
        // Extract world state from Blackboard
        ws := extractWorldState(bb)
        plan := planner.Plan(ws)
        bb.Set("plan", plan.Actions)
        bb.Set("plan_cost", fmt.Sprintf("%.2f", plan.Cost))
        bb.Set("plan_complete", fmt.Sprintf("%v", plan.Complete))
        if plan.Complete {
            return btcore.Success
        }
        return btcore.Failure
    }), nil
```

**Step 2: Add helper functions for property parsing**

Add to `internal/engine/tree.go`:

```go
func parseBoolMap(raw string) map[string]bool {
    m := make(map[string]bool)
    if raw == "" {
        return m
    }
    pairs := strings.Split(raw, ",")
    for _, pair := range pairs {
        kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
        if len(kv) == 2 {
            m[kv[0]] = strings.TrimSpace(kv[1]) == "true"
        }
    }
    return m
}

func extractWorldState(bb *btcore.Blackboard) map[string]bool {
    state := make(map[string]bool)
    // Read all world_state_* keys from blackboard
    for _, key := range bb.Keys() {
        if strings.HasPrefix(key, "world_state_") {
            factName := strings.TrimPrefix(key, "world_state_")
            val, _ := bb.Get(key)
            state[factName] = fmt.Sprintf("%v", val) == "true"
        }
    }
    return state
}
```

**Step 3: Add to node type registry in `internal/engine/registry.go`**

Add `"PlannerNode"` and `"GOAPAction"` to the known node types.

**Step 4: Run full engine tests to verify no regressions**

```bash
cd /home/nico/go-bt-evolve && go test ./internal/engine/... -count=1
```

Expected: All existing tests pass + 4 new PlannerNode tests pass.

**Step 5: Commit**

```bash
git add internal/engine/tree.go internal/engine/registry.go
git commit -m "feat: wire PlannerNode into BuildTree with GOAP world state extraction"
```

---

### Gap 2: Long-Horizon Intent Drift — CheckpointVerifier

**Objective:** Add a decorator node that validates world state against expected postconditions after every N actions, rolling back and retrying on mismatch.

**Files:**
- Create: `internal/engine/checkpoint_verifier.go`
- Create: `internal/engine/checkpoint_verifier_test.go`
- Modify: `internal/engine/registry.go`
- Modify: `internal/evolution/node_types.go` (add CheckpointVerifier to KnownNodeTypes)

---

### Task 2.1: Implement CheckpointVerifier decorator

**Objective:** Decorator that snapshots world state, runs child subtree, verifies expected postconditions, retries on mismatch.

**Step 1: Create `internal/engine/checkpoint_verifier.go`**

```go
package engine

import (
	"fmt"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

// CheckpointVerifier is a decorator node that validates world state against
// expected postconditions after child execution. If postconditions aren't met,
// it restores the pre-execution snapshot and retries (up to MaxRetries).
type CheckpointVerifier struct {
	MaxRetries     int
	Postconditions map[string]bool // expected world state after child success
	CheckInterval  int             // verify every N actions (0 = only at end)
}

// snapshotState copies the current world state from the blackboard.
func snapshotState(bb *btcore.Blackboard) map[string]bool {
	return extractWorldState(bb)
}

// restoreState writes a snapshot back to the blackboard.
func restoreState(bb *btcore.Blackboard, snap map[string]bool) {
	for k, v := range snap {
		bb.Set("world_state_"+k, fmt.Sprintf("%v", v))
	}
}

// verifyPostconditions checks if the current world state satisfies all postconditions.
func (c *CheckpointVerifier) verifyPostconditions(state map[string]bool) bool {
	for k, expected := range c.Postconditions {
		if state[k] != expected {
			return false
		}
	}
	return true
}

// Tick executes the child subtree with checkpoint verification.
func (c *CheckpointVerifier) Tick(bb *btcore.Blackboard, child btcore.Node) btcore.Status {
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}

	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		snapshot := snapshotState(bb)

		status := child.Tick(bb)

		if status == btcore.Failure {
			// Child failed — restore and retry
			restoreState(bb, snapshot)
			if attempt < c.MaxRetries {
				bb.Set("checkpoint_retry_reason", fmt.Sprintf("child_failure_attempt_%d", attempt+1))
				continue
			}
			return btcore.Failure
		}

		// Child succeeded — verify postconditions
		currentState := extractWorldState(bb)
		if c.verifyPostconditions(currentState) {
			return btcore.Success
		}

		// Postconditions not met — restore and retry
		restoreState(bb, snapshot)
		if attempt < c.MaxRetries {
			bb.Set("checkpoint_retry_reason", fmt.Sprintf("postcondition_mismatch_attempt_%d", attempt+1))
			continue
		}
	}

	return btcore.Failure
}

// CheckpointVerifierFactory creates a CheckpointVerifier from a SerializableNode.
func CheckpointVerifierFactory(node SerializableNode) (btcore.Node, error) {
	cv := &CheckpointVerifier{
		MaxRetries:    intProp(node, "max_retries", 3),
		CheckInterval: intProp(node, "check_interval", 0),
	}

	raw := strProp(node, "postconditions", "")
	if raw != "" {
		cv.Postconditions = parseBoolMap(raw)
	}

	return btcore.NewDecorator(func(bb *btcore.Blackboard, child btcore.Node) btcore.Status {
		// Record preconditions for telemetry
		preSnap := snapshotState(bb)
		preStr := make([]string, 0, len(preSnap))
		for k, v := range preSnap {
			preStr = append(preStr, fmt.Sprintf("%s=%v", k, v))
		}
		bb.Set("checkpoint_pre_state", strings.Join(preStr, ","))

		result := cv.Tick(bb, child)

		// Record post-state for telemetry
		postSnap := snapshotState(bb)
		postStr := make([]string, 0, len(postSnap))
		for k, v := range postSnap {
			postStr = append(postStr, fmt.Sprintf("%s=%v", k, v))
		}
		bb.Set("checkpoint_post_state", strings.Join(postStr, ","))

		return result
	}), nil
}
```

**Step 2: Verify compilation**

```bash
cd /home/nico/go-bt-evolve && go build ./internal/engine/
```

**Step 3: Commit**

```bash
git add internal/engine/checkpoint_verifier.go
git commit -m "feat: add CheckpointVerifier decorator for long-horizon intent drift prevention"
```

---

### Task 2.2: Write tests for CheckpointVerifier

**Objective:** Test snapshot/restore, postcondition verification, and max retry exhaustion.

**Step 1: Create `internal/engine/checkpoint_verifier_test.go`**

```go
package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
	btcomp "github.com/rvitorper/go-bt/composite"
)

func TestCheckpointVerifier_Success_FirstTry(t *testing.T) {
	bb := btcore.NewBlackboard()
	bb.Set("world_state_door_open", "false")

	// Child action: opens door
	openDoor := btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
		bb.Set("world_state_door_open", "true")
		return btcore.Success
	})

	cv := &CheckpointVerifier{
		MaxRetries:     3,
		Postconditions: map[string]bool{"door_open": true},
	}

	status := cv.Tick(bb, openDoor)
	if status != btcore.Success {
		t.Errorf("expected Success, got %v", status)
	}
}

func TestCheckpointVerifier_Retry_OnFailedPostcondition(t *testing.T) {
	bb := btcore.NewBlackboard()
	bb.Set("world_state_done", "false")
	attempts := 0

	// Child action: only succeeds on 3rd attempt
	child := btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
		attempts++
		if attempts >= 3 {
			bb.Set("world_state_done", "true")
			return btcore.Success
		}
		return btcore.Success // child "succeeds" but doesn't set postcondition
	})

	cv := &CheckpointVerifier{
		MaxRetries:     5,
		Postconditions: map[string]bool{"done": true},
	}

	status := cv.Tick(bb, child)
	if status != btcore.Success {
		t.Errorf("expected Success, got %v", status)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestCheckpointVerifier_ExhaustsRetries(t *testing.T) {
	bb := btcore.NewBlackboard()
	bb.Set("world_state_done", "false")

	child := btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
		return btcore.Success // never sets postcondition
	})

	cv := &CheckpointVerifier{
		MaxRetries:     2,
		Postconditions: map[string]bool{"done": true},
	}

	status := cv.Tick(bb, child)
	if status != btcore.Failure {
		t.Errorf("expected Failure after exhausting retries, got %v", status)
	}
}

func TestCheckpointVerifier_RestoresStateOnFailure(t *testing.T) {
	bb := btcore.NewBlackboard()
	bb.Set("world_state_counter", "0")

	// Child increments counter but fails
	child := btcomp.NewSequence(
		btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
			bb.Set("world_state_counter", "999")
			return btcore.Success
		}),
		btleaf.NewAction(func(bb *btcore.Blackboard) btcore.Status {
			return btcore.Failure
		}),
	)

	cv := &CheckpointVerifier{
		MaxRetries:     1,
		Postconditions: map[string]bool{},
	}

	cv.Tick(bb, child)

	// After retry exhaustion, state should be restored to snapshot
	val, _ := bb.Get("world_state_counter")
	if val != "0" {
		t.Errorf("expected state restored to '0', got '%v'", val)
	}
}
```

**Step 2: Run tests**

```bash
cd /home/nico/go-bt-evolve && go test ./internal/engine/ -run TestCheckpointVerifier -v
```

Expected: 4 PASS

**Step 3: Commit**

```bash
git add internal/engine/checkpoint_verifier_test.go
git commit -m "test: add CheckpointVerifier unit tests for retry and state restoration"
```

---

### Task 2.3: Register CheckpointVerifier in BuildTree and node types

**Objective:** Wire the decorator into the tree builder and evolution validator.

**Step 1: Add to `internal/engine/tree.go` BuildTree**

```go
case "CheckpointVerifier":
    return CheckpointVerifierFactory(node)
```

**Step 2: Add to `internal/evolution/node_types.go` KnownNodeTypes**

```go
"CheckpointVerifier": true,
```

**Step 3: Run full test suite**

```bash
cd /home/nico/go-bt-evolve && go test ./internal/engine/... ./internal/evolution/... -count=1
```

**Step 4: Commit**

```bash
git add internal/engine/tree.go internal/engine/registry.go internal/evolution/node_types.go
git commit -m "feat: register CheckpointVerifier in BuildTree and evolution validator"
```

---

## Sprint 2: P2 — GOAP Contexts + Formal SLOs

### Gap 3: GOAP Contexts — Parameterized Actions

**Objective:** Replace instance-specific GOAP actions (PickAppleTree1, PickAppleTree2) with templated actions that bind parameters at runtime.

**Files:**
- Modify: `internal/engine/planner_node.go` (add parameter binding)
- Create: `internal/engine/parameterized_action.go`

---

### Task 3.1: Implement parameterized GOAP actions

**Step 1: Create `internal/engine/parameterized_action.go`**

```go
package engine

import (
	"fmt"
	"strings"
)

// ParamAction is a GOAP action with runtime parameter binding.
// The ActionFunc field supports template substitution: ${param_name}
// Preconditions and Effects can also use template variables.
type ParamAction struct {
	GOAPAction
	Params map[string]string // parameter name -> binding value
}

// Bind replaces template variables in the action with concrete values.
func (pa *ParamAction) Bind(bindings map[string]string) GOAPAction {
	bound := GOAPAction{
		Name:      bindTemplate(pa.Name, bindings),
		Cost:      pa.Cost,
		ActionFunc: bindTemplate(pa.ActionFunc, bindings),
	}

	bound.Preconditions = make(map[string]bool, len(pa.Preconditions))
	for k, v := range pa.Preconditions {
		bound.Preconditions[bindTemplate(k, bindings)] = v
	}

	bound.Effects = make(map[string]bool, len(pa.Effects))
	for k, v := range pa.Effects {
		bound.Effects[bindTemplate(k, bindings)] = v
	}

	return bound
}

func bindTemplate(tmpl string, bindings map[string]string) string {
	result := tmpl
	for k, v := range bindings {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}
	return result
}

// ParamPlannerNode extends PlannerNode with parameterized actions.
// It resolves parameters from the Blackboard before planning.
type ParamPlannerNode struct {
	PlannerNode
	Templates []ParamAction
}

// PlanWithContext resolves parameters and plans.
func (pp *ParamPlannerNode) PlanWithContext(bb Blackboard, worldState map[string]bool) Plan {
	// Resolve template parameters from blackboard
	bindings := pp.extractBindings(bb)

	pp.Actions = make([]GOAPAction, len(pp.Templates))
	for i, tmpl := range pp.Templates {
		pp.Actions[i] = tmpl.Bind(bindings)
	}

	return pp.Plan(worldState)
}

func (pp *ParamPlannerNode) extractBindings(bb Blackboard) map[string]string {
	bindings := make(map[string]string)
	// Read param_* keys from blackboard to populate bindings
	// This is done via the SerializableNode properties at build time
	return bindings
}
```

**Step 2: Write tests for parameter binding**

```go
func TestParamAction_Bind_Simple(t *testing.T) {
	pa := ParamAction{
		GOAPAction: GOAPAction{
			Name:      "pick_${fruit}_from_${tree}",
			Cost:      1.0,
			Preconditions: map[string]bool{
				"near_${tree}": true,
			},
			Effects: map[string]bool{
				"has_${fruit}": true,
			},
			ActionFunc: "pick_${fruit}_${tree}",
		},
	}

	bound := pa.Bind(map[string]string{"fruit": "apple", "tree": "tree_3"})
	if bound.Name != "pick_apple_from_tree_3" {
		t.Errorf("expected 'pick_apple_from_tree_3', got '%s'", bound.Name)
	}
	if bound.Preconditions["near_tree_3"] != true {
		t.Error("expected precondition 'near_tree_3'")
	}
	if bound.Effects["has_apple"] != true {
		t.Error("expected effect 'has_apple'")
	}
	if bound.ActionFunc != "pick_apple_tree_3" {
		t.Errorf("expected 'pick_apple_tree_3', got '%s'", bound.ActionFunc)
	}
}
```

**Step 3: Commit**

```bash
git add internal/engine/parameterized_action.go internal/engine/parameterized_action_test.go
git commit -m "feat: add parameterized GOAP actions with runtime template binding"
```

---

### Gap 4: Formal SLOs — SLOMonitor Node

**Objective:** Track per-agent tool-call success rate, recovery rate, and p95 latency via a dedicated tree node wired into the gardener metrics pipeline.

**Files:**
- Create: `internal/engine/slo_monitor.go`
- Create: `internal/engine/slo_monitor_test.go`
- Modify: `internal/gardener/gardener.go` (integrate SLO metrics)

---

### Task 4.1: Implement SLOMonitor action node

**Step 1: Create `internal/engine/slo_monitor.go`**

```go
package engine

import (
	"sync"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// SLOMetrics tracks Service Level Objectives for an agent.
type SLOMetrics struct {
	mu sync.RWMutex

	TotalCalls      int64
	SuccessfulCalls int64
	FailedCalls     int64
	RecoveredCalls  int64 // calls that succeeded after retry

	TotalLatencyMs int64
	MaxLatencyMs   int64
	p95Samples     []int64 // rolling window for p95

	AgentName string
	TreeName  string
}

// SuccessRate returns the fraction of successful tool calls.
func (m *SLOMetrics) SuccessRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.TotalCalls == 0 {
		return 1.0
	}
	return float64(m.SuccessfulCalls) / float64(m.TotalCalls)
}

// RecoveryRate returns the fraction of failed calls that recovered.
func (m *SLOMetrics) RecoveryRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.FailedCalls == 0 {
		return 0
	}
	return float64(m.RecoveredCalls) / float64(m.FailedCalls)
}

// AvgLatencyMs returns average latency in milliseconds.
func (m *SLOMetrics) AvgLatencyMs() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.TotalCalls == 0 {
		return 0
	}
	return float64(m.TotalLatencyMs) / float64(m.TotalCalls)
}

// RecordSuccess records a successful tool call.
func (m *SLOMetrics) RecordSuccess(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCalls++
	m.SuccessfulCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
	if ms > m.MaxLatencyMs {
		m.MaxLatencyMs = ms
	}
}

// RecordFailure records a failed tool call.
func (m *SLOMetrics) RecordFailure(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCalls++
	m.FailedCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
}

// RecordRecovery records a recovery after failure.
func (m *SLOMetrics) RecordRecovery(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecoveredCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
}

// Summary returns a human-readable SLO summary.
func (m *SLOMetrics) Summary() string {
	return formatSLO(m.AgentName, m.TreeName, m.SuccessRate(), m.RecoveryRate(), m.AvgLatencyMs())
}

// sloRegistry stores per-agent SLO metrics.
var sloRegistry = &sync.Map{}

// GetSLOMetrics returns or creates SLO metrics for an agent.
func GetSLOMetrics(agentName, treeName string) *SLOMetrics {
	key := agentName + ":" + treeName
	if val, ok := sloRegistry.Load(key); ok {
		return val.(*SLOMetrics)
	}
	m := &SLOMetrics{AgentName: agentName, TreeName: treeName}
	actual, _ := sloRegistry.LoadOrStore(key, m)
	return actual.(*SLOMetrics)
}

// SLOMonitorAction creates an action node that wraps a child action
// and records SLO metrics for it.
func SLOMonitorAction(agentName, treeName string, child btcore.Node) btcore.Node {
	metrics := GetSLOMetrics(agentName, treeName)

	return btcore.NewAction(func(bb *btcore.Blackboard) btcore.Status {
		start := time.Now()

		wasFailed := false
		if prevStatus, ok := bb.Get("_prev_status"); ok && prevStatus == "Failure" {
			wasFailed = true
		}

		status := child.Tick(bb)
		elapsed := time.Since(start)

		switch status {
		case btcore.Success:
			if wasFailed {
				metrics.RecordRecovery(elapsed)
			} else {
				metrics.RecordSuccess(elapsed)
			}
		case btcore.Failure:
			metrics.RecordFailure(elapsed)
		}

		bb.Set("_prev_status", status.String())
		return status
	})
}
```

**Step 2: Commit + test**

```bash
git add internal/engine/slo_monitor.go internal/engine/slo_monitor_test.go
git commit -m "feat: add SLOMonitor for per-agent tool-call SLO tracking"
```

---

### Task 4.2: Wire SLO metrics into gardener

**Objective:** Expose SLO metrics via the gardener's `/metrics` endpoint and dashboard.

Modify `internal/gardener/gardener.go` to call `GetSLOMetrics` and include SLO data in the periodic metrics export.

---

## Sprint 3: P3 — Decentralized Coordination (Validation Gate)

### Gap 5: Gardener Validation Gate

**Objective:** Add a pre-deployment validation step in the gardener that rejects evolved trees below a fitness threshold before they're activated.

**File:** Modify `internal/gardener/gardener.go`

**Implementation:**

Add a `ValidationGate` function that runs after evolution but before agent deployment:

```go
// ValidationGate checks evolved trees against minimum quality thresholds
// before allowing them to be deployed to agents.
func ValidationGate(tree *evolution.SerializableNode, metrics *SLOMetrics) error {
    // Minimum success rate threshold
    if metrics.SuccessRate() < 0.80 {
        return fmt.Errorf("validation gate: success rate %.2f below threshold 0.80", metrics.SuccessRate())
    }
    // Recovery rate must be improving or high
    if metrics.RecoveryRate() < 0.30 {
        return fmt.Errorf("validation gate: recovery rate %.2f below threshold 0.30", metrics.RecoveryRate())
    }
    // Tree must be structurally valid
    if ok, errs := evolution.ValidateTree(tree); !ok {
        return fmt.Errorf("validation gate: tree validation failed: %v", errs)
    }
    return nil
}
```

Wire into the gardener's `RunCycleV2` after tree evolution, before the tree is written to the agent's active config.

---

## Verification & Rollout

### Integration Test: Full Pipeline

```bash
cd /home/nico/go-bt-evolve

# Run all new tests
go test ./internal/engine/ -run "TestPlannerNode|TestCheckpointVerifier|TestParamAction" -v

# Run full suite
go test ./internal/... -count=1 -timeout 120s

# Build binaries
go build ./cmd/bt-gardener/
go build ./cmd/bt-agent/
```

### Rollout Steps
1. Deploy updated gardener binary: kill old PID 522174, build new, start
2. Update `goap_planning` domain tree to use PlannerNode instead of static selectors
3. Add CheckpointVerifier wrapper on the 3 longest-horizon agents
4. Monitor SLO dashboard for success rate trends
5. After 24h stable, enable validation gate

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| PlannerNode A* search could exceed tick budget on large action sets | MaxDepth=5 default, action count warning if > 20 |
| CheckpointVerifier snapshot/restore may miss non-world_state blackboard keys | Document contract: all mutable state must use `world_state_` prefix |
| Parameter binding could break existing static GOAP trees | Backward-compat: ParamPlannerNode is opt-in; existing trees unchanged |
| SLO monitor adds latency overhead | Lock-free fast path for recording; metrics export is async |
| Validation gate too aggressive, blocks valid evolution | Tunable thresholds via gardener config, log rejections for review |

---

## Open Questions
1. Should PlannerNode cache computed plans per world state hash? (Performance vs. staleness tradeoff)
2. CheckpointVerifier retry count per-agent or global?
3. SLO metrics: push to OTLP collector or keep in-process only?
4. Validation gate: soft-reject (log + deploy) or hard-reject (block deployment)?
