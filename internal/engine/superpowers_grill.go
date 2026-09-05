// Package engine — GrillDesignArtifact pure functions: question parsing and
// graded-fallback resolution for design interrogation. No I/O here; the
// NotebookLM/Web answerers are wired up as closures in
// actions_superpowers_prod.go so this file stays trivially testable with
// fakes and never touches the network in tests.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Markdown             string
	OpenCritical         int
	OpenCriticalBranches []string
	Answers              map[int]string
}

var errAnswererUnavailable = errors.New("answerer unavailable")

// parseGrillQuestions extracts "Q [critical|normal] <branch>: <text>" lines.
func parseGrillQuestions(out string) []grillQuestion {
	var qs []grillQuestion
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Q [") {
			continue
		}
		rest := strings.TrimPrefix(line, "Q [")
		before, after, ok := strings.Cut(rest, "]")
		if !ok {
			continue
		}
		sev := strings.ToLower(strings.TrimSpace(before))
		body := strings.TrimSpace(after)
		before0, after0, ok0 := strings.Cut(body, ":")
		if !ok0 {
			continue
		}
		qs = append(qs, grillQuestion{
			Critical: sev == "critical",
			Branch:   strings.TrimSpace(before0),
			Text:     strings.TrimSpace(after0),
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
	var openBranches []string
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
				openBranches = append(openBranches, q.Branch)
			}
		}
	}
	return grillResult{Markdown: b.String(), OpenCritical: open, OpenCriticalBranches: openBranches, Answers: answers}
}

const grillAppendixMarker = "\n## Grill Q&A"

// splitDesignDocument separates the design body from the append-only Grill
// Q&A appendix (everything from the first Grill Q&A heading onward).
func splitDesignDocument(content string) (string, string) {
	if idx := strings.Index(content, grillAppendixMarker); idx >= 0 {
		return content[:idx], content[idx:]
	}
	return content, ""
}

func designBodyHash(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}

func grillRoundHeading(round int) string {
	return fmt.Sprintf("\n## Grill Q&A — round %d\n\n", round)
}

// openCriticalDigest renders the round outcome for review_feedback: what got
// answered (the reviser folds it in) and which criticals are still open (the
// reviser must answer them from the codebase or redesign them away).
func openCriticalDigest(qs []grillQuestion, answers map[int]string) string {
	var b strings.Builder
	for i, q := range qs {
		if ans, ok := answers[i]; ok {
			fmt.Fprintf(&b, "ANSWERED [%s]: %s — %s\n", q.Branch, q.Text, ans)
		} else if q.Critical {
			fmt.Fprintf(&b, "OPEN CRITICAL [%s]: %s\n", q.Branch, q.Text)
		} else {
			fmt.Fprintf(&b, "OPEN [%s]: %s\n", q.Branch, q.Text)
		}
	}
	return b.String()
}
