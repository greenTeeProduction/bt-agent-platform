package engine

import "testing"

// evalCond looks up a registered condition by name and evaluates it against bb.
func evalCond(t *testing.T, name string, bb *Blackboard) bool {
	t.Helper()
	fn := GetCondition(name)
	if fn == nil {
		t.Fatalf("condition %q not registered", name)
	}
	return fn(bb)
}

// assertRouteConds checks the three route conditions against expected values.
func assertRouteConds(t *testing.T, bb *Blackboard, model, fellBack, unresolved bool) {
	t.Helper()
	if got := evalCond(t, "RouteResolvedByModel", bb); got != model {
		t.Errorf("RouteResolvedByModel = %v, want %v (source=%v)", got, model, bb.ChainState["route_source"])
	}
	if got := evalCond(t, "RouteFellBack", bb); got != fellBack {
		t.Errorf("RouteFellBack = %v, want %v (source=%v)", got, fellBack, bb.ChainState["route_source"])
	}
	if got := evalCond(t, "RouteUnresolved", bb); got != unresolved {
		t.Errorf("RouteUnresolved = %v, want %v (source=%v)", got, unresolved, bb.ChainState["route_source"])
	}
}

// TestRouteConditions_NoRouting verifies the conditions are all false before any
// model-routed DecisionTree has run (nil and empty ChainState).
func TestRouteConditions_NoRouting(t *testing.T) {
	assertRouteConds(t, &Blackboard{}, false, false, false)
	assertRouteConds(t, &Blackboard{ChainState: map[string]any{}}, false, false, false)
}

// TestRouteConditions_ModelResolved drives a real model-routed tree to a
// confident model decision and asserts only RouteResolvedByModel fires.
func TestRouteConditions_ModelResolved(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		Task:       "Please review this pull request for security issues",
		ChainState: map[string]any{},
		LLM: &MockLLM{GenerateResp: `{"label":"code_review","confidence":0.92,` +
			`"rationale":"asks to review a pull request"}`},
	}
	RunTask(bb, BuildTree(modelRouterTree(), bb))

	if bb.ChainState["route_source"] != routeSourceModel {
		t.Fatalf("precondition: expected model source, got %v", bb.ChainState["route_source"])
	}
	assertRouteConds(t, bb, true, false, false)
}

// TestRouteConditions_FellBackToDefault drives the tree to a low-confidence
// decision that falls back to the default branch, asserting RouteFellBack and
// RouteUnresolved fire while RouteResolvedByModel does not.
func TestRouteConditions_FellBackToDefault(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		Task:       "something genuinely ambiguous",
		ChainState: map[string]any{},
		LLM:        &MockLLM{GenerateResp: `{"label":"code_review","confidence":0.3,"rationale":"unsure"}`},
	}
	RunTask(bb, BuildTree(modelRouterTree(), bb))

	if bb.ChainState["route_source"] != routeSourceDefault {
		t.Fatalf("precondition: expected default source, got %v", bb.ChainState["route_source"])
	}
	assertRouteConds(t, bb, false, true, true)
}

// TestRouteConditions_ExactFallback verifies that a deterministic exact-match
// fallback (model unavailable) is reported as a fall-back but not unresolved.
func TestRouteConditions_ExactFallback(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"route_source": routeSourceExact}}
	assertRouteConds(t, bb, false, true, false)
}
