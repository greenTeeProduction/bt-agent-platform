package engine

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// Characterization tests for blackboard_tools.go. These pin the current
// exported behavior of PrepareBlackboard, the bb_* ReAct tools, and their
// helper functions so future refactors don't silently change semantics.
// TestBBRecentTool_NewestFirst / TestBBSessionRecentTool in
// blackboard_tools_recent_test.go already cover bb_recent / bb_session_recent
// and provide the findBBTool helper reused here.

// ─── PrepareBlackboard / attachBlackboardTools ──────────────────────────────

func TestPrepareBlackboard_NilGuards(t *testing.T) {
	t.Run("nil blackboard is a no-op", func(t *testing.T) {
		PrepareBlackboard(nil) // must not panic
	})

	t.Run("nil BB handle is a no-op", func(t *testing.T) {
		bb := &Blackboard{}
		PrepareBlackboard(bb) // must not panic
		if bb.ChainState != nil {
			t.Errorf("ChainState = %#v, want nil (no BB handle to prepare)", bb.ChainState)
		}
		if len(bb.ChainTools) != 0 {
			t.Errorf("ChainTools = %#v, want empty", bb.ChainTools)
		}
	})
}

func TestPrepareBlackboard_ChainStateMetadata(t *testing.T) {
	mgr := blackboard.DefaultManager()

	t.Run("populates run_id, agent_name, session_id", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_meta_1", "sess_meta_1", "agent_meta")
		bb := &Blackboard{BB: h}

		PrepareBlackboard(bb)

		if bb.ChainState["run_id"] != "run_meta_1" {
			t.Errorf("run_id = %v, want run_meta_1", bb.ChainState["run_id"])
		}
		if bb.ChainState["agent_name"] != "agent_meta" {
			t.Errorf("agent_name = %v, want agent_meta", bb.ChainState["agent_name"])
		}
		if bb.ChainState["session_id"] != "sess_meta_1" {
			t.Errorf("session_id = %v, want sess_meta_1", bb.ChainState["session_id"])
		}
	})

	t.Run("omits agent_name and session_id when empty", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_meta_2", "", "")
		bb := &Blackboard{BB: h}

		PrepareBlackboard(bb)

		if _, present := bb.ChainState["agent_name"]; present {
			t.Errorf("agent_name should be absent, got %v", bb.ChainState["agent_name"])
		}
		if _, present := bb.ChainState["session_id"]; present {
			t.Errorf("session_id should be absent, got %v", bb.ChainState["session_id"])
		}
	})

	t.Run("existing ChainState is reused, not replaced", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_meta_3", "", "")
		bb := &Blackboard{BB: h, ChainState: map[string]any{"unrelated": "keep-me"}}

		PrepareBlackboard(bb)

		if bb.ChainState["unrelated"] != "keep-me" {
			t.Errorf("unrelated key lost: %#v", bb.ChainState)
		}
		if bb.ChainState["run_id"] != "run_meta_3" {
			t.Errorf("run_id = %v, want run_meta_3", bb.ChainState["run_id"])
		}
	})
}

func TestPrepareBlackboard_ToolAttachment(t *testing.T) {
	mgr := blackboard.DefaultManager()
	runToolNames := []string{"bb_read", "bb_write", "bb_list", "bb_recent", "bb_append"}
	sessionToolNames := []string{"bb_session_read", "bb_session_write", "bb_session_list", "bb_session_recent", "bb_session_append"}

	t.Run("no session: attaches only run tools", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_attach_1", "", "demo")
		bb := &Blackboard{BB: h}

		PrepareBlackboard(bb)

		for _, name := range runToolNames {
			findBBTool(t, bb, name)
		}
		for _, name := range sessionToolNames {
			for _, tool := range bb.ChainTools {
				if bt, ok := tool.(*bbTool); ok && bt.Name() == name {
					t.Errorf("session tool %q attached without a session", name)
				}
			}
		}
		if got := len(bb.ChainTools); got != len(runToolNames) {
			t.Errorf("ChainTools len = %d, want %d", got, len(runToolNames))
		}
	})

	t.Run("with session: attaches run and session tools", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_attach_2", "sess_attach_2", "demo")
		bb := &Blackboard{BB: h}

		PrepareBlackboard(bb)

		for _, name := range append(append([]string{}, runToolNames...), sessionToolNames...) {
			findBBTool(t, bb, name)
		}
		if got := len(bb.ChainTools); got != len(runToolNames)+len(sessionToolNames) {
			t.Errorf("ChainTools len = %d, want %d", got, len(runToolNames)+len(sessionToolNames))
		}
	})

	t.Run("idempotent: calling twice does not duplicate tools", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_attach_3", "sess_attach_3", "demo")
		bb := &Blackboard{BB: h}

		PrepareBlackboard(bb)
		PrepareBlackboard(bb)

		if got := len(bb.ChainTools); got != len(runToolNames)+len(sessionToolNames) {
			t.Errorf("ChainTools len = %d after double prepare, want %d (no duplicates)", got, len(runToolNames)+len(sessionToolNames))
		}
	})

	t.Run("run tools present, session added later gets session tools only", func(t *testing.T) {
		h := blackboard.NewHandle(mgr, "run_attach_4", "", "demo")
		bb := &Blackboard{BB: h}
		PrepareBlackboard(bb) // attaches run tools only

		h.SessionID = "sess_attach_4"
		attachBlackboardTools(bb)

		for _, name := range append(append([]string{}, runToolNames...), sessionToolNames...) {
			findBBTool(t, bb, name)
		}
		if got := len(bb.ChainTools); got != len(runToolNames)+len(sessionToolNames) {
			t.Errorf("ChainTools len = %d, want %d (run tools not duplicated)", got, len(runToolNames)+len(sessionToolNames))
		}
	})
}

// ─── bbTool dispatch ─────────────────────────────────────────────────────────

func TestBBTool_NameAndDescription(t *testing.T) {
	tool := &bbTool{name: "bb_read", description: "reads a value"}
	if tool.Name() != "bb_read" {
		t.Errorf("Name() = %q, want bb_read", tool.Name())
	}
	if tool.Description() != "reads a value" {
		t.Errorf("Description() = %q, want %q", tool.Description(), "reads a value")
	}
}

func TestBBTool_Call_UnknownKind(t *testing.T) {
	tool := &bbTool{name: "bb_mystery", kind: "does-not-exist"}
	got := tool.Call("anything")
	if got != "unknown bb tool kind" {
		t.Errorf("Call() = %q, want %q", got, "unknown bb tool kind")
	}
}

// ─── bb_read / bb_write / bb_list / bb_append (run scope) ───────────────────

func TestBBToolRead(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_read_1", "", "demo")
	bb := &Blackboard{BB: h}
	PrepareBlackboard(bb)

	read := findBBTool(t, bb, "bb_read")
	write := findBBTool(t, bb, "bb_write")

	t.Run("empty key is an error", func(t *testing.T) {
		if got := read.Call("   "); got != "error: key required" {
			t.Errorf("Call(empty) = %q, want %q", got, "error: key required")
		}
	})

	t.Run("missing key surfaces the manager error", func(t *testing.T) {
		got := read.Call("no/such/key")
		if !strings.HasPrefix(got, "error: ") {
			t.Errorf("Call(missing) = %q, want error: prefix", got)
		}
	})

	t.Run("round-trips a written value", func(t *testing.T) {
		write.Call(`{"key":"work/notes","value":"hello world"}`)
		got := read.Call("  work/notes  ")
		if got != "hello world" {
			t.Errorf("Call(work/notes) = %q, want %q", got, "hello world")
		}
	})

	t.Run("summary distinct from value is prefixed", func(t *testing.T) {
		write.Call(`{"key":"work/summarized","value":"the long body","summary":"short gist"}`)
		got := read.Call("work/summarized")
		want := "key=work/summarized summary=short gist value=the long body"
		if got != want {
			t.Errorf("Call(work/summarized) = %q, want %q", got, want)
		}
	})
}

func TestBBToolWrite(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_write_1", "", "demo")
	bb := &Blackboard{BB: h}
	PrepareBlackboard(bb)
	write := findBBTool(t, bb, "bb_write")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"json form", `{"key":"work/a","value":"1234"}`, "stored key=work/a bytes=4"},
		{"key=value fallback form", "work/b=hello", "stored key=work/b bytes=5"},
		{"empty input errors", "", "error: empty input"},
		{"malformed json errors", "{not json", "error: invalid character 'n' looking for beginning of object key string"},
		{"json missing key errors", `{"value":"x"}`, "error: key required"},
		{"non-json without separator errors", "no-equals-sign", "error: expected JSON or key=value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := write.Call(tt.input); got != tt.want {
				t.Errorf("Call(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBBToolList(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_list_1", "", "demo")
	bb := &Blackboard{BB: h}
	PrepareBlackboard(bb)
	list := findBBTool(t, bb, "bb_list")
	write := findBBTool(t, bb, "bb_write")

	t.Run("no keys yet", func(t *testing.T) {
		if got := list.Call(""); got != "(no keys)" {
			t.Errorf("Call(empty scope) = %q, want %q", got, "(no keys)")
		}
	})

	t.Run("lists written keys with size and summary", func(t *testing.T) {
		write.Call(`{"key":"list/a","value":"ab","summary":"s"}`)
		got := list.Call("list/")
		want := "- list/a (2 bytes) summary=s"
		if got != want {
			t.Errorf("Call(list/) = %q, want %q", got, want)
		}
	})
}

func TestBBToolAppend(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_append_1", "", "demo")
	bb := &Blackboard{BB: h}
	PrepareBlackboard(bb)
	appendTool := findBBTool(t, bb, "bb_append")
	read := findBBTool(t, bb, "bb_read")

	t.Run("creates the key on first append", func(t *testing.T) {
		got := appendTool.Call("log/x=line1")
		if got != "appended key=log/x bytes=5 total=5" {
			t.Errorf("Call(first) = %q, want %q", got, "appended key=log/x bytes=5 total=5")
		}
	})

	t.Run("joins subsequent appends with newline", func(t *testing.T) {
		got := appendTool.Call("log/x=line2")
		if got != "appended key=log/x bytes=5 total=11" {
			t.Errorf("Call(second) = %q, want %q", got, "appended key=log/x bytes=5 total=11")
		}
		if readGot := read.Call("log/x"); readGot != "line1\nline2" {
			t.Errorf("read after append = %q, want %q", readGot, "line1\nline2")
		}
	})

	t.Run("malformed input errors", func(t *testing.T) {
		if got := appendTool.Call(""); got != "error: empty input" {
			t.Errorf("Call(empty) = %q, want %q", got, "error: empty input")
		}
	})

	t.Run("handle not configured errors", func(t *testing.T) {
		unconfigured := &bbTool{name: "bb_append", kind: "append", handle: &blackboard.Handle{}}
		got := unconfigured.Call("k=v")
		if got != "error: blackboard handle not configured" {
			t.Errorf("Call(unconfigured) = %q, want %q", got, "error: blackboard handle not configured")
		}
	})
}

// ─── bb_session_* tools ──────────────────────────────────────────────────────

func TestBBSessionTools(t *testing.T) {
	mgr := blackboard.DefaultManager()
	h := blackboard.NewHandle(mgr, "run_sess_1", "sess_1", "demo")
	bb := &Blackboard{BB: h}
	PrepareBlackboard(bb)

	sWrite := findBBTool(t, bb, "bb_session_write")
	sRead := findBBTool(t, bb, "bb_session_read")
	sList := findBBTool(t, bb, "bb_session_list")
	sAppend := findBBTool(t, bb, "bb_session_append")

	t.Run("write then read round-trips in session scope", func(t *testing.T) {
		sWrite.Call(`{"key":"steps/x","value":"session-value"}`)
		if got := sRead.Call("steps/x"); got != "session-value" {
			t.Errorf("session read = %q, want %q", got, "session-value")
		}
	})

	t.Run("session list reflects written keys", func(t *testing.T) {
		got := sList.Call("steps/")
		if !strings.Contains(got, "steps/x") {
			t.Errorf("session list = %q, want it to contain steps/x", got)
		}
	})

	t.Run("session append accumulates", func(t *testing.T) {
		sAppend.Call("steps/log=first")
		got := sAppend.Call("steps/log=second")
		if got != "appended session key=steps/log bytes=6 total=12" {
			t.Errorf("Call(session append) = %q, want %q", got, "appended session key=steps/log bytes=6 total=12")
		}
	})

	t.Run("session read of missing key errors", func(t *testing.T) {
		got := sRead.Call("no/such/key")
		if !strings.HasPrefix(got, "error: ") {
			t.Errorf("session read(missing) = %q, want error: prefix", got)
		}
	})

	t.Run("session read empty key errors", func(t *testing.T) {
		if got := sRead.Call(""); got != "error: key required" {
			t.Errorf("Call(empty) = %q, want %q", got, "error: key required")
		}
	})

	t.Run("no session configured errors on write", func(t *testing.T) {
		noSession := blackboard.NewHandle(mgr, "run_sess_2", "", "demo")
		tool := &bbTool{name: "bb_session_write", kind: "session_write", handle: noSession}
		got := tool.Call(`{"key":"x","value":"y"}`)
		if got != "error: session scope not configured" {
			t.Errorf("Call(no session) = %q, want %q", got, "error: session scope not configured")
		}
	})

	t.Run("no session configured errors on append", func(t *testing.T) {
		noSession := blackboard.NewHandle(mgr, "run_sess_3", "", "demo")
		tool := &bbTool{name: "bb_session_append", kind: "session_append", handle: noSession}
		got := tool.Call("x=y")
		if got != "error: session scope not configured" {
			t.Errorf("Call(no session) = %q, want %q", got, "error: session scope not configured")
		}
	})
}

// ─── formatBBEntry / formatBBEntries ─────────────────────────────────────────

func TestFormatBBEntry(t *testing.T) {
	t.Run("plain value with no distinct summary", func(t *testing.T) {
		e := blackboard.Entry{Key: "k", Value: "just the value"}
		if got := formatBBEntry(e); got != "just the value" {
			t.Errorf("formatBBEntry = %q, want %q", got, "just the value")
		}
	})

	t.Run("summary equal to value is not repeated", func(t *testing.T) {
		e := blackboard.Entry{Key: "k", Value: "same", Summary: "same"}
		if got := formatBBEntry(e); got != "same" {
			t.Errorf("formatBBEntry = %q, want %q", got, "same")
		}
	})

	t.Run("distinct summary is prefixed", func(t *testing.T) {
		e := blackboard.Entry{Key: "k", Value: "body", Summary: "gist"}
		want := "key=k summary=gist value=body"
		if got := formatBBEntry(e); got != want {
			t.Errorf("formatBBEntry = %q, want %q", got, want)
		}
	})

	t.Run("long ASCII value is truncated with byte count", func(t *testing.T) {
		val := strings.Repeat("a", bbReadMaxDisplay+50)
		e := blackboard.Entry{Key: "k", Value: val}
		got := formatBBEntry(e)
		want := "key=k summary= value=" + strings.Repeat("a", bbReadMaxDisplay) + "... [truncated, 4050 bytes total]"
		if got != want {
			t.Errorf("formatBBEntry mismatch (showing lengths only): got len=%d, want len=%d", len(got), len(want))
		}
	})

	// Characterizes the intended contract: truncating a value that contains
	// multi-byte UTF-8 characters must still produce valid UTF-8 output, since
	// the result is displayed directly to an LLM/agent consumer. Today's
	// implementation slices at a fixed byte offset (bbReadMaxDisplay) with no
	// regard for rune boundaries, so a multi-byte rune straddling that offset
	// is cut in half and the truncated text becomes invalid UTF-8. This test
	// documents the desired behavior and currently fails until the slice is
	// widened/narrowed to the nearest rune boundary.
	t.Run("truncation of multi-byte content stays valid UTF-8", func(t *testing.T) {
		prefix := strings.Repeat("a", bbReadMaxDisplay-1)
		val := prefix + "字" + strings.Repeat("b", 100)
		e := blackboard.Entry{Key: "k", Value: val}
		got := formatBBEntry(e)
		if !utf8.ValidString(got) {
			t.Fatalf("formatBBEntry produced invalid UTF-8 for a truncated multi-byte value: %q", got)
		}
	})
}

func TestFormatBBEntries(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		if got := formatBBEntries(nil); got != "(no keys)" {
			t.Errorf("formatBBEntries(nil) = %q, want %q", got, "(no keys)")
		}
	})

	t.Run("multiple entries, one line each", func(t *testing.T) {
		entries := []blackboard.Entry{
			{Key: "a", SizeBytes: 3, Summary: "sa"},
			{Key: "b", SizeBytes: 7, Summary: ""},
		}
		want := "- a (3 bytes) summary=sa\n- b (7 bytes) summary="
		if got := formatBBEntries(entries); got != want {
			t.Errorf("formatBBEntries = %q, want %q", got, want)
		}
	})
}

// ─── parseBBWriteInput ────────────────────────────────────────────────────────

func TestParseBBWriteInput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantKey     string
		wantValue   string
		wantSummary string
		wantErr     bool
	}{
		{"empty input errors", "", "", "", "", true},
		{"whitespace-only input errors", "   ", "", "", "", true},
		{"valid json with summary", `{"key":"k","value":"v","summary":"s"}`, "k", "v", "s", false},
		{"valid json without summary", `{"key":"k","value":"v"}`, "k", "v", "", false},
		{"json missing key errors", `{"value":"v"}`, "", "", "", true},
		{"json blank key errors", `{"key":"   ","value":"v"}`, "", "", "", true},
		{"malformed json errors", `{"key":`, "", "", "", true},
		{"key=value fallback", "k=v", "k", "v", "", false},
		{"key=value trims whitespace", "  k  =  v  ", "k", "v", "", false},
		{"value may contain further equals signs", "k=a=b=c", "k", "a=b=c", "", false},
		{"no separator errors", "no-equals-here", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, summary, err := parseBBWriteInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
			if summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tt.wantSummary)
			}
		})
	}
}
