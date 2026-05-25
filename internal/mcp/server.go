package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	name    string
	tools   []ToolDef
	handler map[string]ToolHandler
	in      *bufio.Reader
	out     io.Writer
}

// NewServer creates a new MCP server.
func NewServer(name string) *Server {
	return &Server{
		name:    name,
		handler: make(map[string]ToolHandler),
		in:      bufio.NewReader(os.Stdin),
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
func (s *Server) Run() error {
	for {
		line, err := s.in.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		}
		if len(line) == 0 {
			continue
		}
		s.handleMessage(line)
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

		handler, ok := s.handler[params.Name]
		if !ok {
			s.writeError(msg.ID, -32601, "Tool not found: "+params.Name)
			return
		}

		result := handler(params.Arguments)
		s.writeResult(msg.ID, result)

	case "notifications/initialized":
		// No response needed

	default:
		s.writeError(msg.ID, -32601, "Method not found: "+msg.Method)
	}
}

func (s *Server) writeResult(id interface{}, result interface{}) {
	msg := Message{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, _ := json.Marshal(msg)
	fmt.Fprintf(s.out, "%s\n", data)
}

func (s *Server) writeError(id interface{}, code int, message string) {
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
