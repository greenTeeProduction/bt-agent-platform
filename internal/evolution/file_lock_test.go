package evolution

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The tests below target the persistence *behavior* of this package rather
// than any one helper, so the package is free to delegate locking and atomic
// writes to the canonical owners (reliability.AcquireFileLock and
// util.SaveJSONAtomic) without the tests having to move with it.

// TestPersistLeavesNoLockSidecar pins that a completed write leaves nothing
// but the persisted artifact behind. The canonical file lock unlinks its
// `<path>.lock` sidecar on release; a local flock that only closes the
// descriptor leaks one sidecar per persisted artifact forever.
func TestPersistLeavesNoLockSidecar(t *testing.T) {
	root := t.TempDir()

	eb, err := NewExperienceBank(filepath.Join(root, "experience"))
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	tree := &SerializableNode{Type: "sequence", Name: "root"}
	if err := eb.AddFromMutation(tree, MutationOp{Operation: "add_before", Target: "N1"}, 0.3, 0.5, nil); err != nil {
		t.Fatalf("AddFromMutation: %v", err)
	}
	if err := eb.Persist(); err != nil {
		t.Fatalf("ExperienceBank.Persist: %v", err)
	}

	ek := NewExpertKnowledge()
	ek.Observe("wrap_retry", "GoDev", 0.4)
	if err := ek.Save(filepath.Join(root, "expert", "archive.json")); err != nil {
		t.Fatalf("ExpertKnowledge.Save: %v", err)
	}

	so := NewSelectorOptimizer(OrderByHybrid)
	so.Record("sel", NodeExecutionRecord{NodeName: "child_a", Outcome: "success"})
	statsPath := filepath.Join(root, "selector", "stats.json")
	if err := os.MkdirAll(filepath.Dir(statsPath), 0o755); err != nil {
		t.Fatalf("mkdir selector dir: %v", err)
	}
	if err := so.SaveSelectorStats(statsPath); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}

	var strays []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".lock") || strings.HasSuffix(path, ".tmp") {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			strays = append(strays, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(strays) > 0 {
		t.Fatalf("persisted writes left %d stray sidecar/temp file(s) behind: %v", len(strays), strays)
	}
}

// TestConcurrentPersistDoesNotLoseEntries pins the lock→merge→write ordering
// of the exported ExperienceBank write path. Each writer owns its own bank
// (its own sync.RWMutex) over one shared file, so nothing but the
// cross-process file lock keeps a full-file rewrite from clobbering a
// sibling's entry between its merge and its rename.
func TestConcurrentPersistDoesNotLoseEntries(t *testing.T) {
	const writers = 12

	dir := filepath.Join(t.TempDir(), "experience")
	banks := make([]*ExperienceBank, writers)
	for i := range banks {
		eb, err := NewExperienceBank(dir)
		if err != nil {
			t.Fatalf("NewExperienceBank(%d): %v", i, err)
		}
		banks[i] = eb
	}

	tree := &SerializableNode{Type: "sequence", Name: "root"}
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i, eb := range banks {
		wg.Add(1)
		go func(i int, eb *ExperienceBank) {
			defer wg.Done()
			op := MutationOp{Operation: "add_before", Target: fmt.Sprintf("node_%02d", i)}
			errs[i] = eb.AddFromMutation(tree, op, 0.3, 0.5, nil)
		}(i, eb)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d AddFromMutation: %v", i, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(dir, "experience.json"))
	if err != nil {
		t.Fatalf("read persisted bank: %v", err)
	}
	var wrapper struct {
		Entries []ExperienceEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse persisted bank: %v", err)
	}
	seen := make(map[string]bool, len(wrapper.Entries))
	for _, e := range wrapper.Entries {
		seen[e.TargetNode] = true
	}
	var missing []string
	for i := range writers {
		if target := fmt.Sprintf("node_%02d", i); !seen[target] {
			missing = append(missing, target)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("concurrent writes lost %d/%d entries: missing %v (file holds %d entries)",
			len(missing), writers, missing, len(wrapper.Entries))
	}
}

// TestSnapshotFilesKeepRestrictivePerms pins the deliberately tighter
// permissions on the quality-gate snapshot artifacts: 0600 files inside an
// 0700 directory. A migration onto a 0644/0755 atomic-write helper would
// silently widen them, so the tighter mode has to be requested explicitly.
func TestSnapshotFilesKeepRestrictivePerms(t *testing.T) {
	snapshotDir := filepath.Join(t.TempDir(), "snapshots")
	tree := &SerializableNode{Type: "sequence", Name: "root"}

	revisionPath, err := SnapshotTreeWithFitness(tree, "perm_tree", snapshotDir, 42.0)
	if err != nil {
		t.Fatalf("SnapshotTreeWithFitness: %v", err)
	}

	dirInfo, err := os.Stat(snapshotDir)
	if err != nil {
		t.Fatalf("stat snapshot dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("snapshot dir perm = %#o, want %#o", got, 0o700)
	}

	for _, path := range []string{revisionPath, snapshotIndexPath("perm_tree", snapshotDir)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s perm = %#o, want %#o", filepath.Base(path), got, 0o600)
		}
	}
}

// TestEvolutionDeclaresNoLocalPersistencePrimitives is a source drift guard.
// Locking and atomic JSON writes have exactly one owner each
// (reliability.AcquireFileLock, util.SaveJSONAtomic); this fails the build if
// a future change re-hand-rolls either idiom inside this package.
func TestEvolutionDeclaresNoLocalPersistencePrimitives(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		violations = append(violations, scanPersistencePrimitives(fset, file)...)
	}

	if len(violations) > 0 {
		t.Fatalf("internal/evolution re-implements persistence primitives that live elsewhere:\n  %s\n\n"+
			"use reliability.AcquireFileLock for the sidecar flock and util.SaveJSONAtomic{,Mode} for the tmp+rename write.",
			strings.Join(violations, "\n  "))
	}
}

// scanPersistencePrimitives reports the three shapes that mean this package
// grew its own copy of a shared persistence primitive: a raw syscall.Flock, a
// redeclared acquireExperienceLock, or an os.Rename committing a `+ ".tmp"`
// staging path (either inline or through a local alias, which is how every
// pre-migration site was written).
func scanPersistencePrimitives(fset *token.FileSet, file *ast.File) []string {
	var out []string
	at := func(pos token.Pos) string { return fset.Position(pos).String() }

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if isPkgSelector(node, "syscall", "Flock") {
				out = append(out, at(node.Pos())+": references syscall.Flock directly")
			}
		case *ast.FuncDecl:
			if node.Name.Name == "acquireExperienceLock" {
				out = append(out, at(node.Pos())+": declares func acquireExperienceLock")
			}
			out = append(out, scanTmpRenames(fset, node)...)
		}
		return true
	})
	return out
}

// scanTmpRenames finds os.Rename calls inside fn whose source operand is a
// `<path> + ".tmp"` concatenation, resolving one level of local binding so
// the canonical `tmp := path + ".tmp"; os.Rename(tmp, path)` pair is caught.
// A rename with a non-".tmp" source (e.g. quarantining to `path+".rejected"`)
// is left alone.
func scanTmpRenames(fset *token.FileSet, fn *ast.FuncDecl) []string {
	if fn.Body == nil {
		return nil
	}

	staged := make(map[string]bool)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			if i >= len(assign.Lhs) || !isTmpConcat(rhs) {
				continue
			}
			if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
				staged[ident.Name] = true
			}
		}
		return true
	})

	var out []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isPkgSelector(sel, "os", "Rename") {
			return true
		}
		src := call.Args[0]
		if isTmpConcat(src) {
			out = append(out, fset.Position(call.Pos()).String()+": os.Rename commits an inline `+ \".tmp\"` staging path")
		} else if ident, ok := src.(*ast.Ident); ok && staged[ident.Name] {
			out = append(out, fmt.Sprintf("%s: os.Rename commits the `+ \".tmp\"` staging path %q", fset.Position(call.Pos()), ident.Name))
		}
		return true
	})
	return out
}

// isTmpConcat reports whether expr is a string concatenation ending in ".tmp".
func isTmpConcat(expr ast.Expr) bool {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.ADD {
		return false
	}
	lit, ok := bin.Y.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	val, err := strconv.Unquote(lit.Value)
	return err == nil && val == ".tmp"
}

// isPkgSelector reports whether sel is `pkg.name` on a bare package identifier.
func isPkgSelector(sel *ast.SelectorExpr, pkg, name string) bool {
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg && sel.Sel.Name == name
}
