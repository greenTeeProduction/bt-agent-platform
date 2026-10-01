package evolution

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// TreeVersion identifies the complete serialized definition, including prompts,
// limits and typed edges. JSON map ordering makes disk round trips stable.
func TreeVersion(tree *SerializableNode) (string, error) {
	if tree == nil {
		return "", fmt.Errorf("missing tree definition")
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return "", fmt.Errorf("encode tree version: %w", err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

const (
	EvidenceExecution   = "execution"
	EvidenceCompilation = "compilation"
	EvidenceFeedback    = "feedback"
)

// ExecutionRecords excludes design/compile checks and explicit satisfaction
// signals from measured task success and latency. Empty kinds are legacy runs;
// they remain historical data, without acquiring a fabricated version.
func ExecutionRecords(records []Record) []Record {
	out := make([]Record, 0, len(records))
	for _, record := range records {
		legacyCompile := record.EvidenceKind == "" && strings.HasPrefix(record.TaskID, "seed-") && strings.HasPrefix(record.Task, "Compile-time validation")
		if !legacyCompile && (record.EvidenceKind == "" || record.EvidenceKind == EvidenceExecution) && record.UserFeedback == "" {
			out = append(out, record)
		}
	}
	return out
}

// FilterByTreeVersion admits only attributed execution of a particular source
// definition. Legacy records cannot prove execution of a newer tree version.
func FilterByTreeVersion(records []Record, treeID, owner, version string) []Record {
	if version == "" {
		return nil
	}
	out := make([]Record, 0, len(records))
	for _, record := range records {
		if record.TreeName == treeID && record.User == owner && record.TreeVersion == version && record.EvidenceKind == EvidenceExecution && len(record.ExecutionVersions) == 1 && record.ExecutionVersions[0] == version && record.ResultChecksDropped == 0 {
			out = append(out, record)
		}
	}
	return out
}
