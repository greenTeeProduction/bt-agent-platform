# BT Platform Troubleshooting Guide

Common issues and their fixes when working with the Go Behavior Tree platform.

## Quick Diagnostics

```bash
# Is Ollama running?
curl -s http://localhost:11434/api/tags | jq '.models | length'

# Are MCP servers alive?
ps aux | grep 'bin/bt-' | grep -v grep

# Is the dashboard up?
curl -s http://localhost:9800/api/health

# Check gardener cycles
cat ~/.go-bt-gardener/gardener-metrics.json | jq '.cycles'

# Check disk space
df -h / /mnt/ssd
```

---

## Build & Compilation

### "package ... is not in GOROOT"

**Symptom:** `go build` fails with module import errors.

**Fix:** Ensure you're inside the module directory and Go is on PATH:
```bash
export PATH=$PATH:/usr/local/go/bin
cd ~/go-bt-evolve
go build ./...
```

### Cross-file type collisions

**Symptom:** `SelectorStats redeclared in this package` or similar duplicate type errors.

**Root cause:** When auto-generating evolution code across multiple files, type names can collide. For example, `decision_tree.go` and `selector_optimizer.go` both defined `SelectorStats`.

**Fix:** Rename the weaker definition or consolidate into a single file. Always scan existing types before adding new ones.

### Dashboard raw string literal issues

**Symptom:** Code added after the dashboard HTML constant disappears or causes syntax errors.

**Root cause:** The dashboard HTML uses Go raw string literals (backticks). Code added immediately after the closing backtick gets swallowed.

**Fix:** Place new functions before the raw string literal or in a separate file. Use `git checkout` to revert if this happens.

---

## Runtime Issues

### Tree reports "success" but produces garbage output

**Symptom:** `bt_run_task` returns `outcome: success` but the output is 3-5 words of nonsense.

**Root cause:** `max_tokens` set below 100 (e.g., 5 or 10) silently truncates all LLM output. The `WasSuccessful` condition checks `bb.Outcome`, not output quality.

**Fix:** Set `max_tokens` ≥ 400 for all ChainAction nodes:
```bash
# Audit all ChainAction nodes for low max_tokens
grep -rn 'max_tokens.*:[[:space:]]*[0-9][0-9]*' internal/ | grep -vE 'float64\(1[0-9][0-9]|float64\([2-9][0-9][0-9]'
```

### Empty outcome with no error

**Symptom:** Task produces empty outcome, zero routing, no error message. Tree never reaches any execution path.

**Root cause:** The `HasClearTask` condition in PreGate rejected the task. The task didn't contain a recognized verb or question pattern.

**Fix:** Add the missing verb to the `HasClearTask` condition in `internal/engine/tree.go`. Current verb list: build, fix, add, create, implement, write, debug, test, deploy, review, refactor, analyze, optimize, update, remove, migrate, upgrade, configure, setup, run, design, explain, show, find, generate, make, check, search, list, audit, summarize, investigate, research, evaluate, validate.

### Tree times out at 600s without completing

**Symptom:** The `hermes_improve` binary or any tree with multiple agent nodes times out.

**Root cause:** On Jetson ARM64 CPU, each Ollama call takes 2-4 minutes. Trees with 7+ ChainAction nodes can take 20-40 minutes total, exceeding the 600s foreground timeout.

**Fix:** Use single-agent trees for self-evolution on Jetson. The gardener handles multi-tree evolution with proper memory bounds. For manual execution, use background mode with `notify_on_complete=true`.

### OOM kill (exit 137)

**Symptom:** Process dies with exit code 137 (SIGKILL from OOM killer).

**Root cause:** Multi-agent-node trees load qwen3.6:35b (~24 GB per instance). Sequential execution accumulates memory.

**Fix:** Use single-agent trees on Jetson. The gardener runs one tree at a time with proper memory bounds.

---

## MCP Server Issues

### All LLM-dependent services crash immediately

**Symptom:** bt-agent, bt-gardener, bt-langagent all crash with `parse "0.0.0.0:11434": first path segment in URL cannot contain colon`. bt-evaluator and bt-dashboard work fine.

**Root cause:** `~/.bashrc` has `export OLLAMA_HOST=0.0.0.0:11434` without the `http://` scheme. Go's `net/url.Parse()` requires a scheme.

**Fix:**
```bash
# Check current value
env | grep OLLAMA_HOST
# Fix: add http:// scheme
export OLLAMA_HOST=http://0.0.0.0:11434
# Add to ~/.bashrc for persistence
```

### Gateway restart loop (hundreds of restarts)

**Symptom:** Hermes gateway keeps killing and restarting MCP server processes.

**Root cause:** A single slow Ollama call (2-4 min) blocks the stdin/stdout loop, causing the gateway to timeout and kill the process.

**Fix:** The MCP server now uses goroutines with a 3-call semaphore for concurrent tool execution. Ensure binaries are built with the latest `internal/mcp/server.go`.

### MCP tools return ClosedResourceError after process kill

**Symptom:** After `pkill -f bt-agent`, MCP tools return `ClosedResourceError`.

**Root cause:** Hermes gateway connections are stale after process kill. The gateway does NOT automatically reconnect mid-session.

**Fix:** After killing and rebuilding an MCP binary:
```bash
# 1. Verify the binary works
hermes mcp test bt-agent
# 2. Reset the Hermes session to re-establish connections
# (use /reset in Hermes chat, or restart the gateway)
```

### Duplicate MCP processes

**Symptom:** Multiple instances of bt-agent, bt-evaluator, or bt-langagent running with different PIDs.

**Root cause:** The Hermes gateway spawns new processes on restart but old ones persist.

**Fix:**
```bash
ps aux | grep 'bin/bt-' | awk '{print $2, $9, $NF}' | sort -k2
# Kill older PIDs, keep only the newest instance of each binary
kill <older-pid>
```

### jq `.[0]` fails with exit code 5 on ev_order_mutations

**Symptom:** Shell scripts using `jq '.[0].operation'` on `ev_order_mutations` output fail with exit code 5.

**Root cause:** The response format changed from a bare JSON array to an object wrapper `{"candidates": [...], "total": N}`.

**Fix:** Use backward-compatible jq filter:
```bash
jq -r '(.candidates[0].operation // (.[0].operation // "none"))'
```

---

## Evolution & Gardener Issues

### Transposition Table always returns 0 entries

**Symptom:** `ev_tt_stats` returns 0 entries even after many evaluations.

**Root cause:** The transposition table is computed in-memory but never persisted to disk.

**Fix:** Call `ev_tt_save()` explicitly to persist to `~/.go-bt-reflections/transposition.json`.

### Mutation death spiral (97.3% regression rate)

**Symptom:** Auto-evolution destroys the tree: fitness drops to 0.195, success rate 0%.

**Root cause:** Unguarded mutations with no automatic rollback.

**Recovery:**
```bash
# Reset tree to default
bt_reset
```

**Prevention:** Quality gates are now active (commit `1c6eb4d`): auto-rollback after regression, composite floor at 0.3, regression threshold at 20%.

### Gardener applies 0 mutations per cycle

**Symptom:** Gardener runs cycles with `Improved: 0/24` every cycle.

**Root cause (V5):** `ScoreMutation` returned -1.0 for neutral mutations (no regression, no improvement). The gate `score <= 0` rejected everything.

**Fix (V6):** `ScoreMutation` now returns 0.0 for neutral. Gate changed to `score < 0`. Neutral mutations pass through.

### Decision tree optimizer crashes with SIGSEGV

**Symptom:** `SIGSEGV: segmentation violation` in `InformationGain()` or `Entropy()`.

**Root cause:** Accessing `Stats[selectorName]` without nil check — the map returns nil for selectors with no recorded stats.

**Fix:** Always nil-check:
```go
ss := d.Stats[selectorName]
if ss == nil { return 0 }
```

---

## Dashboard Issues

### Empty API responses

**Symptom:** `curl http://localhost:9800/api/summary` returns empty body.

**Root cause:** Dashboard server crashed silently or a deadlocked request is blocking.

**Fix:**
```bash
# Check if dashboard is alive
pgrep -f bt-dashboard
# If not running, restart
~/go-bt-evolve/bin/bt-dashboard &
# Verify
curl -s http://localhost:9800/api/health
```

### pkill kills the build process

**Symptom:** `pkill -f bt-dashboard && go build ...` fails — the build is killed mid-compilation.

**Root cause:** `pkill` matches the `go build` process if the pattern overlaps.

**Fix:** Always run build and kill in separate commands:
```bash
# Step 1: Build
go build -o bt-dashboard ./cmd/bt-dashboard/
# Step 2: Kill old (separate terminal call)
pkill -f bt-dashboard
# Step 3: Start new (background)
~/go-bt-evolve/bin/bt-dashboard &
```

---

## Test & Benchmark Issues

### Ollama-dependent tests timeout

**Symptom:** Tests using `DefaultLLM()` timeout after 10+ minutes.

**Cause:** On Jetson CPU, each Ollama call takes 2-4 minutes. Multiple trees × 4 min = timeout.

**Fix for fast tests:**
```bash
go test -short -count=1 -timeout 60s ./...
```

**Fix for full tests:**
```bash
go test -count=1 -timeout 1200s ./...
```

### Background test output flooded by zsh init

**Symptom:** Background Go tests produce ~400 lines of `declare -x` instead of test output.

**Root cause:** `terminal(background=true)` runs zsh which dumps all environment variables.

**Fix:** Use `exec bash -c` for background tests:
```bash
exec bash -c 'cd ~/go-bt-evolve && go test -v -run "TestName" -timeout 1200s ./...' 2>&1
```

### Empty tool output = success, not error

**Symptom:** `go build` or `go test` returns exit code 0 but empty output — perceived as a hang.

**Reality:** Go tools return empty stdout on success. Exit code 0 + empty output = SUCCESS.

---

## Condition & Routing Issues

### Keyword overlap causing misrouting

**Symptom:** Task routes to the wrong strategy path. Example: "audit this code for vulnerabilities" routes to CodeReviewPath instead of SecurityPath.

**Root cause:** Two Selector conditions share keywords. The FIRST matching Selector child wins, even if a later path is more specific.

**Fix:**
1. Remove ambiguous single-word triggers from conditions
2. Use multi-word phrases ("code review", "security audit")
3. Order conditions from most-specific to least-specific
4. Test each condition against tasks meant for OTHER paths

### Domain tree silently fails (no LLM calls)

**Symptom:** Test returns instantly (0.00s) with `outcome: failure`, no LLM errors.

**Root cause:** PreGate condition rejected the task — the task text doesn't contain the required domain keywords.

**Fix:** Prefix tasks with domain keywords:
- Go trees: include "go", "golang", "goroutine", etc.
- Finance trees: include "dcf", "lbo", "comps", "earnings", "kyc"
- Research trees: include "research", "investigate", "analyze", "what is"

---

## Ollama-Specific Issues

### OLLAMA_HOST missing http:// scheme

See "All LLM-dependent services crash immediately" above.

### Model not found

**Symptom:** `model "qwen3.6:35b-a3b" not found`

**Fix:**
```bash
ollama pull qwen3.6:35b-a3b
# Check loaded models
ollama list
```

### Slow inference on Jetson

**Expected:** 2-4 minutes per LLM call on Jetson ARM64 CPU with qwen3.6:35b.

**Optimization:** None available on CPU. Consider using a smaller model for latency-sensitive operations or offloading to DeepSeek API (set `DEEPSEEK_API_KEY`).

---

## Configuration Issues

### Boolean `false` in config file is ignored

**Symptom:** Setting `"gardener_enabled": false` in config JSON doesn't disable the gardener.

**Root cause:** Go's `json.Unmarshal` treats missing bool fields as `false`, indistinguishable from explicitly-set `false`.

**Fix:** The config loader uses `hasExplicitField()` to detect explicit `false` values. This is handled automatically in `LoadFile()`.

### Config file not found

**Symptom:** `BT_CONFIG_FILE` is set but the platform uses defaults.

**Fix:**
```bash
# Check the file exists
ls -la "$BT_CONFIG_FILE"
# Check env var
echo "$BT_CONFIG_FILE"
# Export a default config to use as template
# (platform starts with sensible defaults if no file found)
```

---

## Security Issues

### API key not working on dashboard

**Symptom:** `curl -H "X-API-Key: mykey" http://localhost:9800/api/summary` returns 401.

**Fix:**
```bash
# Check if BT_API_KEY is set
echo "$BT_API_KEY"
# Set it if not
export BT_API_KEY="your-secret-key"
# Restart dashboard
```

### MCP rate limit exceeded

**Symptom:** MCP tools return error code -32000 with "Rate limit exceeded."

**Cause:** Token bucket rate limiter hit. Default: 2 req/s for bt-agent/bt-langagent, 5 req/s for bt-evaluator.

**Fix:** Wait and retry. For persistent issues, stagger cron job schedules to avoid simultaneous MCP calls.

---

## See Also

- [Getting Started Guide](GETTING_STARTED.md)
- [API Reference](API_REFERENCE.md)
- [Tutorial](TUTORIAL.md)
- [Architecture Decision Records](adr/INDEX.md)
- [Runner Setup Guide](runner-setup.md)
