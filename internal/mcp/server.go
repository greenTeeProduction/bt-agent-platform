package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/security"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// Message is a JSON-RPC 2.0 message.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
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

// Run starts the MCP server loop, reading from stdin and writing to stdout.
// Handlers run concurrently so slow operations (Ollama calls) don't block
// other requests. A concurrency limiter prevents unbounded goroutine growth.
// Message size is capped via SetMaxMessageSize (default 1MB) to prevent
// memory exhaustion DoS attacks from oversized stdin lines.
func (s *Server) Run() error {
	// Concurrency limiter: max 3 simultaneous tool calls.
	// Beyond this, requests are rejected with a busy signal instead of
	// queuing indefinitely and causing gateway timeouts.
	sem := make(chan struct{}, 3)
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
		data := make([]byte, len(line))
		copy(data, line)

		// Fast-path: handle initialize/list/notifications synchronously.
		// These never block and must complete before tools/call can work.
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			s.writeError(nil, -32700, "Parse error: "+err.Error())
			continue
		}

		if msg.Method == "tools/call" {
			// Acquire semaphore slot; if full, reject with busy signal.
			select {
			case sem <- struct{}{}:
				wg.Add(1)
				go func(d []byte) {
					defer wg.Done()
					defer func() { <-sem }()
					s.handleMessage(d)
				}(data)
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
func sanitizeArg(v interface{}) interface{} {
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
	case map[string]interface{}:
		out := make(map[string]interface{})
		for k, v2 := range val {
			out[k] = sanitizeArg(v2)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
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
		result := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    s.name,
				"version": "0.1.0",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]bool{},
			},
		}
		s.writeResult(msg.ID, result)

	case "tools/list":
		s.writeResult(msg.ID, map[string]interface{}{
			"tools": s.tools,
		})

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
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
			json.Unmarshal(msg.Params, &authParams)
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
			var rawArgs interface{}
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

		// Execute the tool, recording timing for audit.
		start := time.Now()
		// ── Tracing: wrap tool execution in a span ──
		_, span := tracing.StartSpan(context.Background(), "mcp:"+params.Name)
		result := handler(params.Arguments)
		elapsed := time.Since(start)
		span.SetAttribute("tool", params.Name)
		span.SetAttribute("duration_ms", fmt.Sprintf("%d", elapsed.Milliseconds()))
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

func (s *Server) writeResult(id interface{}, result interface{}) {
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

func (s *Server) writeError(id interface{}, code int, message string) {
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
