package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ─── RouteIndex Tests ──────────────────────────────────────────────────────

func TestNewRouteIndex(t *testing.T) {
	routes := []Route{
		{Path: "/api/health", Method: GET},
		{Path: "/api/trees", Method: GET},
		{Path: "/api/tasks/approve", Method: POST},
	}
	ri := NewRouteIndex(routes)
	if ri.Len() != 3 {
		t.Fatalf("expected 3 routes, got %d", ri.Len())
	}
}

func TestRouteIndex_Lookup(t *testing.T) {
	routes := []Route{
		{Path: "/api/health", Method: GET, Summary: "Health"},
		{Path: "/api/trees", Method: GET, Summary: "Trees"},
		{Path: "/api/tasks/approve", Method: POST, Summary: "Approve"},
	}
	ri := NewRouteIndex(routes)

	tests := []struct {
		method, path string
		wantSummary  string
		wantNil      bool
	}{
		{"get", "/api/health", "Health", false},
		{"GET", "/api/health", "Health", false},
		{"post", "/api/tasks/approve", "Approve", false},
		{"GET", "/api/tasks/approve", "", true},
		{"DELETE", "/api/unknown", "", true},
	}

	for _, tt := range tests {
		got := ri.Lookup(tt.method, tt.path)
		if tt.wantNil {
			if got != nil {
				t.Errorf("Lookup(%q, %q) = %v, want nil", tt.method, tt.path, got.Summary)
			}
		} else {
			if got == nil {
				t.Errorf("Lookup(%q, %q) = nil, want %q", tt.method, tt.path, tt.wantSummary)
			} else if got.Summary != tt.wantSummary {
				t.Errorf("Lookup(%q, %q).Summary = %q, want %q", tt.method, tt.path, got.Summary, tt.wantSummary)
			}
		}
	}
}

func TestRouteIndex_Lookup_CaseInsensitive(t *testing.T) {
	routes := []Route{
		{Path: "/api/health", Method: GET},
	}
	ri := NewRouteIndex(routes)

	for _, method := range []string{"GET", "get", "Get", "gEt"} {
		if ri.Lookup(method, "/api/health") == nil {
			t.Errorf("Lookup(%q, /api/health) returned nil", method)
		}
	}
}

func TestRouteIndex_Lookup_MultipleSamePath(t *testing.T) {
	routes := []Route{
		{Path: "/api/dlq", Method: GET, Summary: "List DLQ"},
		{Path: "/api/dlq", Method: POST, Summary: "Replay DLQ"},
		{Path: "/api/dlq", Method: DELETE, Summary: "Purge DLQ"},
	}
	ri := NewRouteIndex(routes)

	if got := ri.Lookup("GET", "/api/dlq"); got == nil || got.Summary != "List DLQ" {
		t.Errorf("GET /api/dlq: want List DLQ, got %v", got)
	}
	if got := ri.Lookup("POST", "/api/dlq"); got == nil || got.Summary != "Replay DLQ" {
		t.Errorf("POST /api/dlq: want Replay DLQ, got %v", got)
	}
	if got := ri.Lookup("DELETE", "/api/dlq"); got == nil || got.Summary != "Purge DLQ" {
		t.Errorf("DELETE /api/dlq: want Purge DLQ, got %v", got)
	}
}

func TestNewRouteIndex_Empty(t *testing.T) {
	ri := NewRouteIndex(nil)
	if ri.Len() != 0 {
		t.Errorf("empty index should have 0 routes, got %d", ri.Len())
	}
	if ri.Lookup("GET", "/anything") != nil {
		t.Error("empty index should return nil for any lookup")
	}
}

func TestNewRouteIndex_NilReceiver(t *testing.T) {
	var ri *RouteIndex
	if ri.Len() != 0 {
		t.Error("nil RouteIndex.Len() should return 0")
	}
	if ri.Lookup("GET", "/api/health") != nil {
		t.Error("nil RouteIndex.Lookup() should return nil")
	}
}

// ─── ValidateResponse Tests ────────────────────────────────────────────────

func TestValidateResponse_NilRoute(t *testing.T) {
	v := ValidateResponse(nil, 200, []byte(`{"key":"value"}`))
	if len(v) != 0 {
		t.Errorf("nil route should return no violations, got %d", len(v))
	}
}

func TestValidateResponse_EmptyBody(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Description: "ok", Schema: StringSchema("test")},
		},
	}
	v := ValidateResponse(route, 200, nil)
	if len(v) != 0 {
		t.Errorf("empty body should return no violations, got %d", len(v))
	}
	v = ValidateResponse(route, 200, []byte{})
	if len(v) != 0 {
		t.Errorf("empty byte body should return no violations, got %d", len(v))
	}
}

func TestValidateResponse_ValidObject(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode:  200,
				Description: "ok",
				Schema: ObjectSchema(map[string]*Schema{
					"name": StringSchema("name"),
					"age":  IntSchema("age"),
				}, "name"),
			},
		},
	}
	body := []byte(`{"name":"Alice","age":30}`)
	v := ValidateResponse(route, 200, body)
	if len(v) != 0 {
		t.Errorf("valid response should have no violations, got: %+v", v)
	}
}

func TestValidateResponse_MissingRequiredField(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode:  200,
				Description: "ok",
				Schema: ObjectSchema(map[string]*Schema{
					"name": StringSchema("name"),
					"age":  IntSchema("age"),
				}, "name", "age"),
			},
		},
	}
	body := []byte(`{"name":"Alice"}`)
	v := ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected violations for missing required field")
	}
	found := false
	for _, viol := range v {
		if viol.Field == "age" && viol.Message == `missing required field "age"` {
			found = true
		}
	}
	if !found {
		t.Errorf("expected age violation, got: %+v", v)
	}
}

func TestValidateResponse_WrongType(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode: 200,
				Schema: ObjectSchema(map[string]*Schema{
					"count": IntSchema("count"),
				}, "count"),
			},
		},
	}
	body := []byte(`{"count":"not-a-number"}`)
	v := ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected type mismatch violation")
	}
	found := false
	for _, viol := range v {
		if viol.Field == "count" && viol.Message == "expected number, got string" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected count type violation, got: %+v", v)
	}
}

func TestValidateResponse_ArrayValidation(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode: 200,
				Schema: ArraySchema(
					ObjectSchema(map[string]*Schema{
						"id":   StringSchema("id"),
						"name": StringSchema("name"),
					}, "id"),
					"items",
				),
			},
		},
	}

	// Valid array
	body := []byte(`[{"id":"1","name":"Alice"},{"id":"2","name":"Bob"}]`)
	v := ValidateResponse(route, 200, body)
	if len(v) != 0 {
		t.Errorf("valid array should have no violations, got: %+v", v)
	}

	// Array with missing required field
	body = []byte(`[{"name":"Alice"},{"id":"2"}]`)
	v = ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected violation for missing required field in array item")
	}
}

func TestValidateResponse_InvalidJSON(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Schema: ObjectSchema(nil)},
		},
	}
	v := ValidateResponse(route, 200, []byte(`not json`))
	if len(v) == 0 {
		t.Fatal("expected violation for invalid JSON")
	}
	if v[0].Field != "(body)" {
		t.Errorf("expected field=(body), got %q", v[0].Field)
	}
}

func TestValidateResponse_NoSchema(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Description: "ok"}, // No Schema
		},
	}
	v := ValidateResponse(route, 200, []byte(`{"anything":"goes"}`))
	if len(v) != 0 {
		t.Errorf("no-schema response should return no violations, got %d", len(v))
	}
}

func TestValidateResponse_NonJSONContentType(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode:  200,
				Description: "HTML",
				ContentType: "text/html",
				Schema:      StringSchema("html"),
			},
		},
	}
	v := ValidateResponse(route, 200, []byte(`{"should":"be skipped"}`))
	if len(v) != 0 {
		t.Errorf("non-JSON content type should be skipped, got %d violations", len(v))
	}
}

func TestValidateResponse_JSONContentType(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode:  200,
				Description: "JSON",
				ContentType: "application/json",
				Schema: ObjectSchema(map[string]*Schema{
					"status": StringSchema("status"),
				}, "status"),
			},
		},
	}
	v := ValidateResponse(route, 200, []byte(`{"status":"ok"}`))
	if len(v) != 0 {
		t.Errorf("JSON content type with valid body should pass, got: %+v", v)
	}
}

func TestValidateResponse_StatusCodeFallback(t *testing.T) {
	// Route only defines 200 and 404 schemas
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Schema: ObjectSchema(map[string]*Schema{
				"status": StringSchema("status"),
			}, "status")},
			{StatusCode: 404, Schema: ObjectSchema(map[string]*Schema{
				"error": StringSchema("error"),
			}, "error")},
		},
	}

	// 404 response should validate against 404 schema
	v := ValidateResponse(route, 404, []byte(`{"error":"not found"}`))
	if len(v) != 0 {
		t.Errorf("404 response should validate against 404 schema, got: %+v", v)
	}
}

func TestValidateResponse_BooleanField(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode: 200,
				Schema: ObjectSchema(map[string]*Schema{
					"active": BoolSchema("active"),
				}, "active"),
			},
		},
	}
	body := []byte(`{"active":true}`)
	v := ValidateResponse(route, 200, body)
	if len(v) != 0 {
		t.Errorf("boolean field should validate, got: %+v", v)
	}

	body = []byte(`{"active":"yes"}`)
	v = ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected violation for string instead of boolean")
	}
}

func TestValidateResponse_NestedObject(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode: 200,
				Schema: ObjectSchema(map[string]*Schema{
					"user": ObjectSchema(map[string]*Schema{
						"id":   StringSchema("id"),
						"name": StringSchema("name"),
					}, "id", "name"), // both required for nested object
				}, "user"),
			},
		},
	}

	// Valid nested
	body := []byte(`{"user":{"id":"123","name":"Alice"}}`)
	v := ValidateResponse(route, 200, body)
	if len(v) != 0 {
		t.Errorf("valid nested should pass, got: %+v", v)
	}

	// Missing nested required field
	body = []byte(`{"user":{"id":"123"}}`)
	v = ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected violation for missing nested required field")
	}
}

func TestValidateResponse_RootArray(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{
				StatusCode: 200,
				Schema: ObjectSchema(map[string]*Schema{
					"items": ArraySchema(StringSchema("item"), "items"),
				}, "items"),
			},
		},
	}

	body := []byte(`{"items":["a","b","c"]}`)
	v := ValidateResponse(route, 200, body)
	if len(v) != 0 {
		t.Errorf("valid array in object should pass, got: %+v", v)
	}

	// Array with wrong item type
	body = []byte(`{"items":["a",123,"c"]}`)
	v = ValidateResponse(route, 200, body)
	if len(v) == 0 {
		t.Fatal("expected violation for wrong type in array")
	}
}

func TestValidateResponse_RootNotObject(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Schema: ObjectSchema(nil, "status")},
		},
	}

	v := ValidateResponse(route, 200, []byte(`"just a string"`))
	if len(v) == 0 {
		t.Fatal("expected violation for non-object root")
	}
	if v[0].Field != "(root)" {
		t.Errorf("expected field=(root), got %q", v[0].Field)
	}
}

// ─── ResponseCapture Tests ─────────────────────────────────────────────────

func TestResponseCapture_Write(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}

	n, err := rc.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}
	if rc.Status() != 200 {
		t.Errorf("default status should be 200, got %d", rc.Status())
	}
	if rc.body.Len() != 0 {
		t.Errorf("non-JSON body should not be captured, got %d bytes", rc.body.Len())
	}
}

func TestResponseCapture_CapturesJSON(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}

	json := `{"status":"ok"}`
	rc.Write([]byte(json))
	if rc.body.String() != json {
		t.Errorf("JSON body not captured: got %q, want %q", rc.body.String(), json)
	}
	if w.Body.String() != json {
		t.Errorf("body not passed through: got %q, want %q", w.Body.String(), json)
	}
}

func TestResponseCapture_OnlyFirstJSONChunk(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}

	// Write JSON first
	rc.Write([]byte(`{"key":`))
	rc.Write([]byte(`"value"}`))

	if rc.body.String() != `{"key":` {
		t.Errorf("only first chunk captured: got %q", rc.body.String())
	}
	if w.Body.String() != `{"key":"value"}` {
		t.Errorf("full body should pass through: got %q", w.Body.String())
	}
}

func TestResponseCapture_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}

	rc.WriteHeader(404)
	if rc.Status() != 404 {
		t.Errorf("expected status 404, got %d", rc.Status())
	}
}

func TestResponseCapture_WriteHeaderIdempotent(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}

	rc.WriteHeader(200)
	rc.WriteHeader(500) // should be ignored
	if rc.Status() != 200 {
		t.Errorf("expected status 200 (first write wins), got %d", rc.Status())
	}
}

func TestResponseCapture_DefaultStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rc := &responseCapture{ResponseWriter: w}
	if rc.Status() != 200 {
		t.Errorf("default status should be 200, got %d", rc.Status())
	}
}

// ─── ResponseValidator Middleware Tests ─────────────────────────────────────

func TestResponseValidator_Passes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := []Route{
		{
			Path: "/api/test", Method: GET,
			Responses: []RouteResponse{
				{
					StatusCode: 200,
					Schema: ObjectSchema(map[string]*Schema{
						"status": StringSchema("status"),
					}, "status"),
				},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{Logger: logger})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Errorf("body should pass through unchanged, got %q", rec.Body.String())
	}
}

func TestResponseValidator_NonAPIPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := DashboardRoutes()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{Logger: logger})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/static/file.txt", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("non-API path should pass through, got %d", rec.Code)
	}
}

func TestResponseValidator_SkipPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := []Route{
		{
			Path: "/api/skip", Method: GET,
			Responses: []RouteResponse{
				{StatusCode: 200, Schema: ObjectSchema(map[string]*Schema{
					"value": IntSchema("value"),
				}, "value")},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":"not-a-number"}`)) // would normally violate
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{
		Logger:    logger,
		SkipPaths: map[string]bool{"/api/skip": true},
	})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/skip", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("skipped path should return 200, got %d", rec.Code)
	}
}

func TestResponseValidator_UnknownRoute(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := []Route{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"key":"value"}`))
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{Logger: logger})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/unknown", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("unknown route should pass through, got %d", rec.Code)
	}
}

func TestResponseValidator_NilConfig(t *testing.T) {
	routes := []Route{
		{
			Path: "/api/test", Method: GET,
			Responses: []RouteResponse{
				{StatusCode: 200, Schema: StringSchema("status")},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"ok"`))
	})

	validator := ResponseValidator(routes, nil)
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("nil config should work, got %d", rec.Code)
	}
}

func TestResponseValidator_NonJSONResponse(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	routes := []Route{
		{
			Path: "/api/alerts/rules", Method: GET,
			Responses: []RouteResponse{
				{StatusCode: 200, Description: "YAML", ContentType: "text/yaml", Schema: StringSchema("rules")},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		w.Write([]byte("groups:\n  - name: test"))
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{Logger: logger})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/alerts/rules", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("non-JSON response should pass through, got %d", rec.Code)
	}
}

func TestResponseValidator_DriftLogging(t *testing.T) {
	// Use a buffer to capture log output
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	routes := []Route{
		{
			Path: "/api/test", Method: GET,
			Responses: []RouteResponse{
				{
					StatusCode: 200,
					Schema: ObjectSchema(map[string]*Schema{
						"status": StringSchema("status"),
					}, "status", "message"), // message is required
				},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`)) // missing "message"
	})

	validator := ResponseValidator(routes, &ResponseValidatorConfig{Logger: logger})
	wrapped := validator(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("response should still be 200 despite drift, got %d", rec.Code)
	}

	logOutput := buf.String()
	if logOutput == "" {
		t.Error("expected drift warning in logs, got nothing")
	}
	if !strings.Contains(logOutput, "message") || !strings.Contains(logOutput, "missing required field") {
		t.Errorf("log should contain drift warning about 'message', got: %s", logOutput)
	}
}

func TestValidateResponse_FindResponseExact(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Description: "ok"},
			{StatusCode: 404, Description: "not found"},
			{StatusCode: 500, Description: "error"},
		},
	}

	if got := findResponse(route, 404); got == nil || got.Description != "not found" {
		t.Errorf("findResponse(404) = %v, want 'not found'", got)
	}
	if got := findResponse(route, 500); got == nil || got.Description != "error" {
		t.Errorf("findResponse(500) = %v, want 'error'", got)
	}
}

func TestValidateResponse_FindResponseFallback(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Description: "ok"},
		},
	}

	// Status 201 is not in the list — should fall back to first (200)
	if got := findResponse(route, 201); got == nil || got.Description != "ok" {
		t.Errorf("findResponse(201) should fall back to 200, got %v", got)
	}
}

func TestValidateResponse_FindResponseEmpty(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{},
	}
	if got := findResponse(route, 200); got != nil {
		t.Errorf("findResponse on empty responses should return nil, got %v", got)
	}
}

// ─── SchemaViolation JSON Tests ────────────────────────────────────────────

func TestSchemaViolation_JSON(t *testing.T) {
	v := SchemaViolation{Field: "name", Message: "expected string, got int"}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded SchemaViolation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Field != "name" || decoded.Message != "expected string, got int" {
		t.Errorf("roundtrip failed: got %+v", decoded)
	}
}

// ─── DashboardRoutes Integration Test ──────────────────────────────────────

func TestDashboardRoutes_AllIndexable(t *testing.T) {
	routes := DashboardRoutes()
	ri := NewRouteIndex(routes)

	if ri.Len() == 0 {
		t.Fatal("expected non-empty route index")
	}

	// Verify all routes are lookup-able
	for _, route := range routes {
		got := ri.Lookup(string(route.Method), route.Path)
		if got == nil {
			t.Errorf("Route %s %s not found in index", route.Method, route.Path)
		}
	}
}

// ─── Edge Cases ────────────────────────────────────────────────────────────

func TestValidateResponse_NilSchemaInResponse(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Schema: nil},
		},
	}
	v := ValidateResponse(route, 200, []byte(`{"key":"value"}`))
	if len(v) != 0 {
		t.Errorf("nil schema should return no violations, got %d", len(v))
	}
}

func TestValidateResponse_NilProperties(t *testing.T) {
	route := &Route{
		Responses: []RouteResponse{
			{StatusCode: 200, Schema: &Schema{Type: "object"}},
		},
	}
	v := ValidateResponse(route, 200, []byte(`{"anything":"goes","also":42}`))
	if len(v) != 0 {
		t.Errorf("object with nil properties should accept anything, got: %+v", v)
	}
}

func TestRouteIndex_NilReceiver(t *testing.T) {
	var ri *RouteIndex
	if ri.Len() != 0 {
		t.Error("nil receiver Len should return 0")
	}
	if ri.Lookup("GET", "/api/test") != nil {
		t.Error("nil receiver Lookup should return nil")
	}
}
