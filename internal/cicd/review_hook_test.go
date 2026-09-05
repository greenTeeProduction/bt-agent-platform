package cicd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewHookSanitizesGitEnvironment(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "git-hooks", "pre-commit")
	if baseline := os.Getenv("REVIEW_HOOK_UNDER_TEST"); baseline != "" {
		path = baseline
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(body, []byte("TEST_ENV=(env)"))
	if start < 0 || !strings.Contains(string(body), `"${TEST_ENV[@]}" $GO test`) {
		t.Fatal("hook does not isolate the test subprocess Git environment")
	}
	snippet := strings.SplitN(string(body)[start:], "\n\n", 2)[0]
	cmd := exec.Command("bash", "-c", snippet+"\n\"${TEST_ENV[@]}\" env")
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "NORMAL=retained", "GIT_DIR=outer.git", "GIT_INDEX_FILE=outer-index", "GIT_CONFIG_PARAMETERS='core.bare=true'"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook isolation: %v %s", err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.HasPrefix(line, "GIT_") {
			t.Errorf("test subprocess inherited %s", line)
		}
	}
	if !strings.Contains(string(out), "NORMAL=retained") {
		t.Fatal("normal environment lost")
	}
}
