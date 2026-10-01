package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func TestOpenAPIDocumentSurvivesEnforcedResponseValidation(t *testing.T) {
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleOpenAPI))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
	var document struct {
		Version string `json:"openapi"`
		Paths   map[string]map[string]struct {
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || document.Version != "3.0.3" || len(document.Paths) == 0 {
		t.Fatalf("OpenAPI object rejected: status=%d version=%s paths=%d body=%s", rr.Code, document.Version, len(document.Paths), rr.Body.String())
	}
	for _, status := range []string{"401", "403", "503"} {
		if document.Paths["/api/pipelines"]["get"].Responses[status] == nil {
			t.Fatalf("generated inventory schema lacks status %s", status)
		}
	}
}

func TestSummaryCategoryMapSurvivesEnforcedResponseValidation(t *testing.T) {
	old := kg
	kg = knowledge.NewKnowledgeGraph()
	t.Cleanup(func() { kg = old })
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(handleSummary))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/summary", nil))
	var response struct {
		Categories map[string]int `json:"categories"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || response.Categories == nil {
		t.Fatalf("category map rejected as missing a nested categories field: status=%d body=%s", rr.Code, rr.Body.String())
	}
}
