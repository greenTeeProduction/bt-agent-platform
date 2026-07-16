package domains

import (
	"strings"
	"testing"
)

func TestArc42DocsyncTreeShape(t *testing.T) {
	tree := Arc42DocsyncTree()
	if tree.Type != "Sequence" {
		t.Fatalf("root must be a Sequence, got %s", tree.Type)
	}
	if len(tree.Children) != 13 {
		t.Fatalf("want 13 children (12 sections + README), got %d", len(tree.Children))
	}
	for i := 0; i < 12; i++ {
		want := "SyncArc42Section"
		if !strings.HasPrefix(tree.Children[i].Name, want) {
			t.Errorf("child %d = %q, want prefix %q", i, tree.Children[i].Name, want)
		}
	}
	if tree.Children[12].Name != "SyncReadme" {
		t.Errorf("last child = %q, want SyncReadme", tree.Children[12].Name)
	}
}

func TestArc42DocsyncTreeRegistered(t *testing.T) {
	trees := AllDomainTrees()
	if _, ok := trees["arc42:docsync"]; !ok {
		t.Error("arc42:docsync missing from the domain tree map")
	}
}
