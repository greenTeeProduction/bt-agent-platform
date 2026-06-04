package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestVerifyNotebookLMEvidenceRejectsFabricatedOutput(t *testing.T) {
	bb := &Blackboard{
		Result: "AUTH OK. I would run nlm research status <task_id> and then import sources.",
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	if got := verifyNotebookLMEvidenceAction(ctx); got != -1 {
		t.Fatalf("expected fabricated output to fail, got %d", got)
	}
	if bb.Outcome != "failure" {
		t.Fatalf("outcome = %q, want failure", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "fabrication/placeholder marker") {
		t.Fatalf("expected fabrication reason in result, got: %s", bb.Result)
	}
}

func TestVerifyNotebookLMEvidenceRejectsEmptyOrUngroundedSuccess(t *testing.T) {
	bb := &Blackboard{Result: "VERIFIED: NotebookLM operation complete"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	if got := verifyNotebookLMEvidenceAction(ctx); got != -1 {
		t.Fatalf("expected ungrounded success text to fail, got %d", got)
	}
	if !strings.Contains(bb.Result, "missing real NotebookLM UUID evidence") {
		t.Fatalf("expected missing UUID reason, got: %s", bb.Result)
	}
}

func TestVerifyNotebookLMEvidenceAcceptsRealNotebookPayload(t *testing.T) {
	bb := &Blackboard{Results: []string{`{"status":"success","notebook":{"id":"463ca402-e972-470b-889c-b735e37c6746","title":"BT Platform Research","source_count":7,"url":"https://notebooklm.google.com/notebook/463ca402-e972-470b-889c-b735e37c6746"},"sources":[{"id":"529ac5bb-50a0-4cfd-ad40-ac1eee6b8cc2","title":"BT Platform Agent Registry & Scheduler"}]}`}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	if got := verifyNotebookLMEvidenceAction(ctx); got != 1 {
		t.Fatalf("expected real NotebookLM payload to pass, got %d: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	if bb.QualityScore != 1.0 {
		t.Fatalf("quality = %.1f, want 1.0", bb.QualityScore)
	}
	if !strings.Contains(bb.Result, "NOTEBOOKLM EVIDENCE VERIFIED") {
		t.Fatalf("expected verified marker, got: %s", bb.Result)
	}
}

func TestVerifyNotebookLMEvidenceRegistered(t *testing.T) {
	if fn := GetAction("VerifyNotebookLMEvidence"); fn == nil {
		t.Fatal("VerifyNotebookLMEvidence must be registered for tree validation/building")
	}
}
