package engine

import (
	"context"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestGrillMeQuotaSkipReturnsSuccessNotRunning pins the tick-status contract
// for the NotebookLM quota skip: "non-fatal, continue with the pipeline"
// means SUCCESS (1) inside the GoapFusionLoop_Main Sequence. Returning 0
// (Running) makes the memoryless Sequence re-tick from the top until the
// runner's maxTicks cap and stamps the whole run "partial" — the scheduler
// then retries and dead-letters every cycle while the quota window is closed
// (observed live 2026-07-02: 2s partial ×3 → DLQ, ResearchRouter's Claude
// fallback never reached).
func TestGrillMeQuotaSkipReturnsSuccessNotRunning(t *testing.T) {
	fn := GetAction("GrillMeNotebookLM")
	if fn == nil {
		t.Fatal("GrillMeNotebookLM not registered")
	}
	bb := &Blackboard{
		Task: "scheduled goap fusion cycle",
		ChainState: map[string]any{
			"goap_fusion_nlm_quota_until": time.Now().Add(2 * time.Hour).Format(time.RFC3339),
		},
	}
	ctx := btcore.NewBTContext(context.Background(), bb)
	code := fn(ctx)
	if code != 1 {
		t.Fatalf("quota-skip must return 1 (Sequence continues to ResearchRouter fallback), got %d", code)
	}
	if bb.Outcome != "goap_fusion_grill_skipped_quota" {
		t.Fatalf("Outcome marker = %q, want goap_fusion_grill_skipped_quota", bb.Outcome)
	}
}
