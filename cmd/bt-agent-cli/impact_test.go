package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeImpactCLIFixture materializes a tiny Go module under a temp directory
// so impactedTestsForSource has a real go.mod/import graph to walk.
func writeImpactCLIFixture(t *testing.T) (root, sourceAbs string) {
	t.Helper()
	root = t.TempDir()

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
	return root, filepath.Join(root, "pkg", "file.go")
}

// Regression: a changed-file path handed to the CLI is normally absolute (or
// relative to the caller's cwd), not already module-relative like the impact
// graph indexes it. normalizeImpactSource must convert it before querying.
func TestNormalizeImpactSource_AbsoluteUnderRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "pkg", "file.go")

	rel, err := normalizeImpactSource(root, abs)
	if err != nil {
		t.Fatalf("normalizeImpactSource: %v", err)
	}
	if rel != "pkg/file.go" {
		t.Errorf("rel = %q, want %q", rel, "pkg/file.go")
	}
}

func TestNormalizeImpactSource_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	if _, err := normalizeImpactSource(root, filepath.Join(outside, "x.go")); err == nil {
		t.Error("expected an error for a source path outside root")
	}
}

func TestImpactedTestsForSource_AbsoluteSourceUnderRoot(t *testing.T) {
	root, sourceAbs := writeImpactCLIFixture(t)

	tests, err := impactedTestsForSource(root, sourceAbs)
	if err != nil {
		t.Fatalf("impactedTestsForSource: %v", err)
	}
	if len(tests) != 1 || tests[0] != "pkg/file_test.go" {
		t.Errorf("tests = %v, want [pkg/file_test.go]", tests)
	}
}
