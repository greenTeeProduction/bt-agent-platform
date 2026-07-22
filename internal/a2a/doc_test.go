package a2a

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// parseDocGo parses the package's doc.go file, keeping its current
// package-level documentation available for characterization.
func parseDocGo(t *testing.T) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "doc.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse doc.go: %v", err)
	}
	return f
}

// TestDocGoPackageClause pins the package name declared by doc.go.
func TestDocGoPackageClause(t *testing.T) {
	f := parseDocGo(t)
	if f.Name.Name != "a2a" {
		t.Fatalf("doc.go declares package %q, want %q", f.Name.Name, "a2a")
	}
}

// TestDocGoPackageCommentMentions pins the current package doc comment
// against the sibling files and HTTP routes it claims to describe.
func TestDocGoPackageCommentMentions(t *testing.T) {
	f := parseDocGo(t)
	if f.Doc == nil {
		t.Fatal("doc.go has no package-level doc comment")
	}
	docText := f.Doc.Text()

	cases := []struct {
		name   string
		substr string
	}{
		{"mentions card.go", "card.go"},
		{"mentions server.go", "server.go"},
		{"mentions client.go", "client.go"},
		{"mentions task_bridge.go", "task_bridge.go"},
		{"documents default A2A port", "8686"},
		{"documents well-known agent card endpoint", "/.well-known/agent-card.json"},
		// server.go's Start() registers a distinct "/agents/" mux route
		// (handleAgentEndpoint) for per-agent JSON-RPC; the well-known
		// endpoint only ever serves one aggregated global card.
		{"documents per-agent endpoint", "/agents/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(docText, tc.substr) {
				t.Errorf("package doc comment does not mention %q\ngot:\n%s", tc.substr, docText)
			}
		})
	}
}

// TestDocGoReferencedFilesExist pins that every sibling file doc.go credits
// with a specific responsibility still exists in the package.
func TestDocGoReferencedFilesExist(t *testing.T) {
	files := []string{"card.go", "server.go", "client.go", "task_bridge.go"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			if _, err := os.Stat(name); err != nil {
				t.Errorf("doc.go references %q but it does not exist: %v", name, err)
			}
		})
	}
}
