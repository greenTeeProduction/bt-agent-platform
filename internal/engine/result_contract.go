package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func parseResultContract(node *evolution.SerializableNode) (*evolution.ResultContract, error) {
	return evolution.ParseResultContract(node)
}

func resultContractVerifier(node *evolution.SerializableNode, contract *evolution.ResultContract) func(*Blackboard) bool {
	return func(bb *Blackboard) bool {
		err := contract.Verify(resolvedResult(bb))
		bb.contractValidatedResult = ""
		if err == nil && (len(contract.JSONFields) > 0 || len(contract.RequiredKeys) > 0) {
			bb.contractValidatedResult = resolvedResult(bb)
		}
		qualityOK := validateOutputQuality(bb)
		check := evolution.ResultCheck{Gate: node.Name, Passed: err == nil && qualityOK, OutputDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(resolvedResult(bb))))}
		// Retain a detached oracle so later reports recompute the verdict over
		// the actual final result rather than trusting a stored boolean.
		if data, marshalErr := json.Marshal(contract); marshalErr == nil {
			var retained evolution.ResultContract
			if json.Unmarshal(data, &retained) == nil {
				check.Contract = &retained
			}
		}
		if err != nil {
			check.Reason = err.Error()
		} else if !qualityOK {
			check.Reason = "output quality below threshold"
		}
		if bb.runEvidence != nil {
			bb.runEvidence.mu.Lock()
			if len(bb.runEvidence.checks) < maxChildTicks {
				bb.runEvidence.checks = append(bb.runEvidence.checks, check)
			} else {
				bb.runEvidence.checksDropped++
			}
			bb.runEvidence.mu.Unlock()
		}
		if bb.ChainState == nil {
			bb.ChainState = make(map[string]any)
		}
		if !check.Passed {
			bb.ChainState["result_contract_error"] = check.Reason
			bb.QualityScore = 0
		} else {
			delete(bb.ChainState, "result_contract_error")
		}
		return check.Passed
	}
}
