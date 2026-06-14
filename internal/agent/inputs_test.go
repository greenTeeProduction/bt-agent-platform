package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateInputs_Required(t *testing.T) {
	specs := []InputSpec{{Name: "code", Type: "text", Required: true}}
	vals := map[string]string{}
	if err := ValidateInputs(specs, vals); err == nil {
		t.Fatal("expected missing required input error")
	}
}

func TestValidateInputs_Default(t *testing.T) {
	specs := []InputSpec{{Name: "mode", Type: "text", Default: "fast"}}
	vals := map[string]string{}
	if err := ValidateInputs(specs, vals); err != nil {
		t.Fatal(err)
	}
	if vals["mode"] != "fast" {
		t.Fatalf("default not applied: %q", vals["mode"])
	}
}

func TestValidateInputs_JSON(t *testing.T) {
	specs := []InputSpec{{Name: "payload", Type: "json", Required: true}}
	if err := ValidateInputs(specs, map[string]string{"payload": "not-json"}); err == nil {
		t.Fatal("expected JSON error")
	}
	if err := ValidateInputs(specs, map[string]string{"payload": `{"ok":true}`}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateInputs_File(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs := []InputSpec{{Name: "doc", Type: "file", Required: true}}
	if err := ValidateInputs(specs, map[string]string{"doc": f}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildTaskFromInputs(t *testing.T) {
	def := &Definition{
		Inputs: []InputSpec{{Name: "topic", Type: "text", Required: true}},
	}
	out := BuildTaskFromInputs(def, "Research this", map[string]string{"topic": "AI agents"})
	if !strings.Contains(out, "Research this") || !strings.Contains(out, "topic: AI agents") {
		t.Fatalf("unexpected task: %q", out)
	}
}

func TestParseInputParams(t *testing.T) {
	m, err := ParseInputParams([]string{"a=1", "b=two"})
	if err != nil {
		t.Fatal(err)
	}
	if m["a"] != "1" || m["b"] != "two" {
		t.Fatalf("unexpected map: %v", m)
	}
	if _, err := ParseInputParams([]string{"bad"}); err == nil {
		t.Fatal("expected parse error")
	}
}
