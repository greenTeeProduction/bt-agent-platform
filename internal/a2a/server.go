package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
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

	// CardCache holds the same signed agent cards the A2A server advertises
	// (see Server.CardCache), keyed by agent name. The auction-bid branch of
	// Execute scores announcements against these cards rather than re-deriving
	// (and re-signing) a fresh one from the tree definition on every request.
	CardCache map[string]*a2a.AgentCard

	// History is the platform's shared run-history store. Cancel records a
	// "cancelled" RunRecord here so cancelled tasks leave a trace instead of
	// vanishing silently. Nil is tolerated (e.g. in tests that don't need it).
	History *agent.History
}

// Execute runs the BT agent for the given A2A task.
func (e *BTAgentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		// Submit the task. The a2a-go v2 stream contract requires the FIRST
		// event to be a Task (or message) — a leading TaskStatusUpdateEvent is
		// rejected with "first event must be a Task or a message" and the whole
		// call fails as INVALID_AGENT_RESPONSE while the tree still executes.
		if execCtx.StoredTask == nil {
			if !yield(a2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
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

		// Find the target agent: the per-agent endpoint's interceptor carries
		// the name in ctx. ContextID is a fallback for direct callers only —
		// the SDK owns that field (it must match the request's context id;
		// overwriting it fails event validation with "context IDs don't match").
		agentName, _ := ctx.Value(agentNameKey{}).(string)
		if agentName == "" {
			agentName = execCtx.ContextID
		}
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

		// Bid-aware responder: when the incoming message is a JSON
		// TaskAnnouncement, this endpoint is a candidate in an auction — not a
		// task runner. Score the agent's eligibility from its card (tree tags)
		// and answer with a Bid instead of executing the announcement JSON as
		// literal task text against the behavior tree. An ineligible agent
		// declines silently (a completed task with no artifact) so the
		// auctioneer drops it rather than seeing a spurious failure.
		if ann, isAnn := parseAnnouncement(taskText); isAnn {
			// Prefer the already-signed card the A2A server actually advertises
			// (Server.CardCache, mirrored here) over re-deriving one from the
			// tree definition on every inbound request; fall back to a fresh
			// conversion only when no cached card exists.
			card := e.CardCache[agentName]
			if card == nil {
				card, _ = ConvertToAgentCard(inst.Definition, "")
			}
			if payload, ok := RespondToAnnouncement(agentName, card, taskText); ok {
				if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(payload)), nil) {
					return
				}
				if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted,
					a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
						fmt.Sprintf("bid submitted for task %s", ann.TaskID)))), nil) {
					return
				}
				return
			}
			// Recognized announcement but ineligible (or no usable card):
			// decline without running the tree.
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
					fmt.Sprintf("declined auction for task %s", ann.TaskID)))), nil) {
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
		engine.Info("A2A executing agent", "agent", agentName, "tree_name", tree.Name, "tree_type", tree.Type)
		bb := &engine.Blackboard{
			Task: taskText,
			LLM:  e.LLM,
		}
		bt := engine.BuildTree(tree, bb)
		startTime := time.Now()
		result := engine.RunTask(bb, bt)
		elapsed := time.Since(startTime)

		if e.History != nil {
			historyAgent := agentName
			if award, ok := bb.ChainState["auction_award"].(Award); ok && award.WinnerName != "" {
				historyAgent = award.WinnerName
			}
			_ = e.History.Record(agent.RunRecord{
				AgentName: historyAgent,
				Task:      taskText,
				Outcome:   bb.Outcome,
				Output:    result,
				Duration:  elapsed.Truncate(time.Second).String(),
				StartedAt: startTime,
				EndedAt:   startTime.Add(elapsed),
			})
		}

		bridge := &TaskStateBridge{}
		switch state := bridge.BTToA2A(bb.Outcome); state {
		case a2a.TaskStateCompleted:
			if !yield(a2a.NewArtifactEvent(execCtx, a2a.NewTextPart(result)), nil) {
				return
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCompleted,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(
					fmt.Sprintf("BT tree %s completed in %v", inst.Definition.Tree, elapsed.Round(time.Millisecond))))), nil) {
				return
			}
		case a2a.TaskStateInputRequired:
			msg := result
			if msg == "" {
				msg = fmt.Sprintf("BT tree %s awaiting input: %s (elapsed %v)", inst.Definition.Tree, bb.Outcome, elapsed.Round(time.Millisecond))
			}
			if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateInputRequired,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(msg))), nil) {
				return
			}
		default:
			errMsg := failureEventMessage(inst.Definition.Tree, bb.Outcome, result, elapsed)
			if !yield(a2a.NewStatusUpdateEvent(execCtx, state,
				a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(errMsg))), nil) {
				return
			}
		}
	}
}

// failureEventMessage builds the status message for a non-completed bridged
// outcome. When bb.Result is set it is preferred as the human-readable
// message; for a rate-limit carryover the sentinel outcome is kept in the
// text regardless, because the delegating caller (internal/engine's
// DelegateToA2A node) detects the sentinel in the returned error to classify
// the delegation as a healthy deferred pause instead of a hard failure —
// the "new call site" gap the carryover ADRs warn about.
func failureEventMessage(tree, outcome, result string, elapsed time.Duration) string {
	if result == "" {
		return fmt.Sprintf("BT tree %s failed: %s (elapsed %v)", tree, outcome, elapsed.Round(time.Millisecond))
	}
	if agent.IsRateLimitCarryover(outcome) && !strings.Contains(result, outcome) {
		return outcome + ": " + result
	}
	return result
}

// Cancel handles task cancellation.
func (e *BTAgentExecutor) Cancel(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		if e.History != nil {
			agentName, _ := ctx.Value(agentNameKey{}).(string)
			if agentName == "" {
				agentName = execCtx.ContextID
			}
			if agentName != "" {
				_ = e.History.Record(agent.RunRecord{AgentName: agentName, Outcome: "cancelled"})
			}
		}

		if !yield(a2a.NewStatusUpdateEvent(execCtx, a2a.TaskStateCanceled,
			a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("task cancelled by client"))), nil) {
			return
		}
	}
}

// RespondToAnnouncement is the candidate endpoint's server-side bidder: it
// parses an auctioneer's announcement out of the raw A2A task text, scores it
// against the responding agent's card, and returns the JSON-encoded Bid the
// agent submits. It reports ok=false — declining silently — when the task text
// is not a valid announcement, or when ScoreAnnouncement decides the agent
// should not bid (tags uncovered or confidence below the announced minimum).
// This closes the auction loop: an Auctioneer's CollectBids can drive candidate
// agents through this function and consume the bids it returns.
func RespondToAnnouncement(bidderName string, card *a2a.AgentCard, taskText string) (string, bool) {
	if taskText == "" {
		return "", false
	}

	ann, ok := parseAnnouncement(taskText)
	if !ok {
		return "", false // task text was not a well-formed announcement
	}

	bid, ok := ScoreAnnouncement(bidderName, card, ann)
	if !ok {
		return "", false
	}

	payload, err := json.Marshal(bid)
	if err != nil {
		return "", false
	}
	return string(payload), true
}

// parseAnnouncement decodes taskText as a JSON TaskAnnouncement, reporting
// ok=true only when it is well-formed: it unmarshals cleanly, carries the
// task_announcement kind, and passes Validate(). Empty text, non-JSON garbage,
// and JSON that is structurally not an announcement all report ok=false so the
// caller can route the message to the ordinary task path instead of the auction
// responder.
func parseAnnouncement(taskText string) (TaskAnnouncement, bool) {
	if taskText == "" {
		return TaskAnnouncement{}, false
	}
	var ann TaskAnnouncement
	if err := json.Unmarshal([]byte(taskText), &ann); err != nil {
		return TaskAnnouncement{}, false // not JSON — an ordinary task
	}
	if ann.Kind() != KindAnnouncement {
		return TaskAnnouncement{}, false
	}
	if err := ann.Validate(); err != nil {
		return TaskAnnouncement{}, false // structurally not an announcement
	}
	return ann, true
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
		Reg:       reg,
		LLM:       llmClient,
		CardCache: cards,
	}

	return &Server{
		Port:      port,
		BaseURL:   baseURL,
		Reg:       reg,
		Executor:  executor,
		CardCache: cards,
	}, nil
}

// RefreshCards rebuilds CardCache from the live agent registry and keeps
// Executor.CardCache (the auction-bid scoring path) in sync. An agent created
// after the server started — via bt_agent_create or autopilot's
// activateAutomation, both of which call Create against this same registry —
// is otherwise invisible to the per-agent endpoint, the global agent card, and
// AuctionCardSource's candidate pool until the process restarts and NewServer
// rebuilds the one-shot snapshot. Callers should invoke this after any
// registry mutation that adds or removes an agent.
func (s *Server) RefreshCards() error {
	cards, err := BuildCardRegistry(s.Reg, s.BaseURL)
	if err != nil {
		return fmt.Errorf("refresh card registry: %w", err)
	}
	s.CardCache = cards
	if s.Executor != nil {
		s.Executor.CardCache = cards
	}
	return nil
}

// AuctionCardSource returns a closure yielding this server's live card registry,
// suitable for installing as a2a.AuctionCardsFn so production auctions draw their
// candidates from the same cards the A2A server serves. Returning the closure
// (rather than the map) lets callers wire the seam without importing the a2a-go
// AgentCard type.
func (s *Server) AuctionCardSource() func() map[string]*a2a.AgentCard {
	return func() map[string]*a2a.AgentCard { return s.CardCache }
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

	slog.Info("a2a: starting A2A server", "addr", addr)
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

	if sig, err := SignAgentCard(card); err == nil {
		card.Signatures = append(card.Signatures, a2a.AgentCardSignature{Signature: sig})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(card)
}

// handleWellKnown serves well-known discovery.
func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

// agentNameKey carries the target agent name through the request context.
type agentNameKey struct{}

// agentNameInterceptor injects an agent name into the request context. It
// must NOT touch execCtx.ContextID: that field is the SDK's server-generated
// correlation id and every emitted event is validated against it.
type agentNameInterceptor struct {
	name string
}

func (a *agentNameInterceptor) Intercept(ctx context.Context, _ *a2asrv.ExecutorContext) (context.Context, error) {
	return context.WithValue(ctx, agentNameKey{}, a.name), nil
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
