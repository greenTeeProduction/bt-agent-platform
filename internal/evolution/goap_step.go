package evolution

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GoapStepSpec separates predicted effects from the source that must observe
// them. Bindings name fields in that source, never commands for a model to run.
type GoapStepSpec struct {
	Source        string                     `json:"source"`
	Bindings      map[string]string          `json:"bindings"`
	Preconditions map[string]json.RawMessage `json:"preconditions,omitempty"`
	Effects       map[string]json.RawMessage `json:"effects"`
}

func ParseGoapStep(node *SerializableNode) (*GoapStepSpec, error) {
	if len(node.Children) != 1 || strings.TrimSpace(node.Name) == "" {
		return nil, fmt.Errorf("GoapStep requires a name and exactly one executable child")
	}
	spec := &GoapStepSpec{Source: "capability"}
	if raw, ok := node.Metadata["effect_source"]; ok {
		var valid bool
		spec.Source, valid = raw.(string)
		if !valid {
			return nil, fmt.Errorf("effect_source must be a string")
		}
	}
	if spec.Source != "result" && spec.Source != "file_task" && spec.Source != "capability" {
		return nil, fmt.Errorf("unknown GOAP effect source %q", spec.Source)
	}
	contract, err := ParseCheckpointContract(&SerializableNode{Metadata: map[string]any{"postconditions": node.Metadata["effects"]}, Children: node.Children})
	if err != nil {
		return nil, err
	}
	spec.Effects = contract.Facts.JSONFields
	if raw, ok := node.Metadata["preconditions"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &spec.Preconditions); err != nil {
			return nil, err
		}
		if len(spec.Preconditions) > 0 {
			if _, err = ParseCheckpointContract(&SerializableNode{Metadata: map[string]any{"postconditions": raw}, Children: node.Children}); err != nil {
				return nil, err
			}
		}
	}
	spec.Bindings = make(map[string]string, len(spec.Effects))
	for key := range spec.Effects {
		spec.Bindings[key] = key
		if spec.Source == "result" {
			field, ok := strings.CutPrefix(key, "result.")
			if !ok || strings.TrimSpace(field) == "" {
				return nil, fmt.Errorf("model result effects must use result.<field>; external completion requires a capability or file observer")
			}
			spec.Bindings[key] = field
		}
	}
	if raw, ok := node.Metadata["effect_bindings"]; ok {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var bindings map[string]string
		if err := json.Unmarshal(data, &bindings); err != nil {
			return nil, err
		}
		if len(bindings) != len(spec.Effects) {
			return nil, fmt.Errorf("every GOAP effect requires exactly one observation binding")
		}
		for key, field := range bindings {
			if _, ok := spec.Effects[key]; !ok || strings.TrimSpace(field) == "" {
				return nil, fmt.Errorf("invalid GOAP observation binding %q", key)
			}
		}
		spec.Bindings = bindings
	}
	return spec, nil
}

// GoapCheck retains the actual observed fields and their declared value oracle.
// Source/Origin distinguish checked model output from capability/file evidence.
type GoapCheck struct {
	Step         string                     `json:"step"`
	Scope        string                     `json:"scope"`
	Source       string                     `json:"source"`
	Origin       string                     `json:"origin"`
	Passed       bool                       `json:"passed"`
	Reason       string                     `json:"reason,omitempty"`
	Observed     map[string]json.RawMessage `json:"observed,omitempty"`
	Expected     map[string]json.RawMessage `json:"expected"`
	OutputDigest string                     `json:"output_digest,omitempty"`
}

func (c GoapCheck) Verify() error {
	if len(c.Expected) == 0 {
		return fmt.Errorf("GOAP observation lacks a value oracle")
	}
	data, err := json.Marshal(c.Observed)
	if err != nil {
		return err
	}
	return (&ResultContract{JSONFields: c.Expected}).Verify(string(data))
}
