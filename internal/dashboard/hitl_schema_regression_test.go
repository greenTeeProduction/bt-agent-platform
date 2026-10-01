package dashboard

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/hitl"
)

func TestHITLTemplatesValidateActualHandlerResponses(t *testing.T) {
	previous := hitl.DefaultStore
	t.Cleanup(func() { hitl.DefaultStore = previous })
	store, err := hitl.InitStore(filepath.Join(t.TempDir(), "hitl"))
	if err != nil {
		t.Fatal(err)
	}
	req := hitl.NewRequest("test", "HumanApprovalGate", "task", "plan", "proposed", "review", nil)
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(HandleHITL))
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, "/api/hitl/" + req.ID, "", http.StatusOK},
		{http.MethodPost, "/api/hitl/" + req.ID + "/approve", `{"reviewer":"test"}`, http.StatusOK},
		{http.MethodGet, "/api/hitl/does-not-exist", "", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s status=%d body=%s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestHITLStorageFailuresReturnUnavailableWithEnforcedSchema(t *testing.T) {
	previous := hitl.DefaultStore
	t.Cleanup(func() { hitl.DefaultStore = previous })
	root := t.TempDir()
	_, err := hitl.InitStore(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "hitl", "requests.json")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"corrupt", "cancelled"} {
		if failure == "cancelled" {
			if err := os.WriteFile(path, []byte("[]"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/hitl/pending"}, {http.MethodGet, "/api/hitl/id"},
			{http.MethodPost, "/api/hitl/id/approve"}, {http.MethodPost, "/api/hitl/id/reject"}, {http.MethodPost, "/api/hitl/id/escalate"},
		} {
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(HandleHITL))
			request := httptest.NewRequest(route.method, route.path, bytes.NewBufferString(`{}`))
			if failure == "cancelled" {
				ctx, cancel := context.WithCancel(request.Context())
				cancel()
				request = request.WithContext(ctx)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s %s status=%d body=%s", failure, route.path, response.Code, response.Body.String())
			}
		}
	}
}
