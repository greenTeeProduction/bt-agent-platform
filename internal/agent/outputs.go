package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateOutputs checks agent output against YAML output specs.
// Agents emit a single text blob; when multiple outputs are declared, type
// constraints apply to that blob (e.g. any json output requires valid JSON).
func ValidateOutputs(specs []OutputSpec, output string) (ok bool, reasons []string) {
	if len(specs) == 0 {
		return true, nil
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return false, []string{"output is empty"}
	}

	needsJSON := false
	for _, spec := range specs {
		if strings.EqualFold(strings.TrimSpace(spec.Type), "json") {
			needsJSON = true
			break
		}
	}
	if needsJSON {
		if err := validateOutputJSON(trimmed); err != nil {
			return false, []string{fmt.Sprintf("output type json: %v", err)}
		}
	}

	return true, nil
}

func validateOutputJSON(output string) error {
	candidates := []string{output}
	if extracted := extractJSONPayload(output); extracted != "" && extracted != output {
		candidates = append([]string{extracted}, candidates...)
	}
	for _, candidate := range candidates {
		var v any
		if err := json.Unmarshal([]byte(candidate), &v); err == nil {
			return nil
		}
	}
	return fmt.Errorf("invalid JSON")
}

// extractJSONPayload pulls JSON from fenced code blocks or a surrounding object/array.
func extractJSONPayload(output string) string {
	lower := strings.ToLower(output)
	for _, fence := range []string{"```json", "```JSON"} {
		if idx := strings.Index(output, fence); idx >= 0 {
			rest := output[idx+len(fence):]
			if end := strings.Index(rest, "```"); end >= 0 {
				return strings.TrimSpace(rest[:end])
			}
		}
	}
	if fence := strings.Index(lower, "```"); fence >= 0 {
		rest := output[fence+3:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			candidate := strings.TrimSpace(rest[:end])
			if strings.HasPrefix(candidate, "{") || strings.HasPrefix(candidate, "[") {
				return candidate
			}
		}
	}
	startObj := strings.Index(output, "{")
	startArr := strings.Index(output, "[")
	start := -1
	end := -1
	switch {
	case startObj >= 0 && (startArr < 0 || startObj < startArr):
		start = startObj
		end = strings.LastIndex(output, "}")
	case startArr >= 0:
		start = startArr
		end = strings.LastIndex(output, "]")
	}
	if start >= 0 && end > start {
		return strings.TrimSpace(output[start : end+1])
	}
	return output
}
