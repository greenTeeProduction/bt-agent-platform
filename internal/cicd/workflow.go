package cicd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowReport is the evidence artifact produced by ValidateWorkflows.
type WorkflowReport struct {
	Root             string   `json:"root"`
	Checks           []Check  `json:"checks"`
	Passed           int      `json:"passed"`
	Failed           int      `json:"failed"`
	AllPassed        bool     `json:"all_passed"`
	Workflow         []string `json:"workflow_files"`
	Summary          string   `json:"summary"`
	DependabotExists bool     `json:"dependabot_exists"`
	RunnerInstalled  bool     `json:"runner_installed"`
}

// Check records one CI/CD maturity assertion.
type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details"`
}

// ValidateWorkflows validates repository CI/CD workflow readiness locally.
func ValidateWorkflows(root string) (WorkflowReport, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return WorkflowReport{}, err
	}
	report := WorkflowReport{Root: abs}

	ci, ciErr := loadWorkflow(filepath.Join(abs, ".github", "workflows", "ci.yml"))
	nightly, nightlyErr := loadWorkflow(filepath.Join(abs, ".github", "workflows", "nightly.yml"))
	codeql, codeqlErr := loadWorkflow(filepath.Join(abs, ".github", "workflows", "codeql.yml"))
	if ciErr == nil {
		report.Workflow = append(report.Workflow, ".github/workflows/ci.yml")
	}
	if nightlyErr == nil {
		report.Workflow = append(report.Workflow, ".github/workflows/nightly.yml")
	}
	sort.Strings(report.Workflow)

	report.add("ci workflow exists and parses", ciErr == nil, errDetail(ciErr, "ci.yml parsed"))
	report.add("nightly workflow exists and parses", nightlyErr == nil, errDetail(nightlyErr, "nightly.yml parsed"))
	report.add("codeql workflow exists and parses", codeqlErr == nil, errDetail(codeqlErr, "codeql.yml parsed"))
	if ciErr == nil {
		report.validateCI(ci)
	}
	if nightlyErr == nil {
		report.validateNightly(nightly)
	}
	if codeqlErr == nil {
		report.validateCodeQL(codeql)
	}

	// Validate Dependabot config for automated dependency updates.
	dependabotPath := filepath.Join(abs, ".github", "dependabot.yml")
	dependabotCfg, dependabotErr := loadDependabotConfig(dependabotPath)
	report.DependabotExists = dependabotErr == nil
	report.add("dependabot config exists and parses", dependabotErr == nil, errDetail(dependabotErr, "dependabot.yml parsed"))
	if dependabotErr == nil {
		hasGoMod := false
		hasGHA := false
		for _, u := range dependabotCfg.Updates {
			if u.Ecosystem == "gomod" {
				hasGoMod = true
			}
			if u.Ecosystem == "github-actions" {
				hasGHA = true
			}
		}
		report.add("dependabot config covers gomod", hasGoMod, boolDetail(hasGoMod, "gomod ecosystem configured", "gomod ecosystem missing"))
		report.add("dependabot config covers github-actions", hasGHA, boolDetail(hasGHA, "github-actions ecosystem configured", "github-actions ecosystem missing"))
	}

	// Check if a GitHub Actions self-hosted runner is installed on this machine.
	// Advisory only — runner presence is environment-dependent, not a workflow structure issue.
	runnerDir := filepath.Join(os.Getenv("HOME"), "actions-runner")
	_, runnerErr := os.Stat(filepath.Join(runnerDir, ".runner"))
	report.RunnerInstalled = runnerErr == nil
	report.addAdvisory("self-hosted runner installed", report.RunnerInstalled,
		boolDetail(report.RunnerInstalled, "runner configured at "+runnerDir,
			"no runner at "+runnerDir+" -- run 'make setup-runner TOKEN=...'"))

	report.AllPassed = report.Failed == 0
	report.Summary = fmt.Sprintf("%d/%d CI/CD workflow checks passed", report.Passed, report.Passed+report.Failed)
	return report, nil
}

type dependabotConfig struct {
	Version int `yaml:"version"`
	Updates []struct {
		Ecosystem string `yaml:"package-ecosystem"`
		Directory string `yaml:"directory"`
	} `yaml:"updates"`
}

func loadDependabotConfig(path string) (dependabotConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return dependabotConfig{}, err
	}
	var cfg dependabotConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return dependabotConfig{}, err
	}
	if cfg.Version != 2 || len(cfg.Updates) == 0 {
		return dependabotConfig{}, fmt.Errorf("dependabot config: invalid version or no updates")
	}
	return cfg, nil
}

func (r *WorkflowReport) add(name string, passed bool, details string) {
	r.Checks = append(r.Checks, Check{Name: name, Passed: passed, Details: details})
	if passed {
		r.Passed++
	} else {
		r.Failed++
	}
}

// addAdvisory records a check without affecting AllPassed status.
// Used for environment-dependent checks (e.g., self-hosted runner presence).
func (r *WorkflowReport) addAdvisory(name string, passed bool, details string) {
	r.Checks = append(r.Checks, Check{Name: name, Passed: passed, Details: details})
	if passed {
		r.Passed++
	}
}

type workflow struct {
	Name        string                 `yaml:"name"`
	On          any                    `yaml:"on"`
	Permissions any                    `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Name          string         `yaml:"name"`
	RunsOn        any            `yaml:"runs-on"`
	Needs         any            `yaml:"needs"`
	If            string         `yaml:"if"`
	TimeoutMinute int            `yaml:"timeout-minutes"`
	Permissions   any            `yaml:"permissions"`
	Steps         []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string      `yaml:"name"`
	Uses string      `yaml:"uses"`
	Run  string      `yaml:"run"`
	With interface{} `yaml:"with"`
}

func loadWorkflow(path string) (workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return workflow{}, err
	}
	var wf workflow
	if err := yaml.Unmarshal(b, &wf); err != nil {
		return workflow{}, err
	}
	if wf.Name == "" || len(wf.Jobs) == 0 {
		return workflow{}, fmt.Errorf("workflow %s is missing name or jobs", path)
	}
	return wf, nil
}

func (r *WorkflowReport) validateCI(wf workflow) {
	requiredJobs := []string{"lint", "security", "test", "build", "release"}
	for _, job := range requiredJobs {
		_, ok := wf.Jobs[job]
		r.add("ci job: "+job, ok, boolDetail(ok, "job present", "job missing"))
	}
	r.add("ci runs on push and pull_request", workflowHasEvents(wf.On, "push", "pull_request"), "push + pull_request triggers required")
	r.add("ci lint runs go vet", jobRuns(wf.Jobs["lint"], "go vet ./..."), "lint job must run go vet ./...")
	r.add("ci lint verifies go mod tidy", jobRuns(wf.Jobs["lint"], "go mod tidy"), "lint job must run go mod tidy check")
	r.add("ci lint runs golangci-lint", jobUsesOrRuns(wf.Jobs["lint"], "golangci-lint"), "lint job must run golangci-lint")
	r.add("ci security runs gosec", jobUsesOrRuns(wf.Jobs["security"], "gosec"), "security job must run gosec")
	r.add("ci security runs govulncheck", jobRuns(wf.Jobs["security"], "govulncheck"), "security job must run govulncheck")
	r.add("ci tests run short race coverage", jobRunsAll(wf.Jobs["test"], "go test", "-short", "-race", "-coverprofile"), "test job must run short race coverage")
	r.add("ci build compiles core binaries", jobRunsAll(wf.Jobs["build"], "cmd/bt-agent", "cmd/bt-evaluator", "cmd/bt-langagent", "cmd/bt-dashboard", "cmd/bt-gardener"), "build job must compile all core binaries")
	r.add("ci build compiles auxiliary binaries", jobRunsAll(wf.Jobs["build"], "cmd/benchcmp", "cmd/bt-security-probe", "cmd/bt-ci-doctor", "cmd/bt-tree-integration", "cmd/bt-scalability-probe"), "build job must compile all auxiliary binaries")
	r.add("release waits for gates", needsAll(wf.Jobs["release"].Needs, "lint", "security", "test", "build"), "release job must need lint/security/test/build")
	r.add("release builds amd64 and arm64", jobRunsAll(wf.Jobs["release"], "GOARCH=amd64", "GOARCH=arm64"), "release job must build multi-arch artifacts")
	r.add("release builds auxiliary multi-arch", jobRunsAll(wf.Jobs["release"], "bt-security-probe-linux-arm64", "bt-ci-doctor-linux-arm64", "bt-tree-integration-linux-arm64", "benchcmp-linux-arm64", "bt-scalability-probe-linux-arm64"), "release job must build multi-arch auxiliary binaries")

	// Verify timeout-minutes set on all CI jobs to prevent runaway builds.
	for _, name := range requiredJobs {
		job, ok := wf.Jobs[name]
		r.add("ci job "+name+" has timeout-minutes", ok && job.TimeoutMinute > 0,
			timeoutDetail(ok, job.TimeoutMinute, name))
	}
}

func (r *WorkflowReport) validateNightly(wf workflow) {
	requiredJobs := []string{"full-tests", "benchmark-compare"}
	for _, job := range requiredJobs {
		_, ok := wf.Jobs[job]
		r.add("nightly job: "+job, ok, boolDetail(ok, "job present", "job missing"))
	}
	r.add("nightly has schedule and manual dispatch", workflowHasEvents(wf.On, "schedule", "workflow_dispatch"), "nightly must support cron + manual run")
	r.add("nightly runs on Jetson self-hosted labels", runsOnAll(wf.Jobs["full-tests"].RunsOn, "self-hosted", "jetson", "arm64"), "full-tests must run on self-hosted jetson arm64")
	r.add("nightly verifies Ollama", jobRuns(wf.Jobs["full-tests"], "localhost:11434"), "full-tests must check local Ollama")
	r.add("nightly runs full non-short suite", jobRunsAll(wf.Jobs["full-tests"], "go test", "-timeout 90m", "./..."), "full-tests must run the full suite")
	r.add("nightly uploads artifacts", jobUsesOrRuns(wf.Jobs["full-tests"], "actions/upload-artifact"), "full-tests must upload evidence artifacts")
	r.add("nightly benchmark comparison depends on full tests", needsAll(wf.Jobs["benchmark-compare"].Needs, "full-tests"), "benchmark comparison must depend on full-tests")
	r.add("nightly detects benchmark regressions", jobRunsAll(wf.Jobs["benchmark-compare"], "benchcmp", "check"), "benchmark comparison must call benchcmp check")
}

// validateCodeQL validates a CodeQL workflow for SAST analysis coverage.
func (r *WorkflowReport) validateCodeQL(wf workflow) {
	requiredJobs := []string{"analyze"}
	for _, job := range requiredJobs {
		_, ok := wf.Jobs[job]
		r.add("codeql job: "+job, ok, boolDetail(ok, "job present", "job missing"))
	}
	r.add("codeql runs on push and pull_request and schedule", workflowHasEvents(wf.On, "push", "pull_request", "schedule"), "codeql must run on push + PR + weekly schedule")
	r.add("codeql uses github/codeql-action/init@v3", jobUsesOrRuns(wf.Jobs["analyze"], "github/codeql-action/init@v3"), "codeql must use init@v3")
	r.add("codeql uses github/codeql-action/analyze@v3", jobUsesOrRuns(wf.Jobs["analyze"], "github/codeql-action/analyze@v3"), "codeql must use analyze@v3")
	r.add("codeql has security-events write permission", r.hasPermission(wf, "security-events"), "codeql needs security-events: write permission")
	r.add("codeql uses security-and-quality query suite", jobRuns(wf.Jobs["analyze"], "security-and-quality"), "codeql should use +security-and-quality queries")
	r.add("codeql provides Go module caching", jobUsesOrRuns(wf.Jobs["analyze"], "actions/cache@v4"), "codeql job should use Go module cache")
}

func workflowHasEvents(on any, events ...string) bool {
	s := flatten(on)
	for _, event := range events {
		if !containsExactOrKey(s, event) {
			return false
		}
	}
	return true
}

func jobRuns(job workflowJob, fragment string) bool {
	return jobRunsAll(job, fragment)
}

func jobRunsAll(job workflowJob, fragments ...string) bool {
	body := strings.ToLower(flatten(job.Steps))
	for _, fragment := range fragments {
		if !strings.Contains(body, strings.ToLower(fragment)) {
			return false
		}
	}
	return true
}

func jobUsesOrRuns(job workflowJob, fragment string) bool {
	return strings.Contains(strings.ToLower(flatten(job.Steps)), strings.ToLower(fragment))
}

func needsAll(needs any, expected ...string) bool {
	body := flatten(needs)
	for _, want := range expected {
		if !containsExactOrKey(body, want) {
			return false
		}
	}
	return true
}

func runsOnAll(runsOn any, labels ...string) bool {
	body := flatten(runsOn)
	for _, label := range labels {
		if !containsExactOrKey(body, label) {
			return false
		}
	}
	return true
}

func containsExactOrKey(body, want string) bool {
	body = strings.ToLower(body)
	want = strings.ToLower(want)
	return strings.Contains(body, want)
}

func flatten(v any) string {
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
			return
		case string:
			out = append(out, t)
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for k, item := range t {
				out = append(out, k)
				walk(item)
			}
		case map[any]any:
			for k, item := range t {
				walk(k)
				walk(item)
			}
		case []workflowStep:
			for _, step := range t {
				out = append(out, step.Name, step.Uses, step.Run)
				if step.With != nil {
					walk(step.With)
				}
			}
		default:
			out = append(out, fmt.Sprint(t))
		}
	}
	walk(v)
	return strings.Join(out, "\n")
}

func errDetail(err error, ok string) string {
	if err != nil {
		return err.Error()
	}
	return ok
}

func boolDetail(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func timeoutDetail(ok bool, timeoutMin int, jobName string) string {
	if !ok {
		return "job " + jobName + " not found"
	}
	if timeoutMin > 0 {
		return fmt.Sprintf("timeout-minutes=%d", timeoutMin)
	}
	return "timeout-minutes not set — job may run indefinitely"
}

// hasPermission checks if a workflow or its analyze job has a specific permission set.
func (r *WorkflowReport) hasPermission(wf workflow, perm string) bool {
	// Check workflow-level permissions first
	if wf.Permissions != nil {
		if permStr, ok := wf.Permissions.(string); ok {
			if strings.Contains(strings.ToLower(permStr), "write-all") {
				return true
			}
		}
		body := flatten(wf.Permissions)
		if strings.Contains(strings.ToLower(body), strings.ToLower(perm)) &&
			strings.Contains(strings.ToLower(body), "write") {
			return true
		}
	}
	// Fall back to job-level permissions (CodeQL places permissions under jobs.analyze.permissions)
	if job, ok := wf.Jobs["analyze"]; ok && job.Permissions != nil {
		if permStr, ok := job.Permissions.(string); ok {
			return strings.Contains(strings.ToLower(permStr), "write-all")
		}
		body := flatten(job.Permissions)
		return strings.Contains(strings.ToLower(body), strings.ToLower(perm)) &&
			strings.Contains(strings.ToLower(body), "write")
	}
	return false
}
