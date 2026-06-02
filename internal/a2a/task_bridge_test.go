package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestTaskStateBridge_BTToA2A(t *testing.T) {
	b := &TaskStateBridge{}

	tests := []struct {
		btOutcome string
		expected  a2a.TaskState
	}{
		{"success", a2a.TaskStateCompleted},
		{"failure", a2a.TaskStateFailed},
		{"running", a2a.TaskStateWorking},
		{"input-required", a2a.TaskStateInputRequired},
		{"unknown_outcome", a2a.TaskStateFailed},
		{"", a2a.TaskStateFailed},
		{"partial", a2a.TaskStateFailed},
		{"cancelled", a2a.TaskStateFailed},
	}

	for _, tt := range tests {
		got := b.BTToA2A(tt.btOutcome)
		if got != tt.expected {
			t.Errorf("BTToA2A(%q) = %v, want %v", tt.btOutcome, got, tt.expected)
		}
	}
}

func TestTaskStateBridge_A2AToBT(t *testing.T) {
	b := &TaskStateBridge{}

	tests := []struct {
		state    a2a.TaskState
		expected string
	}{
		{a2a.TaskStateCompleted, "success"},
		{a2a.TaskStateFailed, "failure"},
		{a2a.TaskStateCanceled, "cancelled"},
		{a2a.TaskStateWorking, "running"},
		{a2a.TaskStateInputRequired, "unknown"},
		{a2a.TaskStateSubmitted, "unknown"},
		{a2a.TaskState("UNKNOWN_STATE"), "unknown"},
	}

	for _, tt := range tests {
		got := b.A2AToBT(tt.state)
		if got != tt.expected {
			t.Errorf("A2AToBT(%v) = %q, want %q", tt.state, got, tt.expected)
		}
	}
}

func TestTaskStateBridge_IsTerminal(t *testing.T) {
	b := &TaskStateBridge{}

	tests := []struct {
		state    a2a.TaskState
		terminal bool
	}{
		{a2a.TaskStateCompleted, true},
		{a2a.TaskStateFailed, true},
		{a2a.TaskStateCanceled, true},
		{a2a.TaskStateWorking, false},
		{a2a.TaskStateInputRequired, false},
		{a2a.TaskStateSubmitted, false},
	}

	for _, tt := range tests {
		got := b.IsTerminal(tt.state)
		if got != tt.terminal {
			t.Errorf("IsTerminal(%v) = %v, want %v", tt.state, got, tt.terminal)
		}
	}
}
