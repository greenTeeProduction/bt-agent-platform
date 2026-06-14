package domains

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type agentTemplateYAML struct {
	Name     string            `yaml:"name"`
	Tree     string            `yaml:"tree"`
	Metadata map[string]string `yaml:"metadata"`
}

func findRepoTemplatesDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "agents", "templates")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("agents/templates not found from test cwd")
	return ""
}

// TestAllTemplatesResolveTree ensures shipped agent templates bind to resolvable trees.
func TestAllTemplatesResolveTree(t *testing.T) {
	tmplDir := findRepoTemplatesDir(t)
	entries, err := os.ReadDir(tmplDir)
	if err != nil {
		t.Fatal(err)
	}

	var checked, skipped int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tmplDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var tpl agentTemplateYAML
		if err := yaml.Unmarshal(data, &tpl); err != nil {
			t.Fatalf("%s: parse yaml: %v", e.Name(), err)
		}
		if tpl.Metadata["rest_only"] == "true" {
			skipped++
			continue
		}
		if tpl.Tree == "" {
			t.Errorf("%s: missing tree field", e.Name())
			continue
		}
		checked++
		if strings.HasPrefix(tpl.Tree, "domain:") {
			name := tpl.Tree[7:]
			if AllDomainTrees()[name] == nil {
				t.Errorf("%s: tree %q not in AllDomainTrees", e.Name(), tpl.Tree)
			}
			continue
		}
		if ResolveTreeID(tpl.Tree) == nil {
			t.Errorf("%s: tree %q did not resolve", e.Name(), tpl.Tree)
		}
	}
	if checked == 0 {
		t.Fatal("no templates checked")
	}
	t.Logf("checked %d templates, skipped %d rest_only", checked, skipped)
}
