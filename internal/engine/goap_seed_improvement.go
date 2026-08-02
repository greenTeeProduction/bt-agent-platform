package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Continuous-seeding hardening (2026-07-10): the fleet idled for hours because
// both seeders rejected proposals ALL-OR-NOTHING (a 5-milestone arc42 program
// was discarded over 1 malformed milestone), retried only on the next 4h
// schedule / next cycle with the SAME blind prompt, and the loop had no
// deterministic floor when research produced nothing (nlm outage → 15 silent
// idle-seed attempts, zero programs). This file adds: tolerant validation
// (drop bad milestones, keep the proposal when ≥2 valid remain), ONE in-run
// retry carrying the concrete rejection reasons, and a deterministic
// coverage-backlog fallback so an idle fleet ALWAYS gets grounded Q1 work.

// goapSeedValidation partitions a proposal's milestones.
type goapSeedValidation struct {
	Valid      []string
	Malformed  []string
	Ungrounded []string
}

// acceptable reports whether enough valid milestones remain to seed — the
// program store's own minimum of 2.
func (v goapSeedValidation) acceptable() bool { return len(v.Valid) >= 2 }

// dropped returns the pruned milestones (malformed + ungrounded).
func (v goapSeedValidation) dropped() []string {
	return append(append([]string{}, v.Malformed...), v.Ungrounded...)
}

// rejectionReasons renders per-milestone problems for the feedback retry.
func (v goapSeedValidation) rejectionReasons() []string {
	out := make([]string, 0, len(v.Malformed)+len(v.Ungrounded))
	for _, m := range v.Malformed {
		out = append(out, "malformed (needs an imperative, non-prose step naming a Go file): "+truncateGoap(m, 140))
	}
	for _, m := range v.Ungrounded {
		out = append(out, "ungrounded (its Go files do not exist at HEAD — name EXISTING production files): "+truncateGoap(m, 140))
	}
	return out
}

// validateGoapProgramMilestones partitions milestones into valid / malformed /
// ungrounded instead of judging the whole proposal at once.
func validateGoapProgramMilestones(milestones []string) goapSeedValidation {
	var v goapSeedValidation
	for _, m := range milestones {
		switch {
		case !isValidProgramMilestone(m):
			v.Malformed = append(v.Malformed, m)
		case !milestoneTouchesExistingFile(m):
			v.Ungrounded = append(v.Ungrounded, m)
		default:
			v.Valid = append(v.Valid, m)
		}
	}
	return v
}

// goapSeedAttempt is the outcome of fetchAcceptableGoapProgram.
type goapSeedAttempt struct {
	Spec       *goapProgramSpec // accepted spec (pruned to its valid milestones), or nil
	Dropped    []string         // milestones pruned from the accepted spec
	Rejections []string         // human-readable reasons from failed attempts
	Fetches    int
}

// fetchAcceptableGoapProgram fetches a proposal via seedProgramFetchFn and,
// when the first one is unusable, retries EXACTLY ONCE with the concrete
// rejection reasons appended to the prompt — a fixable proposal no longer
// costs a 4h schedule slot or a full cycle. gate (nil ok) lets the arc42
// seeder add its goal-naming requirement; it returns "" when satisfied or the
// feedback line when not. An accepted spec is pruned to its valid milestones.
func fetchAcceptableGoapProgram(prompt string, gate func(*goapProgramSpec) string) goapSeedAttempt {
	att := goapSeedAttempt{}
	current := prompt
	for try := 0; try < 2; try++ {
		att.Fetches++
		answer := seedProgramFetchFn(current)
		spec := extractGoapProgram(answer)
		if strings.TrimSpace(answer) == "" {
			// No answer at all is an OUTAGE, not a formatting problem — the
			// NotebookLM query failed and the Claude fallback errored too. Say
			// so: from 2026-07-18 the arc42 seeder reported "no parseable
			// PROGRAM/MILESTONEn block" for both, and an operator could not tell
			// "re-authenticate nlm" from "fix the prompt". And do NOT append the
			// feedback block below — asking a model that never answered to
			// correct a proposal it never made just burns the single retry.
			att.Rejections = append(att.Rejections, "proposal source returned no answer (NotebookLM query and Claude fallback both produced nothing)")
			continue
		}
		if spec == nil {
			att.Rejections = append(att.Rejections, "returned no parseable PROGRAM/MILESTONEn block")
		} else {
			v := validateGoapProgramMilestones(spec.Milestones)
			gateMsg := ""
			if gate != nil {
				gateMsg = gate(spec)
			}
			if v.acceptable() && gateMsg == "" {
				spec.Milestones = v.Valid
				att.Spec = spec
				att.Dropped = v.dropped()
				return att
			}
			reasons := v.rejectionReasons()
			if gateMsg != "" {
				reasons = append(reasons, gateMsg)
			}
			if !v.acceptable() {
				reasons = append(reasons, fmt.Sprintf("only %d valid milestone(s) remain; at least 2 are required", len(v.Valid)))
			}
			att.Rejections = append(att.Rejections, fmt.Sprintf("proposal %q rejected: %s", spec.Title, strings.Join(reasons, "; ")))
		}
		current = prompt + "\n\nYOUR PREVIOUS PROPOSAL WAS rejected — fix EXACTLY these problems and return a corrected PROGRAM/MILESTONEn block:\n- " + strings.Join(att.Rejections, "\n- ")
	}
	return att
}

// goapFusionListRepoGoFilesFn lists repo-relative files under internal/ and
// cmd/ at HEAD of the (bare) main repo. Var for test override.
var goapFusionListRepoGoFilesFn = func() []string {
	out, err := runGoapShellTimeout("git ls-tree -r --name-only HEAD -- internal cmd", 15*time.Second)
	if err != nil {
		return nil
	}
	return splitNonEmptyLines(out)
}

// untestedProductionGoFiles returns production .go files (under internal/ or
// cmd/) lacking an exact sibling _test.go, sorted — the deterministic,
// endless-until-done Q1 backlog.
func untestedProductionGoFiles(all []string) []string {
	set := make(map[string]bool, len(all))
	for _, f := range all {
		set[f] = true
	}
	var out []string
	for _, f := range all {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		if !strings.HasPrefix(f, "internal/") && !strings.HasPrefix(f, "cmd/") {
			continue
		}
		if set[strings.TrimSuffix(f, ".go")+"_test.go"] {
			continue
		}
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// goapCoverageFallbackMilestones bounds a deterministic coverage program.
const goapCoverageFallbackMilestones = 3

// buildUntestedFilesProgramSpec synthesizes a grounded Q1 coverage program
// from untested production files, skipping files already mentioned by ANY
// existing program (existingPrograms carries all titles + milestone texts) so
// successive fallbacks walk the codebase instead of repeating. Returns nil
// when fewer than 2 unclaimed untested files remain — the backlog is done.
func buildUntestedFilesProgramSpec(existingPrograms []string, files []string) *goapProgramSpec {
	claimed := strings.Join(existingPrograms, "\n")
	var picked []string
	for _, f := range untestedProductionGoFiles(files) {
		if strings.Contains(claimed, f) {
			continue
		}
		picked = append(picked, f)
		if len(picked) == goapCoverageFallbackMilestones {
			break
		}
	}
	if len(picked) < 2 {
		return nil
	}
	spec := &goapProgramSpec{
		Title: fmt.Sprintf("Deterministic coverage backlog: characterization tests for %s and %d more (Q1 Correctness)", picked[0], len(picked)-1),
	}
	for _, f := range picked {
		spec.Milestones = append(spec.Milestones, fmt.Sprintf(
			"Add characterization tests pinning the current exported behavior of %s in sibling %s_test.go — table-driven where natural; no production changes unless a test exposes a real bug, then fix it minimally. (files: %s)",
			f, strings.TrimSuffix(f, ".go"), f))
	}
	return spec
}

// goapFusionSeedSection renders the cycle's seeding outcome for the fusion
// analysis note. Seed reports used to vanish when later stages rewrote
// bb.Result — 15 idle-seed attempts ran invisibly on 2026-07-10, discoverable
// only via selector telemetry.
func goapFusionSeedSection(bb *Blackboard) string {
	outcome, _ := bb.ChainState["goap_fusion_seed_outcome"].(string)
	if strings.TrimSpace(outcome) == "" {
		return ""
	}
	return fmt.Sprintf("\n## Backlog Seeding\n%s\n", outcome)
}
