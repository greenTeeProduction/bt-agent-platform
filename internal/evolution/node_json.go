package evolution

import "encoding/json"

// UnmarshalJSON preserves result/checkpoint contract numbers exactly across persistence.
// Other metadata keeps its existing float64 representation for compatibility.
func (n *SerializableNode) UnmarshalJSON(data []byte) error {
	type plainNode SerializableNode
	var fields struct {
		*plainNode
		Metadata map[string]json.RawMessage `json:"metadata"`
	}
	var decoded plainNode
	fields.plainNode = &decoded
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields.Metadata != nil {
		decoded.Metadata = make(map[string]any, len(fields.Metadata))
		for key, raw := range fields.Metadata {
			if key == "result_contract" || key == "postconditions" {
				decoded.Metadata[key] = raw
				continue
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			decoded.Metadata[key] = value
		}
	}
	*n = SerializableNode(decoded)
	return nil
}
