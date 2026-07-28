package a2a

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
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

// TestVerifyAgentCard_RejectsUnkeyedForgery is the regression test for the
// "fake signature" vulnerability: the pre-fix SignAgentCard was a bare,
// unkeyed SHA-256 hash of the card's JSON encoding, so anyone — with no
// access to any secret — could compute the exact same hash themselves and
// forge a signature VerifyAgentCard would accept. A real keyed scheme
// (HMAC-SHA256 with a secret key, or ed25519) must reject a signature
// produced this way, since producing a valid signature must require
// knowledge of the secret, not just the card's serialized bytes.
func TestVerifyAgentCard_RejectsUnkeyedForgery(t *testing.T) {
	card := &a2a.AgentCard{
		Name:        "forge-target",
		Description: "attacker-controlled card",
	}

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	hash := sha256.Sum256(data)
	forged := hex.EncodeToString(hash[:])

	valid, err := VerifyAgentCard(card, forged)
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if valid {
		t.Error("expected an unkeyed SHA-256 forgery to be rejected by a real keyed signature scheme")
	}
}

// ---- wiring: card-serving path (card.go) ------------------------------------

// ConvertToAgentCard is the single origin point every served/cached AgentCard
// passes through (BuildCardRegistry, and the server's per-bid card build). It
// must attach a signature so downstream consumers can detect tampering.
func TestConvertToAgentCard_AttachesSignature(t *testing.T) {
	def := agent.Definition{
		Name:        "signed-agent",
		Description: "agent whose card should carry a signature",
		Version:     "1.0.0",
		Tree:        "domain:code_review",
	}

	card, err := ConvertToAgentCard(def, "http://localhost:8686")
	if err != nil {
		t.Fatalf("ConvertToAgentCard failed: %v", err)
	}

	if len(card.Signatures) == 0 {
		t.Fatal("expected ConvertToAgentCard to attach a signature to the card")
	}

	sig := card.Signatures[len(card.Signatures)-1].Signature
	card.Signatures = card.Signatures[:len(card.Signatures)-1]
	valid, err := VerifyAgentCard(card, sig)
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if !valid {
		t.Error("attached signature does not verify against the card's content")
	}
}

// ---- wiring: card-consuming path (auction.go) -------------------------------

// auctionCandidates is production's real trust boundary: it turns the live
// card registry into dispatchable candidate URLs for AuctionDelegate. A card
// whose attached signature no longer matches its content (tampered after
// signing, or forged) must never become a dispatchable candidate — but a card
// that was never signed at all must still be trusted, since signing is
// opt-in and existing candidate cards carry none.
func TestAuctionCandidates_RejectsTamperedCardSignature(t *testing.T) {
	valid := cardWithURL("valid", "http://valid", "domain")
	sig, err := SignAgentCard(valid)
	if err != nil {
		t.Fatalf("SignAgentCard failed: %v", err)
	}
	valid.Signatures = []a2a.AgentCardSignature{{Signature: sig}}

	tampered := cardWithURL("tampered", "http://tampered", "domain")
	sig2, err := SignAgentCard(tampered)
	if err != nil {
		t.Fatalf("SignAgentCard failed: %v", err)
	}
	tampered.Signatures = []a2a.AgentCardSignature{{Signature: sig2}}
	tampered.Name = "tampered-modified" // mutated after signing: signature no longer matches

	unsigned := cardWithURL("unsigned", "http://unsigned", "domain")

	origCards := AuctionCardsFn
	AuctionCardsFn = func() map[string]*a2a.AgentCard {
		return map[string]*a2a.AgentCard{
			"valid":    valid,
			"tampered": tampered,
			"unsigned": unsigned,
		}
	}
	t.Cleanup(func() { AuctionCardsFn = origCards })

	got := auctionCandidates(TaskAnnouncement{TaskID: "t1"}, nil)

	if _, ok := got["tampered"]; ok {
		t.Error("expected candidate with a tampered card signature to be excluded")
	}
	if _, ok := got["valid"]; !ok {
		t.Error("expected candidate with a valid card signature to be included")
	}
	if _, ok := got["unsigned"]; !ok {
		t.Error("expected candidate with no card signature at all to remain trusted (opt-in signing)")
	}
}

// ---- wiring: card HTTP-serving path (server.go) -----------------------------

// The global agent card served at /.well-known/agent-card.json is assembled
// ad hoc in handleGlobalAgentCard rather than via ConvertToAgentCard, so it
// needs its own signing step before being written to the response.
func TestHandleGlobalAgentCard_ResponseIsSigned(t *testing.T) {
	s := &Server{
		BaseURL: "http://localhost:8686",
		CardCache: map[string]*a2a.AgentCard{
			"agent-a": cardWithURL("agent-a", "http://agent-a", "domain"),
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	rec := httptest.NewRecorder()
	s.handleGlobalAgentCard(rec, req)

	var card a2a.AgentCard
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("failed to decode served agent card: %v", err)
	}

	if len(card.Signatures) == 0 {
		t.Fatal("expected served global agent card to carry a signature")
	}

	sig := card.Signatures[len(card.Signatures)-1].Signature
	card.Signatures = card.Signatures[:len(card.Signatures)-1]
	valid, err := VerifyAgentCard(&card, sig)
	if err != nil {
		t.Fatalf("VerifyAgentCard failed: %v", err)
	}
	if !valid {
		t.Error("served global agent card signature does not verify against its content")
	}
}
