package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Characterization tests for the NotebookLM zero-LLM action nodes in
// actions_notebooklm.go. ResearchNotebookLM's novelty gate and success-marks-
// done paths are already pinned in nlm_research_novelty_test.go, and
// CheckNotebookLMAuthAndRefresh in nlm_auth_refresh_test.go; this file covers
// the remaining actions and helpers, plus a few ResearchNotebookLM branches
// those files don't reach.

// --- ListNotebookLMNotebooks ------------------------------------------------

func TestListNotebookLMNotebooks_StoresListInChainState(t *testing.T) {
	old := nlmRun
	var calledArgs []string
	nlmRun = func(_ time.Duration, args ...string) string {
		calledArgs = args
		return `[{"id":"nb-1","title":"Research"}]`
	}
	t.Cleanup(func() { nlmRun = old })

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := runFusionAction(t, "ListNotebookLMNotebooks", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	if strings.Join(calledArgs, " ") != "notebook list --json" {
		t.Fatalf("nlm invoked with %v, want [notebook list --json]", calledArgs)
	}
	if bb.ChainState["nlm_notebook_list"] != `[{"id":"nb-1","title":"Research"}]` {
		t.Fatalf("ChainState[nlm_notebook_list] = %v", bb.ChainState["nlm_notebook_list"])
	}
	if !strings.Contains(bb.Result, "nb-1") {
		t.Fatalf("result must embed the notebook list: %s", bb.Result)
	}
}

// --- GetNotebookLMNotebook --------------------------------------------------

func TestGetNotebookLMNotebook_UsesDefaultNotebookID(t *testing.T) {
	old := nlmRun
	var calledArgs []string
	nlmRun = func(_ time.Duration, args ...string) string {
		calledArgs = args
		return `{"id":"nb-1","sourceCount":3}`
	}
	t.Cleanup(func() { nlmRun = old })

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := runFusionAction(t, "GetNotebookLMNotebook", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	wantArgs := "notebook get " + defaultNotebook + " --json"
	if strings.Join(calledArgs, " ") != wantArgs {
		t.Fatalf("nlm invoked with %q, want %q", strings.Join(calledArgs, " "), wantArgs)
	}
	if bb.ChainState["nlm_notebook_id"] != defaultNotebook {
		t.Fatalf("ChainState[nlm_notebook_id] = %v, want %s", bb.ChainState["nlm_notebook_id"], defaultNotebook)
	}
	if bb.ChainState["nlm_notebook_get"] != `{"id":"nb-1","sourceCount":3}` {
		t.Fatalf("ChainState[nlm_notebook_get] = %v", bb.ChainState["nlm_notebook_get"])
	}
}

// --- QueryNotebookLM ---------------------------------------------------------

func TestQueryNotebookLM_QueriesDefaultNotebookWithTaskAsQuestion(t *testing.T) {
	old := nlmRun
	var calledArgs []string
	nlmRun = func(_ time.Duration, args ...string) string {
		calledArgs = args
		return "The answer is 42."
	}
	t.Cleanup(func() { nlmRun = old })

	bb := &Blackboard{Task: "what is the meaning of life?"}
	if got := runFusionAction(t, "QueryNotebookLM", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	wantArgs := []string{"notebook", "query", defaultNotebook, "what is the meaning of life?"}
	if strings.Join(calledArgs, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("nlm invoked with %v, want %v", calledArgs, wantArgs)
	}
	if !strings.Contains(bb.Result, "NotebookLM Query") || !strings.Contains(bb.Result, "The answer is 42.") {
		t.Fatalf("result must embed the query answer: %s", bb.Result)
	}
}

// --- SaveNotebookLMFindings --------------------------------------------------
// Unlike ResearchNotebookLM (which writes through the injectable
// nlmResearchSynthesesDir var so tests never touch the live research vault),
// SaveNotebookLMFindings must also write through an injectable directory var
// — otherwise invoking this action for real in a test writes into the live
// /mnt/ssd/clawd/wiki/bt-research/syntheses vault.

func TestSaveNotebookLMFindings_WritesReportUnderInjectableDir(t *testing.T) {
	dir := t.TempDir()
	old := nlmFindingsSaveDir
	nlmFindingsSaveDir = dir
	t.Cleanup(func() { nlmFindingsSaveDir = old })

	bb := &Blackboard{Task: "daily findings task", Result: "prior accumulated results", ChainState: map[string]any{}}
	if got := runFusionAction(t, "SaveNotebookLMFindings", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	savePath, _ := bb.ChainState["nlm_save_path"].(string)
	if savePath == "" || !strings.HasPrefix(savePath, dir) {
		t.Fatalf("ChainState[nlm_save_path] = %q, want a path under %q", savePath, dir)
	}
	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("saved file must exist: %v", err)
	}
	if !strings.Contains(string(data), "daily findings task") || !strings.Contains(string(data), "prior accumulated results") {
		t.Fatalf("saved content must include the task and prior results: %s", data)
	}
	if !strings.Contains(bb.Result, "Saved to") {
		t.Fatalf("result must confirm the save: %s", bb.Result)
	}
}

func TestSaveNotebookLMFindings_SaveErrorFailsWithoutDataLoss(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	old := nlmFindingsSaveDir
	nlmFindingsSaveDir = blocker // exists as a file, so MkdirAll must fail
	t.Cleanup(func() { nlmFindingsSaveDir = old })

	bb := &Blackboard{Task: "t", Result: "r", ChainState: map[string]any{}}
	if got := runFusionAction(t, "SaveNotebookLMFindings", bb); got != -1 {
		t.Fatalf("status = %d, want -1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "failure" {
		t.Fatalf("outcome = %q, want failure", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "Save error") {
		t.Fatalf("result must report the save error: %s", bb.Result)
	}
}

// --- ResearchNotebookLM: branches not covered by nlm_research_novelty_test.go ---

func TestResearchNotebookLM_MissingTaskIDFailsBeforeStatusPoll(t *testing.T) {
	withNlmEconomy(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC))

	var calls []string
	old := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "research" && args[1] == "start" {
			return "no task id in this output"
		}
		return "{}"
	}
	t.Cleanup(func() { nlmRun = old })

	bb := &Blackboard{Task: "task id extraction failure characterization test", ChainState: map[string]any{}}
	if got := runFusionAction(t, "ResearchNotebookLM", bb); got != -1 {
		t.Fatalf("status = %d, want -1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "failure" {
		t.Fatalf("outcome = %q, want failure", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "Could not extract task_id") {
		t.Fatalf("result must explain the extraction failure: %s", bb.Result)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "research status") || strings.HasPrefix(c, "research import") {
			t.Fatalf("must not poll status or import without a task_id, but called: %v", calls)
		}
	}
}

func TestResearchNotebookLM_ImportRequestsCitedOnlyWhenStatusMentionsCited(t *testing.T) {
	withNlmEconomy(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 29, 13, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	old := nlmResearchSynthesesDir
	nlmResearchSynthesesDir = dir
	t.Cleanup(func() { nlmResearchSynthesesDir = old })

	var importArgs []string
	oldRun := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		switch {
		case args[0] == "research" && args[1] == "start":
			return "task_id: cited-run-1"
		case args[0] == "research" && args[1] == "status":
			return "status: completed, 5 cited sources"
		case args[0] == "research" && args[1] == "import":
			importArgs = args
			return "imported 5 sources"
		}
		return "{}"
	}
	t.Cleanup(func() { nlmRun = oldRun })

	bb := &Blackboard{Task: "cited-only import routing characterization test", ChainState: map[string]any{}}
	if got := runFusionAction(t, "ResearchNotebookLM", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	found := false
	for _, a := range importArgs {
		if a == "--cited-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("import args %v must include --cited-only when status mentions cited sources", importArgs)
	}
}

func TestResearchNotebookLM_ImportOmitsCitedOnlyWithoutCitedStatus(t *testing.T) {
	withNlmEconomy(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC))
	dir := t.TempDir()
	old := nlmResearchSynthesesDir
	nlmResearchSynthesesDir = dir
	t.Cleanup(func() { nlmResearchSynthesesDir = old })

	var importArgs []string
	oldRun := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		switch {
		case args[0] == "research" && args[1] == "start":
			return "task_id: uncited-run-1"
		case args[0] == "research" && args[1] == "status":
			return "status: completed, no citation info"
		case args[0] == "research" && args[1] == "import":
			importArgs = args
			return "imported all sources"
		}
		return "{}"
	}
	t.Cleanup(func() { nlmRun = oldRun })

	bb := &Blackboard{Task: "uncited import routing characterization test", ChainState: map[string]any{}}
	if got := runFusionAction(t, "ResearchNotebookLM", bb); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	for _, a := range importArgs {
		if a == "--cited-only" {
			t.Fatalf("import args %v must not include --cited-only without cited status", importArgs)
		}
	}
}

func TestResearchNotebookLM_VaultSaveErrorDoesNotFailTheRun(t *testing.T) {
	withNlmEconomy(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 29, 15, 0, 0, 0, time.UTC))

	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	old := nlmResearchSynthesesDir
	nlmResearchSynthesesDir = blocker
	t.Cleanup(func() { nlmResearchSynthesesDir = old })

	oldRun := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		if args[0] == "research" && args[1] == "start" {
			return "task_id: save-error-run-1"
		}
		return "{}"
	}
	t.Cleanup(func() { nlmRun = oldRun })

	bb := &Blackboard{Task: "vault save error resilience characterization test", ChainState: map[string]any{}}
	if got := runFusionAction(t, "ResearchNotebookLM", bb); got != 1 {
		t.Fatalf("status = %d, want 1 (a vault save error must not fail the run); result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success despite the save error", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "Save error") {
		t.Fatalf("result must still surface the save error: %s", bb.Result)
	}
}

// --- extractTaskID -----------------------------------------------------------

func TestExtractTaskID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "labeled field with quotes and trailing comma",
			output: "{\n  \"task_id\": \"1234abcd-1234-abcd-1234-abcdefabcdef\",\n  \"status\": \"running\"\n}",
			want:   "1234abcd-1234-abcd-1234-abcdefabcdef",
		},
		{
			name:   "plain colon form",
			output: "task_id: abc-123-def-456\nstatus: running",
			want:   "abc-123-def-456",
		},
		{
			name:   "fallback to bare UUID when no labeled field present",
			output: "Started research run 12345678-1234-1234-1234-123456789012 for the notebook.",
			want:   "12345678-1234-1234-1234-123456789012",
		},
		{
			name:   "empty labeled value falls through to UUID fallback",
			output: "task_id:\nrun id 87654321-4321-4321-4321-cba987654321 accepted",
			want:   "87654321-4321-4321-4321-cba987654321",
		},
		{
			name:   "nothing extractable returns empty string",
			output: "no identifiers here at all",
			want:   "",
		},
		{
			name:   "empty output returns empty string",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTaskID(tt.output); got != tt.want {
				t.Fatalf("extractTaskID(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

// --- writeString --------------------------------------------------------------

func TestWriteString_CreatesParentDirsAndWritesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "out.md")
	if err := writeString(path, "hello world"); err != nil {
		t.Fatalf("writeString error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file must exist: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q, want %q", string(data), "hello world")
	}
}

func TestWriteString_ErrorsWhenParentIsARegularFile(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeString(filepath.Join(blocker, "child.md"), "content"); err == nil {
		t.Fatal("writeString must error when a path component is a regular file")
	}
}

// --- nlmResearchQueryKeyContent ------------------------------------------------

func TestNlmResearchQueryKeyContent(t *testing.T) {
	if got := nlmResearchQueryKeyContent("multi-agent coordination"); got != "nlm research query: multi-agent coordination" {
		t.Fatalf("got %q", got)
	}
}
