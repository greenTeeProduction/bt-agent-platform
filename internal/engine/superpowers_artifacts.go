package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const superpowersRepoDir = "/home/nico/go-bt-evolve"
const superpowersRunsDir = "/home/nico/go-bt-evolve/docs/superpowers/runs"

func newSuperpowersRunID(task string, now time.Time) string {
	return fmt.Sprintf("%s-%s", now.Format("20060102T150405"), superpowersTaskHashSuffix(task))
}

func superpowersTaskHashSuffix(task string) string {
	// Include the date so the same scheduled task gets a different hash each day,
	// allowing fresh Superpowers implementation attempts on every tick without
	// the saturation guard blocking legitimate recurring research-to-implementation
	// cycles.
	h := sha1.Sum([]byte(task + time.Now().Format("2006-01-02")))
	return hex.EncodeToString(h[:])[:8]
}

func superpowersPlanAttemptSaturatedInDir(baseDir, task string, maxAttempts int) (bool, []string) {
	if maxAttempts <= 0 {
		return false, nil
	}
	suffix := "-" + superpowersTaskHashSuffix(task)
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return false, nil
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			matches = append(matches, filepath.Join(baseDir, entry.Name()))
		}
	}
	return len(matches) >= maxAttempts, matches
}

func superpowersPlanAttemptSaturated(task string, maxAttempts int) (bool, []string) {
	return superpowersPlanAttemptSaturatedInDir(superpowersRunsDir, task, maxAttempts)
}

func ensureSuperpowersRunDirs(run *SuperpowersRun) error {
	if run == nil {
		return fmt.Errorf("nil superpowers run")
	}
	for _, dir := range []string{run.ArtifactDir, filepath.Join(run.ArtifactDir, "tasks"), filepath.Join(run.ArtifactDir, "verification")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeSuperpowersRunJSON(run *SuperpowersRun) error {
	if err := ensureSuperpowersRunDirs(run); err != nil {
		return err
	}
	run.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(run.ArtifactDir, "run.json"), data, 0o644)
}

func readSuperpowersRunJSON(path string) (*SuperpowersRun, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var run SuperpowersRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func writeArtifactOnce(path string, content []byte) (bool, error) {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, content, 0o644)
}

func safeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = strings.Trim(re.ReplaceAllString(s, "-"), "-")
	if s == "" {
		return "task"
	}
	if len(s) > 72 {
		s = strings.Trim(s[:72], "-")
	}
	return s
}

func currentSuperpowersRun(bb *Blackboard) (*SuperpowersRun, error) {
	if run, ok := getSuperpowersRun(bb); ok {
		return run, nil
	}
	if bb == nil {
		return nil, fmt.Errorf("nil blackboard")
	}
	now := time.Now()
	id := newSuperpowersRunID(bb.Task, now)
	run := &SuperpowersRun{
		ID:          id,
		Task:        bb.Task,
		Mode:        superpowersModeFromTask(bb.Task),
		Phase:       SuperpowersPhaseDesign,
		RepoDir:     superpowersRepoDir,
		ArtifactDir: filepath.Join(superpowersRunsDir, id),
		StartedAt:   now,
		UpdatedAt:   now,
	}
	setSuperpowersRun(bb, run)
	return run, nil
}
