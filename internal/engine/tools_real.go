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
	"strings"
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

// goModuleRoot returns the directory containing go.mod (CI workspace or local checkout).
func goModuleRoot() string {
	if dir := os.Getenv("BT_MODULE_ROOT"); dir != "" {
		return dir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
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
			cmd.Dir = goModuleRoot()
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
			cmd.Dir = goModuleRoot()
			out, err := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))
			if result == "" {
				if err != nil {
					result = fmt.Sprintf("go test failed: %v", err)
				} else {
					result = "go test completed (no output)"
				}
			}
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
		fn: func(_ string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "vet", "./...")
			cmd.Dir = goModuleRoot()
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
			cmd.Dir = goModuleRoot()
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
