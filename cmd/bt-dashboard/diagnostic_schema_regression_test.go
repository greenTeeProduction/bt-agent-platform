package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/security"
)

func TestSecurityAuditRealResponsesSurviveEnforcedValidation(t *testing.T) {
	isolatePipelinePaths(t)
	old := security.GlobalAuditBuffer()
	t.Cleanup(func() { security.SetGlobalAuditBuffer(old) })
	for _, fixture := range []string{"disabled", "empty", "populated"} {
		t.Run(fixture, func(t *testing.T) {
			var buffer *security.AuditBuffer
			if fixture != "disabled" {
				buffer = security.NewAuditBuffer(3)
			}
			if fixture == "populated" {
				buffer.Push("fixture-one", map[string]string{"owner": "review"})
				buffer.Push("fixture-two", nil)
			}
			security.SetGlobalAuditBuffer(buffer)
			handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("diagnostic-fixture-key"))
			for _, key := range []string{"", "wrong", "diagnostic-fixture-key"} {
				rr := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/api/security/audit", nil)
				req.Header.Set("X-API-Key", key)
				handler.ServeHTTP(rr, req)
				if key != "diagnostic-fixture-key" {
					if rr.Code != 401 {
						t.Fatalf("audit auth failed: status=%d body=%s", rr.Code, rr.Body.String())
					}
					continue
				}
				var result security.AuditBufferJSON
				if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if rr.Code != 200 || result.Events == nil || result.EventCounts == nil {
					t.Fatalf("actual diagnostic payload rejected: status=%d body=%s", rr.Code, rr.Body.String())
				}
				if result.BufferEnabled != (fixture != "disabled") {
					t.Fatalf("buffer disposition lost: %+v", result)
				}
				if fixture == "populated" {
					if len(result.Events) != 2 || result.EventCounts["fixture-one"] != 1 || result.Events[1].Attrs["owner"] != "review" || result.Events[0].Event != "fixture-two" {
						t.Fatalf("actual audit evidence truncated/replaced: %+v", result)
					}
				} else if len(result.Events) != 0 || len(result.EventCounts) != 0 {
					t.Fatalf("empty audit invented entries: %+v", result)
				}
			}
		})
	}
}

func TestLiveMetricsCategoryMapSurvivesEnforcedValidation(t *testing.T) {
	isolatePipelinePaths(t)
	old := kg
	kg = knowledge.NewKnowledgeGraph()
	t.Cleanup(func() { kg = old })
	handler := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("diagnostic-fixture-key"))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics/live", nil)
	req.Header.Set("X-API-Key", "diagnostic-fixture-key")
	handler.ServeHTTP(rr, req)
	var response struct {
		Trees struct {
			Categories map[string]int `json:"categories"`
		} `json:"trees"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || response.Trees.Categories == nil {
		t.Fatalf("actual category map rejected: status=%d body=%s", rr.Code, rr.Body.String())
	}
}
