package engine

// Model routing outcome conditions.
//
// classifyTaskRoute (model_routing.go) resolves a model-routed DecisionTree's
// branch and records the provenance of that choice in ChainState["route_source"]:
// routeSourceModel when the configured LLM classified the task with sufficient
// confidence, or fallback_exact / fallback_default when the model was
// unavailable, errored, returned an unparseable or non-candidate label, or fell
// below the confidence threshold.
//
// The resolved branch label alone cannot distinguish a confident model route
// from a deterministic default, so these conditions surface that distinction to
// the tree. A Selector can divert a task the model could not confidently route
// into a clarification (HITL), decomposition, or escalation branch instead of
// silently proceeding down the default path — mirroring how AgentStalled /
// AgentCompleted let a tree branch on whether the agent loop finished cleanly.

func init() {
	// RouteResolvedByModel: the last model-routed DecisionTree chose its branch
	// from the LLM's classification (confidence met the node threshold).
	RegisterCondition("RouteResolvedByModel", func(bb *Blackboard) bool {
		return lastRouteSource(bb) == routeSourceModel
	})
	// RouteFellBack: the last routing decision used the deterministic fallback —
	// either an exact whole-string match or the default branch — because the
	// model was unavailable, errored, or its proposal was rejected. Lets a tree
	// react to "the model did not confidently place this task".
	RegisterCondition("RouteFellBack", func(bb *Blackboard) bool {
		s := lastRouteSource(bb)
		return s == routeSourceExact || s == routeSourceDefault
	})
	// RouteUnresolved: the strongest "model could not place this task" signal —
	// neither the model nor a deterministic exact match produced a branch, so the
	// DecisionTree took its default. Ideal for triggering a clarification gate.
	RegisterCondition("RouteUnresolved", func(bb *Blackboard) bool {
		return lastRouteSource(bb) == routeSourceDefault
	})
}

// lastRouteSource returns the provenance of the most recent model-routed
// DecisionTree branch decision, as recorded in ChainState["route_source"] by
// persistRouteDecision, or "" if no model routing has run on this blackboard.
func lastRouteSource(bb *Blackboard) string {
	if bb == nil || bb.ChainState == nil {
		return ""
	}
	s, _ := bb.ChainState["route_source"].(string)
	return s
}
