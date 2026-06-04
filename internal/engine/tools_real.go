package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// realTool is a tool implementation that actually executes commands,
// reads/writes files, or makes HTTP calls. It satisfies the
// Name() + Call(string) string interface expected by executeAgentTool.
type realTool struct {
	name string
	desc string
	fn   func(input string) string
}

func (t *realTool) Name() string        { return t.name }
func (t *realTool) Description() string { return t.desc }
func (t *realTool) Call(input string) string {
	return t.fn(input)
}

// NewRealToolFactory returns real executable tools by name.
//
// This is intentionally a whitelist factory, not an LLM-generated plugin
// mechanism: agents may discover and request tools on demand, but every tool
// produced here is a compiled, tested implementation. Unknown tools fail
// closed instead of being simulated by the model.
func NewRealToolFactory() map[string]func() *realTool {
	return map[string]func() *realTool{
		"calculator":                 newCalculatorTool,
		"file_read":                  newFileReadTool,
		"file_write":                 newFileWriteTool,
		"go_build":                   newGoBuildTool,
		"go_test":                    newGoTestTool,
		"go_vet":                     newGoVetTool,
		"graphify":                   newGraphifyTool,
		"http_get":                   newHTTPGetTool,
		"notebooklm_list":            newNotebookLMListTool,
		"notebooklm_notebook_get":    newNotebookLMGetTool,
		"notebooklm_notebook_query":  newNotebookLMQueryTool,
		"notebooklm_refresh_auth":    newNotebookLMAuthRefreshTool,
		"notebooklm_research_import": newNotebookLMResearchImportTool,
		"notebooklm_research_start":  newNotebookLMResearchStartTool,
		"notebooklm_research_status": newNotebookLMResearchStatusTool,
		"notebooklm_server_info":     newNotebookLMServerInfoTool,
		"shell_exec":                 newShellExecTool,
		"web_search":                 newWebSearchTool,
	}
}

func buildRealTool(name string) (*realTool, bool) {
	maker, ok := NewRealToolFactory()[name]
	if !ok {
		return nil, false
	}
	return maker(), true
}

func buildRealTools(names ...string) []any {
	tools := make([]any, 0, len(names))
	for _, name := range names {
		if tool, ok := buildRealTool(name); ok {
			tools = append(tools, tool)
		}
	}
	return tools
}

func allRealToolNames() []string {
	factory := NewRealToolFactory()
	names := make([]string, 0, len(factory))
	for name := range factory {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// newShellExecTool creates a tool that runs shell commands.
func newShellExecTool() *realTool {
	return &realTool{
		name: "shell_exec",
		desc: "Execute a shell command and return its output. Use for: running scripts, checking processes, testing, building, system commands. Input: the full bash command to run.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c", input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			out := strings.TrimSpace(stdout.String())
			if err != nil {
				errOut := strings.TrimSpace(stderr.String())
				if errOut != "" {
					if out != "" {
						out += "\n"
					}
					out += errOut
				}
				if out == "" {
					out = fmt.Sprintf("command failed: %v", err)
				}
			}
			if len(out) > 8192 {
				out = out[:8192] + "\n... [truncated]"
			}
			return out
		},
	}
}

// newFileReadTool creates a tool that reads file contents.
func newFileReadTool() *realTool {
	return &realTool{
		name: "file_read",
		desc: "Read the contents of a file. Input: file path (absolute or relative). Returns file contents (trimmed to 16KB).",
		fn: func(input string) string {
			input = strings.TrimSpace(input)
			data, err := os.ReadFile(input)
			if err != nil {
				return fmt.Sprintf("error reading file: %v", err)
			}
			out := string(data)
			if len(out) > 16384 {
				out = out[:16384] + "\n... [truncated]"
			}
			return out
		},
	}
}

// newFileWriteTool creates a tool that writes content to a file.
func newFileWriteTool() *realTool {
	return &realTool{
		name: "file_write",
		desc: "Write content to a file. Input format: 'FILEPATH\\nCONTENT' (first line is path, rest is content). Creates parent directories.",
		fn: func(input string) string {
			parts := strings.SplitN(input, "\n", 2)
			if len(parts) < 2 {
				return "error: input must be 'FILEPATH\\nCONTENT'"
			}
			path := strings.TrimSpace(parts[0])
			content := parts[1]
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return fmt.Sprintf("error creating directories: %v", err)
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Sprintf("error writing file: %v", err)
			}
			return fmt.Sprintf("written %d bytes to %s", len(content), path)
		},
	}
}

// newWebSearchTool creates a tool that performs web searches via DuckDuckGo HTML.
func newWebSearchTool() *realTool {
	return &realTool{
		name: "web_search",
		desc: "Search the web for information. Input: search query string. Returns top results with titles and URLs.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(input))
			req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
			if err != nil {
				return fmt.Sprintf("search error: %v", err)
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BT-Agent/1.0)")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Sprintf("search error: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
			if err != nil {
				return fmt.Sprintf("search read error: %v", err)
			}
			results := extractDuckDuckGoResults(string(body))
			if results == "" {
				return "no results found for: " + input
			}
			if len(results) > 4096 {
				results = results[:4096] + "\n... [truncated]"
			}
			return results
		},
	}
}

// extractDuckDuckGoResults parses HTML for result snippets and URLs.
func extractDuckDuckGoResults(html string) string {
	var b strings.Builder
	// Extract result__snippet spans
	snippetRe := regexp.MustCompile(`class="(?:result|web-result)[^"]*".*?class="result__snippet"[^>]*>(.*?)</`)
	urlRe := regexp.MustCompile(`class="result__url"[^>]*>(.*?)</`)

	snippets := snippetRe.FindAllStringSubmatch(html, 5)
	urls := urlRe.FindAllStringSubmatch(html, 5)

	for i, s := range snippets {
		if i >= len(snippets) {
			break
		}
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, stripHTML(s[1])))
		if i < len(urls) {
			b.WriteString(fmt.Sprintf("   URL: %s\n", stripHTML(urls[i][1])))
		}
	}

	if b.Len() == 0 {
		// Fallback: try broader extraction
		linkRe := regexp.MustCompile(`class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
		links := linkRe.FindAllStringSubmatch(html, 5)
		for i, l := range links {
			b.WriteString(fmt.Sprintf("%d. %s\n   URL: %s\n", i+1, stripHTML(l[2]), l[1]))
		}
	}
	return strings.TrimSpace(b.String())
}

func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

// newGoBuildTool creates a tool that runs go build.
func newGoBuildTool() *realTool {
	return &realTool{
		name: "go_build",
		desc: "Run 'go build ./...' in the Go project directory. Returns build output or errors.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			args := strings.Fields(input)
			if len(args) == 0 {
				args = []string{"./..."}
			}
			cmd := exec.CommandContext(ctx, "go", append([]string{"build"}, args...)...)
			cmd.Dir = "/home/nico/go-bt-evolve"
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if result == "" {
				result = "build successful"
			}
			if len(result) > 4096 {
				result = result[:4096] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newGoTestTool creates a tool that runs go test.
func newGoTestTool() *realTool {
	return &realTool{
		name: "go_test",
		desc: "Run 'go test ./...' with verbose output in the Go project directory.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
			defer cancel()
			args := strings.Fields(input)
			if len(args) == 0 {
				args = []string{"./...", "-v", "-count=1"}
			}
			cmd := exec.CommandContext(ctx, "go", append([]string{"test"}, args...)...)
			cmd.Dir = "/home/nico/go-bt-evolve"
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if len(result) > 8192 {
				result = result[:8192] + "\n... [output truncated]"
			}
			return result
		},
	}
}

// newGoVetTool creates a tool that runs go vet.
func newGoVetTool() *realTool {
	return &realTool{
		name: "go_vet",
		desc: "Run 'go vet ./...' for static analysis in the Go project directory.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "vet", "./...")
			cmd.Dir = "/home/nico/go-bt-evolve"
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if result == "" {
				result = "vet passed: no issues found"
			}
			if len(result) > 4096 {
				result = result[:4096] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newGraphifyTool creates a tool that runs graphify commands.
func newGraphifyTool() *realTool {
	return &realTool{
		name: "graphify",
		desc: "Query or update the code knowledge graph. Input format: 'update' to rebuild, 'query <question>' to search, 'path <A> <B>' for relationships, 'explain <concept>' for details.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			input = strings.TrimSpace(input)
			parts := strings.SplitN(input, " ", 2)
			action := parts[0]
			arg := ""
			if len(parts) > 1 {
				arg = parts[1]
			}
			var args []string
			switch action {
			case "update":
				if arg != "" {
					args = []string{"update", arg}
				} else {
					args = []string{"update", "."}
				}
			case "query":
				args = []string{"query", arg}
			case "path":
				nodes := strings.Fields(arg)
				args = append([]string{"path"}, nodes...)
			case "explain":
				args = []string{"explain", arg}
			default:
				// Treat bare input as a query
				args = []string{"query", input}
			}
			cmd := exec.CommandContext(ctx, "graphify", args...)
			cmd.Dir = "/home/nico/go-bt-evolve"
			out, err := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if len(result) > 8192 {
				result = result[:8192] + "\n... [output truncated]"
			}
			if err != nil && result == "" {
				return fmt.Sprintf("graphify error: %v", err)
			}
			return result
		},
	}
}

// --- NotebookLM tools -----------------------------------------------------------
// These wrap the nlm CLI so the ChainAction ReAct agent calls deterministic
// Go functions instead of formatting shell commands (which the LLM may fabricate).
// Each tool execs the real nlm binary and returns its JSON output.

const nlmBin = "/home/nico/.local/bin/nlm"
const defaultNotebook = "463ca402-e972-470b-889c-b735e37c6746"

// nlmRun runs an nlm command with the given arguments, with retry and circuit breaker.
func nlmRun(timeout time.Duration, args ...string) string {
	const maxRetries = 3
	const baseDelay = 2 * time.Second
	const maxDelay = 30 * time.Second

	// Circuit breaker check
	nlmCircuitMu.Lock()
	if nlmCircuitOpen {
		if time.Since(nlmOpenedAt) > nlmCooldown {
			nlmCircuitOpen = false
			nlmFailCount = 0
		} else {
			nlmCircuitMu.Unlock()
			return `{"error": "NotebookLM circuit breaker open", "retry_after": "` + nlmCooldown.Truncate(time.Second).String() + `"}`
		}
	}
	nlmCircuitMu.Unlock()

	// Determine operation type for metrics
	op := "unknown"
	for _, a := range args {
		switch a {
		case "notebook", "get":
			op = "get"
		case "list":
			op = "list"
		case "research":
			if op != "import" {
				op = "research"
			}
		case "import":
			op = "import"
		case "query":
			op = "query"
		case "login":
			op = "auth"
		}
	}

	start := time.Now()
	var lastOut string
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			delay := baseDelay * time.Duration(1<<(attempt-1))
			if delay > maxDelay {
				delay = maxDelay
			}
			time.Sleep(delay)
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		cmd := exec.CommandContext(ctx, nlmBin, args...)
		cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":/home/nico/.local/bin")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		cancel()

		out := strings.TrimSpace(stdout.String())
		if err != nil {
			errOut := strings.TrimSpace(stderr.String())
			if errOut != "" {
				if out != "" {
					out += "\n"
				}
				out += errOut
			}
			if out == "" {
				out = fmt.Sprintf("nlm error: %v", err)
			}

			if strings.Contains(errOut, "Authentication failed") ||
				strings.Contains(errOut, "Usage:") ||
				strings.Contains(errOut, "Error:") {
				nlmCircuitMu.Lock()
				nlmFailCount++
				if nlmFailCount >= nlmCircuitThresh {
					nlmCircuitOpen = true
					nlmOpenedAt = time.Now()
					nlmMetrics.RecordCBOpened()
				}
				nlmCircuitMu.Unlock()
				nlmMetrics.RecordFailure()
				nlmMetrics.RecordLatency(op, time.Since(start))
				if len(out) > 8192 {
					out = out[:8192] + "\n... [truncated]"
				}
				return out
			}

			lastOut = out
			continue
		}

		// Success
		nlmCircuitMu.Lock()
		nlmSuccessCount++
		nlmFailCount = 0
		if nlmCircuitOpen && nlmCooldown > 0 {
			nlmMetrics.RecordCBClosed()
		}
		nlmCircuitMu.Unlock()

		nlmMetrics.RecordLatency(op, time.Since(start))
		if len(out) > 8192 {
			out = out[:8192] + "\n... [truncated]"
		}
		return out
	}

	// All retries exhausted
	nlmCircuitMu.Lock()
	nlmFailCount++
	if nlmFailCount >= nlmCircuitThresh {
		nlmCircuitOpen = true
		nlmOpenedAt = time.Now()
		nlmMetrics.RecordCBOpened()
	}
	nlmCircuitMu.Unlock()

	nlmMetrics.RecordFailure()
	nlmMetrics.RecordLatency(op, time.Since(start))
	if len(lastOut) > 8192 {
		lastOut = lastOut[:8192] + "\n... [truncated]"
	}
	return lastOut
}

// nlmCircuitBreaker tracks NotebookLM call health.
var (
	nlmCircuitOpen   bool
	nlmFailCount     int
	nlmSuccessCount  int
	nlmCircuitMu     sync.Mutex
	nlmCircuitThresh = 5               // consecutive failures to open
	nlmCooldown      = 5 * time.Minute // cooldown before half-open
	nlmOpenedAt      time.Time
)

// nlmCircuitBreaker wraps a call with circuit breaker logic.
// newNotebookLMServerInfoTool wraps `nlm login --check` (auth status / version).
func newNotebookLMServerInfoTool() *realTool {
	return &realTool{
		name: "notebooklm_server_info",
		desc: "Check NotebookLM authentication status and server version. Takes no input. Returns auth status and version info.",
		fn: func(input string) string {
			return nlmRun(30*time.Second, "login", "--check")
		},
	}
}

// newNotebookLMListTool wraps `nlm notebook list`.
func newNotebookLMListTool() *realTool {
	return &realTool{
		name: "notebooklm_list",
		desc: "List all NotebookLM notebooks. Takes no input. Returns JSON array of notebooks with id, title, source_count.",
		fn: func(input string) string {
			return nlmRun(30*time.Second, "notebook", "list", "--json")
		},
	}
}

// newNotebookLMGetTool wraps `nlm notebook get <id> --json`.
// Input: notebook UUID (default: 463ca402-...).
func newNotebookLMGetTool() *realTool {
	return &realTool{
		name: "notebooklm_notebook_get",
		desc: "Get notebook details: id, title, source_count, and source list. Input: notebook UUID (or empty for default BT Platform Research notebook 463ca402-e972-470b-889c-b735e37c6746). Returns JSON.",
		fn: func(input string) string {
			id := strings.TrimSpace(input)
			if id == "" {
				id = defaultNotebook
			}
			return nlmRun(30*time.Second, "notebook", "get", id, "--json")
		},
	}
}

// newNotebookLMResearchStartTool wraps `nlm research start`.
// Input format: "notebook_id|<query>" or just "<query>" (uses default notebook).
func newNotebookLMResearchStartTool() *realTool {
	return &realTool{
		name: "notebooklm_research_start",
		desc: `Start NotebookLM web evolution. Input: "notebook_id|query|mode|source" (e.g. "463ca402-...|latest AI research|fast|web"). Mode: fast (~30s) or deep (~5min). Source: web or drive. Returns task_id.`,
		fn: func(input string) string {
			parts := strings.SplitN(strings.TrimSpace(input), "|", 4)
			nbID := defaultNotebook
			query := input
			mode := "fast"
			source := "web"
			if len(parts) >= 2 {
				if parts[0] != "" {
					nbID = parts[0]
				}
				query = parts[1]
			}
			if len(parts) >= 3 && parts[2] != "" {
				mode = parts[2]
			}
			if len(parts) >= 4 && parts[3] != "" {
				source = parts[3]
			}
			return nlmRun(30*time.Second,
				"research", "start", query,
				"--notebook-id", nbID,
				"--mode", mode,
				"--source", source,
			)
		},
	}
}

// newNotebookLMResearchStatusTool wraps `nlm research status`.
// Input: "notebook_id|task_id" or just "task_id" (uses default notebook).
func newNotebookLMResearchStatusTool() *realTool {
	return &realTool{
		name: "notebooklm_research_status",
		desc: "Poll research progress. Input: \"notebook_id|task_id\" (or just task_id). Returns status and discovered sources when complete. Use --max-wait 300.",
		fn: func(input string) string {
			parts := strings.SplitN(strings.TrimSpace(input), "|", 2)
			nbID := defaultNotebook
			taskID := input
			if len(parts) >= 2 {
				nbID = parts[0]
				taskID = parts[1]
			}
			return nlmRun(360*time.Second,
				"research", "status", nbID,
				"--task-id", taskID,
				"--compact",
				"--max-wait", "300",
			)
		},
	}
}

// newNotebookLMResearchImportTool wraps `nlm research import`.
// Input: "notebook_id|task_id|cited_only" where cited_only is "true" or "false".
func newNotebookLMResearchImportTool() *realTool {
	return &realTool{
		name: "notebooklm_research_import",
		desc: `Import discovered sources into notebook. Input: "notebook_id|task_id|cited_only" (cited_only: true/false). Import ALL if cited_only=false. Returns imported count and source IDs.`,
		fn: func(input string) string {
			parts := strings.SplitN(strings.TrimSpace(input), "|", 3)
			if len(parts) < 2 {
				return `{"error": "input must be notebook_id|task_id[|cited_only]"}`
			}
			nbID := parts[0]
			taskID := parts[1]
			citedOnly := false
			if len(parts) >= 3 && strings.TrimSpace(parts[2]) == "true" {
				citedOnly = true
			}
			args := []string{"research", "import", nbID, taskID}
			if citedOnly {
				args = append(args, "--cited-only")
			}
			return nlmRun(300*time.Second, args...)
		},
	}
}

// newNotebookLMQueryTool wraps `nlm notebook query`.
// Input: "notebook_id|question" or just "question" (uses default notebook).
func newNotebookLMQueryTool() *realTool {
	return &realTool{
		name: "notebooklm_notebook_query",
		desc: `Ask AI about notebook sources with citations. Input: "notebook_id|question" (or just question for default notebook). Returns citation-backed answer.`,
		fn: func(input string) string {
			parts := strings.SplitN(strings.TrimSpace(input), "|", 2)
			nbID := defaultNotebook
			question := input
			if len(parts) >= 2 {
				nbID = parts[0]
				question = parts[1]
			}
			return nlmRun(180*time.Second,
				"notebook", "query", nbID,
				question,
			)
		},
	}
}

// newNotebookLMAuthRefreshTool wraps `nlm login`.
func newNotebookLMAuthRefreshTool() *realTool {
	return &realTool{
		name: "notebooklm_refresh_auth",
		desc: "Refresh NotebookLM authentication. Call this when server_info shows auth is stale/expired. Takes no input.",
		fn: func(input string) string {
			return nlmRun(120*time.Second, "login")
		},
	}
}

// --- Real replacements for stubs (production tool maturity) ----------------

// newHTTPGetTool performs real HTTP GET requests.
func newHTTPGetTool() *realTool {
	return &realTool{
		name: "http_get",
		desc: "Make an HTTP GET request and return response body (truncated to 8KB). Input: URL to fetch.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSpace(input), nil)
			if err != nil {
				return fmt.Sprintf("http_get error: %v", err)
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BT-Agent/1.0)")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Sprintf("http_get error: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
			if err != nil {
				return fmt.Sprintf("http_get read error: %v", err)
			}
			result := fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, string(body))
			if len(result) > 8192 {
				result = result[:8192] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newProcessCheckTool checks if a process is running by name.
func newProcessCheckTool() *realTool {
	return &realTool{
		name: "process_check",
		desc: "Check if a process is running by name. Input: process name (e.g., 'bt-agent', 'nginx'). Returns process info or 'not running'.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			name := strings.TrimSpace(input)
			cmd := exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("ps aux | grep -v grep | grep '%s' || echo 'NOT_RUNNING'", name))
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if len(result) > 4096 {
				result = result[:4096] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newDiskUsageTool checks disk usage on a mount point.
func newDiskUsageTool() *realTool {
	return &realTool{
		name: "disk_usage",
		desc: "Check disk usage on a mount point. Input: mount point path (default: /). Returns df output.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			path := strings.TrimSpace(input)
			if path == "" {
				path = "/"
			}
			cmd := exec.CommandContext(ctx, "df", "-BM", path)
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if len(result) > 4096 {
				result = result[:4096] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newMemoryUsageTool checks memory usage statistics.
func newMemoryUsageTool() *realTool {
	return &realTool{
		name: "memory_usage",
		desc: "Check memory usage statistics. Takes no input. Returns free -m output.",
		fn: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "free", "-m")
			out, _ := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if len(result) > 4096 {
				result = result[:4096] + "\n... [truncated]"
			}
			return result
		},
	}
}

// newCalculatorTool performs basic mathematical calculations.
func newCalculatorTool() *realTool {
	return &realTool{
		name: "calculator",
		desc: "Perform mathematical calculations. Input: arithmetic expression (e.g., '2+3*4', 'sqrt(16)'). Supports +-*/^ and basic functions.",
		fn: func(input string) string {
			input = strings.TrimSpace(input)
			// Use bc for calculation
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("echo 'scale=6; %s' | bc -l 2>&1", input))
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Sprintf("calculator error: %v (%s)", err, strings.TrimSpace(string(out)))
			}
			result := strings.TrimSpace(string(out))
			if result == "" {
				return fmt.Sprintf("calculator: no result for '%s'", input)
			}
			return result
		},
	}
}
