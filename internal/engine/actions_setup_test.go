package engine

import (
	"reflect"
	"strings"
	"testing"
)

// Characterization tests for the tool-setup action nodes and helpers in
// actions_setup.go — pinning current exported behavior (registered action
// names, the exact tool sets each Setup* action installs, and the
// EnsureTaskTools / inferToolsForTask / appendMissingRealTools / uniqueStrings
// helpers) with no production changes.

// toolNames extracts the .name field of each *realTool on a Blackboard's
// ChainTools, in order, for assertions against expected tool sets.
func toolNames(tools []any) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if rt, ok := t.(*realTool); ok {
			names = append(names, rt.name)
		}
	}
	return names
}

func TestSetupActions_InstallExpectedToolSets(t *testing.T) {
	tests := []struct {
		action string
		want   []string
	}{
		{"SetupDevTools", []string{"go_build", "go_test", "go_vet", "web_search"}},
		{"SetupUniversalTools", []string{"shell_exec", "file_read", "file_write", "web_search", "calculator"}},
		{"SetupDataPipelineTools", []string{"file_read", "file_write", "shell_exec", "calculator"}},
		{"SetupNotebookLMTools", []string{
			"notebooklm_server_info",
			"notebooklm_list",
			"notebooklm_notebook_get",
			"notebooklm_research_start",
			"notebooklm_research_status",
			"notebooklm_research_import",
			"notebooklm_notebook_query",
			"notebooklm_refresh_auth",
			"shell_exec",
			"file_read",
			"file_write",
			"web_search",
		}},
		{"SetupResearchTools", []string{"web_search", "http_get", "file_read", "shell_exec", "graphify", "calculator"}},
		{"SetupStartupTools", []string{"web_search", "calculator"}},
		{"SetupFusionTools", []string{"file_read", "file_write", "shell_exec", "web_search", "graphify", "calculator"}},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			bb := &Blackboard{}
			if got := runFusionAction(t, tt.action, bb); got != 1 {
				t.Fatalf("status = %d, want 1", got)
			}
			if got := toolNames(bb.ChainTools); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ChainTools = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- DiscoverAvailableTools --------------------------------------------------

func TestDiscoverAvailableTools_NoToolsFallsBackToAllRealToolNames(t *testing.T) {
	bb := &Blackboard{}
	if got := runFusionAction(t, "DiscoverAvailableTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	want := strings.Join(allRealToolNames(), ", ")
	if got := bb.ChainState["available_tools"]; got != want {
		t.Fatalf("ChainState[available_tools] = %v, want %v", got, want)
	}
}

func TestDiscoverAvailableTools_InitializesNilChainState(t *testing.T) {
	bb := &Blackboard{ChainState: nil}
	if got := runFusionAction(t, "DiscoverAvailableTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if bb.ChainState == nil {
		t.Fatal("ChainState must be initialized")
	}
}

func TestDiscoverAvailableTools_WithToolsReportsTheirNames(t *testing.T) {
	bb := &Blackboard{ChainTools: buildRealTools("calculator", "web_search")}
	if got := runFusionAction(t, "DiscoverAvailableTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if got := bb.ChainState["available_tools"]; got != "calculator, web_search" {
		t.Fatalf("ChainState[available_tools] = %v, want %q", got, "calculator, web_search")
	}
}

// --- EnsureTaskTools ----------------------------------------------------------

func TestEnsureTaskTools_AddsInferredToolsAndRecordsState(t *testing.T) {
	// Note: ".go file" contains the literal substring "go " (from ".go" followed
	// by a space), so this task text trips both the code-file branch and the
	// build/test/ci branch of inferToolsForTask.
	bb := &Blackboard{Task: "review this .go file for bugs"}
	if got := runFusionAction(t, "EnsureTaskTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	wantRequested := "file_read, shell_exec, go_build, go_test, go_vet"
	if got := bb.ChainState["requested_tools"]; got != wantRequested {
		t.Fatalf("ChainState[requested_tools] = %v, want %q", got, wantRequested)
	}
	if got := bb.ChainState["created_tools"]; got != wantRequested {
		t.Fatalf("ChainState[created_tools] = %v, want %q (nothing pre-existing)", got, wantRequested)
	}
	wantTools := []string{"file_read", "shell_exec", "go_build", "go_test", "go_vet"}
	if got := toolNames(bb.ChainTools); !reflect.DeepEqual(got, wantTools) {
		t.Fatalf("ChainTools = %v, want %v", got, wantTools)
	}
	wantAvailable := wantRequested
	if got := bb.ChainState["available_tools"]; got != wantAvailable {
		t.Fatalf("ChainState[available_tools] = %v, want %q", got, wantAvailable)
	}
}

func TestEnsureTaskTools_SkipsToolsAlreadyPresent(t *testing.T) {
	bb := &Blackboard{
		Task:       "run go build and go test for ci",
		ChainTools: buildRealTools("go_build"),
	}
	if got := runFusionAction(t, "EnsureTaskTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	wantRequested := "go_build, go_test, go_vet, shell_exec"
	if got := bb.ChainState["requested_tools"]; got != wantRequested {
		t.Fatalf("ChainState[requested_tools] = %v, want %q", got, wantRequested)
	}
	wantCreated := "go_test, go_vet, shell_exec"
	if got := bb.ChainState["created_tools"]; got != wantCreated {
		t.Fatalf("ChainState[created_tools] = %v, want %q (go_build already present)", got, wantCreated)
	}
	wantTools := []string{"go_build", "go_test", "go_vet", "shell_exec"}
	if got := toolNames(bb.ChainTools); !reflect.DeepEqual(got, wantTools) {
		t.Fatalf("ChainTools = %v, want %v", got, wantTools)
	}
}

func TestEnsureTaskTools_InitializesNilChainState(t *testing.T) {
	bb := &Blackboard{Task: "", ChainState: nil}
	if got := runFusionAction(t, "EnsureTaskTools", bb); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if bb.ChainState == nil {
		t.Fatal("ChainState must be initialized")
	}
}

// --- appendMissingRealTools ---------------------------------------------------

func TestAppendMissingRealTools_NilBlackboardReturnsNil(t *testing.T) {
	if got := appendMissingRealTools(nil, "calculator"); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestAppendMissingRealTools_SkipsEmptyExistingAndUnknownNames(t *testing.T) {
	bb := &Blackboard{ChainTools: buildRealTools("calculator")}
	added := appendMissingRealTools(bb, "", "calculator", "not_a_real_tool", "web_search")
	if !reflect.DeepEqual(added, []string{"web_search"}) {
		t.Fatalf("added = %v, want [web_search]", added)
	}
	if got := toolNames(bb.ChainTools); !reflect.DeepEqual(got, []string{"calculator", "web_search"}) {
		t.Fatalf("ChainTools = %v, want [calculator web_search]", got)
	}
}

func TestAppendMissingRealTools_AllUnknownReturnsEmptySlice(t *testing.T) {
	bb := &Blackboard{}
	added := appendMissingRealTools(bb, "bogus1", "bogus2")
	if len(added) != 0 {
		t.Fatalf("added = %v, want empty", added)
	}
	if len(bb.ChainTools) != 0 {
		t.Fatalf("ChainTools = %v, want empty", bb.ChainTools)
	}
}

// --- inferToolsForTask ---------------------------------------------------------

func TestInferToolsForTask(t *testing.T) {
	tests := []struct {
		name string
		task string
		want []string
	}{
		{
			name: "notebooklm keyword",
			task: "sync the NotebookLM notebook",
			want: []string{
				"notebooklm_server_info", "notebooklm_list", "notebooklm_notebook_get",
				"notebooklm_notebook_query", "notebooklm_research_start",
				"notebooklm_research_status", "notebooklm_research_import",
				"notebooklm_refresh_auth",
			},
		},
		{
			name: "code review keyword",
			task: "review this code for bugs",
			want: []string{"file_read", "shell_exec"},
		},
		{
			name: "go file extension implies code tools",
			task: "check main.go",
			want: []string{"file_read", "shell_exec"},
		},
		{
			name: "build/test/ci keyword",
			task: "run the CI build",
			want: []string{"go_build", "go_test", "go_vet", "shell_exec"},
		},
		{
			name: "research/web/http/url keyword",
			task: "research this topic on the web",
			want: []string{"web_search", "http_get"},
		},
		{
			name: "data/csv/json/extract/pipeline keyword",
			task: "extract data from the csv pipeline",
			want: []string{"file_read", "file_write", "shell_exec", "calculator"},
		},
		{
			name: "graph/graphify/architecture keyword",
			task: "update the graphify architecture",
			want: []string{"graphify", "file_read"},
		},
		{
			name: "no keywords falls back to default set",
			task: "do something unrelated",
			want: []string{"shell_exec", "file_read", "web_search", "calculator"},
		},
		{
			name: "empty task falls back to default set",
			task: "",
			want: []string{"shell_exec", "file_read", "web_search", "calculator"},
		},
		{
			name: "overlapping keywords are deduplicated in first-seen order",
			// "pipeline" also trips the data branch (file_write, calculator)
			// on top of the code and build/test/ci branches.
			task: "build and test this file.go for the ci pipeline",
			want: []string{"file_read", "shell_exec", "go_build", "go_test", "go_vet", "file_write", "calculator"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inferToolsForTask(tt.task); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("inferToolsForTask(%q) = %v, want %v", tt.task, got, tt.want)
			}
		})
	}
}

// --- uniqueStrings --------------------------------------------------------------

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"preserves first-seen order and dedups", []string{"a", "b", "a", "c", "b"}, []string{"a", "b", "c"}},
		{"skips empty strings", []string{"", "a", "", "b"}, []string{"a", "b"}},
		{"empty input returns empty non-nil slice", []string{}, []string{}},
		{"nil input returns empty non-nil slice", nil, []string{}},
		{"all empty returns empty non-nil slice", []string{"", ""}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueStrings(tt.in)
			if got == nil {
				t.Fatal("uniqueStrings must never return nil")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("uniqueStrings(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
