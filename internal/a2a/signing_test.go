package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func TestSignAgentCard(t *testing.T) {
	card := &a2a.AgentCard{
		Name:        "test-agent",
		Description: "A test agent card",
		Version:     "1.0.0",
	}

	sig, err := SignAgentCard(card)
	if err != nil {
		t.Fatalf("SignAgentCard failed: %v", err)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}
	// SHA-256 produces 64 hex chars
	if len(sig) != 64 {
		t.Errorf("expected signature length 64, got %d", len(sig))
	}
}

func TestSignAgentCard_Deterministic(t *testing.T) {
	card := &a2a.AgentCard{
		Name: "deterministic-agent",
	}

	sig1, err := SignAgentCard(card)
	if err != nil {
		t.Fatalf("SignAgentCard first call failed: %v", err)
	}
	sig2, err := SignAgentCard(card)
	if err != nil {
		t.Fatalf("SignAgentCard second call failed: %v", err)
	}
	if sig1 != sig2 {
		t.Errorf("expected deterministic signing, got %q vs %q", sig1, sig2)
	}
}

func TestSignAgentCard_DifferentCards(t *testing.T) {
	cardA := &a2a.AgentCard{Name: "agent-a", Version: "1.0.0"}
	cardB := &a2a.AgentCard{Name: "agent-b", Version: "2.0.0"}

	sigA, _ := SignAgentCard(cardA)
	sigB, _ := SignAgentCard(cardB)
	if sigA == sigB {
		t.Error("expected different cards to produce different signatures")
	}
}

func TestVerifyAgentCard_Valid(t *testing.T) {
	card := &a2a.AgentCard{
		Name:        "verify-me",
		Description: "Card to verify",
	}

	sig, err := SignAgentCard(card)
	if err != nil {
		t.Fatalf("SignAgentCard failed: %v", err)
	}

	valid, err := VerifyAgentCard(card, sig)
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if !valid {
		t.Error("expected signature verification to pass")
	}
}

func TestVerifyAgentCard_Invalid(t *testing.T) {
	card := &a2a.AgentCard{
		Name: "tampered-agent",
	}

	valid, err := VerifyAgentCard(card, "deadbeef"+"deadbeef"+"deadbeef"+"deadbeef"+
		"deadbeef"+"deadbeef"+"deadbeef"+"deadbeef")
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if valid {
		t.Error("expected invalid signature verification to fail")
	}
}

func TestVerifyAgentCard_EmptySignature(t *testing.T) {
	card := &a2a.AgentCard{
		Name: "empty-sig-agent",
	}

	valid, err := VerifyAgentCard(card, "")
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if valid {
		t.Error("expected empty signature verification to fail")
	}
}

func TestVerifyAgentCard_AfterModification(t *testing.T) {
	card := &a2a.AgentCard{
		Name:        "original",
		Description: "original description",
	}

	sig, err := SignAgentCard(card)
	if err != nil {
		t.Fatalf("SignAgentCard failed: %v", err)
	}

	// Modify the card after signing
	card.Description = "tampered description"

	valid, err := VerifyAgentCard(card, sig)
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if valid {
		t.Error("expected verification to fail after card modification")
	}
}

func TestSignAgentCard_NilCard(t *testing.T) {
	// Note: json.Marshal(nil) returns "null" which produces a deterministic hash
	// This is fine — we just verify it doesn't panic
	_, err := SignAgentCard(nil)
	if err != nil {
		t.Fatalf("SignAgentCard(nil) should not error, got: %v", err)
	}
}
