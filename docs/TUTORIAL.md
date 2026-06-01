# Go BT Platform — Interactive Tutorial

> **Estimated time:** 30-45 minutes
> **Prerequisites:** Go 1.23+, git, a terminal

This hands-on tutorial walks you through building, running, and extending the Go
Behavior Tree platform. By the end, you'll have:

- Run the full test suite
- Built all 8 binaries
- Started the dashboard and explored the UI
- Executed tasks through behavior trees
- Created a custom behavior tree
- Watched the evolution engine improve trees

Each section includes **commands to run** (copy-paste ready) and
**expected output** so you know you're on track.

---

## 1. Setup (2 min)

### 1.1 Clone and enter the repo

```bash
git clone https://github.com/nico/go-bt-evolve.git
cd go-bt-evolve
```

**Expected:** Directory listing shows `cmd/`, `internal/`, `docs/`, `go.mod`.

### 1.2 Verify Go version

```bash
go version
```

**Expected:** `go version go1.23.x linux/arm64` (or amd64). 1.23+ required.

### 1.3 Download dependencies

```bash
go mod download
```

**Expected:** Downloads complete without errors. Subsequent commands will use the module cache.

> **✅ Checkpoint:** You're in the repo, Go is ready, deps are cached.

---

## 2. Run Tests (3 min)

The platform has over 1,500 tests across 34 packages. Fast tests run in seconds without Ollama.

### 2.1 Fast test suite (no LLM needed)

```bash
go test -short -count=1 -timeout 60s ./...
```

**Expected:** All packages pass. Look for `ok` lines like:
```
ok  	github.com/nico/go-bt-evolve/internal/engine	0.123s
ok  	github.com/nico/go-bt-evolve/internal/security	0.045s
...
```

> **💡 TIP:** Use `-v` for verbose output: `go test -short -count=1 -v ./internal/engine/`

### 2.2 Coverage snapshot

```bash
go test -short -count=1 -coverprofile=/tmp/bt-coverage.out ./... 2>&1 | grep coverage
```

**Expected:** Per-package coverage percentages (engine ~68%, security ~90%, etc.)

### 2.3 Check for common issues

```bash
go vet ./...
```

**Expected:** No output (or only informational warnings). Zero errors = clean.

> **✅ Checkpoint:** All fast tests pass, `go vet` is clean.

---

## 3. Build All Binaries (2 min)

The platform has 8 binaries. Let's build them.

### 3.1 Create the bin directory

```bash
mkdir -p bin
```

### 3.2 Build the core MCP servers

```bash
go build -o bin/bt-agent ./cmd/bt-agent/
go build -o bin/bt-evaluator ./cmd/bt-evaluator/
go build -o bin/bt-langagent ./cmd/bt-langagent/
```

**Expected:** Three binaries created in `bin/`. Verify:
```bash
ls -lh bin/bt-*
```

### 3.3 Build the dashboard and gardener

```bash
go build -o bin/bt-dashboard ./cmd/bt-dashboard/
go build -o bin/bt-gardener ./cmd/bt-gardener/
```

**Expected:** Two more binaries in `bin/`.

### 3.4 Build utility binaries

```bash
go build -o bin/benchcmp ./cmd/benchcmp/
```

**Expected:** All 8 binaries exist:
```bash
ls bin/
# bt-agent  bt-dashboard  bt-evaluator  bt-gardener  bt-langagent  benchcmp
```

> **✅ Checkpoint:** All 8 binaries built successfully.

---

## 4. Start the Dashboard (3 min)

The dashboard is a web UI on port 9800 with 7 tabs: Overview, ThinkTank, Company,
Tasks, Trees, MindMap, and Evolution.

### 4.1 Start the server

```bash
./bin/bt-dashboard &
```

**Expected:** Server starts on port 9800. No errors in output.

### 4.2 Verify it's running

```bash
curl -s http://localhost:9800/api/health | head -c 200
```

**Expected:** JSON response with `"status":"ok"` and component statuses.

### 4.3 Explore the API

```bash
# Platform summary
curl -s http://localhost:9800/api/summary | python3 -m json.tool | head -20

# OpenAPI spec
curl -s http://localhost:9800/api/openapi.json | python3 -m json.tool | head -20

# Scalability snapshot
curl -s http://localhost:9800/api/scalability | python3 -m json.tool
```

**Expected:** Structured JSON for each endpoint.

> **💡 TIP:** Open `http://localhost:9800` in a browser for the full dashboard with charts, mind maps, and the chat panel.

> **✅ Checkpoint:** Dashboard running, health endpoint responds, APIs return valid JSON.

---

## 5. Execute Tasks Through Behavior Trees (5 min)

Behavior trees route tasks through decision nodes (Selectors) and action sequences.
Let's run tasks through different trees.

### 5.1 Explore available trees

```bash
# List all registered trees
curl -s http://localhost:9800/api/trees | python3 -m json.tool | grep '"name"' | head -15
```

**Expected:** 40+ tree names across categories: godev, code_review, devops_ci,
finance:pitch_agent, research:deep_research, startup:ceo, thinktank, merged, etc.

### 5.2 Run a task through the GoDev tree

If you have `mcporter` or `hermes` CLI available:

```bash
# Via mcporter (if installed):
# mcporter call bt-agent bt_run_task '{"task":"Review this Go code for nil pointer bugs"}'

# Via curl to the dashboard's agent endpoint:
curl -s -X POST http://localhost:9800/api/agents/execute \
  -H "Content-Type: application/json" \
  -d '{"agent":"godev","task":"Review this Go code for nil pointer bugs"}' | python3 -m json.tool
```

**Expected:** AgentResult with output, duration, and success status.

### 5.3 Try different task types

```bash
# Research task → deep_research tree
curl -s -X POST http://localhost:9800/api/agents/execute \
  -H "Content-Type: application/json" \
  -d '{"agent":"research:deep_research","task":"Research the impact of behavior trees on autonomous AI agents"}'

# Finance task → pitch_agent tree  
curl -s -X POST http://localhost:9800/api/agents/execute \
  -H "Content-Type: application/json" \
  -d '{"agent":"finance:pitch_agent","task":"Build a DCF model for a SaaS company with $10M ARR"}'
```

**Expected:** Each task routes to the correct domain tree, produces domain-specific output.

> **💡 TIP:** The `merged` tree auto-routes tasks to the best domain tree based on keywords.

> **✅ Checkpoint:** Tasks execute through behavior trees, domain routing works correctly.

---

## 6. Create Your First Custom Behavior Tree (10 min)

Let's build a tree from scratch using the knowledge graph and agent factory.

### 6.1 Explore the knowledge graph

```bash
# View knowledge graph stats (via API)
curl -s http://localhost:9800/api/summary | python3 -m json.tool | grep -A5 knowledge
```

### 6.2 Create a tree programmatically (Go code)

Create a file `cmd/my-tree/main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func main() {
	// Build a simple priority router tree
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "PriorityRouter",
		Children: []evolution.SerializableNode{
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Condition", Name: "HasClearTask"},
				},
			},
			{
				Type: "Selector",
				Name: "StrategyRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence",
						Name: "BugFixPath",
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasBugKeyword"},
							{Type: "ChainAction", Name: "agent:fix bugs in: {{.Task}}",
								Metadata: map[string]any{
									"max_tokens": float64(400),
								}},
							{Type: "Action", Name: "MarkSuccessful"},
						},
					},
					{
						Type: "Sequence",
						Name: "GeneralPath",
						Children: []evolution.SerializableNode{
							{Type: "ChainAction", Name: "agent:handle: {{.Task}}",
								Metadata: map[string]any{
									"max_tokens": float64(400),
								}},
							{Type: "Action", Name: "MarkSuccessful"},
						},
					},
				},
			},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{Type: "Action", Name: "SelfCorrect"},
				},
			},
		},
	}

	data, _ := json.MarshalIndent(tree, "", "  ")
	fmt.Println(string(data))
}
```

Build and run:
```bash
go run ./cmd/my-tree/
```

**Expected:** JSON representation of the tree structure.

### 6.3 Register your tree

Add to the engine's tree registry. In real usage, trees are registered via the
agent factory or MCP `bt_create_agent` tool. For this tutorial, we'll use the
existing tree registration pattern:

```go
// In cmd/bt-agent/main.go, add your tree to the resolveTree function.
// See the existing resolveTree for examples.
```

> **✅ Checkpoint:** You understand tree structure (Sequence → Selector → Condition/Action) and how nodes compose into executable behavior trees.

---

## 7. Understand Tree Evolution (5 min)

The gardener daemon continuously evolves trees using Stockfish-inspired algorithms.

### 7.1 Check the metrics tracker

```bash
# If gardener has been running:
cat ~/.go-bt-gardener/gardener-metrics.json | python3 -m json.tool | head -30
```

### 7.2 Explore the evolution algorithms

The platform has 7 evolution algorithms:

| Algorithm | What it does | File |
|-----------|-------------|------|
| **Expert Knowledge** | Applies 6 proven design patterns, blocks 5 anti-patterns | `expert.go` |
| **Genetic Algorithm** | Tournament selection, crossover, elite preservation | `learning.go` |
| **Q-Learning** | Epsilon-greedy state→action mutation selection | `learning.go` |
| **Stockfish (chess-adapted)** | TT, killer moves, alpha-beta pruning, iterative deepening | `../evaluator/` |
| **Decision Tree (C4.5/CART)** | Entropy/Gini-based Selector optimization | `decision_tree.go` |
| **Memetic Local Search** | Hill Climbing, Simulated Annealing, Tabu Search | `local_search.go` |
| **Ensemble Methods** | Voting, weighted, stacking, boosting | `ensemble.go` |

### 7.3 View quality gates

```bash
grep -n "QualityGate\|MinComposite\|MaxRegression\|ConsecutiveFailures" internal/evolution/quality_gate.go | head -10
```

**Expected:** Quality gate implementation with auto-rollback on regression.

> **✅ Checkpoint:** You understand the evolution pipeline and how trees improve automatically.

---

## 8. Explore the Dashboard UI (5 min)

If you have a browser, open `http://localhost:9800`. Key features:

### 8.1 Overview tab
- 4 stat cards (agents, trees, cycles, fitness)
- 7 category breakdowns
- Recent activity timeline

### 8.2 ThinkTank tab
- 5 analytical fellows (Bull, Bear, Technical, Macro, Contrarian)
- Hegelian dialectic analysis pipeline
- Run Analysis button for real LLM-powered research

### 8.3 Company tab
- Startup simulation with 6 role trees (CEO, CTO, PM, Engineer, Marketing, Sales)
- MRR, runway, team metrics
- Sprint/quarter/year simulation controls

### 8.4 Tasks tab
- Kanban board with approve/reject
- Task priority filter
- Detail modal with status history

### 8.5 Trees tab
- All 40+ trees grouped by category
- View tree structure (JSON)

### 8.6 MindMap tab
- SVG-based horizontal tree visualization
- Color-coded nodes (Sequence=blue, Selector=green, Condition=amber, Action=purple)
- Zoom and pan controls

### 8.7 Evolution tab
- Gardener cycles and metrics
- Algorithm status
- Transposition table hit rate

### 8.8 Chat panel
- 💬 button (bottom-right)
- 7 specialized agents
- qwen3.6:35b powered (~2-3 min response on CPU)

---

## 9. Next Steps

### 9.1 Run with real Ollama (optional)

```bash
# Pull the model
ollama pull qwen3.6:35b-a3b

# Start Ollama
ollama serve &

# Run full test suite (takes 20+ min on CPU)
go test -count=1 -timeout 600s ./...
```

### 9.2 Start the evolution daemon

```bash
./bin/bt-gardener &
# Check progress:
cat ~/.go-bt-gardener/gardener-metrics.json
```

### 9.3 Register with Hermes Agent

```bash
hermes mcp add bt-agent --command $(pwd)/bin/bt-agent
hermes mcp add bt-evaluator --command $(pwd)/bin/bt-evaluator
hermes mcp add bt-langagent --command $(pwd)/bin/bt-langagent
```

### 9.4 Explore deeper

| Resource | Path |
|----------|------|
| Architecture Decision Records | `docs/adr/` |
| API Reference | `docs/API_REFERENCE.md` |
| Getting Started Guide | `docs/GETTING_STARTED.md` |
| Runner Setup | `docs/runner-setup.md` |
| Graphify Code Map | `graphify-out/GRAPH_REPORT.md` |
| Changelog | `CHANGELOG.md` |

### 9.5 Run continuous integration locally

```bash
# Full local CI pipeline (lint → vet → test → build)
make ci
```

---

## Troubleshooting

### "go: command not found"
```bash
export PATH=$PATH:/usr/local/go/bin
```

### Ollama not responding
```bash
# Check if Ollama is running
curl -s http://localhost:11434/api/tags

# If not, start it
ollama serve &
```

### Dashboard port already in use
```bash
# Find and kill the old process
lsof -ti:9800 | xargs kill -9
```

### Tests timing out on slow hardware
```bash
# Run only fast tests (no Ollama)
go test -short -count=1 -timeout 120s ./...

# Run a specific package
go test -short -count=1 -timeout 30s ./internal/engine/
```

### "Failed to build"
```bash
# Verify Go version
go version  # must be 1.23+

# Clean and rebuild
go clean -cache
go mod tidy
go build ./...
```

---

## Tutorial Complete! 🎉

You've successfully:
1. ✅ Set up the Go BT Platform
2. ✅ Run the full test suite
3. ✅ Built all 8 binaries
4. ✅ Started and explored the dashboard
5. ✅ Executed tasks through behavior trees
6. ✅ Created a custom behavior tree
7. ✅ Understood the evolution engine
8. ✅ Explored the dashboard UI

**What you've built knowledge of:**
- Behavior trees as composable decision structures
- ChainAction nodes for LLM-powered execution
- The evolution pipeline with 7 algorithms
- The dashboard with 7 interactive tabs
- MCP integration for Hermes Agent
- Quality gates preventing regression

For questions, consult the Architecture Decision Records (`docs/adr/`)
or the full API Reference (`docs/API_REFERENCE.md`).
