package persona

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Automation lifecycle states. A proposal moves pending → approved (agent
// scheduled) or pending → rejected (never re-proposed for the same pattern).
const (
	AutomationPending  = "pending"
	AutomationApproved = "approved"
	AutomationRejected = "rejected"
	// AutomationFlagged pauses an automation pending human review: unlike
	// Pending/Approved/Rejected, this state is entered only via repeated
	// negative user feedback (Q4 Personalization milestone 2/3), never via
	// the autopilot proposal flow — but it is still a plain status string so
	// the engine's execution gate (Status == AutomationApproved) treats it as
	// non-executable without any change on that side.
	AutomationFlagged = "flagged"
)

// AutomationRecord tracks one automation proposal derived from a recurring
// pattern, keyed by the pattern's keyword signature. It is the autopilot's
// dedup + rejection memory: the same habit is proposed at most once, and a
// rejected proposal is never re-raised (ADR-133 Phase 4 anti-spam rail).
type AutomationRecord struct {
	// Signature is the stable keyword fingerprint of the pattern (see
	// PatternSignature).
	Signature string `json:"signature"`
	Status    string `json:"status"` // pending | approved | rejected
	// HITLID is the approval request handling this proposal.
	HITLID string `json:"hitl_id,omitempty"`
	TreeID string `json:"tree_id,omitempty"`
	// AgentName is set once an approved proposal is activated as a
	// scheduled agent.
	AgentName string `json:"agent_name,omitempty"`
	// Representative is the task text the pattern was mined from.
	Representative string `json:"representative,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// AutomationStore persists a user's automation-proposal ledger at
// <workspace>/automations.json (ADR-003 atomic writes).
type AutomationStore struct {
	mu   sync.Mutex
	path string
}

// AutomationsPath is where the workspace keeps the proposal ledger.
func (w Workspace) AutomationsPath() string {
	return filepath.Join(w.Root, "automations.json")
}

// NewAutomationStore opens the ledger for a user workspace, creating the
// workspace directory if needed.
func NewAutomationStore(w Workspace) (*AutomationStore, error) {
	if err := os.MkdirAll(w.Root, 0755); err != nil {
		return nil, fmt.Errorf("persona: automation store: %w", err)
	}
	return &AutomationStore{path: w.AutomationsPath()}, nil
}

// All returns every record, newest first.
func (s *AutomationStore) All() ([]AutomationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(records, func(a, b AutomationRecord) int {
		return cmp.Compare(b.CreatedAt, a.CreatedAt)
	})
	return records, nil
}

// Get returns the record for a pattern signature.
func (s *AutomationStore) Get(signature string) (*AutomationRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return nil, false, err
	}
	for i := range records {
		if records[i].Signature == signature {
			return &records[i], true, nil
		}
	}
	return nil, false, nil
}

// Upsert inserts or replaces the record with the same signature.
func (s *AutomationStore) Upsert(rec AutomationRecord) error {
	if strings.TrimSpace(rec.Signature) == "" {
		return fmt.Errorf("persona: automation record requires a signature")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	rec.UpdatedAt = now
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	replaced := false
	for i := range records {
		if records[i].Signature == rec.Signature {
			if rec.CreatedAt == now && records[i].CreatedAt != 0 {
				rec.CreatedAt = records[i].CreatedAt
			}
			records[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		records = append(records, rec)
	}
	return s.saveLocked(records)
}

// SetStatus updates the status (and optionally the agent name) of the record
// with the given HITL request ID. Returns the updated record, or ok=false
// when no record references that request.
func (s *AutomationStore) SetStatus(hitlID, status, agentName string) (*AutomationRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return nil, false, err
	}
	for i := range records {
		if records[i].HITLID == hitlID {
			records[i].Status = status
			if agentName != "" {
				records[i].AgentName = agentName
			}
			records[i].UpdatedAt = time.Now().Unix()
			if err := s.saveLocked(records); err != nil {
				return nil, false, err
			}
			rec := records[i]
			return &rec, true, nil
		}
	}
	return nil, false, nil
}

// CountApproved returns how many automations are active (approved) — the
// input to the per-user MaxAutoCreatedAgents cap.
func (s *AutomationStore) CountApproved() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range records {
		if rec.Status == AutomationApproved {
			n++
		}
	}
	return n, nil
}

func (s *AutomationStore) loadLocked() ([]AutomationRecord, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("persona: read automations: %w", err)
	}
	var records []AutomationRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("persona: parse automations: %w", err)
	}
	return records, nil
}

func (s *AutomationStore) saveLocked(records []AutomationRecord) error {
	if records == nil {
		records = []AutomationRecord{}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("persona: marshal automations: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("persona: write automations: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("persona: rename automations: %w", err)
	}
	return nil
}

// PatternSignature fingerprints a recurring pattern by its significant
// keywords (sorted, capped) so re-mined clusters with a different most-recent
// representative still map to the same automation proposal.
func PatternSignature(representative string) string {
	set := keywordSet(representative)
	words := make([]string, 0, len(set))
	for w := range set {
		words = append(words, w)
	}
	slices.Sort(words)
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		return "pattern"
	}
	return strings.Join(words, "_")
}
