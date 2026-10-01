package evolution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// TaskTrial retains actual output and the independent contract verdict. Trials
// qualify only the stated task corpus, not general assistant performance.
type TaskTrial struct {
	Task             string         `json:"task"`
	Contract         ResultContract `json:"contract"`
	BeforeOutput     string         `json:"before_output"`
	AfterOutput      string         `json:"after_output"`
	BeforeOutcome    string         `json:"before_outcome"`
	AfterOutcome     string         `json:"after_outcome"`
	BeforeDurationMs int64          `json:"before_duration_ms"`
	AfterDurationMs  int64          `json:"after_duration_ms"`
}

type RuntimeQualification struct {
	TreeID           string      `json:"tree_id"`
	User             string      `json:"user"`
	BaselineVersion  string      `json:"baseline_version"`
	CandidateVersion string      `json:"candidate_version"`
	Backend          string      `json:"backend"`
	Model            string      `json:"model"`
	BaselineCalls    int         `json:"baseline_calls"`
	CandidateCalls   int         `json:"candidate_calls"`
	MeasuredAt       time.Time   `json:"measured_at"`
	Trials           []TaskTrial `json:"trials"`
}

func (q *RuntimeQualification) PassCounts() (int, int) {
	before, after := 0, 0
	for _, trial := range q.Trials {
		if trial.BeforeOutcome == "success" && trial.Contract.Verify(trial.BeforeOutput) == nil {
			before++
		}
		if trial.AfterOutcome == "success" && trial.Contract.Verify(trial.AfterOutput) == nil {
			after++
		}
	}
	return before, after
}

// Validate recomputes verdicts from retained outputs; caller-provided scalar
// scores and "passed" flags cannot authorize publication.
func (q *RuntimeQualification) Validate(base, candidate *SerializableNode) error {
	return q.validate(base, candidate, true)
}

func (q *RuntimeQualification) validate(base, candidate *SerializableNode, fresh bool) error {
	if q == nil || q.TreeID == "" || len(q.Trials) < 3 || q.BaselineCalls < len(q.Trials) || q.CandidateCalls < len(q.Trials) {
		return fmt.Errorf("promotion requires at least three paired real-model trials")
	}
	if q.Backend != "ollama" && q.Backend != "sol" || q.Model == "" || q.Backend == "sol" && q.Model != "gpt-6.1-sol" {
		return fmt.Errorf("unqualified benchmark provider")
	}
	if age := time.Since(q.MeasuredAt); fresh && (age < -time.Minute || age > 30*time.Minute) {
		return fmt.Errorf("qualification is stale or future-dated")
	}
	bv, err := TreeVersion(base)
	if err != nil {
		return err
	}
	cv, err := TreeVersion(candidate)
	if err != nil {
		return err
	}
	if bv != q.BaselineVersion || cv != q.CandidateVersion || bv == cv || !PreservesGovernance(base, candidate) {
		return fmt.Errorf("qualification does not match preserved candidate definitions")
	}
	baseOwner, _ := base.Metadata["user"].(string)
	candidateOwner, _ := candidate.Metadata["user"].(string)
	if baseOwner != candidateOwner {
		return fmt.Errorf("candidate changed definition ownership")
	}
	if kind, _ := base.Metadata["factory_kind"].(string); kind == "response" {
		baseTask, _ := base.Metadata["task"].(string)
		candidateTask, _ := candidate.Metadata["task"].(string)
		candidateKind, _ := candidate.Metadata["factory_kind"].(string)
		if baseTask == "" || baseTask != candidateTask || candidateKind != kind {
			return fmt.Errorf("candidate changed the requested factory task")
		}
	}
	for _, tree := range []*SerializableNode{base, candidate} {
		if owner, _ := tree.Metadata["user"].(string); owner != "" && owner != q.User {
			return fmt.Errorf("definition owner mismatch")
		}
	}
	for _, trial := range q.Trials {
		if strings.TrimSpace(trial.Task) == "" || len(trial.Contract.JSONFields) == 0 || trial.BeforeDurationMs < 0 || trial.AfterDurationMs < 0 {
			return fmt.Errorf("trial lacks an explicit task/value contract")
		}
		if trial.BeforeOutcome == "success" && trial.Contract.Verify(trial.BeforeOutput) == nil && (trial.AfterOutcome != "success" || trial.Contract.Verify(trial.AfterOutput) != nil) {
			return fmt.Errorf("candidate regressed a passing task")
		}
	}
	before, after := q.PassCounts()
	if after <= before || after != len(q.Trials) {
		return fmt.Errorf("candidate must improve task passes and satisfy every declared trial")
	}
	return nil
}

// RuntimeRelease is the atomic active-version pointer. Definitions and evidence
// are content addressed and never replaced. Ancestors supports repeated rollback.
type RuntimeRelease struct {
	TreeID        string    `json:"tree_id"`
	User          string    `json:"user"`
	Version       string    `json:"version"`
	Ancestors     []string  `json:"ancestors,omitempty"`
	Qualification string    `json:"qualification,omitempty"`
	Generation    uint64    `json:"generation"`
	Action        string    `json:"action"`
	Reason        string    `json:"reason,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RuntimeReleaseStore struct{ root string }

func NewRuntimeReleaseStore(root string) *RuntimeReleaseStore {
	return &RuntimeReleaseStore{root: root}
}

// RecordAttempt retains rejected real trials without changing runtime admission.
func (s *RuntimeReleaseStore) RecordAttempt(q *RuntimeQualification, failure error) error {
	if q == nil || failure == nil {
		return nil
	}
	dir, err := s.directory(q.TreeID, q.User)
	if err != nil {
		return err
	}
	attempt := struct {
		Qualification *RuntimeQualification `json:"qualification"`
		Rejection     string                `json:"rejection"`
	}{q, failure.Error()}
	data, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	return util.SaveJSONAtomic(filepath.Join(dir, "attempts", fmt.Sprintf("%x.json", sha256.Sum256(data))), attempt)
}

func (s *RuntimeReleaseStore) directory(id, user string) (string, error) {
	if strings.TrimSpace(s.root) == "" || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("runtime release root and tree ID are required")
	}
	key := sha256.Sum256([]byte(user + "\x00" + id))
	return filepath.Join(s.root, fmt.Sprintf("%x", key)), nil
}

func versionFile(dir, version string) (string, error) {
	value := strings.TrimPrefix(version, "sha256:")
	if len(value) != 64 || !strings.HasPrefix(version, "sha256:") {
		return "", fmt.Errorf("invalid definition version")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", err
	}
	return filepath.Join(dir, "versions", value+".json"), nil
}

func readRelease(dir, id, user string) (*RuntimeRelease, error) {
	data, err := util.ReadPersistenceFile(filepath.Join(dir, "active.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var release RuntimeRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, err
	}
	if release.TreeID != id || release.User != user || release.Generation == 0 {
		return nil, fmt.Errorf("runtime release identity mismatch")
	}
	if release.Action != "promote" && release.Action != "rollback" {
		return nil, fmt.Errorf("invalid runtime release action")
	}
	return &release, nil
}

func readVersion(dir, version string) (*SerializableNode, error) {
	path, err := versionFile(dir, version)
	if err != nil {
		return nil, err
	}
	data, err := util.ReadPersistenceFile(path)
	if err != nil {
		return nil, err
	}
	var tree SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	actual, err := TreeVersion(&tree)
	if err != nil || actual != version {
		return nil, fmt.Errorf("runtime definition integrity check failed")
	}
	return &tree, nil
}

func saveVersion(dir string, tree *SerializableNode) error {
	version, err := TreeVersion(tree)
	if err != nil {
		return err
	}
	path, err := versionFile(dir, version)
	if err != nil {
		return err
	}
	if _, err := readVersion(dir, version); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return util.SaveJSONAtomic(path, tree)
}

func (s *RuntimeReleaseStore) Resolve(id, user string) (*SerializableNode, *RuntimeRelease, error) {
	dir, err := s.directory(id, user)
	if err != nil {
		return nil, nil, err
	}
	release, err := readRelease(dir, id, user)
	if err != nil || release == nil {
		return nil, release, err
	}
	tree, err := readVersion(dir, release.Version)
	if err == nil {
		if owner, _ := tree.Metadata["user"].(string); owner != "" && owner != user {
			err = fmt.Errorf("runtime definition owner mismatch")
		}
	}
	if err == nil && release.Action == "promote" {
		if len(release.Qualification) != 64 {
			return nil, release, fmt.Errorf("qualification reference missing")
		}
		if _, err := hex.DecodeString(release.Qualification); err != nil {
			return nil, release, err
		}
		data, readErr := util.ReadPersistenceFile(filepath.Join(dir, "qualifications", release.Qualification+".json"))
		if readErr != nil {
			return nil, release, readErr
		}
		var q RuntimeQualification
		if err := json.Unmarshal(data, &q); err != nil {
			return nil, release, err
		}
		canonical, marshalErr := json.Marshal(&q)
		if marshalErr != nil || fmt.Sprintf("%x", sha256.Sum256(canonical)) != release.Qualification || q.TreeID != id || q.User != user || q.CandidateVersion != release.Version {
			return nil, release, fmt.Errorf("qualification integrity check failed")
		}
		base, baseErr := readVersion(dir, q.BaselineVersion)
		if baseErr != nil {
			return nil, release, baseErr
		}
		if err := q.validate(base, tree, false); err != nil {
			return nil, release, err
		}
	}
	return tree, release, err
}

// Promote commits only if the currently active definition is the measured
// predecessor. The first promotion retains the inspected legacy predecessor.
func (s *RuntimeReleaseStore) Promote(ctx context.Context, base, candidate *SerializableNode, q *RuntimeQualification) (*RuntimeRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := q.Validate(base, candidate); err != nil {
		return nil, err
	}
	dir, err := s.directory(q.TreeID, q.User)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "active.json")
	if err := util.EnsurePersistenceParent(path); err != nil {
		return nil, err
	}
	unlock, err := reliability.AcquireFileLockWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := readRelease(dir, q.TreeID, q.User)
	if err != nil {
		return nil, err
	}
	release := &RuntimeRelease{TreeID: q.TreeID, User: q.User, Generation: 1, Action: "promote", UpdatedAt: time.Now().UTC()}
	if current != nil {
		if current.Version != q.BaselineVersion {
			return nil, fmt.Errorf("active version changed since measurement")
		}
		if _, err := readVersion(dir, current.Version); err != nil {
			return nil, err
		}
		release.Generation = current.Generation + 1
		release.Ancestors = append(release.Ancestors, current.Ancestors...)
	}
	release.Ancestors = append(release.Ancestors, q.BaselineVersion)
	if len(release.Ancestors) > 128 {
		release.Ancestors = release.Ancestors[len(release.Ancestors)-128:]
	}
	for _, tree := range []*SerializableNode{base, candidate} {
		if err := saveVersion(dir, tree); err != nil {
			return nil, err
		}
	}
	evidence, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(evidence)
	release.Qualification = fmt.Sprintf("%x", digest)
	if err := util.SaveJSONAtomic(filepath.Join(dir, "qualifications", release.Qualification+".json"), q); err != nil {
		return nil, err
	}
	release.Version = q.CandidateVersion
	if err := commitRelease(dir, release); err != nil {
		return nil, err
	}
	return release, nil
}

func commitRelease(dir string, release *RuntimeRelease) error {
	// An event written before a failed pointer swap is merely a prepared
	// event. Only active.json (generation + version) proves adoption.
	data, err := json.Marshal(release)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	if err := util.SaveJSONAtomic(filepath.Join(dir, "events", fmt.Sprintf("%x.json", digest)), release); err != nil {
		return err
	}
	return util.SaveJSONAtomic(filepath.Join(dir, "active.json"), release)
}

func (s *RuntimeReleaseStore) Rollback(ctx context.Context, id, user, expectedVersion, reason string) (*RuntimeRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("rollback reason required")
	}
	dir, err := s.directory(id, user)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "active.json")
	unlock, err := reliability.AcquireFileLockWithContext(ctx, path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := readRelease(dir, id, user)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Version != expectedVersion || len(current.Ancestors) == 0 {
		return nil, fmt.Errorf("rollback predecessor unavailable or active version changed")
	}
	previous := current.Ancestors[len(current.Ancestors)-1]
	if _, err := readVersion(dir, previous); err != nil {
		return nil, err
	}
	current.Version = previous
	current.Ancestors = current.Ancestors[:len(current.Ancestors)-1]
	current.Generation++
	current.Action, current.Reason, current.UpdatedAt = "rollback", reason, time.Now().UTC()
	current.Qualification = ""
	if err := commitRelease(dir, current); err != nil {
		return nil, err
	}
	return current, nil
}
