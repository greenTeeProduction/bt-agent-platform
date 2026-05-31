// Package api provides OpenAPI 3.0 specification generation for the BT Platform dashboard API.
package api

import (
	"encoding/json"
	"sort"
)

// ─── OpenAPI Route Definition ──────────────────────────────────────────────

// HTTPMethod represents an HTTP method for OpenAPI route definitions.
type HTTPMethod string

// Valid HTTP methods for API documentation.
const (
	GET    HTTPMethod = "get"
	POST   HTTPMethod = "post"
	PUT    HTTPMethod = "put"
	DELETE HTTPMethod = "delete"
)

// ParameterLocation specifies where a parameter is found in an HTTP request.
type ParameterLocation string

const (
	ParamQuery  ParameterLocation = "query"
	ParamHeader ParameterLocation = "header"
	ParamPath   ParameterLocation = "path"
)

// RouteParam describes a single parameter for an API route.
type RouteParam struct {
	Name        string            `json:"name"`
	In          ParameterLocation `json:"in"`
	Required    bool              `json:"required"`
	Description string            `json:"description,omitempty"`
	Schema      *Schema           `json:"schema"`
}

// RouteResponse describes a single response for an API route.
type RouteResponse struct {
	StatusCode  int     `json:"status_code"`
	Description string  `json:"description"`
	Schema      *Schema `json:"schema,omitempty"`
	ContentType string  `json:"content_type,omitempty"`
}

// Route describes a single API endpoint for OpenAPI generation.
type Route struct {
	Path        string        `json:"path"`
	Method      HTTPMethod    `json:"method"`
	Summary     string        `json:"summary"`
	Description string        `json:"description,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Parameters  []RouteParam  `json:"parameters,omitempty"`
	RequestBody *Schema       `json:"request_body,omitempty"`
	Responses   []RouteResponse `json:"responses"`
	Deprecated  bool          `json:"deprecated,omitempty"`
	Auth        bool          `json:"auth"` // requires API key
}

// ─── OpenAPI Spec Generator ─────────────────────────────────────────────────

// OpenAPISpec represents the top-level OpenAPI 3.0 document.
type OpenAPISpec struct {
	OpenAPI    string                `json:"openapi"`
	Info       OpenAPIInfo           `json:"info"`
	Servers    []OpenAPIServer       `json:"servers,omitempty"`
	Paths      map[string]map[string]interface{} `json:"paths"`
	Components OpenAPIComponents     `json:"components,omitempty"`
	Tags       []OpenAPITag          `json:"tags,omitempty"`
}

// OpenAPIInfo contains API metadata.
type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// OpenAPIServer describes a server URL.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// OpenAPITag describes a tag group for organizing endpoints.
type OpenAPITag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// OpenAPIComponents holds reusable schemas and security definitions.
type OpenAPIComponents struct {
	Schemas         map[string]interface{} `json:"schemas,omitempty"`
	SecuritySchemes map[string]interface{} `json:"securitySchemes,omitempty"`
}

// OpenAPIGenerator builds OpenAPI 3.0 specifications from route definitions.
type OpenAPIGenerator struct {
	title       string
	version     string
	description string
	servers     []OpenAPIServer
	tags        []OpenAPITag
	routes      []Route
	schemas     map[string]interface{}
}

// NewOpenAPIGenerator creates a new generator with API metadata.
func NewOpenAPIGenerator(title, version, description string) *OpenAPIGenerator {
	return &OpenAPIGenerator{
		title:       title,
		version:     version,
		description: description,
		schemas:     make(map[string]interface{}),
	}
}

// AddServer adds a server URL to the spec.
func (g *OpenAPIGenerator) AddServer(url, description string) {
	g.servers = append(g.servers, OpenAPIServer{URL: url, Description: description})
}

// AddTag adds a tag for organizing endpoints.
func (g *OpenAPIGenerator) AddTag(name, description string) {
	g.tags = append(g.tags, OpenAPITag{Name: name, Description: description})
}

// AddRoute registers a route for inclusion in the spec.
func (g *OpenAPIGenerator) AddRoute(r Route) {
	g.routes = append(g.routes, r)
}

// AddSchema registers a reusable component schema.
func (g *OpenAPIGenerator) AddSchema(name string, schema interface{}) {
	g.schemas[name] = schema
}

// Generate builds the complete OpenAPI 3.0 specification as a JSON-marshalable struct.
func (g *OpenAPIGenerator) Generate() OpenAPISpec {
	paths := make(map[string]map[string]interface{})

	for _, route := range g.routes {
		if _, exists := paths[route.Path]; !exists {
			paths[route.Path] = make(map[string]interface{})
		}

		methodObj := make(map[string]interface{})
		methodObj["summary"] = route.Summary

		if route.Description != "" {
			methodObj["description"] = route.Description
		}
		if len(route.Tags) > 0 {
			methodObj["tags"] = route.Tags
		}
		if route.Deprecated {
			methodObj["deprecated"] = true
		}

		// Parameters
		if len(route.Parameters) > 0 {
			params := make([]map[string]interface{}, 0, len(route.Parameters))
			for _, p := range route.Parameters {
				paramObj := map[string]interface{}{
					"name":     p.Name,
					"in":       string(p.In),
					"required": p.Required,
					"schema":   schemaToMap(p.Schema),
				}
				if p.Description != "" {
					paramObj["description"] = p.Description
				}
				params = append(params, paramObj)
			}
			methodObj["parameters"] = params
		}

		// Request body
		if route.RequestBody != nil {
			methodObj["requestBody"] = map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": schemaToMap(route.RequestBody),
					},
				},
			}
		}

		// Responses
		responses := make(map[string]interface{})
		for _, resp := range route.Responses {
			statusKey := httpStatusText(resp.StatusCode)
			respObj := map[string]interface{}{
				"description": resp.Description,
			}
			if resp.Schema != nil {
				respObj["content"] = map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": schemaToMap(resp.Schema),
					},
				}
			}
			responses[statusKey] = respObj
		}
		methodObj["responses"] = responses

		// Security
		if route.Auth {
			methodObj["security"] = []map[string][]string{
				{"ApiKeyAuth": {}},
			}
		}

		paths[route.Path][string(route.Method)] = methodObj
	}

	// Sort paths for deterministic output
	sortedPaths := make(map[string]map[string]interface{})
	pathKeys := make([]string, 0, len(paths))
	for k := range paths {
		pathKeys = append(pathKeys, k)
	}
	sort.Strings(pathKeys)
	for _, k := range pathKeys {
		sortedPaths[k] = paths[k]
	}

	components := OpenAPIComponents{
		Schemas: g.schemas,
		SecuritySchemes: map[string]interface{}{
			"ApiKeyAuth": map[string]interface{}{
				"type":        "apiKey",
				"in":          "header",
				"name":        "X-API-Key",
				"description": "Dashboard API key. Required for all /api/* endpoints when BT_API_KEY is configured.",
			},
		},
	}

	return OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       g.title,
			Version:     g.version,
			Description: g.description,
		},
		Servers:    g.servers,
		Paths:      sortedPaths,
		Components: components,
		Tags:       g.tags,
	}
}

// GenerateJSON returns the OpenAPI 3.0 spec as formatted JSON bytes.
func (g *OpenAPIGenerator) GenerateJSON() ([]byte, error) {
	return json.MarshalIndent(g.Generate(), "", "  ")
}

// GenerateJSONCompact returns the OpenAPI 3.0 spec as compact JSON bytes.
func (g *OpenAPIGenerator) GenerateJSONCompact() ([]byte, error) {
	return json.Marshal(g.Generate())
}

// ─── Schema Helpers ─────────────────────────────────────────────────────────

// schemaToMap converts an internal Schema to a map representation for OpenAPI.
func schemaToMap(s *Schema) map[string]interface{} {
	if s == nil {
		return map[string]interface{}{"type": "object"}
	}

	m := map[string]interface{}{
		"type": s.Type,
	}

	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if s.Pattern != "" {
		m["pattern"] = s.Pattern
	}

	if len(s.Required) > 0 {
		m["required"] = toAnySlice(s.Required)
	}
	if len(s.Enum) > 0 {
		m["enum"] = toAnySlice(s.Enum)
	}

	if s.MinLength != nil {
		m["minLength"] = *s.MinLength
	}
	if s.MaxLength != nil {
		m["maxLength"] = *s.MaxLength
	}
	if s.Minimum != nil {
		m["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		m["maximum"] = *s.Maximum
	}

	if s.Type == "object" && len(s.Properties) > 0 {
		props := make(map[string]interface{})
		for k, v := range s.Properties {
			props[k] = schemaToMap(v)
		}
		m["properties"] = props
	}

	if s.Type == "array" && s.Items != nil {
		m["items"] = schemaToMap(s.Items)
	}

	return m
}

// toAnySlice converts a []string to []interface{} for JSON encoding.
func toAnySlice(ss []string) []interface{} {
	out := make([]interface{}, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// httpStatusText converts an HTTP status code to its string representation
// as used in OpenAPI response keys (e.g., "200", "404", "default").
func httpStatusText(code int) string {
	switch {
	case code >= 100 && code < 600:
		return string([]byte{
			byte('0' + code/100),
			byte('0' + (code/10)%10),
			byte('0' + code%10),
		})
	default:
		return "default"
	}
}

// ─── Convenience Schema Constructors ─────────────────────────────────────────

// ObjectSchema creates an object schema with the given properties.
func ObjectSchema(props map[string]*Schema, required ...string) *Schema {
	return &Schema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

// StringSchema creates a string schema with optional description.
func StringSchema(desc string) *Schema {
	return &Schema{Type: "string", Description: desc}
}

// IntSchema creates an integer schema with optional description.
func IntSchema(desc string) *Schema {
	return &Schema{Type: "integer", Description: desc}
}

// NumberSchema creates a number schema with optional description.
func NumberSchema(desc string) *Schema {
	return &Schema{Type: "number", Description: desc}
}

// BoolSchema creates a boolean schema with optional description.
func BoolSchema(desc string) *Schema {
	return &Schema{Type: "boolean", Description: desc}
}

// ArraySchema creates an array schema with the given item schema.
func ArraySchema(items *Schema, desc string) *Schema {
	return &Schema{Type: "array", Items: items, Description: desc}
}

// ─── Standard Dashboard API Routes ───────────────────────────────────────────

// RouteBuilder helps construct OpenAPI routes fluently.
type RouteBuilder struct {
	route Route
}

// NewRoute creates a RouteBuilder for a given path and method.
func NewRoute(path string, method HTTPMethod) *RouteBuilder {
	return &RouteBuilder{
		route: Route{
			Path:   path,
			Method: method,
			Responses: []RouteResponse{
				{StatusCode: 200, Description: "Successful response"},
			},
		},
	}
}

// Summary sets the route summary.
func (rb *RouteBuilder) Summary(s string) *RouteBuilder {
	rb.route.Summary = s
	return rb
}

// Description sets the route description.
func (rb *RouteBuilder) Description(s string) *RouteBuilder {
	rb.route.Description = s
	return rb
}

// Tags sets the route tags.
func (rb *RouteBuilder) Tags(tags ...string) *RouteBuilder {
	rb.route.Tags = tags
	return rb
}

// QueryParam adds a query parameter.
func (rb *RouteBuilder) QueryParam(name, desc string, required bool, schema *Schema) *RouteBuilder {
	rb.route.Parameters = append(rb.route.Parameters, RouteParam{
		Name:        name,
		In:          ParamQuery,
		Required:    required,
		Description: desc,
		Schema:      schema,
	})
	return rb
}

// JSONResponse overrides the default 200 response with a described schema.
func (rb *RouteBuilder) JSONResponse(code int, desc string, schema *Schema) *RouteBuilder {
	if code == 200 {
		// Replace default
		rb.route.Responses[0] = RouteResponse{StatusCode: code, Description: desc, Schema: schema}
	} else {
		rb.route.Responses = append(rb.route.Responses, RouteResponse{StatusCode: code, Description: desc, Schema: schema})
	}
	return rb
}

// ErrorResponse adds an error response definition.
func (rb *RouteBuilder) ErrorResponse(code int, desc string) *RouteBuilder {
	errSchema := ObjectSchema(map[string]*Schema{
		"error": StringSchema("Error message"),
	}, "error")
	rb.route.Responses = append(rb.route.Responses, RouteResponse{StatusCode: code, Description: desc, Schema: errSchema})
	return rb
}

// ContentResponse adds a response with a specific content type (non-JSON).
func (rb *RouteBuilder) ContentResponse(code int, contentType, desc string) *RouteBuilder {
	resp := RouteResponse{
		StatusCode:  code,
		Description: desc,
		ContentType: contentType,
		Schema:      StringSchema(desc),
	}
	if code == 200 {
		rb.route.Responses[0] = resp
	} else {
		rb.route.Responses = append(rb.route.Responses, resp)
	}
	return rb
}

// WithAuth marks the route as requiring API key authentication.
func (rb *RouteBuilder) WithAuth() *RouteBuilder {
	rb.route.Auth = true
	return rb
}

// Deprecated marks the route as deprecated.
func (rb *RouteBuilder) Deprecated() *RouteBuilder {
	rb.route.Deprecated = true
	return rb
}

// Build returns the compiled Route.
func (rb *RouteBuilder) Build() Route {
	return rb.route
}

// DashboardRoutes returns the standard set of dashboard API routes.
func DashboardRoutes() []Route {
	return []Route{
		// Public endpoints
		NewRoute("/api/health", GET).
			Summary("Health check").
			Tags("System").
			JSONResponse(200, "Dashboard health status", ObjectSchema(map[string]*Schema{
				"status":  StringSchema("Health status (ok/degraded)"),
				"version": StringSchema("Dashboard version"),
			})).Build(),

		NewRoute("/api/metrics", GET).
			Summary("Prometheus metrics").
			Description("Returns platform metrics in Prometheus text format.").
			Tags("System").
			JSONResponse(200, "Prometheus metrics text", StringSchema("Prometheus-formatted metrics")).Build(),

		NewRoute("/api/alerts", GET).
			Summary("Active alerts").
			Description("Returns evaluated alert rules from platform metrics.").
			Tags("System").
			JSONResponse(200, "Alert report", ObjectSchema(map[string]*Schema{
				"evaluated_at": StringSchema("ISO 8601 timestamp of evaluation"),
				"total_rules":  IntSchema("Total number of alert rules"),
				"firing_count": IntSchema("Number of currently firing alerts"),
				"alerts": ArraySchema(ObjectSchema(map[string]*Schema{
					"name":        StringSchema("Alert rule name"),
					"severity":    StringSchema("Severity: critical/warning/info"),
					"component":   StringSchema("System component"),
					"summary":     StringSchema("Alert summary"),
					"description": StringSchema("Detailed alert description"),
					"firing":      BoolSchema("Whether alert is currently firing"),
					"value":       StringSchema("Current metric value"),
				}), "List of alert evaluations"),
				"all_clear": BoolSchema("True when no alerts are firing"),
			}, "evaluated_at", "total_rules", "firing_count", "all_clear")).Build(),

		NewRoute("/api/alerts/rules", GET).
			Summary("Alert rules file").
			Description("Returns the raw Prometheus alert rules YAML file for direct scraping by Prometheus or other monitoring tools.").
			Tags("System").
			ContentResponse(200, "text/yaml", "Prometheus alert rules YAML").Build(),

		NewRoute("/api/openapi.json", GET).
			Summary("OpenAPI specification").
			Description("Returns the OpenAPI 3.0 specification for all dashboard endpoints.").
			Tags("System").
			JSONResponse(200, "OpenAPI 3.0 JSON specification", StringSchema("OpenAPI JSON document")).Build(),

		NewRoute("/api/swagger", GET).
			Summary("Swagger UI").
			Description("Interactive API documentation using Swagger UI. Loads the OpenAPI spec from /api/openapi.json and renders it as a browsable API reference.").
			Tags("System").
			ContentResponse(200, "text/html", "Swagger UI HTML page").Build(),

		NewRoute("/api/scalability", GET).
			Summary("Scalability status").
			Description("Returns a snapshot of scalability components: worker pool utilization, concurrency limiter stats, queue depth, and agent router health. Public monitoring endpoint.").
			Tags("System").
			JSONResponse(200, "Scalability component snapshot", ObjectSchema(map[string]*Schema{
				"timestamp": StringSchema("ISO 8601 snapshot timestamp"),
				"worker_pool": ObjectSchema(map[string]*Schema{
					"workers":   IntSchema("Total workers"),
					"active":    IntSchema("Currently active workers"),
					"queued":    IntSchema("Queued tasks"),
					"total":     IntSchema("Total submitted tasks"),
					"completed": IntSchema("Total completed tasks"),
				}, "workers", "active", "queued", "total", "completed"),
				"concurrency_limiter": ObjectSchema(map[string]*Schema{
					"active":    IntSchema("Active slots"),
					"waiting":   IntSchema("Waiting goroutines"),
					"capacity":  IntSchema("Max concurrent slots"),
					"available": IntSchema("Free slots"),
					"total":     IntSchema("Total acquisitions"),
				}, "active", "waiting", "capacity", "available"),
				"queue": ObjectSchema(map[string]*Schema{
					"pending": IntSchema("Pending tasks in queue"),
					"max_len": IntSchema("Max queue length"),
				}, "pending"),
				"router": ObjectSchema(map[string]*Schema{
					"total":     IntSchema("Total executors"),
					"healthy":   IntSchema("Healthy executors"),
					"unhealthy": IntSchema("Unhealthy executors"),
				}, "total", "healthy", "unhealthy"),
			}, "timestamp")).Build(),

		NewRoute("/api/config", GET).
			Summary("Runtime configuration").
			Description("Returns the effective runtime configuration with secret fields (API keys, TLS paths) redacted. Public monitoring endpoint — no auth required.").
			Tags("System").
			JSONResponse(200, "Sanitized runtime configuration", ObjectSchema(map[string]*Schema{
				"dashboard_port":      IntSchema("Dashboard HTTP port"),
				"llm_provider":        StringSchema("LLM provider (ollama/deepseek)"),
				"ollama_host":         StringSchema("Ollama server URL"),
				"ollama_model":        StringSchema("Ollama model name"),
				"deepseek_host":       StringSchema("DeepSeek API URL"),
				"deepseek_model":      StringSchema("DeepSeek model name"),
				"llm_timeout":         IntSchema("LLM call timeout in seconds"),
				"rate_limit_rps":      NumberSchema("Rate limit requests per second"),
				"rate_limit_burst":    IntSchema("Rate limit burst capacity"),
				"gardener_enabled":    BoolSchema("Gardener evolution daemon enabled"),
				"scheduler_enabled":   BoolSchema("Agent scheduler enabled"),
				"auto_evolve_enabled":  BoolSchema("Auto-evolution enabled"),
				"kanban_enabled":      BoolSchema("Kanban board integration enabled"),
				"thinktank_enabled":   BoolSchema("Thinktank analysis enabled"),
				"startup_sim_enabled": BoolSchema("Startup simulation enabled"),
				"gardener_cycle_interval": IntSchema("Gardener cycle interval in seconds"),
				"gardener_mutations_per":  IntSchema("Mutations applied per cycle"),
				"max_body_size":       IntSchema("Max request body size in bytes"),
			}, "dashboard_port", "llm_provider", "ollama_model")).Build(),

		// Platform overview
		NewRoute("/api/summary", GET).
			Summary("Platform summary").
			Description("Returns an overview of the BT platform: tree counts, categories, MCP tools, and model name.").
			Tags("Platform").
			JSONResponse(200, "Platform summary", ObjectSchema(map[string]*Schema{
				"total_trees": IntSchema("Total number of behavior trees"),
				"categories": ObjectSchema(map[string]*Schema{
					"core":        IntSchema(""),
					"finance":     IntSchema(""),
					"research":    IntSchema(""),
					"domain":      IntSchema(""),
					"startup":     IntSchema(""),
					"thinktank":   IntSchema(""),
					"evolution":   IntSchema(""),
				}, "categories"),
				"mcp_tools": IntSchema("Total MCP tools"),
				"model":     StringSchema("LLM model name"),
			}, "total_trees", "categories", "mcp_tools", "model")).WithAuth().Build(),

		NewRoute("/api/trees", GET).
			Summary("List behavior trees").
			Description("Returns all registered behavior trees with their IDs, names, categories, and node counts.").
			Tags("Trees").
			JSONResponse(200, "Array of tree objects", ArraySchema(ObjectSchema(map[string]*Schema{
				"id":         StringSchema("Tree identifier"),
				"name":       StringSchema("Display name"),
				"category":   StringSchema("Category (core, finance, research, etc.)"),
				"node_count": IntSchema("Number of nodes in the tree"),
			}), "List of trees")).WithAuth().Build(),

		NewRoute("/api/tree/structure", GET).
			Summary("Get tree structure").
			Description("Returns the JSON structure of a specific behavior tree.").
			Tags("Trees").
			QueryParam("id", "Tree identifier (e.g., 'merged', 'godev')", true, StringSchema("Tree ID")).
			JSONResponse(200, "Tree structure JSON", ObjectSchema(map[string]*Schema{
				"id":         StringSchema("Tree identifier"),
				"name":       StringSchema("Display name"),
				"category":   StringSchema("Category"),
				"node_count": IntSchema("Number of nodes"),
				"structure":  ObjectSchema(nil, "structure"),
			}, "id", "name", "structure")).
			ErrorResponse(404, "Tree not found").WithAuth().Build(),

		// Thinktank
		NewRoute("/api/thinktank/fellows", GET).
			Summary("List thinktank fellows").
			Description("Returns the five analytical fellows with their roles and perspectives.").
			Tags("Thinktank").
			JSONResponse(200, "Array of fellow objects", ArraySchema(ObjectSchema(map[string]*Schema{
				"name":        StringSchema("Fellow name"),
				"role":        StringSchema("Role (bull/bear/technical/macro/contrarian)"),
				"perspective": StringSchema("Analytical perspective description"),
				"confidence":  NumberSchema("Confidence score 0.0-1.0"),
			}), "List of fellows")).WithAuth().Build(),

		NewRoute("/api/thinktank/analyze", POST).
			Summary("Run thinktank analysis").
			Description("Runs a think tank analysis on the given topic using the 5-fellow Hegelian dialectic pipeline.").
			Tags("Thinktank").
			QueryParam("topic", "Analysis topic (e.g., 'AI safety frameworks')", true, StringSchema("Topic")).
			JSONResponse(200, "Analysis results", ObjectSchema(map[string]*Schema{
				"topic":    StringSchema("Analysis topic"),
				"findings": ArraySchema(ObjectSchema(map[string]*Schema{
					"fellow":     StringSchema("Fellow name"),
					"role":       StringSchema("Analytical role"),
					"insights":   StringSchema("Key insights from this fellow"),
					"confidence": NumberSchema("Confidence score"),
				}), "Research findings from each fellow"),
			}, "topic", "findings")).
			ErrorResponse(503, "Ollama unavailable").WithAuth().Build(),

		// Company
		NewRoute("/api/company/default", GET).
			Summary("Get default company state").
			Description("Returns the default HermesAI startup company state with metrics like MRR, users, runway.").
			Tags("Company").
			JSONResponse(200, "Company state", ObjectSchema(map[string]*Schema{
				"name":     StringSchema("Company name"),
				"mrr":      NumberSchema("Monthly recurring revenue"),
				"arr":      NumberSchema("Annual recurring revenue"),
				"users":    IntSchema("Active users"),
				"team_size": IntSchema("Team size"),
				"runway":   IntSchema("Months of runway"),
				"cash":     NumberSchema("Cash on hand"),
			}, "name", "mrr", "users")).WithAuth().Build(),

		// Tasks
		NewRoute("/api/tasks", GET).
			Summary("List tasks").
			Description("Returns the full task pipeline with statuses and assignments.").
			Tags("Tasks").
			JSONResponse(200, "Array of task objects", ArraySchema(ObjectSchema(map[string]*Schema{
				"id":       StringSchema("Task identifier"),
				"title":    StringSchema("Task title"),
				"priority": StringSchema("Priority: critical/high/medium/low"),
				"role":     StringSchema("Assigned role: CEO/CTO/PM/Engineer/Marketing/Sales"),
				"sprint":   IntSchema("Sprint number"),
				"sp":       IntSchema("Story points"),
				"status":   StringSchema("Status: pending/approved/rejected/in_progress/completed"),
			}), "List of tasks")).WithAuth().Build(),

		NewRoute("/api/tasks/approve", POST).
			Summary("Approve a task").
			Description("Approves a task by ID, marking it ready for sprint execution.").
			Tags("Tasks").
			QueryParam("id", "Task identifier", true, StringSchema("Task ID")).
			JSONResponse(200, "Approval confirmation", ObjectSchema(map[string]*Schema{
				"status": StringSchema("'approved' on success"),
				"id":     StringSchema("Task identifier"),
			}, "status", "id")).
			ErrorResponse(404, "Task not found").WithAuth().Build(),

		NewRoute("/api/tasks/reject", POST).
			Summary("Reject a task").
			Description("Rejects a task by ID.").
			Tags("Tasks").
			QueryParam("id", "Task identifier", true, StringSchema("Task ID")).
			JSONResponse(200, "Rejection confirmation", ObjectSchema(map[string]*Schema{
				"status": StringSchema("'rejected' on success"),
				"id":     StringSchema("Task identifier"),
			}, "status", "id")).
			ErrorResponse(404, "Task not found").WithAuth().Build(),

		// Sprint
		NewRoute("/api/sprint/execute", POST).
			Summary("Execute sprint").
			Description("Executes all approved tasks in a sprint. Runs asynchronously with a job ID for status polling.").
			Tags("Sprint").
			QueryParam("fast", "Fast mode (skip Ollama calls)", false, BoolSchema("Fast mode flag")).
			JSONResponse(200, "Sprint started", ObjectSchema(map[string]*Schema{
				"status":  StringSchema("'sprint_started' or 'no_approved_tasks'"),
				"job_id":  StringSchema("Job ID for polling /api/sprint/status"),
				"message": StringSchema("Human-readable status message"),
			}, "status")).
			JSONResponse(400, "No approved tasks", ObjectSchema(map[string]*Schema{
				"status": StringSchema("'no_approved_tasks'"),
			})).WithAuth().Build(),

		NewRoute("/api/sprint/status", GET).
			Summary("Get sprint status").
			Description("Returns the current sprint execution status.").
			Tags("Sprint").
			JSONResponse(200, "Sprint status", ObjectSchema(map[string]*Schema{
				"running":    BoolSchema("Whether a sprint is currently executing"),
				"job_id":     StringSchema("Active sprint job ID"),
				"started_at": StringSchema("ISO 8601 start timestamp"),
				"progress":   StringSchema("Current progress: starting/running/running_tasks/completing/done"),
			}, "running", "progress")).WithAuth().Build(),

		// Chat
		NewRoute("/api/chat", POST).
			Summary("Dashboard chat").
			Description("Sends a message to a tab-specific AI agent and returns the reply.").
			Tags("Chat").
			QueryParam("msg", "User message", true, StringSchema("Message text")).
			QueryParam("tab", "Chat tab (overview/thinktank/company/tasks/trees/mindmap/evolution)", false, StringSchema("Tab identifier")).
			JSONResponse(200, "Chat response", ObjectSchema(map[string]*Schema{
				"reply": StringSchema("Agent response"),
				"tab":   StringSchema("Tab identifier echo"),
			}, "reply")).
			ErrorResponse(503, "Ollama unavailable").WithAuth().Build(),

		// Dead Letter Queue
		NewRoute("/api/dlq", GET).
			Summary("List dead letter queue entries").
			Description("Returns all failed tasks in the dead letter queue with their IDs, agents, errors, and timestamps.").
			Tags("Reliability").
			JSONResponse(200, "DLQ entries list", ObjectSchema(map[string]*Schema{
				"count": IntSchema("Number of dead letter entries"),
				"entries": ArraySchema(ObjectSchema(map[string]*Schema{
					"id":        StringSchema("Entry unique identifier"),
					"task":      StringSchema("Original task text"),
					"agent":     StringSchema("Agent name"),
					"error":     StringSchema("Failure error message"),
					"attempts":  IntSchema("Number of retry attempts made"),
					"failed_at": StringSchema("ISO 8601 failure timestamp"),
					"circuit":   StringSchema("Circuit breaker identifier (optional)"),
				}), "List of dead letter entries"),
			}, "count", "entries")).WithAuth().Build(),

		NewRoute("/api/dlq/replay", POST).
			Summary("Replay a dead letter entry").
			Description("Removes a specific entry from the DLQ and returns it for re-execution.").
			Tags("Reliability").
			QueryParam("id", "DLQ entry identifier", true, StringSchema("Entry UUID")).
			JSONResponse(200, "Replayed entry", ObjectSchema(map[string]*Schema{
				"status":  StringSchema("'replayed' on success"),
				"entry": ObjectSchema(map[string]*Schema{
					"id":        StringSchema("Entry identifier"),
					"task":      StringSchema("Original task text"),
					"agent":     StringSchema("Agent name"),
					"error":     StringSchema("Failure error message"),
					"attempts":  IntSchema("Number of attempts made"),
					"failed_at": StringSchema("Failure timestamp"),
				}, "id", "task"),
				"pending": IntSchema("Remaining entries in DLQ"),
			}, "status", "entry", "pending")).
			ErrorResponse(400, "Missing id parameter").
			ErrorResponse(404, "Entry not found").WithAuth().Build(),

		NewRoute("/api/dlq/purge", DELETE).
			Summary("Purge dead letter queue").
			Description("Removes all entries from the dead letter queue. IRREVERSIBLE.").
			Tags("Reliability").
			JSONResponse(200, "Purge confirmation", ObjectSchema(map[string]*Schema{
				"status":  StringSchema("'purged' on success"),
				"cleared": IntSchema("Number of entries cleared"),
			}, "status", "cleared")).WithAuth().Build(),
	}
}
