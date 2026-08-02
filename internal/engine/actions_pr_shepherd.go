package engine

// PR pipeline shepherd (spec: docs/superpowers/specs/2026-07-22-pr-pipeline-
// shepherd-design.md). origin/master is branch-protected (PR #13), so fleet
// landings accrue on LOCAL master. The ShepherdFleetPR action — inserted
// after SetupFusionTools in both goap fusion trees — ships that work in one
// NON-BLOCKING pass per cycle: ff local master when a merge landed upstream;
// push the fleet branch + open a PR when local master is ahead; skip while CI
// runs; run ONE bounded Claude fix attempt when CI is red; merge when CI is
// green. It never waits on CI (the 90-minute run budget killed cycles before,
// dfffeb1) — CI progresses between tree touches, so a green PR merges at the
// next touch. Per the SelfReviewTree lesson the action returns SUCCESS on
// every path: an early failure would bubble into the ClaudeErrorHandler
// wrapper and trigger spurious recovery for routine steady states.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// prShepherdMu serializes shepherd passes: both goap fusion agents run in the
// one daemon process and their cycles can overlap; two concurrent passes
// could double-create PRs or race the branch push.
var prShepherdMu sync.Mutex

func prShepherdEnabled() bool {
	return getenvDefault("BT_PR_SHEPHERD", "on") != "off"
}

func fleetPRBranch() string {
	return getenvDefault("BT_FLEET_PR_BRANCH", "fleet/landing")
}

// prShepherdTokenVars is the lookup order for the GitHub API token. The
// daemon inherits its environment from the unit's EnvironmentFile.
var prShepherdTokenVars = []string{"BT_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"}

// prShepherdGitCredentialsPath locates the git credential store consulted as
// the token fallback; a var so tests can redirect it away from the real file.
var prShepherdGitCredentialsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".git-credentials")
}

func prShepherdToken() string {
	for _, k := range prShepherdTokenVars {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	// Fallback: credential.helper=store already authenticates every git push
	// from this host, and a github.com PAT from that store works for the API
	// too — zero-config, no secret duplicated into an env file.
	return githubTokenFromCredentialStore(prShepherdGitCredentialsPath())
}

// githubTokenFromCredentialStore parses the first github.com entry of a
// git-credentials file (https://<user>:<token>@github.com) and returns the
// token. Tokens are stored verbatim (PATs contain no URL-reserved characters),
// so no percent-decoding is attempted.
func githubTokenFromCredentialStore(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed user-owned credential-store path, no user traversal
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "github.com") || !strings.HasPrefix(line, "https://") {
			continue
		}
		rest := strings.TrimPrefix(line, "https://")
		at := strings.LastIndex(rest, "@")
		if at < 0 {
			continue
		}
		if c := strings.Index(rest[:at], ":"); c >= 0 {
			if tok := rest[c+1 : at]; tok != "" {
				return tok
			}
		}
	}
	return ""
}

func prShepherdMaxFixAttempts() int {
	if n, err := strconv.Atoi(getenvDefault("BT_PR_SHEPHERD_MAX_FIX_ATTEMPTS", "3")); err == nil && n > 0 {
		return n
	}
	return 3
}

// ---------------------------------------------------------------------------
// Durable state

type prShepherdState struct {
	PRNumber int `json:"pr_number,omitempty"`
	// FixAttempts counts Claude fix attempts per PR-head SHA so one broken
	// head cannot burn Claude forever. TotalFixAttempts additionally bounds
	// an evolving-SHA ping-pong (each fix changes the head); both reset when
	// a merge or upstream sync succeeds.
	FixAttempts      map[string]int `json:"fix_attempts,omitempty"`
	TotalFixAttempts int            `json:"total_fix_attempts,omitempty"`
	// LastOutcome/LastPassAt are the durable observability record: the
	// action's blackboard outcome is overwritten by later tree nodes and the
	// cycle-complete log line only carries the FINAL outcome, so without
	// these every shepherd pass is invisible (cost 70 blind minutes on
	// 2026-07-22 before this field existed).
	LastOutcome string `json:"last_outcome,omitempty"`
	LastPassAt  string `json:"last_pass_at,omitempty"`
}

// prShepherdDirOverride redirects durable state in tests (mirrors
// selfReviewDirOverride). Empty in production.
var prShepherdDirOverride string

func prShepherdDir() string {
	if prShepherdDirOverride != "" {
		return prShepherdDirOverride
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "pr_shepherd")
}

func prShepherdStatePath(dir string) string { return filepath.Join(dir, "state.json") }

func loadPRShepherdState(dir string) prShepherdState {
	var s prShepherdState
	readErrorHandlerJSON(prShepherdStatePath(dir), &s)
	if s.FixAttempts == nil {
		s.FixAttempts = map[string]int{}
	}
	return s
}

func savePRShepherdState(dir string, s prShepherdState) error {
	return writeErrorHandlerJSON(prShepherdStatePath(dir), s)
}

// ---------------------------------------------------------------------------
// Minimal GitHub API client (net/http only — no gh CLI on this host)

type githubPRClient struct {
	base  string // https://api.github.com, or a test server
	owner string
	repo  string
	token string
	hc    *http.Client
}

type githubPR struct {
	Number int `json:"number"`
	Head   struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
}

// githubRequiredChecks is the branch-protection required status checks object.
// Strict is GitHub's "require branches to be up to date before merging".
type githubRequiredChecks struct {
	Strict   bool     `json:"strict"`
	Contexts []string `json:"contexts"`
}

type githubCommitStatus struct {
	Context string `json:"context"`
	State   string `json:"state"` // success | pending | failure | error
}

type githubCheckRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`     // queued | in_progress | completed
	Conclusion string `json:"conclusion"` // success | failure | neutral | cancelled | timed_out | action_required | skipped | stale
}

func (c *githubPRClient) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var reader *strings.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = strings.NewReader(string(b))
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, err
		}
		return resp.StatusCode, nil
	}
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return resp.StatusCode, fmt.Errorf("github api %s %s: HTTP %d: %s", method, path, resp.StatusCode, apiErr.Message)
	}
	return resp.StatusCode, nil
}

func (c *githubPRClient) findOpenPR(ctx context.Context, branch string) (*githubPR, error) {
	var prs []githubPR
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&head=%s:%s", c.owner, c.repo, c.owner, branch)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

func (c *githubPRClient) createPR(ctx context.Context, branch, title, body string) (*githubPR, error) {
	var pr githubPR
	payload := map[string]string{"title": title, "head": branch, "base": "master", "body": body}
	status, err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", c.owner, c.repo), payload, &pr)
	if err != nil {
		// 422 "A pull request already exists" — a concurrent pass won the
		// race; adopt the existing PR instead of failing.
		if status == 422 {
			if existing, findErr := c.findOpenPR(ctx, branch); findErr == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return &pr, nil
}

func (c *githubPRClient) listCheckRuns(ctx context.Context, sha string) ([]githubCheckRun, error) {
	var out struct {
		CheckRuns []githubCheckRun `json:"check_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", c.owner, c.repo, sha)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.CheckRuns, nil
}

// requiredStatusChecks reads the branch-protection required status checks for
// branch. GitHub answers 404 when the branch is unprotected or has no required
// checks, which is reported as (nil, nil) — "nothing required".
func (c *githubPRClient) requiredStatusChecks(ctx context.Context, branch string) (*githubRequiredChecks, error) {
	var req githubRequiredChecks
	path := fmt.Sprintf("/repos/%s/%s/branches/%s/protection/required_status_checks", c.owner, c.repo, branch)
	status, err := c.do(ctx, http.MethodGet, path, nil, &req)
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// listCommitStatuses returns the legacy commit statuses on sha. Required
// contexts can be satisfied by either a check run or a commit status, so both
// have to be consulted before deciding a context is unmet.
func (c *githubPRClient) listCommitStatuses(ctx context.Context, sha string) ([]githubCommitStatus, error) {
	var out struct {
		Statuses []githubCommitStatus `json:"statuses"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/status", c.owner, c.repo, sha)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Statuses, nil
}

func (c *githubPRClient) listAnnotations(ctx context.Context, checkRunID int64, limit int) []string {
	var anns []struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		Level     string `json:"annotation_level"`
		Message   string `json:"message"`
	}
	path := fmt.Sprintf("/repos/%s/%s/check-runs/%d/annotations", c.owner, c.repo, checkRunID)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &anns); err != nil {
		return nil // annotations are best-effort evidence
	}
	out := make([]string, 0, len(anns))
	for _, a := range anns {
		if len(out) >= limit {
			break
		}
		out = append(out, fmt.Sprintf("%s:%d [%s] %s", a.Path, a.StartLine, a.Level, a.Message))
	}
	return out
}

func (c *githubPRClient) mergePR(ctx context.Context, number int) error {
	payload := map[string]string{"merge_method": "merge"}
	_, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", c.owner, c.repo, number), payload, nil)
	return err
}

func (c *githubPRClient) deleteBranch(ctx context.Context, branch string) {
	// Best-effort cleanup; a failure only leaves a stale remote branch.
	_, _ = c.do(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", c.owner, c.repo, branch), nil, nil)
}

// parseGitHubRemote extracts owner/repo from an origin URL in either the
// https://github.com/owner/repo(.git) or git@github.com:owner/repo(.git) form.
func parseGitHubRemote(url string) (owner, repo string, err error) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	var tail string
	switch {
	case strings.Contains(url, "github.com/"):
		tail = url[strings.Index(url, "github.com/")+len("github.com/"):]
	case strings.Contains(url, "github.com:"):
		tail = url[strings.Index(url, "github.com:")+len("github.com:"):]
	default:
		return "", "", fmt.Errorf("origin %q is not a github.com remote", url)
	}
	parts := strings.Split(tail, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse owner/repo from origin %q", url)
	}
	return parts[0], parts[1], nil
}

func newGitHubPRClientFromEnv(ctx context.Context, runner CommandRunner, repoDir string) (*githubPRClient, error) {
	token := prShepherdToken()
	if token == "" {
		return nil, fmt.Errorf("no GitHub token in env (checked %s) and no github.com entry in the git credential store", strings.Join(prShepherdTokenVars, ", "))
	}
	remote := runner.Run(ctx, repoDir, "git", "remote", "get-url", "origin")
	if remote.Err != nil {
		return nil, fmt.Errorf("git remote get-url origin: %v", remote.Err)
	}
	owner, repo, err := parseGitHubRemote(remote.Output)
	if err != nil {
		return nil, err
	}
	return &githubPRClient{
		base: "https://api.github.com", owner: owner, repo: repo, token: token,
		hc: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// prShepherdRequirementGate reports why branch protection cannot currently
// admit the pinned head, or "" when the merge may be attempted.
//
// Check runs going green is NOT the merge condition: GitHub arbitrates on the
// required *contexts*, and a context that never reported reads as "expected"
// forever. PR #61 sat green with 4 of 4 contexts unreported and burned a merge
// call every pass (live 2026-08-02). Naming the gap is what an operator needs.
func prShepherdRequirementGate(req *githubRequiredChecks, runs []githubCheckRun, statuses []githubCommitStatus, headUpToDate bool) string {
	if req == nil {
		return ""
	}
	if req.Strict && !headUpToDate {
		return "branch protection is strict (branches must be up to date) and the pinned head is out of date with master — the required checks can never report against the current base"
	}
	satisfied := make(map[string]bool, len(runs)+len(statuses))
	for _, r := range runs {
		// GitHub counts skipped and neutral check runs as non-blocking.
		if r.Status == "completed" && (r.Conclusion == "success" || r.Conclusion == "skipped" || r.Conclusion == "neutral") {
			satisfied[r.Name] = true
		}
	}
	for _, s := range statuses {
		if s.State == "success" {
			satisfied[s.Context] = true
		}
	}
	var unmet []string
	for _, ctxName := range req.Contexts {
		if !satisfied[ctxName] {
			unmet = append(unmet, ctxName)
		}
	}
	if len(unmet) == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d required status check(s) never reported on the pinned head: %s — the context names must match what CI publishes",
		len(unmet), len(req.Contexts), strings.Join(unmet, ", "))
}

// ---------------------------------------------------------------------------
// Deps + action registration

type prShepherdDeps struct {
	runner   CommandRunner
	claude   ClaudeRunner
	repoDir  string
	stateDir string
	// api, when nil, is built from the environment at run time (token env
	// vars + origin remote). Tests inject an httptest-backed client.
	api           *githubPRClient
	now           func() time.Time
	claudeTimeout time.Duration
	// fetchRetryDelay is the pause before the single fetch retry; zero in
	// tests so they run instantly.
	fetchRetryDelay time.Duration
}

var prShepherdDepsOverride *prShepherdDeps

func defaultPRShepherdDeps() prShepherdDeps {
	return prShepherdDeps{
		runner: defaultSuperpowersCommandRunner,
		claude: execClaudeRunner{
			AllowedTools: getenvDefault("BT_PR_SHEPHERD_ALLOWED_TOOLS",
				defaultSuperpowersAllowedTools+
					",Bash(go mod tidy:*),Bash(/usr/local/go/bin/go mod tidy:*),Bash(make check-quick:*)"),
		},
		repoDir:         goapFusionRepo,
		stateDir:        prShepherdDir(),
		now:             time.Now,
		claudeTimeout:   20 * time.Minute,
		fetchRetryDelay: 10 * time.Second,
	}
}

func init() {
	RegisterAction("ShepherdFleetPR", func(ctx *btcore.BTContext[Blackboard]) int {
		deps := defaultPRShepherdDeps()
		if prShepherdDepsOverride != nil {
			deps = *prShepherdDepsOverride
		}
		return runPRShepherd(ctx.Blackboard, deps)
	})
}

// prShepherdSkip records a healthy skip: the shepherd NEVER fails the cycle.
func prShepherdSkip(bb *Blackboard, outcome, format string, args ...any) int {
	bb.Outcome = outcome
	bb.Result = fmt.Sprintf(format, args...)
	return 1
}

// ---------------------------------------------------------------------------
// The one non-blocking pass

func runPRShepherd(bb *Blackboard, deps prShepherdDeps) int {
	if !prShepherdEnabled() {
		return prShepherdSkip(bb, "pr_shepherd_disabled", "## PR Shepherd Disabled\n\nBT_PR_SHEPHERD=off.")
	}
	now := deps.now
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Single durable save + log line per pass: the ONLY reliable trace of
	// what the shepherd decided (bb.Outcome is overwritten by later nodes).
	state := loadPRShepherdState(deps.stateDir)
	defer func() {
		state.LastOutcome = bb.Outcome
		state.LastPassAt = now().Format(time.RFC3339)
		if deps.stateDir != "" {
			_ = savePRShepherdState(deps.stateDir, state)
		}
		switch bb.Outcome {
		case "pr_shepherd_idle", "pr_shepherd_ci_pending", "pr_shepherd_synced_master", "pr_shepherd_pr_opened", "pr_shepherd_merged", "pr_shepherd_fix_pushed":
			Info("pr shepherd: pass complete", "outcome", bb.Outcome)
		default:
			Warn("pr shepherd: pass needs attention", "outcome", bb.Outcome, "detail", truncateGoap(bb.Result, 300))
		}
		// Noteworthy PR lifecycle changes notify the operator directly —
		// routine passes (idle, pending, synced, healthy skips) stay quiet.
		switch bb.Outcome {
		case "pr_shepherd_pr_opened", "pr_shepherd_merged", "pr_shepherd_fix_pushed",
			"pr_shepherd_merge_blocked", "pr_shepherd_fix_exhausted", "pr_shepherd_diverged":
			notifyFleetEvent("pr-shepherd", strings.TrimPrefix(bb.Outcome, "pr_shepherd_"), truncateGoap(bb.Result, 500))
		}
	}()

	api := deps.api
	if api == nil {
		built, err := newGitHubPRClientFromEnv(ctx, deps.runner, deps.repoDir)
		if err != nil {
			return prShepherdSkip(bb, "pr_shepherd_no_token",
				"## PR Shepherd Skipped\n\n%v — add one of %s to the daemon environment (unit EnvironmentFile).",
				err, strings.Join(prShepherdTokenVars, ", "))
		}
		api = built
	}
	branch := fleetPRBranch()

	prShepherdMu.Lock()
	defer prShepherdMu.Unlock()

	// --prune is load-bearing: after a merge the fleet branch is deleted via
	// the API, which leaves the LOCAL remote-tracking ref stale — the next
	// `push --force-with-lease` then fails its lease with "stale info"
	// forever (live 2026-07-22 16:52). Pruning keeps tracking refs truthful.
	fetch := deps.runner.Run(ctx, deps.repoDir, "git", "fetch", "origin", "--prune")
	if fetch.Err != nil {
		// One retry after a short pause: transient host network flakes ("No
		// route to host", twice on 2026-07-22) each cost a whole pass, and
		// the next touch can be 30-60 minutes away behind a long
		// implementation cycle. A single retry recovers the blip without
		// looping on a genuinely dead network.
		time.Sleep(deps.fetchRetryDelay)
		fetch = deps.runner.Run(ctx, deps.repoDir, "git", "fetch", "origin", "--prune")
	}
	if fetch.Err != nil {
		return prShepherdSkip(bb, "pr_shepherd_fetch_failed",
			"## PR Shepherd Skipped\n\ngit fetch origin failed twice (offline?):\n```\n%s\n```", truncateGoap(fetch.Output, 800))
	}
	localSHA := strings.TrimSpace(deps.runner.Run(ctx, deps.repoDir, "git", "rev-parse", "refs/heads/master").Output)
	originSHA := strings.TrimSpace(deps.runner.Run(ctx, deps.repoDir, "git", "rev-parse", "refs/remotes/origin/master").Output)
	if localSHA == "" || originSHA == "" {
		return prShepherdSkip(bb, "pr_shepherd_fetch_failed", "## PR Shepherd Skipped\n\nCould not resolve master/origin-master SHAs.")
	}

	if localSHA == originSHA {
		if state.PRNumber != 0 {
			api.deleteBranch(ctx, branch)
			state = prShepherdState{}
		}
		bb.OutcomeRefinement = "no_change"
		bb.QualityScore = 0.5
		bb.QualityAuthoritative = true
		return prShepherdSkip(bb, "pr_shepherd_idle", "## PR Shepherd Idle\n\nLocal master is in sync with origin (%s).", localSHA[:min(12, len(localSHA))])
	}

	localBehind := deps.runner.Run(ctx, deps.repoDir, "git", "merge-base", "--is-ancestor", "refs/heads/master", "refs/remotes/origin/master").Err == nil
	if localBehind {
		// A merge landed upstream (ours or someone else's): adopt it with the
		// fleet's invariant-safe non-forced ff primitive; the goap preflight
		// materializer syncs the working tree on the next cycle.
		if ff := deps.runner.Run(ctx, deps.repoDir, "git", "fetch", ".", "refs/remotes/origin/master:master"); ff.Err != nil {
			return prShepherdSkip(bb, "pr_shepherd_sync_failed",
				"## PR Shepherd Skipped\n\nNon-forced ff of local master refused:\n```\n%s\n```", truncateGoap(ff.Output, 800))
		}
		api.deleteBranch(ctx, branch)
		state = prShepherdState{}
		return prShepherdSkip(bb, "pr_shepherd_synced_master",
			"## PR Shepherd Synced\n\nFast-forwarded local master to origin (%s).", originSHA[:min(12, len(originSHA))])
	}
	originBehind := deps.runner.Run(ctx, deps.repoDir, "git", "merge-base", "--is-ancestor", "refs/remotes/origin/master", "refs/heads/master").Err == nil
	if !originBehind {
		return prShepherdSkip(bb, "pr_shepherd_diverged",
			"## PR Shepherd Needs Operator\n\nLocal master (%s) and origin/master (%s) have DIVERGED; refusing any autonomous rewrite. Reconcile manually.",
			localSHA[:min(12, len(localSHA))], originSHA[:min(12, len(originSHA))])
	}

	// Local master is strictly ahead: make sure the fleet branch + PR track it.
	pr, err := api.findOpenPR(ctx, branch)
	if err != nil {
		return prShepherdSkip(bb, "pr_shepherd_api_error", "## PR Shepherd Skipped\n\n%v", err)
	}
	if pr == nil {
		// No open PR: this is a fresh batch, so push local master and open
		// one. The branch is ALWAYS pushed from local master (fix commits
		// land on master first), so force-with-lease can never destroy work.
		push := deps.runner.Run(ctx, deps.repoDir, "git", "push", "--force-with-lease", "origin", "refs/heads/master:refs/heads/"+branch)
		if push.Err != nil {
			return prShepherdSkip(bb, "pr_shepherd_push_failed",
				"## PR Shepherd Skipped\n\nBranch push failed:\n```\n%s\n```", truncateGoap(push.Output, 800))
		}
		title, body := fleetPRTitleAndBody(ctx, deps.runner, deps.repoDir)
		created, err := api.createPR(ctx, branch, title, body)
		if err != nil {
			return prShepherdSkip(bb, "pr_shepherd_api_error", "## PR Shepherd Skipped\n\nPR creation failed: %v", err)
		}
		state.PRNumber = created.Number
		return prShepherdSkip(bb, "pr_shepherd_pr_opened",
			"## PR Shepherd Opened PR #%d\n\nBranch `%s` at %s; CI will be checked next cycle.", created.Number, branch, localSHA[:min(12, len(localSHA))])
	}
	state.PRNumber = pr.Number

	// The open PR's head is PINNED for this batch: local-master commits that
	// land while its CI is still in flight must NOT be pushed onto it — every
	// push restarted CI and starved PR #25 for hours (2026-07-23 incident).
	// New landings accumulate on local master for the NEXT batch, opened only
	// after this PR merges. CI is therefore always checked against the pinned
	// head, never against local master's possibly-advanced tip.
	pinnedSHA := pr.Head.SHA

	runs, err := api.listCheckRuns(ctx, pinnedSHA)
	if err != nil {
		return prShepherdSkip(bb, "pr_shepherd_api_error", "## PR Shepherd Skipped\n\ncheck-runs query failed: %v", err)
	}
	var failed []githubCheckRun
	pending := len(runs) == 0
	for _, r := range runs {
		if r.Status != "completed" {
			pending = true
			continue
		}
		switch r.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required", "stale":
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 && pending {
		return prShepherdSkip(bb, "pr_shepherd_ci_pending",
			"## PR Shepherd Waiting\n\nPR #%d CI still running for %s (%d check runs); checked again next cycle.", pr.Number, pinnedSHA[:min(12, len(pinnedSHA))], len(runs))
	}

	if len(failed) == 0 {
		// Pre-flight branch protection. Reading it needs elevated scope, so a
		// query failure degrades OPEN (attempt the merge, let GitHub arbitrate)
		// rather than gating every merge the fleet makes.
		if req, reqErr := api.requiredStatusChecks(ctx, "master"); reqErr != nil {
			Warn("pr shepherd: required-status-checks query failed, merging unguarded", "err", reqErr)
		} else if req != nil {
			var statuses []githubCommitStatus
			if st, stErr := api.listCommitStatuses(ctx, pinnedSHA); stErr == nil {
				statuses = st
			} else {
				Warn("pr shepherd: commit-status query failed", "err", stErr)
			}
			headUpToDate := deps.runner.Run(ctx, deps.repoDir, "git", "merge-base", "--is-ancestor", "refs/remotes/origin/master", pinnedSHA).Err == nil
			if reason := prShepherdRequirementGate(req, runs, statuses, headUpToDate); reason != "" {
				return prShepherdSkip(bb, "pr_shepherd_merge_blocked",
					"## PR Shepherd Merge Blocked\n\nPR #%d check runs are green but branch protection cannot admit it: %s.\n\nNo merge was attempted; this needs an operator.", pr.Number, reason)
			}
		}
		if err := api.mergePR(ctx, pr.Number); err != nil {
			return prShepherdSkip(bb, "pr_shepherd_merge_blocked",
				"## PR Shepherd Merge Blocked\n\nPR #%d is green but the merge was refused: %v", pr.Number, err)
		}
		_ = deps.runner.Run(ctx, deps.repoDir, "git", "fetch", "origin", "--prune")
		if ff := deps.runner.Run(ctx, deps.repoDir, "git", "fetch", ".", "refs/remotes/origin/master:master"); ff.Err != nil {
			return prShepherdSkip(bb, "pr_shepherd_merged",
				"## PR Shepherd Merged PR #%d\n\nMerged, but the local master ff was refused (will sync next cycle):\n```\n%s\n```", pr.Number, truncateGoap(ff.Output, 400))
		}
		api.deleteBranch(ctx, branch)
		state = prShepherdState{}
		return prShepherdSkip(bb, "pr_shepherd_merged",
			"## PR Shepherd Merged PR #%d\n\nPipeline green; merged and fast-forwarded local master.", pr.Number)
	}

	// Fix-red commits for the pinned head are the one exception allowed to
	// update the open PR's branch while a batch is in flight.
	return runPRShepherdFix(ctx, bb, deps, api, &state, branch, pinnedSHA, pr.Number, failed, now)
}

// runPRShepherdFix runs ONE bounded Claude fix attempt for a red pipeline:
// evidence from check-run annotations, Claude edits in a fresh worktree, a
// DETERMINISTIC commit (Claude has no git-write tools, mirroring the
// superpowers apply step), landing on local master via the non-forced ff
// primitive, then re-pushing the fleet branch.
func runPRShepherdFix(ctx context.Context, bb *Blackboard, deps prShepherdDeps, api *githubPRClient, state *prShepherdState, branch, headSHA string, prNumber int, failed []githubCheckRun, now func() time.Time) int {
	maxAttempts := prShepherdMaxFixAttempts()
	if state.FixAttempts[headSHA] >= maxAttempts || state.TotalFixAttempts >= 2*maxAttempts {
		return prShepherdSkip(bb, "pr_shepherd_fix_exhausted",
			"## PR Shepherd Fix Budget Exhausted\n\nPR #%d head %s: %d/%d attempts for this head, %d total since the last merge. Operator attention needed.",
			prNumber, headSHA[:min(12, len(headSHA))], state.FixAttempts[headSHA], maxAttempts, state.TotalFixAttempts)
	}
	if claudeBackoffActive(bb, now()) {
		until, _ := loadClaudeBackoffState(bb)
		return prShepherdSkip(bb, "pr_shepherd_rate_limited",
			"## PR Shepherd Skipped\n\nClaude backoff active until %s; fix deferred.", until.Format(time.RFC3339))
	}

	var evidence strings.Builder
	for _, f := range failed {
		fmt.Fprintf(&evidence, "- check %q concluded %s\n", f.Name, f.Conclusion)
		for _, a := range api.listAnnotations(ctx, f.ID, 50) {
			fmt.Fprintf(&evidence, "  %s\n", a)
		}
	}

	chargeAttempt := func() {
		if state.FixAttempts == nil {
			state.FixAttempts = map[string]int{}
		}
		state.FixAttempts[headSHA]++
		state.TotalFixAttempts++
	}

	short := headSHA[:min(8, len(headSHA))]
	fixBranch := "pr-fix-" + short
	wtPath := shepherdFixWorktreePath(fixBranch)
	if wt := deps.runner.Run(ctx, deps.repoDir, "git", "worktree", "add", "-b", fixBranch, wtPath, "refs/heads/master"); wt.Err != nil {
		return prShepherdSkip(bb, "pr_shepherd_fix_failed",
			"## PR Shepherd Fix Failed\n\nWorktree setup failed:\n```\n%s\n```", truncateGoap(wt.Output, 800))
	}
	cleanup := func() {
		_ = deps.runner.Run(ctx, deps.repoDir, "git", "worktree", "remove", "--force", wtPath)
		_ = deps.runner.Run(ctx, deps.repoDir, "git", "branch", "-D", fixBranch)
	}

	prompt := buildPRFixPrompt(branch, headSHA, prNumber, evidence.String())
	claudeCtx, cancel := context.WithTimeout(ctx, deps.claudeTimeout)
	res := deps.claude.RunClaude(claudeCtx, wtPath, prompt)
	cancel()
	combined := res.Output
	if res.Err != nil {
		combined += " " + res.Err.Error()
	}
	if isClaudeRateLimit(combined) {
		cleanup()
		saveClaudeBackoffState(bb, claudeBackoffDeadline(combined, now(), goapClaudeBackoffWindow))
		return prShepherdSkip(bb, "pr_shepherd_rate_limited",
			"## PR Shepherd Rate-Limited\n\n```\n%s\n```", truncateGoap(combined, 1200))
	}

	status := deps.runner.Run(ctx, wtPath, "git", "status", "--porcelain")
	if strings.TrimSpace(status.Output) == "" {
		cleanup()
		chargeAttempt()
		return prShepherdSkip(bb, "pr_shepherd_fix_failed",
			"## PR Shepherd Fix Produced No Changes\n\nAttempt %d/%d for head %s:\n```\n%s\n```",
			state.FixAttempts[headSHA], prShepherdMaxFixAttempts(), short, truncateGoap(combined, 1200))
	}
	if add := stageAllExceptGenerated(ctx, deps.runner, wtPath); add.Err != nil {
		cleanup()
		chargeAttempt()
		return prShepherdSkip(bb, "pr_shepherd_fix_failed", "## PR Shepherd Fix Failed\n\nStaging failed:\n```\n%s\n```", truncateGoap(add.Output, 800))
	}
	msg := fmt.Sprintf("fleet: fix CI for PR #%d head %s (attempt %d)", prNumber, short, state.FixAttempts[headSHA]+1)
	if commit := deps.runner.Run(ctx, wtPath, "git", "commit", "-m", msg); commit.Err != nil {
		cleanup()
		chargeAttempt()
		return prShepherdSkip(bb, "pr_shepherd_fix_failed",
			"## PR Shepherd Fix Rejected By Hook\n\n```\n%s\n```", truncateGoap(commit.Output, 1200))
	}
	if ff := deps.runner.Run(ctx, deps.repoDir, "git", "fetch", ".", "refs/heads/"+fixBranch+":master"); ff.Err != nil {
		cleanup()
		chargeAttempt()
		return prShepherdSkip(bb, "pr_shepherd_fix_failed",
			"## PR Shepherd Fix Landed Nowhere\n\nNon-forced ff of the fix onto local master refused (master moved mid-fix):\n```\n%s\n```", truncateGoap(ff.Output, 800))
	}
	cleanup()
	if push := deps.runner.Run(ctx, deps.repoDir, "git", "push", "--force-with-lease", "origin", "refs/heads/master:refs/heads/"+branch); push.Err != nil {
		chargeAttempt()
		return prShepherdSkip(bb, "pr_shepherd_push_failed",
			"## PR Shepherd Fix Landed Locally, Push Failed\n\n```\n%s\n```", truncateGoap(push.Output, 800))
	}
	chargeAttempt()
	return prShepherdSkip(bb, "pr_shepherd_fix_pushed",
		"## PR Shepherd Pushed CI Fix\n\nPR #%d: fix for head %s landed on master and re-pushed to `%s` (attempt %d/%d); CI re-runs.",
		prNumber, short, branch, state.FixAttempts[headSHA], prShepherdMaxFixAttempts())
}

func buildPRFixPrompt(branch, headSHA string, prNumber int, evidence string) string {
	return fmt.Sprintf(`You are fixing a FAILING GitHub Actions pipeline for PR #%d (branch %s, head %s) of this Go repository. You are in a dedicated git worktree at the branch's current code.

CI failure evidence (check runs and their annotations):
%s

Instructions:
- Reproduce locally first: PATH=/usr/local/go/bin:$PATH go vet ./..., gofmt -l ., and the focused tests for the files named above. 'make check-quick' mirrors the CI lint job.
- Fix the ROOT CAUSE in the source; do not weaken or skip tests to get green.
- Known-environmental on THIS host only: TestNewGoBuildTool_* fail here with buildvcs errors but PASS in CI — do not "fix" those.
- Do NOT run any git write commands (no add/commit/push) — the caller stages and commits deterministically after you finish.
- When done, summarize what was broken and what you changed.`, prNumber, branch, headSHA, evidence)
}

// fleetPRTitleAndBody derives the PR title/body from the commits local master
// is ahead by.
func fleetPRTitleAndBody(ctx context.Context, runner CommandRunner, repoDir string) (string, string) {
	subject := strings.TrimSpace(runner.Run(ctx, repoDir, "git", "log", "-1", "--format=%s", "refs/heads/master").Output)
	if subject == "" {
		subject = "fleet landings"
	}
	count := strings.TrimSpace(runner.Run(ctx, repoDir, "git", "rev-list", "--count", "refs/remotes/origin/master..refs/heads/master").Output)
	title := subject
	if n, err := strconv.Atoi(count); err == nil && n > 1 {
		title = fmt.Sprintf("%s (+%d more)", subject, n-1)
	}
	body := fmt.Sprintf("Automated fleet landing PR: %s commits accrued on the daemon's local master (origin/master is branch-protected). The PR shepherd fixes CI failures and merges on green.\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)", count)
	return title, body
}

// ---------------------------------------------------------------------------
// Landing tail: ship a landed run to origin — direct push when allowed,
// fleet-PR fallback when master is protected.

// landingPRClientFactory builds the GitHub client for the landing-tail PR
// fallback; a package var so tests can inject an httptest-backed client.
var landingPRClientFactory = newGitHubPRClientFromEnv

// pushLandingMasterToOrigin replaces the bare `git push origin master` step of
// the landing commit: on a protected-branch rejection it ships local master
// to the fleet PR branch instead (ApplyStatus committed_pr_opened, SUCCESS) —
// the shepherd drives that PR to merge on subsequent cycles. Any other
// failure keeps the committed_unpushed contract (refunded infra, caefb02).
func pushLandingMasterToOrigin(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	push := runner.Run(ctx, run.RepoDir, "git", "push", "origin", "master")
	if push.Err == nil {
		notifyFleetEvent("fleet-landing", "landed",
			fmt.Sprintf("Landed %s (run %s) — pushed straight to origin/master.", run.AppliedCommit, run.ID))
		return nil
	}
	if strings.Contains(push.Output, "protected branch") {
		prNumber, err := shipLandingToPR(ctx, runner, run.RepoDir)
		if err == nil {
			run.ApplyStatus = "committed_pr_opened"
			notifyFleetEvent("fleet-landing", "landed",
				fmt.Sprintf("Landed %s (run %s) — origin/master is protected; shipped to PR #%d (%s). The PR shepherd merges on green CI.",
					run.AppliedCommit, run.ID, prNumber, fleetPRBranch()))
			return nil
		}
		writeApplyCommitEvidence(run, "fleet PR fallback failed", CommandResult{Command: "shipLandingToPR", Output: err.Error(), Err: err})
	}
	run.ApplyStatus = "committed_unpushed"
	return fmt.Errorf("committed_unpushed: git push origin master failed: %v\n%s", push.Err, push.Output)
}

func shipLandingToPR(ctx context.Context, runner CommandRunner, repoDir string) (int, error) {
	api, err := landingPRClientFactory(ctx, runner, repoDir)
	if err != nil {
		return 0, err
	}

	// Serialize with runPRShepherd's ShepherdFleetPR pass: both push the same
	// fleet branch and read/write the same durable prShepherdState, and two
	// separate scheduled agents in one daemon process can overlap their
	// cycles (see prShepherdMu's doc comment above).
	prShepherdMu.Lock()
	defer prShepherdMu.Unlock()

	branch := fleetPRBranch()
	// Best-effort prune first: a remote-tracking ref left behind by a merged
	// branch's API deletion would fail the lease below with "stale info".
	_ = runner.Run(ctx, repoDir, "git", "fetch", "origin", "--prune")
	if push := runner.Run(ctx, repoDir, "git", "push", "--force-with-lease", "origin", "refs/heads/master:refs/heads/"+branch); push.Err != nil {
		return 0, fmt.Errorf("fleet branch push failed: %v\n%s", push.Err, push.Output)
	}
	pr, err := api.findOpenPR(ctx, branch)
	if err != nil {
		return 0, err
	}
	if pr == nil {
		title, body := fleetPRTitleAndBody(ctx, runner, repoDir)
		if pr, err = api.createPR(ctx, branch, title, body); err != nil {
			return 0, err
		}
	}
	st := loadPRShepherdState(prShepherdDir())
	if st.PRNumber != pr.Number {
		st.PRNumber = pr.Number
		_ = savePRShepherdState(prShepherdDir(), st)
	}
	return pr.Number, nil
}
