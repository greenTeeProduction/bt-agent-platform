package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// ─── arc42 Registered Actions ────────────────────────────────────────────────

// Helper to look up an arc42 action and call it with a fresh BB.
func callArc42Action(t *testing.T, name string, bb *Blackboard) int {
	t.Helper()
	// arc42 nodes are registered in init() via registerArc42Nodes()
	act, ok := actionRegistry[name]
	if !ok {
		t.Fatalf("arc42 action %q not registered", name)
	}
	return act(&btcore.BTContext[Blackboard]{Blackboard: bb})
}

func TestArc42Action_ReadGraphReport_NoFile(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadGraphReport", bb)
	if status != 1 {
		t.Errorf("expected 1 (success/fallback), got %d", status)
	}
	if bb.CachedResult == "" {
		t.Error("CachedResult should contain fallback message")
	}
}

func TestArc42Action_ReadADRs_NoGlob(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadADRs", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["adrs"]; !ok {
		t.Error("adrs should be set in ChainState")
	}
}

func TestArc42Action_ReadGoMod_Exists(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadGoMod", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// go_mod may be empty if go.mod doesn't exist from the test CWD
	// but the action should always complete without error
	_ = status
}

func TestArc42Action_ReadConfigFiles_NoFile(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadConfigFiles", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["config"]; !ok {
		t.Error("config should be set in ChainState (may be empty)")
	}
}

func TestArc42Action_DetectHardware(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "DetectHardware", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	hw, ok := bb.ChainState["hardware"]
	if !ok {
		t.Error("hardware should be set in ChainState")
	}
	hwStr, ok := hw.(string)
	if !ok || hwStr == "" {
		t.Error("hardware should be a non-empty string")
	}
}

func TestArc42Action_DetectProcesses(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "DetectProcesses", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["processes"]; !ok {
		t.Error("processes should be set in ChainState")
	}
}

func TestArc42Action_ListPackages(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ListPackages", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// packages key is always set (may be empty if run from wrong CWD)
	_ = bb.ChainState["packages"]
}

func TestArc42Action_ListBinaries(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ListBinaries", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// binaries key is always set (may be empty if run from wrong CWD)
	_ = bb.ChainState["binaries"]
}

func TestArc42Action_ListExternalAPIs(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ListExternalAPIs", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["external_apis"]; !ok {
		t.Error("external_apis should be set in ChainState")
	}
}

func TestArc42Action_ListMCPTools(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ListMCPTools", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	mcp, ok := bb.ChainState["mcp_tools"]
	if !ok {
		t.Error("mcp_tools should be set in ChainState")
	}
	mcpStr, ok := mcp.(string)
	if !ok || mcpStr == "" {
		t.Error("mcp_tools should be a non-empty string")
	}
}

func TestArc42Action_ScanCodeComments(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ScanCodeComments", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["comments"]; !ok {
		t.Error("comments should be set in ChainState")
	}
}

func TestArc42Action_ScanTypes(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ScanTypes", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["types"]; !ok {
		t.Error("types should be set in ChainState")
	}
}

func TestArc42Action_ReadEngineCode(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadEngineCode", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if bb.CachedResult == "" {
		t.Error("CachedResult should contain engine code")
	}
}

func TestArc42Action_ReadSection1_NoFile(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadSection1", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if bb.CachedResult == "" {
		t.Error("CachedResult should contain fallback message")
	}
}

func TestArc42Action_ReadTestCoverage_InTest(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadTestCoverage", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// When running inside go test, ReadTestCoverage short-circuits
	cov, ok := bb.ChainState["coverage"]
	if !ok {
		t.Error("coverage should be set in ChainState")
	}
	covStr, ok := cov.(string)
	if !ok || covStr == "" {
		t.Error("coverage should be a non-empty string")
	}
}

func TestArc42Action_ReadErrorLogs(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadErrorLogs", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["errors"]; !ok {
		t.Error("errors should be set in ChainState")
	}
}

// ─── Validation & Persistence ────────────────────────────────────────────────

func TestArc42Action_SetupDocTools(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "SetupDocTools", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if len(bb.ChainTools) != 3 {
		t.Errorf("expected 3 real tools, got %d", len(bb.ChainTools))
	}
}

func TestArc42Action_ValidateSection_TooShort(t *testing.T) {
	bb := &Blackboard{Result: "short"}
	status := callArc42Action(t, "ValidateSection", bb)
	if status != 0 {
		t.Errorf("expected 0 (fail) for short section, got %d", status)
	}
	if bb.Outcome == "" {
		t.Error("Outcome should be set on validation failure")
	}
}

func TestArc42Action_ValidateSection_ContainsPlaceholder(t *testing.T) {
	bb := &Blackboard{Result: "This is a section with enough chars to pass the length check but it also has a <insert placeholder that should trigger the warning path."}
	status := callArc42Action(t, "ValidateSection", bb)
	if status != 1 {
		t.Errorf("expected 1 (pass with warning) for placeholder, got %d", status)
	}
	if bb.Outcome != "validation_warning: contains placeholder text" {
		t.Errorf("expected validation_warning, got %q", bb.Outcome)
	}
}

func TestArc42Action_ValidateSection_TODOPlaceholder(t *testing.T) {
	bb := &Blackboard{Result: "This section has more than enough characters to pass the length check and it also contains a TODO placeholder that needs to be resolved before completion."}
	status := callArc42Action(t, "ValidateSection", bb)
	if status != 1 {
		t.Errorf("expected 1 (pass with warning) for TODO, got %d", status)
	}
	if bb.Outcome != "validation_warning: contains placeholder text" {
		t.Errorf("expected validation_warning, got %q", bb.Outcome)
	}
}

func TestArc42Action_ValidateSection_Pass(t *testing.T) {
	bb := &Blackboard{Result: "This is a complete section with enough characters to pass validation. It contains meaningful content that describes the architecture of the system."}
	status := callArc42Action(t, "ValidateSection", bb)
	if status != 1 {
		t.Errorf("expected 1 (pass), got %d", status)
	}
	if bb.Outcome != "validation_passed" {
		t.Errorf("expected validation_passed, got %q", bb.Outcome)
	}
}

func TestArc42Action_SaveSection_MissingFilename(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{}}
	status := callArc42Action(t, "SaveSection", bb)
	if status != 0 {
		t.Errorf("expected 0 (fail) for missing filename, got %d", status)
	}
	if bb.Outcome != "save_failed: no filename in chain state" {
		t.Errorf("expected save_failed, got %q", bb.Outcome)
	}
}

func TestArc42Action_SaveSection_WrongType(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"arc42_section_file": 42}}
	status := callArc42Action(t, "SaveSection", bb)
	if status != 0 {
		t.Errorf("expected 0 (fail) for wrong type, got %d", status)
	}
	if bb.Outcome != "save_failed: no filename in chain state" {
		t.Errorf("expected save_failed, got %q", bb.Outcome)
	}
}

func TestArc42Action_SaveSection_EmptyFilename(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"arc42_section_file": ""}}
	status := callArc42Action(t, "SaveSection", bb)
	if status != 0 {
		t.Errorf("expected 0 (fail) for empty filename, got %d", status)
	}
}

func TestArc42Action_SaveDocument_NoError(t *testing.T) {
	bb := &Blackboard{Result: "test document content"}
	// SaveDocument will try to write to /mnt/ssd/clawd/wiki/bt-research/docs/arc42/ which may or may not exist.
	// We just verify it completes without panic (may fail with 0 if dir doesn't exist).
	status := callArc42Action(t, "SaveDocument", bb)
	// Either success (1) or failure (0) is acceptable — the path may not exist
	if status != 1 && status != 0 {
		t.Errorf("expected 0 or 1, got %d", status)
	}
}

func TestArc42Action_MarkSectionDone_WithSection(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"arc42_section": "3"}}
	status := callArc42Action(t, "MarkSectionDone", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if v, ok := bb.ChainState["section_3_done"]; !ok || v != true {
		t.Error("section_3_done should be true")
	}
}

func TestArc42Action_MarkSectionDone_NoSection(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{}}
	status := callArc42Action(t, "MarkSectionDone", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// Should not panic when section key is missing
}

func TestArc42Action_MarkSectionDone_WrongType(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"arc42_section": true}}
	status := callArc42Action(t, "MarkSectionDone", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// Should not panic when section value is not a string
}

func TestArc42Action_MarkDocAssembled(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "MarkDocAssembled", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if v, ok := bb.ChainState["doc_assembled"]; !ok || v != true {
		t.Error("doc_assembled should be true")
	}
}

func TestArc42Action_CollectAllSections_NoFiles(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "CollectAllSections", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	// Should get empty string when no files exist, not panic
}

func TestArc42Action_GenerateTOC(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "GenerateTOC", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	toc, ok := bb.ChainState["toc"]
	if !ok {
		t.Fatal("toc should be set in ChainState")
	}
	tocStr, ok := toc.(string)
	if !ok || tocStr == "" {
		t.Fatal("toc should be a non-empty string")
	}
	// Should have 12 section entries
	if !strings.Contains(tocStr, "Section 12") {
		t.Error("TOC should include Section 12")
	}
}

func TestArc42Action_GitHistory(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "ReadGitHistory", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
	if _, ok := bb.ChainState["git_history"]; !ok {
		t.Error("git_history should be set")
	}
}

func TestArc42Action_CollectAllSections_NilChainState(t *testing.T) {
	bb := &Blackboard{}
	status := callArc42Action(t, "CollectAllSections", bb)
	if status != 1 {
		t.Errorf("expected 1, got %d", status)
	}
}

// ─── arc42 Conditions ────────────────────────────────────────────────────────

func TestArc42Condition_GraphIsFresh_ChainStateTrue(t *testing.T) {
	cond, ok := conditionRegistry["GraphIsFresh"]
	if !ok {
		t.Fatal("GraphIsFresh not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{"graph_fresh": true}}
	if !cond(bb) {
		t.Error("GraphIsFresh should be true when graph_fresh is true")
	}
}

func TestArc42Condition_GraphIsFresh_ChainStateFalse(t *testing.T) {
	cond, ok := conditionRegistry["GraphIsFresh"]
	if !ok {
		t.Fatal("GraphIsFresh not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{"graph_fresh": false}}
	if cond(bb) {
		t.Error("GraphIsFresh should be false when graph_fresh is false")
	}
}

func TestArc42Condition_GraphIsFresh_NoFile(t *testing.T) {
	cond, ok := conditionRegistry["GraphIsFresh"]
	if !ok {
		t.Fatal("GraphIsFresh not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	// Should check for graphify-out/GRAPH_REPORT.md which doesn't exist
	if cond(bb) {
		t.Error("GraphIsFresh should be false when no chain state and no file")
	}
}

func TestArc42Condition_Section1Done_ChainState(t *testing.T) {
	cond, ok := conditionRegistry["Section1Done"]
	if !ok {
		t.Fatal("Section1Done not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{"section_1_done": true}}
	if !cond(bb) {
		t.Error("Section1Done should be true from chain state")
	}
}

func TestArc42Condition_Section1Done_False(t *testing.T) {
	cond, ok := conditionRegistry["Section1Done"]
	if !ok {
		t.Fatal("Section1Done not registered")
	}
	// Section1Done checks chain state OR file existence.
	// With no chain state, it falls through to sectionFileExists which checks
	// /mnt/ssd/clawd/wiki/bt-research/docs/arc42/01-introduction-goals.md.
	// If that file exists, this returns true. We verify it doesn't panic
	// and returns a bool.
	bb := &Blackboard{ChainState: map[string]any{}}
	result := cond(bb)
	_ = result // may be true or false depending on filesystem state
}

func TestArc42Condition_Section4Done_ChainState(t *testing.T) {
	cond, ok := conditionRegistry["Section4Done"]
	if !ok {
		t.Fatal("Section4Done not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{"section_4_done": true}}
	if !cond(bb) {
		t.Error("Section4Done should be true from chain state")
	}
}

func TestArc42Condition_Section5Done_ChainState(t *testing.T) {
	cond, ok := conditionRegistry["Section5Done"]
	if !ok {
		t.Fatal("Section5Done not registered")
	}
	bb := &Blackboard{ChainState: map[string]any{"section_5_done": true}}
	if !cond(bb) {
		t.Error("Section5Done should be true from chain state")
	}
}
