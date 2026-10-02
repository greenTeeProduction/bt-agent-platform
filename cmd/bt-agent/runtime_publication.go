package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/agentexec"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/util"
)

func publicationStore(deps *mcpDeps) (*evolution.RuntimeReleaseStore, error) {
	if deps.treeStore == nil {
		return nil, fmt.Errorf("tree store not configured")
	}
	return evolution.NewRuntimeReleaseStore(filepath.Join(deps.treeStore.Dir(), "runtime-versions")), nil
}

// Read the exact requested authority. Unknown IDs and another user's trees
// cannot become a default-tree baseline for an evolutionary comparison.
func publicationBaseline(deps *mcpDeps, id, user string) (*evolution.SerializableNode, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\\x00") {
		return nil, fmt.Errorf("invalid tree ID")
	}
	store, err := publicationStore(deps)
	if err != nil {
		return nil, err
	}
	if agentexec.AutomationBlocked(user, id) {
		return nil, fmt.Errorf("automation is not approved")
	}
	tree, _, err := store.Resolve(id, user)
	if err != nil {
		return nil, err
	}
	if tree == nil {
		if user != "" {
			if deps.personaStore == nil {
				return nil, fmt.Errorf("personal tree store not configured")
			}
			tree, err = evolution.LoadNamedTree(deps.personaStore.Workspace(user).TreesDir(), id)
		} else {
			tree, err = deps.treeStore.LoadNamed(id)
			if err == nil && tree == nil && id == "default" {
				tree, err = deps.treeStore.Load()
			}
			if err == nil && tree == nil {
				tree = domains.LookupTreeID(id)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if tree == nil {
		return nil, fmt.Errorf("requested tree does not exist")
	}
	owner, _ := tree.Metadata["user"].(string)
	if owner != user {
		return nil, fmt.Errorf("tree owner does not match requested owner")
	}
	return tree, nil
}

// Search proposals are retained outside every runtime tree discovery directory.
// Saving one does not make it runnable or give it runtime fitness credit.
func publishEvolutionProposal(deps *mcpDeps, id, user string, base, candidate *evolution.SerializableNode, estimate float64, result map[string]any) {
	result["persisted"], result["qualified"] = false, false
	store, err := publicationStore(deps)
	if err != nil {
		result["persist_error"] = err.Error()
		return
	}
	if info := engine.ValidateTreeFull(candidate); !info.Valid() {
		result["validation_errors"] = info.Errors
		return
	}
	baseVersion, err := evolution.TreeVersion(base)
	if err != nil {
		result["persist_error"] = err.Error()
		return
	}
	version, err := evolution.TreeVersion(candidate)
	if err != nil {
		result["persist_error"] = err.Error()
		return
	}
	result["baseline_version"], result["candidate_version"] = baseVersion, version
	key := sha256.Sum256([]byte(user + "\x00" + id + "\x00" + baseVersion + "\x00" + version))
	path := filepath.Join(deps.treeStore.Dir(), "evolution-proposals", fmt.Sprintf("%x.json", key))
	proposal := map[string]any{"tree_id": id, "user": user, "baseline_version": baseVersion, "candidate_version": version, "candidate": candidate, "estimated_structural_fitness": estimate}
	if err = util.SaveJSONAtomic(path, proposal); err != nil {
		result["persist_error"] = err.Error()
		return
	}
	result["proposal_file"], result["proposal_saved"] = path, true
	q, release, err := benchmark.PublishRuntimeCandidate(context.Background(), store, id, user, base, candidate, benchmark.SuiteForTree(id))
	if q != nil {
		result["qualification_attempt"] = q
	}
	if err != nil {
		result["qualification_error"] = err.Error()
		return
	}
	before, after := q.PassCounts()
	result["persisted"], result["qualified"] = true, true
	result["tree_id"], result["release"] = id, release
	result["baseline_pass_rate"] = float64(before) / float64(len(q.Trials))
	result["candidate_pass_rate"] = float64(after) / float64(len(q.Trials))
	if user == "" && deps.kg != nil {
		// Attribute the publication we just committed. Re-reading the active
		// pointer here could credit a concurrent successor instead.
		deps.kg.RecordRun(knowledge.RunRecord{TreeID: id, Task: "qualified runtime version " + release.Version, Outcome: "evolved", Quality: evolution.AssessGovernance(candidate).Score})
		deps.kg.MarkFeedbackDirty()
		if err := deps.kg.FlushFeedback(false); err != nil {
			result["feedback_error"] = err.Error()
			slog.Warn("qualified runtime feedback save failed", "error", err)
		}
	}
}

func evolveCurrentTree(deps *mcpDeps, args json.RawMessage) *engine.ToolResult {
	result := map[string]any{"evolved": false}
	finish := func(err error) *engine.ToolResult {
		if err != nil {
			result["error"] = err.Error()
		}
		data, _ := json.Marshal(result)
		return textToolResult(string(data))
	}
	params, err := requestedEvolutionTree(deps, args)
	if err != nil {
		return finish(err)
	}
	base, err := publicationBaseline(deps, params.Tree, params.User)
	if err != nil {
		return finish(err)
	}
	if deps.refStore == nil {
		return finish(fmt.Errorf("execution evidence store not configured"))
	}
	records, err := deps.refStore.LoadAll()
	if err != nil {
		return finish(err)
	}
	version, err := evolution.TreeVersion(base)
	if err != nil {
		return finish(err)
	}
	failures := 0
	for _, record := range evolution.FilterByTreeVersion(records, params.Tree, params.User, version) {
		if record.Outcome == evolution.Failure {
			failures++
		}
	}
	result["matching_failures"], result["baseline_version"] = failures, version
	if failures < 3 {
		result["reason"] = "need three failed executions of this exact tree, owner and version"
		return finish(nil)
	}
	data, err := json.Marshal(base)
	if err != nil {
		return finish(err)
	}
	var candidate evolution.SerializableNode
	if err := json.Unmarshal(data, &candidate); err != nil {
		return finish(err)
	}
	ops := []evolution.MutationOp{}
	for _, target := range evolution.ContractRecoveryTargets(base) {
		ops = append(ops, evolution.MutationOp{Operation: "add_contract_recovery", Target: target})
	}
	if len(ops) == 0 {
		ops = []evolution.MutationOp{{Operation: "wrap_retry", Target: "AnalyzeTask"}, {Operation: "increase_retries", Target: "RetrySelfCorrect"}}
	}
	applied := evolution.ApplyMutations(&candidate, ops)
	result["proposed_mutations"] = applied
	if applied == 0 {
		result["reason"] = "no applicable recovery mutation"
		return finish(nil)
	}
	publishEvolutionProposal(deps, params.Tree, params.User, base, &candidate, evolution.AssessGovernance(&candidate).Score, result)
	result["evolved"] = result["qualified"] == true
	return finish(nil)
}

type evolutionTreeSelection struct {
	Tree string `json:"tree"`
	User string `json:"user"`
}

func requestedEvolutionTree(deps *mcpDeps, args json.RawMessage) (evolutionTreeSelection, error) {
	var params evolutionTreeSelection
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return params, err
		}
	}
	if params.Tree == "" {
		params.Tree = "default"
		if deps.bb != nil && deps.bb.TreeID != "" {
			params.Tree = deps.bb.TreeID
			if params.User == "" {
				params.User = deps.bb.User
			}
		}
	}
	return params, nil
}

func currentTreeEvidence(deps *mcpDeps, args json.RawMessage, fitness bool) *engine.ToolResult {
	finish := func(value any) *engine.ToolResult {
		data, _ := json.Marshal(value)
		return textToolResult(string(data))
	}
	params, err := requestedEvolutionTree(deps, args)
	if err != nil {
		return finish(map[string]any{"error": err.Error()})
	}
	tree, err := publicationBaseline(deps, params.Tree, params.User)
	if err != nil {
		return finish(map[string]any{"error": err.Error()})
	}
	if !fitness {
		return finish(tree)
	}
	if deps.refStore == nil {
		return finish(map[string]any{"error": "execution evidence store not configured"})
	}
	records, err := deps.refStore.LoadAll()
	if err != nil {
		return finish(map[string]any{"error": err.Error()})
	}
	version, err := evolution.TreeVersion(tree)
	if err != nil {
		return finish(map[string]any{"error": err.Error()})
	}
	records = evolution.FilterByTreeVersion(records, params.Tree, params.User, version)
	successes, failures := 0, 0
	for _, r := range records {
		if r.Outcome == evolution.Success {
			successes++
		}
		if r.Outcome == evolution.Failure {
			failures++
		}
	}
	rate := 0.0
	if len(records) > 0 {
		rate = float64(successes) / float64(len(records))
	}
	return finish(map[string]any{"tree_id": params.Tree, "user": params.User, "tree_version": version, "total_tasks": len(records), "successes": successes, "failures": failures, "success_rate": fmt.Sprintf("%.2f", rate), "node_count": evolution.CountNodes(tree)})
}
