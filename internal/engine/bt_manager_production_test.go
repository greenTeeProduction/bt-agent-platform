package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestBTManagerTargetNamePrefersMeaningfulIdentifiers(t *testing.T) {
	records := []evolution.Record{{
		TaskID:        "hermes-monitor-123",
		Task:          "scheduled run for hermes-monitor",
		TreeName:      "",
		Outcome:       evolution.Failure,
		WhatToImprove: []string{"timeout waiting for disk check"},
	}}

	groups := groupByTreeName(records)
	if _, ok := groups["(unnamed)"]; ok {
		t.Fatal("unnamed bucket should be replaced by a meaningful agent/tree target")
	}
	if _, ok := groups["hermes-monitor"]; !ok {
		t.Fatalf("expected hermes-monitor target, got %#v", groups)
	}
}
