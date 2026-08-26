package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// writeImpactMCPFixture materializes a tiny Go module under a temp directory
// so bt_impact_tests has a real go.mod/import graph to walk.
func writeImpactMCPFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod": "module example.com/proj\n\ngo 1.26\n",
		"pkg/file.go": "package pkg\n\n" +
			"func F() int { return 1 }\n",
		"pkg/file_test.go": "package pkg\n\n" +
			"import \"testing\"\n\n" +
			"func TestF(t *testing.T) {\n" +
			"\tif F() != 1 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
	}
	for rel, body := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// Change-scoped test gating (NotebookLM research: the impact graph had zero
// production consumers). impactTests wraps knowledge.ImpactedTests as the
// pure handler bt_impact_tests registers, so a caller can gate commits on a
// scoped test list instead of always running the full suite.
func TestImpactTests_ReturnsImpactedTestsForSource(t *testing.T) {
	root := writeImpactMCPFixture(t)

	result := impactTests(root, "pkg/file.go")
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}
	tests, _ := result["tests"].([]string)
	if len(tests) != 1 || tests[0] != "pkg/file_test.go" {
		t.Errorf("tests = %v, want [pkg/file_test.go]", result["tests"])
	}
}

func TestImpactTests_RejectsMissingSource(t *testing.T) {
	root := writeImpactMCPFixture(t)

	result := impactTests(root, "")
	if result["error"] == nil {
		t.Error("missing source must error")
	}
}

// Regression: a changed-file path handed to the MCP tool is normally
// absolute (or relative to the caller's cwd), not already module-relative
// like the impact graph indexes it. impactTests must normalize it (via
// knowledge.NormalizeImpactSource) before querying, matching the CLI
// sibling (cmd/bt-agent-cli/impact.go's impactedTestsForSource).
func TestImpactTests_NormalizesAbsoluteSourceUnderRoot(t *testing.T) {
	root := writeImpactMCPFixture(t)
	sourceAbs := filepath.Join(root, "pkg", "file.go")

	result := impactTests(root, sourceAbs)
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}
	tests, _ := result["tests"].([]string)
	if len(tests) != 1 || tests[0] != "pkg/file_test.go" {
		t.Errorf("tests = %v, want [pkg/file_test.go]", result["tests"])
	}
}

// Regression: a source path outside root must be an honest {"error": ...}
// like the CLI's normalizeImpactSource rejection, not a silent tests:[]
// that looks like "no tests are impacted".
func TestImpactTests_RejectsSourceOutsideRoot(t *testing.T) {
	root := writeImpactMCPFixture(t)
	outside := t.TempDir()
	source := filepath.Join(outside, "other.go")

	result := impactTests(root, source)
	if result["error"] == nil {
		t.Errorf("impactTests(%q, %q): expected error for path outside root, got tests=%v", root, source, result["tests"])
	}
}

func TestBTImpactTestsRegistered(t *testing.T) {
	server := engine.NewServer("test")
	registerMCPTools(server, &mcpDeps{})
	if !server.HasTool("bt_impact_tests") {
		t.Fatal("bt_impact_tests must be registered by registerMCPTools")
	}

	root := writeImpactMCPFixture(t)
	args := json.RawMessage(fmt.Sprintf(`{"root":%q,"source":"pkg/file.go"}`, root))
	res, ok := server.Invoke("bt_impact_tests", args)
	if !ok || res == nil || len(res.Content) == 0 {
		t.Fatal("bt_impact_tests returned no content")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	tests, _ := out["tests"].([]any)
	if len(tests) != 1 || tests[0] != "pkg/file_test.go" {
		t.Errorf("tests = %v, want [pkg/file_test.go]", out["tests"])
	}
}
