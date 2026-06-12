package dashboard

import "testing"

func TestRecordNodeTickAndBlockOp(t *testing.T) {
	RecordNodeTick("Action", "TestNode", "Parent", "core:tool_execution", "success", 12)
	RecordNodeTick("Action", "TestNode", "Parent", "core:tool_execution", "failure", 5)
	RecordBlockOp("expand", "core:pre_gate", "ok", 3)

	snap := NodeMetricsSnapshot()
	if len(snap) == 0 {
		t.Fatal("expected node tick metrics")
	}
	bsnap := BlockMetricsSnapshot()
	if len(bsnap) == 0 {
		t.Fatal("expected block op metrics")
	}
}
