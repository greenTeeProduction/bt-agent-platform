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
