package evolution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ResultContract is an explicit task contract, separate from text-quality
// heuristics. Unknown or empty declarations fail validation instead of silently
// weakening a requested gateway.
type ResultContract struct {
	JSONFields   map[string]json.RawMessage `json:"json_fields,omitempty"`
	RequiredKeys []string                   `json:"required_keys,omitempty"`
	MinLength    int                        `json:"min_length,omitempty"`
}

// ParseResultContract validates an optional result_contract declaration.
func ParseResultContract(node *SerializableNode) (*ResultContract, error) {
	raw, exists := node.Metadata["result_contract"]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(data, []byte("null")) {
		return nil, fmt.Errorf("result_contract cannot be null")
	}
	var contract ResultContract
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&contract); err != nil {
		return nil, fmt.Errorf("invalid result_contract: %w", err)
	}
	if contract.MinLength < 0 || (contract.MinLength == 0 && len(contract.JSONFields) == 0 && len(contract.RequiredKeys) == 0) {
		return nil, fmt.Errorf("result_contract requires a nonempty constraint")
	}
	for _, key := range contract.RequiredKeys {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("result_contract contains an empty required key")
		}
	}
	for key := range contract.JSONFields {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("result_contract contains an empty field name")
		}
	}
	return &contract, nil
}

// Verify checks the actual output against every declared constraint.
func (c *ResultContract) Verify(output string) error {
	if len(output) < c.MinLength {
		return fmt.Errorf("result shorter than %d characters", c.MinLength)
	}
	if len(c.JSONFields) == 0 && len(c.RequiredKeys) == 0 {
		return nil
	}
	text := strings.TrimSpace(output)
	if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "```") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "```"))
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &object); err != nil || object == nil {
		return fmt.Errorf("result must be a JSON object")
	}
	for _, key := range c.RequiredKeys {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("missing required JSON field %q", key)
		}
	}
	for key, expected := range c.JSONFields {
		actual, ok := object[key]
		if !ok {
			return fmt.Errorf("missing required JSON field %q", key)
		}
		a, actualErr := decodeContractValue(actual)
		b, expectedErr := decodeContractValue(expected)
		if actualErr != nil || expectedErr != nil || !contractValuesEqual(a, b) {
			return fmt.Errorf("JSON field %q does not satisfy its declared value", key)
		}
	}
	return nil
}

func decodeContractValue(raw json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	err := decoder.Decode(&value)
	return value, err
}

// Keep integer/decimal comparisons exact instead of rounding through float64.
// Bounds prevent short exponent strings from allocating unbounded integers.
func contractNumber(number json.Number) (*big.Rat, bool) {
	text := number.String()
	if len(text) > 2048 {
		return nil, false
	}
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponent, err := strconv.ParseInt(text[index+1:], 10, 32)
		if err != nil || exponent < -4096 || exponent > 4096 {
			return nil, false
		}
	}
	return new(big.Rat).SetString(text)
}

func contractValuesEqual(a, b any) bool {
	switch value := a.(type) {
	case json.Number:
		other, ok := b.(json.Number)
		if !ok {
			return false
		}
		left, leftOK := contractNumber(value)
		right, rightOK := contractNumber(other)
		return leftOK && rightOK && left.Cmp(right) == 0
	case []any:
		other, ok := b.([]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for i := range value {
			if !contractValuesEqual(value[i], other[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		other, ok := b.(map[string]any)
		if !ok || len(value) != len(other) {
			return false
		}
		for key, item := range value {
			expected, found := other[key]
			if !found || !contractValuesEqual(item, expected) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	case bool:
		other, ok := b.(bool)
		return ok && value == other
	case string:
		other, ok := b.(string)
		return ok && value == other
	default:
		return false
	}
}
