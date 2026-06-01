// Package api provides response validation middleware that detects
// drift between documented OpenAPI schemas and actual API responses.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// RouteIndex provides fast lookup of Route definitions by path and HTTP method.
// Used by the response validation middleware to find the documented schema
// for an API response.
type RouteIndex struct {
	routes []Route
	// Index: "METHOD /path" → Route
	byMethodPath map[string]*Route
}

// NewRouteIndex builds a RouteIndex from a slice of Route definitions.
// If multiple routes share the same method+path, the last one wins.
func NewRouteIndex(routes []Route) *RouteIndex {
	ri := &RouteIndex{
		routes:       routes,
		byMethodPath: make(map[string]*Route, len(routes)),
	}
	for i := range routes {
		key := routeKey(routes[i].Method, routes[i].Path)
		ri.byMethodPath[key] = &routes[i]
	}
	return ri
}

// Lookup returns the Route for the given method and path, or nil if not found.
func (ri *RouteIndex) Lookup(method, path string) *Route {
	if ri == nil {
		return nil
	}
	return ri.byMethodPath[routeKey(HTTPMethod(strings.ToLower(method)), path)]
}

// Len returns the number of indexed routes.
func (ri *RouteIndex) Len() int {
	if ri == nil {
		return 0
	}
	return len(ri.byMethodPath)
}

// routeKey builds the index key from method and path.
func routeKey(method HTTPMethod, path string) string {
	return string(method) + " " + path
}

// ─── Schema Validation ─────────────────────────────────────────────────────

// SchemaViolation describes a single mismatch between an actual API response
// and its documented schema.
type SchemaViolation struct {
	Field   string `json:"field"`   // JSON path to the violation (e.g., "alerts[0].name")
	Message string `json:"message"` // Human-readable description of the mismatch
}

// ValidateResponse validates an HTTP response body against the documented schema
// for a given route and status code. Returns nil if the response matches the schema
// or if no schema is documented for the response (schema-less endpoints are not validated).
//
// Non-JSON responses (ContentType set to non-empty, non-JSON) are skipped.
func ValidateResponse(route *Route, statusCode int, body []byte) []SchemaViolation {
	if route == nil || len(body) == 0 {
		return nil
	}

	// Find the response definition for this status code
	response := findResponse(route, statusCode)
	if response == nil {
		return nil // no documented schema for this status code
	}

	// Skip non-JSON responses (text/yaml, text/html, etc.)
	if response.ContentType != "" && response.ContentType != "application/json" {
		return nil
	}

	// Skip responses without a schema
	if response.Schema == nil {
		return nil
	}

	// Parse the body as JSON
	var v interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return []SchemaViolation{{
			Field:   "(body)",
			Message: fmt.Sprintf("response is not valid JSON: %v", err),
		}}
	}

	// Validate against the schema
	var violations []SchemaViolation
	collectViolations(v, response.Schema, "", &violations)
	return violations
}

// findResponse finds the RouteResponse for the given status code.
// Falls back to the 200 response if the exact status code is not found.
func findResponse(route *Route, statusCode int) *RouteResponse {
	for i := range route.Responses {
		if route.Responses[i].StatusCode == statusCode {
			return &route.Responses[i]
		}
	}
	// Fallback: return the first response (typically 200)
	if len(route.Responses) > 0 {
		return &route.Responses[0]
	}
	return nil
}

// collectViolations builds a list of SchemaViolation by comparing a JSON value
// against a Schema. This is a non-strict validator — it reports warnings but
// does not fail on missing required fields (only reports them as drift).
func collectViolations(v interface{}, s *Schema, path string, violations *[]SchemaViolation) {
	if s == nil {
		return
	}

	switch s.Type {
	case "object":
		m, ok := v.(map[string]interface{})
		if !ok {
			*violations = append(*violations, SchemaViolation{
				Field:   pathOrRoot(path),
				Message: fmt.Sprintf("expected object, got %T", v),
			})
			return
		}

		// Check required fields
		for _, req := range s.Required {
			if _, exists := m[req]; !exists {
				*violations = append(*violations, SchemaViolation{
					Field:   fieldPath(path, req),
					Message: fmt.Sprintf("missing required field %q", req),
				})
			}
		}

		// Validate known properties
		for key, propSchema := range s.Properties {
			if val, exists := m[key]; exists && propSchema != nil {
				collectViolations(val, propSchema, fieldPath(path, key), violations)
			}
		}

	case "array":
		arr, ok := v.([]interface{})
		if !ok {
			*violations = append(*violations, SchemaViolation{
				Field:   pathOrRoot(path),
				Message: fmt.Sprintf("expected array, got %T", v),
			})
			return
		}
		if s.Items != nil {
			for i, item := range arr {
				collectViolations(item, s.Items, fmt.Sprintf("%s[%d]", path, i), violations)
			}
		}

	case "string":
		if _, ok := v.(string); !ok {
			*violations = append(*violations, SchemaViolation{
				Field:   pathOrRoot(path),
				Message: fmt.Sprintf("expected string, got %T", v),
			})
		}

	case "number", "integer":
		switch v.(type) {
		case float64, json.Number:
			// ok
		default:
			*violations = append(*violations, SchemaViolation{
				Field:   pathOrRoot(path),
				Message: fmt.Sprintf("expected number, got %T", v),
			})
		}

	case "boolean":
		if _, ok := v.(bool); !ok {
			*violations = append(*violations, SchemaViolation{
				Field:   pathOrRoot(path),
				Message: fmt.Sprintf("expected boolean, got %T", v),
			})
		}
	}
}

func pathOrRoot(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func fieldPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

// ─── Response Capture Writer ────────────────────────────────────────────────

// responseCapture wraps http.ResponseWriter to capture the response body
// and status code for post-processing.
type responseCapture struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
	wroteHeader bool
}

// WriteHeader captures the status code and passes it through.
func (rc *responseCapture) WriteHeader(code int) {
	if !rc.wroteHeader {
		rc.statusCode = code
		rc.wroteHeader = true
	}
	rc.ResponseWriter.WriteHeader(code)
}

// Write captures the body and passes it through.
func (rc *responseCapture) Write(b []byte) (int, error) {
	if !rc.wroteHeader {
		rc.statusCode = http.StatusOK
		rc.wroteHeader = true
	}
	// Only capture if it's likely JSON (starts with { or [ or ")
	if rc.body.Len() == 0 && len(b) > 0 {
		first := b[0]
		if first == '{' || first == '[' {
			rc.body.Write(b)
		}
	}
	return rc.ResponseWriter.Write(b)
}

// Status returns the captured status code (defaults to 200 if never set).
func (rc *responseCapture) Status() int {
	if rc.statusCode == 0 {
		return http.StatusOK
	}
	return rc.statusCode
}

// ─── Response Validation Middleware ─────────────────────────────────────────

// ResponseValidatorConfig configures the response validation middleware.
type ResponseValidatorConfig struct {
	// Logger receives schema violation warnings. If nil, slog.Default() is used.
	Logger *slog.Logger

	// SkipPaths is a set of paths to skip validation for.
	SkipPaths map[string]bool
}

// ResponseValidator creates HTTP middleware that validates API responses
// against documented OpenAPI schemas.
//
// The middleware wraps the http.ResponseWriter to capture response body and
// status code. After the handler returns, it looks up the route definition
// and validates the response JSON against the documented schema for that
// status code.
//
// Validation failures are logged as WARN-level messages via the configured
// logger. The middleware never blocks or alters the response — it is purely
// an advisory drift detector.
//
// Usage:
//
//	validator := api.ResponseValidator(api.DashboardRoutes())
//	mux := http.NewServeMux()
//	mux.Handle("/api/health", validator(healthHandler))
func ResponseValidator(routes []Route, config *ResponseValidatorConfig) func(http.Handler) http.Handler {
	index := NewRouteIndex(routes)
	logger := slog.Default()
	if config != nil && config.Logger != nil {
		logger = config.Logger
	}
	skipPaths := map[string]bool{}
	if config != nil {
		skipPaths = config.SkipPaths
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if path is excluded
			if skipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Only validate /api/* paths
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			// Look up the route definition
			route := index.Lookup(r.Method, r.URL.Path)
			if route == nil {
				// Unknown route — pass through (could be a dynamic route not in the index)
				// TODO: support path parameter matching (e.g., /api/tasks/approve?id=...)
				next.ServeHTTP(w, r)
				return
			}

			// Capture the response
			rc := &responseCapture{ResponseWriter: w}
			next.ServeHTTP(rc, r)

			// Validate the captured body
			if rc.body.Len() > 0 {
				violations := ValidateResponse(route, rc.Status(), rc.body.Bytes())
				if len(violations) > 0 {
					for _, v := range violations {
						logger.Warn("API response schema drift detected",
							"path", r.URL.Path,
							"method", r.Method,
							"status", rc.Status(),
							"field", v.Field,
							"message", v.Message,
						)
					}
				}
			}
		})
	}
}
