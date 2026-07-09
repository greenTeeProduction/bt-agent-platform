package engine

import (
	"strings"
	"time"
)

type SuperpowersPhase string

const (
	SuperpowersPhaseDesign         SuperpowersPhase = "design"
	SuperpowersPhasePlan           SuperpowersPhase = "plan"
	SuperpowersPhaseHITL           SuperpowersPhase = "hitl"
	SuperpowersPhaseImplementation SuperpowersPhase = "implementation"
	SuperpowersPhaseVerification   SuperpowersPhase = "verification"
	SuperpowersPhaseFinish         SuperpowersPhase = "finish"
)

type SuperpowersMode string

const (
	SuperpowersModeDryRun SuperpowersMode = "dry_run"
	SuperpowersModeApply  SuperpowersMode = "apply"
)

type SuperpowersRun struct {
	ID             string              `json:"id"`
	Task           string              `json:"task"`
	Mode           SuperpowersMode     `json:"mode"`
	Phase          SuperpowersPhase    `json:"phase"`
	RepoDir        string              `json:"repo_dir"`
	WorktreePath   string              `json:"worktree_path"`
	WorktreeBranch string              `json:"worktree_branch"`
	ArtifactDir    string              `json:"artifact_dir"`
	DesignPath     string              `json:"design_path"`
	PlanPath       string              `json:"plan_path"`
	Tasks          []SuperpowersTask   `json:"tasks"`
	Verification   []VerificationCheck `json:"verification"`
	ChangedFiles   []string            `json:"changed_files"`
	ApplyStatus    string              `json:"apply_status,omitempty"`
	// PartialFailure records a failed task that was carried forward while the
	// run's completed tasks landed (partial-landing mode).
	PartialFailure string `json:"partial_failure,omitempty"`
	// Arc42Sync records what the documentation sync stage did for this run.
	Arc42Sync string `json:"arc42_sync,omitempty"`
	// DocDriftSync records what the doc-drift repair stage did for this run.
	DocDriftSync  string    `json:"doc_drift_sync,omitempty"`
	PatchPath     string    `json:"patch_path,omitempty"`
	AppliedCommit string    `json:"applied_commit,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SuperpowersTask struct {
	Index       int      `json:"index"`
	Title       string   `json:"title"`
	Objective   string   `json:"objective"`
	Files       []string `json:"files"`
	Tests       []string `json:"tests"`
	Risk        string   `json:"risk"`
	Body        string   `json:"body"`
	ArtifactDir string   `json:"artifact_dir"`
	Status      string   `json:"status"`
}

type VerificationCheck struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Passed   bool   `json:"passed"`
	Output   string `json:"output"`
	Duration string `json:"duration"`
}

const chainKeySuperpowersRun = "superpowers_run"

func setSuperpowersRun(bb *Blackboard, run *SuperpowersRun) {
	if bb == nil {
		return
	}
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState[chainKeySuperpowersRun] = run
}

func getSuperpowersRun(bb *Blackboard) (*SuperpowersRun, bool) {
	if bb == nil || bb.ChainState == nil {
		return nil, false
	}
	run, ok := bb.ChainState[chainKeySuperpowersRun].(*SuperpowersRun)
	return run, ok && run != nil
}

func superpowersModeFromTask(task string) SuperpowersMode {
	if stringsContainsFold(task, "dry_run") || stringsContainsFold(task, "dry-run") || stringsContainsFold(task, "dry run") {
		return SuperpowersModeDryRun
	}
	return SuperpowersModeApply
}

func stringsContainsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
