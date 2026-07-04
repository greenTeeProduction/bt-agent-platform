package agent

import (
	"testing"
	"time"
)

func TestFastRequeueAfterSuccess(t *testing.T) {
	now := time.Date(2026, 7, 4, 10, 3, 0, 0, time.UTC)
	cronNext := now.Add(27 * time.Minute) // next :30 slot

	fast := fastRequeueAfterSuccess("success", "…\n\nPROGRAM-CONTINUE: \"x\" milestone 2/5 pending", cronNext, now)
	if !fast.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("marker + success must requeue in 2m, got %v", fast)
	}
	if got := fastRequeueAfterSuccess("failure", "PROGRAM-CONTINUE: pending", cronNext, now); !got.Equal(cronNext) {
		t.Fatal("failures must keep the cron slot")
	}
	if got := fastRequeueAfterSuccess("success", "no marker here", cronNext, now); !got.Equal(cronNext) {
		t.Fatal("no marker must keep the cron slot")
	}
	soon := now.Add(30 * time.Second)
	if got := fastRequeueAfterSuccess("success", "PROGRAM-CONTINUE", soon, now); !got.Equal(soon) {
		t.Fatal("an earlier cron slot must win over the fast requeue")
	}
}
