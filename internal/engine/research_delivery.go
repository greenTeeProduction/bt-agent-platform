package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

func researchTracePath(user string) string { return research.TracePath(btFusionKnowledgePath, user) }

// Preserve file scope in attribution identity. Failure budgets intentionally
// strip it, but delivery in one file must never credit a different file.
func researchGoalTraceID(line string) string {
	t := stripGoapGoalTransientNotes(line)
	t = strings.TrimSpace(strings.TrimPrefix(t, "Implement the complete, verified change for this goal:"))
	for _, prefix := range []string{"[P0]", "[P1]", "[P2]"} {
		t = strings.TrimSpace(strings.TrimPrefix(t, prefix))
	}
	t = strings.TrimSpace(strings.TrimPrefix(t, "NotebookLM research:"))
	return research.Key(t)
}

func ResearchDeliveryStatus(user string) (map[string]any, error) {
	s, err := research.OpenTraces(researchTracePath(user), user)
	if err != nil {
		return nil, err
	}
	return s.Summary(), nil
}

// A landed side effect is never retried to repair missing attribution.
func recordSuperpowersResearchDelivery(run *SuperpowersRun) {
	recordSuperpowersResearchDeliveryContext(context.Background(), run)
}

func recordSuperpowersResearchDeliveryContext(ctx context.Context, run *SuperpowersRun) {
	run.ResearchDeliveryError = ""
	run.ResearchDeliveryPending = true
	noDelivery := run.Mode == SuperpowersModeDryRun || run.ApplyStatus == "applied_no_commit" || run.ApplyStatus == "main_repo" || run.ApplyStatus == "no_changes"
	if noDelivery {
		run.ResearchDeliveryPending = false
	} else if err := recordImplementedGoalsContext(ctx, run); err != nil {
		run.ResearchDeliveryError = err.Error()
		Warn("landed code lacks complete research attribution", "run", run.ID, "err", err.Error())
	} else {
		run.ResearchDeliveryPending = false
	}
	if err := writeSuperpowersRunJSON(run); err != nil {
		run.ResearchDeliveryError += "; could not save delivery status: " + err.Error()
		Warn("research delivery status was not persisted", "run", run.ID, "err", err.Error())
	}
}

// Reconcile only explicitly journaled deliveries. Legacy source labels and
// unmarked historical runs are never silently upgraded into proof. A pending
// receipt holds new planning; repair repeats metadata reads/writes, never code.
func reconcileResearchDeliveries(ctx context.Context, runsDir, user string) error {
	entries, err := os.ReadDir(runsDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			continue
		}
		run, err := readSuperpowersRunJSON(filepath.Join(runsDir, entry.Name(), "run.json"))
		if os.IsNotExist(err) {
			continue
		} // directories may precede the first journal
		if err != nil {
			return fmt.Errorf("unreadable run journal %s: %w", entry.Name(), err)
		}
		if run.ID != entry.Name() {
			return fmt.Errorf("run journal identity mismatch: %s", entry.Name())
		}
		if run.User != user || !run.ResearchDeliveryPending {
			continue
		}
		if run.ApplyStatus != "committed" && run.ApplyStatus != "committed_unpushed" && run.ApplyStatus != "committed_pr_opened" {
			continue
		}
		recordSuperpowersResearchDeliveryContext(ctx, run)
		if run.ResearchDeliveryError != "" {
			return fmt.Errorf("run %s: %s", run.ID, run.ResearchDeliveryError)
		}
	}
	return nil
}

func recordGoapResearchSource(bb *Blackboard, goals []goapResearchGoal, source, answer string) {
	if len(goals) == 0 {
		return
	}
	err := research.UpdateTraces(context.Background(), researchTracePath(bb.User), bb.User, func(s *research.TraceStore) error {
		for _, g := range goals {
			if isActionableGoapGoal(g.Line()) {
				s.Observe(researchGoalTraceID(g.Line()), g.Goal, source, answer)
			}
		}
		return nil
	})
	if err != nil {
		setGoapState(bb, "research_trace_error", err.Error())
		Warn("research source attribution was not persisted", "err", err.Error())
	}
}

// inspectResearchDelivery uses actual Git objects rather than task status or
// source labels. The receipt establishes a verified code change, not semantic
// completion of an arbitrary goal, deployed code, or improved task outcomes.
func inspectResearchDelivery(ctx context.Context, run *SuperpowersRun) (research.Delivery, error) {
	d := research.Delivery{}
	if run == nil || run.Mode != SuperpowersModeApply || (run.ApplyStatus != "committed" && run.ApplyStatus != "committed_unpushed" && run.ApplyStatus != "committed_pr_opened") {
		return d, fmt.Errorf("research delivery requires a committed apply run")
	}
	commit := strings.TrimSpace(run.AppliedCommit)
	if run.ID == "" || run.RepoDir == "" || len(commit) < 7 || len(commit) > 64 || strings.Trim(commit, "0123456789abcdef") != "" {
		return d, fmt.Errorf("research delivery is missing a valid run or commit identity")
	}
	latest := map[string]VerificationCheck{}
	for _, check := range run.Verification {
		latest[check.Name] = check
	}
	for _, name := range []string{"main-focused-tests", "main-build"} {
		if !latest[name].Passed {
			return d, fmt.Errorf("research delivery lacks passing %s verification", name)
		}
	}
	var names []string
	for name, check := range latest {
		if !check.Passed || check.Command == "" {
			return d, fmt.Errorf("research delivery has unresolved verification %s", name)
		}
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		check := latest[name]
		d.Checks = append(d.Checks, research.DeliveryCheck{Name: name, Command: check.Command, OutputHash: fmt.Sprintf("%x", sha256.Sum256([]byte(check.Output)))})
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	git := func(args ...string) (string, error) {
		res := (execCommandRunner{}).Run(ctx, run.RepoDir, "git", args...)
		if res.Err != nil {
			return "", fmt.Errorf("research delivery git %s: %w", args[0], res.Err)
		}
		return strings.TrimSpace(res.Output), nil
	}
	resolved, err := git("rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return d, err
	}
	bare, err := git("rev-parse", "--is-bare-repository")
	if err != nil {
		return d, err
	}
	ref := "HEAD"
	if bare == "true" {
		ref = "refs/heads/master"
	}
	if _, err := git("merge-base", "--is-ancestor", resolved, ref); err != nil {
		return d, fmt.Errorf("research commit is not landed: %w", err)
	}
	subject, err := git("show", "-s", "--format=%s", resolved)
	if err != nil {
		return d, err
	}
	if subject != "superpowers: apply verified run "+run.ID {
		return d, fmt.Errorf("research commit does not belong to run %s", run.ID)
	}
	tree, err := git("rev-parse", resolved+"^{tree}")
	if err != nil {
		return d, err
	}
	files, err := git("diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "-z", resolved)
	if err != nil {
		return d, err
	}
	d.RunID, d.Commit, d.Tree = run.ID, resolved, tree
	d.StartedAt = run.StartedAt
	d.Repository, err = filepath.Abs(run.RepoDir)
	if err != nil {
		return d, err
	}
	if canonical, err := filepath.EvalSymlinks(d.Repository); err == nil {
		d.Repository = canonical
	}
	for file := range strings.SplitSeq(files, "\x00") {
		if file != "" {
			d.Files = append(d.Files, file)
		}
	}
	slices.Sort(d.Files)
	d.RecordedAt = time.Now().UTC()
	return d, d.Validate()
}

func recordImplementedGoals(run *SuperpowersRun) error {
	return recordImplementedGoalsContext(context.Background(), run)
}

func recordImplementedGoalsContext(ctx context.Context, run *SuperpowersRun) error {
	delivery, err := inspectResearchDelivery(ctx, run)
	if err != nil {
		return err
	}
	var recorded []string
	err = research.UpdateTraces(ctx, researchTracePath(run.User), run.User, func(s *research.TraceStore) error {
		for _, task := range run.Tasks {
			if task.Status != "done" && task.Status != "completed" {
				continue
			}
			scope := append(slices.Clone(task.Files), task.Tests...)
			scope = append(scope, extractGoFilePaths(stripGoapGoalTransientNotes(task.Objective))...)
			var changed []string
			for _, file := range delivery.Files {
				if slices.Contains(scope, file) {
					changed = append(changed, file)
				}
			}
			if len(changed) == 0 {
				continue
			}
			d := delivery
			d.TaskIndex, d.Files = task.Index, changed
			key := researchGoalTraceID(task.Objective)
			if err := s.RecordDelivery(key, stripGoapGoalTransientNotes(task.Title), d); err != nil {
				return err
			}
			recorded = append(recorded, goapResearchGoalKey(task.Objective))
		}
		if len(recorded) == 0 {
			return fmt.Errorf("landed commit contains no completed task's declared files")
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Personal runs do not mutate the shared framework failure budget.
	if run.User != "" {
		return nil
	}
	// The delivery ledger is authoritative even if legacy budget cleanup fails.
	budget, err := research.OpenGoalAttempts(goapGoalAttemptsPath)
	if err != nil {
		return err
	}
	changed := false
	for _, key := range recorded {
		if budget.Clear(key) {
			changed = true
		}
	}
	if changed {
		return budget.Save()
	}
	return nil
}
