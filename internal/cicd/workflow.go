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
	Root      string   `json:"root"`
	Checks    []Check  `json:"checks"`
	Passed    int      `json:"passed"`
	Failed    int      `json:"failed"`
	AllPassed bool     `json:"all_passed"`
	Workflow  []string `json:"workflow_files"`
	Summary   string   `json:"summary"`
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
	if ciErr == nil {
		report.Workflow = append(report.Workflow, ".github/workflows/ci.yml")
	}
	if nightlyErr == nil {
		report.Workflow = append(report.Workflow, ".github/workflows/nightly.yml")
	}
	sort.Strings(report.Workflow)

	report.add("ci workflow exists and parses", ciErr == nil, errDetail(ciErr, "ci.yml parsed"))
	report.add("nightly workflow exists and parses", nightlyErr == nil, errDetail(nightlyErr, "nightly.yml parsed"))
	if ciErr == nil {
		report.validateCI(ci)
	}
	if nightlyErr == nil {
		report.validateNightly(nightly)
	}

	report.AllPassed = report.Failed == 0
	report.Summary = fmt.Sprintf("%d/%d CI/CD workflow checks passed", report.Passed, report.Passed+report.Failed)
	return report, nil
}

func (r *WorkflowReport) add(name string, passed bool, details string) {
	r.Checks = append(r.Checks, Check{Name: name, Passed: passed, Details: details})
	if passed {
		r.Passed++
	} else {
		r.Failed++
	}
}

type workflow struct {
	Name string                 `yaml:"name"`
	On   any                    `yaml:"on"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Name          string         `yaml:"name"`
	RunsOn        any            `yaml:"runs-on"`
	Needs         any            `yaml:"needs"`
	If            string         `yaml:"if"`
	TimeoutMinute int            `yaml:"timeout-minutes"`
	Steps         []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string `yaml:"name"`
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
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
	r.add("ci security runs gosec", jobUsesOrRuns(wf.Jobs["security"], "gosec"), "security job must run gosec")
	r.add("ci security runs govulncheck", jobRuns(wf.Jobs["security"], "govulncheck"), "security job must run govulncheck")
	r.add("ci tests run short race coverage", jobRunsAll(wf.Jobs["test"], "go test", "-short", "-race", "-coverprofile"), "test job must run short race coverage")
	r.add("ci build compiles core binaries", jobRunsAll(wf.Jobs["build"], "cmd/bt-agent", "cmd/bt-evaluator", "cmd/bt-langagent", "cmd/bt-dashboard", "cmd/bt-gardener"), "build job must compile all core binaries")
	r.add("ci build compiles auxiliary binaries", jobRunsAll(wf.Jobs["build"], "cmd/benchcmp", "cmd/bt-security-probe", "cmd/bt-ci-doctor", "cmd/bt-tree-integration"), "build job must compile all auxiliary binaries")
	r.add("release waits for gates", needsAll(wf.Jobs["release"].Needs, "lint", "security", "test", "build"), "release job must need lint/security/test/build")
	r.add("release builds amd64 and arm64", jobRunsAll(wf.Jobs["release"], "GOARCH=amd64", "GOARCH=arm64"), "release job must build multi-arch artifacts")
	r.add("release builds auxiliary multi-arch", jobRunsAll(wf.Jobs["release"], "bt-security-probe-linux-arm64", "bt-ci-doctor-linux-arm64", "bt-tree-integration-linux-arm64", "benchcmp-linux-arm64"), "release job must build multi-arch auxiliary binaries")
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
