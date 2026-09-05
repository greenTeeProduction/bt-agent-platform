package engine

import (
	"strings"
	"testing"
)

// The hermes-daily-updater agent's own quality gate requires the keywords
// "Hermes", "update", and "version" in the report. The up-to-date path (by
// far the most common: 0 commits behind) said "**Before**: Hermes Agent
// v0.18.2 …" — the literal word "version" never appeared, so a perfectly
// healthy run failed its quality gate every single day.
func TestHermesUpdateReportSatisfiesQualityKeywords(t *testing.T) {
	full := hermesUpdateReportHeader("Hermes Agent v0.18.2 (2026.7.7.2)") + hermesUpToDateStatus()
	lower := strings.ToLower(full)
	for _, kw := range []string{"hermes", "update", "version"} {
		if !strings.Contains(lower, kw) {
			t.Fatalf("up-to-date report missing quality keyword %q:\n%s", kw, full)
		}
	}
}
