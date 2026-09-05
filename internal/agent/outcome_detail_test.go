package agent

import (
	"strings"
	"testing"
)

// TestOutcomeErrorDetail pins the tail-distillation contract of the exported
// OutcomeErrorDetail helper so the bt-agent scheduler closure can reuse it for
// DLQ-diagnosable outcome errors outside internal/agent.
func TestOutcomeErrorDetail(t *testing.T) {
	t.Run("empty output yields sentinel", func(t *testing.T) {
		for _, in := range []string{"", "   \n", "\n\t  "} {
			if got := OutcomeErrorDetail(in); got != "no run output" {
				t.Fatalf("OutcomeErrorDetail(%q) = %q, want %q", in, got, "no run output")
			}
		}
	})

	t.Run("short output flattens newlines", func(t *testing.T) {
		in := "line one\nline two\nline three"
		want := "line one | line two | line three"
		if got := OutcomeErrorDetail(in); got != want {
			t.Fatalf("OutcomeErrorDetail(%q) = %q, want %q", in, got, want)
		}
	})

	t.Run("long output truncated to last 400 bytes with leading ellipsis", func(t *testing.T) {
		body := strings.Repeat("a", 500)
		finalLine := "FINAL-DIAGNOSTIC-LINE"
		in := body + "\n" + finalLine

		got := OutcomeErrorDetail(in)

		if !strings.HasPrefix(got, "…") {
			t.Fatalf("expected leading ellipsis, got %q", got)
		}
		tail := strings.TrimPrefix(got, "…")
		// The distilled tail is the last 400 bytes of the trimmed input with
		// newlines flattened to " | ".
		trimmed := strings.TrimSpace(in)
		wantTail := strings.ReplaceAll(trimmed[len(trimmed)-400:], "\n", " | ")
		if tail != wantTail {
			t.Fatalf("tail = %q, want %q", tail, wantTail)
		}
		if !strings.Contains(got, finalLine) {
			t.Fatalf("expected result to contain final line %q, got %q", finalLine, got)
		}
	})
}
