package dashboard

import (
	"bytes"
	"encoding/json"
	"math"
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

// TestHandleHITL_MalformedBody_ReturnsBadRequest ensures a malformed JSON
// body on approve/reject/escalate is reported to the caller instead of being
// silently ignored and treated as an empty (zero-value) body.
func TestHandleHITL_MalformedBody_ReturnsBadRequest(t *testing.T) {
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
	r := httptest.NewRequest(http.MethodPost, "/api/hitl/"+req.ID+"/approve", bytes.NewReader([]byte(`{"reviewer":`)))
	HandleHITL(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed body, got %d body %s", rr.Code, rr.Body.String())
	}

	got, ok := store.Get(req.ID)
	if !ok || got.Status != hitl.StatusPending {
		t.Fatalf("expected request to remain pending after malformed body, got %v ok=%v", got, ok)
	}
}

// TestEncodeJSON_EncodeFailure_DoesNotDoubleWriteHeader ensures encodeJSON
// doesn't attempt to write a second status code (which is a silent no-op on
// a real ResponseWriter once headers are sent) when the value fails to
// marshal. The status recorded must reflect the failure, not the original
// success status.
func TestEncodeJSON_EncodeFailure_DoesNotDoubleWriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	encodeJSON(rr, http.StatusOK, map[string]any{"bad": math.Inf(1)})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 after encode failure, got %d body %s", rr.Code, rr.Body.String())
	}
}
