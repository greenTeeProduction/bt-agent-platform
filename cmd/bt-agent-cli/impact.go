package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// normalizeImpactSource converts a changed-file path (typically absolute, or
// relative to the caller's cwd) into the module-relative, slash-separated
// form the impact graph indexes, rejecting paths outside root.
func normalizeImpactSource(root, source string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	absSource := source
	if !filepath.IsAbs(absSource) {
		absSource = filepath.Join(absRoot, source)
	}
	absSource, err = filepath.Abs(absSource)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(absRoot, absSource)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source %q is outside root %q", source, root)
	}
	return filepath.ToSlash(rel), nil
}

// impactedTestsForSource normalizes source relative to root and returns the
// tests the impact graph reports as affected by a change to it.
func impactedTestsForSource(root, source string) ([]string, error) {
	rel, err := normalizeImpactSource(root, source)
	if err != nil {
		return nil, err
	}
	return knowledge.ImpactedTests(root, rel)
}

// cmdImpact prints the change-scoped test list for a changed source file, so
// a commit gate can run a narrow suite instead of always running everything.
func cmdImpact() {
	fs := flag.NewFlagSet("impact", flag.ExitOnError)
	root := fs.String("root", ".", "Module root directory (contains go.mod)")
	_ = fs.Parse(os.Args[2:])

	source := fs.Arg(0)
	if source == "" {
		fmt.Fprintln(os.Stderr, "Error: source file required")
		fmt.Fprintln(os.Stderr, "Usage: bt-agent-cli impact <source-file> [--root <dir>]")
		os.Exit(1)
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tests, err := impactedTestsForSource(absRoot, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(tests) == 0 {
		fmt.Println("No impacted tests found for", source)
		return
	}
	for _, t := range tests {
		fmt.Println(t)
	}
}
