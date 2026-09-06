package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Exercise the real exec/result handling without building this checkout or
// running an installed graphify. These tests must remain non-parallel: Setenv
// changes process-wide state. An isolated PATH prevents accidental fallthrough.
type realToolCommandFixture struct {
	repo string
	args string
	cwd  string
}

func newRealToolCommandFixture(t *testing.T, name, body string) realToolCommandFixture {
	t.Helper()
	dir := t.TempDir()
	f := realToolCommandFixture{
		repo: filepath.Join(dir, "fixture repo"),
		args: filepath.Join(dir, "args"),
		cwd:  filepath.Join(dir, "cwd"),
	}
	bin := filepath.Join(dir, "bin")
	for _, path := range []string{f.repo, bin} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.repo, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nset -eu\nprintf '%s\\0' \"$@\" > \"$BT_TOOL_FIXTURE_ARGS\"\npwd -P > \"$BT_TOOL_FIXTURE_CWD\"\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("BT_MODULE_ROOT", f.repo)
	t.Setenv("BT_TOOL_FIXTURE_ARGS", f.args)
	t.Setenv("BT_TOOL_FIXTURE_CWD", f.cwd)
	return f
}

func (f realToolCommandFixture) assertInvocation(t *testing.T, want ...string) {
	t.Helper()
	data, err := os.ReadFile(f.args)
	if err != nil {
		t.Fatalf("fixture executable did not record arguments: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %#v, want %#v", got, want)
	}
	data, err = os.ReadFile(f.cwd)
	if err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != wantDir {
		t.Errorf("working directory = %q, want %q", got, wantDir)
	}
}

func assertGraphifyFixtureCall(t *testing.T, input string, want ...string) {
	t.Helper()
	f := newRealToolCommandFixture(t, "graphify", "printf '  fixture graph output  \\n'")
	if got := newGraphifyTool().Call(input); got != "fixture graph output" {
		t.Fatalf("result = %q, want fixture graph output", got)
	}
	f.assertInvocation(t, want...)
}

func TestNewGraphifyTool_FixtureArguments(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		args        []string
	}{
		{"empty", "", []string{"query", ""}},
		{"bare question", "how are nodes related?", []string{"query", "how are nodes related?"}},
		{"update path", "update ./a directory", []string{"update", "./a directory"}},
		{"path", "path BuildTree   RunTree", []string{"path", "BuildTree", "RunTree"}},
	} {
		t.Run(tc.name, func(t *testing.T) { assertGraphifyFixtureCall(t, tc.input, tc.args...) })
	}
}

func TestRealCommandTools_FixtureResults(t *testing.T) {
	for _, tool := range []struct {
		name   string
		make   func() *realTool
		limit  int
		marker string
	}{
		{"go", newGoBuildTool, 4096, "\n... [truncated]"},
		{"graphify", newGraphifyTool, 8192, "\n... [output truncated]"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			t.Run("stderr failure", func(t *testing.T) {
				newRealToolCommandFixture(t, tool.name, "printf '  fixture failure  \\n' >&2\nexit 23")
				if got := tool.make().Call(""); got != "fixture failure" {
					t.Fatalf("result = %q, want fixture failure", got)
				}
			})
			t.Run("truncation", func(t *testing.T) {
				newRealToolCommandFixture(t, tool.name, "printf '%s' '"+strings.Repeat("x", tool.limit+100)+"'")
				want := strings.Repeat("x", tool.limit) + tool.marker
				if got := tool.make().Call(""); got != want {
					t.Fatalf("truncation mismatch: got %d bytes, want %d", len(got), len(want))
				}
			})
		})
	}
}

func TestNewGraphifyTool_EmptyFailure(t *testing.T) {
	newRealToolCommandFixture(t, "graphify", "exit 23")
	if got := newGraphifyTool().Call("query test"); got != "graphify error: exit status 23" {
		t.Fatalf("unexpected failure result: %q", got)
	}
}

func TestRealCommandTools_PreCancelled(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() *realTool
	}{
		{"go", newGoBuildTool}, {"graphify", newGraphifyTool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRealToolCommandFixture(t, tc.name, "exit 0")
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if got := tc.make().CallContext(ctx, ""); got != "tool cancelled: context canceled" {
				t.Fatalf("unexpected cancellation result: %q", got)
			}
			if _, err := os.Stat(f.args); !os.IsNotExist(err) {
				t.Fatalf("pre-cancelled tool started executable: %v", err)
			}
		})
	}
}

func TestRealCommandTools_ParentDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func() *realTool
		args []string
	}{
		{"go", newGoBuildTool, []string{"build", "./..."}},
		{"graphify", newGraphifyTool, []string{"query", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// exec avoids orphaned children retaining output pipes. The fixture
			// emits a diagnostic before blocking, so interrupted output is checked.
			f := newRealToolCommandFixture(t, tc.name, "printf 'fixture started\\n'\nexec /bin/sleep 10")
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			start := time.Now()
			got := tc.make().CallContext(ctx, "")
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("parent deadline ignored: took %v", elapsed)
			}
			if ctx.Err() != context.DeadlineExceeded {
				t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
			}
			if got != "fixture started" {
				t.Fatalf("interrupted output = %q, want fixture started", got)
			}
			f.assertInvocation(t, tc.args...)
		})
	}
}
