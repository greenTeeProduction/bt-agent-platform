package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func grillTestBlackboard(t *testing.T, agentName string) *Blackboard {
	t.Helper()
	mgr := blackboard.NewManager(nil)
	return &Blackboard{
		BB: blackboard.NewHandle(mgr, "run-1", "", agentName),
	}
}

func TestGrillState_PersistsAcrossRuns(t *testing.T) {
	// Two runs sharing the manager (as the agent-scope store does across
	// scheduled runs): round saved by run 1 must be visible to run 2.
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveGrillState(run1, 2, "conv-abc")

	round, conv := loadGrillState(run2)
	if round != 2 || conv != "conv-abc" {
		t.Fatalf("loadGrillState = (%d, %q), want (2, conv-abc)", round, conv)
	}
}

func TestGrillState_WrapResetsRoundAndConversation(t *testing.T) {
	bb := grillTestBlackboard(t, "goap-loop")
	saveGrillState(bb, 1, "")

	round, conv := loadGrillState(bb)
	if round != 1 || conv != "" {
		t.Fatalf("after wrap: loadGrillState = (%d, %q), want (1, \"\")", round, conv)
	}
}

func TestGrillState_ChainStateFallbackParsesString(t *testing.T) {
	// Without a scoped blackboard, the string stored by setGoapState must be
	// parsed (the old code type-asserted float64/int and stayed stuck at 1).
	bb := &Blackboard{}
	setGoapState(bb, "grill_round", "3")
	setGoapState(bb, "grill_conversation_id", "conv-x")

	round, conv := loadGrillState(bb)
	if round != 3 || conv != "conv-x" {
		t.Fatalf("loadGrillState = (%d, %q), want (3, conv-x)", round, conv)
	}
}

func TestGrillState_InvalidRoundDefaultsToOne(t *testing.T) {
	bb := &Blackboard{}
	setGoapState(bb, "grill_round", "not-a-number")
	if round, _ := loadGrillState(bb); round != 1 {
		t.Fatalf("round = %d, want 1 for unparseable state", round)
	}
	setGoapState(bb, "grill_round", "7")
	if round, _ := loadGrillState(bb); round != 1 {
		t.Fatalf("round = %d, want 1 for out-of-range state", round)
	}
}

func TestReadNewestVaultDocs_CapsToNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-1 * time.Hour)
	for i := range 6 {
		name := filepath.Join(dir, "doc-"+string(rune('a'+i))+".md")
		if err := os.WriteFile(name, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(name, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	docs := readNewestVaultDocs(dir, "Doc",
		func(name string) bool { return strings.HasSuffix(name, ".md") }, 2, 100)
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
	// Newest first: doc-f, then doc-e.
	if !strings.Contains(docs[0], "doc-f.md") || !strings.Contains(docs[1], "doc-e.md") {
		t.Fatalf("expected newest two docs first, got:\n%s\n%s", docs[0], docs[1])
	}
}

func TestReadNewestVaultDocs_MissingDir(t *testing.T) {
	if docs := readNewestVaultDocs(filepath.Join(t.TempDir(), "nope"), "Doc",
		func(string) bool { return true }, 3, 100); docs != nil {
		t.Fatalf("expected nil for missing dir, got %v", docs)
	}
}

func TestRunGoapShellTimeout_ReportsTimeout(t *testing.T) {
	// runGoapShellTimeout always cds into goapFusionRepo; the production
	// default (/home/nico/go-bt-evolve) does not exist on CI runners.
	prev := goapFusionRepo
	goapFusionRepo = t.TempDir()
	t.Cleanup(func() { goapFusionRepo = prev })

	_, err := runGoapShellTimeout("sleep 2", 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "shell timeout") {
		t.Fatalf("expected shell-timeout error, got %v", err)
	}
}

func TestDefaultSuperpowersAllowedTools_OnePrefixPerBashRule(t *testing.T) {
	// Claude Code permission syntax allows one command prefix per Bash() rule;
	// a colon-joined multi-command list silently denies everything.
	for _, rule := range strings.Split(defaultSuperpowersAllowedTools, ",") {
		if !strings.HasPrefix(rule, "Bash(") {
			continue
		}
		spec := strings.TrimSuffix(strings.TrimPrefix(rule, "Bash("), ")")
		spec = strings.TrimSuffix(spec, ":*")
		if strings.Contains(spec, ":") {
			t.Errorf("Bash rule %q contains multiple colon-joined prefixes", rule)
		}
	}
	if !strings.Contains(defaultSuperpowersAllowedTools, "Bash(/usr/local/go/bin/go test:*)") {
		t.Error("allowedTools must cover the absolute go path used in the prompts")
	}
}
