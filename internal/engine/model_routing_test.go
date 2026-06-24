package engine

import (
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// modelRouterTree builds a model-routed DecisionTree over code/research branches
// with a fallback default branch.
func modelRouterTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "DecisionTree",
		Name: "ModelTaskRouter",
		Metadata: map[string]any{
			"source":               "model",
			"default":              "fallback",
			"confidence_threshold": 0.6,
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "ModelRouteCodePath", Metadata: map[string]any{"match": "code_review"}},
			{Type: "Action", Name: "ModelRouteResearchPath", Metadata: map[string]any{"match": "research"}},
			{Type: "Action", Name: "ModelRouteFallbackPath", Metadata: map[string]any{"branch": "fallback"}},
		},
	}
}

func TestModelRouting_HighConfidenceRoutesToModelLabel(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		Task:       "Please review this pull request for security issues",
		ChainState: map[string]any{},
		LLM: &MockLLM{GenerateResp: `{"label":"code_review","confidence":0.92,` +
			`"rationale":"asks to review a pull request"}`},
	}

	bt := BuildTree(modelRouterTree(), bb)
	result := RunTask(bb, bt)

	if result != "code path selected" {
		t.Fatalf("expected code branch, got %q", result)
	}
	if bb.ChainState["route_source"] != routeSourceModel {
		t.Fatalf("expected model source, got %v", bb.ChainState["route_source"])
	}
	if bb.ChainState["route_label"] != "code_review" {
		t.Fatalf("expected route_label code_review, got %v", bb.ChainState["route_label"])
	}
	if conf, _ := bb.ChainState["route_confidence"].(float64); conf < 0.6 {
		t.Fatalf("expected persisted confidence >= 0.6, got %v", bb.ChainState["route_confidence"])
	}
	if r, _ := bb.ChainState["route_rationale"].(string); r == "" {
		t.Fatalf("expected a persisted rationale")
	}
}

func TestModelRouting_LowConfidenceFallsBackToDefault(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		Task:       "something genuinely ambiguous",
		ChainState: map[string]any{},
		// Confidence below the 0.6 threshold → deterministic fallback.
		LLM: &MockLLM{GenerateResp: `{"label":"code_review","confidence":0.3,"rationale":"unsure"}`},
	}

	bt := BuildTree(modelRouterTree(), bb)
	result := RunTask(bb, bt)

	if result != "fallback selected" {
		t.Fatalf("expected fallback branch on low confidence, got %q", result)
	}
	if bb.ChainState["route_source"] != routeSourceDefault {
		t.Fatalf("expected fallback_default source, got %v", bb.ChainState["route_source"])
	}
}

func TestModelRouting_LLMErrorFallsBackToExactMatch(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		// Task is exactly a candidate label → deterministic exact match wins
		// even though the model errored.
		Task:       "research",
		ChainState: map[string]any{},
		LLM:        &MockLLM{GenerateErr: errors.New("ollama down")},
	}

	bt := BuildTree(modelRouterTree(), bb)
	result := RunTask(bb, bt)

	if result != "research path selected" {
		t.Fatalf("expected research branch via exact fallback, got %q", result)
	}
	if bb.ChainState["route_source"] != routeSourceExact {
		t.Fatalf("expected fallback_exact source, got %v", bb.ChainState["route_source"])
	}
}

func TestModelRouting_NilLLMUsesDeterministicFallback(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{
		Task:       "no model configured and not an exact label",
		ChainState: map[string]any{},
		LLM:        nil,
	}

	bt := BuildTree(modelRouterTree(), bb)
	result := RunTask(bb, bt)

	if result != "fallback selected" {
		t.Fatalf("expected fallback branch when LLM is nil, got %q", result)
	}
	if bb.ChainState["route_source"] != routeSourceDefault {
		t.Fatalf("expected fallback_default source, got %v", bb.ChainState["route_source"])
	}
}

func TestParseRouteResponse_LineBasedFallback(t *testing.T) {
	label, conf, rationale, ok := parseRouteResponse("label: research\nconfidence: 0.8\nrationale: gather info")
	if !ok || label != "research" || conf != 0.8 || rationale != "gather info" {
		t.Fatalf("line-based parse failed: label=%q conf=%v rationale=%q ok=%v", label, conf, rationale, ok)
	}
}

func TestRouteHistory_AccumulatesAcrossDecisions(t *testing.T) {
	registerDecisionTreeAction(t, "ModelRouteCodePath", "code path selected")
	registerDecisionTreeAction(t, "ModelRouteResearchPath", "research path selected")
	registerDecisionTreeAction(t, "ModelRouteFallbackPath", "fallback selected")

	bb := &Blackboard{ChainState: map[string]any{}}

	// First decision: model picks research with high confidence.
	first := RouteDecision{Label: "research", Confidence: 0.81, Rationale: "gather info", Source: routeSourceModel}
	persistRouteDecision(bb, modelRouterTree(), first)

	// Second decision under a distinct node key: deterministic exact fallback.
	secondNode := modelRouterTree()
	secondNode.Name = "SecondRouter"
	second := RouteDecision{Label: "code_review", Confidence: 1.0, Source: routeSourceExact}
	persistRouteDecision(bb, secondNode, second)

	hist := RouteHistory(bb)
	if len(hist) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(hist))
	}
	if hist[0].Key != "ModelTaskRouter" || hist[0].Label != "research" || hist[0].Source != routeSourceModel {
		t.Fatalf("unexpected first entry: %+v", hist[0])
	}
	if hist[1].Key != "SecondRouter" || hist[1].Label != "code_review" || hist[1].Source != routeSourceExact {
		t.Fatalf("unexpected second entry: %+v", hist[1])
	}
	// Singleton keys still reflect only the latest decision.
	if bb.ChainState["route_label"] != "code_review" {
		t.Fatalf("expected latest route_label code_review, got %v", bb.ChainState["route_label"])
	}
}

func TestRouteHistory_NilBlackboard(t *testing.T) {
	if RouteHistory(nil) != nil {
		t.Fatal("expected nil history for nil blackboard")
	}
	if RouteHistory(&Blackboard{}) != nil {
		t.Fatal("expected nil history when ChainState is nil")
	}
}

func TestCollectBranchLabels_SkipsDefault(t *testing.T) {
	labels := collectBranchLabels(modelRouterTree())
	if len(labels) != 2 {
		t.Fatalf("expected 2 candidate labels (default excluded), got %v", labels)
	}
}
