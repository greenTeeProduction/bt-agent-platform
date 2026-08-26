// Package engine implements ... (MCP server merged from internal/mcp)
//
// It provides tool registration, concurrent request handling (3-call semaphore
// with mutex-protected stdout), rate limiting, API key authentication, structured
// audit logging, and message size limits. The server is used by all three BT MCP
// binaries (bt-agent, bt-evaluator, bt-langagent).
//
// Key types:
//   - Server — the MCP server with RegisterTool, SetRateLimit, SetSecurity, SetAudit
//   - ToolResult — structured response with ContentItem array
//   - ContentItem — text/image/resource content for tool responses
//
// Concurrency model: tools/call requests are dispatched to goroutines (up to 3
// concurrent), while initialize, tools/list, and notifications run inline.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/security"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// Message is a JSON-RPC 2.0 message.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ToolDef is an MCP tool definition (tools/list response).
type ToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema is the JSON Schema for tool parameters.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

// Property is a single input schema property.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ToolResult is the content returned from a tool call.
type ToolResult struct {
	Content []ContentItem `json:"content"`
}

// ContentItem is a single content block in a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolHandler is a function that handles a tool call.
type ToolHandler func(args json.RawMessage) *ToolResult

// Server is a minimal MCP JSON-RPC 2.0 stdio server.
type Server struct {
	name           string
	tools          []ToolDef
	handler        map[string]ToolHandler
	in             io.Reader // stdin reader (os.Stdin by default, overridable for tests)
	out            io.Writer
	mu             sync.Mutex // protects out writes (concurrent handlers)
	bbMu           sync.Mutex // guards handlers registered via RegisterBlackboardTool
	sanitizeArgs   bool
	apiKey         string
	rateLimiter    *security.RateLimiter
	auditEnabled   bool
	maxMessageSize int // max bytes per JSON-RPC line (0 = default 1MB)
}

// NewServer creates a new MCP server.
func NewServer(name string) *Server {
	return &Server{
		name:    name,
		handler: make(map[string]ToolHandler),
		in:      os.Stdin,
		out:     os.Stdout,
	}
}

// RegisterTool adds a tool with its handler.
func (s *Server) RegisterTool(name, description string, props map[string]Property, required []string, handler ToolHandler) {
	s.tools = append(s.tools, ToolDef{
		Name:        name,
		Description: description,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	})
	s.handler[name] = handler
}

// RegisterBlackboardTool registers a tool like RegisterTool, but wraps the
// handler so its whole body runs under a Server-wide mutex shared by every
// tool registered through this method. Run() (below) dispatches concurrent
// tools/call requests to goroutines, so any handler that reads or writes a
// *Blackboard shared across tool calls — the common pattern of one
// server-wide Blackboard backing every registered tool — must register
// through RegisterBlackboardTool instead of RegisterTool: RegisterTool
// alone provides no protection, and a lock taken ad hoc inside only some
// handlers (the failure mode this replaces) still lets any handler that
// forgot to opt in interleave into a "protected" handler's critical
// section, corrupting the shared Blackboard. Every tool sharing a
// Blackboard must register through this method for the guarantee to hold.
func (s *Server) RegisterBlackboardTool(name, description string, props map[string]Property, required []string, handler ToolHandler) {
	s.RegisterTool(name, description, props, required, func(args json.RawMessage) *ToolResult {
		s.bbMu.Lock()
		defer s.bbMu.Unlock()
		return handler(args)
	})
}

// HasTool reports whether a tool with the given name has been registered.
// It exposes the private handler registry so tests (and other in-process
// callers) can assert tool presence without driving the stdio JSON-RPC loop.
func (s *Server) HasTool(name string) bool {
	_, ok := s.handler[name]
	return ok
}

// Invoke runs a registered tool's handler by name and returns its result along
// with true. If no tool is registered under that name, it returns (nil, false).
// This is a thin in-process seam over the handler registry; it bypasses the
// security, rate-limit, and tracing wrapping applied by the tools/call path in
// handleMessage and is intended for tests exercising tools by name.
func (s *Server) Invoke(name string, args json.RawMessage) (*ToolResult, bool) {
	handler, ok := s.handler[name]
	if !ok {
		return nil, false
	}
	return handler(args), true
}

// Run starts the MCP server loop, reading from stdin and writing to stdout.
// Handlers run concurrently so slow operations (Ollama calls) don't block
// other requests. A concurrency limiter prevents unbounded goroutine growth.
// Message size is capped via SetMaxMessageSize (default 1MB) to prevent
// memory exhaustion DoS attacks from oversized stdin lines.
func (s *Server) Run() error {
	// Concurrency limiter: max 5 simultaneous tool calls.
	// Beyond this, requests are rejected with a busy signal instead of
	// queuing indefinitely and causing gateway timeouts.
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup // tracks in-flight goroutines for clean shutdown

	// Use a Scanner with a max buffer to enforce message size limits.
	// Default is 1MB — MCP JSON-RPC messages should never be that large.
	maxSize := s.maxMessageSize
	if maxSize <= 0 {
		maxSize = 1 << 20 // 1 MB default
	}

	// Allow test override of stdin via s.in, otherwise read from os.Stdin.
	var reader io.Reader = os.Stdin
	if s.in != nil {
		reader = s.in
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, maxSize) // nil = default initial buffer, maxSize = hard ceiling

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Copy line data — scanner.Bytes() is only valid until next Scan().
		data := slices.Clone(line)

		// Fast-path: handle initialize/list/notifications synchronously.
		// These never block and must complete before tools/call can work.
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			s.writeError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		if msg.Method == "tools/call" {
			// Extract the tool name up front so a panicking handler can be
			// named in the recovery error response below.
			var callParams struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(msg.Params, &callParams)

			// Acquire semaphore slot; if full, reject with busy signal.
			select {
			case sem <- struct{}{}:
				wg.Add(1)
				go func(d []byte, id any, tool string) {
					defer wg.Done()
					defer func() { <-sem }()
					// A panicking tool handler must not take down the daemon:
					// answer this call with an internal error and keep serving.
					defer func() {
						if r := recover(); r != nil {
							s.writeError(id, -32603, fmt.Sprintf("Internal error: tool %q panicked: %v", tool, r))
						}
					}()
					s.handleMessage(d)
				}(data, msg.ID, callParams.Name)
			default:
				s.writeError(msg.ID, -32000, "Server busy: max 3 concurrent tool calls. Retry in a few seconds.")
			}
		} else {
			// Non-blocking methods: handle inline.
			s.handleMessage(data)
		}
	}

	if err := scanner.Err(); err != nil {
		wg.Wait() // flush in-flight handlers
		return fmt.Errorf("read stdin: %w", err)
	}
	wg.Wait() // flush in-flight handlers
	return nil
}

// SetSecurity enables argument sanitization, audit logging, and optional API key validation.
// When sanitize is true, tool call arguments are sanitized before reaching handlers.
// When apiKey is non-empty, every tools/call request must include a matching
// "bt_api_key" in its params. If both are disabled (default), no security is applied.
// Audit logging is automatically enabled when SetSecurity is called.
func (s *Server) SetSecurity(sanitize bool, apiKey string) {
	s.sanitizeArgs = sanitize
	s.apiKey = apiKey
	s.auditEnabled = sanitize || apiKey != ""
}

// SetRateLimit enables time-based rate limiting on tools/call requests.
// rate=tokens/second, burst=max burst size. Uses the security package's
// token bucket implementation with the server name as the client key.
// Set to 0 to disable (default: no rate limiting).
func (s *Server) SetRateLimit(rate float64, burst int) {
	if rate <= 0 || burst <= 0 {
		s.rateLimiter = nil
		return
	}
	s.rateLimiter = security.NewRateLimiter(rate, burst)
}

// SetAudit enables or disables structured security audit logging via
// the security package's slog-based AuditSecurityEvent. When enabled,
// auth failures, rate limit hits, and tool call execution are logged as
// structured SECURITY events. Enabled by default when SetSecurity is called.
func (s *Server) SetAudit(enabled bool) {
	s.auditEnabled = enabled
}

// SetMaxMessageSize sets the maximum size in bytes for a single JSON-RPC
// message line read from stdin. Messages exceeding this size are rejected
// with a parse error. A value <= 0 uses the default of 1 MB. This prevents
// memory exhaustion DoS attacks via oversized stdin lines.
func (s *Server) SetMaxMessageSize(size int) {
	s.maxMessageSize = size
}

// sanitizeArg recursively sanitizes JSON values by stripping null bytes,
// ANSI escape sequences, and control characters from strings.
func sanitizeArg(v any) any {
	switch val := v.(type) {
	case string:
		s := strings.ReplaceAll(val, "\x00", "")
		// Strip ANSI escape sequences
		for strings.Contains(s, "\x1b[") {
			start := strings.Index(s, "\x1b[")
			end := start + 2
			for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == ';' || s[end] == '[') {
				end++
			}
			if end < len(s) {
				end++
			}
			if end > len(s) {
				end = len(s)
			}
			s = s[:start] + s[end:]
		}
		return strings.TrimSpace(s)
	case map[string]any:
		out := make(map[string]any)
		for k, v2 := range val {
			out[k] = sanitizeArg(v2)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = sanitizeArg(item)
		}
		return out
	default:
		return v
	}
}

func (s *Server) handleMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		s.writeError(nil, -32700, "Parse error: "+err.Error())
		return
	}

	switch msg.Method {
	case "initialize":
		result := map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    s.name,
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]bool{},
			},
		}
		s.writeResult(msg.ID, result)

	case "tools/list":
		s.writeResult(msg.ID, map[string]any{
			"tools": s.tools,
		})

	case "tools/call":
		var params struct {
			Name        string          `json:"name"`
			Arguments   json.RawMessage `json:"arguments"`
			Traceparent string          `json:"traceparent,omitempty"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			s.writeError(msg.ID, -32602, "Invalid params: "+err.Error())
			return
		}

		if params.Name == "" {
			s.writeError(msg.ID, -32602, "Invalid params: missing tool name")
			return
		}

		// ── Security: API key validation with audit logging ──
		if s.apiKey != "" {
			var authParams struct {
				BtAPIKey string `json:"bt_api_key"`
			}
			_ = json.Unmarshal(msg.Params, &authParams)
			if authParams.BtAPIKey != s.apiKey {
				if s.auditEnabled {
					security.AuditSecurityEvent(context.Background(), "mcp_auth_failure",
						"server", s.name,
						"tool", params.Name,
					)
				} else {
					fmt.Fprintf(os.Stderr, "mcp: tools/call denied (bad api key) for tool=%s\n", params.Name)
				}
				s.writeError(msg.ID, -32001, "Authentication required: invalid or missing bt_api_key")
				return
			}
		}

		// ── Security: time-based rate limiting with audit logging ──
		if s.rateLimiter != nil && !s.rateLimiter.Allow(s.name) {
			if s.auditEnabled {
				security.AuditSecurityEvent(context.Background(), "mcp_rate_limit_exceeded",
					"server", s.name,
					"tool", params.Name,
				)
			} else {
				fmt.Fprintf(os.Stderr, "mcp: tools/call rate limited for tool=%s\n", params.Name)
			}
			s.writeError(msg.ID, -32000, "Rate limit exceeded. Retry later.")
			return
		}

		// ── Security: sanitize arguments ──
		if s.sanitizeArgs {
			var rawArgs any
			if err := json.Unmarshal(params.Arguments, &rawArgs); err == nil {
				cleaned := sanitizeArg(rawArgs)
				if data, err := json.Marshal(cleaned); err == nil {
					params.Arguments = data
				}
			}
		}

		handler, ok := s.handler[params.Name]
		if !ok {
			s.writeError(msg.ID, -32601, "Tool not found: "+params.Name)
			return
		}

		// ── Tracing: build context with optional W3C traceparent for distributed tracing ──
		// The Hermes gateway can inject a traceparent into tools/call params so MCP
		// server spans become children of the gateway's trace root. Without a traceparent,
		// the span starts a new trace (context.Background).
		traceCtx := context.Background()
		if params.Traceparent != "" {
			traceCtx = tracing.ContextWithTraceParentHeader(traceCtx, params.Traceparent)
		}

		// Execute the tool, recording timing for audit.
		start := time.Now()
		_, span := tracing.StartSpan(traceCtx, "mcp:"+params.Name)
		result := handler(params.Arguments)
		elapsed := time.Since(start)
		span.SetAttribute("tool", params.Name)
		span.SetAttribute("duration_ms", fmt.Sprintf("%d", elapsed.Milliseconds()))
		if params.Traceparent != "" {
			span.SetAttribute("traceparent", "injected")
		}
		span.End()

		// ── Security: audit tool execution ──
		if s.auditEnabled {
			security.AuditSecurityEvent(context.Background(), "mcp_tool_call",
				"server", s.name,
				"tool", params.Name,
				"duration_ms", elapsed.Milliseconds(),
			)
		}

		s.writeResult(msg.ID, result)

	case "notifications/initialized":
		// No response needed

	default:
		s.writeError(msg.ID, -32601, "Method not found: "+msg.Method)
	}
}

func (s *Server) writeResult(id any, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := Message{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(s.out, "%s\n", data)
}

func (s *Server) writeError(id any, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := Message{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(s.out, "%s\n", data)
}
