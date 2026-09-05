package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ValidateInputs checks provided values against an agent's input spec.
// Missing keys with defaults are filled in place.
func ValidateInputs(specs []InputSpec, values map[string]string) error {
	if len(specs) == 0 {
		return nil
	}
	if values == nil {
		values = make(map[string]string)
	}
	for _, in := range specs {
		v := strings.TrimSpace(values[in.Name])
		if v == "" && in.Default != "" {
			values[in.Name] = in.Default
			continue
		}
		if in.Required && v == "" {
			return fmt.Errorf("missing required input %q", in.Name)
		}
		if v == "" {
			continue
		}
		if err := validateInputType(in.Type, v); err != nil {
			return fmt.Errorf("input %q: %w", in.Name, err)
		}
	}
	return nil
}

func validateInputType(typ, value string) error {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "text":
		return nil
	case "json":
		var v any
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("invalid JSON")
		}
	case "file":
		if _, err := os.Stat(value); err != nil {
			return fmt.Errorf("file not found: %s", value)
		}
	default:
		return nil
	}
	return nil
}

// BuildTaskFromInputs merges base task text with structured input values.
func BuildTaskFromInputs(def *Definition, baseTask string, values map[string]string) string {
	if def == nil || len(def.Inputs) == 0 {
		return baseTask
	}
	if values == nil {
		values = make(map[string]string)
	}
	if baseTask != "" {
		if _, ok := values["task"]; !ok {
			values["task"] = baseTask
		}
	}

	var b strings.Builder
	if baseTask != "" {
		b.WriteString(baseTask)
		b.WriteString("\n\n")
	}
	b.WriteString("## Inputs\n")
	for _, in := range def.Inputs {
		if v := strings.TrimSpace(values[in.Name]); v != "" {
			fmt.Fprintf(&b, "- %s: %s\n", in.Name, v)
		}
	}
	return strings.TrimSpace(b.String())
}

// ParseInputParams parses key=value pairs from CLI --param flags.
func ParseInputParams(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --param %q (expected name=value)", p)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("invalid --param %q (empty name)", p)
		}
		out[k] = strings.TrimSpace(v)
	}
	return out, nil
}
