package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCodexAcceptsOperatorManagedGroupWritableInstall(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+codexOutputLastMessageSh), 0700); err != nil {
		t.Fatal(err)
	}
	// npm-global uses a trusted shared operator group (0775 in production).
	if err := os.Chmod(bin, 0775); err != nil {
		t.Fatal(err)
	}
	res := (execCodexRunner{Bin: bin}).RunCodex(context.Background(), dir, "hello")
	if res.Err != nil {
		t.Fatalf("operator-managed install rejected: %v", res.Err)
	}
}

func TestCodexPromptCannotInjectFlags(t *testing.T) {
	args := (execCodexRunner{}).buildCodexArgs("--dangerously-bypass-approvals-and-sandbox", "out")
	if len(args) < 2 || args[len(args)-2] != "--" {
		t.Fatalf("prompt not separated from options: %q", args)
	}
}

func TestCodexRejectsRelativeExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\necho executed\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	res := (execCodexRunner{Bin: "./codex"}).RunCodex(context.Background(), dir, "hello")
	if res.Err == nil || !strings.Contains(res.Err.Error(), "absolute") {
		t.Fatalf("relative executable not rejected: %+v", res)
	}
}

func TestCodexRejectsEscapingOutputSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "secret")
	if err := os.WriteFile(victim, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "codex")
	script := fmt.Sprintf("#!/bin/sh\nwhile [ \"$#\" -gt 1 ]; do\nif [ \"$1\" = \"--output-last-message\" ]; then ln -s %s \"$2\"; exit 0; fi\nshift\ndone\n", strconv.Quote(victim))
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	res := (execCodexRunner{Bin: bin}).RunCodex(context.Background(), dir, "hello")
	if res.Err == nil || res.Output == "secret" {
		t.Fatalf("followed output symlink: %+v", res)
	}
}
