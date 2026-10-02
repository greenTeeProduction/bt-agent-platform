package research

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// SourceEvidence identifies the actual answer from which a goal was extracted.
// Its digest covers the complete answer; Excerpt is only a bounded preview.
type SourceEvidence struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Excerpt    string    `json:"excerpt"`
	ObservedAt time.Time `json:"observed_at"`
}

type DeliveryCheck struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	OutputHash string `json:"output_hash"`
}

// Delivery is code-delivery evidence, not proof of runtime adoption or impact.
// The engine creates it only after checking the actual Git object and ancestry.
type Delivery struct {
	RunID      string          `json:"run_id"`
	TaskIndex  int             `json:"task_index"`
	Commit     string          `json:"commit"`
	Tree       string          `json:"tree"`
	Repository string          `json:"repository"`
	Files      []string        `json:"files"`
	Checks     []DeliveryCheck `json:"checks"`
	SourceIDs  []string        `json:"source_ids,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	RecordedAt time.Time       `json:"recorded_at"`
}

type GoalTrace struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Sources      []SourceEvidence `json:"sources,omitempty"`
	Deliveries   []Delivery       `json:"deliveries,omitempty"`
	ReviewReason string           `json:"review_reason,omitempty"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// TraceStore is separate from the legacy dedup index: observing a finding or
// writing a goap:implemented label cannot become delivery evidence.
type TraceStore struct {
	SchemaVersion int                   `json:"schema_version"`
	User          string                `json:"user"`
	Goals         map[string]*GoalTrace `json:"goals"`
}

func TracePath(knowledgePath, user string) string {
	return fmt.Sprintf("%s.trace-%x.json", knowledgePath, sha256.Sum256([]byte(user)))
}

func decodeTraces(data []byte, user string) (*TraceStore, error) {
	s := &TraceStore{}
	if data != nil {
		if err := json.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("research trace is corrupt: %w", err)
		}
	} else {
		s.SchemaVersion, s.User, s.Goals = 1, user, map[string]*GoalTrace{}
	}
	if s.SchemaVersion != 1 || s.Goals == nil {
		return nil, fmt.Errorf("invalid research trace schema")
	}
	if s.User != user {
		return nil, fmt.Errorf("research trace owner mismatch")
	}
	if s.Goals == nil {
		s.Goals = map[string]*GoalTrace{}
	}
	for id, goal := range s.Goals {
		if goal == nil || goal.ID != id || len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" || strings.TrimSpace(goal.Title) == "" {
			return nil, fmt.Errorf("invalid research goal %q", id)
		}
		for _, source := range goal.Sources {
			if len(source.ID) != 64 || strings.Trim(source.ID, "0123456789abcdef") != "" || source.Source == "" || source.ObservedAt.IsZero() {
				return nil, fmt.Errorf("invalid research source")
			}
		}
		for _, d := range goal.Deliveries {
			if err := d.Validate(); err != nil {
				return nil, err
			}
			for _, sourceID := range d.SourceIDs {
				if !slices.ContainsFunc(goal.Sources, func(source SourceEvidence) bool {
					return source.ID == sourceID && !source.ObservedAt.After(d.StartedAt)
				}) {
					return nil, fmt.Errorf("delivery links an unavailable or later source")
				}
			}
		}
	}
	return s, nil
}

func OpenTraces(path, user string) (*TraceStore, error) {
	data, err := util.ReadPersistenceFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return decodeTraces(data, user)
}

// UpdateTraces holds a bounded sidecar lock across the complete transaction.
func UpdateTraces(ctx context.Context, path, user string, update func(*TraceStore) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return reliability.UpdateSharedJSONWithContext(ctx, path, func(data []byte) (any, error) {
		s, err := decodeTraces(data, user)
		if err != nil {
			return nil, err
		}
		if err := update(s); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(s)
		if err != nil {
			return nil, err
		}
		if _, err := decodeTraces(encoded, user); err != nil {
			return nil, err
		}
		return s, nil
	})
}

func (s *TraceStore) Goal(id, title string) *GoalTrace {
	g := s.Goals[id]
	if g == nil {
		g = &GoalTrace{ID: id, Title: truncateRunes(title, excerptLimit)}
		s.Goals[id] = g
	}
	g.UpdatedAt = time.Now().UTC()
	return g
}

func (s *TraceStore) Observe(id, title, source, answer string) {
	g := s.Goal(id, title)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(answer)))
	if !slices.ContainsFunc(g.Sources, func(e SourceEvidence) bool { return e.ID == hash && e.Source == source }) {
		g.Sources = append(g.Sources, SourceEvidence{ID: hash, Source: source, Excerpt: truncateRunes(answer, excerptLimit), ObservedAt: time.Now().UTC()})
	}
}

func (d Delivery) Validate() error {
	if d.RunID == "" || d.Repository == "" || !gitObjectID(d.Commit) || !gitObjectID(d.Tree) || len(d.Files) == 0 || len(d.Checks) == 0 || d.RecordedAt.IsZero() || d.StartedAt.IsZero() || d.RecordedAt.Before(d.StartedAt) {
		return fmt.Errorf("incomplete research delivery evidence")
	}
	for _, check := range d.Checks {
		if check.Name == "" || check.Command == "" || len(check.OutputHash) != 64 || strings.Trim(check.OutputHash, "0123456789abcdef") != "" {
			return fmt.Errorf("incomplete delivery verification check")
		}
	}
	for _, file := range d.Files {
		if !filepath.IsLocal(file) || file == "." || strings.ContainsAny(file, "\x00\\") {
			return fmt.Errorf("invalid delivery file")
		}
	}
	return nil
}

func gitObjectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

func (s *TraceStore) RecordDelivery(id, title string, d Delivery) error {
	if err := d.Validate(); err != nil {
		return err
	}
	g := s.Goal(id, title)
	// Bind only sources known before this run began. Later research cannot
	// retroactively earn credit for an already-delivered change.
	d.SourceIDs = nil
	for _, source := range g.Sources {
		if !source.ObservedAt.IsZero() && !source.ObservedAt.After(d.StartedAt) {
			d.SourceIDs = append(d.SourceIDs, source.ID)
		}
	}
	slices.Sort(d.SourceIDs)
	d.SourceIDs = slices.Compact(d.SourceIDs)
	for _, old := range g.Deliveries {
		if old.RunID == d.RunID && old.TaskIndex == d.TaskIndex {
			// Replay may have a new observation time; all evidence must agree.
			d.RecordedAt = old.RecordedAt
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(d)
			if string(a) != string(b) {
				return fmt.Errorf("conflicting delivery for run %s task %d", d.RunID, d.TaskIndex)
			}
			return nil
		}
	}
	g.Deliveries = append(g.Deliveries, d)
	g.ReviewReason = ""
	return nil
}

func (s *TraceStore) Delivered(id string) bool {
	g := s.Goals[id]
	return g != nil && len(g.Deliveries) > 0
}

func (s *TraceStore) NeedsReview(id string) bool {
	g := s.Goals[id]
	return g != nil && g.ReviewReason != "" && len(g.Deliveries) == 0
}

// Summary deliberately leaves adoption and measured impact unknown. A commit
// cannot establish either without a version-bound runtime observation.
func (s *TraceStore) Summary() map[string]any {
	observed, delivered, linked, review := 0, 0, 0, 0
	for id, g := range s.Goals {
		if len(g.Sources) > 0 {
			observed++
		}
		if s.Delivered(id) {
			delivered++
			if slices.ContainsFunc(g.Deliveries, func(d Delivery) bool { return len(d.SourceIDs) > 0 }) {
				linked++
			}
		}
		if s.NeedsReview(id) {
			review++
		}
	}
	return map[string]any{"user": s.User, "observed_goals": observed, "delivered_goals": delivered,
		"source_linked_deliveries": linked, "goals_needing_review": review,
		"runtime_adoption": "not_linked", "measured_impact": "not_linked", "goals": s.Goals}
}
