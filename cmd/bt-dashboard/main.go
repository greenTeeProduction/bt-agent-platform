package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/agentexec"
	"github.com/nico/go-bt-evolve/internal/api"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/doormate"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/security"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

//go:embed static/*
var staticFS embed.FS

var kg *knowledge.KnowledgeGraph
var sharedLLM llm.LLM
var dashAgentRunner *agent.RunDeps

// dlq is the dead letter queue for failed agent tasks.
// Persisted to ~/.go-bt-evolve/dead_letter_queue.json.
var dlq *reliability.DeadLetterQueue

// scalability components: WorkerPool and ConcurrencyLimiter for agent execution.
var dashWorkerPool *reliability.WorkerPool
var dashConcurrencyLimiter *reliability.ConcurrencyLimiter

// dashTaskQueue is the pending-task backlog surfaced by /api/scalability.
// It is fed by in-flight pipeline runs (pipeline_handlers.go) and drained as
// each run completes, so the endpoint reports real queue depth instead of 0.
var dashTaskQueue *reliability.TaskQueue

// dashAgentRouter distributes agent execution across the horizontal-scaling
// substrate. In single-node mode it holds one healthy in-process local
// executor; RemoteExecutor peers join it when configured. The scalability
// endpoint reports its executor count and health.
var dashAgentRouter *reliability.AgentRouter

// dashConfig holds the runtime configuration loaded at startup.
var dashConfig *config.Config

// sessionStore manages authenticated user sessions (login/logout, cookie-based auth).
var sessionStore *security.SessionStore

// loginThrottle prevents brute-force password guessing with per-IP exponential backoff.
var loginThrottle *security.LoginThrottle

// Sprint tracking
var sprintState = struct {
	sync.Mutex
	Running        bool
	JobID          string
	StartedAt      time.Time
	Progress       string
	TasksTotal     int
	TasksCompleted int
	CurrentTask    string
}{}

// taskStore is the persistent task pipeline.
var taskStore *dashboard.TaskStore

// companyState holds the startup simulation state.
var companyState *startup.CompanyState

func getHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	if home == "" {
		home = "."
	}
	return home
}

func init() {
	home := getHomeDir()
	taskStore = dashboard.NewTaskStore(home + "/.go-bt-evolve/tasks.json")
	companyState = startup.NewDefaultCompany()
}

func main() {
	engine.Init()
	engine.SetAsDefault()

	port := os.Getenv("BT_DASHBOARD_PORT")
	if port == "" {
		port = "9800"
	}

	// Structured logging
	slog.Info("BT Dashboard starting", "port", port)

	kg = knowledge.BuildKnowledgeGraph()

	// Dead letter queue — persisted alongside other agent state
	dlqPath := getHomeDir() + "/.go-bt-evolve/dead_letter_queue.json"
	dlq = reliability.NewDeadLetterQueue(dlqPath)
	slog.Info("DLQ initialized", "path", dlqPath, "entries", dlq.Len())

	// Deploy-drift watcher (program 94b0b31) — detection-only by default; WARNs
	// when this binary falls behind repo HEAD. BT_AUTO_REBUILD_ON_DRIFT=1 opts
	// into out-of-place rebuild+swap.
	if repoDir, wdErr := os.Getwd(); wdErr == nil {
		agent.StartDriftWatcher(context.Background(), agent.DriftWatchConfig{
			RepoDir:         repoDir,
			RunningRevision: dashboard.ReadBuildIdentity().Revision,
			AutoRebuild:     agent.AutoRebuildEnabled(),
			Targets:         agent.DashboardRebuildTargets(repoDir),
			Binary:          "bt-dashboard",
			Backoff:         agent.NewRebuildBackoff(),
		}, agent.DefaultDriftCheckInterval)
	}

	// Scalability components: worker pool and concurrency limiter for agent tasks
	dashWorkerPool = reliability.NewWorkerPool(4)                 // 4 concurrent agent workers
	dashConcurrencyLimiter = reliability.NewConcurrencyLimiter(2) // max 2 concurrent LLM-bound agent executions

	// Horizontal-scaling substrate: task queue + agent router. The router starts
	// with a single in-process local executor (RemoteExecutor peers join it when
	// configured); the queue tracks pending pipeline work. Both are surfaced by
	// /api/scalability instead of the former nil/0 placeholders.
	dashTaskQueue = reliability.NewTaskQueue(getHomeDir() + "/.go-bt-evolve/task_queue.json")
	dashAgentRouter = reliability.NewAgentRouter(reliability.NewLocalExecutor(
		"dashboard-local",
		func(agentName, task string) (*reliability.AgentResult, error) {
			output, outcome, err := newAgentExecutor().RunTask(agentName, task, "")
			return &reliability.AgentResult{
				Agent:   agentName,
				Task:    task,
				Output:  output,
				Success: outcome == "success" || outcome == "completed",
			}, err
		},
	))
	slog.Info("Scalability components initialized",
		"worker_pool_size", 4,
		"concurrency_limit", 2,
		"router_executors", len(dashAgentRouter.Executors()),
		"queue_pending", dashTaskQueue.Len())

	// ── Tracing (OTel SDK; no-op unless OTEL_EXPORTER_OTLP_ENDPOINT/BT_OTLP_ENDPOINT set) ──
	tracingShutdown := tracing.InitFromEnv("bt-dashboard")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(ctx)
	}()

	logShutdown := engine.InitLogExport("bt-dashboard")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = logShutdown(ctx)
	}()

	var err error
	sharedLLM, err = llm.NewClient(llm.DefaultConfig())
	if err != nil {
		slog.Warn("Ollama unavailable", "error", err)
		sharedLLM = nil
	}

	// Load runtime configuration
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		slog.Warn("Failed to load config, using defaults", "error", cfgErr)
		dashConfig = &config.Config{}
	} else {
		dashConfig = cfg
		slog.Info("Configuration loaded", "llm_provider", cfg.LLMProvider, "ollama_model", cfg.OllamaModel)
	}

	if runner, err := agentexec.NewRunDeps(); err != nil {
		slog.Warn("In-process agent runner unavailable (pipelines will fail)", "error", err)
	} else {
		dashAgentRunner = runner
		slog.Info("In-process agent runner initialized")
	}

	// API key from env/config — if set, all /api/* endpoints require X-API-Key header
	apiKey := os.Getenv("BT_API_KEY")
	if apiKey == "" && dashConfig != nil {
		apiKey = dashConfig.APIKey
	}

	// Session store — cookie-based session management with TTL-based expiry.
	// Sessions are backed by the same API key for password-based login.
	// CookieSecure matches TLS config (auto-detected below).
	sessionStore = security.NewSessionStore(security.SessionStoreConfig{
		DefaultTTL:  24 * time.Hour,
		CookieName:  "bt_session",
		CookiePath:  "/api",
		MaxSessions: 100,
	})
	slog.Info("Session store initialized", "ttl", "24h", "cookie", "bt_session")

	// Login throttle — per-IP exponential backoff for brute-force protection
	loginThrottle = security.NewLoginThrottle(security.DefaultLoginThrottleConfig())
	slog.Info("Login throttle initialized",
		"lockout_threshold", 20,
		"lockout_duration", "30m",
	)

	// HITL — human-in-the-loop approval policy and store
	config.ApplyHITLPolicy(dashConfig)
	hitlBase := filepath.Join(getHomeDir(), ".go-bt-evolve")
	if _, err := hitl.InitStore(hitlBase); err != nil {
		slog.Warn("HITL store init failed", "error", err)
	} else {
		slog.Info("HITL store initialized", "path", hitlBase+"/hitl")
	}

	// CORS origin: default to wildcard for dev, restrict in production via config
	corsOrigin := dashConfig.CORSDashboardOrigin
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	slog.Info("Dashboard CORS origin", "origin", corsOrigin)

	// Rate limiter: 10 req/sec per client, burst 10.
	// Production tuning: the Jetson ARM64 cannot serve 100 req/s from a single process,
	// so 10/10 provides meaningful protection against burst abuse while allowing
	// normal interactive dashboard usage. The security probe sends 25 rapid requests;
	// with burst 10 and 10 req/s refill, the 20th+ request within the same second
	// should reliably trigger a 429.
	rateLimiter := security.NewRateLimiter(10, 10)

	// Security audit buffer: capture security events in-memory for dashboard visibility
	auditBuffer := security.NewAuditBuffer(200)
	security.SetGlobalAuditBuffer(auditBuffer)

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveDashboard)
	mux.HandleFunc("/static/", serveStatic)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/metrics", dashboard.PrometheusHandler().ServeHTTP)
	mux.HandleFunc("/api/alerts", handleAlerts)
	mux.HandleFunc("/api/alerts/rules", handleAlertRules)
	mux.HandleFunc("/api/security/audit", handleSecurityAudit)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/openapi.json", handleOpenAPI)
	mux.HandleFunc("/api/swagger", handleSwagger)
	mux.HandleFunc("/api/scalability", handleScalability)
	mux.HandleFunc("/api/login", handleLogin)
	mux.HandleFunc("/api/logout", handleLogout)
	mux.HandleFunc("/api/session", handleSession)
	// Session-aware auth: checks session cookie first, falls back to API key header.
	// This preserves backward compatibility with existing X-API-Key header workflows
	// while adding cookie-based browser sessions via /api/login.
	sessionAuth := func(next http.HandlerFunc) http.HandlerFunc {
		if apiKey == "" {
			return next
		}
		return sessionStore.SessionMiddleware(apiKey, nil)(next)
	}

	mux.HandleFunc("/api/summary", sessionAuth(handleSummary))
	mux.HandleFunc("/api/metrics/live", sessionAuth(handleMetricsLive))
	mux.HandleFunc("/api/trees", sessionAuth(handleTrees))
	mux.HandleFunc("/api/thinktank/fellows", sessionAuth(handleFellows))
	mux.HandleFunc("/api/thinktank/analyze", sessionAuth(handleAnalyze))
	mux.HandleFunc("/api/company/default", sessionAuth(handleDefaultCompany))
	mux.HandleFunc("/api/agents", sessionAuth(handleAgentsList))
	mux.HandleFunc("/api/agents/run", sessionAuth(handleAgentRun))
	mux.HandleFunc("/api/agents/execute", sessionAuth(handleAgentExecute))
	mux.HandleFunc("/api/agents/create", sessionAuth(handleAgentCreate))
	mux.HandleFunc("/api/agents/delete", sessionAuth(handleAgentDelete))
	mux.HandleFunc("/api/tasks", sessionAuth(handleTasks))
	mux.HandleFunc("/api/tasks/approve", sessionAuth(handleTaskApprove))
	mux.HandleFunc("/api/tasks/create", sessionAuth(handleTaskCreate))
	mux.HandleFunc("/api/tasks/reject", sessionAuth(handleTaskReject))
	mux.HandleFunc("/api/hitl/pending", sessionAuth(dashboard.HandleHITLPending))
	mux.HandleFunc("/api/hitl/", sessionAuth(dashboard.HandleHITL))
	mux.HandleFunc("/api/sprint/execute", sessionAuth(handleSprintExecute))
	mux.HandleFunc("/api/sprint/status", sessionAuth(handleSprintStatus))
	mux.HandleFunc("/api/tree/structure", sessionAuth(handleTreeStructure))
	mux.HandleFunc("/api/chat", sessionAuth(handleChat))
	mux.HandleFunc("/api/dlq", sessionAuth(handleDLQ))
	mux.HandleFunc("/api/dlq/replay", sessionAuth(handleDLQReplay))
	mux.HandleFunc("/api/dlq/purge", sessionAuth(handleDLQPurge))
	mux.HandleFunc("/api/pipelines", sessionAuth(handlePipelines))
	mux.HandleFunc("/api/pipelines/run", sessionAuth(handlePipelineRun))
	mux.HandleFunc("/api/pipelines/status", sessionAuth(handlePipelineStatus))
	mux.HandleFunc("/api/blackboard", sessionAuth(handleBlackboard))
	mux.HandleFunc("/api/blackboard/scopes", sessionAuth(handleBlackboardScopes))

	// DoorMate components initialization & registration
	dmStore, err := doormate.NewStore(filepath.Join(getHomeDir(), ".go-bt-evolve", "doormate"))
	if err != nil {
		slog.Error("DoorMate store initialization failed", "error", err)
	} else {
		slog.Info("DoorMate store initialized", "path", filepath.Join(getHomeDir(), ".go-bt-evolve", "doormate"))
		dmAgent := doormate.NewPageAgent(sharedLLM)
		dmHandler := doormate.NewHandler(dmStore, dmAgent)

		mux.HandleFunc("/api/doormate/intent", sessionAuth(dmHandler.HandleIntent))
		mux.HandleFunc("/api/doormate/bookmark", sessionAuth(dmHandler.HandleBookmark))
		mux.HandleFunc("/api/doormate/rate", sessionAuth(dmHandler.HandleRate))
		mux.HandleFunc("/api/doormate/profile", sessionAuth(dmHandler.HandleProfile))
	}

	// TLS support — set BT_TLS_CERT and BT_TLS_KEY to enable HTTPS
	tlsCert := os.Getenv("BT_TLS_CERT")
	tlsKey := os.Getenv("BT_TLS_KEY")
	tlsEnabled := tlsCert != "" && tlsKey != ""

	// Security headers — enable HSTS when TLS is active
	secCfg := security.DefaultSecurityHeaders()
	if tlsEnabled {
		secCfg.EnableHSTS = true
		// Update session store cookie security for HTTPS
		sessionStore = security.NewSessionStore(security.SessionStoreConfig{
			DefaultTTL:   24 * time.Hour,
			CookieName:   "bt_session",
			CookiePath:   "/api",
			CookieSecure: true,
			MaxSessions:  100,
		})
	}

	// Middleware stack: security headers → request ID → tracing → cors → csrf → content_type → sanitize → rate limit → metrics → compression → response validation
	var handler http.Handler = mux
	handler = security.SecurityHeadersMiddleware(secCfg)(handler)
	handler = security.RequestIDMiddleware(handler) // correlation IDs for audit trail
	handler = tracingMiddleware(handler)            // distributed tracing spans per request
	handler = security.CrossOriginMiddleware(corsOrigin, "GET, POST, PUT, DELETE, OPTIONS")(handler)
	handler = security.CSRFMiddleware(nil)(handler)                   // CSRF protection for state-changing requests
	handler = security.JSONContentTypeMiddleware(handler)             // enforce application/json Content-Type on mutating requests
	handler = security.SanitizeMiddleware(1 << 20)(handler)           // 1MB body limit + input cleaning
	handler = security.RateLimitMiddleware(rateLimiter, nil)(handler) // token bucket rate limiting
	handler = dashboard.MetricsMiddleware(handler)                    // Prometheus metrics collection
	handler = api.CompressionMiddleware(handler)                      // gzip response compression (70-90% size reduction for JSON/HTML)
	handler = api.ResponseValidator(api.DashboardRoutes(), &api.ResponseValidatorConfig{
		Logger: slog.Default(),
		SkipPaths: map[string]bool{
			"/api/health":  true, // constant response — no schema drift risk
			"/api/metrics": true, // Prometheus text format, not JSON
			"/api/swagger": true, // HTML page, not JSON
		},
		Enforce: dashConfig.APIEnforceResponseValidation, // controlled by BT_API_ENFORCE_RESPONSE_VALIDATION env var or config file
	})(handler)

	// Security: enforce TLS. When cert+key are configured via env vars,
	// serve HTTPS with HSTS enabled. Plain HTTP otherwise (dev mode).
	addr := ":" + port
	if tlsEnabled {
		slog.Info("BT Studio Dashboard ready (TLS)", "addr", addr)
		if err := http.ListenAndServeTLS(addr, tlsCert, tlsKey, handler); err != nil {
			slog.Error("Dashboard server failed", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Warn("BT Studio Dashboard ready (HTTP — set BT_TLS_CERT+BT_TLS_KEY for TLS)", "addr", addr)
		if err := http.ListenAndServe(addr, handler); err != nil {
			slog.Error("Dashboard server failed", "error", err)
			os.Exit(1)
		}
	}
}

// tracingResponseWriter wraps http.ResponseWriter to capture the status code
// for the tracing middleware's http.status_code span attribute.
type tracingResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *tracingResponseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *tracingResponseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.statusCode = http.StatusOK
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *tracingResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

var _ http.Flusher = (*tracingResponseWriter)(nil)

// tracingMiddleware creates an OTel span (via the tracing facade, activated
// by tracing.InitFromEnv at startup) for every HTTP request. If the incoming
// request carries a W3C traceparent header, the span joins the remote trace
// as a child — mirroring internal/engine/mcp_server.go's traceparent handling.
func tracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := r.Context()
		if tp := r.Header.Get("traceparent"); tp != "" {
			ctx = tracing.ContextWithTraceParentHeader(ctx, tp)
		}

		ctx, span := tracing.StartSpan(ctx, "http:"+r.Method+" "+r.URL.Path)
		defer span.End()
		span.SetAttribute("http.method", r.Method)
		span.SetAttribute("http.url", r.URL.String())

		rw := &tracingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		span.SetAttribute("http.status_code", fmt.Sprintf("%d", rw.statusCode))
		span.SetAttribute("http.duration_ms", fmt.Sprintf("%d", time.Since(start).Milliseconds()))
	})
}

func serveDashboard(w http.ResponseWriter, _ *http.Request) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "dashboard not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		http.Error(w, "static files not available", http.StatusInternalServerError)
		return
	}
	http.StripPrefix("/static/", http.FileServer(http.FS(sub))).ServeHTTP(w, r)
}

func handleSummary(w http.ResponseWriter, _ *http.Request) {
	cats := make(map[string]int)
	for _, t := range kg.Trees {
		cats[t.Category]++
	}
	model := "qwen3.6:35b-a3b"
	if dashConfig != nil && dashConfig.OllamaModel != "" {
		model = dashConfig.OllamaModel
	}
	_ = encodeJSON(w, map[string]interface{}{
		"total_trees": len(kg.Trees),
		"categories":  cats,
		"mcp_tools":   26,
		"model":       model,
	})
}

func handleMetricsLive(w http.ResponseWriter, _ *http.Request) {
	cats := make(map[string]int)
	for _, t := range kg.Trees {
		cats[t.Category]++
	}
	m := dashboard.Collect(len(kg.Trees), cats)
	_ = encodeJSON(w, m)
}
func handleTrees(w http.ResponseWriter, _ *http.Request) {
	r2 := make([]map[string]interface{}, 0, 8)
	for _, t := range kg.Trees {
		r2 = append(r2, map[string]interface{}{"id": t.ID, "name": t.Name, "category": t.Category, "node_count": t.NodeCount})
	}
	_ = encodeJSON(w, r2)
}
func handleFellows(w http.ResponseWriter, _ *http.Request) {
	f := thinktank.DefaultFellows()
	r2 := make([]map[string]interface{}, 0, 8)
	for _, x := range f {
		r2 = append(r2, map[string]interface{}{"name": x.Name, "role": x.Role, "perspective": x.Perspective, "confidence": x.Confidence})
	}
	_ = encodeJSON(w, r2)
}
func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	c := sharedLLM
	if c == nil {
		_ = encodeJSON(w, map[string]string{"error": "Ollama unavailable"})
		return
	}
	tt := thinktank.NewThinkTank("Council", topic)
	orch := thinktank.NewOrchestrator(tt, c)
	_ = orch.RunResearchRound()

	// Auto-generate tasks from findings
	ff := make([]map[string]interface{}, 0, 8)
	for _, f := range tt.ResearchFindings {
		ff = append(ff, map[string]interface{}{
			"fellow": f.FellowName, "role": f.Role,
			"insights": f.KeyInsights, "confidence": f.ConfidenceScore,
		})

		// Create tasks from high-confidence insights
		if f.ConfidenceScore >= 0.6 && len(f.KeyInsights) > 0 {
			for idx, insight := range f.KeyInsights[:min(2, len(f.KeyInsights))] {
				priority := "medium"
				if f.ConfidenceScore >= 0.8 {
					priority = "high"
				}
				task := dashboard.Task{
					ID:          fmt.Sprintf("tt-%d-%d", time.Now().UnixNano(), idx),
					Title:       f.FellowName + ": " + insight,
					Description: strings.Join(f.KeyInsights, "\n"),
					Priority:    priority,
					Assignee:    f.FellowName,
					Source:      "thinktank",
					SourceID:    f.FellowName,
					Sprint:      companyState.CurrentSprint,
					StoryPoints: int(f.ConfidenceScore * 10),
				}
				task.TreeID = dashboard.PickTreeForTask(task)
				_ = taskStore.Create(task)
			}
		}
	}
	_ = encodeJSON(w, map[string]interface{}{"topic": topic, "findings": ff})
}
func handleDefaultCompany(w http.ResponseWriter, _ *http.Request) {
	_ = encodeJSON(w, companyState)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	tab := r.URL.Query().Get("tab")
	if msg == "" {
		msg = "Hello"
	}

	agents := map[string]string{
		"overview":  "BT Studio admin agent. 38 trees, 26 MCP tools, 7 categories. Help the user navigate and manage.",
		"thinktank": "ThinkTank moderator. 5 fellows: Bull, Bear, Technical, Macro, Contrarian. Help with analyses.",
		"company":   "Startup strategy agent. BT Studio Inc, pre-seed. Help with company decisions.",
		"tasks":     "PM agent. 6 tasks across 3 sprints. Help prioritize, approve, and execute.",
		"trees":     "Tree architect. 38 trees. Help create, evolve, and manage behavior trees.",
		"mindmap":   "Tree visualization agent. SVG mind maps. Help navigate tree structures.",
		"evolution": "Evolution optimizer. Stockfish+genetic+Q-learning. Help tune evolution parameters.",
	}
	sys := agents[tab]
	if sys == "" {
		sys = "BT Studio assistant. Help the user administer the behavior tree framework."
	}

	if sharedLLM == nil {
		_ = encodeJSON(w, map[string]string{"reply": "Ollama unavailable. Start the Ollama service."})
		return
	}

	reply, err := sharedLLM.Generate(sys + "\n\nUser: " + msg)
	if err != nil {
		_ = encodeJSON(w, map[string]string{"reply": "Error: " + err.Error()})
		return
	}
	_ = encodeJSON(w, map[string]string{"reply": reply, "tab": tab})
}

func handleTasks(w http.ResponseWriter, _ *http.Request) {
	tasks := taskStore.List()
	// Convert to []map for frontend compatibility
	out := make([]map[string]interface{}, len(tasks))
	for i, t := range tasks {
		out[i] = map[string]interface{}{
			"id": t.ID, "title": t.Title, "description": t.Description,
			"priority": t.Priority, "role": t.Assignee, "sprint": t.Sprint,
			"sp": t.StoryPoints, "status": t.Status, "source": t.Source,
			"tree_id": t.TreeID, "output": t.Output, "outcome": t.Outcome,
		}
	}
	_ = encodeJSON(w, out)
}

func handleTaskApprove(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	if err := taskStore.Approve(taskID, "dashboard"); err != nil {
		w.WriteHeader(404)
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]string{"status": "approved", "id": taskID}
	if hitl.DefaultStore != nil {
		resolvedFrom := "pending"
		if pending, ok := hitl.DefaultStore.FindPendingByTaskID(taskID); ok && pending.Status == hitl.StatusEscalated {
			resolvedFrom = "escalated"
		}
		if req, err := hitl.DefaultStore.ApproveByTaskID(taskID, "dashboard", "task approved via dashboard"); err == nil {
			resp["hitl_request_id"] = req.ID
			resp["hitl_status"] = string(req.Status)
			resp["hitl_resolved_from"] = resolvedFrom
		} else {
			resp["hitl_note"] = err.Error()
		}
	}
	if err := encodeJSON(w, resp); err != nil {
		return
	}
}

func handleTaskReject(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "task rejected via dashboard"
	}
	if err := taskStore.Reject(taskID, "dashboard", reason); err != nil {
		w.WriteHeader(404)
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]string{"status": "rejected", "id": taskID}
	if hitl.DefaultStore != nil {
		resolvedFrom := "pending"
		if pending, ok := hitl.DefaultStore.FindPendingByTaskID(taskID); ok && pending.Status == hitl.StatusEscalated {
			resolvedFrom = "escalated"
		}
		if req, err := hitl.DefaultStore.RejectByTaskID(taskID, "dashboard", "task rejected via dashboard"); err == nil {
			resp["hitl_request_id"] = req.ID
			resp["hitl_status"] = string(req.Status)
			resp["hitl_resolved_from"] = resolvedFrom
		} else {
			resp["hitl_note"] = err.Error()
		}
	}
	if err := encodeJSON(w, resp); err != nil {
		return
	}
}
func handleSprintExecute(w http.ResponseWriter, _ *http.Request) {
	// Approved() returns tasks ordered by priority then sprint, so the
	// sequential dispatch loop below already runs critical/high-priority
	// tasks before lower-priority ones regardless of creation order.
	approved := taskStore.Approved()
	if len(approved) == 0 {
		_ = encodeJSON(w, map[string]string{"status": "no_approved_tasks"})
		return
	}

	jobID := fmt.Sprintf("sprint-%d", time.Now().UnixNano())
	sprintState.Lock()
	sprintState.Running = true
	sprintState.JobID = jobID
	sprintState.StartedAt = time.Now()
	sprintState.TasksTotal = len(approved)
	sprintState.TasksCompleted = 0
	sprintState.Progress = "dispatching"
	sprintState.Unlock()

	_ = encodeJSON(w, map[string]interface{}{
		"status": "sprint_started", "job_id": jobID,
		"message": fmt.Sprintf("Dispatching %d tasks to BT agents", len(approved)),
		"count":   len(approved),
	})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sprint panic", "error", r)
			}
			sprintState.Lock()
			sprintState.Running = false
			sprintState.Progress = "done"
			sprintState.Unlock()
		}()

		executor := newAgentExecutor()

		for i, task := range approved {
			sprintState.Lock()
			sprintState.TasksCompleted = i
			sprintState.CurrentTask = task.Title
			sprintState.Progress = "running"
			sprintState.Unlock()

			// Mark as in_progress
			_ = taskStore.UpdateStatus(task.ID, "in_progress")

			// Pick tree if not set
			treeID := task.TreeID
			if treeID == "" {
				treeID = dashboard.PickTreeForTask(task)
			}

			// Resolve agent name
			agentName := dashboard.ResolveAgentName(task.Assignee)
			taskDesc := task.Title
			if task.Description != "" {
				taskDesc = task.Description
			}

			slog.Info("sprint: running task", "task", task.ID, "agent", agentName, "tree", treeID)

			output, outcome, err := executor.RunTask(agentName, taskDesc, treeID)

			if err != nil && outcome == "timeout" {
				_ = taskStore.UpdateStatus(task.ID, "failed")
				_ = taskStore.SetOutput(task.ID, "timeout: "+err.Error(), "timeout")
			} else if outcome == "failed" || err != nil {
				_ = taskStore.UpdateStatus(task.ID, "failed")
				_ = taskStore.SetOutput(task.ID, output, "failed")
			} else {
				_ = taskStore.UpdateStatus(task.ID, "completed")
				_ = taskStore.SetOutput(task.ID, output, outcome)
			}
		}

		sprintState.Lock()
		sprintState.TasksCompleted = len(approved)
		sprintState.Unlock()
	}()
}

func handleSprintStatus(w http.ResponseWriter, _ *http.Request) {
	sprintState.Lock()
	defer sprintState.Unlock()
	tasks := taskStore.List()
	completed := 0
	for _, t := range tasks {
		if t.Status == "completed" {
			completed++
		}
	}
	_ = encodeJSON(w, map[string]interface{}{
		"running": sprintState.Running, "job_id": sprintState.JobID,
		"elapsed":         time.Since(sprintState.StartedAt).Seconds(),
		"tasks_completed": completed, "tasks_total": len(tasks),
		"current_task": sprintState.CurrentTask,
	})
}

func handleTreeStructure(w http.ResponseWriter, r *http.Request) {
	treeID := r.URL.Query().Get("id")

	// Strip category prefix (e.g., "domain:code_review" -> "code_review")
	if idx := strings.LastIndex(treeID, ":"); idx >= 0 {
		treeID = treeID[idx+1:]
	}

	// ── Domain trees (14) ──
	domainTrees := domains.AllDomainTrees()
	if tree, ok := domainTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── Finance trees (10) ──
	financeTrees := map[string]*evolution.SerializableNode{
		"pitch_agent":        evolution.PitchAgentTree(),
		"earnings_reviewer":  evolution.EarningsReviewerTree(),
		"market_researcher":  evolution.MarketResearcherTree(),
		"model_builder":      evolution.ModelBuilderTree(),
		"meeting_prep":       evolution.MeetingPrepTree(),
		"valuation_reviewer": evolution.ValuationReviewerTree(),
		"gl_reconciler":      evolution.GLReconcilerTree(),
		"month_end_closer":   evolution.MonthEndCloserTree(),
		"statement_auditor":  evolution.StatementAuditorTree(),
		"kyc_screener":       evolution.KYCScreenerTree(),
	}
	if tree, ok := financeTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── Startup trees (6) ──
	startupTrees := map[string]*evolution.SerializableNode{
		"ceo":       startup.CEOTree(),
		"cto":       startup.CTOTree(),
		"pm":        startup.PMTree(),
		"engineer":  startup.EngineerTree(),
		"marketing": startup.MarketingTree(),
		"sales":     startup.SalesTree(),
	}
	if tree, ok := startupTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── Research trees (2) ──
	researchTrees := map[string]*evolution.SerializableNode{
		"deep_research":  evolution.DeepResearchTree(),
		"quick_research": evolution.QuickResearchTree(),
	}
	if tree, ok := researchTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── ThinkTank trees (3 static + FellowResearch/Debate parameterized) ──
	thinktankTrees := map[string]*evolution.SerializableNode{
		"synthesis":   thinktank.SynthesisTree(),
		"peer_review": thinktank.PeerReviewTree(),
		"report":      thinktank.ReportGenerationTree(),
	}
	if tree, ok := thinktankTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── Evolution / core trees (2) ──
	evolutionTrees := map[string]*evolution.SerializableNode{
		"godev":   evolution.GoDeveloperTree(),
		"default": evolution.DefaultTree(),
	}
	if tree, ok := evolutionTrees[treeID]; ok {
		_ = encodeJSON(w, tree)
		return
	}

	// ── Fallback: simplified node for trees without SerializableNode ──
	for _, t := range kg.Trees {
		name := t.ID
		if idx := strings.LastIndex(name, ":"); idx >= 0 {
			name = name[idx+1:]
		}
		if name == treeID {
			_ = encodeJSON(w, map[string]interface{}{
				"id": t.ID, "name": t.Name, "type": "Sequence", "node_type": "Sequence",
				"node_count": t.NodeCount,
				"children":   []map[string]interface{}{},
			})
			return
		}
	}

	http.Error(w, `{"error":"tree not found"}`, http.StatusNotFound)
}

// --- Security & Health ---

// authMiddleware wraps a handler with optional API key authentication.
// If apiKey is empty, all requests pass through (no auth required).
// If apiKey is set, requests must include X-API-Key header matching the key.// handleHealth returns platform health status.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	_ = encodeJSON(w, map[string]interface{}{
		"status":   "ok",
		"version":  "1.0.0",
		"uptime":   "operational",
		"packages": 19,
		"trees":    38,
	})
}

// ─── Session Management Handlers ──────────────────────────────────────────────

// handleLogin authenticates a user via password and creates a session.
// POST /api/login — body: {"password": "<api_key>"}
// The password must match BT_API_KEY env var. On success, sets a session cookie.
// Public endpoint (no auth required — this is how you get a session).
//
// Brute-force protection: after 20 failed attempts from the same IP, the IP
// is locked out for 30 minutes. Cooldown periods apply before lockout with
// exponential backoff (1s → 5s → 30s → 2m → 10m).
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = encodeJSON(w, map[string]string{"error": "method not allowed — use POST"})
		return
	}

	apiKey := os.Getenv("BT_API_KEY")
	if apiKey == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = encodeJSON(w, map[string]string{"error": "login not configured — BT_API_KEY not set"})
		return
	}

	// Check brute-force throttle before processing
	if loginThrottle.IsBlocked(r.RemoteAddr) {
		remaining := loginThrottle.RemainingCooldown(r.RemoteAddr)
		security.AuditSecurityEvent(r.Context(), "login_throttled",
			"reason", "ip_blocked",
			"remote_addr", r.RemoteAddr,
			"remaining", remaining.String(),
		)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", remaining.Seconds()))
		w.WriteHeader(http.StatusTooManyRequests)
		_ = encodeJSON(w, map[string]string{
			"error":    "too many failed login attempts — IP temporarily blocked",
			"retry_in": remaining.String(),
		})
		return
	}

	// Apply cooldown delay if the IP has recent failures
	remaining := loginThrottle.RemainingCooldown(r.RemoteAddr)
	if remaining > 0 {
		time.Sleep(remaining)
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	if body.Password != apiKey {
		loginThrottle.RecordFailure(r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = encodeJSON(w, map[string]string{"error": "invalid password"})
		security.AuditSecurityEvent(r.Context(), "login_failed",
			"reason", "invalid_password",
			"remote_addr", r.RemoteAddr,
			"failed_attempts", loginThrottle.State(r.RemoteAddr).FailedCount,
		)
		return
	}

	// Successful login — clear throttle state
	loginThrottle.RecordSuccess(r.RemoteAddr)

	token, err := sessionStore.CreateSession("")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = encodeJSON(w, map[string]string{"error": "failed to create session: " + err.Error()})
		return
	}

	sessionStore.SetSessionCookie(w, token)
	security.AuditSecurityEvent(r.Context(), "login_success",
		"session_count", sessionStore.Count(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = encodeJSON(w, map[string]interface{}{
		"status":  "authenticated",
		"message": "Session created. Include the session cookie in subsequent requests.",
	})
}

// handleLogout destroys the current session and clears the session cookie.
// POST /api/logout — requires valid session cookie or API key header.
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = encodeJSON(w, map[string]string{"error": "method not allowed — use POST"})
		return
	}

	// Extract and destroy session from cookie
	if cookie, err := r.Cookie("bt_session"); err == nil {
		sessionStore.DestroySession(cookie.Value)
	}
	sessionStore.ClearSessionCookie(w)

	security.AuditSecurityEvent(r.Context(), "logout",
		"session_count", sessionStore.Count(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = encodeJSON(w, map[string]string{
		"status":  "logged_out",
		"message": "Session destroyed and cookie cleared.",
	})
}

// handleSession returns information about the current session.
// GET /api/session — requires valid session cookie or API key header.
func handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = encodeJSON(w, map[string]string{"error": "method not allowed — use GET"})
		return
	}

	// Check for session cookie
	if cookie, err := r.Cookie("bt_session"); err == nil {
		if info := sessionStore.SessionInfo(cookie.Value); info != nil {
			w.Header().Set("Content-Type", "application/json")
			_ = encodeJSON(w, map[string]interface{}{
				"status":      "authenticated",
				"auth_method": "session",
				"created_at":  info.CreatedAt,
				"expires_at":  info.ExpiresAt,
				"last_used":   info.LastUsed,
				"remaining":   info.Remaining.String(),
			})
			return
		}
	}

	// Check for API key header
	apiKey := os.Getenv("BT_API_KEY")
	if apiKey != "" && r.Header.Get("X-API-Key") == apiKey {
		w.Header().Set("Content-Type", "application/json")
		_ = encodeJSON(w, map[string]interface{}{
			"status":      "authenticated",
			"auth_method": "api_key",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = encodeJSON(w, map[string]string{
		"status":  "unauthenticated",
		"message": "No valid session cookie or API key found.",
	})
}

// handleAlerts evaluates prometheus alert rules against current metrics and
// returns which alerts are firing. Public endpoint (no auth) so monitoring
// tools can scrape it.
func handleAlerts(w http.ResponseWriter, _ *http.Request) {
	metricsJSON := dashboard.MetricsJSON()
	b, err := json.Marshal(metricsJSON)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	report, err := agent.EvaluateFromJSON(b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, report)
}

// ─── Dead Letter Queue Handlers ────────────────────────────────────────────────

// handleDLQ lists all entries in the dead letter queue.
func handleDLQ(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// The executor process mutates the shared file as it replays; reload so
	// the panel lists the current queue, not this process's boot-time view.
	dlq.Reload()
	entries := dlq.List()
	resp := map[string]interface{}{
		"count":   len(entries),
		"entries": entries,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, resp)
}

// handleDLQReplay flags a dead-lettered entry for retry so bt-agent's executor
// requeues it. The dashboard runs in a separate process and has no tree runner,
// so it cannot execute the task itself. Calling dlq.Replay here would REMOVE the
// entry and persist the removal — a cross-process silent drop, since bt-agent
// would never see it again. Instead we reload the queue from disk (to pick up any
// concurrent changes from the executor) and mark the entry via Requeue, which
// stamps RequeuedAt without removing it, leaving it on disk for the executor.
func handleDLQReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing id parameter"})
		return
	}

	// Reload from disk so we requeue against the executor's latest view rather
	// than a stale in-memory copy that could clobber concurrent changes.
	dlq.Reload()

	entry, ok := dlq.Requeue(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = encodeJSON(w, map[string]string{"error": "entry not found", "id": id})
		return
	}

	resp := map[string]interface{}{
		"status":  "requeued",
		"entry":   entry,
		"pending": dlq.Len(),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, resp)
}

// handleDLQPurge removes all entries from the dead letter queue.
func handleDLQPurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	count := dlq.Len()
	dlq.Purge()
	resp := map[string]interface{}{
		"status":  "purged",
		"removed": count,
		"pending": 0,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, resp)
}

// handleOpenAPI serves the OpenAPI 3.0 specification for the dashboard API.
// This endpoint is public (no auth) so API consumers can discover the schema.
func handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	gen := api.NewOpenAPIGenerator(
		"BT Platform API",
		"1.0.0",
		"Dashboard REST API for the Go Behavior Tree Platform. "+
			"Manages behavior trees, thinktank analysis, company simulation, "+
			"task pipelines, sprint execution, and dashboard chat. "+
			"All /api/* endpoints except /api/health, /api/metrics, /api/alerts, /api/alerts/rules, "+
			"and /api/openapi.json require an X-API-Key header when BT_API_KEY is configured.",
	)
	gen.AddServer("http://localhost:9800", "Local development server")
	gen.AddServer("http://100.123.73.66:9800", "Tailscale production server")

	gen.AddTag("System", "Health, metrics, and alerts")
	gen.AddTag("Platform", "Platform overview and tree management")
	gen.AddTag("Trees", "Behavior tree listing and structure")
	gen.AddTag("Thinktank", "Analytical thinktank with 5 fellows")
	gen.AddTag("Company", "Startup company state")
	gen.AddTag("Tasks", "Task pipeline management")
	gen.AddTag("Sprint", "Sprint execution")
	gen.AddTag("Chat", "Dashboard AI chat")
	gen.AddTag("Agents", "Agent management and execution")
	gen.AddTag("Scalability", "Horizontal scaling, worker pool, queues")
	gen.AddTag("Reliability", "Dead letter queue, circuit breaker")
	gen.AddTag("Session", "Login, logout, session management")
	gen.AddTag("DoorMate", "Page-First AI Assistant endpoints")

	for _, route := range api.DashboardRoutes() {
		gen.AddRoute(route)
	}

	data, err := gen.GenerateJSON()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(data)
}

// swaggerUIHTML is a self-contained Swagger UI page that loads the OpenAPI spec
// from /api/openapi.json. Uses CDN-hosted Swagger UI (no local deps).
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>BT Platform API — Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin:0; background: #0f172a; }
    .swagger-ui .topbar { background-color: #1e293b; }
    .swagger-ui .topbar .download-url-wrapper .select-label { color: #e2e8f0; }
    .swagger-ui .info .title { color: #f1f5f9; }
    .swagger-ui .scheme-container { background: #1e293b; box-shadow: 0 1px 2px 0 rgba(0,0,0,.15); }
    #swagger-ui { max-width: 1200px; margin: 0 auto; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js" crossorigin></script>
  <script>
    window.onload = function() {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.json",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        plugins: [SwaggerUIBundle.plugins.DownloadUrl],
        layout: "StandaloneLayout",
        defaultModelsExpandDepth: 1,
        defaultModelExpandDepth: 1,
        docExpansion: "list",
        filter: true,
        showExtensions: true,
        showCommonExtensions: true,
        syntaxHighlight: { theme: "monokai" }
      });
    };
  </script>
</body>
</html>`

// handleSwagger serves a Swagger UI page that renders the OpenAPI spec
// from /api/openapi.json. Public endpoint — no auth required (same as
// /api/health, /api/metrics, /api/alerts, /api/openapi.json).
func handleSwagger(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

// handleAlertRules serves the raw Prometheus alert rules YAML file so
// Prometheus or other monitoring tools can scrape it directly.
// Public endpoint (no auth) — same as /api/alerts, /api/health, /api/dashboard.
func handleAlertRules(w http.ResponseWriter, _ *http.Request) {
	// Look relative to the binary's working directory (repo root)
	rulesPath := "monitoring/prometheus-alerts.yml"

	// Fallback: if running from outside the repo, try absolute path
	if _, err := os.Stat(rulesPath); os.IsNotExist(err) {
		rulesPath = "/home/nico/go-bt-evolve/monitoring/prometheus-alerts.yml"
	}

	data, err := os.ReadFile(rulesPath)
	if err != nil {
		http.Error(w, "alert rules file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(data)
}

func handleSecurityAudit(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	buf := security.GlobalAuditBuffer()
	if buf == nil {
		_ = encodeJSON(w, map[string]any{
			"capacity":        0,
			"total_events":    0,
			"captured_events": 0,
			"buffer_enabled":  false,
			"event_counts":    map[string]int{},
			"events":          []security.AuditEvent{},
		})
		return
	}
	events := buf.Recent(200)
	_ = encodeJSON(w, security.AuditBufferJSON{
		Capacity:       buf.Capacity(),
		TotalEvents:    buf.Count(),
		CapturedEvents: len(events),
		Events:         events,
		EventCounts:    security.CountEvents().EventCounts,
		BufferEnabled:  true,
	})
}

// handleScalability returns a JSON snapshot of scalability components:
// worker pool, concurrency limiter, queue depth, and agent router health.
// Public endpoint (no auth) — same as /api/health, /api/metrics, /api/alerts.
func handleScalability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Create a snapshot from the wired scalability components. WorkerPool,
	// ConcurrencyLimiter, TaskQueue, and AgentRouter are all initialized at
	// dashboard startup; read their live state instead of the former placeholders.
	var (
		queuePending, queueMax     int
		routerTotal, routerHealthy int
		routerFailures             int
		heartbeat                  *reliability.HeartbeatStats
	)
	if dashTaskQueue != nil {
		queuePending = dashTaskQueue.Len()
	}
	if dashAgentRouter != nil {
		routerTotal = len(dashAgentRouter.Executors())
		routerHealthy = len(dashAgentRouter.HealthyExecutors())
		routerFailures = dashAgentRouter.ConsecutiveFailures()
		heartbeat = dashAgentRouter.HeartbeatStats()
	}

	status := reliability.NewScalabilityStatus(
		dashWorkerPool,         // worker pool (4 workers, active/queued counts from running tasks)
		dashConcurrencyLimiter, // concurrency limiter (2 max concurrent LLM executions)
		queuePending,           // queue pending (injected TaskQueue depth)
		queueMax,               // queue max len (unbounded)
		routerTotal,            // router total (injected AgentRouter executor count)
		routerHealthy,          // router healthy
		nil,                    // connection pool (managed by RemoteExecutor)
		routerFailures,         // router failures (executors with consecutive failures)
		heartbeat,              // heartbeat stats (nil unless a tracker is configured)
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = encodeJSON(w, status)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	sanitized := dashConfig.Sanitized()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = encodeJSON(w, sanitized)
}

// handleTaskCreate creates a new task via query params (GET — avoids CSRF on API endpoints).
func handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		w.WriteHeader(400)
		_ = encodeJSON(w, map[string]string{"error": "missing title parameter"})
		return
	}
	task := dashboard.Task{
		ID:          fmt.Sprintf("task-%d", time.Now().UnixNano()),
		Title:       title,
		Description: r.URL.Query().Get("desc"),
		Priority:    r.URL.Query().Get("priority"),
		Assignee:    r.URL.Query().Get("assignee"),
		Source:      "manual",
	}
	if task.Priority == "" {
		task.Priority = "medium"
	}
	if task.Assignee == "" {
		task.Assignee = "bt-implementer"
	}
	task.TreeID = dashboard.PickTreeForTask(task)
	if err := taskStore.Create(task); err != nil {
		w.WriteHeader(500)
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	_ = encodeJSON(w, map[string]interface{}{"status": "created", "id": task.ID})
}

// ─── Agent Handlers ──────────────────────────────────────────────────────

// handleAgentExecute handles POST /api/agents/execute — the server-side
// counterpart to RemoteExecutor for horizontal scaling. Accepts JSON body
// {agent, task, tree?} and returns a reliability.AgentResult.
// Execution is submitted through the dashboard WorkerPool with
// ConcurrencyLimiter gating to prevent LLM resource exhaustion.
func handleAgentExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Agent string `json:"agent"`
		Task  string `json:"task"`
		Tree  string `json:"tree"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	if req.Agent == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required field: agent"})
		return
	}
	if req.Task == "" {
		req.Task = dashboard.DefaultAgentTask(req.Agent)
	}

	treeID := req.Tree

	// Acquire concurrency slot before submitting to worker pool.
	// The limiter prevents more than 2 simultaneous LLM-bound agent
	// executions, avoiding Ollama queue overflows on this Jetson.
	if dashConcurrencyLimiter != nil {
		dashConcurrencyLimiter.Acquire()
	}
	releaseLimiter := func() {
		if dashConcurrencyLimiter != nil {
			dashConcurrencyLimiter.Release()
		}
	}

	result := make(chan reliability.AgentResult, 1)
	if dashWorkerPool != nil {
		dashWorkerPool.Submit(func() {
			defer releaseLimiter()
			start := time.Now()
			executor := newAgentExecutor()
			output, outcome, err := executor.RunTask(req.Agent, req.Task, treeID)
			elapsed := time.Since(start)

			res := reliability.AgentResult{
				Agent:    req.Agent,
				Task:     req.Task,
				Output:   output,
				Duration: elapsed,
				Success:  outcome == "success" || outcome == "completed",
			}
			if outcome == "failed" || outcome == "timeout" {
				res.Success = false
			}
			if err != nil {
				res.Error = err.Error()
			}
			result <- res
		})
	} else {
		// Fallback: execute synchronously (no worker pool configured)
		start := time.Now()
		executor := newAgentExecutor()
		output, outcome, err := executor.RunTask(req.Agent, req.Task, treeID)
		elapsed := time.Since(start)
		releaseLimiter()

		res := reliability.AgentResult{
			Agent:    req.Agent,
			Task:     req.Task,
			Output:   output,
			Duration: elapsed,
			Success:  outcome == "success" || outcome == "completed",
		}
		if outcome == "failed" || outcome == "timeout" {
			res.Success = false
		}
		if err != nil {
			res.Error = err.Error()
		}
		result <- res
	}

	res := <-result
	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, res)
}

// handleAgentsList returns all registered BT agents with their live status and circuit breaker info.
func handleAgentsList(w http.ResponseWriter, _ *http.Request) {
	agents := dashboard.ListAgentsWithCB()
	if agents == nil {
		agents = []dashboard.AgentWithStatus{}
	}
	_ = encodeJSON(w, agents)
}

// handleAgentRun runs an agent with a given task.
func handleAgentRun(w http.ResponseWriter, r *http.Request) {
	agentName := r.URL.Query().Get("agent")
	task := r.URL.Query().Get("task")
	if agentName == "" {
		w.WriteHeader(400)
		_ = encodeJSON(w, map[string]string{"error": "missing agent parameter"})
		return
	}
	if task == "" {
		task = dashboard.DefaultAgentTask(agentName)
	}

	treeID := r.URL.Query().Get("tree")

	// Acquire concurrency slot to prevent LLM resource exhaustion.
	if dashConcurrencyLimiter != nil {
		dashConcurrencyLimiter.Acquire()
	}

	result := make(chan map[string]interface{}, 1)
	if dashWorkerPool != nil {
		dashWorkerPool.Submit(func() {
			defer func() {
				if dashConcurrencyLimiter != nil {
					dashConcurrencyLimiter.Release()
				}
			}()
			executor := newAgentExecutor()
			res, err := executor.RunTaskResult(agentName, task, treeID)
			resp := map[string]interface{}{
				"agent": agentName, "outcome": "failure", "output": "",
			}
			if res != nil {
				resp["outcome"] = res.Outcome
				resp["output"] = res.Output
				if res.RunID != "" {
					resp["run_id"] = res.RunID
				}
				if res.SessionID != "" {
					resp["session_id"] = res.SessionID
				}
			}
			if err != nil {
				resp["error"] = err.Error()
			}
			result <- resp
		})
	} else {
		executor := newAgentExecutor()
		res, err := executor.RunTaskResult(agentName, task, treeID)
		if dashConcurrencyLimiter != nil {
			dashConcurrencyLimiter.Release()
		}
		resp := map[string]interface{}{
			"agent": agentName, "outcome": "failure", "output": "",
		}
		if res != nil {
			resp["outcome"] = res.Outcome
			resp["output"] = res.Output
			if res.RunID != "" {
				resp["run_id"] = res.RunID
			}
			if res.SessionID != "" {
				resp["session_id"] = res.SessionID
			}
		}
		if err != nil {
			resp["error"] = err.Error()
		}
		result <- resp
	}

	res := <-result
	_ = encodeJSON(w, res)
}

// handleAgentCreate handles POST /api/agents/create — creates a new agent in the registry.
func handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = encodeJSON(w, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Tree        string `json:"tree"`
		Schedule    string `json:"schedule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required field: name"})
		return
	}
	if req.Tree == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required field: tree"})
		return
	}

	cfg := dashboard.AgentYAMLConfig{
		Name:        req.Name,
		Description: req.Description,
		Tree:        req.Tree,
		Schedule:    req.Schedule,
	}
	if err := dashboard.CreateAgent(cfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Return the created agent info
	info := dashboard.AgentWithStatus{
		AgentInfo: dashboard.AgentInfo{
			Name:        cfg.Name,
			Description: cfg.Description,
			Tree:        cfg.Tree,
			Status:      "created",
			Schedule:    cfg.Schedule,
		},
		CBStatus: "unknown",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = encodeJSON(w, info)
}

// handleAgentDelete handles POST /api/agents/delete — deletes an agent YAML template.
func handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = encodeJSON(w, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return
	}

	if req.Name == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = encodeJSON(w, map[string]string{"error": "missing required field: name"})
		return
	}

	if err := dashboard.DeleteAgent(req.Name); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = encodeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = encodeJSON(w, map[string]string{"status": "deleted", "name": req.Name})
}
