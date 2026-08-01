package engine

// goap_red_evidence.go — what counts as EVIDENCE about a milestone's red-pass.
//
// Two consumers ask the same question from different points in a cycle:
// precheckGoapStaleMilestones (charge time) and handleGoapRedPassCycleFailure
// (after a cycle stopped on a red-pass). Both infer "the RED command passed, so
// the work already landed" — an inference that is only valid when the RED
// command can DISCRIMINATE. The primitives here make the two ways that
// inference breaks explicit and, above all, keep "provably absent" and "could
// not tell" apart. Collapsing the second into the first is the
// unknown-means-failed inversion that treadmilled a milestone for ~40h.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

// goapDeliverableVerdict classifies a milestone goal's named _test.go
// deliverables against HEAD.
type goapDeliverableVerdict int

const (
	// goapDeliverablesSatisfied: the goal names no _test.go deliverable, or
	// every one it names is present at HEAD. A goal that names none is
	// deliberately unaffected — a "fix the bug in Y.go" plan writes its own
	// failing regression test, so there a red-pass really is evidence the fix
	// already landed, which is the case red-evidence completion exists for.
	goapDeliverablesSatisfied goapDeliverableVerdict = iota
	// goapDeliverablesMissing: at least one named deliverable is provably absent.
	goapDeliverablesMissing
	// goapDeliverablesUnknown: at least one probe could not determine the answer.
	goapDeliverablesUnknown
)

// goapRepoFileShellFn runs one HEAD existence probe. Var for test override.
var goapRepoFileShellFn = func(command string) (string, error) {
	return runGoapShellTimeout(command, 10*time.Second)
}

// goapFusionRepoFileStateFn is the tri-state probe. Var for test override.
var goapFusionRepoFileStateFn = goapFusionRepoFileState

// goapFusionRepoFileState reports whether relPath exists at HEAD of the (bare)
// main repo AND whether the probe could determine that at all.
//
// It uses `git rev-parse --verify --quiet HEAD:<path>`, NOT `git cat-file -e`.
// cat-file looks the wrong way for this question: a `HEAD:<path>` rev that does
// not resolve is a revision-parse fatal, so an absent file exits 128 — the same
// code a real git error produces — while its exit 1 is reserved for a
// well-formed object NAME missing from the object DB. Verified on git 2.25.1:
//
//	git cat-file -e HEAD:internal/engine/absent_test.go -> 128 (absent)
//	git cat-file -e 000...0                             -> 1
//	git rev-parse --verify --quiet HEAD:<absent>        -> 1, silent
//	git rev-parse --verify --quiet HEAD:<present>       -> 0
//
// rev-parse --quiet gives the clean 1/0 this needs, leaving every other outcome
// — a git error, the timeout, a shell that never ran — as "we do not know".
// TestGoapFusionRepoFileState_AgainstRealGit pins that against the real binary;
// a stub can only ever confirm the assumption it encodes.
func goapFusionRepoFileState(relPath string) (exists bool, determined bool) {
	_, err := goapRepoFileShellFn(fmt.Sprintf("git rev-parse --verify --quiet HEAD:%s", relPath))
	if err == nil {
		return true, true
	}
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) && coded.ExitCode() == 1 {
		return false, true
	}
	return false, false
}

// goapRedPassDeliverableVerdict probes every _test.go path the goal names.
// "Missing" wins over "unknown": one provably-absent deliverable is enough to
// know the RED command did not discriminate, whatever the other probes did.
func goapRedPassDeliverableVerdict(goal string) goapDeliverableVerdict {
	verdict := goapDeliverablesSatisfied
	for _, f := range extractGoFilePaths(goal) {
		if !strings.HasSuffix(f, "_test.go") {
			continue
		}
		switch exists, determined := goapFusionRepoFileStateFn(f); {
		case !determined:
			verdict = goapDeliverablesUnknown
		case !exists:
			return goapDeliverablesMissing
		}
	}
	return verdict
}

// goapMilestoneGoalText reads the goal text of programID:idx. Deliberately
// unlocked: the caller only needs it to choose which probe to shell, and the
// authoritative state change is re-taken under the flock afterwards. An
// unreadable store yields "", i.e. no named deliverable, i.e. the pre-existing
// behavior.
func goapMilestoneGoalText(programID string, idx int) string {
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return ""
	}
	for _, p := range ps.Programs {
		if p.ID != programID {
			continue
		}
		if idx < 0 || idx >= len(p.Milestones) {
			return ""
		}
		return p.Milestones[idx].Goal
	}
	return ""
}

// goapRedRunProducedVerdict reports whether a FAILED pre-check RED run actually
// produced a test verdict. Only an error carrying a process exit code means the
// command ran to completion and failed. A wrapped context deadline (the
// goapRedPrecheckTimeout bound), a shell that could not start, and the
// under-test unavailability sentinel all mean "we do not know".
//
// Measured 2026-08-01: without this, every verdict-less run was read as "the
// predicted regression exists" and destroyed the milestone's evidence — which
// is one half of why goapRedPassCompleteStreak was unreachable in production.
func goapRedRunProducedVerdict(err error) bool {
	if err == nil ||
		errors.Is(err, errGoapRedPrecheckUnavailable) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var coded interface{ ExitCode() int }
	return errors.As(err, &coded)
}
