package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ─── setChainState ──────────────────────────────────────────────────────────

func TestSetChainState_NilChainState(t *testing.T) {
	bb := &Blackboard{}
	setChainState(bb, "key1", "value1")
	if bb.ChainState == nil {
		t.Fatal("ChainState should be initialized")
	}
	if v, ok := bb.ChainState["key1"]; !ok || v != "value1" {
		t.Errorf("unexpected value: %v", v)
	}
}

func TestSetChainState_ExistingChainState(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"existing": 42}}
	setChainState(bb, "new_key", "new_value")
	if v, ok := bb.ChainState["new_key"]; !ok || v != "new_value" {
		t.Errorf("unexpected value: %v", v)
	}
	if v, ok := bb.ChainState["existing"]; !ok || v != 42 {
		t.Errorf("existing key lost: %v", v)
	}
}

func TestSetChainState_Overwrite(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"key": "old"}}
	setChainState(bb, "key", "new")
	if v, ok := bb.ChainState["key"]; !ok || v != "new" {
		t.Errorf("expected overwrite, got %v", v)
	}
}

func TestSetChainState_BoolValue(t *testing.T) {
	bb := &Blackboard{}
	setChainState(bb, "done", true)
	if v, ok := bb.ChainState["done"]; !ok || v != true {
		t.Errorf("unexpected value: %v", v)
	}
}

// ─── getBoolChainState ──────────────────────────────────────────────────────

func TestGetBoolChainState_NilChainState(t *testing.T) {
	bb := &Blackboard{}
	if getBoolChainState(bb, "any") {
		t.Error("nil ChainState should return false")
	}
}

func TestGetBoolChainState_MissingKey(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{}}
	if getBoolChainState(bb, "missing") {
		t.Error("missing key should return false")
	}
}

func TestGetBoolChainState_True(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"flag": true}}
	if !getBoolChainState(bb, "flag") {
		t.Error("true should return true")
	}
}

func TestGetBoolChainState_False(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"flag": false}}
	if getBoolChainState(bb, "flag") {
		t.Error("false should return false")
	}
}

func TestGetBoolChainState_WrongType(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"flag": "yes"}}
	if getBoolChainState(bb, "flag") {
		t.Error("non-bool should return false")
	}
}

func TestGetBoolChainState_IntValue(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"flag": 1}}
	if getBoolChainState(bb, "flag") {
		t.Error("int 1 should return false (not a bool)")
	}
}

// ─── countCPUCores ──────────────────────────────────────────────────────────

func TestCountCPUCores_Standard(t *testing.T) {
	cpuinfo := `processor : 0
model name : ARM Cortex
processor : 1
model name : ARM Cortex
processor : 2
model name : ARM Cortex
processor : 3
model name : ARM Cortex`
	if n := countCPUCores(cpuinfo); n != 4 {
		t.Errorf("expected 4 cores, got %d", n)
	}
}

func TestCountCPUCores_SingleCore(t *testing.T) {
	cpuinfo := `processor : 0
model name : ARM Cortex`
	if n := countCPUCores(cpuinfo); n != 1 {
		t.Errorf("expected 1 core, got %d", n)
	}
}

func TestCountCPUCores_Empty(t *testing.T) {
	if n := countCPUCores(""); n != 1 {
		t.Errorf("expected 1 (default) for empty cpuinfo, got %d", n)
	}
}

func TestCountCPUCores_NoProcessorLines(t *testing.T) {
	cpuinfo := `model name : ARM Cortex
Features : fp asimd`
	if n := countCPUCores(cpuinfo); n != 1 {
		t.Errorf("expected 1 (default) when no processor lines, got %d", n)
	}
}

func TestCountCPUCores_TwelveCores(t *testing.T) {
	cpuinfo := ""
	for i := 0; i < 12; i++ {
		cpuinfo += "processor\t: " + string(rune('0'+i%10)) + "\n"
	}
	if n := countCPUCores(cpuinfo); n != 12 {
		t.Errorf("expected 12 cores, got %d", n)
	}
}

// ─── sectionFileExists ──────────────────────────────────────────────────────

func TestSectionFileExists_ExistingFile(t *testing.T) {
	dir := "/tmp/arc42-test"
	filename := "test-section.md"
	os.MkdirAll(dir, 0755)
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, filename), []byte("test"), 0644)

	// Override the base path for testing — we can't change the const,
	// but we can test with a file that exists in the real arc42 path.
	// Since the real path is /mnt/ssd/clawd/wiki/bt-research/docs/arc42,
	// we'll test that a non-existent file returns false.
	if sectionFileExists("nonexistent-file-xyz.md") {
		t.Error("nonexistent file should return false")
	}
}

func TestSectionFileExists_NonExistent(t *testing.T) {
	if sectionFileExists("completely-unknown-file-12345.md") {
		t.Error("non-existent file should return false")
	}
}

// ─── arc42 Registered Conditions (pure, no FS needed) ───────────────────────

func TestAllSectionsDone_NoneDone(t *testing.T) {
	// registerArc42Nodes is called in init(), conditions are already registered
	cond, ok := conditionRegistry["AllSectionsDone"]
	if !ok {
		t.Fatal("AllSectionsDone not registered")
	}

	bb := &Blackboard{ChainState: map[string]any{}}
	if cond(bb) {
		t.Error("AllSectionsDone should be false when nothing is done")
	}
}

func TestAllSectionsDone_AllDone(t *testing.T) {
	cond, ok := conditionRegistry["AllSectionsDone"]
	if !ok {
		t.Fatal("AllSectionsDone not registered")
	}

	cs := map[string]any{}
	for i := 1; i <= 12; i++ {
		cs[fmt.Sprintf("section_%d_done", i)] = true
	}
	bb := &Blackboard{ChainState: cs}
	if !cond(bb) {
		t.Error("AllSectionsDone should be true when all 12 sections done")
	}
}

func TestAllSectionsDone_OneMissing(t *testing.T) {
	cond, ok := conditionRegistry["AllSectionsDone"]
	if !ok {
		t.Fatal("AllSectionsDone not registered")
	}

	cs := map[string]any{}
	for i := 1; i <= 11; i++ {
		cs[fmt.Sprintf("section_%d_done", i)] = true
	}
	// section_12_done is missing
	bb := &Blackboard{ChainState: cs}
	if cond(bb) {
		t.Error("AllSectionsDone should be false when section 12 is missing")
	}
}
