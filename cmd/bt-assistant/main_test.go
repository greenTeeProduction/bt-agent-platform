package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
)

type fakeLLM struct{ lastPrompt string }

func (f *fakeLLM) Generate(prompt string) (string, error) {
	f.lastPrompt = prompt
	return "assistant answer", nil
}

type fakeExecutor struct {
	agent string
	task  string
	tree  string
}

func (f *fakeExecutor) RunTask(agentName, task, treeID string) (string, string, error) {
	f.agent = agentName
	f.task = task
	f.tree = treeID
	return "tree output", "success", nil
}

func newTestApp(t *testing.T) (*app, *bytes.Buffer, *fakeLLM, *fakeExecutor) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	llm := &fakeLLM{}
	exec := &fakeExecutor{}
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return &app{out: out, err: errOut, registry: reg, llm: llm, executor: exec, graph: buildGraph()}, out, llm, exec
}

func TestAgentLifecycleCommands(t *testing.T) {
	a, out, _, _ := newTestApp(t)

	if code := a.run([]string{"create", "reviewer", "--tree", "domain:code_review", "--desc", "Reviews code"}); code != 0 {
		t.Fatalf("create exit=%d stderr=%s", code, a.err)
	}
	if !strings.Contains(out.String(), "Created agent: reviewer") {
		t.Fatalf("create output = %q", out.String())
	}

	out.Reset()
	if code := a.run([]string{"agents"}); code != 0 {
		t.Fatalf("agents exit=%d stderr=%s", code, a.err)
	}
	if got := out.String(); !strings.Contains(got, "reviewer") || !strings.Contains(got, "domain:code_review") {
		t.Fatalf("agents output missing created agent: %q", got)
	}

	out.Reset()
	if code := a.run([]string{"schedule", "reviewer", "--every", "*/15 * * * *"}); code != 0 {
		t.Fatalf("schedule exit=%d stderr=%s", code, a.err)
	}
	inst, err := a.registry.Get("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Definition.Schedule != "*/15 * * * *" {
		t.Fatalf("schedule not persisted: %q", inst.Definition.Schedule)
	}

	out.Reset()
	if code := a.run([]string{"delete", "reviewer", "--yes"}); code != 0 {
		t.Fatalf("delete exit=%d stderr=%s", code, a.err)
	}
	if _, err := a.registry.Get("reviewer"); err == nil {
		t.Fatal("expected reviewer to be deleted")
	}
}

func TestRunTreeCommandDelegatesToExecutor(t *testing.T) {
	a, out, _, exec := newTestApp(t)

	code := a.run([]string{"run-tree", "godev", "fix", "the", "builder"})
	if code != 0 {
		t.Fatalf("run-tree exit=%d stderr=%s", code, a.err)
	}
	if exec.tree != "godev" || exec.task != "fix the builder" || exec.agent != "bt-assistant" {
		t.Fatalf("executor call = agent=%q tree=%q task=%q", exec.agent, exec.tree, exec.task)
	}
	if got := out.String(); !strings.Contains(got, "Outcome: success") || !strings.Contains(got, "tree output") {
		t.Fatalf("run-tree output = %q", got)
	}
}

func TestAskIncludesPlatformContext(t *testing.T) {
	a, out, llm, _ := newTestApp(t)
	if code := a.run([]string{"create", "researcher", "--tree", "research:deep_research"}); code != 0 {
		t.Fatalf("create exit=%d stderr=%s", code, a.err)
	}
	out.Reset()

	code := a.run([]string{"ask", "what", "agents", "exist?"})
	if code != 0 {
		t.Fatalf("ask exit=%d stderr=%s", code, a.err)
	}
	if !strings.Contains(out.String(), "assistant answer") {
		t.Fatalf("ask output = %q", out.String())
	}
	if !strings.Contains(llm.lastPrompt, "researcher") || !strings.Contains(llm.lastPrompt, "research:deep_research") {
		t.Fatalf("LLM prompt missing platform state: %q", llm.lastPrompt)
	}
}

func TestNaturalLanguageIntentParser(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"list agents", []string{"agents"}},
		{"show platform status", []string{"status"}},
		{"run tree domain:code_review review the diff", []string{"run-tree", "domain:code_review", "review", "the", "diff"}},
		{"discover tree for improve observability", []string{"discover", "improve", "observability"}},
	}
	for _, tc := range cases {
		got := parseIntent(tc.input)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Fatalf("parseIntent(%q)=%v want %v", tc.input, got, tc.want)
		}
	}
}
