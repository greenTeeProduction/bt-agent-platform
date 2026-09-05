package engine

import (
	"os"
	"path/filepath"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewArc42TerminalFailures(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BT_ARC42_OUTPUT_DIR", blocked)
	for _, tc := range []struct {
		name  string
		state map[string]any
	}{{"ValidateSection", nil}, {"SaveSection", nil}, {"SaveSection", map[string]any{"arc42_section_file": "section.md"}}} {
		bb := &Blackboard{ChainState: tc.state}
		got := bb.actionForName(tc.name)(btcore.NewBTContext(t.Context(), bb))
		if got != -1 {
			t.Errorf("%s = %d, want terminal Failure", tc.name, got)
		}
	}
}
