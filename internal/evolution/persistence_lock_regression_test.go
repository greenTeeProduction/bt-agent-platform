package evolution

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Exercise every migrated write path that previously continued without its
// sidecar lock. A directory at the lock path makes acquisition fail even as
// root, while the JSON destination remains writable.
func TestPersistenceLockFailurePreservesState(t *testing.T) {
	for _, name := range []string{"selector", "decision-tree", "experience-persist", "experience-add", "experience-reuse"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			var save func() error
			var state func() any
			switch name {
			case "selector":
				disk := NewSelectorOptimizer(OrderByHybrid)
				disk.Record("sel", NodeExecutionRecord{NodeName: "disk", Outcome: "success"})
				if err := disk.SaveSelectorStats(path); err != nil {
					t.Fatal(err)
				}
				so := NewSelectorOptimizer(OrderByHybrid)
				so.Record("sel", NodeExecutionRecord{NodeName: "local", Outcome: "success"})
				save = func() error { return so.SaveSelectorStats(path) }
				state = func() any { return []any{so.Stats, so.unsaved} }
			case "decision-tree":
				disk := NewDTAnalyzer()
				disk.RecordHit("disk", "path", "", true)
				if err := disk.Save(path); err != nil {
					t.Fatal(err)
				}
				d := NewDTAnalyzer()
				d.RecordHit("local", "path", "", true)
				save = func() error { return d.Save(path) }
				state = func() any { return d.Stats }
			default:
				disk := &ExperienceBank{PersistPath: path, Entries: []ExperienceEntry{{ID: "disk"}}}
				if err := disk.Persist(); err != nil {
					t.Fatal(err)
				}
				eb := &ExperienceBank{PersistPath: path, Entries: []ExperienceEntry{{ID: "local"}}}
				state = func() any { return eb.Entries }
				switch name {
				case "experience-persist":
					save = eb.Persist
				case "experience-add":
					save = func() error { return eb.addEntry(ExperienceEntry{ID: "new"}) }
				case "experience-reuse":
					save = func() error { return eb.MarkReused([]string{"disk"}) }
				}
			}
			beforeDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeState, err := json.Marshal(state())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path+".lock", 0755); err != nil {
				t.Fatal(err)
			}
			if err := save(); err == nil {
				t.Error("save succeeded despite failed lock acquisition")
			}
			afterDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeDisk, afterDisk) {
				t.Error("lock failure overwrote persisted state")
			}
			afterState, err := json.Marshal(state())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeState, afterState) {
				t.Error("lock failure merged/mutated memory or consumed unsaved delta")
			}
			if err := os.Remove(path + ".lock"); err != nil {
				t.Fatal(err)
			}
			if err := save(); err != nil {
				t.Fatalf("retry after lock recovery: %v", err)
			}
			if name == "selector" {
				merged, err := readSelectorStatsFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, child := range []string{"disk", "local"} {
					cs := merged["sel"].Children[child]
					if cs == nil || cs.Successes != 1 {
						t.Errorf("retry lost/doubled %s delta: %+v", child, cs)
					}
				}
			}
		})
	}
}

func TestPersistenceFirstSaveCreatesParent(t *testing.T) {
	for _, name := range []string{"selector", "decision-tree", "experience-persist", "experience-add", "experience-reuse"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "new", "nested", "state.json")
			var err error
			switch name {
			case "selector":
				err = NewSelectorOptimizer(OrderByHybrid).SaveSelectorStats(path)
			case "decision-tree":
				err = NewDTAnalyzer().Save(path)
			default:
				eb := &ExperienceBank{PersistPath: path}
				switch name {
				case "experience-persist":
					err = eb.Persist()
				case "experience-add":
					err = eb.addEntry(ExperienceEntry{ID: "new"})
				case "experience-reuse":
					err = eb.MarkReused([]string{"new"})
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(data) {
				t.Fatalf("invalid persisted JSON: %s", data)
			}
		})
	}
}

// A successful first write alone cannot distinguish an unlocked fallback.
// Pin directory-before-lock-before-write ordering in the actual save methods,
// alongside the filesystem tests above; no timing races or production hooks.
func TestPersistenceFirstSaveCreatesParentBeforeLock(t *testing.T) {
	targets := map[string][]string{
		"selector_optimizer.go": {"SaveSelectorStats"},
		"decision_tree.go":      {"Save"},
		"experience_bank.go":    {"Persist", "addEntry", "MarkReused"},
	}
	for file, names := range targets {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			t.Run(file+"/"+name, func(t *testing.T) {
				var calls []string
				for _, decl := range f.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Name.Name != name {
						continue
					}
					ast.Inspect(fn.Body, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						sel, ok := call.Fun.(*ast.SelectorExpr)
						if !ok {
							return true
						}
						if isPkgSelector(sel, "os", "MkdirAll") {
							calls = append(calls, "mkdir")
						}
						if isPkgSelector(sel, "reliability", "AcquireFileLock") {
							calls = append(calls, "lock")
						}
						return true
					})
				}
				if !reflect.DeepEqual(calls, []string{"mkdir", "lock"}) {
					t.Fatalf("first-save setup = %v, want [mkdir lock]", calls)
				}
			})
		}
	}
}
