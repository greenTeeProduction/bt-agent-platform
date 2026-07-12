package knowledge

// ImpactGraph is a directed symbol graph that links each Go source file to the
// test files a change to it can affect. It powers graph-based impact analysis:
// given an edited source file, TestsFor answers "which tests should I run?".
//
// Two heuristics build the edges:
//
//   - Import-based analysis: if a test file's package imports the package a
//     source file belongs to, then changing that source file can affect the
//     test. Every source file in the imported package gains an edge to the test.
//   - Directory proximity: a test file that lives in the same directory as a
//     source file is treated as impacted by it, even without a direct import
//     (same-package white-box tests reference unexported symbols implicitly).
//
// Paths in the graph are module-relative and slash-separated (e.g.
// "mathx/mathx.go") so results are stable across operating systems. TestsFor
// returns a sorted slice, so repeated queries are deterministic regardless of
// map iteration order.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ImpactGraph maps each source file to the set of test files it can impact.
type ImpactGraph struct {
	// tests maps a module-relative source path to its sorted, de-duplicated
	// list of impacted module-relative test paths.
	tests map[string][]string
}

// TestsFor returns the sorted list of test files impacted by a change to the
// given module-relative source file. An unknown source file yields nil.
func (g *ImpactGraph) TestsFor(source string) []string {
	return g.tests[source]
}

// ImpactedTests is the single build+query entry point CLI and MCP consumers
// share: it builds the impact graph for the module rooted at root and
// returns the tests impacted by a change to source, so a caller can gate a
// commit on a scoped test list instead of always running the full suite.
func ImpactedTests(root, source string) ([]string, error) {
	graph, err := BuildImpactGraph(root)
	if err != nil {
		return nil, err
	}
	return graph.TestsFor(source), nil
}

// goFileInfo captures the parsed facts we need from a single .go file.
type goFileInfo struct {
	rel     string   // module-relative, slash-separated path
	dir     string   // module-relative, slash-separated directory
	isTest  bool     // true for *_test.go files
	imports []string // import paths declared in the file
}

// BuildImpactGraph walks the Go module rooted at root and constructs an
// ImpactGraph linking source files to the tests they impact. It reads the
// module path from go.mod so import edges can be resolved against in-module
// packages.
func BuildImpactGraph(root string) (*ImpactGraph, error) {
	modPath, err := readModulePath(root)
	if err != nil {
		return nil, err
	}

	files, err := collectGoFiles(root)
	if err != nil {
		return nil, err
	}

	// Map each in-module package import path to the directory that defines it,
	// and each directory to the source (non-test) files it contains.
	dirImportPath := make(map[string]string)  // dir -> import path
	sourcesByDir := make(map[string][]string) // dir -> source rel paths
	testsByDir := make(map[string][]string)   // dir -> test rel paths
	for _, f := range files {
		dirImportPath[f.dir] = importPathFor(modPath, f.dir)
		if f.isTest {
			testsByDir[f.dir] = append(testsByDir[f.dir], f.rel)
		} else {
			sourcesByDir[f.dir] = append(sourcesByDir[f.dir], f.rel)
		}
	}

	// Reverse index: import path -> source files in that package.
	sourcesByImport := make(map[string][]string)
	for dir, sources := range sourcesByDir {
		sourcesByImport[dirImportPath[dir]] = sources
	}

	// Accumulate edges as sets to avoid duplicates before sorting.
	edges := make(map[string]map[string]struct{}) // source rel -> set of test rel
	addEdge := func(source, test string) {
		set := edges[source]
		if set == nil {
			set = make(map[string]struct{})
			edges[source] = set
		}
		set[test] = struct{}{}
	}

	// Proximity edges: every source file impacts the tests beside it.
	for dir, sources := range sourcesByDir {
		for _, source := range sources {
			for _, test := range testsByDir[dir] {
				addEdge(source, test)
			}
		}
	}

	// Import edges: a test importing an in-module package is impacted by every
	// source file in that package.
	for _, f := range files {
		if !f.isTest {
			continue
		}
		for _, imp := range f.imports {
			for _, source := range sourcesByImport[imp] {
				addEdge(source, f.rel)
			}
		}
	}

	tests := make(map[string][]string, len(edges))
	for source, set := range edges {
		list := make([]string, 0, len(set))
		for test := range set {
			list = append(list, test)
		}
		sort.Strings(list)
		tests[source] = list
	}

	return &ImpactGraph{tests: tests}, nil
}

// readModulePath extracts the module path from the go.mod at root.
func readModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", nil
}

// collectGoFiles walks root and parses the import declarations of every .go
// file, skipping vendored and hidden directories.
func collectGoFiles(root string) ([]goFileInfo, error) {
	var files []goFileInfo
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if path != root && (name == "vendor" || name == "testdata" ||
				strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		imports := make([]string, 0, len(parsed.Imports))
		for _, spec := range parsed.Imports {
			imports = append(imports, strings.Trim(spec.Path.Value, `"`))
		}

		files = append(files, goFileInfo{
			rel:     rel,
			dir:     path2dir(rel),
			isTest:  strings.HasSuffix(rel, "_test.go"),
			imports: imports,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// path2dir returns the slash-separated directory of a module-relative file,
// using "." for files at the module root.
func path2dir(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	return dir
}

// importPathFor computes the in-module import path for a directory, where dir
// is slash-separated and module-relative ("." for the root package).
func importPathFor(modPath, dir string) string {
	if dir == "." || dir == "" {
		return modPath
	}
	return modPath + "/" + dir
}
