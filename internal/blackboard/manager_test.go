package blackboard

import "testing"

func TestManager_SetGetList(t *testing.T) {
	m := DefaultManager()
	scope := Scope{Kind: ScopeRun, ID: "run_test"}

	if err := m.Set(scope, "work/note", "hello world", "", "text"); err != nil {
		t.Fatal(err)
	}
	e, err := m.Get(scope, "work/note")
	if err != nil {
		t.Fatal(err)
	}
	if e.Value != "hello world" {
		t.Fatalf("unexpected value: %q", e.Value)
	}
	if e.Summary == "" {
		t.Fatal("expected auto summary")
	}

	list, err := m.List(scope, "work/", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v err=%v", list, err)
	}
}

func TestHandle_RunScope(t *testing.T) {
	m := DefaultManager()
	h := NewHandle(m, "run_1", "", "demo")
	if err := h.Set("obs/1", "payload", "short", "text"); err != nil {
		t.Fatal(err)
	}
	e, err := h.Get("obs/1")
	if err != nil || e.Value != "payload" {
		t.Fatalf("get: %+v err=%v", e, err)
	}
}

func TestManager_Append(t *testing.T) {
	m := DefaultManager()
	scope := Scope{Kind: ScopeRun, ID: "run_append"}

	// First append creates the key with no leading separator.
	e, err := m.Append(scope, "work/log", "first", "\n", "text")
	if err != nil {
		t.Fatal(err)
	}
	if e.Value != "first" {
		t.Fatalf("create append: %q", e.Value)
	}

	// Second append joins with the separator.
	e, err = m.Append(scope, "work/log", "second", "\n", "text")
	if err != nil {
		t.Fatal(err)
	}
	if e.Value != "first\nsecond" {
		t.Fatalf("join append: %q", e.Value)
	}

	got, err := m.Get(scope, "work/log")
	if err != nil || got.Value != "first\nsecond" {
		t.Fatalf("get after append: %+v err=%v", got, err)
	}
	if got.SizeBytes != len("first\nsecond") {
		t.Fatalf("size not updated: %d", got.SizeBytes)
	}
}

func TestManager_AppendConcurrent(t *testing.T) {
	m := DefaultManager()
	scope := Scope{Kind: ScopeSession, ID: "sess_append"}

	const n = 50
	done := make(chan struct{})
	for range n {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := m.Append(scope, "steps/log", "x", "\n", "text"); err != nil {
				t.Errorf("append: %v", err)
			}
		}()
	}
	for range n {
		<-done
	}

	e, err := m.Get(scope, "steps/log")
	if err != nil {
		t.Fatal(err)
	}
	// n "x" values joined by n-1 newlines — no lost updates under the store lock.
	if want := n + (n - 1); len(e.Value) != want {
		t.Fatalf("concurrent append lost updates: got %d bytes, want %d", len(e.Value), want)
	}
}

func TestManager_ListRecent(t *testing.T) {
	m := DefaultManager()
	scope := Scope{Kind: ScopeRun, ID: "run_recent"}

	// Write subtask results in order. Keys are chosen so that lexical order is
	// NOT write order ("sub/10" sorts before "sub/2").
	for _, k := range []string{"sub/1", "sub/2", "sub/10", "sub/3"} {
		if err := m.Set(scope, k, "result-"+k, "", "text"); err != nil {
			t.Fatal(err)
		}
	}
	// Re-write sub/2 last so it is unambiguously the most recently updated entry.
	if err := m.Set(scope, "sub/2", "result-sub/2-updated", "", "text"); err != nil {
		t.Fatal(err)
	}

	recent, err := m.ListRecent(scope, "sub/", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("ListRecent limit: got %d entries, want 2", len(recent))
	}
	if recent[0].Key != "sub/2" {
		t.Fatalf("ListRecent must surface the newest write first, got %q", recent[0].Key)
	}

	// Contrast: key-sorted List truncates to the lexically smallest keys and so
	// hides the most recent write behind the limit — the gap ListRecent fills.
	byKey, err := m.List(scope, "sub/", 2)
	if err != nil {
		t.Fatal(err)
	}
	if byKey[0].Key != "sub/1" {
		t.Fatalf("List should be key-sorted, got %q first", byKey[0].Key)
	}
	for _, e := range byKey {
		if e.Key == "sub/2" {
			t.Fatal("expected key-sorted List to hide the newest write (sub/2) behind the limit")
		}
	}
}

func TestManager_Limits(t *testing.T) {
	m := NewManager(map[ScopeKind]Limits{
		ScopeRun: {MaxEntries: 1, MaxTotalBytes: 100},
	})
	scope := Scope{Kind: ScopeRun, ID: "limited"}
	if err := m.Set(scope, "a", "x", "", "text"); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(scope, "b", "y", "", "text"); err == nil {
		t.Fatal("expected entry limit error")
	}
}

func TestHandle_SessionScope(t *testing.T) {
	m := DefaultManager()
	h := NewHandle(m, "run_1", "sess_pipeline", "demo")
	if err := h.SetSession("steps/a/output", "step result", "summary", "text"); err != nil {
		t.Fatal(err)
	}
	e, err := h.GetSession("steps/a/output")
	if err != nil || e.Value != "step result" {
		t.Fatalf("session get: %+v err=%v", e, err)
	}
	list, err := h.ListSession("steps/", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("session list: %v err=%v", list, err)
	}
}

func TestListPersistedScopeIDs(t *testing.T) {
	dir := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeAgent, ID: "demo-agent"}
	if err := m.Set(scope, "runs/latest/output", "hello", "", "text"); err != nil {
		t.Fatal(err)
	}
	ids, err := m.ListPersistedScopeIDs(ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "demo-agent" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}
