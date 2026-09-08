package engine

import (
	"errors"
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

// A failed `git rev-list --count HEAD..origin/main` (e.g. upstream renamed
// main) used to be swallowed: Atoi failed, behind stayed 0, and the agent
// reported "Already up to date" forever. The parse must distinguish a real
// zero from an undeterminable count.
func TestParseHermesBehindCount(t *testing.T) {
	cases := []struct {
		name      string
		out       string
		err       error
		wantCount int
		wantKnown bool
	}{
		{"real count", "388\n", nil, 388, true},
		{"real zero", "0\n", nil, 0, true},
		{"negative count", "-1\n", nil, 0, false},
		{"rev-list error", "fatal: bad revision 'HEAD..origin/main'", errors.New("exit status 128"), 0, false},
		{"empty output", "", nil, 0, false},
		{"garbage output", "not-a-number", nil, 0, false},
	}
	for _, tc := range cases {
		got := parseHermesBehindCount([]byte(tc.out), tc.err)
		if got.count != tc.wantCount || got.known != tc.wantKnown {
			t.Errorf("%s: parseHermesBehindCount(%q, %v) = {count:%d known:%v}, want {count:%d known:%v}",
				tc.name, tc.out, tc.err, got.count, got.known, tc.wantCount, tc.wantKnown)
		}
	}
}

// Only a verified zero may take the early "Already up to date" exit; an
// unknown behind count must fall through to running hermes update.
func TestHermesRepoUpToDate(t *testing.T) {
	if !hermesRepoUpToDate(hermesBehind{count: 0, known: true}) {
		t.Error("verified 0 behind should count as up to date")
	}
	if hermesRepoUpToDate(hermesBehind{count: 0, known: false}) {
		t.Error("unknown behind count must NOT count as up to date")
	}
	if hermesRepoUpToDate(hermesBehind{count: 5, known: true}) {
		t.Error("5 behind is not up to date")
	}
}

// `hermes update` exiting 0 is not proof the update applied. The verdict
// requires HEAD to have moved; a zero-exit run that left HEAD in place is a
// failure so the scheduler retries next run instead of reporting success.
func TestHermesUpdateVerdict(t *testing.T) {
	known := func(n int) hermesBehind { return hermesBehind{count: n, known: true} }
	unknown := hermesBehind{}

	cases := []struct {
		name          string
		before, after string // commits
		behindBefore  hermesBehind
		behindAfter   hermesBehind
		wantOK        bool
		wantContains  []string
	}{
		{"clean update", "abc1234", "def5678", known(221), known(0), true,
			[]string{"Updated (+221 commits)"}},
		{"exit 0 but HEAD unmoved", "abc1234", "abc1234", known(388), known(388), false,
			[]string{"HEAD did not move", "388"}},
		{"HEAD unmoved and count undeterminable", "abc1234", "abc1234", unknown, unknown, false,
			[]string{"HEAD did not move", "could not be determined"}},
		{"moved but still behind", "abc1234", "def5678", known(300), known(12), false,
			[]string{"FAILED", "still 12 behind"}},
		{"moved, post-update count unknown", "abc1234", "def5678", known(300), unknown, false,
			[]string{"FAILED", "could not re-verify"}},
		{"commits unreadable", "", "", known(300), known(0), false,
			[]string{"FAILED", "could not verify HEAD movement"}},
		{"before unreadable", "", "def5678", known(300), known(0), false,
			[]string{"FAILED", "could not verify HEAD movement"}},
		{"after unreadable", "abc1234", "", known(300), known(0), false,
			[]string{"FAILED", "could not verify HEAD movement"}},
		{"unknown before verified after", "abc1234", "def5678", unknown, known(0), true,
			[]string{"Updated"}},
		{"negative residual", "abc1234", "def5678", known(300), known(-1), false,
			[]string{"FAILED"}},
		{"git diagnostic is not a commit", "abc1234", "fatal: bad revision", known(300), known(0), false,
			[]string{"FAILED", "could not verify HEAD movement"}},
		{"whitespace is not a commit", "abc1234", " ", known(300), known(0), false,
			[]string{"FAILED", "could not verify HEAD movement"}},
	}
	for _, tc := range cases {
		status, ok := hermesUpdateVerdict(tc.before, tc.after, tc.behindBefore, tc.behindAfter)
		if ok != tc.wantOK {
			t.Errorf("%s: ok = %v, want %v (status: %s)", tc.name, ok, tc.wantOK, status)
		}
		for _, want := range tc.wantContains {
			if !strings.Contains(status, want) {
				t.Errorf("%s: status missing %q:\n%s", tc.name, want, status)
			}
		}
	}
}
