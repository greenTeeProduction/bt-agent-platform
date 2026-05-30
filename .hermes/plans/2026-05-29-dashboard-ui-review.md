# BT Dashboard UI — In-Depth Review & Improvement Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.
> **Urgency:** High — 80% of dashboard is fake/hardcoded data. Chat is broken. Mobile is dead.
> **Architecture decision:** Keep Go backend, replace monolith HTML string with separate static files (templates + JS). Use build-time embedding.

**Goal:** Transform the BT Dashboard from a hardcoded demo into a live, real-time monitoring and management interface for the BT Agent Platform.

**Tech Stack:** Go 1.26 (backend), vanilla HTML/CSS/JS (frontend — no framework bloat on Jetson), SSE for real-time updates.

---

## Review: What's Broken

### 🔴 Critical — Fake/Stale Data (80% of dashboard)

| Tab | What it shows | Reality | Fix needed |
|-----|---------------|---------|------------|
| **Overview** | "41 trees, 26 MCP tools, 7 categories" | Hardcoded constants in `handleSummary()` | Fetch from KG + MCP tool registry at runtime |
| **Overview** | "Recent Activity: now, 5m ago, 10m ago, 1h ago" | Hardcoded HTML strings | Pull from agent run history + scheduler logs |
| **ThinkTank** | 5 fellows, analysis button | Works but takes 2-3 min, no progress indicator | Add SSE streaming for analysis progress |
| **Company** | MRR $0k, 0 users, 0mo runway | Startup sim is functional but data is fake | Wire to real startup simulation state |
| **Tasks** | 6 hardcoded tasks | `init()` seeds them, but sprint execution is a no-op | Wire sprint execution to actual BT agent runs |
| **Trees** | List of 41 trees with categories | API returns real tree list (dynamic) but node_counts are `?` for most | ✅ This tab is partially real — just needs node counts |
| **MindMap** | 3 hardcoded tree structures | 38 real trees exist; only 3 have hardcoded JSON | Use tree serializer to generate real structure from KG |
| **Evolution** | "1,247 cycles, 89 improvements, 73% hit rate" | Completely fake | Read from gardener-metrics.json or gardener API |

### 🔴 Critical — Chat Is Broken

```go
// cmd/bt-dashboard/main.go:204 — synchronous, blocking, no streaming
reply, err := sharedLLM.Generate(sys + "\n\nUser: " + msg)
```

- Comment says "~2-3 min" on Jetson — impractical for interactive use
- No streaming, no typing indicators, no conversation history
- Uses Ollama directly instead of the MCP agent chain
- Floating button pattern is bolted-on, not integrated

### 🟡 Major — Architecture

1. **Monolithic 1337-line Go file** — 60KB HTML string constant `htmlDashboard` is unreadable, unmaintainable, and blocks IDE features
2. **No separation of concerns** — HTML, CSS, JS all in one string in one Go file
3. **No live updates** — Zero WebSocket, zero SSE, zero polling. Everything is snapshot-on-load.
4. **No error recovery** — Failed API calls show "Error loading" with no retry
5. **Mobile is broken** — Sidebar gets `display:none` with no hamburger menu
6. **Hardcoded model name** — "qwen3.6:35b-a3b" hardcoded in 3 places, not from config

### 🟡 Major — Missing Integrations

| Feature | Status | API exists? |
|---------|--------|-------------|
| **BT Agent list + status** | ❌ Not visible | Yes — `bt_agent_list` via MCP |
| **Agent run history** | ❌ Not visible | Yes — `bt_agent_history` via MCP |
| **Cron job list + management** | ❌ Not visible | Yes — cronjob tool |
| **Live system health** | ❌ Fake "Recent Activity" | Yes — monitor agent produces real reports |
| **Gardener metrics** | ❌ Fake evolution tab | Yes — `gardener-metrics.json` (888 cycles) |
| **DLQ viewer** | ❌ No UI | Yes — `/api/dlq` endpoint |
| **OpenAPI spec** | ❌ Not linked in UI | Yes — `/api/openapi.json` |
| **Dark/light toggle** | ❌ Not present | N/A |
---

## Improvement Plan

### Phase 1: Extract Frontend (Foundation)

**Goal:** Move HTML/CSS/JS out of Go string constant into maintainable static files.

#### Task 1.1: Create static file structure

```
cmd/bt-dashboard/
├── main.go              (backend only — route registration, handlers)
├── static/
│   ├── index.html       (shell HTML)
│   ├── css/
│   │   ├── base.css     (variables, reset, layout)
│   │   ├── components.css (cards, badges, buttons, tables)
│   │   └── chat.css     (chat panel styles)
│   └── js/
│       ├── app.js       (init, routing, state)
│       ├── tabs/
│       │   ├── overview.js
│       │   ├── thinktank.js
│       │   ├── company.js
│       │   ├── tasks.js
│       │   ├── trees.js
│       │   ├── mindmap.js
│       │   ├── evolution.js
│       │   └── agents.js     ← NEW
│       ├── components/
│       │   ├── chat.js
│       │   ├── toast.js
│       │   └── modal.js
│       └── lib/
│           ├── api.js        (fetch helper with retry)
│           └── sse.js        (SSE client for real-time)
```

Use Go 1.16+ `embed` directive to embed at build time:

```go
//go:embed static/*
var staticFiles embed.FS

func serveDashboard(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/" {
        data, _ := staticFiles.ReadFile("static/index.html")
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write(data)
        return
    }
    http.FileServer(http.FS(staticFiles)).ServeHTTP(w, r)
}
```

**Files:** Create ~15 files, modify `main.go`
**Risk:** Low — pure extraction, no behavior changes

#### Task 1.2: CSS redesign — Linear-inspired dark theme

Current design is functional but generic. Apply a Linear-inspired design system:

- **Typography:** Inter (Google Fonts), tighter letter-spacing (-0.02em), monospace for data
- **Colors:** Deeper background (#000000 → #0d0d0d), softer borders, blue-purple accent gradient
- **Spacing:** Consistent 8px grid, more breathing room
- **Components:** Softer shadows, border-radius 6px, subtle hover states
- **Sidebar:** Compact, icon+label, active state with left-border accent

Reference: `skill_view(name="popular-web-designs", file_path="templates/linear.app.md")`

**Files:** Modify `static/css/*.css`
**Risk:** Low — purely visual

#### Task 1.3: Mobile responsive sidebar

Add hamburger menu for mobile. Sidebar slides in from left on toggle.

```css
@media(max-width:768px){
  .sidebar{transform:translateX(-100%);transition:transform .2s}
  .sidebar.open{transform:translateX(0)}
  .hamburger{display:block;position:fixed;top:16px;left:16px;z-index:200}
  .main{margin-left:0}
}
```

**Files:** Modify `static/css/base.css`, `static/js/app.js`
**Risk:** Low

---

### Phase 2: Live Data (Real-Time)

**Goal:** Replace hardcoded data with real runtime values. Add SSE for live updates.

#### Task 2.1: Add SSE endpoint for real-time metrics

```go
// New: /api/stream — Server-Sent Events
func handleStream(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, _ := w.(http.Flusher)
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            data := collectLiveMetrics()
            fmt.Fprintf(w, "data: %s\n\n", jsonBytes(data))
            flusher.Flush()
        }
    }
}

func collectLiveMetrics() map[string]interface{} {
    return map[string]interface{}{
        "timestamp":  time.Now().Unix(),
        "agents":     getAgentStates(),        // from MCP bt_agent_list
        "cron_jobs":  getCronJobStates(),      // from Hermes cronjob list
        "trees":      kg.TreeStats(),          // from knowledge graph
        "system":     getSystemHealth(),       // disk, memory, processes
    }
}
```

**Files:** Create `internal/dashboard/metrics.go`, modify `main.go`
**Risk:** Medium — new SSE pattern, needs testing

#### Task 2.2: Replace hardcoded Overview stats with live data

| Current (hardcoded) | → | Live source |
|-----|---|-------------|
| "41 trees" | → | `len(kg.Trees)` |
| "26 MCP tools" | → | `len(btAgentServer.Tools)` |
| "5 fellows" | → | `len(thinktank.DefaultFellows())` |
| "Recent Activity" | → | Agent run history (last 10 events) |
| "qwen3.6:35b" | → | Config.LLM.Model |

**Files:** Modify `handleSummary()`, create `internal/dashboard/live.go`
**Risk:** Low

#### Task 2.3: Replace fake Evolution tab with gardener metrics

Read from `~/.go-bt-gardener/gardener-metrics.json`:

```go
func handleEvolution(w http.ResponseWriter, r *http.Request) {
    data, _ := os.ReadFile(gardenerMetricsPath)
    var metrics GardenerMetrics
    json.Unmarshal(data, &metrics)
    // Return: cycles, improvements, fitness_scores, tree_counts, etc.
    json.NewEncoder(w).Encode(metrics)
}
```

**Files:** Modify `main.go`, add `internal/dashboard/evolution.go`
**Risk:** Low — read existing file

#### Task 2.4: Make MindMap dynamic — use tree serializer

Instead of 3 hardcoded trees, serialize real trees from the knowledge graph:

```go
func handleTreeStructure(w http.ResponseWriter, r *http.Request) {
    treeID := r.URL.Query().Get("id")
    tree := kg.GetTree(treeID)
    if tree == nil {
        http.Error(w, "tree not found", 404)
        return
    }
    // Serialize the real tree (not hardcoded JSON)
    json.NewEncoder(w).Encode(serializeTree(tree))
}
```

The tree select dropdown should populate from the real `/api/trees` list.

**Files:** Modify `handleTreeStructure()`, `buildTreeJSON()`, update mindmap JS
**Risk:** Medium — need to handle all tree shapes

---

### Phase 3: BT Agent & Cron Job Integration

**Goal:** Make BT agents and cron jobs visible and manageable from the dashboard.

#### Task 3.1: New "Agents" tab

Show the 3 registered BT agents with:
- Name, description, tree type
- Status (created/scheduled/running)
- Success rate, total runs, avg quality
- Last run time and outcome
- Schedule info
- "Run Now" button
- Run history (last 10)

```go
func handleAgentsList(w http.ResponseWriter, r *http.Request) {
    // Call bt_agent_list via MCP (or internal agent registry)
    agents := agentRegistry.List()
    json.NewEncoder(w).Encode(agents)
}

func handleAgentRun(w http.ResponseWriter, r *http.Request) {
    name := r.URL.Query().Get("name")
    task := r.URL.Query().Get("task")
    result := agentRegistry.RunNow(name, task)
    json.NewEncoder(w).Encode(result)
}
```

**Files:** Create `static/js/tabs/agents.js`, add handlers in `main.go`
**Risk:** Medium — MCP integration

#### Task 3.2: Cron Job visibility

Add a "Cron Jobs" section (either as a sub-tab or in Agents):
- List all 7 cron jobs with name, schedule, last run, status
- Pause/Resume/Remove buttons
- "Run Now" button

```go
func handleCronJobsList(w http.ResponseWriter, r *http.Request) {
    // Use Hermes cronjob tool or read from cron store
    jobs := cronStore.List()
    json.NewEncoder(w).Encode(jobs)
}
```

**Files:** Modify `static/js/tabs/agents.js` or new `cron.js`, add handlers
**Risk:** Medium — need to call Hermes cronjob tool from Go

---

### Phase 4: Fix Chat

**Goal:** Make chat actually useful, not the 2-3 minute blocker it is now.

#### Task 4.1: Remove floating chat button, add inline chat panel

Integrate chat into the main layout as a collapsible right panel (not floating):

```
+--------+------------------+-----------+
| Sidebar| Main Content     | Chat (300px)|
|        |                  | [collapsible]|
+--------+------------------+-----------+
```

**Files:** Modify `static/index.html`, `static/css/chat.css`
**Risk:** Low

#### Task 4.2: Chat uses DeepSeek (not Ollama)

Current code uses `sharedLLM` which points to Ollama. Switch to DeepSeek v4 (what Hermes uses):

```go
// In handleChat:
// Use the gateway's configured LLM, not local Ollama
// OR route through the bt-agent MCP server which has the LLM chain
```

Better approach: Chat calls should go through the Hermes MCP bt-agent's `llm_call` chain, which already has proper provider config.

**Files:** Modify `handleChat()`
**Risk:** Low — config change

#### Task 4.3: Add SSE streaming to chat

```go
func handleChatStream(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/event-stream")
    // Stream LLM tokens as they arrive
    for token := range llmStream {
        fmt.Fprintf(w, "data: {\"token\": %q}\n\n", token)
        flusher.Flush()
    }
}
```

**Files:** Modify `handleChat()`, add SSE version, update `static/js/components/chat.js`
**Risk:** Medium — streaming requires LLM provider support

---

### Phase 5: Polish

#### Task 5.1: Add search/filter to Trees tab

Filter 38+ trees by name, category, or keyword.

**Files:** Modify `static/js/tabs/trees.js`
**Risk:** Low

#### Task 5.2: Add DLQ viewer

Simple table showing dead-lettered tasks with Replay/Purge buttons.

**Files:** Add DLQ section or tab, existing API handlers are ready
**Risk:** Low

#### Task 5.3: Dark/Light toggle

Add theme toggle with localStorage persistence.

**Files:** Modify `static/css/base.css`, `static/js/app.js`
**Risk:** Low

#### Task 5.4: Keyboard shortcuts

- `1-7`: Switch tabs
- `/`: Focus search
- `Esc`: Close modal/chat
- `?`: Show shortcuts

**Files:** Modify `static/js/app.js`
**Risk:** Low

---

## Implementation Order

```
Week 1: Phase 1.1 (Extract frontend) → Phase 1.2 (CSS redesign) → Phase 1.3 (Mobile)
Week 2: Phase 2.1 (SSE) → Phase 2.2 (Live overview) → Phase 2.3 (Live evolution) → Phase 2.4 (Dynamic mindmap)
Week 3: Phase 3.1 (Agents tab) → Phase 3.2 (Cron jobs) → Phase 4 (Fix chat)
Week 4: Phase 5 (Polish — search, DLQ, shortcuts, theme)
```

## Files Summary

| File | Action | Phase |
|------|--------|-------|
| `cmd/bt-dashboard/main.go` | **Heavy refactor** — extract HTML, add SSE, add new handlers, fix chat | 1-4 |
| `cmd/bt-dashboard/static/index.html` | **Create** — shell HTML | 1 |
| `cmd/bt-dashboard/static/css/*.css` | **Create** — 3 CSS files | 1 |
| `cmd/bt-dashboard/static/js/app.js` | **Create** — router, state, init | 1 |
| `cmd/bt-dashboard/static/js/tabs/*.js` | **Create** — 8 tab modules | 1-3 |
| `cmd/bt-dashboard/static/js/components/*.js` | **Create** — chat, toast, modal | 1, 4 |
| `cmd/bt-dashboard/static/js/lib/api.js` | **Create** — fetch helper with retry | 1 |
| `cmd/bt-dashboard/static/js/lib/sse.js` | **Create** — SSE client | 2 |
| `internal/dashboard/live.go` | **Create** — live metrics collector | 2 |
| `internal/dashboard/evolution.go` | **Create** — gardener metrics reader | 2 |
| `internal/dashboard/agents.go` | **Create** — agent registry bridge | 3 |

## Anti-Goals (what NOT to do)

- ❌ Don't add a JS framework (React/Vue/Svelte) — Jetson is ARM64, keep it vanilla
- ❌ Don't add npm/webpack build step — Go embed handles static files
- ❌ Don't make a separate SPA build process — everything in Go binary
- ❌ Don't remove any existing API endpoints — keep backward compat
- ❌ Don't touch the MCP server, scheduler, or engine — dashboard-only changes
