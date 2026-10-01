package evolution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestSnapshotConcurrentProcessesPreserveEveryRevision(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		output bytes.Buffer
	}
	children := make([]*child, 3)
	for writer := range children {
		c := &child{cmd: exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSnapshotProcessHelper$")}
		c.cmd.Env = append(os.Environ(), "BT_TEST_SNAPSHOT_ROOT="+root, fmt.Sprintf("BT_TEST_SNAPSHOT_WRITER=%d", writer))
		c.cmd.Stdout, c.cmd.Stderr = &c.output, &c.output
		if err := c.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		children[writer] = c
	}
	for _, c := range children {
		if err := c.cmd.Wait(); err != nil {
			t.Fatalf("writer: %v\n%s", err, c.output.String())
		}
	}
	revisions, err := ListRevisions("shared", root)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 24 {
		t.Fatalf("revision count=%d want 24", len(revisions))
	}
	seen := make(map[string]bool)
	for _, revision := range revisions {
		tree, err := RestoreTreeRevision("shared", root, revision)
		if err != nil {
			t.Fatal(err)
		}
		if seen[tree.Name] {
			t.Fatalf("revision overwritten: %s", tree.Name)
		}
		seen[tree.Name] = true
	}
}

func TestSnapshotProcessHelper(t *testing.T) {
	root := os.Getenv("BT_TEST_SNAPSHOT_ROOT")
	if root == "" {
		return
	}
	writer := os.Getenv("BT_TEST_SNAPSHOT_WRITER")
	for i := range 8 {
		tree := &SerializableNode{Type: "Action", Name: fmt.Sprintf("%s-%d", writer, i)}
		if _, err := SnapshotTreeWithFitnessContext(t.Context(), tree, "shared", root, float64(i)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshotPreservesUnindexedRevision(t *testing.T) {
	root := t.TempDir()
	orphan := snapshotRevisionPath("tree", root, 1)
	original := []byte(`{"type":"Action","name":"orphan evidence"}`)
	if err := os.WriteFile(orphan, original, 0600); err != nil {
		t.Fatal(err)
	}
	path, err := SnapshotTree(&SerializableNode{Type: "Action", Name: "next"}, "tree", root)
	if err != nil {
		t.Fatal(err)
	}
	if path == orphan {
		t.Fatal("reused unindexed revision")
	}
	actual, err := os.ReadFile(orphan)
	if err != nil || !bytes.Equal(actual, original) {
		t.Fatal("unindexed recovery evidence overwritten")
	}
	revisions, err := ListRevisions("tree", root)
	if err != nil || len(revisions) != 1 || revisions[0] != 2 {
		t.Fatalf("indexed revisions=%v %v", revisions, err)
	}
}

func TestSnapshotDeadlinePreservesRevisionHistory(t *testing.T) {
	root := t.TempDir()
	tree := &SerializableNode{Type: "Action", Name: "before"}
	if _, err := SnapshotTree(tree, "tree", root); err != nil {
		t.Fatal(err)
	}
	release, err := reliability.AcquireFileLockWithContext(t.Context(), snapshotIndexPath("tree", root))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := SnapshotTreeWithContext(ctx, tree, "tree", root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("snapshot error=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("snapshot ignored deadline")
	}
	revisions, err := ListRevisions("tree", root)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("failed snapshot changed history: %v %v", revisions, err)
	}
}

func TestSnapshotRejectsUnsafeNamesAndInvalidIndices(t *testing.T) {
	tree := &SerializableNode{Type: "Action", Name: "safe"}
	for _, name := range []string{"", ".", "..", "nested/name", "foo/../../escape", `nested\name`, "nul\x00name"} {
		root := t.TempDir()
		if _, err := SnapshotTree(tree, name, root); err == nil {
			t.Fatalf("accepted name %q", name)
		}
		if _, err := ListRevisions(name, root); err == nil {
			t.Fatalf("read accepted name %q", name)
		}
	}
	for _, revisions := range [][]int{{0}, {2, 1}, {1, 1}, {math.MaxInt}} {
		root := t.TempDir()
		index := snapshotIndex{Revisions: revisions}
		data, err := json.Marshal(index)
		if err != nil {
			t.Fatal(err)
		}
		path := snapshotIndexPath("tree", root)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := SnapshotTree(tree, "tree", root); err == nil {
			t.Fatalf("accepted revisions %v", revisions)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, data) {
			t.Fatal("invalid index overwritten")
		}
	}
}

func TestSnapshotReadsCannotFollowOutsideSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"type":"Action","name":"outside"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, snapshotRevisionPath("tree", root, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreTreeRevision("tree", root, 1); err == nil {
		t.Fatal("read escaped snapshot root")
	}
}

func TestSnapshotEmptyRootCannotUseWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	tree := &SerializableNode{Type: "Action", Name: "fixture"}
	if _, err := SnapshotTree(tree, "tree", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRevisions("tree", ""); err == nil {
		t.Fatal("empty root read cwd index")
	}
	if _, err := RestoreTreeRevision("tree", "", 1); err == nil {
		t.Fatal("empty root read cwd revision")
	}
	release, err := reliability.AcquireFileLockWithContext(t.Context(), snapshotIndexPath("tree", "."))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	if _, err := SnapshotTreeWithContext(ctx, tree, "tree", ""); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("empty root consulted cwd lock: %v", err)
	}
	revisions, err := ListRevisions("tree", ".")
	if err != nil || len(revisions) != 1 {
		t.Fatal("empty root changed cwd history")
	}
}
