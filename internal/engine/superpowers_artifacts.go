package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

const superpowersRepoDir = "/home/nico/go-bt-evolve"

// superpowersRunsDir is a var (not const) so tests can scope run-artifact
// reads and writes away from the operator's live runs directory: TestMain
// redirects it for the whole engine test binary, and
// isolateSuperpowersRunsDir(t) gives a test a private, deterministic dir.
var superpowersRunsDir = "/home/nico/go-bt-evolve/docs/superpowers/runs"

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
	next := *run
	next.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(&next, "", "  ")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = reliability.UpdateSharedJSONWithContext(ctx, filepath.Join(run.ArtifactDir, "run.json"), func(current []byte) (any, error) {
		if string(current) != run.artifactSnapshot {
			return nil, fmt.Errorf("run journal changed concurrently; reload before updating")
		}
		return &next, nil
	})
	if err == nil {
		run.UpdatedAt, run.artifactSnapshot = next.UpdatedAt, string(data)
	}
	return err
}

func readSuperpowersRunJSON(path string) (*SuperpowersRun, error) {
	data, err := util.ReadPersistenceFile(path)
	if err != nil {
		return nil, err
	}
	var run SuperpowersRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	run.artifactSnapshot = string(data)
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
		if run.User != bb.User {
			return nil, fmt.Errorf("superpowers run owner mismatch")
		}
		// A run that reached finish is consumed: it applied and its worktree
		// was cleaned. Reusing it sends the next implementation batch into a
		// deleted directory (12:44:56 on 2026-07-10: a preflight-resumed run
		// finished, then Phase 5 re-entered the runtime with the same run and
		// died on "chdir …: no such file or directory"). Start fresh instead;
		// finish/report actions read the old run via getSuperpowersRun.
		if run.Phase != SuperpowersPhaseFinish {
			return run, nil
		}
	}
	if bb == nil {
		return nil, fmt.Errorf("nil blackboard")
	}
	now := time.Now()
	id := newSuperpowersRunID(bb.Task, now)
	run := &SuperpowersRun{
		ID:          id,
		User:        bb.User,
		Task:        bb.Task,
		Mode:        superpowersModeFromTask(bb.Task),
		Phase:       SuperpowersPhaseDesign,
		RepoDir:     superpowersRepoDir,
		ArtifactDir: filepath.Join(superpowersRunsDir, id),
		StartedAt:   now,
		UpdatedAt:   now,
	}
	refs, err := captureProgramMilestones(bb)
	if err != nil {
		return nil, err
	}
	run.ProgramMilestones = refs
	setSuperpowersRun(bb, run)
	return run, nil
}
