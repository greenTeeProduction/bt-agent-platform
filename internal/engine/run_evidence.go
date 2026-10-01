package engine

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/util"
	btcore "github.com/rvitorper/go-bt/core"
)

type treeDefinition struct {
	id, version, expandedVersion string
	publication                  *evolution.RuntimeRelease
}
type definitionCommand struct {
	inner      btcore.Command[Blackboard]
	definition treeDefinition
	owner      string
}

func (c *definitionCommand) Run(ctx *btcore.BTContext[Blackboard]) int {
	if c.owner != "" && ctx.Blackboard.User != c.owner {
		ctx.Blackboard.Outcome = "failure"
		ctx.Blackboard.Result = "tree execution owner mismatch"
		return -1
	}
	return c.inner.Run(ctx)
}

func bindTreeDefinition(command btcore.Command[Blackboard], source, expanded *evolution.SerializableNode, treeID string) (btcore.Command[Blackboard], error) {
	version, err := evolution.TreeVersion(source)
	if err != nil {
		return nil, err
	}
	expandedVersion, err := evolution.TreeVersion(expanded)
	if err != nil {
		return nil, err
	}
	if treeID == "" {
		treeID = source.Name
	}
	publication, err := source.RuntimePublication()
	if err != nil {
		return nil, err
	}
	if publication != nil && publication.TreeID != treeID {
		return nil, fmt.Errorf("runtime publication tree identity mismatch")
	}
	owner, _ := source.Metadata["user"].(string)
	return &definitionCommand{inner: command, definition: treeDefinition{treeID, version, expandedVersion, publication}, owner: owner}, nil
}

// Shared only to collect a reflection request from parallel branches. Final
// outcome and persistence belong to the run owner, after all gates finish.
type runEvidence struct {
	mu            sync.Mutex
	id            string
	started       time.Time
	definition    treeDefinition
	versions      []string
	checks        []evolution.ResultCheck
	effects       []evolution.EffectReceipt
	checksDropped int
	reflect       bool
	finalized     bool
	err           error
}

// EvidenceExecutionVersions returns a detached list of definitions actually
// executed during this run. Qualification rejects mixed or unpinned versions.
func (bb *Blackboard) EvidenceExecutionVersions() []string {
	if bb.runEvidence == nil {
		return nil
	}
	bb.runEvidence.mu.Lock()
	defer bb.runEvidence.mu.Unlock()
	return append([]string(nil), bb.runEvidence.versions...)
}

func beginRunEvidence(bb *Blackboard, command btcore.Command[Blackboard], started time.Time) {
	bb.contractValidatedResult = ""
	token := make([]byte, 16)
	// crypto/rand.Read fills the buffer or terminates on an unrecoverable failure.
	_, _ = rand.Read(token)
	e := &runEvidence{id: fmt.Sprintf("run-%x", token), started: started}
	if bound, ok := command.(*definitionCommand); ok {
		e.definition = bound.definition
		e.versions = []string{bound.definition.expandedVersion}
	} else {
		e.definition.id = bb.TreeID
	}
	bb.runEvidence = e
	bb.EvidenceError = nil
}

func (bb *Blackboard) requestOutcomeReflection() {
	if bb.runEvidence == nil {
		return
	}
	bb.runEvidence.mu.Lock()
	bb.runEvidence.reflect = true
	bb.runEvidence.mu.Unlock()
}

func (bb *Blackboard) recordExecutionVersion(tree *evolution.SerializableNode) {
	if bb.runEvidence == nil {
		return
	}
	version, err := evolution.TreeVersion(tree)
	if err != nil {
		return
	} // mutated trees have already passed JSON validation
	e := bb.runEvidence
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.versions) == 0 || e.versions[len(e.versions)-1] != version {
		e.versions = append(e.versions, version)
	}
}

// FinalizeRunEvidence persists one final record, including outer quality gates
// when a caller sets DeferRunEvidence. Repeated calls never replay inference or
// retry a completed task. Persistence failure leaves the task outcome intact.
func FinalizeRunEvidence(bb *Blackboard, diagnostics ...error) error {
	if bb == nil || bb.runEvidence == nil {
		return nil
	}
	e := bb.runEvidence
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.finalized {
		return e.err
	}
	e.finalized = true
	if bb.Reflections == nil || bb.Sandbox {
		return nil
	}
	record := &evolution.Record{
		Build: util.CurrentBuildProvenance(), StartedAt: e.started, Publication: e.definition.publication,
		TaskID: e.id, RunID: bb.RunID, TreeName: e.definition.id,
		TreeVersion: e.definition.version, ExecutionVersions: append([]string(nil), e.versions...),
		EvidenceKind: evolution.EvidenceExecution, User: bb.User,
		ResultChecks:        append([]evolution.ResultCheck(nil), e.checks...),
		ResultChecksDropped: e.checksDropped,
		Effects:             append([]evolution.EffectReceipt(nil), e.effects...),
		Task:                bb.Task, Plan: bb.Plan, Result: bb.Result,
		Outcome: evolution.Outcome(bb.Outcome), QualityScore: bb.QualityScore,
		Path: bb.CurrentPath,
	}
	if record.RunID == "" {
		record.RunID = e.id
	}
	if diagnostic := errors.Join(append(diagnostics, bb.ExecutionError())...); diagnostic != nil {
		record.Error = diagnostic.Error()
	}
	if e.reflect && bb.LLM != nil {
		record.WhatWentWell, record.WhatToImprove = terminalReflection(bb)
	}
	bb.DurationMs = time.Since(e.started).Milliseconds()
	record.DurationMs = bb.DurationMs
	e.err = bb.Reflections.Save(record)
	bb.EvidenceError = e.err
	if e.err != nil {
		bb.Log().Error("terminal run evidence not saved", "tree", record.TreeName, "run_id", record.RunID, "error", e.err)
	}
	return e.err
}

func terminalReflection(bb *Blackboard) (well, improve []string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			improve = []string{fmt.Sprintf("terminal reflection failed: %v", recovered)}
		}
	}()
	good, bad := bb.LLM.Reflect(bb.Task, bb.Outcome, bb.Plan+"\nResult:\n"+bb.Result)
	return []string{good}, []string{bad}
}

// EvidenceTreeID returns the identity bound to the command that actually ran.
func (bb *Blackboard) EvidenceTreeID() string {
	if bb.runEvidence != nil {
		return bb.runEvidence.definition.id
	}
	return bb.TreeID
}

// EvidenceTreeVersion returns the source definition fingerprint for this run.
func (bb *Blackboard) EvidenceTreeVersion() string {
	if bb.runEvidence != nil {
		return bb.runEvidence.definition.version
	}
	return ""
}
