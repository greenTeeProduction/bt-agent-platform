package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Route decision sources, persisted to the Blackboard for observability.
const (
	routeSourceModel   = "model"            // LLM classified the task with sufficient confidence
	routeSourceExact   = "fallback_exact"   // deterministic exact match (LLM unavailable / low confidence)
	routeSourceDefault = "fallback_default" // no exact match; default branch will be taken
)

// defaultRouteThreshold is the minimum model confidence required to accept a
// model-produced route label. Below this the router falls back to deterministic
// exact/default matching.
const defaultRouteThreshold = 0.5

// RouteDecision is the structured result of model-assisted task routing.
// It is produced by classifyTaskRoute and persisted into the Blackboard's
// ChainState so downstream nodes, conditions, and observability can inspect
// why a particular branch was taken.
type RouteDecision struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
	Source     string  `json:"source"`
}

// isModelRouted reports whether a DecisionTree node is configured to resolve its
// branch label via the configured LLM instead of a raw blackboard value.
// Enabled by metadata `source: "model"` or `classify: true`.
func isModelRouted(node *evolution.SerializableNode) bool {
	if node.Metadata == nil {
		return false
	}
	if src, ok := node.Metadata["source"].(string); ok && src == "model" {
		return true
	}
	if c, ok := node.Metadata["classify"].(bool); ok && c {
		return true
	}
	return false
}

// routeThreshold reads the per-node confidence threshold, defaulting to
// defaultRouteThreshold. Accepts float64, int, or numeric string metadata.
func routeThreshold(node *evolution.SerializableNode) float64 {
	if node.Metadata == nil {
		return defaultRouteThreshold
	}
	switch v := node.Metadata["confidence_threshold"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultRouteThreshold
}

// collectBranchLabels gathers the candidate route labels from a DecisionTree's
// children, derived from their match/branch/matches metadata. Default branches
// are skipped — they are the fallback, never a classification target.
func collectBranchLabels(node *evolution.SerializableNode) []string {
	// The node-level default branch label is never a classification candidate.
	nodeDefault := ""
	if node.Metadata != nil {
		if d, ok := node.Metadata["default"].(string); ok {
			nodeDefault = strings.TrimSpace(d)
		}
	}
	seen := make(map[string]bool)
	var labels []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		labels = append(labels, s)
	}
	for i := range node.Children {
		child := &node.Children[i]
		if child.Metadata == nil {
			continue
		}
		if isDefault, ok := child.Metadata["default"].(bool); ok && isDefault {
			continue
		}
		// Skip the child designated as default via node-level `default: <branch>`.
		if nodeDefault != "" {
			if b, ok := child.Metadata["branch"].(string); ok && strings.TrimSpace(b) == nodeDefault {
				continue
			}
			if m, ok := child.Metadata["match"]; ok && stringifyDecisionValue(m) == nodeDefault {
				continue
			}
		}
		if m, ok := child.Metadata["match"]; ok {
			add(stringifyDecisionValue(m))
		}
		if b, ok := child.Metadata["branch"].(string); ok {
			add(b)
		}
		switch vals := child.Metadata["matches"].(type) {
		case []string:
			for _, v := range vals {
				add(v)
			}
		case []any:
			for _, v := range vals {
				add(stringifyDecisionValue(v))
			}
		}
	}
	return labels
}

// classifyTaskRoute resolves a route label for free-text input.
//
// Primary path: when an LLM is configured it is asked to classify the input into
// exactly one of the candidate labels, returning a structured label/confidence/
// rationale. The label must match a candidate exactly (case-insensitive) and meet
// the confidence threshold to be accepted.
//
// Fallback path (LLM unavailable, empty/unparseable response, unknown label, or
// low confidence): deterministic exact match of the input against a candidate;
// otherwise an empty label that routes to the DecisionTree's default branch. The
// fallback is never substring/keyword based — only whole-string exact equality.
func classifyTaskRoute(bb *Blackboard, input string, candidates []string, threshold float64) RouteDecision {
	exact := exactLabel(input, candidates)

	if bb == nil || bb.LLM == nil || len(candidates) == 0 {
		return fallbackDecision(exact)
	}

	ctx := bb.TraceContext
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := bb.LLM.GenerateCtx(ctx, buildRoutePrompt(input, candidates))
	if err != nil || strings.TrimSpace(resp) == "" {
		return fallbackDecision(exact)
	}

	label, conf, rationale, ok := parseRouteResponse(resp)
	if !ok {
		return fallbackDecision(exact)
	}
	canon := exactLabel(label, candidates)
	if canon == "" || conf < threshold {
		return fallbackDecision(exact)
	}

	return RouteDecision{
		Label:      canon,
		Confidence: conf,
		Rationale:  strings.TrimSpace(rationale),
		Source:     routeSourceModel,
	}
}

// fallbackDecision builds a deterministic decision from an exact-match result.
func fallbackDecision(exact string) RouteDecision {
	if exact != "" {
		return RouteDecision{Label: exact, Confidence: 1.0, Source: routeSourceExact}
	}
	return RouteDecision{Source: routeSourceDefault}
}

// exactLabel returns the canonical candidate equal to value (case-insensitive,
// trimmed), or "" if there is no whole-string match.
func exactLabel(value string, candidates []string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return ""
	}
	for _, c := range candidates {
		if strings.ToLower(strings.TrimSpace(c)) == v {
			return c
		}
	}
	return ""
}

// buildRoutePrompt asks the model to classify the task into one candidate label.
func buildRoutePrompt(input string, candidates []string) string {
	var b strings.Builder
	b.WriteString("You are a task router. Classify the task into exactly ONE of the candidate route labels.\n")
	b.WriteString("Respond ONLY with a JSON object of the form ")
	b.WriteString(`{"label": "<one candidate>", "confidence": <0.0-1.0>, "rationale": "<short reason>"}.`)
	b.WriteString("\nIf none clearly fits, set label to \"\" (empty) and confidence to 0.\n\n")
	b.WriteString("Candidate labels:\n")
	for _, c := range candidates {
		b.WriteString("- ")
		b.WriteString(c)
		b.WriteString("\n")
	}
	b.WriteString("\nTask:\n")
	b.WriteString(input)
	b.WriteString("\n")
	return b.String()
}

// parseRouteResponse extracts label/confidence/rationale from a model response.
// It first tries a JSON object embedded anywhere in the text, then falls back to
// simple "key: value" line parsing. Returns ok=false when nothing usable found.
func parseRouteResponse(resp string) (label string, confidence float64, rationale string, ok bool) {
	if obj := extractJSONObject(resp); obj != "" {
		var parsed struct {
			Label      string  `json:"label"`
			Confidence float64 `json:"confidence"`
			Rationale  string  `json:"rationale"`
		}
		if err := json.Unmarshal([]byte(obj), &parsed); err == nil {
			return parsed.Label, parsed.Confidence, parsed.Rationale, true
		}
	}

	// Line-based fallback for non-JSON model output.
	var foundLabel bool
	for _, line := range strings.Split(resp, "\n") {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), `"',`))
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "label":
			label = val
			foundLabel = true
		case "confidence":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				confidence = f
			}
		case "rationale":
			rationale = val
		}
	}
	return label, confidence, rationale, foundLabel
}

// extractJSONObject returns the substring from the first '{' to the last '}',
// or "" if no balanced-looking object is present.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// persistRouteDecision records the routing decision on the Blackboard so
// downstream nodes and observability can inspect how the branch was chosen.
func persistRouteDecision(bb *Blackboard, node *evolution.SerializableNode, d RouteDecision) {
	if bb == nil {
		return
	}
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["route_label"] = d.Label
	bb.ChainState["route_confidence"] = d.Confidence
	bb.ChainState["route_rationale"] = d.Rationale
	bb.ChainState["route_source"] = d.Source
	// Scope the resolved label under the node's decision key too, so re-entrant
	// reads and chained DecisionTrees observe the same value.
	bb.ChainState[decisionKey(node)] = d.Label
}
