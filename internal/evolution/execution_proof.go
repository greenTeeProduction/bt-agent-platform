package evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func ValidTreeVersion(version string) bool {
	raw, ok := strings.CutPrefix(version, "sha256:")
	if !ok || len(raw) != 64 {
		return false
	}
	_, err := hex.DecodeString(raw)
	return err == nil
}

// ExactExecution excludes legacy, compilation, feedback and mutable mixed-tree
// records from source/version attribution. Failed executions still count as
// adoption observations; they never become successful task results.
func (r Record) ExactExecution() bool {
	return r.EvidenceKind == EvidenceExecution && r.UserFeedback == "" && r.TaskID != "" && r.RunID != "" && r.TreeName != "" &&
		!r.StartedAt.IsZero() && r.Timestamp >= r.StartedAt.UnixMilli() && ValidTreeVersion(r.TreeVersion) &&
		len(r.ExecutionVersions) == 1 && r.ExecutionVersions[0] == r.TreeVersion
}

// VerifiedFinalResult recomputes an executed value oracle against the retained
// final output. Stored pass flags, prose quality and key-presence alone do not
// establish task correctness. Earlier failed attempts may be valid recovery.
func (r Record) VerifiedFinalResult() bool {
	if !r.ExactExecution() || r.Outcome != Success || r.Error != "" || r.ResultChecksDropped != 0 {
		return false
	}
	for _, effect := range r.Effects {
		if effect.WriteCommitted && !effect.Verified {
			return false
		}
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(r.Result)))
	for _, check := range r.ResultChecks {
		if check.Passed && check.OutputDigest == digest && check.Contract != nil && len(check.Contract.JSONFields) > 0 && check.Contract.Verify(r.Result) == nil {
			return true
		}
	}
	return false
}
