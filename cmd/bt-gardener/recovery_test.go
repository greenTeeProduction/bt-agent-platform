package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryCommandRequiresOfflineAndReportsCommittedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tree-default.json")
	bad := []byte(`{"type":"Sequence","name":"WrongRoot","children":[{"type":"Action","name":"SelfCorrect"}]}`)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if err := runTreeRecovery(t.Context(), []string{"--dir", dir, "--apply"}, &out, &diagnostic); err == nil {
		t.Fatal("online apply admitted")
	}
	if err := runTreeRecovery(t.Context(), []string{"--dir", dir}, &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, bad) {
		t.Fatal("inspection changed tree")
	}
	out.Reset()
	if err := runTreeRecovery(t.Context(), []string{"--dir", dir, "--apply", "--offline"}, &out, &diagnostic); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Applied  bool
		Restored int
		Manifest string
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Restored != 1 || report.Manifest == "" {
		t.Fatalf("unacknowledged recovery: %s", out.String())
	}
	if _, err := os.Stat(report.Manifest); err != nil {
		t.Fatal(err)
	}
}
