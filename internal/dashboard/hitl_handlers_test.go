package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

func TestHITLHandlers_PendingApproveReject(t *testing.T) {
	dir := t.TempDir()
	store, err := hitl.InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	req := hitl.NewRequest("Gate", "HumanApprovalGate", "task body", "plan", "proposed", "please review", nil)
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/hitl/pending", nil)
	HandleHITLPending(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("pending: status %d", rr.Code)
	}
	var pending []*hitl.Request
	if err := json.Unmarshal(rr.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}

	rr = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/hitl/"+req.ID+"/approve", bytes.NewReader([]byte(`{"reviewer":"tester","comment":"ok"}`)))
	HandleHITL(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: status %d body %s", rr.Code, rr.Body.String())
	}

	got, ok := store.Get(req.ID)
	if !ok || got.Status != hitl.StatusApproved {
		t.Fatalf("expected approved, got %v ok=%v", got, ok)
	}

	req2 := hitl.NewRequest("Gate2", "HumanApprovalGate", "t2", "", "", "review", nil)
	_ = store.Create(req2)
	rr = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/hitl/"+req2.ID+"/reject", bytes.NewReader([]byte(`{"reviewer":"tester","reason":"no"}`)))
	HandleHITL(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject: status %d", rr.Code)
	}
}
