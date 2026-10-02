package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/startup"
)

func TestTreeStructureCatalogRoundTripsUnderEnforcedValidation(t *testing.T) {
	isolatePipelinePaths(t)
	old := domains.DynamicResolveFn
	domains.DynamicResolveFn = nil
	t.Cleanup(func() { domains.DynamicResolveFn = old })
	fixtures := make(map[string]*evolution.SerializableNode)
	for prefix, trees := range map[string]map[string]*evolution.SerializableNode{
		"domain:": domains.AllDomainTrees(), "finance:": evolution.AllFinanceTrees(),
		"research:": evolution.ResearchTrees(), "startup:": startup.StartupTrees(),
	} {
		for name, tree := range trees {
			fixtures[prefix+name] = tree
		}
	}
	for name := range domains.ResolverReachableDomainTrees() {
		fixtures[name] = domains.ResolveTreeID(name)
	}
	for _, id := range []string{"default", "godev", "thinktank:synthesis", "thinktank:peer_review", "thinktank:report", "composed:task", "composed:task:hitl", "composed:task:agentic", "composed:task:full"} {
		fixtures[id] = domains.ResolveTreeID(id)
	}
	maps.Copy(fixtures, domains.AllDomainTrees()) // historical inspection aliases
	generated := &evolution.SerializableNode{Type: "Sequence", Name: "ActualGenerated", Children: []evolution.SerializableNode{{Type: "Action", Name: "ActualChild", Description: "retained", Metadata: map[string]any{"owner": "fixture"}}}}
	domains.DynamicResolveFn = func(id string) *evolution.SerializableNode {
		if id == "goal:generated" {
			return generated
		}
		return nil
	}
	fixtures["goal:generated"] = generated
	mux := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("structure-fixture-key"))
	for id, expected := range fixtures {
		t.Run(id, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/tree/structure?id="+url.QueryEscape(id), nil)
			req.Header.Set("X-API-Key", "structure-fixture-key")
			mux.ServeHTTP(rr, req)
			if rr.Code != 200 {
				t.Fatalf("registered definition unavailable: status=%d body=%s", rr.Code, rr.Body.String())
			}
			var got, want any
			encoded, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("definition replaced or truncated: id=%s", id)
			}
		})
	}
}

func TestTreeStructureMissesDoNotInventDefinitions(t *testing.T) {
	isolatePipelinePaths(t)
	oldHook, oldKG := domains.DynamicResolveFn, kg
	domains.DynamicResolveFn = nil
	kg = knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "metadata-only", Name: "No definition", NodeCount: 99})
	t.Cleanup(func() { domains.DynamicResolveFn, kg = oldHook, oldKG })
	mux := api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{Enforce: true})(dashboardMux("structure-fixture-key"))
	for _, tc := range []struct {
		id, key string
		status  int
	}{
		{"domain:code_review", "", 401}, {"domain:code_review", "wrong", 401},
		{"", "structure-fixture-key", 400}, {"metadata-only", "structure-fixture-key", 404},
		{"unknown:code_review", "structure-fixture-key", 404}, {"thinktank:missing", "structure-fixture-key", 404},
		{"core:../../outside", "structure-fixture-key", 404},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/tree/structure?id="+url.QueryEscape(tc.id), nil)
		req.Header.Set("X-API-Key", tc.key)
		mux.ServeHTTP(rr, req)
		if rr.Code != tc.status {
			t.Fatalf("id=%q key-present=%t status=%d want=%d body=%s", tc.id, tc.key != "", rr.Code, tc.status, rr.Body.String())
		}
	}
}
