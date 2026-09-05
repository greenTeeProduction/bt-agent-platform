package engine

import (
	"context"
	"reflect"
	"slices"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

// forkBlackboard isolates branch-owned data. Services (LLM, synchronized stores,
// event bus and tick log) remain shared; per-node execution state never does.
func forkBlackboard(bb *Blackboard) *Blackboard {
	if bb == nil {
		return nil
	}
	cp := *bb
	cp.ChainState = cloneParallelValue(bb.ChainState)
	cp.ChainTools = slices.Clone(bb.ChainTools)
	cp.Results = slices.Clone(bb.Results)
	cp.VisitedPaths = slices.Clone(bb.VisitedPaths)
	cp.ChainMemory = cloneParallelValue(bb.ChainMemory)
	cp.parallelStates = nil
	cp.buildCapture = nil
	if bb.BB != nil {
		h := *bb.BB
		cp.BB = &h
	}
	return &cp
}

// Clone data graphs, including nested pointer DTOs and cycles. Opaque service
// structs with private fields are dependencies, not branch-owned DTOs, and must
// provide their own synchronization (as the platform stores do).
func cloneParallelValue[T any](value T) T {
	seen := map[reflect.Value]reflect.Value{}
	type sliceIdentity struct {
		typ     reflect.Type
		pointer uintptr
		length  int
	}
	seenSlices := map[sliceIdentity]reflect.Value{}
	var clone func(reflect.Value) reflect.Value
	clone = func(v reflect.Value) reflect.Value {
		if !v.IsValid() {
			return v
		}
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				return v
			}
			c := reflect.New(v.Type()).Elem()
			c.Set(clone(v.Elem()))
			return c
		case reflect.Pointer:
			if v.IsNil() {
				return v
			}
			if c, ok := seen[v]; ok {
				return c
			}
			if v.Elem().Kind() == reflect.Struct {
				for i := range v.Elem().NumField() {
					if !v.Elem().Type().Field(i).IsExported() {
						return v
					}
				}
			}
			c := reflect.New(v.Type().Elem())
			seen[v] = c
			c.Elem().Set(clone(v.Elem()))
			return c
		case reflect.Map:
			if v.IsNil() {
				return v
			}
			if c, ok := seen[v]; ok {
				return c
			}
			c := reflect.MakeMapWithSize(v.Type(), v.Len())
			seen[v] = c
			it := v.MapRange()
			for it.Next() {
				c.SetMapIndex(it.Key(), clone(it.Value()))
			}
			return c
		case reflect.Slice:
			if v.IsNil() {
				return v
			}
			key := sliceIdentity{v.Type(), v.Pointer(), v.Len()}
			if c, ok := seenSlices[key]; ok {
				return c
			}
			c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
			seenSlices[key] = c
			for i := range v.Len() {
				c.Index(i).Set(clone(v.Index(i)))
			}
			return c
		case reflect.Array:
			c := reflect.New(v.Type()).Elem()
			for i := range v.Len() {
				c.Index(i).Set(clone(v.Index(i)))
			}
			return c
		case reflect.Struct:
			c := reflect.New(v.Type()).Elem()
			c.Set(v)
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					c.Field(i).Set(clone(v.Field(i)))
				}
			}
			return c
		default:
			return v
		}
	}
	v := clone(reflect.ValueOf(value))
	if !v.IsValid() {
		return value
	}
	return v.Interface().(T)
}

type parallelCommand struct {
	children        []btcore.Command[Blackboard]
	mode            ParallelMode
	monitors        []int
	cancelOnMonitor bool
}
type parallelState struct {
	bases    []*Blackboard
	contexts []*btcore.BTContext[Blackboard]
	statuses []int
}

func BuildReactiveParallel(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	p := &parallelCommand{mode: ParallelAll, cancelOnMonitor: true}
	if node.Metadata != nil {
		switch node.Metadata["mode"] {
		case "any":
			p.mode = ParallelAny
		case "race":
			p.mode = ParallelRace
		case "monitor":
			p.mode = ParallelMonitor
		}
		p.monitors = intSliceFromInterface(node.Metadata["monitor_indices"])
		if v, ok := node.Metadata["cancel_on_monitor"].(bool); ok {
			p.cancelOnMonitor = v
		}
	}
	for i := range node.Children {
		p.children = append(p.children, buildNode(&node.Children[i], bb, node.Name))
	}
	return p
}
func (p *parallelCommand) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.parallelStates == nil {
		bb.parallelStates = map[*parallelCommand]*parallelState{}
	}
	state := bb.parallelStates[p]
	if state == nil {
		state = &parallelState{bases: make([]*Blackboard, len(p.children)), contexts: make([]*btcore.BTContext[Blackboard], len(p.children)), statuses: make([]int, len(p.children))}
		bb.parallelStates[p] = state
	}
	result := p.tick(ctx, state)
	if result != 0 {
		delete(bb.parallelStates, p)
	}
	return result
}
func runReactiveParallel(children []btcore.Command[Blackboard], mode ParallelMode, monitorIndices, _ []int, cancelOnMonitor bool, ctx *btcore.BTContext[Blackboard]) int {
	return (&parallelCommand{children: children, mode: mode, monitors: monitorIndices, cancelOnMonitor: cancelOnMonitor}).Run(ctx)
}
func (p *parallelCommand) tick(ctx *btcore.BTContext[Blackboard], state *parallelState) int {
	parent := ctx.Context
	if parent == nil {
		parent = chainContext(ctx.Blackboard)
	}
	if parent.Err() != nil {
		return -1
	}
	childCtx, cancel := context.WithCancel(parent)
	defer cancel()
	type result struct{ index, code int }
	results := make(chan result, len(p.children))
	before := make([]*Blackboard, len(p.children))
	count := 0
	for i, cmd := range p.children {
		if state.statuses[i] != 0 && (p.mode != ParallelMonitor || !slices.Contains(p.monitors, i) || state.statuses[i] != 1) {
			continue
		}
		if state.contexts[i] == nil {
			state.contexts[i] = btcore.NewBTContext(childCtx, forkBlackboard(ctx.Blackboard))
			state.bases[i] = forkBlackboard(ctx.Blackboard)
		}
		local := state.contexts[i]
		local.Blackboard.TokensUsed = ctx.Blackboard.TokensUsed
		local.Blackboard.TreeTicks = ctx.Blackboard.TreeTicks
		local.Blackboard.FailureCount = ctx.Blackboard.FailureCount
		local.Context = childCtx
		local.Blackboard.TraceContext = childCtx
		if ctx.Now != nil {
			local.Now = ctx.Now
		}
		before[i] = forkBlackboard(local.Blackboard)
		count++
		go func() {
			code := -1
			defer func() {
				if v := recover(); v != nil {
					reliability.DefaultPanicHandler(v, "parallel child")
				}
				results <- result{i, code}
			}()
			code = cmd.Run(local)
		}()
	}
	winner := -1
	cancelled := false
	done := parent.Done()
	for range count {
		var r result
		select {
		case r = <-results:
		case <-done:
			cancelled = true
			cancel()
			done = nil
			r = <-results // Join: tools must honor their child context before returning.
		}
		state.statuses[r.index] = r.code
		if winner < 0 && ((p.mode == ParallelAny && r.code == 1) || (p.mode == ParallelRace && r.code != 0)) {
			winner = r.index
			cancel()
		}
		if (p.mode == ParallelAll || p.mode == ParallelMonitor || p.mode > ParallelMonitor) && r.code < 0 {
			if p.mode != ParallelMonitor || p.cancelOnMonitor {
				cancel()
			}
		}
	}
	running, failed := false, false
	for _, code := range state.statuses {
		running = running || code == 0
		failed = failed || code < 0
	}
	// Merge in declaration order, irrespective of goroutine completion order.
	// Any/race publish only the winner's data; all work contributes resource use
	// and outstanding approval flags, even a losing or cancelled branch.
	for i, old := range before {
		if old == nil {
			continue
		}
		child := state.contexts[i].Blackboard
		bb := ctx.Blackboard
		bb.TokensUsed += max(0, child.TokensUsed-old.TokensUsed)
		bb.TreeTicks += max(0, child.TreeTicks-old.TreeTicks)
		bb.FailureCount += max(0, child.FailureCount-old.FailureCount)
		if p.mode != ParallelAny && p.mode != ParallelRace {
			mergeParallelOutput(bb, old, child)
		} else if winner == i {
			mergeParallelOutput(bb, state.bases[i], child)
			bb.Outcome = child.Outcome
		}
	}
	if running {
		for _, local := range state.contexts {
			if local != nil && local.Blackboard.Outcome == "pending_approval" {
				ctx.Blackboard.Outcome = "pending_approval"
			}
		}
	}
	if cancelled || parent.Err() != nil {
		return -1
	}
	if winner >= 0 {
		return state.statuses[winner]
	}
	if p.mode == ParallelAny {
		if running {
			return 0
		}
		return -1
	}
	if failed {
		return -1
	}
	if running {
		return 0
	}
	return 1
}

func mergeParallelOutput(dst, old, src *Blackboard) {
	// Scalars use last changed child in declaration order. Counters are additive
	// in tick; append-only logs contribute only each child's new suffix.
	if src.Result != old.Result {
		dst.Result = src.Result
	}
	if src.Plan != old.Plan {
		dst.Plan = src.Plan
	}
	if src.Complexity != old.Complexity {
		dst.Complexity = src.Complexity
	}
	if src.Outcome != old.Outcome {
		dst.Outcome = src.Outcome
	}
	if src.KgResults != old.KgResults {
		dst.KgResults = src.KgResults
	}
	if src.CachedResult != old.CachedResult {
		dst.CachedResult = src.CachedResult
	}
	if src.QualityScore != old.QualityScore {
		dst.QualityScore = src.QualityScore
	}
	if src.OutcomeRefinement != old.OutcomeRefinement {
		dst.OutcomeRefinement = src.OutcomeRefinement
	}
	if src.QualityAuthoritative != old.QualityAuthoritative {
		dst.QualityAuthoritative = src.QualityAuthoritative
	}
	if src.TickBudget != old.TickBudget {
		dst.TickBudget = src.TickBudget
	}
	if !reflect.DeepEqual(src.ChainTools, old.ChainTools) {
		dst.ChainTools = slices.Clone(src.ChainTools)
	}
	if !reflect.DeepEqual(src.ChainMemory, old.ChainMemory) {
		dst.ChainMemory = cloneParallelValue(src.ChainMemory)
	}
	if len(src.Results) > len(old.Results) {
		dst.Results = append(dst.Results, src.Results[len(old.Results):]...)
	}
	if len(src.VisitedPaths) > len(old.VisitedPaths) {
		dst.VisitedPaths = append(dst.VisitedPaths, src.VisitedPaths[len(old.VisitedPaths):]...)
	}
	if dst.ChainState == nil {
		dst.ChainState = map[string]any{}
	}
	for key, v := range src.ChainState {
		if prev, ok := old.ChainState[key]; !ok || !reflect.DeepEqual(prev, v) {
			dst.ChainState[key] = cloneParallelValue(v)
		}
	}
	for key := range old.ChainState {
		if _, ok := src.ChainState[key]; !ok {
			delete(dst.ChainState, key)
		}
	}
}
