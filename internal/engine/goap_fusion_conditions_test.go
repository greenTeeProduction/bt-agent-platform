package engine

import "testing"

func TestGoapFusion_IsApplyRequestDoesNotTreatReportWritingAsCodeApply(t *testing.T) {
	fn := GetCondition("IsApplyRequest")
	if fn == nil {
		t.Fatal("IsApplyRequest condition not registered")
	}

	shouldNotApply := []string{
		"research goap fusion status and write deterministic analysis only",
		"generate scheduled fusion report only",
		"analyze gaps and do not apply code changes",
		"Scheduled GOAP fusion cycle: read vault research and graphify report, identify improvement gaps, prioritize goals, apply highest-priority improvements via Superpowers runtime, run health checks, and record vault analysis.",
	}
	for _, task := range shouldNotApply {
		if fn(&Blackboard{Task: task}) {
			t.Fatalf("IsApplyRequest(%q) = true; scheduled/report-only tasks must stay on deterministic path", task)
		}
	}
}

func TestGoapFusion_IsApplyRequestAllowsExplicitCodeChangingTasks(t *testing.T) {
	fn := GetCondition("IsApplyRequest")
	if fn == nil {
		t.Fatal("IsApplyRequest condition not registered")
	}

	shouldApply := []string{
		"implement one concrete Superpowers runtime fix",
		"patch engine to add a regression test",
		"create domain tree for the new BT capability",
		"modify code to register the missing action",
	}
	for _, task := range shouldApply {
		if !fn(&Blackboard{Task: task}) {
			t.Fatalf("IsApplyRequest(%q) = false; explicit code-changing tasks must use Superpowers apply path", task)
		}
	}
}
