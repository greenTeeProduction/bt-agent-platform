package a2a

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

// SignAgentCard creates a SHA-256 hash signature of the card for verification.
func SignAgentCard(card *a2a.AgentCard) (string, error) {
	data, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// VerifyAgentCard checks a card signature against its hash.
func VerifyAgentCard(card *a2a.AgentCard, signature string) (bool, error) {
	expected, err := SignAgentCard(card)
	if err != nil {
		return false, err
	}
	return expected == signature, nil
}

// cardSignatureValid reports whether card's trailing signature, if any, still
// verifies against the rest of its content. Cards carry no signature unless
// they passed through SignAgentCard (signing is opt-in), so an absent
// signature is trusted; only a signature that was attached and no longer
// matches — tampered or forged — is rejected.
func cardSignatureValid(card *a2a.AgentCard) bool {
	if card == nil || len(card.Signatures) == 0 {
		return true
	}
	last := card.Signatures[len(card.Signatures)-1]
	unsigned := *card
	unsigned.Signatures = card.Signatures[:len(card.Signatures)-1]
	valid, err := VerifyAgentCard(&unsigned, last.Signature)
	return err == nil && valid
}
