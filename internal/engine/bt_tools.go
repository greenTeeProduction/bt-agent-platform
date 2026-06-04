// Package engine provides the core BT execution engine, agent registry,
// tool implementations, validation suites, and tree lifecycle management.
package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Tool represents a callable tool with name, description, and implementation.
type Tool struct {
	name string
	desc string
	call func(string) string
}

func (t Tool) Name() string        { return t.name }
func (t Tool) Description() string { return t.desc }
func (t Tool) Call(input string) string {
	if t.call != nil {
		return t.call(input)
	}
	return fmt.Sprintf("tool '%s' has no implementation", t.name)
}

// ShellExec runs a shell command and returns its output.
// Input: the shell command to execute. Timeout: 30 seconds.
func ShellExec() Tool {
	return Tool{
		name: "shell_exec",
		desc: "Execute a shell command and return its stdout/stderr. Use for: checking processes (ps), disk (df), memory (free), files (ls), network (ss), system inspection.",
		call: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "bash", "-c", input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			result := strings.TrimSpace(stdout.String())
			if stderr.Len() > 0 {
				errStr := strings.TrimSpace(stderr.String())
				if errStr != "" {
					result += "\n[stderr]: " + errStr
				}
			}
			if err != nil {
				if result == "" {
					return fmt.Sprintf("command failed: %v", err)
				}
				return result + fmt.Sprintf("\n[error]: %v", err)
			}
			if result == "" {
				return "(command completed with no output)"
			}
			return result
		},
	}
}

// HTTPGet makes an HTTP GET request and returns the response body.
// Input: URL to fetch. Timeout: 15 seconds.
func HTTPGet() Tool {
	return Tool{
		name: "http_get",
		desc: "Make an HTTP GET request to a URL and return the response body. Use for: health checks, API calls, fetching data from local services.",
		call: func(input string) string {
			client := &http.Client{Timeout: 15 * time.Second}
			resp, err := client.Get(strings.TrimSpace(input))
			if err != nil {
				return fmt.Sprintf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // 64KB max
			if err != nil {
				return fmt.Sprintf("failed to read response: %v", err)
			}
			return fmt.Sprintf("HTTP %d\n%s", resp.StatusCode, strings.TrimSpace(string(body)))
		},
	}
}

// ProcessCheck checks if a process is running by name.
// Input: process name pattern to search for.
func ProcessCheck() Tool {
	return Tool{
		name: "process_check",
		desc: "Check if a process is running and return its PID, CPU%, memory. Input: process name or pattern (e.g., 'bt-agent', 'python').",
		call: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "bash", "-c",
				fmt.Sprintf("ps aux | grep -v grep | grep -i '%s' || echo 'NOT RUNNING'", input))
			out, err := cmd.Output()
			if err != nil {
				return fmt.Sprintf("process check failed: %v", err)
			}
			result := strings.TrimSpace(string(out))
			if result == "NOT RUNNING" || result == "" {
				return fmt.Sprintf("NOT RUNNING: no process matching '%s' found", input)
			}
			return result
		},
	}
}

// DiskUsage checks disk usage for a mount point.
// Input: mount point path (e.g., "/", "/mnt/ssd"), or "all" for all mounts.
func DiskUsage() Tool {
	return Tool{
		name: "disk_usage",
		desc: "Check disk usage and free space. Input: mount point (e.g., '/', '/mnt/ssd') or 'all' for all mounts.",
		call: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			args := []string{"-h"}
			input = strings.TrimSpace(input)
			if input != "" && input != "all" {
				args = append(args, input)
			}
			cmd := exec.CommandContext(ctx, "df", args...)
			out, err := cmd.Output()
			if err != nil {
				return fmt.Sprintf("disk check failed: %v", err)
			}
			return strings.TrimSpace(string(out))
		},
	}
}

// MemoryUsage checks memory usage.
// Input: ignored (always returns full memory stats).
func MemoryUsage() Tool {
	return Tool{
		name: "memory_usage",
		desc: "Check memory usage and free RAM. Returns total, used, free, available memory and swap.",
		call: func(input string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "free", "-h")
			out, err := cmd.Output()
			if err != nil {
				return fmt.Sprintf("memory check failed: %v", err)
			}
			return strings.TrimSpace(string(out))
		},
	}
}

// FileRead reads a text file.
// Input: absolute file path to read (max 32KB returned).
func FileRead() Tool {
	return Tool{
		name: "file_read",
		desc: "Read a text file and return its contents (limited to 32KB). Input: absolute file path.",
		call: func(input string) string {
			data, err := os.ReadFile(strings.TrimSpace(input))
			if err != nil {
				return fmt.Sprintf("file read failed: %v", err)
			}
			if len(data) > 32*1024 {
				return string(data[:32*1024]) + "\n... [truncated]"
			}
			return string(data)
		},
	}
}

// AllTools returns all built-in tools for use on the blackboard.
// Uses SandboxedShell instead of ShellExec for security.
func AllTools() []any {
	return []any{
		SandboxedShell(),
		HTTPGet(),
		ProcessCheck(),
		DiskUsage(),
		MemoryUsage(),
		FileRead(),
	}
}

// MonitorTools returns tools appropriate for the agent monitor.
func MonitorTools() []any {
	return []any{
		SandboxedShell(),
		HTTPGet(),
		ProcessCheck(),
		DiskUsage(),
		MemoryUsage(),
	}
}

// DevTools returns tools appropriate for development agents.
func DevTools() []any {
	return []any{
		SandboxedShell(),
		FileRead(),
		HTTPGet(),
	}
}

// SandboxedShell runs ONLY whitelisted commands. Blocks destructive operations.
func SandboxedShell() Tool {
	return Tool{
		name: "shell_exec",
		desc: "Execute whitelisted shell commands (ps, df, free, ls, ss, curl, grep, wc, head, tail, cat, echo, date, uptime, find, sort, du, pgrep, pidof, stat, test, git log/status/diff, go build/test/vet) and return output. Destructive commands blocked.",
		call: func(input string) string {
			if err := validateShellCommand(input); err != "" {
				return fmt.Sprintf("BLOCKED: %s", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "bash", "-c", input)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			result := strings.TrimSpace(stdout.String())
			if stderr.Len() > 0 {
				errStr := strings.TrimSpace(stderr.String())
				if errStr != "" {
					result += "\n[stderr]: " + errStr
				}
			}
			if err != nil {
				if result == "" {
					return fmt.Sprintf("command failed: %v", err)
				}
				return result + fmt.Sprintf("\n[error]: %v", err)
			}
			if result == "" {
				return "(command completed with no output)"
			}
			return result
		},
	}
}

// validateShellCommand checks a shell command against the whitelist.
func validateShellCommand(input string) string {
	allowed := map[string]bool{
		"ps": true, "df": true, "free": true, "ls": true, "ss": true,
		"curl": true, "grep": true, "wc": true, "head": true, "tail": true,
		"cat": true, "echo": true, "date": true, "uptime": true,
		"hostname": true, "whoami": true, "id": true, "uname": true,
		"find": true, "sort": true, "du": true, "pgrep": true,
		"pidof": true, "stat": true, "test": true, "sleep": true,
		"true": true, "false": true, "env": true, "printenv": true,
		"which": true, "type": true, "pwd": true, "basename": true,
		"dirname": true, "readlink": true, "realpath": true,
	}

	blocked := map[string]bool{
		"rm": true, "mv": true, "dd": true, "mkfs": true,
		"chmod": true, "chown": true, "kill": true, "pkill": true,
		"killall": true, "shutdown": true, "reboot": true, "halt": true,
		"poweroff": true, "mount": true, "umount": true, "fdisk": true,
		"parted": true, "iptables": true, "nft": true, "systemctl": true,
		"service": true, "init": true, "telinit": true,
	}

	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "empty command"
	}

	for _, tok := range strings.Fields(trimmed) {
		if tok == ">" || tok == ">>" {
			return "redirect operators are blocked for safety"
		}
		if strings.HasPrefix(tok, "/dev/") {
			return "direct device access is blocked"
		}
	}

	segments := splitPipeline(trimmed)
	for _, seg := range segments {
		fields := strings.Fields(strings.TrimSpace(seg))
		if len(fields) == 0 {
			continue
		}
		cmd := fields[0]
		if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
			cmd = cmd[idx+1:]
		}

		if blocked[cmd] {
			return fmt.Sprintf("command '%s' is blocked for safety", cmd)
		}
		if allowed[cmd] {
			continue
		}
		// Allow safe git subcommands
		if cmd == "git" && len(fields) > 1 {
			gitAllowed := map[string]bool{
				"log": true, "status": true, "show": true, "diff": true,
				"branch": true, "rev-parse": true, "rev-list": true,
				"tag": true, "describe": true, "ls-files": true,
				"ls-tree": true, "cat-file": true,
			}
			if gitAllowed[fields[1]] {
				continue
			}
		}
		// Allow safe go subcommands
		if cmd == "go" && len(fields) > 1 {
			goAllowed := map[string]bool{
				"build": true, "test": true, "vet": true, "version": true,
				"env": true, "list": true, "doc": true, "fmt": true,
				"mod": true, "tool": true,
			}
			if goAllowed[fields[1]] {
				continue
			}
		}
		return fmt.Sprintf("command '%s' is not allowed", cmd)
	}
	return ""
}

// splitPipeline splits a shell command by pipe separators, respecting quotes.
func splitPipeline(cmd string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}
		if ch == '|' && !inSingle && !inDouble {
			if i+1 < len(cmd) && cmd[i+1] == '|' {
				current.WriteByte(ch)
				continue
			}
			segments = append(segments, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}
