package a2a

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
)

// signingKeyEnv, when set, overrides the persisted signing key below — e.g.
// to share one key across a fleet instead of relying on each process's own
// generated key file.
const signingKeyEnv = "A2A_SIGNING_KEY"

// signingKeyFile is where a generated signing key is persisted so every
// process on this machine (and across restarts) signs and verifies with the
// same key without operator setup.
func signingKeyFile() string {
	return filepath.Join(agent.HomeDir(), "a2a_signing.key")
}

var (
	signingKeyOnce  sync.Once
	signingKeyBytes []byte
)

// signingKey returns the process-wide HMAC key used by SignAgentCard /
// VerifyAgentCard: the A2A_SIGNING_KEY env override when set, otherwise a
// key persisted at signingKeyFile(), generated on first use. Either way,
// producing a valid signature requires this key — unlike the pre-fix bare
// SHA-256 hash, which anyone could reproduce with no secret at all.
func signingKey() []byte {
	signingKeyOnce.Do(func() {
		if v := os.Getenv(signingKeyEnv); v != "" {
			signingKeyBytes = []byte(v)
			return
		}
		signingKeyBytes = loadOrCreateSigningKey(signingKeyFile())
	})
	return signingKeyBytes
}

// loadOrCreateSigningKey reads a previously generated key from path, or
// generates and persists a fresh random 32-byte key when none exists yet.
func loadOrCreateSigningKey(path string) []byte {
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return data
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("a2a: generate signing key: " + err.Error())
	}
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0700)
	}
	_ = os.WriteFile(path, key, 0600) // best-effort; an unwritable dir just re-generates the key next process
	return key
}

// SignAgentCard produces an HMAC-SHA256 signature of the card's JSON
// encoding, keyed by signingKey(). Unlike a bare hash, computing a valid
// signature requires knowledge of that key, not just the card's serialized
// bytes.
func SignAgentCard(card *a2a.AgentCard) (string, error) {
	data, err := json.Marshal(card)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, signingKey())
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyAgentCard checks a card signature against its expected HMAC-SHA256
// value, using a constant-time comparison so verification time doesn't leak
// how much of the signature matched.
func VerifyAgentCard(card *a2a.AgentCard, signature string) (bool, error) {
	expected, err := SignAgentCard(card)
	if err != nil {
		return false, err
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false, err
	}
	want, err := hex.DecodeString(expected)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
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
