package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SLOSnapshot is the serializable, cross-process form of SLOMetrics. The agent
// process saves snapshots after task execution; the gardener process loads them
// as deployment evidence for the validation gate (remediation task B1).
type SLOSnapshot struct {
	AgentName       string    `json:"agent_name"`
	TreeName        string    `json:"tree_name"`
	TotalCalls      int64     `json:"total_calls"`
	SuccessfulCalls int64     `json:"successful_calls"`
	FailedCalls     int64     `json:"failed_calls"`
	RecoveredCalls  int64     `json:"recovered_calls"`
	TotalLatencyMs  int64     `json:"total_latency_ms"`
	MaxLatencyMs    int64     `json:"max_latency_ms"`
	SavedAt         time.Time `json:"saved_at"`
}

// SuccessRate mirrors SLOMetrics.SuccessRate semantics.
func (s SLOSnapshot) SuccessRate() float64 {
	if s.TotalCalls == 0 {
		return 1.0
	}
	return float64(s.SuccessfulCalls) / float64(s.TotalCalls)
}

// RecoveryRate mirrors SLOMetrics.RecoveryRate semantics.
func (s SLOSnapshot) RecoveryRate() float64 {
	if s.FailedCalls == 0 {
		return 0
	}
	return float64(s.RecoveredCalls) / float64(s.FailedCalls)
}

// Snapshot returns a copy of the metrics safe for serialization.
func (m *SLOMetrics) Snapshot() SLOSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return SLOSnapshot{
		AgentName:       m.AgentName,
		TreeName:        m.TreeName,
		TotalCalls:      m.TotalCalls,
		SuccessfulCalls: m.SuccessfulCalls,
		FailedCalls:     m.FailedCalls,
		RecoveredCalls:  m.RecoveredCalls,
		TotalLatencyMs:  m.TotalLatencyMs,
		MaxLatencyMs:    m.MaxLatencyMs,
		SavedAt:         time.Now(),
	}
}

// SaveSLOMetrics writes all registered SLO metrics to path as a JSON array,
// atomically (tmp + rename per ADR-003). The parent directory is created if
// missing.
func SaveSLOMetrics(path string) error {
	var snapshots []SLOSnapshot
	sloRegistry.Range(func(_, value any) bool {
		snapshots = append(snapshots, value.(*SLOMetrics).Snapshot())
		return true
	})

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create SLO dir: %w", err)
	}
	data, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal SLO snapshots: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write SLO tmp file: %w", err)
	}
	return os.Rename(tmp, path)
}

// LoadSLOEvidence reads SLO snapshots from path without touching the in-memory
// registry, so a reader process (gardener) does not fabricate metrics for
// trees it never executed.
func LoadSLOEvidence(path string) ([]SLOSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SLO evidence: %w", err)
	}
	var snapshots []SLOSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return nil, fmt.Errorf("parse SLO evidence: %w", err)
	}
	return snapshots, nil
}
