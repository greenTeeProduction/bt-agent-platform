package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIGenerator_Generate(t *testing.T) {
	gen := NewOpenAPIGenerator("BT Platform API", "1.0.0", "Dashboard API for the Go BT platform")
	gen.AddServer("http://localhost:9800", "Development server")

	gen.AddRoute(NewRoute("/api/health", GET).
		Summary("Health check").
		Tags("System").
		JSONResponse(200, "OK", ObjectSchema(map[string]*Schema{
			"status": StringSchema("Status"),
		})).Build())

	spec := gen.Generate()

	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI version 3.0.3, got %q", spec.OpenAPI)
	}
	if spec.Info.Title != "BT Platform API" {
		t.Errorf("expected title 'BT Platform API', got %q", spec.Info.Title)
	}
	if spec.Info.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", spec.Info.Version)
	}

	// Check server
	if len(spec.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(spec.Servers))
	}
	if spec.Servers[0].URL != "http://localhost:9800" {
		t.Errorf("expected server URL, got %q", spec.Servers[0].URL)
	}

	// Check path exists
	healthPath, ok := spec.Paths["/api/health"]
	if !ok {
		t.Fatal("expected /api/health path")
	}
	healthGet, ok := healthPath["get"]
	if !ok {
		t.Fatal("expected GET method on /api/health")
	}
	healthMap := healthGet.(map[string]interface{})
	if healthMap["summary"] != "Health check" {
		t.Errorf("expected summary 'Health check', got %v", healthMap["summary"])
	}
	if tags, ok := healthMap["tags"].([]string); !ok || len(tags) != 1 || tags[0] != "System" {
		t.Errorf("expected tags ['System'], got %v", healthMap["tags"])
	}
}

func TestOpenAPIGenerator_GenerateJSON(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	gen.AddRoute(NewRoute("/api/test", GET).
		Summary("Test endpoint").
		JSONResponse(200, "OK", StringSchema("response")).Build())

	data, err := gen.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	// Verify valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["openapi"] != "3.0.3" {
		t.Errorf("expected openapi 3.0.3, got %v", parsed["openapi"])
	}

	// Pretty-printed JSON should have newlines
	if !strings.Contains(string(data), "\n") {
		t.Error("GenerateJSON should produce indented (multiline) output")
	}
}

func TestOpenAPIGenerator_GenerateJSONCompact(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	gen.AddRoute(NewRoute("/api/test", GET).
		Summary("Test endpoint").
		JSONResponse(200, "OK", StringSchema("response")).Build())

	data, err := gen.GenerateJSONCompact()
	if err != nil {
		t.Fatalf("GenerateJSONCompact failed: %v", err)
	}

	// Verify valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Compact JSON should NOT have newlines
	if strings.Contains(string(data), "\n") {
		t.Error("GenerateJSONCompact should produce compact (single-line) output")
	}
}

func TestOpenAPIGenerator_MultipleRoutes(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	gen.AddRoute(NewRoute("/api/a", GET).Summary("A").JSONResponse(200, "OK", StringSchema("")).Build())
	gen.AddRoute(NewRoute("/api/b", POST).Summary("B").JSONResponse(201, "Created", StringSchema("")).Build())

	spec := gen.Generate()
	if len(spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(spec.Paths))
	}

	// Verify both paths exist (not order — Go map iteration is unordered)
	if _, ok := spec.Paths["/api/a"]; !ok {
		t.Error("expected /api/a path")
	}
	if _, ok := spec.Paths["/api/b"]; !ok {
		t.Error("expected /api/b path")
	}

	// Check POST route has correct status
	bPath := spec.Paths["/api/b"]["post"].(map[string]interface{})
	responses := bPath["responses"].(map[string]interface{})
	if _, ok := responses["201"]; !ok {
		t.Errorf("expected 201 response on /api/b POST, got keys: %v", mapKeys(responses))
	}

	// Verify JSON output has paths in alphabetical order
	data, err := gen.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}
	aIdx := strings.Index(string(data), "\"/api/a\"")
	bIdx := strings.Index(string(data), "\"/api/b\"")
	if aIdx == -1 || bIdx == -1 {
		t.Fatal("expected /api/a and /api/b in JSON output")
	}
	if aIdx > bIdx {
		t.Errorf("expected /api/a before /api/b in sorted JSON output (aIdx=%d, bIdx=%d)", aIdx, bIdx)
	}
}

func TestOpenAPIGenerator_Parameters(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	gen.AddRoute(NewRoute("/api/search", GET).
		Summary("Search").
		QueryParam("q", "Search query", true, StringSchema("Query")).
		QueryParam("limit", "Max results", false, IntSchema("Limit")).
		JSONResponse(200, "Results", ArraySchema(StringSchema("Item"), "")).Build())

	spec := gen.Generate()
	searchPath := spec.Paths["/api/search"]["get"].(map[string]interface{})
	params := searchPath["parameters"].([]map[string]interface{})

	if len(params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(params))
	}
	if params[0]["name"] != "q" {
		t.Errorf("expected first param 'q', got %v", params[0]["name"])
	}
	if params[0]["required"] != true {
		t.Error("expected 'q' to be required")
	}
	if params[1]["name"] != "limit" {
		t.Errorf("expected second param 'limit', got %v", params[1]["name"])
	}
	if params[1]["required"] != false {
		t.Error("expected 'limit' to be optional")
	}

	// Verify schema types
	qSchema := params[0]["schema"].(map[string]interface{})
	if qSchema["type"] != "string" {
		t.Errorf("expected q schema type 'string', got %v", qSchema["type"])
	}

	limitSchema := params[1]["schema"].(map[string]interface{})
	if limitSchema["type"] != "integer" {
		t.Errorf("expected limit schema type 'integer', got %v", limitSchema["type"])
	}
}

func TestOpenAPIGenerator_RequestBody(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")

	reqSchema := ObjectSchema(map[string]*Schema{
		"name":  StringSchema("Agent name"),
		"email": StringSchema("Email address"),
	}, "name")

	gen.AddRoute(NewRoute("/api/agents", POST).
		Summary("Create agent").
		JSONResponse(201, "Created", ObjectSchema(map[string]*Schema{
			"id": StringSchema("Agent ID"),
		})).Build())
	// Set request body after building
	routes := gen.routes
	routes[len(routes)-1].RequestBody = reqSchema
	gen.routes = routes

	spec := gen.Generate()
	agentPath := spec.Paths["/api/agents"]["post"].(map[string]interface{})
	reqBody := agentPath["requestBody"].(map[string]interface{})

	if reqBody["required"] != true {
		t.Error("expected requestBody required=true")
	}

	content := reqBody["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})
	jsonSchema := jsonContent["schema"].(map[string]interface{})

	if jsonSchema["type"] != "object" {
		t.Errorf("expected object schema, got %v", jsonSchema["type"])
	}
	required := jsonSchema["required"].([]interface{})
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("expected required ['name'], got %v", required)
	}
}

func TestOpenAPIGenerator_ErrorResponses(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	gen.AddRoute(NewRoute("/api/items/{id}", GET).
		Summary("Get item").
		JSONResponse(200, "Item found", ObjectSchema(map[string]*Schema{
			"id":   StringSchema("ID"),
			"name": StringSchema("Name"),
		})).
		ErrorResponse(404, "Not found").
		ErrorResponse(500, "Server error").Build())

	spec := gen.Generate()
	itemPath := spec.Paths["/api/items/{id}"]["get"].(map[string]interface{})
	responses := itemPath["responses"].(map[string]interface{})

	if _, ok := responses["200"]; !ok {
		t.Error("expected 200 response")
	}
	if _, ok := responses["404"]; !ok {
		t.Error("expected 404 response")
	}
	if _, ok := responses["500"]; !ok {
		t.Error("expected 500 response")
	}

	// Check error response has schema
	error404 := responses["404"].(map[string]interface{})
	errorContent := error404["content"].(map[string]interface{})
	errorJSON := errorContent["application/json"].(map[string]interface{})
	errorSchema := errorJSON["schema"].(map[string]interface{})
	if errorSchema["type"] != "object" {
		t.Errorf("expected error schema type 'object', got %v", errorSchema["type"])
	}
}

func TestOpenAPIGenerator_AuthFlag(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")

	// Public route
	gen.AddRoute(NewRoute("/api/public", GET).
		Summary("Public").Build())

	// Authenticated route
	gen.AddRoute(NewRoute("/api/private", GET).
		Summary("Private").WithAuth().Build())

	spec := gen.Generate()

	// Public route should NOT have security
	publicPath := spec.Paths["/api/public"]["get"].(map[string]interface{})
	if _, hasSec := publicPath["security"]; hasSec {
		t.Error("public route should not have security")
	}

	// Private route should have security
	privatePath := spec.Paths["/api/private"]["get"].(map[string]interface{})
	sec, hasSec := privatePath["security"].([]map[string][]string)
	if !hasSec {
		t.Error("private route should have security")
	}
	if len(sec) != 1 {
		t.Fatalf("expected 1 security requirement, got %d", len(sec))
	}
	if _, ok := sec[0]["ApiKeyAuth"]; !ok {
		t.Error("expected ApiKeyAuth security scheme")
	}

	// Check security schemes in components
	schemes := spec.Components.SecuritySchemes
	if schemes == nil {
		t.Fatal("expected security schemes in components")
	}
	apiKeyScheme := schemes["ApiKeyAuth"].(map[string]interface{})
	if apiKeyScheme["type"] != "apiKey" {
		t.Errorf("expected apiKey type, got %v", apiKeyScheme["type"])
	}
	if apiKeyScheme["name"] != "X-API-Key" {
		t.Errorf("expected header name X-API-Key, got %v", apiKeyScheme["name"])
	}
}

func TestOpenAPIGenerator_Deprecated(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")

	gen.AddRoute(NewRoute("/api/v1/old", GET).
		Summary("Old endpoint").Deprecated().Build())

	gen.AddRoute(NewRoute("/api/v2/new", GET).
		Summary("New endpoint").Build())

	spec := gen.Generate()

	oldPath := spec.Paths["/api/v1/old"]["get"].(map[string]interface{})
	if deprecated, ok := oldPath["deprecated"].(bool); !ok || !deprecated {
		t.Error("expected deprecated=true on old route")
	}

	newPath := spec.Paths["/api/v2/new"]["get"].(map[string]interface{})
	if _, ok := newPath["deprecated"]; ok {
		t.Error("new route should not have deprecated flag")
	}
}

func TestOpenAPIGenerator_ReusableSchemas(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")

	taskSchema := ObjectSchema(map[string]*Schema{
		"id":       StringSchema("Task ID"),
		"title":    StringSchema("Task title"),
		"status":   StringSchema("Task status"),
		"priority": StringSchema("Priority"),
	}, "id", "title")
	gen.AddSchema("Task", schemaToMap(taskSchema))

	spec := gen.Generate()

	if spec.Components.Schemas == nil {
		t.Fatal("expected schemas in components")
	}
	if _, ok := spec.Components.Schemas["Task"]; !ok {
		t.Error("expected Task schema in components")
	}

	taskDef := spec.Components.Schemas["Task"].(map[string]interface{})
	if taskDef["type"] != "object" {
		t.Errorf("expected object type, got %v", taskDef["type"])
	}
}

func TestOpenAPIGenerator_EmptyRoutes(t *testing.T) {
	gen := NewOpenAPIGenerator("Test API", "0.1.0", "")
	spec := gen.Generate()

	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(spec.Paths))
	}
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected openapi 3.0.3 even with no routes")
	}
}

func TestSchemaToMap_Nil(t *testing.T) {
	m := schemaToMap(nil)
	if m["type"] != "object" {
		t.Errorf("expected nil schema to default to object, got %v", m["type"])
	}
}

func TestSchemaToMap_Object(t *testing.T) {
	minLen := 3
	maxLen := 50
	s := &Schema{
		Type:        "object",
		Properties: map[string]*Schema{
			"name": {Type: "string", Description: "Full name", MinLength: &minLen, MaxLength: &maxLen},
			"age":  {Type: "integer"},
		},
		Required:    []string{"name"},
		Description: "Person object",
	}

	m := schemaToMap(s)

	if m["type"] != "object" {
		t.Errorf("expected type 'object', got %v", m["type"])
	}
	if m["description"] != "Person object" {
		t.Errorf("expected description, got %v", m["description"])
	}

	required := m["required"].([]interface{})
	if len(required) != 1 || required[0] != "name" {
		t.Errorf("expected required ['name'], got %v", required)
	}

	props := m["properties"].(map[string]interface{})
	nameProp := props["name"].(map[string]interface{})
	if nameProp["type"] != "string" {
		t.Errorf("expected name type 'string', got %v", nameProp["type"])
	}
	if nameProp["minLength"].(int) != 3 {
		t.Errorf("expected minLength 3")
	}
	if nameProp["maxLength"].(int) != 50 {
		t.Errorf("expected maxLength 50")
	}
}

func TestSchemaToMap_Array(t *testing.T) {
	s := &Schema{
		Type: "array",
		Items: &Schema{
			Type:        "string",
			Description: "A tag value",
		},
		MinLength: &[]int{5}[0],
	}

	m := schemaToMap(s)

	if m["type"] != "array" {
		t.Errorf("expected type 'array', got %v", m["type"])
	}
	items := m["items"].(map[string]interface{})
	if items["type"] != "string" {
		t.Errorf("expected items type 'string', got %v", items["type"])
	}
}

func TestSchemaToMap_Enum(t *testing.T) {
	s := &Schema{
		Type: "string",
		Enum: []string{"pending", "in_progress", "completed"},
	}

	m := schemaToMap(s)

	enum := m["enum"].([]interface{})
	if len(enum) != 3 {
		t.Fatalf("expected 3 enum values, got %d", len(enum))
	}
	if enum[0] != "pending" {
		t.Errorf("expected first enum 'pending', got %v", enum[0])
	}
}

func TestSchemaToMap_NumberBounds(t *testing.T) {
	min := 0.0
	max := 100.0
	s := &Schema{
		Type:    "number",
		Minimum: &min,
		Maximum: &max,
		Format:  "float",
	}

	m := schemaToMap(s)

	if m["minimum"].(float64) != 0.0 {
		t.Errorf("expected minimum 0.0, got %v", m["minimum"])
	}
	if m["maximum"].(float64) != 100.0 {
		t.Errorf("expected maximum 100.0, got %v", m["maximum"])
	}
	if m["format"] != "float" {
		t.Errorf("expected format 'float', got %v", m["format"])
	}
}

func TestHTTPStatusText(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{200, "200"},
		{404, "404"},
		{500, "500"},
		{201, "201"},
		{301, "301"},
		{99, "default"},   // below HTTP range
		{600, "default"},  // above HTTP range
		{0, "default"},    // zero
	}

	for _, tc := range tests {
		result := httpStatusText(tc.code)
		if result != tc.expected {
			t.Errorf("httpStatusText(%d) = %q, want %q", tc.code, result, tc.expected)
		}
	}
}

func TestDashboardRoutes_Completeness(t *testing.T) {
	routes := DashboardRoutes()

	if len(routes) == 0 {
		t.Fatal("expected non-empty dashboard routes")
	}

	// All routes should have a summary
	for i, r := range routes {
		if r.Summary == "" {
			t.Errorf("route %d (%s %s) has empty summary", i, r.Method, r.Path)
		}
		if r.Tags == nil || len(r.Tags) == 0 {
			t.Errorf("route %d (%s %s) has no tags", i, r.Method, r.Path)
		}
		if len(r.Responses) == 0 {
			t.Errorf("route %d (%s %s) has no responses", i, r.Method, r.Path)
		}
	}

	// Verify unique paths
	seen := make(map[string]bool)
	for _, r := range routes {
		key := string(r.Method) + " " + r.Path
		if seen[key] {
			t.Errorf("duplicate route: %s", key)
		}
		seen[key] = true
	}
}

func TestDashboardRoutes_GeneratesValidSpec(t *testing.T) {
	routes := DashboardRoutes()
	gen := NewOpenAPIGenerator("BT Platform API", "1.0.0", "Dashboard API")
	for _, r := range routes {
		gen.AddRoute(r)
	}

	spec := gen.Generate()

	// Verify it's a valid OpenAPI 3.0 structure
	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected openapi 3.0.3")
	}
	if spec.Info.Title != "BT Platform API" {
		t.Errorf("expected title")
	}
	if len(spec.Paths) == 0 {
		t.Error("expected non-empty paths")
	}

	// All paths should have at least one method
	for path, methods := range spec.Paths {
		if len(methods) == 0 {
			t.Errorf("path %s has no methods", path)
		}
		for method, obj := range methods {
			mObj := obj.(map[string]interface{})
			if _, ok := mObj["responses"]; !ok {
				t.Errorf("path %s method %s missing responses", path, method)
			}
		}
	}

	// Verify known endpoints exist
	requiredPaths := []string{
		"/api/health",
		"/api/metrics",
		"/api/alerts",
		"/api/openapi.json",
		"/api/swagger",
		"/api/scalability",
		"/api/summary",
		"/api/trees",
		"/api/tree/structure",
		"/api/thinktank/fellows",
		"/api/thinktank/analyze",
		"/api/company/default",
		"/api/tasks",
		"/api/tasks/approve",
		"/api/tasks/reject",
		"/api/sprint/execute",
		"/api/sprint/status",
		"/api/chat",
	}
	for _, p := range requiredPaths {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("required path %s missing from spec", p)
		}
	}
}

func TestDashboardRoutes_AuthConsistency(t *testing.T) {
	routes := DashboardRoutes()
	gen := NewOpenAPIGenerator("BT Platform API", "1.0.0", "")
	for _, r := range routes {
		gen.AddRoute(r)
	}
	spec := gen.Generate()

	// /api/health, /api/metrics, /api/alerts, /api/openapi.json should be public
	publicPaths := map[string]bool{
		"/api/health":       true,
		"/api/metrics":      true,
		"/api/alerts":       true,
		"/api/openapi.json": true,
		"/api/swagger":      true,
		"/api/scalability":  true,
	}

	for path := range spec.Paths {
		for _, method := range []string{"get", "post", "put", "delete"} {
			if obj, ok := spec.Paths[path][method]; ok {
				mObj := obj.(map[string]interface{})
				_, hasSec := mObj["security"]
				if publicPaths[path] && hasSec {
					t.Errorf("public path %s %s should not have security", path, method)
				}
				if !publicPaths[path] && !hasSec {
					t.Logf("note: %s %s has no security (may be intentional)", path, method)
				}
			}
		}
	}
}

func TestRouteBuilder_FluentAPI(t *testing.T) {
	route := NewRoute("/api/example", POST).
		Summary("Example endpoint").
		Description("Detailed description").
		Tags("Examples", "Testing").
		QueryParam("id", "Item ID", true, StringSchema("ID")).
		QueryParam("verbose", "Enable verbose mode", false, BoolSchema("Verbose flag")).
		JSONResponse(200, "Success", ObjectSchema(map[string]*Schema{
			"data": StringSchema("Result data"),
		})).
		ErrorResponse(400, "Bad request").
		ErrorResponse(500, "Internal error").
		WithAuth().
		Build()

	if route.Path != "/api/example" {
		t.Errorf("expected path '/api/example', got %q", route.Path)
	}
	if route.Method != POST {
		t.Errorf("expected method POST, got %q", route.Method)
	}
	if route.Summary != "Example endpoint" {
		t.Errorf("expected summary, got %q", route.Summary)
	}
	if route.Description != "Detailed description" {
		t.Errorf("expected description, got %q", route.Description)
	}
	if len(route.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(route.Tags))
	}
	if len(route.Parameters) != 2 {
		t.Errorf("expected 2 params, got %d", len(route.Parameters))
	}
	if len(route.Responses) != 3 {
		t.Errorf("expected 3 responses (200+400+500), got %d", len(route.Responses))
	}
	if !route.Auth {
		t.Error("expected auth=true")
	}

	// Verify param details
	if route.Parameters[0].Required != true {
		t.Error("expected first param required")
	}
	if route.Parameters[1].Required != false {
		t.Error("expected second param optional")
	}
}

func TestRouteBuilder_DeprecatedChain(t *testing.T) {
	route := NewRoute("/api/old", GET).
		Summary("Old route").
		Tags("Legacy").
		Deprecated().
		JSONResponse(200, "OK", StringSchema("Data")).
		Build()

	if !route.Deprecated {
		t.Error("expected deprecated=true")
	}
}

func TestConvenienceSchemaConstructors(t *testing.T) {
	// StringSchema
	s := StringSchema("A string field")
	if s.Type != "string" {
		t.Errorf("expected type 'string', got %q", s.Type)
	}
	if s.Description != "A string field" {
		t.Errorf("expected description, got %q", s.Description)
	}

	// IntSchema
	i := IntSchema("An integer field")
	if i.Type != "integer" {
		t.Errorf("expected type 'integer', got %q", i.Type)
	}

	// NumberSchema
	n := NumberSchema("A number field")
	if n.Type != "number" {
		t.Errorf("expected type 'number', got %q", n.Type)
	}

	// BoolSchema
	b := BoolSchema("A boolean field")
	if b.Type != "boolean" {
		t.Errorf("expected type 'boolean', got %q", b.Type)
	}

	// ArraySchema
	a := ArraySchema(StringSchema("Item"), "An array of strings")
	if a.Type != "array" {
		t.Errorf("expected type 'array', got %q", a.Type)
	}
	if a.Items == nil {
		t.Error("expected non-nil items")
	}
	if a.Items.Type != "string" {
		t.Errorf("expected item type 'string', got %q", a.Items.Type)
	}

	// ObjectSchema
	o := ObjectSchema(map[string]*Schema{
		"name": StringSchema("Name"),
	}, "name")
	if o.Type != "object" {
		t.Errorf("expected type 'object', got %q", o.Type)
	}
	if len(o.Required) != 1 || o.Required[0] != "name" {
		t.Errorf("expected required ['name']")
	}
	if len(o.Properties) != 1 {
		t.Errorf("expected 1 property")
	}
}

func TestDashboardRoutes_SwaggerRoute(t *testing.T) {
	routes := DashboardRoutes()
	var swaggerRoute *Route
	for i := range routes {
		if routes[i].Path == "/api/swagger" {
			swaggerRoute = &routes[i]
			break
		}
	}
	if swaggerRoute == nil {
		t.Fatal("expected /api/swagger route in DashboardRoutes")
	}
	if swaggerRoute.Method != GET {
		t.Errorf("expected GET method, got %s", swaggerRoute.Method)
	}
	if swaggerRoute.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if swaggerRoute.Tags == nil || len(swaggerRoute.Tags) == 0 {
		t.Error("expected tags")
	}
	// Swagger UI is a public endpoint — no auth required
	if swaggerRoute.Auth {
		t.Error("expected /api/swagger to be a public endpoint (no auth)")
	}
	// Should have an HTML content type response
	found200 := false
	for _, r := range swaggerRoute.Responses {
		if r.StatusCode == 200 {
			found200 = true
			if r.ContentType != "text/html" {
				t.Errorf("expected ContentType text/html for 200 response, got %q", r.ContentType)
			}
		}
	}
	if !found200 {
		t.Error("expected 200 response for /api/swagger")
	}
}

// Helper
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
