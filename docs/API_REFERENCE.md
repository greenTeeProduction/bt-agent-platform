# Go BT Platform — API Reference

> Auto-generated from Go source and maintained as the canonical API reference.
> Last updated: 2026-06-02 · Version: 0.2.0 · Go module: `github.com/nico/go-bt-evolve`

## Packages at a Glance (35 packages + 3 utility scripts)

| Package | Lines | Coverage | Purpose |
|---|---|---|---|
| Package | Lines | Coverage | Purpose |
|---|---|---|---|---|
| [`engine`](#package-engine) | 7,082 | 90% | Core BT runtime, Blackboard, BuildTree, RunTask, 10 chain types |
| [`evolution`](#package-evolution) | 14,000+ | 82% | 46 trees, 7 algorithm engines, HasClearTask, mutation operators |
| [`agent`](#package-agent) | 1,400+ | 77% | Agent definitions (YAML), registry, scheduler, history, catalog |
| [`reliability`](#package-reliability) | 2,000+ | 87% | Circuit breaker, backoff, DLQ, worker pool, queues, executor |
| [`mcp`](#package-mcp) | 500+ | 88% | JSON-RPC 2.0 stdio MCP server, concurrent dispatch |
| [`security`](#package-security) | 580+ | 92% | Rate limiter, sanitization, IP filter, audit, request IDs |
| [`config`](#package-config) | 540+ | 94% | Env-based config, JSON file support, hot-reload watcher |
| [`metrics`](#package-metrics) | 200+ | 92% | Prometheus Counter/Gauge/Histogram, middleware |
| [`tracing`](#package-tracing) | 250+ | 90% | OpenTelemetry-ready Tracer/Span, console exporter |
| [`benchmark`](#package-benchmark) | 1,500+ | 85% | A/B test suite, ScoreMutation, BFCL/SWE-bench/τ-bench/ToolBench/BTPG |
| [`api`](#package-api) | 800+ | 94% | OpenAPI 3.0 generator, JSON schema I/O, content types |
| [`benchreg`](#package-benchreg) | 200+ | 96% | Benchmark regression detection, baseline comparison |
| [`cicd`](#package-cicd) | 350+ | 90% | CI/CD workflow validation, ci-doctor, runner status |
| [`dashboard`](#package-internal-dashboard) | 500+ | — | Dashboard API handlers, agent management UI backend |
| [`factory`](#package-factory) | 600+ | 84% | Skill→BT compiler (analyzer + generator) |
| [`knowledge`](#package-knowledge) | 500+ | 89% | Tree knowledge graph, discovery, auto-creation, factory |
| [`domains`](#package-domains) | 800+ | 100% | 10 domain trees (code review, DevOps, security, etc.) |
| [`finance`](#package-finance) | 2,000+ | 100% | 10 Anthropic finance trees (pitch, DCF, LBO, KYC, etc.) |
| [`thinktank`](#package-thinktank) | 1,200+ | 80% | 5-fellow dialectic analysis, 5-phase pipeline |
| [`startup`](#package-startup) | 1,000+ | 91% | Startup company simulation (sprint/quarter/year) |
| [`evaluator`](#package-evaluator) | 1,200+ | 94% | Stockfish-style tree evaluator, multi-dim fitness, TT, move ordering |
| [`gardener`](#package-gardener) | 900+ | 81% | 24/7 evolution daemon, 25 trees, 5-minute cycles, MetricsTracker |
| [`goap`](#package-goap) | 600+ | 89% | GOAP planner, DocPlanner, BlackboardBridge, world-state modeling |
| [`langagent`](#package-langagent) | 400+ | 93% | LangChain ReAct agent wrapping BT tools |
| [`llm`](#package-llm) | 300+ | 56% | LLM client interface, Ollama client, mock for testing |
| [`monitoring`](#package-monitoring) | 200+ | 96% | Prometheus alert rules, Alert evaluator, MetricsJSON parser |
| [`reflection`](#package-reflection) | 200+ | 75% | Reflection store, Record persistence, JSON file I/O |
| [`research`](#package-research) | 600+ | 100% | Deep and quick research trees, gap analysis |
| [`tools`](#package-tools) | 350+ | 90% | Built-in tool implementations for BT agent chains |
| [`a2a`](#package-a2a) | 600+ | 42% | Agent-to-Agent protocol, agent cards, inter-agent delegation |
| [`eval`](#package-eval) | 300+ | 89% | Platform evaluation runner, use case suites, maturity scorecard |
| [`validate`](#package-validate) | 500+ | 100% | Agent validation suites, composite scoring, 5 test kinds |
| [`workflow`](#package-workflow) | 400+ | 93% | Multi-agent orchestration, sequential/parallel/conditional/loop |
| [`log`](#package-log) | 150+ | 58% | Structured logging with file rotation (10MB, 5 backups) |
| [`util`](#package-util) | 100+ | 100% | Shared utility functions, string conversions |
| [`persistence`](#package-persistence) | 100+ | — | Generic persistence primitives for BT state |

---

## Package: engine

`github.com/nico/go-bt-evolve/internal/engine`

Core behavior tree execution engine. Builds trees from serializable definitions,
runs them with multi-tick support, and provides 10 chain types for LLM integration.

### Types

#### Blackboard
Shared state passed through the behavior tree. Every node reads and writes to it.

```go
type Blackboard struct {
    Task         string           // the task to execute
    Complexity   string           // "low" | "medium" | "high" | "critical"
    Plan         string           // execution plan (populated by AnalyzeTask)
    Result       string           // final output text
    Outcome      string           // "success" | "failure" | "partial"
    DurationMs   int64            // execution time in milliseconds
    KgResults    string           // knowledge graph results
    CachedResult string           // last chain action output (for tool pipelines)
    FailureCount int              // consecutive failures for retry logic
    Reflections  *reflection.Store // reflection persistence
    TreeStore    *evolution.TreeStore // tree state persistence
    LLM          llm.LLM          // LLM client (interface)

    // Langchain integration
    ChainMemory  any              // conversation memory
    ChainTools   []any            // tools available to chains
    ChainState   map[string]any   // arbitrary chain execution state
    Results      []string         // accumulated results from all chain actions
    QualityScore float64          // 0.0-1.0 output quality score
}
```

#### SerializableNode
Serializable tree node definition (from `evolution` package, used by engine).

```go
type SerializableNode struct {
    Type     string            // "Sequence" | "Selector" | "Condition" | "Action" | "ChainAction" | "Retry"
    Name     string            // node name (e.g., "IsGoRelated", "ExecutePlan", "chain_type:prompt")
    Children []SerializableNode
    Metadata map[string]any    // chain config: max_tokens, json_schema, tools, params
}
```

#### ActionFunc / ConditionFunc
```go
type ActionFunc = func(ctx *btcore.BTContext[Blackboard]) int
type ConditionFunc = func(b *Blackboard) bool
```

### Functions

```go
// BuildTree constructs a go-bt Command from a SerializableNode tree definition.
func BuildTree(serTree *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard]

// RunTask executes a task through the behavior tree to completion.
// Loops tree.Run() until terminal state (Success=1 or Failure=-1),
// supporting multi-tick decorators (Repeat) with a 1000-tick safety limit.
func RunTask(bb *Blackboard, tree btcore.Command[Blackboard]) string

// RegisterAction adds an action handler to the global registry.
func RegisterAction(name string, fn ActionFunc)

// RegisterCondition adds a condition handler to the global registry.
func RegisterCondition(name string, fn ConditionFunc)

// GetAction returns the action handler for a name.
func GetAction(name string, bb *Blackboard) ActionFunc

// GetCondition returns the condition handler for a name.
func GetCondition(name string, bb *Blackboard) ConditionFunc

// ValidateTree checks that all nodes reference known handlers.
// Returns an error if any node name is unregistered.
func ValidateTree(tree *evolution.SerializableNode) error
```

### Chain Types (ChainAction nodes)

| Chain Type | Max Suggestion | Description |
|---|---|---|
| `llm_call` | 400 tokens | Single LLM invocation with `{{.Task}}` template |
| `rag_query` | 400 | Retrieval-augmented QA using `bb.KgResults` |
| `tool_call` | 200 | Named tool invocation via LLM reasoning |
| `conversation` | 400 | Multi-turn chat with `bb.ChainMemory` |
| `structured_output` | 400 | JSON output with `json_schema` constraint |
| `retrieval_qa` | 400 | Two-phase retrieve-then-answer |
| `map_reduce` | 400 | Decompose → process subtasks → combine |
| `refine` | 400 | Iterative self-improvement (2 passes) |
| `agent` | 400 | ReAct loop (Thought→Action→Observation→Final Answer) |
| `tool_action` | 200 | Direct tool invocation, no agent overhead |

Template variables: `{{.Task}}`, `{{.Plan}}`, `{{.Result}}`, `{{.Outcome}}`, `{{.Complexity}}`, `{{.CachedResult}}`

---

## Package: evolution

`github.com/nico/go-bt-evolve/internal/evolution`

Tree definitions, mutation operators, and algorithm engines.

### Tree Factories
```go
func DefaultTree() *SerializableNode           // 17-node general-purpose tree
func GoDeveloperTree() *SerializableNode       // 27-node Go-specific tree
func DeepResearchTree() *SerializableNode      // 20-node research pipeline
func QuickResearchTree() *SerializableNode     // 12-node fast research
func HermesSelfEvolutionTree() *SerializableNode // meta-cognitive self-improvement
func MergedTree() *SerializableNode            // 21-path universal router (all domains)
```

### Finance Trees (10)
```go
func PitchAgentTree() *SerializableNode
func EarningsReviewerTree() *SerializableNode
func MarketResearcherTree() *SerializableNode
func ModelBuilderTree() *SerializableNode
func MeetingPrepTree() *SerializableNode
func ValuationReviewerTree() *SerializableNode
func GLReconcilerTree() *SerializableNode
func MonthEndCloserTree() *SerializableNode
func StatementAuditorTree() *SerializableNode
func KYCScreenerTree() *SerializableNode
```

### Algorithm Engines

```go
// ExpertKnowledge: 6 design patterns, 5 anti-patterns, 10 heuristics, 6 archetypes
type ExpertKnowledge struct { ... }
func (ek *ExpertKnowledge) GetRecommendations(tree *SerializableNode) []Recommendation

// Genetic Algorithm: tournament selection, crossover, elitism
type Population struct { ... }
func NewPopulation(size int, trees []*SerializableNode) *Population
func (p *Population) Evolve(generations int) []*SerializableNode

// Q-Learning: epsilon-greedy mutation selection
type QLearner struct { ... }
func NewQLearner(epsilon float64) *QLearner
func (ql *QLearner) SelectMutation(state string) string

// Ensemble Methods: voting, weighted, stacking, boosting
func VotingEnsemble(trees []*SerializableNode, task string) string
func WeightedEnsemble(trees []*SerializableNode, weights []float64, task string) string

// Decision Tree Optimizer: C4.5/CART on Selectors
type DTAnalyzer struct { ... }
func NewDTAnalyzer() *DTAnalyzer
func (d *DTAnalyzer) Entropy(name string) float64
func (d *DTAnalyzer) InformationGain(name, condition string) float64
func (d *DTAnalyzer) GiniImpurity(name string) float64
func (d *DTAnalyzer) AnalyzeTree(tree *SerializableNode, name string) *DTAnalysis

// SelectorOptimizer: IG/Gini/Killer reordering
type SelectorOptimizer struct { ... }
func NewSelectorOptimizer() *SelectorOptimizer

// Memetic Local Search: Hill Climbing, Simulated Annealing, Tabu Search
func HillClimb(tree *SerializableNode, params map[string]float64) *SerializableNode
func SimulatedAnneal(tree *SerializableNode, temp float64, coolingRate float64) *SerializableNode
func TabuSearch(tree *SerializableNode, iterations int, tabuSize int) *SerializableNode
```

### Key Conditions
```go
func ContainsWord(s, word string) bool      // case-insensitive substring match
func ContainsAny(s string, words ...string) bool
func HasClearTask(bb *Blackboard) bool       // task has verb or question pattern
func WasSuccessful(bb *Blackboard) bool      // bb.Outcome == "success"
```

### Key Mutation Operations
```go
func ApplyMutation(tree *SerializableNode, op string) *SerializableNode
// Operations: add_before, wrap_retry, increase_retries, add_fallback,
//             reorder_children, add_condition, remove_node
```

### Tree Store
```go
type TreeStore struct { ... }
func NewTreeStore(path string) *TreeStore
func (ts *TreeStore) Save(tree *SerializableNode) error
func (ts *TreeStore) Load() (*SerializableNode, error)
func (ts *TreeStore) SaveAs(name string, tree *SerializableNode) error
func (ts *TreeStore) LoadNamed(name string) (*SerializableNode, error)
```

---

## Package: agent

`github.com/nico/go-bt-evolve/internal/agent`

Agent lifecycle management: definitions, registry, scheduling, history.

### Types

```go
type Definition struct {
    Name        string            `yaml:"name"`
    Description string            `yaml:"description"`
    Version     string            `yaml:"version"`
    Tree        string            `yaml:"tree"`       // "domain:code_review", "finance:pitch_agent", etc.
    Schedule    string            `yaml:"schedule"`   // cron or "on_demand"
    Inputs      []InputSpec       `yaml:"inputs"`
    Outputs     []OutputSpec      `yaml:"outputs"`
    Quality     *QualitySpec      `yaml:"quality"`
    Metadata    map[string]string `yaml:"metadata"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type RunContext struct {
    AgentName string
    Task      string
    StartTime time.Time
}

type RunRecord struct {
    ID        string    `json:"id"`
    Agent     string    `json:"agent"`
    Task      string    `json:"task"`
    Outcome   string    `json:"outcome"`
    Output    string    `json:"output"`
    Duration  float64   `json:"duration_s"`
    Error     string    `json:"error,omitempty"`
    Timestamp time.Time `json:"timestamp"`
}
```

### Registry
```go
type Registry struct { ... }
func NewRegistry(home string) *Registry
func (r *Registry) Create(def Definition) error
func (r *Registry) Get(name string) (*AgentInstance, error)
func (r *Registry) List() []AgentInstance
func (r *Registry) Delete(name string) error
func (r *Registry) Update(def Definition) error
```

### Catalog
```go
type Catalog struct { ... }
func NewCatalog() *Catalog
func (c *Catalog) ListTemplates() []Template
func (c *Catalog) Search(query string) []Template
func (c *Catalog) Install(name string) error
```

### Scheduler
```go
type Scheduler struct { ... }
type SchedulerConfig struct {
    Registry *Registry
    History  *History
    Interval time.Duration
}
func NewScheduler(cfg SchedulerConfig) *Scheduler
func (s *Scheduler) Start(runner func(RunContext) (outcome, output string, err error))
func (s *Scheduler) Schedule(agent, cronExpr string, timeout time.Duration, maxRetries int) error
func (s *Scheduler) Unschedule(agent string) error
func (s *Scheduler) Jobs() []ScheduledJob
```

### History
```go
type History struct { ... }
func NewHistory(home string) *History
func (h *History) Record(rec RunRecord) error
func (h *History) ForAgent(name string, limit int) ([]RunRecord, error)
func (h *History) Stats(name string) (*AgentStats, error)

type AgentStats struct {
    TotalRuns    int     `json:"total_runs"`
    SuccessRate  float64 `json:"success_rate"`
    AvgDuration  float64 `json:"avg_duration_s"`
    AvgQuality   float64 `json:"avg_quality"`
    Panics       int     `json:"panics"`
    LastRun      string  `json:"last_run,omitempty"`
}
```

---

## Package: reliability

`github.com/nico/go-bt-evolve/internal/reliability`

Production-grade reliability primitives.

### Circuit Breaker
```go
type CircuitBreaker struct { ... }
type CircuitState int  // CircuitClosed | CircuitOpen | CircuitHalfOpen

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker
func (cb *CircuitBreaker) Allow() bool
func (cb *CircuitBreaker) RecordSuccess()
func (cb *CircuitBreaker) RecordFailure()
func (cb *CircuitBreaker) State() CircuitState
```

### Exponential Backoff
```go
func RetryWithBackoff(fn func() error, maxAttempts int, baseDelay time.Duration, maxDelay time.Duration) error
```

### Dead Letter Queue
```go
type DeadLetterQueue struct { ... }
func NewDeadLetterQueue(path string) *DeadLetterQueue
func (dlq *DeadLetterQueue) Push(task string, agent string, err error) error
func (dlq *DeadLetterQueue) List() ([]DeadLetterEntry, error)
func (dlq *DeadLetterQueue) Replay(id string) error
func (dlq *DeadLetterQueue) Purge() error
```

### Worker Pool
```go
type WorkerPool struct { ... }
func NewWorkerPool(size int) *WorkerPool
func (wp *WorkerPool) Submit(task func())
func (wp *WorkerPool) Stats() WorkerStats
func (wp *WorkerPool) Shutdown()
```

### Concurrency Limiter
```go
type ConcurrencyLimiter struct { ... }
func NewConcurrencyLimiter(capacity int) *ConcurrencyLimiter
func (cl *ConcurrencyLimiter) Acquire()
func (cl *ConcurrencyLimiter) TryAcquire() bool
func (cl *ConcurrencyLimiter) Release()
func (cl *ConcurrencyLimiter) Available() int
```

### Queue Interfaces
```go
type Queue interface {
    Enqueue(task string)
    Dequeue() string
    Peek() string
    Len() int
}

type PriorityTaskQueue interface {
    Enqueue(task, agent string, priority Priority) string
    Dequeue() PriorityTask
    Peek() PriorityTask
    Len() int
}

type Priority int  // PriorityCritical(0), PriorityHigh(1), PriorityMedium(2), PriorityLow(3), PriorityBackground(4)
```

### Agent Executor
```go
type AgentExecutor interface {
    Execute(agent, task string) (*AgentResult, error)
    Health() error
    String() string
}

type AgentResult struct {
    Agent, Task, Output string
    Duration time.Duration
    Success  bool
    Error    string
    QualityScore float64
}

// LocalExecutor: in-process execution
func NewLocalExecutor(fn func(agent, task string) (*AgentResult, error)) *LocalExecutor

// RemoteExecutor: HTTP-based remote execution
func NewRemoteExecutor(baseURL, apiKey string, timeout time.Duration) *RemoteExecutor

// AgentRouter: health-aware round-robin routing
type AgentRouter struct { ... }
func NewAgentRouter() *AgentRouter
func (ar *AgentRouter) Add(name string, executor AgentExecutor)
func (ar *AgentRouter) SetLocal(executor AgentExecutor)
func (ar *AgentRouter) Execute(agent, task string) (*AgentResult, error)
```

### Panic Handling
```go
func SafeGo(fn func())                // goroutine with defer/recover
func Recover(component string)        // must be called in defer
```

---

## Package: mcp

`github.com/nico/go-bt-evolve/internal/mcp`

JSON-RPC 2.0 stdio MCP server with concurrent dispatch.

### Types

```go
type Server struct { ... }
type ToolDef struct {
    Name        string
    Description string
    InputSchema map[string]any
}
type ToolResult struct {
    Content []ContentItem `json:"content"`
    IsError bool          `json:"isError,omitempty"`
}
type ContentItem struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
    Data string `json:"data,omitempty"`
    MimeType string `json:"mimeType,omitempty"`
}
```

### Functions
```go
func NewServer(name, version string) *Server
func (s *Server) RegisterTool(def ToolDef, handler func(args json.RawMessage) *ToolResult)
func (s *Server) RegisterResource(uri, name, description, mimeType string, handler func() (string, error))
func (s *Server) SetSecurity(enabled bool, apiKey string)
func (s *Server) SetRateLimit(rate float64, burst int)
func (s *Server) SetAudit(logger *slog.Logger)
func (s *Server) SetMaxMessageSize(maxBytes int)
func (s *Server) Run() error
```

---

## Package: config

`github.com/nico/go-bt-evolve/internal/config`

Environment-based configuration with JSON file support and hot-reload.

### Types

```go
type Config struct {
    DashboardPort   int      // BT_DASHBOARD_PORT (default: 9800)
    APIKey          string   // BT_API_KEY
    TLSCert, TLSKey string   // BT_TLS_CERT, BT_TLS_KEY

    LLMProvider   string   // BT_LLM_PROVIDER (ollama|deepseek)
    OllamaHost    string   // OLLAMA_HOST (default: http://localhost:11434)
    OllamaModel   string   // BT_OLLAMA_MODEL (default: qwen3.6:35b-a3b)
    DeepSeekHost  string   // BT_DEEPSEEK_HOST
    DeepSeekModel string   // BT_DEEPSEEK_MODEL
    DeepSeekKey   string   // BT_DEEPSEEK_KEY
    LLMTimeout    int      // BT_LLM_TIMEOUT (seconds, default: 300)

    RateLimitRPS   float64 // BT_RATE_LIMIT_RPS (default: 100)
    RateLimitBurst int     // BT_RATE_LIMIT_BURST (default: 20)

    // Feature flags
    GardenerEnabled     bool // BT_GARDENER_ENABLED (default: true)
    SchedulerEnabled    bool // BT_SCHEDULER_ENABLED (default: true)
    DashboardEnabled    bool // BT_DASHBOARD_ENABLED (default: true)
    EvaluationEnabled   bool // BT_EVALUATION_ENABLED (default: true)
    LangchainEnabled    bool // BT_LANGCHAIN_ENABLED (default: true)
    AutoResearchEnabled bool // BT_AUTO_RESEARCH_ENABLED (default: false)

    // Persistence
    AgentHome      string // BT_AGENT_HOME
    HistoryDir     string // BT_HISTORY_DIR
    ReflectionDir  string // BT_REFLECTION_DIR
    GardenerDir    string // BT_GARDENER_DIR
}
```

### Functions
```go
func Load() (*Config, error)                // priority: defaults → file → env
func LoadFile(path string) (*Config, error)  // load JSON file + env overrides
func (c *Config) SaveFile(path string) error // export as pretty JSON
func (c *Config) Validate() error            // 11 validation rules

// ConfigWatcher: polling-based hot-reload
type ConfigWatcher struct { ... }
func NewConfigWatcher(path string, interval time.Duration) *ConfigWatcher
func (cw *ConfigWatcher) Watch(onChange func(*Config))
func (cw *ConfigWatcher) Stop()
```

---

## Package: security

`github.com/nico/go-bt-evolve/internal/security`

Security middleware: rate limiting, sanitization, IP filtering, audit, request IDs.

### Rate Limiter
```go
type RateLimiter struct { ... }
func NewRateLimiter(rate float64, burst int) *RateLimiter
func (rl *RateLimiter) Allow(key string) bool
func (rl *RateLimiter) RateLimitMiddleware(next http.Handler) http.Handler
```

### Input Sanitization
```go
func SanitizeMiddleware(next http.Handler) http.Handler
func SanitizeInput(input string) string
```

### Security Headers
```go
type SecurityHeadersConfig struct {
    EnableHSTS  bool
    FrameOption string // "DENY" | "SAMEORIGIN"
    CSP         string
}
func SecurityHeadersMiddleware(cfg SecurityHeadersConfig) func(http.Handler) http.Handler
```

### CORS
```go
func CrossOriginMiddleware(allowedOrigins []string) func(http.Handler) http.Handler
```

### IP Filter
```go
type IPFilterMode int  // FilterAllowlist | FilterBlocklist
type IPFilter struct { ... }
func NewIPFilter(mode IPFilterMode, entries ...string) *IPFilter
func (f *IPFilter) Allowed(ipStr string) bool
func (f *IPFilter) Add(entry string)
func (f *IPFilter) Remove(entry string)
func IPFilterMiddleware(filter *IPFilter) func(http.Handler) http.Handler
```

### Audit
```go
func AuditSecurityEvent(ctx context.Context, eventType string, attrs ...slog.Attr)
func AuditContext(ctx context.Context) context.Context
func AuditMiddleware(next http.Handler) http.Handler
```

### Request IDs
```go
func GenerateRequestID() string            // crypto/rand, 16 hex chars
func RequestID(ctx context.Context) string  // retrieve ID from context
func RequestIDMiddleware(next http.Handler) http.Handler // X-Request-ID propagation
```

### Dashboard Middleware Stack
```go
// Recommended order:
//   RequestIDMiddleware → TracingMiddleware → SecurityHeaders → CrossOrigin → Sanitize → RateLimit → IPFilter → Audit → Metrics → Mux
```

---

## Package: metrics

`github.com/nico/go-bt-evolve/internal/metrics`

Prometheus-compatible metrics export.

```go
type Counter struct { ... }
func NewCounter(name, help string) *Counter
func (c *Counter) Inc()
func (c *Counter) Add(val float64)

type Gauge struct { ... }
func NewGauge(name, help string) *Gauge
func (g *Gauge) Set(val float64)
func (g *Gauge) Value() float64

type Histogram struct { ... }
func NewHistogram(name, help string, buckets []float64) *Histogram
func (h *Histogram) Observe(val float64)

func MetricsMiddleware(next http.Handler) http.Handler
func MetricsJSON() []byte                                // Prometheus text format
func RecordTask(agentName string, success bool, duration time.Duration, quality float64)
```

## Package: tracing

`github.com/nico/go-bt-evolve/internal/tracing`

OpenTelemetry-ready distributed tracing with console export.

```go
type Tracer interface {
    StartSpan(ctx context.Context, name string) (context.Context, Span)
}
type Span interface {
    End()
    AddEvent(name string, attrs map[string]string)
    SetAttribute(key, value string)
    RecordError(err error)
    SpanContext() SpanContext
    IsRecording() bool
}
type SpanContext struct {
    TraceID, SpanID string
}

func NewConsoleTracer(serviceName string, w io.Writer) Tracer
func NewNoopTracer() Tracer
func SetGlobalTracer(t Tracer)
func StartSpan(ctx context.Context, name string) (context.Context, Span)
func SpanFromContext(ctx context.Context) Span

// HTTP tracing middleware — creates a span for every request
func TracingMiddleware(next http.Handler) http.Handler
```

---

## Package: benchmark

`github.com/nico/go-bt-evolve/internal/benchmark`

Statistical mutation quality testing with external benchmark suites.

```go
type Suite struct {
    Name  string
    Tasks []SuiteTask
}
type SuiteTask struct {
    Task           string
    ExpectedRoute  string
    ExpectedOutcome string
}

func SuiteForTree(name string) *Suite
func DefaultLLM() llm.LLM      // real Ollama, falls back to mock
func QuickValidate(suite *Suite, baseline, candidate *SerializableNode) *BenchmarkResult

func ScoreMutation(tree *SerializableNode, suite *Suite, mutator func(*SerializableNode) *SerializableNode) float64

// External benchmarks
func RunBFCLTest(client llm.LLM) []BFCLResult
func RunSWELiteTest(client llm.LLM) []SWEBenchResult
func RunTauBenchTest(client llm.LLM) []TauBenchResult
func RunToolBenchTest() []ToolBenchResult
func RunBTPGTest(tree *SerializableNode) BTPGResult
```

---

## Package: api

`github.com/nico/go-bt-evolve/internal/api`

OpenAPI 3.0 generator, JSON schema I/O, content types.

```go
type ContentType string
const (
    ContentTypeText     ContentType = "text"
    ContentTypeJSON     ContentType = "json"
    ContentTypeMarkdown ContentType = "markdown"
    ContentTypeFile     ContentType = "file"
    ContentTypeCode     ContentType = "code"
)

type AgentDefinition struct {
    APIVersion string       `json:"api_version"`
    InputSchema  *JSONSchema `json:"input_schema,omitempty"`
    OutputSchema *JSONSchema `json:"output_schema,omitempty"`
}

func ValidateOutput(input any, schema *JSONSchema) error

// OpenAPI 3.0 Generator
type OpenAPIGenerator struct { ... }
func NewOpenAPIGenerator(title, version string) *OpenAPIGenerator
func (g *OpenAPIGenerator) AddRoute(route RouteDef)
func (g *OpenAPIGenerator) Generate() []byte

type RouteBuilder struct { ... }
func (g *OpenAPIGenerator) Route(method, path string) *RouteBuilder
func (rb *RouteBuilder) Tag(tag string) *RouteBuilder
func (rb *RouteBuilder) Summary(s string) *RouteBuilder
func (rb *RouteBuilder) Add() // finalizes and adds to generator

func DashboardRoutes() []RouteDef  // 16 endpoints across 8 tag groups
```

---

## Package: knowledge

`github.com/nico/go-bt-evolve/internal/knowledge`

Tree knowledge graph for discovery and auto-creation.

```go
type KnowledgeGraph struct { ... }
func NewKnowledgeGraph() *KnowledgeGraph
func (kg *KnowledgeGraph) Register(treeID, category string, capabilities []string, keywords []string)
func (kg *KnowledgeGraph) Discover(task string) ([]Match, error)
func (kg *KnowledgeGraph) QueryByCapability(capability string) []string
func (kg *KnowledgeGraph) AutoCreateTree(task string) (string, *SerializableNode, error)

// Tree Factory (crossover breeding)
func CreateTree(task, category string, parents []string) (*SerializableNode, error)
func CreateFromParents(parentA, parentB, task string) (*SerializableNode, error)
```

---

## Package: factory

`github.com/nico/go-bt-evolve/internal/factory`

Skill→BT compiler. Analyzes SKILL.md, generates behavior trees.

```go
type Analyzer struct { ... }
func NewAnalyzer(llm llm.LLM) *Analyzer
func (a *Analyzer) Analyze(skillContent string) (*TreeSpec, error)

type Generator struct { ... }
func NewGenerator() *Generator
func (g *Generator) Generate(spec *TreeSpec, name string, bb *engine.Blackboard) (*engine.SerializableNode, error)

type AgentFactory struct { ... }
func NewAgentFactory(llm llm.LLM) *AgentFactory
func (af *AgentFactory) CreateFromSkillDir(path string) (*engine.SerializableNode, error)
```

---

## Package: llm

`github.com/nico/go-bt-evolve/internal/llm`

LLM client wrapper for Ollama via langchaingo.

```go
type LLM interface {
    Generate(prompt string) (string, error)
    AnalyzeComplexity(task string) string
    GeneratePlan(task, complexity string) string
    Reflect(task, outcome, plan string) (wentWell string, toImprove string)
}

type Client struct { ... }
func NewClient(cfg Config) (*Client, error)
func DefaultConfig() Config

type HealthMonitor struct { ... }
func NewHealthMonitor(client LLM, probeInterval time.Duration) *HealthMonitor
func (hm *HealthMonitor) Start()
func (hm *HealthMonitor) State() HealthState
func (hm *HealthMonitor) IsHealthy() bool
```

---

## Package: monitoring

`github.com/nico/go-bt-evolve/internal/monitoring`

Prometheus alert rules evaluating and alert evaluator.

```go
type Alert struct {
    Name        string `json:"name"`
    Severity    string `json:"severity"`    // critical | warning | info
    Component   string `json:"component"`
    Summary     string `json:"summary"`
    Description string `json:"description"`
    Firing      bool   `json:"firing"`
    Value       string `json:"value"`
}

type AlertReport struct {
    EvaluatedAt string  `json:"evaluated_at"`
    TotalRules  int     `json:"total_rules"`
    FiringCount int     `json:"firing_count"`
    Alerts      []Alert `json:"alerts"`
    AllClear    bool    `json:"all_clear"`
}

func EvaluateAlerts(metrics MetricsJSON) AlertReport
func EvaluateFromJSON(data []byte) (AlertReport, error)
```

---

## Package: domains

`github.com/nico/go-bt-evolve/internal/domains`

11 domain-specific behavior trees. All 100% test-covered.

| Tree | Conditions | Actions | Use Case |
|---|---|---|---|
| `CodeReviewTree()` | IsCodeReview, IsBugReport, IsSecurityIssue, IsStyleIssue | ReviewCode, SuggestBugFixes, SuggestSecurityFixes, SuggestStyleFixes | PR/code review |
| `DevOpsCITree()` | IsCIJob, NeedsBuild, NeedsDeploy | BuildService, RunTests, DeployService | CI/CD management |
| `AgentMonitorTree()` | IsHealthCheck, IsMetricsRequest, HasDeadAgents | CollectHealth, CollectMetrics, RestartDeadAgents | Agent monitoring |
| `RefactoringTree()` | IsRefactoring, IsPerformanceIssue, IsReadabilityIssue | AnalyzeCode, SuggestRefactoring, ApplyRefactoring | Code refactoring |
| `SecurityAuditTree()` | IsSecurityCheck | AuditDependencies, ScanVulnerabilities, ReportFindings | Security audit |
| `DataPipelineTree()` | IsETLTask, IsIngestion, IsTransformation, IsLoading | ExtractData, TransformData, LoadData | ETL pipeline |
| `MeetingNotesTree()` | IsMeetingTask | TranscribeMeeting, ExtractActionItems, GenerateMinutes | Meeting notes |
| `CrashInvestigatorTree()` | IsIncident, HasStackTrace, HasLogs | AnalyzeCrash, IdentifyRootCause, SuggestFix | Incident response |
| `GameAITree()` | IsNPCBehavior, IsPathfinding, IsDecision | PlanPath, ExecuteBehavior, EvaluateDecision | Game AI |
| `TradingSignalTree()` | IsTradingSignal | AnalyzeMarket, GenerateSignal, RouteAlert | Trading signals |
| `AlertRouterTree()` | IsCritical, IsSecurity, IsTrading, IsDiskAlert, IsHealthAlert | RouteToAllChannels, RouteToSecurityChannel, RouteToTradingChannel, RouteToDevOpsChannel, RouteToDefaultChannel | Alert routing |

Call via MCP: `bt_use_domain_tree(tree="code_review")`

---

## Package: thinktank

`github.com/nico/go-bt-evolve/internal/thinktank`

Hegelian dialectic analysis with 5 fellows.

```go
type ThinkTank struct { ... }
func NewThinkTank(name string) *ThinkTank

type Fellow struct {
    Name, Role, Lens, Persona string
}

type ResearchFinding struct { ... }
type DebateTurn struct { ... }
type Synthesis struct { ... }
type ReviewComment struct { ... }
type Report struct { ... }

type ThinkTankOrchestrator struct { ... }
func NewThinkTankOrchestrator(tt *ThinkTank, llm llm.LLM) *ThinkTankOrchestrator
func (o *ThinkTankOrchestrator) RunFullAnalysis(topic string) (*Report, error)
```

---

## Package: startup

`github.com/nico/go-bt-evolve/internal/startup`

Startup company simulation with role-based trees.

```go
type CompanyState struct {
    Name        string
    MRR, ARR    float64
    Users       int
    TeamSize    int
    Runway      float64
    Cash        float64
    // ... 40 fields total
}

type CompanyOrchestrator struct { ... }
func NewDefaultCompany() *CompanyState
func NewOrchestrator(company *CompanyState, llm llm.LLM) *CompanyOrchestrator
func (o *CompanyOrchestrator) RunSprint() (*SprintResult, error)
func (o *CompanyOrchestrator) RunQuarter() (*QuarterResult, error)
func (o *CompanyOrchestrator) RunYear() (*YearResult, error)
```

---

## Package: evaluator

`github.com/nico/go-bt-evolve/internal/evaluator`

Stockfish-style behavior tree evaluator. Multi-dimensional fitness (success_rate, stability, coverage, speed, complexity), transposition table (SHA256 tree+task → cached outcome), killer move ordering, iterative deepening.

```go
type Evaluator struct { ... }
func NewEvaluator(llm llm.LLM) *Evaluator
func (e *Evaluator) Evaluate(tree *SerializableNode, task string) (float64, error)
func (e *Evaluator) OrderMutations(tree *SerializableNode, mutations []Mutation) []Mutation
func (e *Evaluator) Deepen(tree *SerializableNode, task string, depth int) (*SerializableNode, error)
```

## Package: gardener

`github.com/nico/go-bt-evolve/internal/gardener`

24/7 evolution daemon. Runs 25 trees on 5-minute cycles using Stockfish-style move ordering. Benchmark-validated mutations, MetricsTracker, idempotency guards, soft diversity preference.

```go
type Gardener struct { ... }
type Config struct { UseRealLLM bool }
type MetricsTracker struct { ... }
func NewGardener(cfg Config) *Gardener
func (g *Gardener) RunCycle() CycleResult
func (m *MetricsTracker) Save() error
func (m *MetricsTracker) CyclesForTree(name string) int
```

## Package: goap

`github.com/nico/go-bt-evolve/internal/goap`

GOAP (Goal-Oriented Action Planning) planner integrated with behavior trees. DocPlanner for world-state modeling, BlackboardBridge for BT state sync, StandardActions registry, plan validation.

```go
type Planner interface { Plan(world WorldState, goal Goal) (Plan, error) }
type DocPlanner struct { ... }
func NewDocPlanner(actions []Action) *DocPlanner
func BuildGoalState(task string) WorldState
type BlackboardBridge struct { ... }
func (b *BlackboardBridge) SyncToBlackboard(bb *engine.Blackboard) error
```

## Package: langagent

`github.com/nico/go-bt-evolve/internal/langagent`

LangChain ReAct agent wrapping BT tools as agent tools. Provides managed tool execution via ReAct loop (Thought→Action→Observation→Final Answer). 3 MCP tools exposed via bt-langagent binary.

```go
type Agent struct { ... }
func NewAgent(llm llm.LLM, tools []Tool) (*Agent, error)
func (a *Agent) Run(input string) (string, error)
```

## Package: reflection

`github.com/nico/go-bt-evolve/internal/reflection`

Reflection store for task outcomes. Records are JSON-persisted with atomic write-tmp-rename. Used by the evolution loop to track success/failure patterns for tree adaptation.

```go
type Store struct { ... }
type Record struct { Task, Outcome, Result, Plant string }
func NewStore(path string) (*Store, error)
func (s *Store) Save(record Record) error
func (s *Store) List() ([]Record, error)
```

## Package: research

`github.com/nico/go-bt-evolve/internal/research`

Deep and quick research behavior trees. DeepResearchTree (20 nodes, agent-based iterative search loop, refine chain for synthesis). QuickResearchTree (12 nodes, agent-based research).

## Package: validate

`github.com/nico/go-bt-evolve/internal/validate`

Agent validation suites with composite scoring. 5 test kinds: smoke, routing, output, regression, edge. Weighted scoring (SR×0.4 + OQ×0.3 + Speed×0.2 + Robustness×0.1).

```go
type TestSuite struct { ... }
type TestKind int
const ( SmokeTest TestKind = iota; RoutingTest; OutputTest; RegressionTest; EdgeTest )
type SuiteResult struct { ... }
func RunSuite(suite *TestSuite, tree *SerializableNode) (*SuiteResult, error)
func CompositeScore(suite *SuiteResult) float64
```

## Package: workflow

`github.com/nico/go-bt-evolve/internal/workflow`

Multi-agent workflow orchestrator. Sequential, parallel, conditional, loop, and retry execution patterns. Converts thinktank analysis to prioritized company tasks with role assignment.

```go
type Workflow struct { ... }
type Task struct { ID, Description, Priority, Assignee, Status string }
func (w *Workflow) RecommendationsToTasks(report *thinktank.Report) []Task
func (w *Workflow) ExecuteSprint() error
```

## Package: log

`github.com/nico/go-bt-evolve/internal/log`

Structured logging with file rotation. RotatingWriter implements io.WriteCloser with size-based rotation (10MB default, 5 backups). Pure stdlib — no external deps.

```go
type RotatingWriter struct { ... }
func NewRotatingWriter(path string) (*RotatingWriter, error)
func (w *RotatingWriter) Write(p []byte) (n int, err error)
func (w *RotatingWriter) Close() error
func Init(path string) error
```

---

## Package: persistence

`github.com/nico/go-bt-evolve/internal/persistence`

Generic persistence primitives for BT state storage. Provides file-backed persistent storage interfaces used by the reflection store, job store, and scheduler state.

```go
type Store struct { ... }
func NewStore(basePath string) (*Store, error)
func (s *Store) Save(name string, data []byte) error
func (s *Store) Load(name string) ([]byte, error)
func (s *Store) List(prefix string) ([]string, error)
func (s *Store) Delete(name string) error
```

## Package: util

`github.com/nico/go-bt-evolve/internal/util`

Shared utility functions used across the bt-evolve codebase. Provides string conversion helpers and common patterns to eliminate duplicate implementations.

```go
func Itoa(i int) string
func Atoi(s string) (int, error)
func Contains(s, substr string) bool
```

## Package: tools

`github.com/nico/go-bt-evolve/internal/tools`

Built-in tool implementations for BT agent chains. Each tool implements `Name()`, `Description()`, and `Call(string)string` for use with `ChainTools` on the engine blackboard. Provides real implementations for web search, file I/O, command execution, and Go toolchain operations.

```go
type Tool struct { ... }
func NewWebSearch() Tool
func NewGoBuild() Tool
func NewGoTest() Tool
func NewGoVet() Tool
func NewCalculator() Tool
func NewFileRead() Tool
func NewFileWrite() Tool
func (t Tool) Name() string
func (t Tool) Description() string
func (t Tool) Call(input string) string
```

## Package: a2a

`github.com/nico/go-bt-evolve/internal/a2a`

Agent-to-Agent (A2A) protocol integration. Auto-generates A2A Agent Cards from BT agent definitions, provides an A2A server wrapping the agent registry, an A2A client for BT trees to delegate to external agents, and a task bridge mapping A2A task lifecycle to BT Blackboard outcomes.

```go
type AgentCard struct { Name, Description, URL string }
func GenerateAgentCard(agent *agent.Definition) *AgentCard
func NewServer(registry *agent.Registry, port int) *Server
func (s *Server) Start() error
func NewClient(baseURL string) *Client
func (c *Client) SendTask(task string) (string, error)
```

## Package: eval

`github.com/nico/go-bt-evolve/internal/eval`

Platform evaluation runner that executes all 20 use case suites against the merged behavior tree and produces a maturity scorecard. Validates tree routing, output quality, and domain-specific task handling across all registered trees.

```go
type PlatformEvalResult struct { ... }
func RunAllSuites(llm llm.LLM) (*PlatformEvalResult, error)
func RunSuite(name string, llm llm.LLM) (*SuiteResult, error)
func Scorecard(result *PlatformEvalResult) string
```

## Package: benchreg

`github.com/nico/go-bt-evolve/internal/benchreg`

Benchmark regression detection for Go benchmarks. Parses `go test -bench` output, stores baseline results, and compares new runs against stored baselines to detect significant performance regressions. Used by the CI/CD pipeline for automated performance monitoring.

```go
type BaselineStore struct { ... }
func NewBaselineStore(path string) *BaselineStore
func (s *BaselineStore) Load() ([]BenchmarkResult, error)
func (s *BaselineStore) Save(results []BenchmarkResult) error
type Comparator struct { ... }
func NewComparator(store *BaselineStore, config RegressionConfig) *Comparator
func (c *Comparator) Compare(current []BenchmarkResult) []ComparisonResult
```

## Package: cicd

`github.com/nico/go-bt-evolve/internal/cicd`

CI/CD workflow validation and maturity checks. Implements ci-doctor — 37 evidence-gated checks covering GitHub Actions workflows, linting, build, test, release configuration, runner presence, and security scanning. Produces WorkflowReport evidence artifacts for platform maturity validation.

```go
type WorkflowReport struct { ... }
func ValidateWorkflows(rootDir string) *WorkflowReport
type Check struct { Name, Details string; Passed bool }
func ValidateChangelog(rootDir string) error
func ValidateRunnerStatus() bool
```

## Package: internal/dashboard

`github.com/nico/go-bt-evolve/internal/dashboard`

Dashboard API handlers for the bt-dashboard web UI. Provides endpoints for agent management, task execution, tree visualization, scalability monitoring, and OTLP collector stats. Wraps the agent registry, scheduler, and evaluator for browser-accessible operations.

```go
type AgentInfo struct { Name, Status, Schedule string; SuccessRate float64 }
type ScheduledJob struct { Name, Agent, Schedule, Status, LastRun string }
func TaskApproveHandler(w http.ResponseWriter, r *http.Request)
func TaskRejectHandler(w http.ResponseWriter, r *http.Request)
func ScalabilityHandler(w http.ResponseWriter, r *http.Request)
func OtlpStatsHandler(w http.ResponseWriter, r *http.Request)
func AgentsHandler(w http.ResponseWriter, r *http.Request)
```

## Binaries

| Binary | Port | Purpose |
|---|---|---|
| `bt-agent` | — (MCP stdio) | Core BT execution (32 MCP tools) |
| `bt-evaluator` | — (MCP stdio) | Stockfish-style tree evaluator (5 tools) |
| `bt-langagent` | — (MCP stdio) | Langchain ReAct agent (3 tools) |
| `bt-dashboard` | 9800 | Web dashboard (20+ API endpoints) |
| `bt-gardener` | — (daemon) | 24/7 tree evolution, 5-min cycles |
| `bt-otlp-collector` | 4318 | OTLP/HTTP trace collector, auto-started by dashboard |
| `bt-security-probe` | — (CLI) | 23-check security posture assessment |
| `bt-scalability-probe` | — (CLI) | Single-node scalability probe and multi-node probing |
| `bt-agent-cli` | — (CLI) | Agent management CLI |
| `bt-tree-integration` | — (CLI) | Tree registration and loading validation |
| `bt-assistant` | — (CLI) | BT assistant CLI |
| `evolve_all` | — (script) | Batch evolution runner |
| `hermes_improve` | — (script) | Self-improvement runner |

## MCP Tools Inventory (40 total)

**bt-agent** (32): `bt_run_task`, `bt_get_tree`, `bt_get_reflections`, `bt_evolve`, `bt_reset`, `bt_get_fitness`, `bt_create_agent`, `bt_use_go_tree`, `bt_use_finance_tree`, `bt_list_finance_trees`, `bt_use_research_tree`, `bt_use_domain_tree`, `bt_startup_simulate`, `bt_startup_summary`, `bt_thinktank_analyze`, `bt_delegate_to_tree`, `bt_kg_discover`, `bt_kg_query`, `bt_kg_auto_create`, `bt_kg_summary`, `bt_agent_create`, `bt_agent_list`, `bt_agent_run`, `bt_agent_history`, `bt_agent_schedule`, `bt_agent_delete`, `bt_factory_create`, `bt_evolve_expert`, `bt_evolve_genetic`, `bt_health`, `bt_workflow_run`, `bt_workflow_approve`

**bt-evaluator** (5): `ev_evaluate`, `ev_order_mutations`, `ev_deepen`, `ev_tt_stats`, `ev_tt_save`

**bt-langagent** (3): `la_run`, `la_evolve`, `la_fitness`

## Quick Links

- [Getting Started Guide](GETTING_STARTED.md)
- [Architecture Decision Records](adr/INDEX.md)
- [Changelog](../CHANGELOG.md)
- [Maturity Progress Tracker](../../../../mnt/ssd/clawd/wiki/bt-research/goals/maturity-progress.md)
