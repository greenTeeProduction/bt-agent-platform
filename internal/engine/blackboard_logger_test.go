package engine

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestBlackboardLog_FallsBackToGlobal(t *testing.T) {
	bb := &Blackboard{}
	if bb.Log() == nil {
		t.Fatal("Log() must never return nil")
	}
}

func TestBlackboardLog_UsesBoundLogger(t *testing.T) {
	var buf bytes.Buffer
	bb := &Blackboard{Logger: slog.New(slog.NewJSONHandler(&buf, nil)).With(
		"run_id", "r-1", "agent", "a-1", "tree", "t-1")}
	bb.Log().Info("hello")
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["run_id"] != "r-1" || rec["agent"] != "a-1" || rec["tree"] != "t-1" {
		t.Fatalf("missing bound fields: %v", rec)
	}
}
