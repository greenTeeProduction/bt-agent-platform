package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// QualificationCases binds fixed factory tasks to their declared oracle. Other
// suites must supply independently specified expected values for every task.
// Prose length, historical success, and routing alone cannot publish a version.
func QualificationCases(tree *evolution.SerializableNode, suite Suite) ([]TaskCase, []evolution.ResultContract, error) {
	if kind, _ := tree.Metadata["factory_kind"].(string); kind == "response" {
		task, _ := tree.Metadata["task"].(string)
		if tree.Type != "Sequence" || len(tree.Children) == 0 || task == "" {
			return nil, nil, fmt.Errorf("factory task definition missing")
		}
		last := &tree.Children[len(tree.Children)-1]
		contract, err := evolution.ParseResultContract(last)
		if err != nil || last.Type != "QualityGate" || contract == nil || len(contract.JSONFields) == 0 {
			return nil, nil, fmt.Errorf("factory promotion requires explicit expected result values")
		}
		return []TaskCase{{Task: task, ShouldSucceed: true}}, []evolution.ResultContract{*contract}, nil
	}
	if len(suite.Tasks) == 0 {
		return nil, nil, fmt.Errorf("task-specific qualification corpus is missing")
	}
	contracts := make([]evolution.ResultContract, len(suite.Tasks))
	for i, task := range suite.Tasks {
		if !task.ShouldSucceed || task.ShouldReject || len(task.ExpectedJSON) == 0 {
			return nil, nil, fmt.Errorf("task %d needs an independent result-value contract and isolated task fixture", i+1)
		}
		contracts[i] = evolution.ResultContract{JSONFields: make(map[string]json.RawMessage), MinLength: task.MinResultLen}
		for key, value := range task.ExpectedJSON {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, nil, err
			}
			contracts[i].JSONFields[key] = encoded
		}
	}
	return suite.Tasks, contracts, nil
}

// QualifyRuntimeCandidate measures both definitions on identical declared tasks.
// It never projects predecessor history onto the candidate. A backend switch
// discards the mixed comparison and restarts once on the settled provider.
func QualifyRuntimeCandidate(ctx context.Context, id, user string, base, candidate *evolution.SerializableNode, suite Suite, model *LiveModel) (*evolution.RuntimeQualification, error) {
	if model == nil || base == nil || candidate == nil {
		return nil, fmt.Errorf("real model and both definitions required")
	}
	if !evolution.PreservesGovernance(base, candidate) {
		return nil, fmt.Errorf("candidate removes task capabilities or governing contracts")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tasks, contracts, err := QualificationCases(base, suite)
	if err != nil {
		return nil, err
	}
	bv, err := evolution.TreeVersion(base)
	if err != nil {
		return nil, err
	}
	cv, err := evolution.TreeVersion(candidate)
	if err != nil {
		return nil, err
	}
	if bv == cv {
		return nil, fmt.Errorf("candidate is unchanged")
	}
	for range 2 {
		initial := model.Evidence()
		q := &evolution.RuntimeQualification{TreeID: id, User: user, BaselineVersion: bv, CandidateVersion: cv, MeasuredAt: time.Now().UTC()}
		for range 3 {
			for i, task := range tasks {
				before, beforeCalls, err := measureTask(ctx, id, user, base, bv, task.Task, model)
				if err != nil {
					return q, err
				}
				after, afterCalls, err := measureTask(ctx, id, user, candidate, cv, task.Task, model)
				if err != nil {
					return q, err
				}
				q.BaselineCalls += beforeCalls
				q.CandidateCalls += afterCalls
				q.Trials = append(q.Trials, evolution.TaskTrial{Task: task.Task, Contract: contracts[i], BeforeOutput: before.Output, AfterOutput: after.Output, BeforeOutcome: before.Outcome, AfterOutcome: after.Outcome, BeforeDurationMs: before.DurationMs, AfterDurationMs: after.DurationMs})
			}
		}
		final := model.Evidence()
		if final.Fallbacks != initial.Fallbacks {
			continue
		}
		q.Backend, q.Model = final.Backend, final.Model
		return q, q.Validate(base, candidate)
	}
	return nil, fmt.Errorf("provider changed during both qualification attempts")
}

func measureTask(ctx context.Context, id, user string, source *evolution.SerializableNode, version, task string, model *LiveModel) (Result, int, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, 0, err
	}
	// Clone to isolate build-time expansion, telemetry optimizers and runtime
	// state. Publication retains exactly the bytes this trial identifies.
	data, err := json.Marshal(source)
	if err != nil {
		return Result{}, 0, err
	}
	var tree evolution.SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return Result{}, 0, err
	}
	bb := &engine.Blackboard{TreeID: id, User: user, Task: task, LLM: model, TraceContext: ctx, NodeAdmission: benchmarkAdmission}
	command, err := engine.BuildAndValidate(&tree, bb)
	if err != nil {
		return Result{}, 0, err
	}
	before := model.Evidence()
	output := engine.RunTask(bb, command)
	after := model.Evidence()
	if err := ctx.Err(); err != nil {
		return Result{}, 0, err
	}
	versions := bb.EvidenceExecutionVersions()
	if bb.EvidenceTreeVersion() != version || len(versions) != 1 || versions[0] != version {
		return Result{}, 0, fmt.Errorf("qualification requires an unchanged, fully resolved definition")
	}
	if after.Errors != before.Errors || after.Calls <= before.Calls {
		return Result{}, 0, fmt.Errorf("trial lacks successful real-model inference")
	}
	if benchmarkOutcome(bb) == "benchmark_unsupported" {
		return Result{}, 0, fmt.Errorf("trial lacks an isolated capability fixture")
	}
	return Result{Task: task, Output: output, Outcome: bb.Outcome, DurationMs: bb.DurationMs, QualityScore: bb.QualityScore}, after.Calls - before.Calls, nil
}
