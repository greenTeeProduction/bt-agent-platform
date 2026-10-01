package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestDLQHTTPRecoveryAndStorageAcknowledgement(t *testing.T) {
	isolatePipelinePaths(t)
	previous := dlq
	t.Cleanup(func() { dlq = previous })
	path := filepath.Join(t.TempDir(), "dlq.json")
	dlq = reliability.NewDeadLetterQueue(path)
	if err := dlq.PushExecutionFailureWithError(reliability.DeadLetterEntry{ID: "held"}, &reliability.ExecutionUncertainError{Err: errors.New("original fixture uncertainty")}); err != nil {
		t.Fatal(err)
	}
	if err := dlq.PushWithError(reliability.DeadLetterEntry{ID: "ordinary"}); err != nil {
		t.Fatal(err)
	}
	actions := 0
	// Construct a fresh owner over the same bytes, including the real canonical
	// authentication middleware and enforcing the documented response schemas.
	dlq = reliability.NewDeadLetterQueue(path)
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("dlq-fixture-key"))
	request := func(method, url string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, nil)
		if authenticated {
			r.Header.Set("X-API-Key", "dlq-fixture-key")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodPost, "/api/dlq/replay?id=held", false); w.Code != 401 {
		t.Fatalf("unauthenticated replay: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/dlq/replay?id=held", true); w.Code != 409 || w.Header().Get(reliability.ExecutionAdmissionHeader) != "false" {
		t.Fatalf("held replay: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodGet, "/api/dlq", true); w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w := request(http.MethodDelete, "/api/dlq/purge", true)
	var response struct{ Removed, Pending int }
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || response.Removed != 1 || response.Pending != 1 {
		t.Fatalf("purge: %d %s err=%v", w.Code, w.Body.String(), err)
	}
	if actions != 0 || dlq.Len() != 1 || !dlq.List()[0].RecoveryRequired {
		t.Fatal("ordinary operation erased hold or dispatched work")
	}
	original := []byte("{unreadable fixture")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, url string }{{http.MethodGet, "/api/dlq"}, {http.MethodPost, "/api/dlq/replay?id=held"}, {http.MethodDelete, "/api/dlq/purge"}} {
		w := request(test.method, test.url, true)
		if w.Code != 503 || w.Header().Get(reliability.ExecutionAdmissionHeader) != "false" {
			t.Fatalf("failed storage %s: %d %s", test.method, w.Code, w.Body.String())
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(original) {
		t.Fatal("HTTP failure overwrote unreadable state")
	}
}
