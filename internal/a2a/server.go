package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// BTAgentExecutor implements a2asrv.AgentExecutor for the BT platform.
type BTAgentExecutor struct {
	Reg     *agent.Registry
	LLM     llm.LLM
	TreeMap map[string]*evolution.SerializableNode
}

// Execute runs the BT agent for the given A2A task.
func (e *BTAgentExecutor) Execute(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		// Submit the task
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateSubmitted, nil), nil) {
				return
			}
		}

		// Extract task text from the first text part
		taskText := ""
		if execCtx.Message != nil {
			for _, part := range execCtx.Message.Parts {
				if t := part.Text(); t != "" {
					taskText = t
				}
			}
		}

		// Find target agent from context ID
		agentName := execCtx.ContextID
		if agentName == "" {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("no agent specified"))), nil) {
				return
			}
			return
		}

		inst, err := e.Reg.Get(agentName)
		if err != nil || inst == nil {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
					fmt.Sprintf("agent %q not found", agentName)))), nil) {
				return
			}
			return
		}

		// Resolve tree
		var tree *evolution.SerializableNode
		if e.TreeMap != nil {
			tree = e.TreeMap[agentName]
		}
		if tree == nil {
			tree = resolveTreeByID(inst.Definition.Tree)
		}
		if tree == nil {
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
					fmt.Sprintf("no tree for agent %q (tree: %s)", agentName, inst.Definition.Tree)))), nil) {
				return
			}
			return
		}

		// Mark working
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateWorking, nil), nil) {
			return
		}

		// Execute
		bb := &engine.Blackboard{
			Task: taskText,
			LLM:  e.LLM,
		}
		bt := engine.BuildTree(tree, bb)
		startTime := time.Now()
		result := engine.RunTask(bb, bt)
		elapsed := time.Since(startTime)

		if bb.Outcome == "success" {
			if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil) {
				return
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
					fmt.Sprintf("BT tree %s completed in %v", inst.Definition.Tree, elapsed.Round(time.Millisecond))))), nil) {
				return
			}
		} else {
			errMsg := fmt.Sprintf("BT tree %s failed: %s (elapsed %v)", inst.Definition.Tree, bb.Outcome, elapsed.Round(time.Millisecond))
			if result != "" {
				errMsg = result
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateFailed,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(errMsg))), nil) {
				return
			}
		}
	}
}

// Cancel handles task cancellation.
func (e *BTAgentExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("task cancelled by client"))), nil) {
			return
		}
	}
}

// resolveTreeByID is injected from main.go via SetTreeResolver.
var resolveTreeByID = func(_ string) *evolution.SerializableNode {
	return nil
}

// SetTreeResolver injects the tree resolution function.
func SetTreeResolver(fn func(string) *evolution.SerializableNode) {
	resolveTreeByID = fn
}

// ─── HTTP Server ─────────────────────────────────────────────────────────

// Server is an A2A protocol server for the BT platform.
type Server struct {
	Port      int
	BaseURL   string
	Reg       *agent.Registry
	Executor  *BTAgentExecutor
	CardCache map[string]*a2a.AgentCard
	httpSrv   *http.Server
}

// NewServer creates a new A2A server.
func NewServer(reg *agent.Registry, llmClient llm.LLM, port int, baseURL string) (*Server, error) {
	cards, err := BuildCardRegistry(reg, baseURL)
	if err != nil {
		return nil, fmt.Errorf("build card registry: %w", err)
	}

	executor := &BTAgentExecutor{
		Reg: reg,
		LLM: llmClient,
	}

	return &Server{
		Port:      port,
		BaseURL:   baseURL,
		Reg:       reg,
		Executor:  executor,
		CardCache: cards,
	}, nil
}

// Start begins listening on the configured port.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/agent-card.json", s.handleGlobalAgentCard)
	mux.HandleFunc("/.well-known/", s.handleWellKnown)
	mux.HandleFunc("/agents/", s.handleAgentEndpoint)
	mux.HandleFunc("/health", s.handleHealth)

	addr := fmt.Sprintf(":%d", s.Port)
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("[a2a] Starting A2A server on %s", addr)
	return s.httpSrv.ListenAndServe()
}

// handleGlobalAgentCard serves the global Agent Card.
func (s *Server) handleGlobalAgentCard(w http.ResponseWriter, _ *http.Request) {
	card := &a2a.AgentCard{
		Name:               "BT Agent Platform",
		Description:        "Go behavior tree agent platform — 41+ trees across 7 domains",
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"text/plain", "application/json", "text/markdown"},
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(s.BaseURL, a2a.TransportProtocolJSONRPC),
		},
	}

	for _, c := range s.CardCache {
		card.Skills = append(card.Skills, c.Skills...)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

// handleWellKnown serves well-known discovery.
func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// agentNameInterceptor injects an agent name into the executor context.
type agentNameInterceptor struct {
	name string
}

func (a *agentNameInterceptor) Intercept(ctx context.Context, execCtx *a2asrv.ExecutorContext) (context.Context, error) {
	execCtx.ContextID = a.name
	return ctx, nil
}

// handleAgentEndpoint routes per-agent A2A JSON-RPC calls.
func (s *Server) handleAgentEndpoint(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/agents/")
	agentName := strings.Split(path, "/")[0]

	if agentName == "" {
		names := make([]string, 0, len(s.CardCache))
		for name := range s.CardCache {
			names = append(names, name)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"agents": names})
		return
	}

	if _, ok := s.CardCache[agentName]; !ok {
		http.Error(w, fmt.Sprintf(`{"error":"agent %q not found"}`, agentName), http.StatusNotFound)
		return
	}

	handler := a2asrv.NewJSONRPCHandler(
		a2asrv.NewHandler(s.Executor,
			a2asrv.WithExecutorContextInterceptor(&agentNameInterceptor{name: agentName}),
		),
	)
	handler.ServeHTTP(w, r)
}

// handleHealth serves health check.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"server": "a2a",
		"agents": len(s.CardCache),
		"port":   s.Port,
	})
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}
