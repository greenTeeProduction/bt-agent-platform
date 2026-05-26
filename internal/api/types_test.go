package api

import (
	"encoding/json"
	"testing"
)

func TestAgentDefinition_Validate_Valid(t *testing.T) {
	def := &AgentDefinition{
		APIVersion: "v1",
		Name:       "test-agent",
		Version:    "1.0.0",
		Tree:       "domain:code_review",
		InputType:  ContentTypeText,
		OutputType: ContentTypeJSON,
	}

	if err := def.Validate(); err != nil {
		t.Errorf("expected valid agent, got: %v", err)
	}
}

func TestAgentDefinition_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		def  AgentDefinition
		msg  string
	}{
		{"missing version", AgentDefinition{APIVersion: "v1", Name: "a", Tree: "t"}, "version"},
		{"missing api_version", AgentDefinition{Name: "a", Tree: "t", Version: "1.0.0"}, "api_version"},
		{"unsupported version", AgentDefinition{APIVersion: "v99", Name: "a", Tree: "t", Version: "1.0.0"}, "unsupported"},
		{"invalid input type", AgentDefinition{APIVersion: "v1", Name: "a", Tree: "t", Version: "1.0.0", InputType: "binary"}, "invalid input_type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Validate()
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestAgentDefinition_JSON_MarshalRoundtrip(t *testing.T) {
	def := &AgentDefinition{
		APIVersion: "v1",
		Name:       "code-reviewer",
		Description: "Reviews code for bugs and style issues",
		Version:    "1.2.3",
		InputType:  ContentTypeCode,
		OutputType: ContentTypeMarkdown,
		Tree:       "domain:code_review",
		Schedule:   "every 1h",
		Timeout:    "5m",
		InputSchema: &Schema{
			Type: "object",
			Properties: map[string]*Schema{
				"code": {Type: "string", Description: "Code to review"},
			},
			Required: []string{"code"},
		},
	}

	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	parsed, err := ParseAgentDefinition(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.Name != def.Name {
		t.Errorf("name mismatch: %s != %s", parsed.Name, def.Name)
	}
	if parsed.InputSchema == nil {
		t.Error("input schema lost in roundtrip")
	}
}

func TestValidateOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		ct       ContentType
		schema   *Schema
		wantErr  bool
	}{
		{"valid JSON", `{"key": "value"}`, ContentTypeJSON, nil, false},
		{"invalid JSON", `not json`, ContentTypeJSON, nil, true},
		{"valid markdown", "# Title\n* item", ContentTypeMarkdown, nil, false},
		{"empty text", "", ContentTypeText, nil, true},
		{"valid text", "hello world", ContentTypeText, nil, false},
		{"valid code", "func main() {}", ContentTypeCode, nil, false},
		{"JSON with schema valid", `{"name": "test"}`, ContentTypeJSON,
			&Schema{Type: "object", Properties: map[string]*Schema{
				"name": {Type: "string"},
			}, Required: []string{"name"}}, false},
		{"JSON with schema missing required", `{"other": "x"}`, ContentTypeJSON,
			&Schema{Type: "object", Properties: map[string]*Schema{
				"name": {Type: "string"},
			}, Required: []string{"name"}}, true},
		{"no type constraint", "anything", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateOutput(tt.output, tt.ct, tt.schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateOutput() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestSchemaValidation(t *testing.T) {
	tests := []struct {
		name    string
		schema  *Schema
		wantErr bool
	}{
		{"valid schema", &Schema{Type: "object"}, false},
		{"invalid type", &Schema{Type: "invalid"}, true},
		{"min > max", &Schema{Type: "string", MinLength: intPtr(10), MaxLength: intPtr(5)}, true},
		{"valid string", &Schema{Type: "string", MinLength: intPtr(1), MaxLength: intPtr(100)}, false},
		{"valid array", &Schema{Type: "array", Items: &Schema{Type: "string"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSchema(tt.schema, "root")
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSchema() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func intPtr(i int) *int { return &i }
