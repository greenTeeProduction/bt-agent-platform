package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestRegisterScriptNodes_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script PATH shim is POSIX-only")
	}

	dir := t.TempDir()
	writeFakePython(t, dir, `#!/bin/sh
printf 'fake-python:%s %s %s %s' "$1" "$2" "$3" "$4"
exit 0
`)
	t.Setenv("PATH", dir)

	registerScriptNodes()

	t.Run("IndexSessions", func(t *testing.T) {
		bb := &Blackboard{}
		status := actionRegistry["IndexSessions"](&btcore.BTContext[Blackboard]{Blackboard: bb})
		if status != 1 {
			t.Fatalf("status=%d, want success", status)
		}
		if bb.Outcome != "success" {
			t.Fatalf("outcome=%q, want success", bb.Outcome)
		}
		if !strings.Contains(bb.Result, "sessions indexed: fake-python:/mnt/ssd/.hermes/scripts/session_indexer.py index 4 500") {
			t.Fatalf("unexpected result: %q", bb.Result)
		}
	})

	t.Run("ExtractMemories", func(t *testing.T) {
		bb := &Blackboard{}
		status := actionRegistry["ExtractMemories"](&btcore.BTContext[Blackboard]{Blackboard: bb})
		if status != 1 {
			t.Fatalf("status=%d, want success", status)
		}
		if bb.Outcome != "success" {
			t.Fatalf("outcome=%q, want success", bb.Outcome)
		}
		if !strings.Contains(bb.Result, "memory extraction: fake-python:/mnt/ssd/.hermes/scripts/memory_extractor.py run") {
			t.Fatalf("unexpected result: %q", bb.Result)
		}
	})
}

func TestRegisterScriptNodes_Failure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script PATH shim is POSIX-only")
	}

	dir := t.TempDir()
	writeFakePython(t, dir, `#!/bin/sh
echo 'boom from fake python'
exit 7
`)
	t.Setenv("PATH", dir)

	registerScriptNodes()

	t.Run("IndexSessions", func(t *testing.T) {
		bb := &Blackboard{}
		status := actionRegistry["IndexSessions"](&btcore.BTContext[Blackboard]{Blackboard: bb})
		if status != -1 {
			t.Fatalf("status=%d, want failure", status)
		}
		if bb.Outcome != "failure" {
			t.Fatalf("outcome=%q, want failure", bb.Outcome)
		}
		if !strings.Contains(bb.Result, "session indexer failed") || !strings.Contains(bb.Result, "boom from fake python") {
			t.Fatalf("unexpected result: %q", bb.Result)
		}
	})

	t.Run("ExtractMemories", func(t *testing.T) {
		bb := &Blackboard{}
		status := actionRegistry["ExtractMemories"](&btcore.BTContext[Blackboard]{Blackboard: bb})
		if status != -1 {
			t.Fatalf("status=%d, want failure", status)
		}
		if bb.Outcome != "failure" {
			t.Fatalf("outcome=%q, want failure", bb.Outcome)
		}
		if !strings.Contains(bb.Result, "memory extractor failed") || !strings.Contains(bb.Result, "boom from fake python") {
			t.Fatalf("unexpected result: %q", bb.Result)
		}
	})
}

func writeFakePython(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "python3")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
}
