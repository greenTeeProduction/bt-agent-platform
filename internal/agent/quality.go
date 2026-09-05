package agent

import (
	"fmt"
	"strings"
)

// ValidateQualitySpec checks agent output against YAML quality gates.
// Returns a score (0–1), whether all gates passed, and human-readable reasons.
func ValidateQualitySpec(spec *QualitySpec, output string) (score float64, ok bool, reasons []string) {
	trimmed := strings.TrimSpace(output)
	lower := strings.ToLower(trimmed)

	if spec == nil {
		return estimateQuality(output), true, nil
	}

	if spec.MinLength > 0 && len(trimmed) < spec.MinLength {
		reasons = append(reasons, fmt.Sprintf("min_length: got %d, want >= %d", len(trimmed), spec.MinLength))
	}
	for _, sec := range spec.RequiredSections {
		if sec == "" {
			continue
		}
		if !strings.Contains(output, sec) && !strings.Contains(lower, strings.ToLower(sec)) {
			reasons = append(reasons, "missing section: "+sec)
		}
	}
	for _, kw := range spec.RequiredKeywords {
		if kw == "" {
			continue
		}
		if !strings.Contains(lower, strings.ToLower(kw)) {
			reasons = append(reasons, "missing keyword: "+kw)
		}
	}
	for _, pat := range spec.BlockedPatterns {
		if pat == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(pat)) {
			reasons = append(reasons, "blocked pattern: "+pat)
		}
	}

	score = estimateQuality(output)
	if len(reasons) > 0 {
		if score > 0.3 {
			score *= 0.5
		}
		return score, false, reasons
	}
	return score, true, nil
}
