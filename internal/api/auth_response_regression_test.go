package api

import "testing"

func TestProtectedDashboardErrorSchemasPreserveAuthDispositions(t *testing.T) {
	for _, route := range DashboardRoutes() {
		if !route.Auth {
			continue
		}
		for _, code := range []int{401, 403} {
			if response := findResponse(&route, code); response == nil || response.StatusCode != code || response.Schema == nil {
				t.Fatalf("protected route %s %s lacks %d error schema", route.Method, route.Path, code)
			}
			if violations := ValidateResponse(&route, code, []byte(`{"error":"fixture authentication rejected"}`)); len(violations) != 0 {
				t.Fatalf("auth disposition rejected by schema: %s %s status=%d violations=%+v", route.Method, route.Path, code, violations)
			}
			if violations := ValidateResponse(&route, code, []byte(`{"unrelated":"not an error"}`)); len(violations) == 0 {
				t.Fatalf("auth schema does not enforce error field: %s %s status=%d", route.Method, route.Path, code)
			}
		}
	}
}

func TestAuthSchemaDefaultsDoNotReplaceExplicitContract(t *testing.T) {
	route := NewRoute("/fixture", GET).JSONResponse(401, "custom auth", ObjectSchema(map[string]*Schema{"reason": StringSchema("reason")}, "reason")).WithAuth().WithAuth().Build()
	counts := make(map[int]int)
	for _, response := range route.Responses {
		counts[response.StatusCode]++
	}
	if counts[401] != 1 || counts[403] != 1 {
		t.Fatalf("duplicate authentication defaults: %+v", counts)
	}
	if violations := ValidateResponse(&route, 401, []byte(`{"reason":"custom fixture"}`)); len(violations) != 0 {
		t.Fatalf("custom authentication contract replaced: %+v", violations)
	}
}

func TestResponseSchemaFallbackRequiresExplicitDefault(t *testing.T) {
	route := NewRoute("/fixture", GET).JSONResponse(200, "success", ArraySchema(StringSchema("item"), "items")).Build()
	if violations := ValidateResponse(&route, 401, []byte(`{"error":"authentication required"}`)); len(violations) != 0 {
		t.Fatalf("undocumented auth disposition became success-shape violation: %+v", violations)
	}
	route.Responses = append(route.Responses, RouteResponse{StatusCode: 0, Schema: ObjectSchema(map[string]*Schema{"error": StringSchema("error")}, "error")})
	if violations := ValidateResponse(&route, 401, []byte(`{"error":"authentication required"}`)); len(violations) != 0 {
		t.Fatalf("explicit default error rejected: %+v", violations)
	}
	if violations := ValidateResponse(&route, 418, []byte(`{"wrong":"body"}`)); len(violations) == 0 {
		t.Fatal("explicit default error schema was skipped")
	}
	if violations := ValidateResponse(&route, 200, []byte(`["item"]`)); len(violations) != 0 {
		t.Fatalf("default schema shadowed exact success: %+v", violations)
	}
}
