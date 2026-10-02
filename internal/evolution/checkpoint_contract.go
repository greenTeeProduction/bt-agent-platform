package evolution

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CheckpointContract checks typed facts in one explicitly selected state map.
// It verifies observed state; restoring that map cannot undo external effects.
type CheckpointContract struct {
	StateKey string
	Facts    *ResultContract
}

func ParseCheckpointContract(node *SerializableNode) (*CheckpointContract, error) {
	if len(node.Children) != 1 {
		return nil, fmt.Errorf("CheckpointVerifier requires exactly one child")
	}
	key := "world_state"
	if raw, exists := node.Metadata["state_key"]; exists {
		var ok bool
		key, ok = raw.(string)
		if !ok || key != "world_state" && key != "goap_world_state" {
			return nil, fmt.Errorf("checkpoint state_key must be world_state or goap_world_state")
		}
	}
	data, err := json.Marshal(node.Metadata["postconditions"])
	if err != nil {
		return nil, err
	}
	var facts map[string]json.RawMessage
	if err := json.Unmarshal(data, &facts); err != nil || len(facts) == 0 {
		return nil, fmt.Errorf("checkpoint requires nonempty typed postconditions")
	}
	for name, raw := range facts {
		value, err := decodeContractValue(raw)
		if err != nil || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("invalid checkpoint fact %q", name)
		}
		switch value := value.(type) {
		case bool, string:
		case json.Number:
			if _, ok := contractNumber(value); !ok {
				return nil, fmt.Errorf("invalid checkpoint number for %q", name)
			}
		default:
			return nil, fmt.Errorf("checkpoint fact %q must be a boolean, string or number", name)
		}
	}
	return &CheckpointContract{StateKey: key, Facts: &ResultContract{JSONFields: facts}}, nil
}

// Verify uses the same exact numeric comparison as result contracts. Missing
// facts, including expected false, cannot satisfy a postcondition.
func (c *CheckpointContract) Verify(state any) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return c.Facts.Verify(string(data))
}
