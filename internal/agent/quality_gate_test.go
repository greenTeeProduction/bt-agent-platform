package agent

import "testing"

func TestEstimateQualityScoresEvidenceRichDeterministicReports(t *testing.T) {
	output := "## Stock Monitor\nstatus: OK\nseverity: INFO\ntimestamp: 2026-06-03T17:00:00Z\nthreshold: 5%\nsymbols: AAPL,MSFT\ndelta: 0.8%\nno alert: below threshold\n"
	q := estimateQuality(output)
	if q < 0.8 {
		t.Fatalf("expected evidence-rich deterministic report quality >= 0.8, got %.2f", q)
	}
}

func TestEstimateQualityPenalizesGenericShortSuccess(t *testing.T) {
	q := estimateQuality("success")
	if q >= 0.5 {
		t.Fatalf("expected generic short success to remain low quality, got %.2f", q)
	}
}
