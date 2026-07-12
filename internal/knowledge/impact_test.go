package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ImpactGraph — directed symbol graph linking source files to relevant tests.
//
// Program "Test-Driven Agentic Development and Graph-Based Impact Analysis"
// milestone 1/4: BuildImpactGraph walks a Go source tree and links each source
// file to the test files its change can affect, using import-based analysis
// (a test whose package imports the source's package) and directory-proximity
// heuristics (a test that lives beside the source).
// =============================================================================

// writeImpactFixture materializes a tiny multi-package Go module under a temp
// directory and returns its root. Layout:
//
//	root/go.mod                 module example.com/proj
//	root/mathx/mathx.go         package mathx (leaf, imported by calc)
//	root/mathx/mathx_test.go    package mathx (same-dir test)
//	root/calc/calc.go           package calc  (imports mathx)
//	root/calc/calc_test.go      package calc_test (imports mathx directly)
func writeImpactFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	files := map[string]string{
		"go.mod": "module example.com/proj\n\ngo 1.26\n",
		"mathx/mathx.go": "package mathx\n\n" +
			"func Add(a, b int) int { return a + b }\n",
		"mathx/mathx_test.go": "package mathx\n\n" +
			"import \"testing\"\n\n" +
			"func TestAdd(t *testing.T) {\n" +
			"\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
		"calc/calc.go": "package calc\n\n" +
			"import \"example.com/proj/mathx\"\n\n" +
			"func Sum(a, b int) int { return mathx.Add(a, b) }\n",
		"calc/calc_test.go": "package calc_test\n\n" +
			"import (\n\t\"testing\"\n\n\t\"example.com/proj/mathx\"\n)\n\n" +
			"func TestSum(t *testing.T) {\n" +
			"\tif mathx.Add(2, 2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n",
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

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestBuildImpactGraph_ProximityEdge(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	// A source file's same-directory test is impacted by directory proximity.
	got := g.TestsFor("mathx/mathx.go")
	if !containsStr(got, "mathx/mathx_test.go") {
		t.Errorf("proximity: mathx/mathx.go should impact mathx/mathx_test.go; got %v", got)
	}
}

func TestBuildImpactGraph_ImportEdge(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	// calc_test.go imports example.com/proj/mathx, so a change to any mathx
	// source file can affect calc's test — an import-based edge.
	got := g.TestsFor("mathx/mathx.go")
	if !containsStr(got, "calc/calc_test.go") {
		t.Errorf("import: mathx/mathx.go should impact calc/calc_test.go; got %v", got)
	}
}

func TestBuildImpactGraph_ProximityForImportingPackage(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	// calc.go's own test lives beside it — proximity edge.
	got := g.TestsFor("calc/calc.go")
	if !containsStr(got, "calc/calc_test.go") {
		t.Errorf("proximity: calc/calc.go should impact calc/calc_test.go; got %v", got)
	}
}

func TestBuildImpactGraph_NoEdgeToNonTest(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	// The graph links source files to *test* files only; a non-test file such
	// as calc/calc.go must never appear as an impacted target.
	for _, target := range g.TestsFor("mathx/mathx.go") {
		if target == "calc/calc.go" {
			t.Errorf("mathx/mathx.go should not list non-test calc/calc.go as impacted: %v",
				g.TestsFor("mathx/mathx.go"))
		}
	}
}

func TestBuildImpactGraph_Deterministic(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	// Repeated queries return a stable, sorted ordering — map iteration order
	// must never leak into results.
	first := g.TestsFor("mathx/mathx.go")
	second := g.TestsFor("mathx/mathx.go")
	if len(first) != len(second) {
		t.Fatalf("non-deterministic length: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("non-deterministic order: %v vs %v", first, second)
		}
	}
	// Sanity: expect both known targets present.
	if !containsStr(first, "mathx/mathx_test.go") || !containsStr(first, "calc/calc_test.go") {
		t.Errorf("expected both mathx_test.go and calc_test.go; got %v", first)
	}
}

func TestBuildImpactGraph_UnknownSourceHasNoTests(t *testing.T) {
	root := writeImpactFixture(t)

	g, err := BuildImpactGraph(root)
	if err != nil {
		t.Fatalf("BuildImpactGraph: %v", err)
	}

	if got := g.TestsFor("does/not/exist.go"); len(got) != 0 {
		t.Errorf("unknown source file should have no impacted tests; got %v", got)
	}
}

// ImpactedTests is the single build+query entry point CLI and MCP consumers
// share, so a change to a file can gate a scoped test list instead of always
// running the full suite (milestone: production consumer of the impact graph).
func TestImpactedTests_BuildsGraphAndQueries(t *testing.T) {
	root := writeImpactFixture(t)

	got, err := ImpactedTests(root, "mathx/mathx.go")
	if err != nil {
		t.Fatalf("ImpactedTests: %v", err)
	}
	if !containsStr(got, "mathx/mathx_test.go") {
		t.Errorf("ImpactedTests(mathx/mathx.go) missing proximity target; got %v", got)
	}
	if !containsStr(got, "calc/calc_test.go") {
		t.Errorf("ImpactedTests(mathx/mathx.go) missing import target; got %v", got)
	}
}

func TestImpactedTests_UnknownSourceHasNoTests(t *testing.T) {
	root := writeImpactFixture(t)

	got, err := ImpactedTests(root, "does/not/exist.go")
	if err != nil {
		t.Fatalf("ImpactedTests: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown source file should have no impacted tests; got %v", got)
	}
}

func TestImpactedTests_PropagatesBuildError(t *testing.T) {
	if _, err := ImpactedTests(t.TempDir(), "whatever.go"); err == nil {
		t.Error("expected an error when root has no go.mod")
	}
}
