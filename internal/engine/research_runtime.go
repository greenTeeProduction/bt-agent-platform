package engine

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/research"
)

type ResearchRuntimeObservation struct {
	DeliveryRun            string                          `json:"delivery_run"`
	DeliveryCommit         string                          `json:"delivery_commit"`
	SourceIDs              []string                        `json:"source_ids,omitempty"`
	TaskID                 string                          `json:"task_id"`
	RunID                  string                          `json:"run_id"`
	Task                   string                          `json:"task"`
	TreeID                 string                          `json:"tree_id"`
	TreeVersion            string                          `json:"tree_version"`
	BuildRevision          string                          `json:"build_revision"`
	StartedAt              time.Time                       `json:"started_at"`
	Outcome                evolution.Outcome               `json:"outcome"`
	ResultContractVerified bool                            `json:"result_contract_verified"`
	Publication            *evolution.RuntimeRelease       `json:"publication,omitempty"`
	Qualification          *evolution.RuntimeQualification `json:"qualification,omitempty"`
	PublicationError       string                          `json:"publication_error,omitempty"`
}

// ResearchRuntimeStatus joins authoritative terminal records to delivered code.
// It reports observed adoption and independently checked task results, without
// treating temporal correlation as a causal research-impact measurement.
func ResearchRuntimeStatus(ctx context.Context, user string, records *evolution.Store, releases *evolution.RuntimeReleaseStore) (map[string]any, error) {
	traces, err := research.OpenTraces(researchTracePath(user), user)
	if err != nil {
		return nil, err
	}
	report := traces.Summary()
	if user == "" {
		programs, err := ResearchProgramStatus(user)
		if err != nil {
			return nil, err
		}
		report["program_status"] = programs
	}
	if records == nil {
		report["runtime_evidence"] = "unavailable"
		return report, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	runs, err := records.LoadAllStrict()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(runs, func(a, b evolution.Record) int { return a.StartedAt.Compare(b.StartedAt) })
	observations := map[string][]ResearchRuntimeObservation{}
	issues := map[string]string{}
	matched := map[string]bool{}
	checked := map[string]bool{}
	qualifiedRuns, verifiedResults := 0, 0
	observedTasks := map[string]bool{}
	for goalID, goal := range traces.Goals {
		for _, delivery := range goal.Deliveries {
			for _, run := range runs {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if run.User != user || !run.ExactExecution() || !run.Build.QualifiesCodeIdentity() || run.StartedAt.Before(delivery.RecordedAt) {
					continue
				}
				key := delivery.Repository + "\x00" + delivery.Commit + "\x00" + delivery.Tree + "\x00" + run.Build.Revision + "\x00" + strings.Join(delivery.Files, "\x00")
				if !checked[key] {
					checked[key] = true
					ok, err := runningBuildContainsDelivery(ctx, delivery, run.Build.Revision)
					if err != nil {
						issues[delivery.RunID+":"+run.Build.Revision] = err.Error()
					}
					matched[key] = ok
				}
				if !matched[key] {
					continue
				}
				o := ResearchRuntimeObservation{DeliveryRun: delivery.RunID, DeliveryCommit: delivery.Commit, SourceIDs: slices.Clone(delivery.SourceIDs), TaskID: run.TaskID, RunID: run.RunID, Task: run.Task, TreeID: run.TreeName, TreeVersion: run.TreeVersion, BuildRevision: run.Build.Revision, StartedAt: run.StartedAt, Outcome: run.Outcome, ResultContractVerified: run.VerifiedFinalResult()}
				if run.Publication != nil {
					p := run.Publication
					if p.TreeID != run.TreeName || p.Version != run.TreeVersion || p.User != "" && p.User != user || p.UpdatedAt.After(run.StartedAt) {
						o.PublicationError = "execution does not match the publication snapshot"
					} else if releases == nil {
						o.PublicationError = "publication evidence store unavailable"
					} else {
						q, err := releases.PublicationQualification(p)
						if err != nil {
							o.PublicationError = err.Error()
						} else {
							o.Publication, o.Qualification = p, q
						}
					}
				}
				observations[goalID] = append(observations[goalID], o)
				if !observedTasks[run.TaskID] {
					observedTasks[run.TaskID] = true
					if o.ResultContractVerified {
						verifiedResults++
					}
					if o.Qualification != nil {
						qualifiedRuns++
					}
				}
			}
		}
	}
	report["runtime_observations"] = observations
	report["adopted_execution_count"] = len(observedTasks)
	report["verified_result_count"] = verifiedResults
	report["qualified_tree_execution_count"] = qualifiedRuns
	report["runtime_evidence"] = "inspected"
	if len(observedTasks) > 0 {
		report["runtime_adoption"] = "observed"
	}
	if len(issues) > 0 {
		report["unresolved_code_links"] = issues
	}
	// A real publication comparison proves a tree's stated task corpus. Its
	// co-occurrence with a code delivery does not prove research caused the gain.
	report["measured_impact"] = "causal_research_impact_not_established"
	return report, nil
}

func runningBuildContainsDelivery(ctx context.Context, delivery research.Delivery, revision string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	git := func(args ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = delivery.Repository
		return command.CombinedOutput()
	}
	tree, err := git("rev-parse", "--verify", delivery.Commit+"^{tree}")
	if err != nil {
		return false, fmt.Errorf("delivery commit unavailable: %w", err)
	}
	if strings.TrimSpace(string(tree)) != delivery.Tree {
		return false, fmt.Errorf("delivery Git tree identity mismatch")
	}
	if _, err := git("merge-base", "--is-ancestor", delivery.Commit, revision); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("build ancestry unavailable: %w", err)
	}
	args := append([]string{"--literal-pathspecs", "diff", "--quiet", delivery.Commit, revision, "--"}, delivery.Files...)
	if _, err := git(args...); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("delivered file comparison unavailable: %w", err)
	}
	return true, nil
}
