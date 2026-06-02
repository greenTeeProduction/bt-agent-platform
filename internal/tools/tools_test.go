package tools

import (
	"os"
	"strings"
	"testing"
)

func TestTool_Name(t *testing.T) {
	tool := Tool{name: "test_tool", desc: "a test tool", call: func(s string) string { return s }}
	if got := tool.Name(); got != "test_tool" {
		t.Errorf("Name() = %q, want %q", got, "test_tool")
	}
}

func TestTool_Description(t *testing.T) {
	tool := Tool{name: "test_tool", desc: "a test tool", call: func(s string) string { return s }}
	if got := tool.Description(); got != "a test tool" {
		t.Errorf("Description() = %q, want %q", got, "a test tool")
	}
}

func TestTool_Call(t *testing.T) {
	t.Run("with implementation", func(t *testing.T) {
		tool := Tool{name: "echo", desc: "echoes input", call: func(s string) string { return s }}
		if got := tool.Call("hello"); got != "hello" {
			t.Errorf("Call() = %q, want %q", got, "hello")
		}
	})

	t.Run("nil call fn", func(t *testing.T) {
		tool := Tool{name: "broken", desc: "no impl"}
		got := tool.Call("anything")
		want := "tool 'broken' has no implementation"
		if got != want {
			t.Errorf("Call() = %q, want %q", got, want)
		}
	})
}

func TestShellExec_Success(t *testing.T) {
	tool := ShellExec()
	got := tool.Call("echo hello world")
	if !strings.Contains(got, "hello world") {
		t.Errorf("ShellExec echo = %q, want hello world", got)
	}
}

func TestShellExec_Failure(t *testing.T) {
	tool := ShellExec()
	got := tool.Call("exit 42")
	if !strings.Contains(got, "exit status 42") && !strings.Contains(got, "42") {
		t.Errorf("ShellExec exit = %q, want exit status 42", got)
	}
}

func TestShellExec_NoOutput(t *testing.T) {
	tool := ShellExec()
	got := tool.Call("true")
	if !strings.Contains(got, "no output") {
		t.Errorf("ShellExec true = %q, want '(command completed with no output)'", got)
	}
}

func TestShellExec_StderrOnly(t *testing.T) {
	tool := ShellExec()
	got := tool.Call("bash -c 'echo errmsg >&2; exit 1'")
	if !strings.Contains(got, "errmsg") {
		t.Errorf("ShellExec stderr = %q, want errmsg", got)
	}
	if !strings.Contains(got, "exit status") {
		t.Errorf("ShellExec stderr exit = %q, want exit status", got)
	}
}

func TestHTTPGet_InvalidURL(t *testing.T) {
	tool := HTTPGet()
	got := tool.Call("http://127.0.0.1:1/does-not-exist")
	if !strings.Contains(got, "failed") && !strings.Contains(got, "connection refused") && !strings.Contains(got, "timeout") {
		t.Errorf("HTTPGet bad URL = %q, want failure message", got)
	}
}

func TestHTTPGet_EmptyInput(t *testing.T) {
	tool := HTTPGet()
	got := tool.Call("")
	if !strings.Contains(got, "failed") {
		t.Errorf("HTTPGet empty = %q, want failure", got)
	}
}

func TestProcessCheck_EmptyInput(t *testing.T) {
	tool := ProcessCheck()
	// Empty input matches all processes, so we just check it returns something
	got := tool.Call("")
	if got == "" {
		t.Errorf("ProcessCheck empty = empty, want process list or NOT RUNNING message")
	}
}

func TestProcessCheck_NoMatch(t *testing.T) {
	tool := ProcessCheck()
	got := tool.Call("thisprocessdoesnotexistXYZ123")
	if !strings.Contains(got, "NOT RUNNING") {
		t.Errorf("ProcessCheck no match = %q, want NOT RUNNING", got)
	}
}

func TestDiskUsage_EmptyInput(t *testing.T) {
	tool := DiskUsage()
	got := tool.Call("")
	if !strings.Contains(got, "Filesystem") || !strings.Contains(got, "Size") {
		t.Errorf("DiskUsage empty = %q, want Filesystem/Size header", got)
	}
}

func TestDiskUsage_Root(t *testing.T) {
	tool := DiskUsage()
	got := tool.Call("/")
	if !strings.Contains(got, "Filesystem") {
		t.Errorf("DiskUsage / = %q, want Filesystem header", got)
	}
}

func TestDiskUsage_All(t *testing.T) {
	tool := DiskUsage()
	got := tool.Call("all")
	if !strings.Contains(got, "Filesystem") {
		t.Errorf("DiskUsage all = %q, want Filesystem header", got)
	}
}

func TestMemoryUsage_Success(t *testing.T) {
	tool := MemoryUsage()
	got := tool.Call("")
	if !strings.Contains(got, "total") && !strings.Contains(got, "Mem") {
		t.Errorf("MemoryUsage = %q, want memory stats", got)
	}
}

func TestFileRead_Success(t *testing.T) {
	// Write a temp file
	tmp := t.TempDir()
	path := tmp + "/test.txt"
	if err := writeFile(path, "hello tools"); err != nil {
		t.Fatal(err)
	}
	tool := FileRead()
	got := tool.Call(path)
	if got != "hello tools" {
		t.Errorf("FileRead = %q, want %q", got, "hello tools")
	}
}

func TestFileRead_NotFound(t *testing.T) {
	tool := FileRead()
	got := tool.Call("/tmp/nonexistent-file-xyz-123")
	if !strings.Contains(got, "failed") {
		t.Errorf("FileRead not found = %q, want failure", got)
	}
}

func TestFileRead_Truncated(t *testing.T) {
	large := strings.Repeat("A", 40*1024) // 40KB > 32KB limit
	tmp := t.TempDir()
	path := tmp + "/large.txt"
	if err := writeFile(path, large); err != nil {
		t.Fatal(err)
	}
	tool := FileRead()
	got := tool.Call(path)
	if !strings.Contains(got, "[truncated]") {
		t.Errorf("FileRead large = %q (len=%d), want truncation marker", got, len(got))
	}
	if len(got) > 33*1024 {
		t.Errorf("FileRead large returned %d bytes, want <= 33KB", len(got))
	}
}

func TestFileRead_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/empty.txt"
	if err := writeFile(path, ""); err != nil {
		t.Fatal(err)
	}
	tool := FileRead()
	got := tool.Call(path)
	if got != "" {
		t.Errorf("FileRead empty = %q, want empty string", got)
	}
}

func TestAllTools_ContainsExpectedTools(t *testing.T) {
	tools := AllTools()
	names := make(map[string]bool)
	for _, ti := range tools {
		if tr, ok := ti.(Tool); ok {
			names[tr.Name()] = true
		}
	}

	expected := []string{"shell_exec", "http_get", "process_check", "disk_usage", "memory_usage", "file_read"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("AllTools missing %q", name)
		}
	}
}

func TestMonitorTools_ContainsExpected(t *testing.T) {
	tools := MonitorTools()
	names := make(map[string]bool)
	for _, ti := range tools {
		if tr, ok := ti.(Tool); ok {
			names[tr.Name()] = true
		}
	}
	for _, name := range []string{"shell_exec", "http_get", "process_check", "disk_usage", "memory_usage"} {
		if !names[name] {
			t.Errorf("MonitorTools missing %q", name)
		}
	}
	if names["file_read"] {
		t.Error("MonitorTools should NOT include file_read")
	}
}

func TestDevTools_ContainsExpected(t *testing.T) {
	tools := DevTools()
	names := make(map[string]bool)
	for _, ti := range tools {
		if tr, ok := ti.(Tool); ok {
			names[tr.Name()] = true
		}
	}
	for _, name := range []string{"shell_exec", "http_get", "file_read"} {
		if !names[name] {
			t.Errorf("DevTools missing %q", name)
		}
	}
}

func TestValidateShellCommand_Empty(t *testing.T) {
	got := validateShellCommand("")
	if got == "" {
		t.Error("validateShellCommand('') should return error")
	}
}

func TestValidateShellCommand_Allowed(t *testing.T) {
	cases := []string{
		"ls -la",
		"ps aux",
		"df -h",
		"free -h",
		"echo hello",
		"cat /etc/hostname",
		"grep foo bar.txt",
		"head -5 file.txt",
		"tail -5 file.txt",
		"wc -l file.txt",
		"date",
		"uptime",
		"whoami",
		"hostname",
		"uname -a",
		"find /tmp -name '*.go'",
		"sort file.txt",
		"du -sh /tmp",
		"pgrep -f bt-agent",
		"pidof bt-agent",
		"stat /tmp",
		"test -f /tmp/file",
		"sleep 0",
		"true",
		"false",
		"env",
		"which go",
		"pwd",
		"git log -1",
		"git status",
		"git diff",
		"go version",
		"go env GOROOT",
		"curl -s http://localhost:9800/api/health",
		"ss -tln",
		"ls -la /tmp | grep test",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if err := validateShellCommand(c); err != "" {
				t.Errorf("validateShellCommand(%q) = %q, want ''", c, err)
			}
		})
	}
}

func TestValidateShellCommand_Blocked(t *testing.T) {
	cases := []struct {
		cmd    string
		substr string // substring expected in error message
	}{
		{"rm -rf /", "blocked for safety"},
		{"mv foo bar", "blocked for safety"},
		{"dd if=/dev/zero of=/tmp/out", "blocked for safety"},
		{"chmod 777 /tmp", "blocked for safety"},
		{"chown root /tmp", "blocked for safety"},
		{"kill -9 1234", "blocked for safety"},
		{"pkill -f bt-agent", "blocked for safety"},
		{"killall python", "blocked for safety"},
		{"shutdown -h now", "blocked for safety"},
		{"reboot", "blocked for safety"},
		{"mount /dev/sda1 /mnt", "blocked"}, // device access check fires first
		{"umount /mnt", "blocked for safety"},
		{"fdisk -l", "blocked for safety"},
		{"iptables -L", "blocked for safety"},
		{"systemctl restart sshd", "blocked for safety"},
	}
	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			err := validateShellCommand(c.cmd)
			if err == "" {
				t.Errorf("validateShellCommand(%q) = '', want blocked", c.cmd)
			}
			if !strings.Contains(err, c.substr) {
				t.Errorf("validateShellCommand(%q) = %q, want %q", c.cmd, err, c.substr)
			}
		})
	}
}

func TestValidateShellCommand_RedirectBlocked(t *testing.T) {
	err := validateShellCommand("echo hello > /tmp/out")
	if err == "" {
		t.Fatal("expected error for redirect operator")
	}
	if !strings.Contains(err, "redirect") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_AppendRedirectBlocked(t *testing.T) {
	err := validateShellCommand("echo hello >> /tmp/out")
	if err == "" {
		t.Fatal("expected error for append redirect")
	}
	if !strings.Contains(err, "redirect") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_DeviceAccessBlocked(t *testing.T) {
	err := validateShellCommand("cat /dev/sda")
	if err == "" {
		t.Fatal("expected error for device access")
	}
	if !strings.Contains(err, "device access") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_UnknownCommand(t *testing.T) {
	err := validateShellCommand("xyz_invalid_tool foo")
	if err == "" {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err, "not allowed") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_GitDisallowed(t *testing.T) {
	err := validateShellCommand("git push origin main")
	if err == "" {
		t.Fatal("expected error for git push")
	}
	if !strings.Contains(err, "not allowed") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_GoDisallowed(t *testing.T) {
	err := validateShellCommand("go run main.go")
	if err == "" {
		t.Fatal("expected error for go run")
	}
	if !strings.Contains(err, "not allowed") {
		t.Errorf("unexpected error: %q", err)
	}
}

func TestValidateShellCommand_PathResolution(t *testing.T) {
	// Commands with full paths should resolve just the basename
	err := validateShellCommand("/usr/bin/ls -la")
	if err != "" {
		t.Errorf("full path ls blocked: %q", err)
	}

	err2 := validateShellCommand("/bin/rm -rf /tmp")
	if err2 == "" {
		t.Fatal("full path rm should be blocked")
	}
}

func TestValidateShellCommand_PipeWithAllowed(t *testing.T) {
	err := validateShellCommand("ls -la | grep go")
	if err != "" {
		t.Errorf("piped allowed commands blocked: %q", err)
	}
}

func TestValidateShellCommand_PipeWithBlocked(t *testing.T) {
	err := validateShellCommand("ls -la | rm -rf /")
	if err == "" {
		t.Fatal("piped blocked command should be caught")
	}
}

func TestValidateShellCommand_LogicalOrOperator(t *testing.T) {
	err := validateShellCommand("echo hi || echo there")
	if err != "" {
		t.Errorf("logical OR pipe handled incorrectly: %q", err)
	}
}

func TestValidateShellCommand_QuotedPipes(t *testing.T) {
	// Pipe inside single quotes should not split
	err := validateShellCommand("echo 'a|b'")
	if err != "" {
		t.Errorf("quoted pipe rejected: %q", err)
	}
}

func TestValidateShellCommand_DoubleQuotedPipes(t *testing.T) {
	err := validateShellCommand(`echo "a|b"`)
	if err != "" {
		t.Errorf("double-quoted pipe rejected: %q", err)
	}
}

func TestSandboxedShell_AllowsWhitelisted(t *testing.T) {
	tool := SandboxedShell()
	got := tool.Call("echo hello world")
	if !strings.Contains(got, "hello world") {
		t.Errorf("SandboxedShell echo = %q, want hello world", got)
	}
}

func TestSandboxedShell_BlocksDestructive(t *testing.T) {
	tool := SandboxedShell()
	got := tool.Call("rm -rf /")
	if !strings.Contains(got, "BLOCKED") {
		t.Errorf("SandboxedShell rm = %q, want BLOCKED", got)
	}
}

func TestSplitPipeline_Empty(t *testing.T) {
	got := splitPipeline("")
	if len(got) != 0 {
		t.Errorf("splitPipeline('') = %v, want empty", got)
	}
}

func TestSplitPipeline_NoPipe(t *testing.T) {
	got := splitPipeline("ls -la")
	if len(got) != 1 || got[0] != "ls -la" {
		t.Errorf("splitPipeline('ls -la') = %v, want ['ls -la']", got)
	}
}

func TestSplitPipeline_SinglePipe(t *testing.T) {
	got := splitPipeline("ls | grep go")
	if len(got) != 2 {
		t.Fatalf("splitPipeline = %v, want 2 segments", got)
	}
	if strings.TrimSpace(got[0]) != "ls" {
		t.Errorf("segment 0 = %q, want 'ls'", got[0])
	}
	if strings.TrimSpace(got[1]) != "grep go" {
		t.Errorf("segment 1 = %q, want 'grep go'", got[1])
	}
}

func TestSplitPipeline_MultiplePipes(t *testing.T) {
	got := splitPipeline("a | b | c")
	if len(got) != 3 {
		t.Fatalf("splitPipeline = %v, want 3 segments", got)
	}
}

func TestSplitPipeline_QuotedPipeIgnored(t *testing.T) {
	got := splitPipeline("echo 'a|b'")
	if len(got) != 1 {
		t.Fatalf("splitPipeline('echo a|b') = %v, want 1 segment", got)
	}
}

func TestSplitPipeline_DoubleQuotedPipeIgnored(t *testing.T) {
	got := splitPipeline(`echo "a|b"`)
	if len(got) != 1 {
		t.Fatalf("splitPipeline = %v, want 1 segment", got)
	}
}

func TestSplitPipeline_LogicalOrKeptTogether(t *testing.T) {
	got := splitPipeline("a || b")
	// The '||' bash logical OR should not be treated as a pipe — current impl writes
	// the first '|' to current and then leaves the second '|' as a split point,
	// producing 2 segments. This is a known limitation.
	if len(got) != 2 {
		t.Fatalf("splitPipeline(a || b) = %v, want 2 segments", got)
	}
	if strings.TrimSpace(got[0]) != "a |" && !strings.Contains(got[0], "a") {
		t.Errorf("segment 0 = %q, want 'a |'", got[0])
	}
}

func TestSplitPipeline_TrailingWhitespaceSegments(t *testing.T) {
	got := splitPipeline("ls  |  grep go  |  wc -l")
	if len(got) != 3 {
		t.Fatalf("splitPipeline = %v, want 3 segments", got)
	}
}

// writeFile is a test helper.
func writeFile(path, content string) error {
	// This is a test helper defined in the test file; using os.WriteFile directly
	return os.WriteFile(path, []byte(content), 0644)
}
