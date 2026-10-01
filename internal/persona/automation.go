package persona

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
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
	User      string `json:"user,omitempty"`
	Schedule  string `json:"schedule,omitempty"`
	Status    string `json:"status"` // pending | approved | rejected
	// HITLID is the approval request handling this proposal.
	HITLID      string `json:"hitl_id,omitempty"`
	TreeID      string `json:"tree_id,omitempty"`
	TreeVersion string `json:"tree_version,omitempty"`
	// AgentName and Schedule are reserved before publication.
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
	user string
}

// AutomationsPath is where the workspace keeps the proposal ledger.
func (w Workspace) AutomationsPath() string {
	return filepath.Join(w.Root, "automations.json")
}

// NewAutomationStore opens the ledger for a user workspace, creating the
// workspace directory if needed.
func NewAutomationStore(w Workspace) (*AutomationStore, error) {
	if err := util.EnsurePersistenceParent(w.AutomationsPath()); err != nil {
		return nil, fmt.Errorf("persona: automation store: %w", err)
	}
	return &AutomationStore{path: w.AutomationsPath(), user: w.User}, nil
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
	return s.update(func(records []AutomationRecord) ([]AutomationRecord, error) {
		if rec.User != "" && rec.User != s.user {
			return nil, fmt.Errorf("automation owner mismatch")
		}
		rec.User = s.user
		now := time.Now().Unix()
		rec.UpdatedAt = now
		if rec.CreatedAt == 0 {
			rec.CreatedAt = now
		}
		for i := range records {
			if records[i].Signature == rec.Signature {
				rec.CreatedAt = records[i].CreatedAt
				records[i] = rec
				return records, nil
			}
		}
		return append(records, rec), nil
	})
}

// Reserve admits one pending proposal under the same lock as the cap and
// dedup check. Pending and flagged records consume slots as well as active ones.
// It must precede publishing a tree or a scheduled definition.
func (s *AutomationStore) Reserve(rec AutomationRecord, limit int) error {
	if rec.User != "" && rec.User != s.user || rec.Signature == "" || rec.Status != AutomationPending || rec.TreeID == "" || rec.AgentName == "" || rec.Schedule == "" || strings.TrimSpace(rec.Representative) == "" || limit <= 0 {
		return fmt.Errorf("incomplete pending automation reservation")
	}
	return s.update(func(records []AutomationRecord) ([]AutomationRecord, error) {
		occupied := 0
		for _, existing := range records {
			if existing.Signature == rec.Signature || existing.TreeID == rec.TreeID || existing.AgentName == rec.AgentName {
				return nil, fmt.Errorf("automation proposal already reserved")
			}
			if existing.Status != AutomationRejected {
				occupied++
			}
		}
		if occupied >= limit {
			return nil, fmt.Errorf("automation cap reached (%d/%d reserved)", occupied, limit)
		}
		rec.User = s.user
		rec.CreatedAt = time.Now().Unix()
		rec.UpdatedAt = rec.CreatedAt
		return append(records, rec), nil
	})
}

// update serializes the complete read/modify/write across store instances and
// processes. A failed commit is returned even when fn prepared an agent file;
// the prior pending ledger remains the execution gate until an explicit retry.
func (s *AutomationStore) update(fn func([]AutomationRecord) ([]AutomationRecord, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := reliability.AcquireFileLockWithContext(ctx, s.path)
	if err != nil {
		return fmt.Errorf("lock automations: %w", err)
	}
	defer release()
	records, err := s.loadLocked()
	if err != nil {
		return err
	}
	records, err = fn(records)
	if err != nil {
		return err
	}
	return s.saveLocked(records)
}

// transition keeps matching, status validation, activation and the final
// ledger commit in one bounded transaction. The callback must not reenter it.
func (s *AutomationStore) transition(signature string, fn func(*AutomationRecord) error) error {
	return s.update(func(records []AutomationRecord) ([]AutomationRecord, error) {
		for i := range records {
			if records[i].Signature == signature {
				if err := fn(&records[i]); err != nil {
					return nil, err
				}
				records[i].User = s.user
				records[i].UpdatedAt = time.Now().Unix()
				return records, nil
			}
		}
		return nil, fmt.Errorf("automation proposal not found")
	})
}

// SetStatus updates the status (and optionally the agent name) of the record
// with the given HITL request ID. Returns the updated record, or ok=false
// when no record references that request.
func (s *AutomationStore) SetStatus(hitlID, status, agentName string) (*AutomationRecord, bool, error) {
	if hitlID == "" {
		return nil, false, fmt.Errorf("empty automation approval request ID")
	}
	var result *AutomationRecord
	err := s.update(func(records []AutomationRecord) ([]AutomationRecord, error) {
		for i := range records {
			if records[i].HITLID == hitlID {
				records[i].Status = status
				if agentName != "" {
					records[i].AgentName = agentName
				}
				records[i].User = s.user
				records[i].UpdatedAt = time.Now().Unix()
				rec := records[i]
				result = &rec
				break
			}
		}
		return records, nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, result != nil, nil
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
	data, err := util.ReadPersistenceFile(s.path)
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
	seen := map[string]bool{}
	for _, rec := range records {
		if rec.Signature == "" || seen[rec.Signature] || rec.User != "" && rec.User != s.user {
			return nil, fmt.Errorf("automation ledger identity mismatch")
		}
		seen[rec.Signature] = true
	}
	return records, nil
}

func (s *AutomationStore) saveLocked(records []AutomationRecord) error {
	if records == nil {
		records = []AutomationRecord{}
	}
	return util.SaveJSONAtomic(s.path, records)
}

// PatternSignature fingerprints a recurring pattern by its significant
// keywords (sorted, capped) so re-mined clusters with a different most-recent
// representative still map to the same automation proposal.
func PatternSignature(representative string) string {
	set := keywordSet(representative)
	words := slices.Sorted(maps.Keys(set))
	if len(words) > 5 {
		words = words[:5]
	}
	if len(words) == 0 {
		return "pattern"
	}
	return strings.Join(words, "_")
}
