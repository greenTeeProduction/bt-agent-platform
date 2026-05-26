// Package api provides typed contracts, JSON Schema I/O, and API versioning
// for the BT platform's agent definitions.
package api

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ─── API Version ────────────────────────────────────────────────────────────

// CurrentAPIVersion is the version of the agent definition schema.
const CurrentAPIVersion = "v1"

// SupportedVersions lists all API versions that can be loaded.
var SupportedVersions = map[string]bool{
	"v1": true,
}

// ─── Type System ────────────────────────────────────────────────────────────

// ContentType represents the type of content an agent produces or consumes.
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeJSON     ContentType = "json"
	ContentTypeMarkdown ContentType = "markdown"
	ContentTypeFile     ContentType = "file"
	ContentTypeCode     ContentType = "code"
)

// ValidContentTypes lists all recognized content types.
var ValidContentTypes = map[ContentType]bool{
	ContentTypeText:     true,
	ContentTypeJSON:     true,
	ContentTypeMarkdown: true,
	ContentTypeFile:     true,
	ContentTypeCode:     true,
}

// ─── JSON Schema I/O ────────────────────────────────────────────────────────

// Schema defines a JSON Schema for input/output validation.
type Schema struct {
	Type        string             `json:"type"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Description string             `json:"description,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
	Format      string             `json:"format,omitempty"`
}

// AgentDefinition is the versioned API contract for agent definitions.
type AgentDefinition struct {
	// API version — enforced on load. Must match a supported version.
	APIVersion string `json:"api_version" yaml:"api_version"`

	// Core identity
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Version     string `json:"version" yaml:"version"` // semantic version of this agent

	// I/O contracts
	InputSchema  *Schema     `json:"input_schema,omitempty" yaml:"input_schema,omitempty"`
	OutputSchema *Schema     `json:"output_schema,omitempty" yaml:"output_schema,omitempty"`
	InputType    ContentType `json:"input_type" yaml:"input_type"`
	OutputType   ContentType `json:"output_type" yaml:"output_type"`

	// Execution
	Tree      string            `json:"tree" yaml:"tree"`           // tree ID
	Schedule  string            `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Timeout   string            `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Validate validates an agent definition against the schema.
func (a *AgentDefinition) Validate() error {
	var errs []string

	// Version check
	if a.APIVersion == "" {
		return fmt.Errorf("api_version is required")
	}
	if !SupportedVersions[a.APIVersion] {
		return fmt.Errorf("unsupported api_version %q (supported: v1)", a.APIVersion)
	}

	// Required fields
	if a.Name == "" {
		errs = append(errs, "name is required")
	}
	if a.Tree == "" {
		errs = append(errs, "tree is required")
	}
	if a.Version == "" {
		errs = append(errs, "version is required (semver)")
	}

	// Content type validation
	if a.InputType != "" && !ValidContentTypes[a.InputType] {
		errs = append(errs, fmt.Sprintf("invalid input_type %q (valid: text, json, markdown, file, code)", a.InputType))
	}
	if a.OutputType != "" && !ValidContentTypes[a.OutputType] {
		errs = append(errs, fmt.Sprintf("invalid output_type %q (valid: text, json, markdown, file, code)", a.OutputType))
	}

	// Schema validation
	if a.InputSchema != nil {
		if err := validateSchema(a.InputSchema, ""); err != nil {
			errs = append(errs, fmt.Sprintf("input_schema: %v", err))
		}
	}
	if a.OutputSchema != nil {
		if err := validateSchema(a.OutputSchema, ""); err != nil {
			errs = append(errs, fmt.Sprintf("output_schema: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("agent definition validation: %s", strings.Join(errs, "; "))
	}
	return nil
}

// validateSchema recursively validates a JSON Schema object.
func validateSchema(s *Schema, path string) error {
	if s == nil {
		return nil
	}

	validTypes := map[string]bool{
		"object": true, "array": true, "string": true,
		"number": true, "integer": true, "boolean": true, "null": true,
	}
	if s.Type != "" && !validTypes[s.Type] {
		return fmt.Errorf("%s: invalid type %q", path, s.Type)
	}

	if s.MinLength != nil && s.MaxLength != nil && *s.MinLength > *s.MaxLength {
		return fmt.Errorf("%s: minLength (%d) > maxLength (%d)", path, *s.MinLength, *s.MaxLength)
	}

	if s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
		return fmt.Errorf("%s: minimum (%f) > maximum (%f)", path, *s.Minimum, *s.Maximum)
	}

	if s.Type == "array" && s.Items != nil {
		if err := validateSchema(s.Items, path+".items"); err != nil {
			return err
		}
	}

	for name, prop := range s.Properties {
		propPath := path + "." + name
		if path == "" {
			propPath = name
		}
		if err := validateSchema(prop, propPath); err != nil {
			return err
		}
	}

	return nil
}

// ─── Type Contract Validation ───────────────────────────────────────────────

// ValidateOutput validates agent output against its output type and schema.
// Returns nil if valid, error describing the mismatch otherwise.
func ValidateOutput(output string, outputType ContentType, schema *Schema) error {
	if outputType == "" {
		return nil // no type constraint
	}

	switch outputType {
	case ContentTypeJSON:
		if !json.Valid([]byte(output)) {
			return fmt.Errorf("expected valid JSON output")
		}
		if schema != nil {
			var v interface{}
			if err := json.Unmarshal([]byte(output), &v); err != nil {
				return fmt.Errorf("JSON unmarshal: %w", err)
			}
			// Basic schema validation — full JSON Schema validation would require
			// a library. We validate structure here.
			if err := validateAgainstSchema(v, schema, ""); err != nil {
				return fmt.Errorf("schema validation: %w", err)
			}
		}

	case ContentTypeMarkdown:
		if !strings.Contains(output, "#") && !strings.Contains(output, "*") && !strings.Contains(output, "-") {
			return fmt.Errorf("output doesn't appear to be markdown (no headers, lists, or formatting)")
		}

	case ContentTypeText:
		if len(strings.TrimSpace(output)) == 0 {
			return fmt.Errorf("expected non-empty text output")
		}

	case ContentTypeCode:
		if len(strings.TrimSpace(output)) == 0 {
			return fmt.Errorf("expected non-empty code output")
		}

	case ContentTypeFile:
		// File output just needs to be non-empty
		if len(output) == 0 {
			return fmt.Errorf("expected non-empty file output")
		}
	}

	return nil
}

// validateAgainstSchema performs basic structural validation of a value against a JSON Schema.
func validateAgainstSchema(v interface{}, s *Schema, path string) error {
	if s == nil {
		return nil
	}

	switch s.Type {
	case "object":
		m, ok := v.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s: expected object, got %T", path, v)
		}
		for _, req := range s.Required {
			if _, exists := m[req]; !exists {
				return fmt.Errorf("%s: missing required field %q", path, req)
			}
		}
		for key, propSchema := range s.Properties {
			if val, exists := m[key]; exists && propSchema != nil {
				propPath := key
				if path != "" {
					propPath = path + "." + key
				}
				if err := validateAgainstSchema(val, propSchema, propPath); err != nil {
					return err
				}
			}
		}

	case "array":
		arr, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf("%s: expected array, got %T", path, v)
		}
		if s.Items != nil {
			for i, item := range arr {
				itemPath := fmt.Sprintf("%s[%d]", path, i)
				if err := validateAgainstSchema(item, s.Items, itemPath); err != nil {
					return err
				}
			}
		}

	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T", path, v)
		}

	case "number", "integer":
		switch v.(type) {
		case float64, int, int64, json.Number:
		default:
			return fmt.Errorf("%s: expected number, got %T", path, v)
		}

	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T", path, v)
		}
	}

	return nil
}

// ─── Marshal / Unmarshal ────────────────────────────────────────────────────

// ParseAgentDefinition parses raw bytes into a validated AgentDefinition.
func ParseAgentDefinition(data []byte) (*AgentDefinition, error) {
	var def AgentDefinition
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("json parse: %w", err)
	}
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return &def, nil
}

// MustParseAgentDefinition parses raw bytes into an AgentDefinition, panicking on error.
func MustParseAgentDefinition(data []byte) *AgentDefinition {
	def, err := ParseAgentDefinition(data)
	if err != nil {
		panic(err)
	}
	return def
}
