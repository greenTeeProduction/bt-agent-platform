package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteTemplatesMatchSegmentsAndPreserveExactPrecedence(t *testing.T) {
	index := NewRouteIndex([]Route{{Path: "/api/items/{id}", Method: GET, Summary: "generic"}, {Path: "/api/{kind}/special", Method: GET, Summary: "special"}, {Path: "/api/items/pending", Method: GET, Summary: "exact"}})
	for _, tc := range []struct{ method, path, want string }{{"GET", "/api/items/123", "generic"}, {"get", "/api/items/pending", "exact"}, {"GET", "/api/tasks/special", "special"}, {"POST", "/api/items/123", ""}, {"GET", "/api/items/", ""}, {"GET", "/api/items/a/extra", ""}} {
		got := index.Lookup(tc.method, tc.path)
		if tc.want == "" {
			if got != nil {
				t.Fatalf("%s unexpectedly matched", tc.path)
			}
		} else if got == nil || got.Summary != tc.want {
			t.Fatalf("%s route=%+v want=%s", tc.path, got, tc.want)
		}
	}
}

func TestHITLParameterizedMutationResponsesAreValidated(t *testing.T) {
	routes := DashboardRoutes()
	index := NewRouteIndex(routes)
	for _, operation := range []string{"approve", "reject", "escalate"} {
		path := "/api/hitl/request-id/" + operation
		route := index.Lookup(http.MethodPost, path)
		if route == nil || !route.Auth || len(route.Parameters) != 1 || route.Parameters[0].In != ParamPath || !route.Parameters[0].Required {
			t.Fatalf("missing authenticated parameterized route %s", path)
		}
		for _, valid := range []bool{true, false} {
			handler := ResponseValidator(routes, &ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if valid {
					_, _ = w.Write([]byte(`{"id":"request-id","status":"approved"}`))
				} else {
					_, _ = w.Write([]byte(`{"status":"approved"}`))
				}
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			want := http.StatusInternalServerError
			if valid {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("%s valid=%v status=%d", path, valid, response.Code)
			}
		}
	}
}
