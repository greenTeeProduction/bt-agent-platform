// Package engine — GrillDesignArtifact pure functions: question parsing and
// graded-fallback resolution for design interrogation. No I/O here; the
// NotebookLM/Web answerers are wired up as closures in
// actions_superpowers_prod.go so this file stays trivially testable with
// fakes and never touches the network in tests.
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type grillQuestion struct {
	Critical bool
	Branch   string
	Text     string
}

type grillAnswerers struct {
	NotebookLM func(ctx context.Context, batch []grillQuestion) (map[int]string, error)
	Web        func(ctx context.Context, batch []grillQuestion) (map[int]string, error)
}

type grillResult struct {
	Markdown     string
	OpenCritical int
}

var errAnswererUnavailable = errors.New("answerer unavailable")

// parseGrillQuestions extracts "Q [critical|normal] <branch>: <text>" lines.
func parseGrillQuestions(out string) []grillQuestion {
	var qs []grillQuestion
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Q [") {
			continue
		}
		rest := strings.TrimPrefix(line, "Q [")
		sevEnd := strings.Index(rest, "]")
		if sevEnd < 0 {
			continue
		}
		sev := strings.ToLower(strings.TrimSpace(rest[:sevEnd]))
		body := strings.TrimSpace(rest[sevEnd+1:])
		colon := strings.Index(body, ":")
		if colon < 0 {
			continue
		}
		qs = append(qs, grillQuestion{
			Critical: sev == "critical",
			Branch:   strings.TrimSpace(body[:colon]),
			Text:     strings.TrimSpace(body[colon+1:]),
		})
	}
	return qs
}

// resolveGrillQuestions answers questions via NotebookLM first (batches of 5),
// web research second; unanswered questions are recorded OPEN. Never panics,
// never blocks the pipeline unless a critical question stays open.
func resolveGrillQuestions(ctx context.Context, qs []grillQuestion, a grillAnswerers) grillResult {
	answers := map[int]string{}
	tryAnswerer := func(fn func(context.Context, []grillQuestion) (map[int]string, error)) {
		if fn == nil {
			return
		}
		var openIdx []int
		var open []grillQuestion
		for i, q := range qs {
			if _, ok := answers[i]; !ok {
				openIdx = append(openIdx, i)
				open = append(open, q)
			}
		}
		for lo := 0; lo < len(open); lo += 5 {
			hi := min(lo+5, len(open))
			got, err := fn(ctx, open[lo:hi])
			if err != nil {
				return // graded degradation: leave remaining for next answerer
			}
			for rel, text := range got {
				answers[openIdx[lo+rel]] = text
			}
		}
	}
	tryAnswerer(a.NotebookLM)
	tryAnswerer(a.Web)

	var b strings.Builder
	b.WriteString("\n## Grill Q&A\n\n")
	open := 0
	for i, q := range qs {
		sev := "normal"
		if q.Critical {
			sev = "critical"
		}
		if ans, ok := answers[i]; ok {
			fmt.Fprintf(&b, "**Q (%s, %s):** %s\n\n**A:** %s\n\n", sev, q.Branch, q.Text, ans)
		} else {
			fmt.Fprintf(&b, "**Q (%s, %s):** %s\n\n**A:** OPEN — no answerer available\n\n", sev, q.Branch, q.Text)
			if q.Critical {
				open++
			}
		}
	}
	return grillResult{Markdown: b.String(), OpenCritical: open}
}
