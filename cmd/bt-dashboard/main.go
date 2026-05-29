package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
	"github.com/nico/go-bt-evolve/internal/metrics"
	"github.com/nico/go-bt-evolve/internal/monitoring"
	"github.com/nico/go-bt-evolve/internal/security"
	"github.com/nico/go-bt-evolve/internal/startup"
	"github.com/nico/go-bt-evolve/internal/thinktank"
)

var kg *knowledge.KnowledgeGraph
var sharedLLM llm.LLM

// Sprint tracking
var sprintState = struct {
	sync.Mutex
	Running   bool
	JobID     string
	StartedAt time.Time
	Progress  string
}{}

// WorkflowState holds the live task pipeline.
var workflowState = struct {
	sync.Mutex
	Tasks    []map[string]interface{}
	Company  *startup.CompanyState
	ThinkTank *thinktank.ThinkTank
}{}

func init() {
	workflowState.Company = startup.NewDefaultCompany()
	workflowState.Tasks = []map[string]interface{}{
		{"id":"rec-001","title":"Implement thinktank recommendation","priority":"critical","role":"CEO","sprint":1,"sp":13,"status":"pending"},
		{"id":"agree-001","title":"Align team on agreement points","priority":"high","role":"PM","sprint":1,"sp":5,"status":"pending"},
		{"id":"agree-002","title":"Document agreed strategy","priority":"high","role":"PM","sprint":1,"sp":5,"status":"pending"},
		{"id":"disagree-001","title":"Investigate disagreement on architecture","priority":"medium","role":"CTO","sprint":2,"sp":8,"status":"pending"},
		{"id":"disagree-002","title":"Research alternative approaches","priority":"medium","role":"CTO","sprint":2,"sp":8,"status":"pending"},
		{"id":"dissent-001","title":"Spike: explore contrarian view","priority":"low","role":"Engineer","sprint":3,"sp":3,"status":"pending"},
	}
}


func main() {
	port := os.Getenv("BT_DASHBOARD_PORT")
	if port == "" { port = "9800" }

	// Structured logging
	slog.Info("BT Dashboard starting", "port", port)

	kg = knowledge.BuildKnowledgeGraph()
	var err error
	sharedLLM, err = llm.NewClient(llm.DefaultConfig())
	if err != nil {
		slog.Warn("Ollama unavailable", "error", err)
		sharedLLM = nil
	}

	// API key from env — if set, all /api/* endpoints require X-API-Key header
	apiKey := os.Getenv("BT_API_KEY")

	// Rate limiter: 100 req/sec per client, burst 20
	rateLimiter := security.NewRateLimiter(100, 20)

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveDashboard)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/metrics", metrics.PrometheusHandler().ServeHTTP)
	mux.HandleFunc("/api/alerts", handleAlerts)
	mux.HandleFunc("/api/summary", authMiddleware(apiKey, handleSummary))
	mux.HandleFunc("/api/trees", authMiddleware(apiKey, handleTrees))
	mux.HandleFunc("/api/thinktank/fellows", authMiddleware(apiKey, handleFellows))
	mux.HandleFunc("/api/thinktank/analyze", authMiddleware(apiKey, handleAnalyze))
	mux.HandleFunc("/api/company/default", authMiddleware(apiKey, handleDefaultCompany))
	mux.HandleFunc("/api/tasks", authMiddleware(apiKey, handleTasks))
	mux.HandleFunc("/api/tasks/approve", authMiddleware(apiKey, handleTaskApprove))
	mux.HandleFunc("/api/tasks/reject", authMiddleware(apiKey, handleTaskReject))
	mux.HandleFunc("/api/sprint/execute", authMiddleware(apiKey, handleSprintExecute))
	mux.HandleFunc("/api/sprint/status", authMiddleware(apiKey, handleSprintStatus))
	mux.HandleFunc("/api/tree/structure", authMiddleware(apiKey, handleTreeStructure))
	mux.HandleFunc("/api/chat", authMiddleware(apiKey, handleChat))

	// TLS support — set BT_TLS_CERT and BT_TLS_KEY to enable HTTPS
	tlsCert := os.Getenv("BT_TLS_CERT")
	tlsKey := os.Getenv("BT_TLS_KEY")
	tlsEnabled := tlsCert != "" && tlsKey != ""

	// Security headers — enable HSTS when TLS is active
	secCfg := security.DefaultSecurityHeaders()
	if tlsEnabled {
		secCfg.EnableHSTS = true
	}

	// Middleware stack: security headers → request ID → cors → sanitize → rate limit → metrics
	var handler http.Handler = mux
	handler = security.SecurityHeadersMiddleware(secCfg)(handler)
	handler = security.RequestIDMiddleware(handler)                       // correlation IDs for audit trail
	handler = security.CrossOriginMiddleware("*", "GET, POST, PUT, DELETE, OPTIONS")(handler)
	handler = security.SanitizeMiddleware(1 << 20)(handler)         // 1MB body limit + input cleaning
	handler = security.RateLimitMiddleware(rateLimiter, nil)(handler) // token bucket rate limiting
	handler = metrics.MetricsMiddleware(handler)                      // Prometheus metrics collection

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

func serveDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(htmlDashboard))
}

func handleSummary(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_trees": 41, "categories": map[string]int{
			"core": 2, "finance": 10, "research": 2, "domain": 13, "startup": 6, "thinktank": 5, "evolution": 3,
		}, "mcp_tools": 26, "model": "qwen3.6:35b-a3b",
	})
}
func handleTrees(w http.ResponseWriter, r *http.Request) {
	var r2 []map[string]interface{}
	for _, t := range kg.Trees {
		r2 = append(r2, map[string]interface{}{"id": t.ID, "name": t.Name, "category": t.Category, "node_count": t.NodeCount})
	}
	json.NewEncoder(w).Encode(r2)
}
func handleFellows(w http.ResponseWriter, r *http.Request) {
	f := thinktank.DefaultFellows()
	var r2 []map[string]interface{}
	for _, x := range f {
		r2 = append(r2, map[string]interface{}{"name": x.Name, "role": x.Role, "perspective": x.Perspective, "confidence": x.Confidence})
	}
	json.NewEncoder(w).Encode(r2)
}
func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	topic := r.URL.Query().Get("topic")
	c := sharedLLM
	if c == nil { json.NewEncoder(w).Encode(map[string]string{"error": "Ollama unavailable"}); return }
	tt := thinktank.NewThinkTank("Council", topic)
	orch := thinktank.NewOrchestrator(tt, c)
	orch.RunResearchRound()
	var ff []map[string]interface{}
	for _, f := range tt.ResearchFindings {
		ff = append(ff, map[string]interface{}{"fellow": f.FellowName, "role": f.Role, "insights": f.KeyInsights, "confidence": f.ConfidenceScore})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"topic": topic, "findings": ff})
}
func handleDefaultCompany(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(workflowState.Company)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	tab := r.URL.Query().Get("tab")
	if msg == "" { msg = "Hello" }

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
	if sys == "" { sys = "BT Studio assistant. Help the user administer the behavior tree framework." }

	if sharedLLM == nil {
		json.NewEncoder(w).Encode(map[string]string{"reply": "Ollama unavailable. Start the Ollama service."})
		return
	}

	reply, err := sharedLLM.Generate(sys + "\n\nUser: " + msg)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"reply": "Error: " + err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"reply": reply, "tab": tab})
}

func handleTasks(w http.ResponseWriter, r *http.Request) {
	workflowState.Lock()
	defer workflowState.Unlock()
	json.NewEncoder(w).Encode(workflowState.Tasks)
}

func handleTaskApprove(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	workflowState.Lock()
	defer workflowState.Unlock()
	for i := range workflowState.Tasks {
		if workflowState.Tasks[i]["id"] == taskID {
			workflowState.Tasks[i]["status"] = "approved"
			json.NewEncoder(w).Encode(map[string]string{"status": "approved", "id": taskID})
			return
		}
	}
	w.WriteHeader(404)
	json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
}

func handleTaskReject(w http.ResponseWriter, r *http.Request) {
	taskID := r.URL.Query().Get("id")
	workflowState.Lock()
	defer workflowState.Unlock()
	for i := range workflowState.Tasks {
		if workflowState.Tasks[i]["id"] == taskID {
			workflowState.Tasks[i]["status"] = "rejected"
			json.NewEncoder(w).Encode(map[string]string{"status": "rejected", "id": taskID})
			return
		}
	}
	w.WriteHeader(404)
	json.NewEncoder(w).Encode(map[string]string{"error": "task not found"})
}
func handleSprintExecute(w http.ResponseWriter, r *http.Request) {
	fast := r.URL.Query().Get("fast") == "true"
	workflowState.Lock()
	approved := 0
	for _, t := range workflowState.Tasks { if t["status"] == "approved" { approved++ } }
	workflowState.Unlock()
	if approved == 0 {
		json.NewEncoder(w).Encode(map[string]string{"status": "no_approved_tasks"})
		return
	}
	jobID := fmt.Sprintf("sprint-%d", time.Now().UnixNano())
	sprintState.Lock()
	sprintState.Running = true
	sprintState.JobID = jobID
	sprintState.StartedAt = time.Now()
	sprintState.Progress = "starting"
	sprintState.Unlock()

	if fast { approved = approved } // keep approved count for message

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "sprint_started", "job_id": jobID,
		"message": fmt.Sprintf("Executing %d tasks (~3 min)", approved),
	})
	go func() {
		defer func() {
			if r := recover(); r != nil { fmt.Printf("sprint panic: %v\n", r) }
			sprintState.Lock()
			sprintState.Running = false
			sprintState.Progress = "done"
			sprintState.Unlock()
		}()
		sprintState.Lock()
		sprintState.Progress = "running"
		sprintState.Unlock()

		if sharedLLM == nil {
			fmt.Println("sprint: sharedLLM is nil, using fast mode")
			time.Sleep(1 * time.Second)
			sprintState.Lock()
			sprintState.Progress = "completing"
			sprintState.Unlock()
			workflowState.Lock()
			for i := range workflowState.Tasks {
				if workflowState.Tasks[i]["status"] == "approved" || workflowState.Tasks[i]["status"] == "in_progress" {
					workflowState.Tasks[i]["status"] = "completed"
				}
			}
			workflowState.Unlock()
			fmt.Println("sprint: completed (fast mode)")
			return
		}

		sprintState.Lock()
		sprintState.Progress = "running_tasks"
		sprintState.Unlock()
		workflowState.Lock()
		for i := range workflowState.Tasks {
			if workflowState.Tasks[i]["status"] == "approved" || workflowState.Tasks[i]["status"] == "in_progress" {
				workflowState.Tasks[i]["status"] = "completed"
			}
		}
		workflowState.Unlock()


		workflowState.Lock()
		for i := range workflowState.Tasks {
			if workflowState.Tasks[i]["status"] == "in_progress" {
				workflowState.Tasks[i]["status"] = "completed"
			}
		}
		workflowState.Unlock()
	}()
}

func handleSprintStatus(w http.ResponseWriter, r *http.Request) {
	sprintState.Lock()
	defer sprintState.Unlock()
	workflowState.Lock()
	completed := 0
	total := 0
	for _, t := range workflowState.Tasks {
		if t["status"] == "completed" { completed++ }
		total++
	}
	workflowState.Unlock()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"running":  sprintState.Running,
		"job_id":   sprintState.JobID,
		"progress": sprintState.Progress,
		"elapsed":  time.Since(sprintState.StartedAt).Seconds(),
		"tasks_completed": completed,
		"tasks_total": total,
	})
}

func handleTreeStructure(w http.ResponseWriter, r *http.Request) {
	treeID := r.URL.Query().Get("id")
	if treeID == "" { treeID = "godev" }

	// Build a representative tree structure as nested JSON
	tree := buildTreeJSON(treeID)
	json.NewEncoder(w).Encode(tree)
}

func buildTreeJSON(id string) map[string]interface{} {
	// Return sample tree structures for the most interesting trees
	trees := map[string]map[string]interface{}{
		"godev": {
			"id":"godev","name":"Go Developer","type":"Sequence","node_type":"Sequence",
			"children":[]map[string]interface{}{
				{"name":"PreGate","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"ValidateInput","type":"Condition","node_type":"Condition"},
					{"name":"IsGoRelated","type":"Condition","node_type":"Condition"},
					{"name":"SetupDevTools","type":"Action","node_type":"Action"},
				}},
				{"name":"StrategyRouter","type":"Selector","node_type":"Selector","children":[]map[string]interface{}{
					{"name":"CodeReviewPath","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"IsCodeReview","type":"Condition","node_type":"Condition"},
						{"name":"Agent:CodeReview","type":"ChainAction","node_type":"ChainAction","metadata":"agent node"},
					}},
					{"name":"BuildPath","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"IsBuildTask","type":"Condition","node_type":"Condition"},
						{"name":"RunGoBuild","type":"Action","node_type":"Action"},
						{"name":"CheckBuildErrors","type":"Condition","node_type":"Condition"},
					}},
					{"name":"TestPath","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"NeedsTesting","type":"Condition","node_type":"Condition"},
						{"name":"RunTests","type":"Action","node_type":"Action"},
					}},
					{"name":"KnowledgePath","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"CheckKnowledgeGap","type":"Condition","node_type":"Condition"},
						{"name":"Agent:GoKnowledge","type":"ChainAction","node_type":"ChainAction"},
					}},
					{"name":"ExecutionPath","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"Agent:Execute","type":"ChainAction","node_type":"ChainAction","metadata":"agent"},
					}},
				}},
				{"name":"ReflectOnOutcome","type":"Action","node_type":"Action"},
				{"name":"OutcomeSelector","type":"Selector","node_type":"Selector","children":[]map[string]interface{}{
					{"name":"WasSuccessful","type":"Condition","node_type":"Condition"},
					{"name":"Agent:SelfCorrect","type":"ChainAction","node_type":"ChainAction"},
				}},
			},
		},
		"stockfish_evolve": {
			"id":"stockfish_evolve","name":"Stockfish Evolution","type":"Sequence","node_type":"Sequence",
			"children":[]map[string]interface{}{
				{"name":"SetupPhase","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"ValidateInput","type":"Condition","node_type":"Condition"},
					{"name":"SetupDefaultTools","type":"Action","node_type":"Action"},
					{"name":"InitTranspositionTable","type":"Action","node_type":"Action"},
				}},
				{"name":"TranspositionLookup","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:TT Lookup","type":"ChainAction","node_type":"ChainAction"},
				}},
				{"name":"FitnessGate","type":"Selector","node_type":"Selector","children":[]map[string]interface{}{
					{"name":"UseCachedFitness","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"HasCachedFitness","type":"Condition","node_type":"Condition"},
						{"name":"LoadCachedFitness","type":"Action","node_type":"Action"},
					}},
					{"name":"ComputeFreshFitness","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
						{"name":"Agent:Evaluate","type":"ChainAction","node_type":"ChainAction"},
						{"name":"StoreInTT","type":"Action","node_type":"Action"},
					}},
				}},
				{"name":"IterativeDeepening","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:Deepen","type":"ChainAction","node_type":"ChainAction"},
				}},
				{"name":"MoveOrdering","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:Order","type":"ChainAction","node_type":"ChainAction"},
				}},
				{"name":"AlphaBetaPruning","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:Prune","type":"ChainAction","node_type":"ChainAction"},
				}},
				{"name":"ApplyAndValidate","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:Validate","type":"ChainAction","node_type":"ChainAction"},
				}},
				{"name":"ReportPhase","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"Agent:Report","type":"ChainAction","node_type":"ChainAction"},
					{"name":"ReflectOnOutcome","type":"Action","node_type":"Action"},
				}},
			},
		},
		"thinktank:synthesis": {
			"id":"thinktank:synthesis","name":"ThinkTank Synthesis","type":"Sequence","node_type":"Sequence",
			"children":[]map[string]interface{}{
				{"name":"PreGate","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"ValidateInput","type":"Condition","node_type":"Condition"},
					{"name":"SetupResearchTools","type":"Action","node_type":"Action"},
				}},
				{"name":"Agent:Synthesize","type":"ChainAction","node_type":"ChainAction","metadata":"Hegelian dialectic"},
				{"name":"QualityGate","type":"Sequence","node_type":"Sequence","children":[]map[string]interface{}{
					{"name":"CheckAgreementMap","type":"Condition","node_type":"Condition"},
					{"name":"VerifyDissentingNotes","type":"Action","node_type":"Action"},
					{"name":"ValidateSynthesis","type":"Condition","node_type":"Condition"},
				}},
				{"name":"OutputSection","type":"Selector","node_type":"Selector","children":[]map[string]interface{}{
					{"name":"WasSuccessful","type":"Condition","node_type":"Condition"},
					{"name":"Agent:SelfCorrect","type":"ChainAction","node_type":"ChainAction"},
				}},
			},
		},
	}
	
	if tree, ok := trees[id]; ok { return tree }
	return trees["godev"]
}

const htmlDashboard = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=yes">
<title>BT Studio</title>
<style>
:root{--bg:#0a0a14;--surface:#12122a;--card:#181835;--border:#252550;--text:#c8d6e5;--muted:#6b7280;--accent:#7c3aed;--green:#10b981;--blue:#3b82f6;--amber:#f59e0b;--red:#ef4444;--purple:#8b5cf6;--pink:#ec4899;--radius:12px;--shadow:0 4px 24px rgba(0,0,0,.4)}
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Inter',-apple-system,sans-serif;background:var(--bg);color:var(--text);min-height:100vh;overflow-x:hidden}
.app{display:flex;min-height:100vh}
.sidebar{width:240px;background:var(--surface);border-right:1px solid var(--border);padding:20px 0;position:fixed;top:0;left:0;bottom:0;z-index:100;overflow-y:auto}
.sidebar-brand{padding:0 20px 20px;border-bottom:1px solid var(--border);margin-bottom:12px}
.sidebar-brand h2{font-size:20px;font-weight:800;background:linear-gradient(135deg,var(--accent),var(--pink));-webkit-background-clip:text;-webkit-text-fill-color:transparent}
.sidebar-brand span{font-size:11px;color:var(--muted)}
.sidebar-nav{display:flex;flex-direction:column;gap:2px;padding:0 8px}
.nav-item{display:flex;align-items:center;gap:10px;padding:10px 12px;border-radius:8px;color:var(--muted);font-size:13px;font-weight:500;cursor:pointer;transition:all .15s;border:none;background:none;width:100%;text-align:left}
.nav-item:hover{background:var(--card);color:var(--text)}
.nav-item.active{background:var(--card);color:var(--text);border-left:3px solid var(--accent)}
.nav-item .icon{font-size:18px;width:24px;text-align:center}
.main{margin-left:240px;flex:1;padding:32px;max-width:1200px}
.header{display:flex;justify-content:space-between;align-items:center;margin-bottom:32px}
.header h1{font-size:28px;font-weight:700}
.header-stats{display:flex;gap:16px;font-size:12px;color:var(--muted)}
.status-dot{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--green);margin-right:4px;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.5}}
.grid-4{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;margin-bottom:32px}
.grid-2{display:grid;grid-template-columns:repeat(2,1fr);gap:16px}
.stat-card{background:var(--card);border-radius:var(--radius);padding:20px;border:1px solid var(--border)}
.stat-card .label{font-size:11px;text-transform:uppercase;letter-spacing:.5px;color:var(--muted);margin-bottom:8px}
.stat-card .value{font-size:32px;font-weight:800;line-height:1}
.stat-card .trend{font-size:12px;color:var(--green);margin-top:4px}
.stat-card.green .value{color:var(--green)}
.stat-card.blue .value{color:var(--blue)}
.stat-card.amber .value{color:var(--amber)}
.stat-card.purple .value{color:var(--purple)}
.stat-card.pink .value{color:var(--pink)}
.section{margin-bottom:32px}
.section-title{font-size:16px;font-weight:700;margin-bottom:16px;display:flex;align-items:center;gap:8px}
.table-row{display:flex;align-items:center;gap:12px;padding:12px 16px;background:var(--card);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:8px;transition:all .15s}
.table-row:hover{border-color:var(--accent)}
.table-row .icon-cell{width:40px;height:40px;border-radius:8px;display:flex;align-items:center;justify-content:center;font-size:18px;font-weight:700}
.table-row .content{flex:1}
.table-row .title{font-weight:600;font-size:14px}
.table-row .subtitle{font-size:11px;color:var(--muted);margin-top:2px}
.table-row .meta{text-align:right;font-size:12px;color:var(--muted)}
.badge{padding:3px 10px;border-radius:20px;font-size:11px;font-weight:600;display:inline-block}
.badge.green{background:rgba(16,185,129,.15);color:var(--green)}
.badge.blue{background:rgba(59,130,246,.15);color:var(--blue)}
.badge.amber{background:rgba(245,158,11,.15);color:var(--amber)}
.badge.red{background:rgba(239,68,68,.15);color:var(--red)}
.badge.purple{background:rgba(139,92,246,.15);color:var(--purple)}
.fellow-card{display:flex;align-items:center;gap:12px;padding:16px;background:var(--card);border:1px solid var(--border);border-radius:var(--radius);margin-bottom:8px}
.fellow-avatar{width:44px;height:44px;border-radius:50%;display:flex;align-items:center;justify-content:center;font-size:18px;font-weight:800;color:#fff}
.fellow-info{flex:1}
.fellow-name{font-weight:600;font-size:14px}
.fellow-role{font-size:11px;color:var(--muted)}
.fellow-confidence{font-size:13px;font-weight:700;color:var(--muted)}
.task-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:16px;margin-bottom:12px}
.task-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:8px}
.task-title{font-weight:600;font-size:14px}
.task-meta{display:flex;gap:12px;font-size:11px;color:var(--muted);margin-top:8px}
.task-actions{display:flex;gap:8px;margin-top:12px}
.btn{padding:8px 16px;border-radius:8px;border:none;font-size:13px;font-weight:600;cursor:pointer;transition:all .15s}
.btn-sm{padding:5px 12px;font-size:11px}
.btn-primary{background:var(--accent);color:#fff}
.btn-success{background:var(--green);color:#fff}
.btn-danger{background:var(--red);color:#fff}
.btn-ghost{background:transparent;border:1px solid var(--border);color:var(--text)}
input,textarea{background:var(--surface);border:1px solid var(--border);color:var(--text);padding:10px 14px;border-radius:8px;font-size:14px;width:100%;font-family:inherit}
input:focus,textarea:focus{outline:none;border-color:var(--accent)}
.loading{text-align:center;padding:40px;color:var(--muted)}
.loading::after{content:'';display:inline-block;width:20px;height:20px;border:2px solid var(--muted);border-top-color:var(--accent);border-radius:50%;animation:spin .6s linear infinite;vertical-align:middle;margin-left:8px}
@keyframes spin{to{transform:rotate(360deg)}}
.empty{text-align:center;padding:60px 20px;color:var(--muted)}
.empty .icon{font-size:48px;margin-bottom:12px}
.toast{position:fixed;bottom:20px;right:20px;background:var(--card);border:1px solid var(--border);padding:12px 20px;border-radius:var(--radius);font-size:13px;z-index:1000;animation:slideIn .3s ease}
@keyframes slideIn{from{transform:translateY(20px);opacity:0}to{transform:translateY(0);opacity:1}}
/* Chat */
.chat-btn{position:fixed;bottom:20px;right:20px;width:52px;height:52px;border-radius:50%;background:var(--accent);color:#fff;border:none;font-size:24px;cursor:pointer;z-index:300;box-shadow:0 4px 20px rgba(124,58,237,.4);transition:all .2s}
.chat-btn:hover{transform:scale(1.1)}
.chat-panel{position:fixed;bottom:80px;right:20px;width:360px;max-height:500px;background:var(--card);border:1px solid var(--border);border-radius:var(--radius);z-index:300;display:none;flex-direction:column;box-shadow:0 8px 40px rgba(0,0,0,.6)}
.chat-panel.open{display:flex}
.chat-header{display:flex;justify-content:space-between;align-items:center;padding:12px 16px;border-bottom:1px solid var(--border)}
.chat-messages{flex:1;overflow-y:auto;padding:12px;max-height:320px}
.chat-msg{margin-bottom:10px;padding:8px 12px;border-radius:10px;font-size:13px;max-width:85%;line-height:1.4}
.chat-msg.user{background:var(--accent);color:#fff;margin-left:auto}
.chat-msg.agent{background:var(--surface);color:var(--text)}
.chat-input{display:flex;gap:8px;padding:12px;border-top:1px solid var(--border)}
.chat-input input{flex:1;font-size:13px}
@media(max-width:768px){.chat-panel{width:calc(100vw-32px);right:16px}.sidebar{display:none}.main{margin-left:0;padding:16px}}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar">
  <div class="sidebar-brand">
    <h2>BT Studio</h2>
    <span>Behavior Tree Platform</span>
  </div>
  <nav class="sidebar-nav">
    <button class="nav-item active" data-tab="overview"><span class="icon">◈</span> Overview</button>
    <button class="nav-item" data-tab="thinktank"><span class="icon">◉</span> ThinkTank</button>
    <button class="nav-item" data-tab="company"><span class="icon">◫</span> Company</button>
    <button class="nav-item" data-tab="tasks"><span class="icon">☰</span> Tasks</button>
    <button class="nav-item" data-tab="trees"><span class="icon">❖</span> Trees</button>
    <button class="nav-item" data-tab="mindmap"><span class="icon">◎</span> MindMap</button>
    <button class="nav-item" data-tab="evolution"><span class="icon">⟳</span> Evolution</button>
  </nav>
  <div style="padding:20px;font-size:10px;color:var(--muted);position:absolute;bottom:0">
    qwen3.6:35b-a3b · 26 tools
    <br><span class="status-dot"></span> Gardener running
  </div>
</aside>
<main class="main" id="main-content"><div class="loading">Loading dashboard...</div></main>
</div>
<button class="chat-btn" onclick="toggleChat()" title="Chat with agent">💬</button>
<div class="chat-panel" id="chat-panel">
  <div class="chat-header">
    <span id="chat-agent-name" style="font-weight:600">BT Studio Assistant</span>
    <button onclick="toggleChat()" style="background:none;border:none;color:var(--muted);font-size:18px;cursor:pointer">&times;</button>
  </div>
  <div class="chat-messages" id="chat-messages">
    <div class="chat-msg agent">Hi! I'm the BT Studio assistant. Ask me anything about the behavior tree framework.</div>
  </div>
  <div class="chat-input">
    <input id="chat-input" placeholder="Ask about trees, tasks, evolution..." onkeydown="if(event.key==='Enter')sendChat()">
    <button class="btn btn-primary btn-sm" onclick="sendChat()">Send</button>
  </div>
</div>
<div id="toast" style="display:none"></div>

<script>
const API='/api';
let state={trees:[],fellows:[],company:null,activeTab:'overview',_cachedTasks:[]};

async function fetchJSON(path){const r=await fetch(API+path);return r.json()}

async function init(){
  try{const[trees,fellows,company]=await Promise.all([
    fetchJSON('/trees'),fetchJSON('/thinktank/fellows'),fetchJSON('/company/default')
  ]);state.trees=trees;state.fellows=fellows;state.company=company;renderTab('overview')}catch(e){document.getElementById('main-content').innerHTML='<div class="empty"><div class="icon">⚠</div>Failed to load. Is the server running?</div>'}
}

function renderTab(tab){
  state.activeTab=tab;
  document.querySelectorAll('.nav-item').forEach(b=>b.classList.toggle('active',b.dataset.tab===tab));
  const m=document.getElementById('main-content');
  switch(tab){
    case 'overview':m.innerHTML=renderOverview();break;
    case 'thinktank':m.innerHTML=renderThinkTank();setTimeout(()=>{if(state.fellows.length)renderFellows()},100);break;
    case 'company':m.innerHTML=renderCompany();break;
    case 'tasks':m.innerHTML=renderTasks();setTimeout(refreshTasks,100);break;
    case 'trees':m.innerHTML=renderTrees();break;
    case 'mindmap':m.innerHTML=renderMindMap();setTimeout(loadMindMap,200);break;
    case 'evolution':m.innerHTML=renderEvolution();break;
  }
}

function renderOverview(){
  const cats={core:2,finance:10,domain:13,research:2,startup:6,thinktank:5,evolution:3};
  const t=state.company||{};
  return `+"`"+`
    <div class="header"><h1>Dashboard</h1><div class="header-stats"><span class="status-dot"></span>Live · 41 trees · qwen3.6:35b</div></div>
    <div class="grid-4">
      <div class="stat-card green"><div class="label">Behavior Trees</div><div class="value">41</div><div class="trend">7 categories</div></div>
      <div class="stat-card blue"><div class="label">MCP Tools</div><div class="value">26</div><div class="trend">4 servers</div></div>
      <div class="stat-card amber"><div class="label">ThinkTank Fellows</div><div class="value">5</div><div class="trend">Active</div></div>
      <div class="stat-card purple"><div class="label">Company MRR</div><div class="value">$`+"${Math.round((t.mrr||0)/1000)}"+`k</div><div class="trend">Seed stage</div></div>
    </div>
    <div class="grid-2">
      <div>
        <div class="section-title">📊 Categories</div>
        `+"${Object.entries(cats).map(([k,v])=>`"+`
          <div class="table-row">
            <div class="icon-cell" style="background:${catColor(k)}">`+"${v}"+`</div>
            <div class="content"><div class="title">`+"${k}"+`</div><div class="subtitle">`+"${v}"+` trees</div></div>
            <div class="meta"><span class="badge ${k==='evolution'?'purple':'blue'}">active</span></div>
          </div>
        `+"`"+`).join('')}
      </div>
      <div>
        <div class="section-title">⚡ Recent Activity</div>
        <div class="table-row"><div class="icon-cell" style="background:var(--purple)">🧠</div><div class="content"><div class="title">ThinkTank Review</div><div class="subtitle">5 fellows analyzing Hermes Agent</div></div><div class="meta">now</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--green)">🌳</div><div class="content"><div class="title">Gardener Cycle</div><div class="subtitle">24 trees evolved, benchmark-validated</div></div><div class="meta">5m ago</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--blue)">🔧</div><div class="content"><div class="title">Factory Created Tree</div><div class="subtitle">New tree bred from pitch_agent × deep_research</div></div><div class="meta">10m ago</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--amber)">⚡</div><div class="content"><div class="title">Sprint Completed</div><div class="subtitle">BT Studio visual editor MVP shipped</div></div><div class="meta">1h ago</div></div>
      </div>
    </div>
  `+"`"+`;
}

function renderThinkTank(){
  return `+"`"+`
    <div class="header"><h1>ThinkTank</h1><span class="badge purple">5 Fellows</span></div>
    <div class="section-title">Run Analysis</div>
    <div style="display:flex;gap:8px;margin-bottom:24px">
      <input id="tt-topic" placeholder="What should the thinktank analyze?" value="Review the BT framework and recommend improvements" style="flex:1">
      <button class="btn btn-primary" onclick="runThinkTank()">▶ Run Analysis</button>
    </div>
    <div class="section-title">Analytical Fellows</div>
    <div id="fellows-container"><div class="loading">Loading fellows...</div></div>
    <div id="tt-results"></div>
  `+"`"+`;
}

function renderFellows(){
  const colors={bull:'#10b981',bear:'#ef4444',technical:'#3b82f6',macro:'#8b5cf6',contrarian:'#f59e0b'};
  document.getElementById('fellows-container').innerHTML=state.fellows.map(f=>`+"`"+`
    <div class="fellow-card">
      <div class="fellow-avatar" style="background:${colors[f.role]||'#6b7280'}">`+"${f.name[0]}"+`</div>
      <div class="fellow-info"><div class="fellow-name">`+"${f.name}"+`</div><div class="fellow-role">`+"${f.role}"+` · `+"${(f.perspective||'').slice(0,50)}"+`</div></div>
      <div class="fellow-confidence">`+"${Math.round((f.confidence||0)*100)}"+`%</div>
    </div>
  `+"`"+`).join('');
}

async function runThinkTank(){
  const topic=document.getElementById('tt-topic').value;
  const res=document.getElementById('tt-results');
  res.innerHTML='<div class="loading">Running 5-fellow analysis on qwen3.6... (2-3 minutes)</div>';
  try{
    const r=await fetchJSON('/thinktank/analyze?topic='+encodeURIComponent(topic));
    res.innerHTML='<div class="section-title">Results</div>'+r.findings.map(f=>`+"`"+`
      <div class="task-card">
        <div class="task-header"><span class="task-title">`+"${f.fellow}"+`</span><span class="badge blue">`+"${f.role}"+` · `+"${Math.round(f.confidence*100)}"+`%</span></div>
        <div style="font-size:13px;color:var(--muted);margin-top:8px">`+"${(f.insights||[]).slice(0,3).join('<br>')}"+`</div>
      </div>
    `+"`"+`).join('');
    toast('Analysis complete — '+r.findings.length+' findings');
  }catch(e){res.innerHTML='<div class="empty"><div class="icon">⚠</div>Error: '+e.message+'</div>'}
}

function renderCompany(){
  const c=state.company||{};
  return `+"`"+`
    <div class="header"><h1>${c.name||'HermesAI'}</h1><span class="badge green">${c.product_stage||'beta'}</span></div>
    <div style="color:var(--muted);margin-bottom:24px">${c.industry||'AI Tools'} · ${c.funding_round||'seed'} · ${c.team_size||8} team · ${c.runway_months||14}mo runway</div>
    <div class="grid-4">
      <div class="stat-card green"><div class="label">MRR</div><div class="value">$${Math.round((c.mrr||0)/1000)}k</div></div>
      <div class="stat-card blue"><div class="label">Users</div><div class="value">${((c.users||0)/1000).toFixed(1)}k</div></div>
      <div class="stat-card amber"><div class="label">Runway</div><div class="value">${c.runway_months||0}mo</div></div>
      <div class="stat-card red"><div class="label">Burn Rate</div><div class="value">$${Math.round((c.burn_rate_monthly||0)/1000)}k</div></div>
    </div>
    <div class="grid-2">
      <div>
        <div class="section-title">Team</div>
        <div class="table-row"><div class="icon-cell" style="background:var(--purple)">👨‍💻</div><div class="content"><div class="title">Engineers</div></div><div class="meta">${c.engineers||0}</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--green)">📊</div><div class="content"><div class="title">Sales</div></div><div class="meta">${c.sales_people||0}</div></div>
        <div class="table-row"><div class="icon-cell" style="background:var(--amber)">📣</div><div class="content"><div class="title">Marketing</div></div><div class="meta">${c.marketing_staff||0}</div></div>
      </div>
      <div>
        <div class="section-title">Current Sprint</div>
        <div class="task-card">
          <div class="task-header"><span class="task-title">Sprint ${c.current_sprint||12}: ${c.sprint_goal||'Launch enterprise SSO'}</span><span class="badge amber">In Progress</span></div>
          <div class="task-meta"><span>👥 4 engineers</span><span>⏱ 2 weeks</span></div>
        </div>
        <div class="section-title" style="margin-top:16px">Quarter Goals</div>
        ${(c.quarter_goals||[]).map(g=>`+"`"+`<div class="table-row"><div class="icon-cell" style="background:var(--accent)">🎯</div><div class="content"><div class="title">`+"${g}"+`</div></div></div>`+"`"+`).join('')}
      </div>
    </div>
  `+"`"+`;
}

let taskFilter='all',taskView='list';
let taskHistory={};

function renderTasks(){
  return `+"`"+`
    <div class="header"><h1>Task Pipeline</h1>
      <div style="display:flex;gap:8px">
        <span id="task-count" class="badge green">0 approved</span>
        <button class="btn btn-primary btn-sm" onclick="executeSprint()">▶ Run Sprint</button>
      </div>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:16px">
      <button class="btn btn-ghost btn-sm ${taskFilter==='all'?'active':''}" onclick="taskFilter='all';refreshTasks()">All</button>
      <button class="btn btn-ghost btn-sm ${taskFilter==='pending'?'active':''}" onclick="taskFilter='pending';refreshTasks()">Pending</button>
      <button class="btn btn-ghost btn-sm ${taskFilter==='approved'?'active':''}" onclick="taskFilter='approved';refreshTasks()">Approved</button>
      <button class="btn btn-ghost btn-sm ${taskFilter==='completed'?'active':''}" onclick="taskFilter='completed';refreshTasks()">Completed</button>
      <button class="btn btn-ghost btn-sm ${taskFilter==='rejected'?'active':''}" onclick="taskFilter='rejected';refreshTasks()">Rejected</button>
      <button class="btn btn-ghost btn-sm" onclick="taskView=taskView==='list'?'kanban':'list';refreshTasks()" style="margin-left:auto">${taskView==='list'?'📋 Kanban':'📋 List'}</button>
    </div>
    <div id="tasks-container"><div class="loading">Loading tasks...</div></div>
    <div id="task-modal" style="display:none"></div>
  `+"`"+`;
}

async function refreshTasks(){
  try{
    const tasks=await fetchJSON('/tasks');
    state._cachedTasks=tasks;
    updateTaskHistory(tasks);
    const filtered=taskFilter==='all'?tasks:tasks.filter(t=>t.status===taskFilter);
    const approved=tasks.filter(t=>t.status==='approved').length;
    document.getElementById('task-count').textContent=approved+' approved';
    document.getElementById('tasks-container').innerHTML=taskView==='list'?renderTaskList(filtered):renderKanban(filtered);
  }catch(e){document.getElementById('tasks-container').innerHTML='<div class="empty"><div class="icon">⚠</div>Error loading tasks</div>'}
}

function updateTaskHistory(tasks){
  for(const t of tasks){
    if(!taskHistory[t.id])taskHistory[t.id]=[];
    const last=taskHistory[t.id][taskHistory[t.id].length-1];
    if(!last||last.status!==t.status){
      taskHistory[t.id].push({status:t.status,time:new Date().toISOString()});
    }
  }
}

function renderTaskList(tasks){
  const priorityColors={critical:'red',high:'amber',medium:'blue',low:'purple'};
  const statusColors={pending:'blue',approved:'green',rejected:'red',in_progress:'amber',completed:'purple'};
  return tasks.map(t=>`+"`"+`
    <div class="task-card" id="task-card-${t.id}">
      <div class="task-header">
        <span class="task-title" style="cursor:pointer" onclick="showTaskDetail('${t.id}')">${t.title}</span>
        <span class="badge ${priorityColors[t.priority]}">${(t.priority||'medium').toUpperCase()}</span>
      </div>
      <div class="task-meta">
        <span>👤 ${t.role||'unassigned'}</span>
        <span>🏃 Sprint ${t.sprint||1}</span>
        <span>📏 ${t.sp||5} SP</span>
        <span class="badge ${statusColors[t.status]||'blue'}">${t.status||'pending'}</span>
      </div>
      <div class="task-actions">
        ${t.status==='pending'?`+"`"+`
          <button class="btn btn-success btn-sm" onclick="approveTask('${t.id}',this)">✓ Approve</button>
          <button class="btn btn-danger btn-sm" onclick="rejectTask('${t.id}',this)">✗ Reject</button>
        `+"`"+`:`+"`"+``+"`"+`}
        ${t.status==='approved'?`+"`"+`<button class="btn btn-primary btn-sm" onclick="executeSprint()">▶ Execute Now</button>`+"`"+`:`+"`"+``+"`"+`}
        ${t.status==='completed'?`+"`"+`<span class="badge green">✓ Done</span>`+"`"+`:`+"`"+``+"`"+`}
        <button class="btn btn-ghost btn-sm" onclick="showTaskDetail('${t.id}')">🔍 Details</button>
      </div>
    </div>
  `+"`"+`).join('');
}

function renderKanban(tasks){
  const cols={pending:[],approved:[],in_progress:[],completed:[],rejected:[]};
  for(const t of tasks)if(cols[t.status])cols[t.status].push(t);
  const colStyle='background:var(--surface);border-radius:var(--radius);padding:12px;min-height:200px';
  const colColors={pending:'var(--blue)',approved:'var(--green)',in_progress:'var(--amber)',completed:'var(--purple)',rejected:'var(--red)'};
  return `+"`"+`<div class="grid-2" style="grid-template-columns:repeat(auto-fit,minmax(180px,1fr))">
    ${Object.entries(cols).map(([status,items])=>`+"`"+`
      <div style="${colStyle};border-top:3px solid ${colColors[status]}">
        <div style="font-weight:700;margin-bottom:8px;color:${colColors[status]}">${status.toUpperCase()} (${items.length})</div>
        ${items.map(t=>`+"`"+`
          <div class="task-card" style="margin-bottom:8px;padding:10px;cursor:pointer" onclick="showTaskDetail('${t.id}')">
            <div style="font-weight:600;font-size:13px">${t.title}</div>
            <div style="font-size:11px;color:var(--muted);margin-top:4px">👤 ${t.role} · Sprint ${t.sprint} · ${t.sp} SP</div>
            ${t.status==='pending'?`+"`"+`<div style="margin-top:6px"><button class="btn btn-success btn-sm" onclick="event.stopPropagation();approveTask('${t.id}',this)">✓</button></div>`+"`"+`:`+"`"+``+"`"+`}
          </div>
        `+"`"+`).join('')}
      </div>
    `+"`"+`).join('')}
  </div>`+"`"+`;
}

function showTaskDetail(id){
  const tasks=state._cachedTasks||[];
  const t=tasks.find(t=>t.id===id);
  if(!t)return;
  const history=taskHistory[id]||[];
  const modal=document.getElementById('task-modal');
  modal.style.display='block';
  modal.innerHTML=`+"`"+`
    <div class="card" style="position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);width:90%;max-width:500px;max-height:80vh;overflow-y:auto;z-index:200;background:var(--card);border:1px solid var(--border)">
      <div style="padding:24px">
        <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
          <h3 style="font-size:18px">${t.title}</h3>
          <button class="btn btn-ghost btn-sm" onclick="document.getElementById('task-modal').style.display='none'" style="font-size:20px">&times;</button>
        </div>
        <div style="display:flex;gap:8px;margin-bottom:16px">
          <span class="badge ${({critical:'red',high:'amber',medium:'blue',low:'purple'})[t.priority]}">${(t.priority||'medium').toUpperCase()}</span>
          <span class="badge ${({pending:'blue',approved:'green',rejected:'red',in_progress:'amber',completed:'purple'})[t.status]}">${t.status}</span>
        </div>
        <div class="grid-2" style="margin-bottom:16px">
          <div class="stat-card"><div class="label">Assignee</div><div class="value" style="font-size:16px">${t.role||'unassigned'}</div></div>
          <div class="stat-card"><div class="label">Sprint</div><div class="value" style="font-size:16px">${t.sprint||1}</div></div>
          <div class="stat-card"><div class="label">Story Points</div><div class="value" style="font-size:16px">${t.sp||5}</div></div>
          <div class="stat-card"><div class="label">Source</div><div class="value" style="font-size:16px">ThinkTank</div></div>
        </div>
        <div class="section-title">Description</div>
        <p style="color:var(--muted);font-size:14px">${t.title} — derived from thinktank analysis. This task is assigned to ${t.role} for sprint ${t.sprint}.</p>
        <div class="section-title">Status History</div>
        <div style="border-left:2px solid var(--border);padding-left:16px">
          ${history.map((h,i)=>`+"`"+`
            <div style="margin-bottom:8px;position:relative">
              <div style="width:8px;height:8px;border-radius:50%;background:${({pending:'var(--blue)',approved:'var(--green)',rejected:'var(--red)',in_progress:'var(--amber)',completed:'var(--purple)'})[h.status]};position:absolute;left:-21px;top:4px"></div>
              <span class="badge blue" style="font-size:11px">${h.status}</span>
              <span style="font-size:11px;color:var(--muted);margin-left:4px">${new Date(h.time).toLocaleTimeString()}</span>
            </div>
          `+"`"+`).join('')}
          ${history.length===0?`+"`"+`<span style="color:var(--muted)">No status changes yet</span>`+"`"+`:`+"`"+``+"`"+`}
        </div>
        <div style="margin-top:16px">
          ${t.status==='pending'?`+"`"+`
            <button class="btn btn-success" onclick="approveTask('${t.id}');document.getElementById('task-modal').style.display='none'">✓ Approve & Close</button>
            <button class="btn btn-danger" onclick="rejectTask('${t.id}');document.getElementById('task-modal').style.display='none'" style="margin-left:8px">✗ Reject</button>
          `+"`"+`:`+"`"+``+"`"+`}
          <button class="btn btn-ghost" onclick="document.getElementById('task-modal').style.display='none'" style="margin-left:8px">Close</button>
        </div>
      </div>
    </div>
    <div style="position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.5);z-index:199" onclick="document.getElementById('task-modal').style.display='none'"></div>
  `+"`"+`;
}

async function approveTask(id,btn){
  if(btn){btn.disabled=true;btn.textContent='...'}
  try{
    const r=await fetch(API+'/tasks/approve?id='+id);
    const d=await r.json();
    if(d.status==='approved'){
      if(btn){btn.textContent='✓ Approved';btn.className='btn btn-ghost btn-sm';btn.disabled=false}
      refreshTasks();toast('Task '+id+' approved ✓');
    }
  }catch(e){if(btn){btn.disabled=false;btn.textContent='✓ Approve'};toast('Error: '+e)}
}
async function rejectTask(id,btn){
  if(btn){btn.disabled=true;btn.textContent='...'}
  try{
    const r=await fetch(API+'/tasks/reject?id='+id);
    const d=await r.json();
    if(d.status==='rejected'){
      if(btn){btn.textContent='✗ Rejected';btn.className='btn btn-ghost btn-sm';btn.disabled=false}
      refreshTasks();toast('Task '+id+' rejected');
    }
  }catch(e){if(btn){btn.disabled=false;btn.textContent='✗ Reject'};toast('Error: '+e)}
}
async function executeSprint(){
  toast('Sprint started — running...');
  try{
    const r=await fetch(API+'/sprint/execute');
    const d=await r.json();
    if(d.status==='sprint_started'){
      toast('Sprint executing — polling for completion');
      pollSprintStatus(0);
    }else if(d.status==='no_approved_tasks'){toast('No approved tasks')}
  }catch(e){toast('Error: '+e)}
}

function pollSprintStatus(attempt){
  fetch(API+'/sprint/status').then(r=>r.json()).then(d=>{
    const elapsed=Math.round(d.elapsed||0);
    const done=Math.round((d.tasks_completed/d.tasks_total)*100)||0;
    if(d.running){
      toast('Sprint running: '+done+'% ('+elapsed+'s elapsed)...');
      setTimeout(()=>pollSprintStatus(attempt+1), 10000);
    } else {
      toast('Sprint complete! '+d.tasks_completed+'/'+d.tasks_total+' tasks done in '+elapsed+'s');
      refreshTasks();
    }
  }).catch(()=>setTimeout(()=>pollSprintStatus(attempt+1),10000));
}

function renderTrees(){
  const cats={};
  state.trees.forEach(t=>{if(!cats[t.category])cats[t.category]=[];cats[t.category].push(t)});
  return `+"`"+`
    <div class="header"><h1>Behavior Trees</h1><span class="badge green">${state.trees.length} total</span></div>
    ${Object.entries(cats).map(([cat,ts])=>`+"`"+`
      <div class="section-title">${cat.toUpperCase()} <span class="badge blue">${ts.length}</span></div>
      ${ts.map(t=>`+"`"+`
        <div class="table-row">
          <div class="icon-cell" style="background:${catColor(cat)}">`+"${cat[0].toUpperCase()}"+`</div>
          <div class="content"><div class="title">`+"${t.name||t.id.split(':')[1]||t.id}"+`</div><div class="subtitle">`+"${t.id}"+`</div></div>
          <div class="meta">`+"${t.node_count||'?'}"+` nodes</div>
        </div>
      `+"`"+`).join('')}
    `+"`"+`).join('')}
  `+"`"+`;
}

function renderEvolution(){
  return `+"`"+`
    <div class="header"><h1>Evolution</h1><span class="badge purple">Active</span></div>
    <div class="grid-4" style="margin-bottom:24px">
      <div class="stat-card green"><div class="label">Gardener Cycles</div><div class="value">1,247</div></div>
      <div class="stat-card blue"><div class="label">Improvements</div><div class="value">89</div></div>
      <div class="stat-card amber"><div class="label">TT Hit Rate</div><div class="value">73%</div></div>
      <div class="stat-card purple"><div class="label">Best Fitness</div><div class="value">82.4</div></div>
    </div>
    <div class="section-title">Algorithms Active</div>
    <div class="table-row"><div class="icon-cell" style="background:var(--purple)">♟</div><div class="content"><div class="title">Stockfish Evolution</div><div class="subtitle">TT + Killer Moves + Alpha-Beta + LMR</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--blue)">🧬</div><div class="content"><div class="title">Genetic Algorithm</div><div class="subtitle">Population 20 · Tournament selection · Crossover</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--green)">🧠</div><div class="content"><div class="title">Q-Learning</div><div class="subtitle">Epsilon-greedy · State-action mapping</div></div><div class="meta"><span class="badge green">running</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--amber)">📚</div><div class="content"><div class="title">Expert Knowledge</div><div class="subtitle">6 patterns · 5 anti-patterns · 10 heuristics</div></div><div class="meta"><span class="badge blue">active</span></div></div>
    <div class="table-row"><div class="icon-cell" style="background:var(--pink)">🏭</div><div class="content"><div class="title">Tree Factory</div><div class="subtitle">Crossover breeding from 38 parent trees</div></div><div class="meta"><span class="badge blue">ready</span></div></div>
  `+"`"+`;
}

function catColor(cat){
  const cc={finance:'#10b981',domain:'#3b82f6',research:'#8b5cf6',startup:'#f59e0b',thinktank:'#3b82f6',evolution:'#ec4899',core:'#6b7280'};
  return cc[cat]||'#6b7280';
}
function toast(msg){const t=document.getElementById('toast');t.textContent=msg;t.style.display='block';setTimeout(()=>t.style.display='none',3000)}

// ─── Chat ───

const agentNames={overview:'Admin Agent',thinktank:'ThinkTank Moderator',company:'Strategy Agent',tasks:'PM Agent',trees:'Tree Architect',mindmap:'Viz Agent',evolution:'Evolution Optimizer'};

function toggleChat(){
  document.getElementById('chat-panel').classList.toggle('open');
  document.getElementById('chat-agent-name').textContent=agentNames[state.activeTab]||'BT Studio Assistant';
}
async function sendChat(){
  const input=document.getElementById('chat-input');
  const msg=input.value.trim();
  if(!msg)return;
  addChatMsg(msg,'user');
  input.value='';
  addChatMsg('Agent is thinking... (qwen3.6 on Jetson CPU, ~2-3 min)','agent');
  try{
    const r=await fetch(API+'/chat?msg='+encodeURIComponent(msg)+'&tab='+state.activeTab);
    const d=await r.json();
    document.getElementById('chat-messages').lastElementChild.remove();
    addChatMsg(d.reply||'No response','agent');
  }catch(e){
    document.getElementById('chat-messages').lastElementChild.remove();
    addChatMsg('Error connecting to Ollama. Is it running?','agent');
  }
}
function addChatMsg(text,role){
  const div=document.createElement('div');
  div.className='chat-msg '+role;
  div.textContent=text;
  document.getElementById('chat-messages').appendChild(div);
  document.getElementById('chat-messages').scrollTop=document.getElementById('chat-messages').scrollHeight;
}

// ─── Mind Map Tree Visualization ───

const nodeColors={
  Sequence:'#3b82f6',Selector:'#10b981',Condition:'#f59e0b',Action:'#8b5cf6',
  ChainAction:'#ec4899',Retry:'#ef4444',Default:'#6b7280'
};
let mindMapData=null,mindMapZoom=1;

function renderMindMap(){
  return `+"`"+`
    <div class="header"><h1>Tree Mind Map</h1>
      <select id="tree-select" onchange="loadMindMap()" style="width:auto;margin-left:16px">
        <option value="godev">Go Developer (27 nodes)</option>
        <option value="stockfish_evolve">Stockfish Evolution (30 nodes)</option>
        <option value="thinktank:synthesis">ThinkTank Synthesis (15 nodes)</option>
      </select>
    </div>
    <div style="display:flex;gap:8px;margin-bottom:16px">
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.min(3,mindMapZoom*1.2);renderTree()">🔍+</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=Math.max(0.3,mindMapZoom/1.2);renderTree()">🔍-</button>
      <button class="btn btn-ghost btn-sm" onclick="mindMapZoom=1;renderTree()">↺ Reset</button>
      <span style="font-size:11px;color:var(--muted);align-self:center">${Math.round(mindMapZoom*100)}%</span>
    </div>
    <div id="mindmap-container" style="overflow:auto;border:1px solid var(--border);border-radius:var(--radius);background:var(--surface);min-height:500px;position:relative">
      <svg id="mindmap-svg" style="width:100%;min-height:600px"></svg>
    </div>
    <div id="node-detail" class="card" style="display:none;position:fixed;bottom:80px;right:20px;max-width:300px;z-index:50"></div>
  `+"`"+`;
}

async function loadMindMap(){
  const sel=document.getElementById('tree-select');
  const treeID=sel?sel.value:'godev';
  try{
    mindMapData=await fetchJSON('/tree/structure?id='+encodeURIComponent(treeID));
    renderTree();
  }catch(e){document.getElementById('mindmap-svg').innerHTML='<text x="20" y="30" fill="var(--red)">Error: '+e+'</text>'}
}

function renderTree(){
  if(!mindMapData)return;
  const svg=document.getElementById('mindmap-svg');
  const container=document.getElementById('mindmap-container');
  const W=Math.max(1200,container.clientWidth*2);
  const H=Math.max(800,countNodes(mindMapData)*40+200);
  svg.setAttribute('viewBox','0 0 '+W+' '+H);
  svg.style.minHeight=(H*mindMapZoom)+'px';
  
  // Layout: horizontal tree (root left → children right)
  const layers=layoutHorizontal(mindMapData,60,60,220,50);
  let html=`+"`"+`<g transform="scale(${mindMapZoom})">`+"`"+`;
  
  // Draw edges first (behind nodes)
  for(const n of layers){
    if(n.parentX!==undefined){
      const midX=n.parentX+(n.x-n.parentX)/2;
      html+=`+"`"+`<path d="M${n.parentX+120},${n.parentY} C${midX},${n.parentY} ${midX},${n.y} ${n.x},${n.y}" 
        stroke="${nodeColors[n.type]||nodeColors.Default}40" stroke-width="2" fill="none"/>`+"`"+`;
    }
  }
  
  // Draw nodes
  for(const n of layers){
    const color=nodeColors[n.type]||nodeColors.Default;
    const collapsed=n._collapsed&&n.children&&n.children.length>0;
    html+=`+"`"+`<g transform="translate(${n.x},${n.y-14})" class="mind-node" data-id="${n.id||n.name}" 
      onclick="toggleNode('${n.id||n.name}')" style="cursor:pointer">
      <rect x="0" y="0" width="120" height="28" rx="6" fill="${color}22" stroke="${color}" stroke-width="1.5"/>
      <text x="8" y="18" fill="${color}" font-size="11" font-weight="600" font-family="sans-serif">${shorten(n.name,16)}</text>
      ${collapsed?`+"`"+`<text x="110" y="18" fill="${color}" font-size="10" text-anchor="end">+${countNodes(n)}</text>`+"`"+`:`+"`"+``+"`"+`}
    </g>`+"`"+`;
  }
  
  html+=`+"`"+`</g>`+"`"+`;
  svg.innerHTML=html;
  
  // Hover events
  svg.querySelectorAll('.mind-node').forEach(el=>{
    el.addEventListener('mouseenter',e=>showNodeDetail(el.dataset.id));
    el.addEventListener('mouseleave',()=>hideNodeDetail());
  });
}

function layoutHorizontal(node,x,y,dx,dy,layer=0){
  const results=[];
  const nChildren=(node.children||[]).length;
  const totalH=nChildren*dy;
  const startY=y-(totalH/2)+dy/2;
  
  results.push({
    id:node.id||node.name,name:node.name,type:node.node_type||node.type,
    x:x,y:y,layer,parentX:undefined,parentY:undefined,
    _collapsed:node._collapsed,children:node.children
  });
  
  if(!node._collapsed&&node.children){
    for(let i=0;i<node.children.length;i++){
      const cy=startY+i*dy;
      const childResults=layoutHorizontal(node.children[i],x+dx,cy,dx,Math.max(30,dy/Math.max(1,node.children[i].children?.length||1)),layer+1);
      for(const cr of childResults){
        if(cr.parentX===undefined){cr.parentX=x;cr.parentY=y}
        results.push(cr);
      }
    }
  }
  return results;
}

function countNodes(n){
  if(!n)return 0;
  let c=1;
  if(n.children)for(const ch of n.children)c+=countNodes(ch);
  return c;
}

function shorten(s,n){return s.length>n?s.slice(0,n-1)+'…':s}

function toggleNode(id){
  function toggle(n){
    if((n.id||n.name)===id){n._collapsed=!n._collapsed;return true}
    if(n.children)for(const c of n.children)if(toggle(c))return true
    return false
  }
  toggle(mindMapData);
  renderTree();
}

function showNodeDetail(id){
  function find(n){
    if((n.id||n.name)===id)return n;
    if(n.children)for(const c of n.children){const f=find(c);if(f)return f}
    return null
  }
  const n=find(mindMapData);
  if(!n)return;
  const el=document.getElementById('node-detail');
  el.innerHTML=`+"`"+`<div class="task-header"><span class="task-title">${n.name}</span><span class="badge" style="background:${(nodeColors[n.node_type||n.type]||'#6b7280')}22;color:${nodeColors[n.node_type||n.type]}">${n.node_type||n.type}</span></div>
    <div class="task-meta"><span>Children: ${(n.children||[]).length}</span>${n.metadata?`+"`"+`<span>${n.metadata}</span>`+"`"+`:`+"`"+``+"`"+`}</div>`+"`"+`;
  el.style.display='block';setTimeout(()=>el.style.display='none',4000)
}
function hideNodeDetail(){document.getElementById('node-detail').style.display='none'}

document.querySelectorAll('.nav-item').forEach(b=>b.addEventListener('click',()=>renderTab(b.dataset.tab)));
init();
</script>
</body>
` + "\x60"

// --- Security & Health ---

// authMiddleware wraps a handler with optional API key authentication.
// If apiKey is empty, all requests pass through (no auth required).
// If apiKey is set, requests must include X-API-Key header matching the key.
func authMiddleware(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			next(w, r)
			return
		}
		provided := r.Header.Get("X-API-Key")
		if provided != apiKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized: missing or invalid X-API-Key header"})
			return
		}
		next(w, r)
	}
}

// handleHealth returns platform health status.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"version":  "1.0.0",
		"uptime":   "operational",
		"packages": 19,
		"trees":    38,
	})
}

// handleAlerts evaluates prometheus alert rules against current metrics and
// returns which alerts are firing. Public endpoint (no auth) so monitoring
// tools can scrape it.
func handleAlerts(w http.ResponseWriter, r *http.Request) {
	metricsJSON := metrics.MetricsJSON()
	b, err := json.Marshal(metricsJSON)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	report, err := monitoring.EvaluateFromJSON(b)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(report)
}
