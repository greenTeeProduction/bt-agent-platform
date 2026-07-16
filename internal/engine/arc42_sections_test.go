package engine

// The manifest is the Go source of truth for the per-section sync layer and
// is pinned against the REAL repo docs (convention:
// TestLoadArc42QualityGoalsAgainstRealRepoDoc) so the docs, the manifest,
// and scripts/check-doc-drift.sh's heading list cannot drift apart silently.
// This also covers the spec's "repo hygiene" bullet: 12 sections present,
// retired artifacts absent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArc42SectionsManifestShape(t *testing.T) {
	if len(arc42Sections) != 12 {
		t.Fatalf("want 12 sections, got %d", len(arc42Sections))
	}
	for i, sec := range arc42Sections {
		if sec.Num != i+1 {
			t.Errorf("section %d has Num %d", i+1, sec.Num)
		}
		if sec.File == "" || sec.Title == "" || len(sec.RequiredHeadings) == 0 {
			t.Errorf("section %d incomplete: %+v", i+1, sec)
		}
	}
}

func TestArc42SectionsAgainstRealRepoDocs(t *testing.T) {
	root := goModuleRoot()
	dir := filepath.Join(root, "docs", "arc42")
	for _, sec := range arc42Sections {
		body, err := os.ReadFile(filepath.Join(dir, sec.File))
		if err != nil {
			t.Fatalf("section %d file missing: %v", sec.Num, err)
		}
		for _, h := range sec.RequiredHeadings {
			if !strings.Contains(string(body), h) {
				t.Errorf("%s missing required heading %q", sec.File, h)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "go-bt-evolve-arc42.md")); err == nil {
		t.Error("retired monolith go-bt-evolve-arc42.md still present")
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "adr")); err == nil {
		t.Error("retired docs/adr directory still present")
	}
	if _, err := os.Stat(filepath.Join(dir, "GUIDELINES.md")); err != nil {
		t.Error("docs/arc42/GUIDELINES.md missing")
	}
}
