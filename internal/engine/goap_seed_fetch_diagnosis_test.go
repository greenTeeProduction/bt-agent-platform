package engine

import (
	"strings"
	"testing"
)

// 2026-08-01: arc42-program-seeder has seeded nothing since 2026-07-18. Its own
// recorded output on the runs that actually got a slot:
//
//	No usable proposal for quality goal Q4 Personalization & Self-Growth even
//	after a feedback retry: returned no parseable PROGRAM/MILESTONEn block |
//	returned no parseable PROGRAM/MILESTONEn block.
//
// Two separate problems that message hides:
//
//  1. The same rejection is emitted whether the model returned unparseable
//     prose or returned NOTHING AT ALL. seedProgramFetchFn queries NotebookLM
//     (whose OAuth profile has been corrupt since ~07-31) and falls back to
//     Claude, returning "" when that errors too. An operator cannot tell an
//     outage from a formatting problem, which is exactly the distinction that
//     decides whether to fix a prompt or re-authenticate a CLI.
//  2. On an empty answer the retry appends "YOUR PREVIOUS PROPOSAL WAS
//     rejected — fix EXACTLY these problems" to the prompt. There was no
//     previous proposal: the retry asks a model that never answered to correct
//     a proposal that never existed, and burns the one retry doing it.

func TestFetchAcceptableGoapProgram_DistinguishesNoAnswerFromUnparseable(t *testing.T) {
	cases := []struct {
		name       string
		answer     string
		wantSubstr string
	}{
		{"model returned nothing", "", "no answer"},
		{"model returned prose", "Here are some thoughts about reliability.", "no parseable PROGRAM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := seedProgramFetchFn
			seedProgramFetchFn = func(string) string { return tc.answer }
			defer func() { seedProgramFetchFn = prev }()

			att := fetchAcceptableGoapProgram("seed prompt", nil)
			if att.Spec != nil {
				t.Fatal("no spec expected")
			}
			joined := strings.Join(att.Rejections, " | ")
			if !strings.Contains(joined, tc.wantSubstr) {
				t.Fatalf("rejection %q does not name the actual failure (want %q) — an outage and a "+
					"formatting problem need different operator responses", joined, tc.wantSubstr)
			}
		})
	}
}

// A retry after an EMPTY answer must re-ask the original prompt. Appending
// "fix EXACTLY these problems" to a model that never answered wastes the single
// retry on correcting a proposal that does not exist.
func TestFetchAcceptableGoapProgram_EmptyAnswerRetriesWithTheOriginalPrompt(t *testing.T) {
	var prompts []string
	prev := seedProgramFetchFn
	seedProgramFetchFn = func(p string) string {
		prompts = append(prompts, p)
		return ""
	}
	defer func() { seedProgramFetchFn = prev }()

	fetchAcceptableGoapProgram("seed prompt", nil)

	if len(prompts) != 2 {
		t.Fatalf("fetch count = %d, want 2 (one retry)", len(prompts))
	}
	if strings.Contains(prompts[1], "YOUR PREVIOUS PROPOSAL") {
		t.Fatalf("the retry after an empty answer asked the model to fix a proposal it never made:\n%s", prompts[1])
	}
	if prompts[1] != "seed prompt" {
		t.Fatalf("retry prompt = %q, want the original prompt verbatim", prompts[1])
	}
}

// A retry after an UNPARSEABLE answer must still carry the feedback — that path
// is the one the feedback retry was built for and must keep working.
func TestFetchAcceptableGoapProgram_UnparseableAnswerRetriesWithFeedback(t *testing.T) {
	var prompts []string
	prev := seedProgramFetchFn
	seedProgramFetchFn = func(p string) string {
		prompts = append(prompts, p)
		return "some prose without the required block"
	}
	defer func() { seedProgramFetchFn = prev }()

	fetchAcceptableGoapProgram("seed prompt", nil)

	if len(prompts) != 2 {
		t.Fatalf("fetch count = %d, want 2", len(prompts))
	}
	if !strings.Contains(prompts[1], "YOUR PREVIOUS PROPOSAL") {
		t.Fatalf("the feedback retry lost its rejection reasons:\n%s", prompts[1])
	}
}
