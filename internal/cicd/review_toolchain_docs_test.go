package cicd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewToolchainDocsMatchModule(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	version := ""
	for line := range strings.SplitSeq(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "go "); ok {
			version = strings.TrimSpace(v)
		}
	}
	if version == "" {
		t.Fatal("no go directive")
	}
	for _, file := range []string{"AGENTS.md", "README.md", "docs/TUTORIAL.md", "docs/GETTING_STARTED.md", "docs/arc42/02-constraints.md", "docs/SECURITY_LINTING.md"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), version) {
			t.Errorf("%s does not document required Go %s", file, version)
		}
	}
}
