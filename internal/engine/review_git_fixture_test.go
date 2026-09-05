package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewGitFixtureIgnoresOuterEnvironment(t *testing.T) {
	outer := t.TempDir()
	index := filepath.Join(outer, "index")
	sentinel := []byte("outer index must remain untouched")
	if err := os.WriteFile(index, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_INDEX_FILE", index)
	t.Setenv("GIT_DIR", filepath.Join(outer, "outer.git"))
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.bare=true'")
	t.Run("review", func(t *testing.T) { newReviewTestRepo(t) })
	t.Run("precheck", TestGoapFusionRepoFileState_AgainstRealGit)
	got, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, sentinel) {
		t.Fatal("outer index modified")
	}
}
