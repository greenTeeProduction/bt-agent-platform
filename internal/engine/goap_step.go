package engine

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

type goapCapabilityObservation struct {
	scope, origin string
	facts         map[string]json.RawMessage
}

// ObserveGoapFacts is for trusted capability adapters after inspecting their
// actual effects. Model text and public ChainState cannot create these receipts.
func (b *Blackboard) ObserveGoapFacts(origin string, facts map[string]any) error {
	if b.goapEffectScope == "" || strings.TrimSpace(origin) == "" {
		return fmt.Errorf("GOAP observation requires an executing step and a capability origin")
	}
	data, err := json.Marshal(facts)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(b.goapObserved) >= maxChildTicks {
		return fmt.Errorf("GOAP capability observation budget exhausted")
	}
	b.goapObserved = append(slices.Clone(b.goapObserved), goapCapabilityObservation{scope: b.goapEffectScope, origin: origin, facts: fields})
	return nil
}

func goapResultFields(result string) (map[string]json.RawMessage, error) {
	text := strings.TrimSpace(result)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "```"))
	}
	var fields map[string]json.RawMessage
	err := json.Unmarshal([]byte(text), &fields)
	if err == nil && fields == nil {
		err = fmt.Errorf("GOAP result must be a JSON object")
	}
	return fields, err
}

func observedGoapFields(b *Blackboard, source, scope string) (map[string]json.RawMessage, string, error) {
	switch source {
	case "result":
		fields, err := goapResultFields(resolvedResult(b))
		return fields, "value-checked result", err
	case "file_task":
		var observed *evolution.EffectReceipt
		for _, receipt := range b.EvidenceEffects() {
			if receipt.Scope != scope {
				continue
			}
			if observed != nil || receipt.Kind != "file" || !receipt.WriteCommitted || !receipt.Verified || receipt.Error != "" {
				return nil, "", fmt.Errorf("GOAP file observer requires exactly one successful write/readback receipt")
			}
			snapshot := receipt
			observed = &snapshot
		}
		if observed != nil {
			data, err := json.Marshal(observed)
			if err != nil {
				return nil, "", err
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				return nil, "", err
			}
			return fields, "owned artifact readback", nil
		}

	case "capability":
		for _, observation := range slices.Backward(b.goapObserved) {
			if observation.scope == scope {
				return observation.facts, observation.origin, nil
			}
		}
	}
	return nil, "", fmt.Errorf("GOAP step has no fresh %s observation", source)
}

func buildGoapStep(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	spec, parseErr := evolution.ParseGoapStep(node)
	var child btcore.Command[Blackboard]
	if parseErr == nil {
		child = buildNode(&node.Children[0], bb, node.Name)
	}
	var owner *Blackboard
	var runID, scope string
	terminal := 0
	prepared := false
	resultStart := 0
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		currentRun := ""
		if b.runEvidence != nil {
			currentRun = b.runEvidence.id
		}
		if b != owner || runID != currentRun {
			owner, runID = b, currentRun
			terminal, prepared = 0, false
			scope = rand.Text()
		}
		if b.applyExecutionStop() {
			return -1
		}
		if terminal != 0 {
			return terminal
		}
		fail := func(err error) int {
			terminal = -1
			b.Outcome = "failure"
			if b.ChainState == nil {
				b.ChainState = map[string]any{}
			}
			b.ChainState["goap_effect_error"] = err.Error()
			for _, effect := range b.EvidenceEffects() {
				if effect.Scope == scope && effect.WriteCommitted {
					b.stopExecution(err.Error(), &reliability.ExecutionUncertainError{Err: err})
					b.applyExecutionStop()
					break
				}
			}
			return terminal
		}
		if parseErr != nil {
			return fail(parseErr)
		}
		if !prepared {
			if spec.Source == "capability" && node.Children[0].Type == "ChainAction" && strings.HasPrefix(node.Children[0].Name, "llm_call:") {
				return fail(fmt.Errorf("GOAP model step requires an explicit result-value or file-task observer"))
			}
			data, err := json.Marshal(goapWorldStateFrom(b))
			if err != nil {
				return fail(err)
			}
			if err = (&evolution.ResultContract{JSONFields: spec.Preconditions}).Verify(string(data)); err != nil {
				return fail(fmt.Errorf("GOAP preconditions: %w", err))
			}
			if spec.Source == "result" {
				resultStart = len(b.Results)
				b.Result, b.CachedResult = "", ""
				b.contractValidatedResult = ""
			}
			prepared = true
		}
		status := func() int {
			previousScope := b.goapEffectScope
			b.goapEffectScope = scope
			defer func() { b.goapEffectScope = previousScope }()
			return child.Run(ctx)
		}()
		if b.applyExecutionStop() {
			return -1
		}
		if status == 0 {
			return 0
		}
		fields, origin, err := observedGoapFields(b, spec.Source, scope)
		if spec.Source == "result" && b.Result == "" && len(b.Results) <= resultStart {
			err = fmt.Errorf("GOAP step produced no fresh result")
		}
		check := evolution.GoapCheck{Step: node.Name, Scope: scope, Source: spec.Source, Origin: origin, Expected: spec.Effects, Observed: map[string]json.RawMessage{}}
		for key, field := range spec.Bindings {
			if value, ok := fields[field]; ok {
				check.Observed[key] = value
			}
		}
		if status != 1 {
			err = fmt.Errorf("GOAP executor failed with status %d", status)
		}
		if err == nil {
			err = check.Verify()
		}
		check.Passed = err == nil
		if err != nil {
			check.Reason = err.Error()
		}
		if spec.Source == "result" {
			check.OutputDigest = artifactDigest([]byte(resolvedResult(b)))
		}
		b.recordGoapCheck(check)
		if err != nil {
			return fail(err)
		}
		if spec.Source == "result" {
			contract := &evolution.ResultContract{JSONFields: map[string]json.RawMessage{}}
			for key, field := range spec.Bindings {
				contract.JSONFields[field] = spec.Effects[key]
			}
			if !resultContractVerifier(node, contract)(b) {
				return fail(fmt.Errorf("GOAP result failed quality contract"))
			}
			b.Result = resolvedResult(b)
		}
		state := goapWorldStateFrom(b).Clone()
		for key, raw := range check.Observed {
			var value any
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return fail(err)
			}
			state[key] = value
		}
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		b.ChainState[goapWorldStateChainKey] = state
		delete(b.ChainState, "goap_effect_error")
		terminal = 1
		return terminal
	})
}

func (b *Blackboard) recordGoapCheck(check evolution.GoapCheck) {
	if b.runEvidence == nil {
		return
	}
	b.runEvidence.mu.Lock()
	defer b.runEvidence.mu.Unlock()
	if len(b.runEvidence.goapChecks) < maxChildTicks {
		b.runEvidence.goapChecks = append(b.runEvidence.goapChecks, check)
	} else {
		b.runEvidence.checksDropped++
	}
}
