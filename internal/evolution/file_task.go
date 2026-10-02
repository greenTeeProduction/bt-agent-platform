package evolution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
)

// FileTaskSpec binds a task to owner-relative input/output artifacts. Paths are
// data, never shell commands or model-selected destinations.
type FileTaskSpec struct {
	Input  string `json:"input,omitempty"`
	Output string `json:"output"`
}

func (s FileTaskSpec) Validate() error {
	for _, path := range []string{s.Input, s.Output} {
		if path == "" {
			continue
		}
		for part := range strings.SplitSeq(path, "/") {
			if strings.HasPrefix(part, ".artifact-") {
				return fmt.Errorf("artifact path uses a reserved internal name")
			}
		}
		if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00") || len(path) > 512 {
			return fmt.Errorf("artifact path must be a relative file beneath the owner's artifact directory")
		}
	}
	if s.Output == "" || s.Input == s.Output {
		return fmt.Errorf("distinct artifact output path is required")
	}
	return nil
}

func ParseFileTask(node *SerializableNode) (*FileTaskSpec, error) {
	data, err := json.Marshal(node.Metadata["file_task"])
	if err != nil {
		return nil, err
	}
	var spec FileTaskSpec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	owner, _ := node.Metadata["user"].(string)
	task, _ := node.Metadata["task"].(string)
	contract, err := ParseResultContract(node)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(task) == "" || len(node.Children) != 1 || contract == nil || len(contract.JSONFields)+len(contract.RequiredKeys) == 0 {
		return nil, fmt.Errorf("FileTask requires an exact owner/task, one child and a JSON result contract")
	}
	return &spec, nil
}

// EffectReceipt records observed file state, distinct from model-generated text.
// Owner, task, version and run identity come from the enclosing execution record.
type EffectReceipt struct {
	Scope          string `json:"scope,omitempty"`
	Kind           string `json:"kind"`
	Input          string `json:"input,omitempty"`
	InputDigest    string `json:"input_digest,omitempty"`
	Output         string `json:"output"`
	OutputDigest   string `json:"output_digest,omitempty"`
	WriteCommitted bool   `json:"write_committed"`
	Verified       bool   `json:"verified"`
	Error          string `json:"error,omitempty"`
}
