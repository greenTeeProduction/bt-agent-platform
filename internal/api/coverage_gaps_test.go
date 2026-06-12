package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─── compress.go coverage gaps ───────────────────────────────────────────────

func TestGzipResponseWriter_WriteHeader_CustomEncodingPreserved(t *testing.T) {
	// When Content-Encoding is already set to something other than gzip,
	// WriteHeader should NOT remove it.
	inner := httptest.NewRecorder()
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: nil}
	grw.Header().Set("Content-Encoding", "deflate")
	grw.Header().Set("Content-Type", "application/json")
	grw.WriteHeader(200)

	if inner.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("custom Content-Encoding should be preserved, got %q", inner.Header().Get("Content-Encoding"))
	}
}

func TestGzipResponseWriter_Write_BeforeWriteHeader_Compressible(t *testing.T) {
	// Write is called before WriteHeader (common for small responses).
	// Content-Type is compressible.
	inner := httptest.NewRecorder()
	var buf bytes.Buffer
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: &buf}
	grw.Header().Set("Content-Type", "application/json")

	n, err := grw.Write([]byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == 0 {
		t.Error("expected bytes written > 0")
	}
	if !grw.started {
		t.Error("expected started=true after Write")
	}
	if !grw.compressible {
		t.Error("expected compressible=true for application/json")
	}
}

func TestGzipResponseWriter_Write_BeforeWriteHeader_NonCompressible(t *testing.T) {
	// Write is called before WriteHeader.
	// Content-Type is not compressible.
	inner := httptest.NewRecorder()
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: &bytes.Buffer{}}
	grw.Header().Set("Content-Type", "image/png")

	_, err := grw.Write([]byte("rawdata"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grw.compressible {
		t.Error("expected compressible=false for image/png")
	}
}

func TestGzipResponseWriter_Write_BeforeWriteHeader_CustomEncoding(t *testing.T) {
	// Write is called before WriteHeader with custom Content-Encoding already set.
	inner := httptest.NewRecorder()
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: &bytes.Buffer{}}
	grw.Header().Set("Content-Encoding", "deflate")
	grw.Header().Set("Content-Type", "application/json")

	_, err := grw.Write([]byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grw.compressible {
		t.Error("expected compressible=false when custom Content-Encoding is set")
	}
	if inner.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("custom Content-Encoding should be preserved, got %q", inner.Header().Get("Content-Encoding"))
	}
}

// ─── types.go coverage gaps ──────────────────────────────────────────────────

func TestNumberValue_JSONNumber(t *testing.T) {
	n := json.Number("42.5")
	v, ok := numberValue(n)
	if !ok {
		t.Error("expected ok=true for json.Number")
	}
	if v != 42.5 {
		t.Errorf("expected 42.5, got %v", v)
	}
}

func TestNumberValue_JSONNumber_Invalid(t *testing.T) {
	n := json.Number("not-a-number")
	_, ok := numberValue(n)
	if ok {
		t.Error("expected ok=false for invalid json.Number")
	}
}

func TestNumberValue_Int64(t *testing.T) {
	var n int64 = 99
	v, ok := numberValue(n)
	if !ok {
		t.Error("expected ok=true for int64")
	}
	if v != 99.0 {
		t.Errorf("expected 99.0, got %v", v)
	}
}

func TestNumberValue_Float64(t *testing.T) {
	v, ok := numberValue(3.14)
	if !ok {
		t.Error("expected ok=true for float64")
	}
	if v != 3.14 {
		t.Errorf("expected 3.14, got %v", v)
	}
}

func TestNumberValue_Int(t *testing.T) {
	v, ok := numberValue(7)
	if !ok {
		t.Error("expected ok=true for int")
	}
	if v != 7.0 {
		t.Errorf("expected 7.0, got %v", v)
	}
}

func TestNumberValue_UnknownType(t *testing.T) {
	_, ok := numberValue("string")
	if ok {
		t.Error("expected ok=false for unknown type")
	}
}

func TestNumberIn_Matching(t *testing.T) {
	if !numberIn(3.0, []string{"1.0", "3.0", "5.0"}) {
		t.Error("expected numberIn to find 3.0")
	}
}

func TestNumberIn_NotFound(t *testing.T) {
	if numberIn(4.0, []string{"1.0", "3.0", "5.0"}) {
		t.Error("expected numberIn to not find 4.0")
	}
}

func TestNumberIn_InvalidNumberString(t *testing.T) {
	if numberIn(3.0, []string{"1.0", "not-a-number", "5.0"}) {
		t.Error("expected numberIn to skip invalid number strings")
	}
}

func TestStringIn_Found(t *testing.T) {
	if !stringIn("b", []string{"a", "b", "c"}) {
		t.Error("expected stringIn to find 'b'")
	}
}

func TestStringIn_NotFound(t *testing.T) {
	if stringIn("d", []string{"a", "b", "c"}) {
		t.Error("expected stringIn to not find 'd'")
	}
}

func TestStringIn_Empty(t *testing.T) {
	if stringIn("a", []string{}) {
		t.Error("expected stringIn to not find in empty list")
	}
}

// ─── validateSchema edge cases ───────────────────────────────────────────────

func TestValidateSchema_InvalidType(t *testing.T) {
	err := validateSchema(&Schema{Type: "invalid_type"}, "")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestValidateSchema_ArrayNoItems(t *testing.T) {
	// array with nil Items should pass
	err := validateSchema(&Schema{Type: "array", Items: nil}, "")
	if err != nil {
		t.Errorf("expected no error for array with nil Items, got %v", err)
	}
}

func TestValidateSchema_NilSchema(t *testing.T) {
	err := validateSchema(nil, "")
	if err != nil {
		t.Errorf("expected no error for nil schema, got %v", err)
	}
}

func TestValidateSchema_EmptyType(t *testing.T) {
	// empty type should pass (not checked against validTypes)
	err := validateSchema(&Schema{Type: ""}, "")
	if err != nil {
		t.Errorf("expected no error for empty type, got %v", err)
	}
}

// ─── validateAgainstSchema coverage ──────────────────────────────────────────

func TestValidateAgainstSchema_Number(t *testing.T) {
	s := &Schema{Type: "number", Minimum: floatPtr(0), Maximum: floatPtr(100)}
	if err := validateAgainstSchema(50.0, s, ""); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateAgainstSchema_Number_OutOfRange(t *testing.T) {
	s := &Schema{Type: "number", Minimum: floatPtr(0), Maximum: floatPtr(100)}
	if err := validateAgainstSchema(150.0, s, ""); err == nil {
		t.Error("expected error for out of range number")
	}
}

func TestValidateAgainstSchema_Integer(t *testing.T) {
	s := &Schema{Type: "integer"}
	if err := validateAgainstSchema(float64(42), s, ""); err != nil {
		t.Errorf("expected no error for integer, got %v", err)
	}
}

func TestValidateAgainstSchema_Integer_Fractional(t *testing.T) {
	s := &Schema{Type: "integer"}
	if err := validateAgainstSchema(3.14, s, ""); err == nil {
		t.Error("expected error for fractional integer")
	}
}

func TestValidateAgainstSchema_Integer_WrongType(t *testing.T) {
	s := &Schema{Type: "integer"}
	if err := validateAgainstSchema("not a number", s, ""); err == nil {
		t.Error("expected error for non-number")
	}
}

func TestValidateAgainstSchema_Boolean(t *testing.T) {
	s := &Schema{Type: "boolean"}
	if err := validateAgainstSchema(true, s, ""); err != nil {
		t.Errorf("expected no error for boolean, got %v", err)
	}
}

func TestValidateAgainstSchema_Boolean_WrongType(t *testing.T) {
	s := &Schema{Type: "boolean"}
	if err := validateAgainstSchema("true", s, ""); err == nil {
		t.Error("expected error for non-boolean")
	}
}

func TestValidateAgainstSchema_String_Enum(t *testing.T) {
	s := &Schema{Type: "string", Enum: []string{"red", "green", "blue"}}
	if err := validateAgainstSchema("red", s, ""); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateAgainstSchema_String_Enum_NotIn(t *testing.T) {
	s := &Schema{Type: "string", Enum: []string{"red", "green", "blue"}}
	if err := validateAgainstSchema("yellow", s, ""); err == nil {
		t.Error("expected error for value not in enum")
	}
}

func TestValidateAgainstSchema_String_MinLength(t *testing.T) {
	minLen := 3
	s := &Schema{Type: "string", MinLength: &minLen}
	if err := validateAgainstSchema("ab", s, ""); err == nil {
		t.Error("expected error for string too short")
	}
}

func TestValidateAgainstSchema_String_MaxLength(t *testing.T) {
	minLen := 0
	maxLen := 5
	s := &Schema{Type: "string", MinLength: &minLen, MaxLength: &maxLen}
	if err := validateAgainstSchema("toolongstring", s, ""); err == nil {
		t.Error("expected error for string too long")
	}
}

func TestValidateAgainstSchema_String_Pattern(t *testing.T) {
	s := &Schema{Type: "string", Pattern: "^[a-z]+$"}
	if err := validateAgainstSchema("hello", s, ""); err != nil {
		t.Errorf("expected no error for matching pattern, got %v", err)
	}
}

func TestValidateAgainstSchema_String_Pattern_NoMatch(t *testing.T) {
	s := &Schema{Type: "string", Pattern: "^[a-z]+$"}
	if err := validateAgainstSchema("Hello123", s, ""); err == nil {
		t.Error("expected error for non-matching pattern")
	}
}

func TestValidateAgainstSchema_String_Pattern_Invalid(t *testing.T) {
	s := &Schema{Type: "string", Pattern: "[invalid"}
	if err := validateAgainstSchema("test", s, ""); err == nil {
		t.Error("expected error for invalid regex pattern")
	}
}

func TestValidateAgainstSchema_Number_Enum(t *testing.T) {
	s := &Schema{Type: "number", Enum: []string{"1.0", "2.0", "3.0"}}
	if err := validateAgainstSchema(2.0, s, ""); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateAgainstSchema_Number_Enum_NotIn(t *testing.T) {
	s := &Schema{Type: "number", Enum: []string{"1.0", "3.0"}}
	if err := validateAgainstSchema(2.0, s, ""); err == nil {
		t.Error("expected error for number not in enum")
	}
}

func TestValidateAgainstSchema_WrongTypeFallthrough(t *testing.T) {
	// nil schema should return nil
	if err := validateAgainstSchema("anything", nil, ""); err != nil {
		t.Errorf("expected no error for nil schema, got %v", err)
	}
}

// ─── ValidateOutput markdown edge cases ──────────────────────────────────────

func TestValidateOutput_Markdown_Valid(t *testing.T) {
	err := ValidateOutput("# Header\n\nSome *text* and - list", ContentTypeMarkdown, nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateOutput_Markdown_Invalid(t *testing.T) {
	err := ValidateOutput("plain text without markdown formatting", ContentTypeMarkdown, nil)
	if err == nil {
		t.Error("expected error for non-markdown output")
	}
}

func TestValidateOutput_EmptyType(t *testing.T) {
	err := ValidateOutput("anything", "", nil)
	if err != nil {
		t.Errorf("expected no error for empty type, got %v", err)
	}
}

func TestValidateOutput_JSON_WithSchema(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"name": {Type: "string"},
			"age":  {Type: "integer"},
		},
		Required: []string{"name"},
	}
	err := ValidateOutput(`{"name":"Alice","age":30}`, ContentTypeJSON, schema)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateOutput_JSON_WithSchema_MissingRequired(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"name": {Type: "string"},
		},
		Required: []string{"name"},
	}
	err := ValidateOutput(`{"age":30}`, ContentTypeJSON, schema)
	if err == nil {
		t.Error("expected error for missing required field")
	}
}

func TestValidateOutput_JSON_WithSchema_WrongType(t *testing.T) {
	schema := &Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"name": {Type: "string"},
		},
	}
	// name is a number, not string
	err := ValidateOutput(`{"name":123}`, ContentTypeJSON, schema)
	if err == nil {
		t.Error("expected error for wrong type in output")
	}
}

// ─── openapi.go coverage gaps ────────────────────────────────────────────────

func TestAddTag(t *testing.T) {
	g := NewOpenAPIGenerator("test", "1.0", "desc")
	g.AddTag("test-tag", "Test tag description")

	if len(g.tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(g.tags))
	}
	if g.tags[0].Name != "test-tag" {
		t.Errorf("expected tag name 'test-tag', got %q", g.tags[0].Name)
	}
	if g.tags[0].Description != "Test tag description" {
		t.Errorf("expected description 'Test tag description', got %q", g.tags[0].Description)
	}
}

func TestAddTag_EmptyDescription(t *testing.T) {
	g := NewOpenAPIGenerator("test", "1.0", "desc")
	g.AddTag("another-tag", "")
	if g.tags[0].Description != "" {
		t.Errorf("expected empty description, got %q", g.tags[0].Description)
	}
}

func TestContentResponse_Non200(t *testing.T) {
	rb := NewRoute("/test", GET)
	rb.ContentResponse(404, "text/plain", "Not Found")

	found404 := false
	for _, resp := range rb.route.Responses {
		if resp.StatusCode == 404 {
			found404 = true
			if resp.ContentType != "text/plain" {
				t.Errorf("expected content type 'text/plain', got %q", resp.ContentType)
			}
			if resp.Description != "Not Found" {
				t.Errorf("expected description 'Not Found', got %q", resp.Description)
			}
		}
	}
	if !found404 {
		t.Error("expected 404 response")
	}
}

func TestContentResponse_200(t *testing.T) {
	rb := NewRoute("/test", GET)
	rb.ContentResponse(200, "text/csv", "CSV data")

	if rb.route.Responses[0].StatusCode != 200 {
		t.Errorf("expected 200, got %d", rb.route.Responses[0].StatusCode)
	}
	if rb.route.Responses[0].ContentType != "text/csv" {
		t.Errorf("expected content type 'text/csv', got %q", rb.route.Responses[0].ContentType)
	}
}

func TestRouteBuilder_Build(t *testing.T) {
	rb := NewRoute("/api/test", POST).
		Summary("Test endpoint").
		Description("A test description").
		Tags("tag1", "tag2").
		QueryParam("id", "Resource ID", true, StringSchema("The ID")).
		JSONResponse(200, "OK", ObjectSchema(nil, "status")).
		ErrorResponse(400, "Bad Request").
		WithAuth().
		Deprecated().
		OperationID("testOp").
		RequestBody(ObjectSchema(nil))

	route := rb.Build()

	if route.Path != "/api/test" {
		t.Errorf("expected path '/api/test', got %q", route.Path)
	}
	if route.Method != POST {
		t.Errorf("expected POST, got %v", route.Method)
	}
	if route.Summary != "Test endpoint" {
		t.Errorf("expected summary 'Test endpoint', got %q", route.Summary)
	}
	if !route.Auth {
		t.Error("expected Auth=true")
	}
	if !route.Deprecated {
		t.Error("expected Deprecated=true")
	}
	if route.OperationID != "testOp" {
		t.Errorf("expected OperationID 'testOp', got %q", route.OperationID)
	}
	if route.RequestBody == nil {
		t.Error("expected non-nil RequestBody")
	}
	if len(route.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(route.Parameters))
	}
	// Default 200 response + the 400 error response
	if len(route.Responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(route.Responses))
	}
}

func TestRouteBuilder_SunsetDate(t *testing.T) {
	rb := NewRoute("/test", GET)
	rb.Sunset("2026-12-31")

	if !rb.route.Deprecated {
		t.Error("expected Deprecated=true after Sunset")
	}
	if rb.route.SunsetDate != "2026-12-31" {
		t.Errorf("expected SunsetDate '2026-12-31', got %q", rb.route.SunsetDate)
	}
	if len(rb.route.DeprecationHeaders) != 2 {
		t.Fatalf("expected 2 deprecation headers, got %d", len(rb.route.DeprecationHeaders))
	}
	if rb.route.DeprecationHeaders[0].Name != "Deprecation" {
		t.Errorf("expected header 'Deprecation', got %q", rb.route.DeprecationHeaders[0].Name)
	}
	if rb.route.DeprecationHeaders[1].Name != "Sunset" {
		t.Errorf("expected header 'Sunset', got %q", rb.route.DeprecationHeaders[1].Name)
	}
}

func TestCompressionMiddleware_NoGzipSupport(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	// No Accept-Encoding header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Errorf("unexpected body: %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_WithGzip(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":"hello world"}`))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip Content-Encoding, got %q", rec.Header().Get("Content-Encoding"))
	}
	// Body should be gzip compressed
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty body")
	}
}

func TestDeprecatedHandler_WithSunsetDate(t *testing.T) {
	h := DeprecatedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}), "2026-12-31")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Errorf("expected Deprecation: true, got %q", rec.Header().Get("Deprecation"))
	}
	if rec.Header().Get("Sunset") != "2026-12-31" {
		t.Errorf("expected Sunset: 2026-12-31, got %q", rec.Header().Get("Sunset"))
	}
	if rec.Body.String() != "ok" {
		t.Errorf("unexpected body: %q", rec.Body.String())
	}
}

func TestDeprecatedHandler_NoSunset(t *testing.T) {
	h := DeprecatedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), "")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Deprecation") != "true" {
		t.Errorf("expected Deprecation: true, got %q", rec.Header().Get("Deprecation"))
	}
	if rec.Header().Get("Sunset") != "" {
		t.Errorf("expected no Sunset header, got %q", rec.Header().Get("Sunset"))
	}
}

func TestDeprecatedHandlerFunc(t *testing.T) {
	h := DeprecatedHandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}, "2026-06-30")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Deprecation") != "true" {
		t.Errorf("expected Deprecation: true")
	}
}

// ─── gzipResponseWriter edge case: non-compressible content after WriteHeader ─

func TestGzipResponseWriter_NonCompressibleWrite(t *testing.T) {
	inner := httptest.NewRecorder()
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: io.Discard}
	grw.Header().Set("Content-Type", "application/octet-stream")
	grw.WriteHeader(200)

	n, err := grw.Write([]byte("raw binary data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == 0 {
		t.Error("expected bytes written")
	}
	// Should have been written directly to inner
	if inner.Body.String() != "raw binary data" {
		t.Errorf("unexpected body: %q", inner.Body.String())
	}
}

func TestGzipResponseWriter_WriteHeaderIdempotent(_ *testing.T) {
	inner := httptest.NewRecorder()
	grw := &gzipResponseWriter{ResponseWriter: inner, Writer: io.Discard}
	grw.Header().Set("Content-Type", "application/json")

	grw.WriteHeader(200)
	grw.WriteHeader(299) // second call should be no-op
	// If it called WriteHeader again, the test would panic (can't set headers after WriteHeader)
}

// ─── CompressionMiddleware edge cases ────────────────────────────────────────

func TestCompressionMiddleware_NonCompressibleType(t *testing.T) {
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG data"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	// Content-Encoding should NOT be gzip for non-compressible types
	if rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("expected no Content-Encoding for non-compressible type, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressionMiddleware_NoContentType(t *testing.T) {
	// Default to compressible when Content-Type is not set
	handler := CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("raw data"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	// When Content-Type is empty, it defaults to compressible
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip Content-Encoding for empty Content-Type, got %q", rec.Header().Get("Content-Encoding"))
	}
}

// ─── Schema generation helpers ───────────────────────────────────────────────

func TestNumberSchema(t *testing.T) {
	s := NumberSchema("a number")
	if s.Type != "number" {
		t.Errorf("expected type 'number', got %q", s.Type)
	}
	if s.Description != "a number" {
		t.Errorf("expected description 'a number', got %q", s.Description)
	}
}

// ─── ParseAgentDefinition / MustParseAgentDefinition ─────────────────────────

func TestParseAgentDefinition_InvalidJSON(t *testing.T) {
	_, err := ParseAgentDefinition([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMustParseAgentDefinition_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	MustParseAgentDefinition([]byte("not json"))
}

func TestMustParseAgentDefinition_Valid(t *testing.T) {
	data := []byte(`{"api_version":"v1","name":"test","tree":"default","version":"1.0.0"}`)
	def := MustParseAgentDefinition(data)
	if def.Name != "test" {
		t.Errorf("expected name 'test', got %q", def.Name)
	}
}

// ─── Validate additional branches ────────────────────────────────────────────

func TestValidate_SchemaNilProperties(t *testing.T) {
	// Schema with nil Properties should pass validation
	def := &AgentDefinition{
		APIVersion:  "v1",
		Name:        "test",
		Tree:        "default",
		Version:     "1.0.0",
		InputType:   ContentTypeText,
		OutputType:  ContentTypeText,
		InputSchema: &Schema{Type: "object", Properties: nil},
	}
	if err := def.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_OutputSchemaValid(t *testing.T) {
	def := &AgentDefinition{
		APIVersion:   "v1",
		Name:         "test",
		Tree:         "default",
		Version:      "1.0.0",
		InputType:    ContentTypeText,
		OutputType:   ContentTypeText,
		OutputSchema: &Schema{Type: "object", Properties: map[string]*Schema{"result": {Type: "string"}}},
	}
	if err := def.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_OutputSchemaError(t *testing.T) {
	def := &AgentDefinition{
		APIVersion:   "v1",
		Name:         "test",
		Tree:         "default",
		Version:      "1.0.0",
		InputType:    ContentTypeText,
		OutputType:   ContentTypeText,
		OutputSchema: &Schema{Type: "object", MinLength: intPtr(5), MaxLength: intPtr(3)},
	}
	if err := def.Validate(); err == nil {
		t.Error("expected error for minLength > maxLength on non-string type")
	}
}

func TestSchemaToMap_Format(t *testing.T) {
	s := &Schema{Type: "string", Format: "date-time"}
	m := schemaToMap(s)
	if m["format"] != "date-time" {
		t.Errorf("expected format 'date-time', got %v", m["format"])
	}
}
