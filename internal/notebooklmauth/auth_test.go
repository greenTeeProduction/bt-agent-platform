package notebooklmauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		output string
		err    error
		want   string
	}{
		{"Authentication valid!", nil, "valid"},
		{"Authenticated as fixture@example.test", nil, "valid"},
		{"Authentication valid!", errors.New("exit 1"), "auth_error"},
		{"Authentication failed: Credentials have expired.", errors.New("exit 1"), "auth_required"},
		{"stale", nil, "auth_required"},
		{"No saved credentials found.", nil, "auth_required"},
		{"network_error: expired credentials may still be valid", nil, "network_error"},
		{"Authentication failed: Could not reach NotebookLM", nil, "network_error"},
		{"expired", context.Canceled, "network_error"},
		{"", nil, "auth_error"},
		{"an error occurred", nil, "auth_error"},
	} {
		if got := classify(tc.output, tc.err); got != tc.want {
			t.Errorf("%q: got %s want %s", tc.output, got, tc.want)
		}
	}
}

// fixture executes helper.py in a subprocess with fake installed APIs. It never
// reads local credentials or contacts any real browser or NotebookLM service.
func fixture(t *testing.T, config map[string]any) (policy, string, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for auth helper integration tests")
	}
	script, err := os.ReadFile("testdata/fake_cli.py")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, b []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), b, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("fake_cli.py", script, 0600)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	write("python", []byte("#!/bin/sh\nexec "+quote(python)+" "+quote(filepath.Join(dir, "fake_cli.py"))+" \"$@\"\n"), 0700)
	if config["page_url"] == nil {
		config["page_url"] = "https://notebook.google.com/"
	}
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	write("fixture.json", b, 0600)
	write("saved", []byte("stale"), 0600)
	write("protected-profile", []byte("unchanged-protected-fixture"), 0600)
	requests := &atomic.Int32{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/json/list" {
			t.Errorf("browser mutation: %s %s", r.Method, r.URL)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		pages := []cdpPage{{Type: "page", URL: config["page_url"].(string), WebSocket: strings.Replace(server.URL, "http", "ws", 1) + "/devtools/page/existing"}}
		if config["no_pages"] == true {
			pages = nil
		}
		_ = json.NewEncoder(w).Encode(pages)
	}))
	t.Cleanup(server.Close)
	interpreter := filepath.Join(dir, "python")
	p := policy{stateDir: filepath.Join(dir, "policy"), cdpURL: server.URL}
	p.run = func(ctx context.Context, args ...string) (string, error) {
		out, err := helperCommand(ctx, interpreter, "cli", args...).CombinedOutput()
		return string(out), err
	}
	p.restore = func(ctx context.Context, endpoint string) Result {
		return restoreWithPython(ctx, endpoint, interpreter)
	}
	return p, dir, requests
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStaleExistingSessionRestoresAndRechecks(t *testing.T) {
	for _, host := range []string{"notebook.google.com", "notebooklm.google.com", "notebooklm.cloud.google.com", "notebook.cloud.google.com", "vertexaisearch.cloud.google.com"} {
		t.Run(host, func(t *testing.T) {
			p, dir, requests := fixture(t, map[string]any{"page_url": "https://" + host + "/"})
			r := p.ensure(context.Background())
			if !r.OK() {
				t.Fatalf("restore failed: %s; events=%s", r.String(), read(t, dir, "events"))
			}
			want := "check\nRuntime.evaluate\nNetwork.getCookies\nRuntime.evaluate\nvalidate\nsave\ncheck\n"
			if got := read(t, dir, "events"); got != want {
				t.Fatalf("events=%s want=%s", got, want)
			}
			if read(t, dir, "saved") != "valid" || read(t, dir, "protected-profile") != "unchanged-protected-fixture" {
				t.Fatal("profile persistence violated")
			}
			if _, err := os.Stat(filepath.Join(p.stateDir, "cooldown.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("success retained cooldown: %v", err)
			}
			if !p.ensure(context.Background()).OK() || requests.Load() != 1 {
				t.Fatal("valid saved auth attempted another restore")
			}
		})
	}
}

func TestRestoreFailuresPreserveProfilesAndCooldown(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"network":              {"check_output": "Authentication failed: network_error expired"},
		"unknown":              {"check_output": "unknown failure"},
		"missing_page":         {"no_pages": true},
		"login_wall":           {"page_url": "https://accounts.google.com/?continue=https://notebook.google.com"},
		"lookalike":            {"page_url": "https://notebook.google.com.evil.test/"},
		"closed_target":        {"closed_target": true},
		"navigated":            {"current_url": "https://accounts.google.com/"},
		"wrong_account":        {"saved_email": "protected@example.test"},
		"unidentified_account": {"saved_email": ""},
		"invalid_candidate":    {"validation_failure": true},
	} {
		t.Run(name, func(t *testing.T) {
			p, dir, requests := fixture(t, cfg)
			r := p.ensure(context.Background())
			if r.OK() || r.RetryAfter.IsZero() {
				t.Fatalf("false success/missing cooldown: %s", r.String())
			}
			if read(t, dir, "saved") != "stale" || read(t, dir, "protected-profile") != "unchanged-protected-fixture" {
				t.Fatal("failed restore changed profile")
			}
			before, count := read(t, dir, "events"), requests.Load()
			if name == "network" || name == "unknown" {
				if count != 0 || before != "check\n" {
					t.Fatal("non-auth failure triggered restore")
				}
			}
			if strings.Contains(before, "save\n") || strings.Contains(r.String(), "FAKE_SECRET") {
				t.Fatal("saved unvalidated credentials or exposed transcript")
			}
			if got := p.ensure(context.Background()); got.Status != "cooldown" {
				t.Fatalf("repeat: %s", got.String())
			}
			if read(t, dir, "events") != before || requests.Load() != count {
				t.Fatal("cooldown still performed CLI/CDP work")
			}
		})
	}
}

func TestRecheckDeterminesFinalVerdict(t *testing.T) {
	p, dir, _ := fixture(t, map[string]any{"recheck_failure": true})
	r := p.ensure(context.Background())
	if r.OK() || r.Status != "auth_required" || !strings.HasSuffix(read(t, dir, "events"), "save\ncheck\n") {
		t.Fatalf("recheck ignored: %s", r.String())
	}
}

func TestCancellationTerminatesRestoreSubprocess(t *testing.T) {
	p, dir, _ := fixture(t, map[string]any{"hang": true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() { done <- p.ensure(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if s := read(t, dir, "pid"); s != "" {
			_, _ = fmt.Sscanf(s, "%d", &pid)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("restore subprocess did not start")
	}
	// A separate owner must not overlap the running attempt.
	if r := p.ensure(context.Background()); r.Status != "in_progress" {
		t.Fatalf("lock failed: %s", r.String())
	}
	cancel()
	select {
	case r := <-done:
		if r.OK() {
			t.Fatal("cancelled restore succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not terminate restore")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("subprocess %d survived: %v", pid, err)
	}
	if read(t, dir, "saved") != "stale" {
		t.Fatal("cancellation changed profile")
	}
}

func TestCrossProcessPolicy(t *testing.T) {
	if dir := os.Getenv("BT_AUTH_TEST_PROCESS"); dir != "" {
		p := policy{stateDir: filepath.Join(dir, "policy"), run: func(context.Context, ...string) (string, error) { panic("cooldown/lock bypassed across process") }}
		fmt.Println(p.ensure(context.Background()).Status)
		return
	}
	p, dir, _ := fixture(t, map[string]any{"validation_failure": true})
	if r := p.ensure(context.Background()); r.OK() {
		t.Fatal("fixture unexpectedly valid")
	}
	runChild := func(want string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestCrossProcessPolicy$")
		cmd.Env = append(os.Environ(), "BT_AUTH_TEST_PROCESS="+dir)
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(out), want) {
			t.Fatalf("child=%s err=%v want=%s", out, err, want)
		}
	}
	runChild("cooldown")
	lock, err := os.OpenFile(filepath.Join(p.stateDir, "policy.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	runChild("in_progress")
}

func TestCorruptCooldownFailsClosed(t *testing.T) {
	p, dir, requests := fixture(t, map[string]any{})
	if err := os.MkdirAll(p.stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.stateDir, "cooldown.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := p.ensure(context.Background()); r.Status != "auth_error" {
		t.Fatalf("corrupt state ignored: %s", r.String())
	}
	if requests.Load() != 0 || read(t, dir, "events") != "" {
		t.Fatal("corrupt state triggered auth")
	}
}

func TestCommandUsesUpgradedInterpreter(t *testing.T) {
	cmd := Command(context.Background(), "login", "--check")
	if cmd.Path != pythonPath || !reflect.DeepEqual(cmd.Args[len(cmd.Args)-3:], []string{"cli", "login", "--check"}) {
		t.Fatalf("unexpected invocation: %s", cmd.Path)
	}
}

func TestCronUsesSameCrossProcessCooldown(t *testing.T) {
	p, dir, requests := fixture(t, map[string]any{"validation_failure": true})
	if r := p.ensure(context.Background()); r.OK() {
		t.Fatal("fixture unexpectedly valid")
	}
	before := read(t, dir, "events")
	binary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	// Stand in for bt-notebooklm-auth with this test binary's policy child mode.
	wrapper := filepath.Join(dir, "auth-bin")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+quote(binary)+" -test.run=^TestCrossProcessPolicy$\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "../../scripts/notebooklm-auth-rotate.sh")
	cmd.Env = append(os.Environ(), "BT_AUTH_TEST_PROCESS="+dir, "BT_NOTEBOOKLM_AUTH_BIN="+wrapper)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "cooldown") {
		t.Fatalf("cron bypass: %s %v", out, err)
	}
	if read(t, dir, "events") != before || requests.Load() != 1 {
		t.Fatal("cron repeated restoration during durable cooldown")
	}
}
