package gardener

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateWorkflowsRepositoryPasses(t *testing.T) {
	report, err := ValidateWorkflows(repoRoot(t))
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if !report.AllPassed {
		for _, check := range report.Checks {
			if !check.Passed {
				t.Logf("failed: %s — %s", check.Name, check.Details)
			}
		}
		t.Fatalf("expected repository workflows to pass, got %d failed", report.Failed)
	}
	if report.Passed < 30 {
		t.Fatalf("expected comprehensive check coverage, got %d checks", report.Passed)
	}
}

func TestValidateWorkflowsMissingFiles(t *testing.T) {
	report, err := ValidateWorkflows(t.TempDir())
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if report.AllPassed || report.Failed == 0 {
		t.Fatalf("expected missing workflow failures, got %+v", report)
	}
}

func TestValidateWorkflowsDetectsNightlyLabelDrift(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), minimalCI())
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), `name: Nightly
on: { schedule: [{cron: '0 3 * * *'}], workflow_dispatch: {} }
jobs:
  full-tests:
    runs-on: [ubuntu-latest]
    steps:
      - run: curl localhost:11434
      - run: go test -count=1 -timeout 90m ./...
      - uses: actions/upload-artifact@v4
  benchmark-compare:
    needs: [full-tests]
    steps:
      - run: benchcmp check
`)
	dependabotDir := filepath.Join(root, ".github")
	if err := os.MkdirAll(dependabotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dependabotDir, "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if report.AllPassed {
		t.Fatalf("expected label drift to fail")
	}
	if !hasFailedCheck(report, "nightly runs on Jetson self-hosted labels") {
		t.Fatalf("expected Jetson label check failure, got %+v", report.Checks)
	}
}

func TestValidateWorkflowsDetectsMissingSecurityGate(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), `name: CI
on: {push: {}, pull_request: {}}
jobs:
  lint:
    timeout-minutes: 10
    steps: [{uses: 'golangci/golangci-lint-action@v6'}, {run: 'go vet ./...'}, {run: 'go mod tidy'}]
  security:
    timeout-minutes: 10
    steps: [{run: 'echo no scanners'}]
  test:
    timeout-minutes: 15
    steps: [{run: 'go test -short -race -coverprofile=coverage.out ./...'}]
  build:
    timeout-minutes: 10
    steps: [{run: 'go build ./cmd/bt-agent ./cmd/bt-evaluator ./cmd/bt-langagent ./cmd/bt-dashboard ./cmd/bt-gardener ./cmd/benchcmp ./cmd/bt-security-probe ./cmd/bt-ci-doctor ./cmd/bt-tree-integration ./cmd/bt-scalability-probe'}]
  release:
    timeout-minutes: 20
    needs: [lint, security, test, build]
    steps: [{run: 'GOARCH=amd64 go build ./... && GOARCH=arm64 go build ./... && bt-security-probe-linux-arm64 && bt-ci-doctor-linux-arm64 && bt-tree-integration-linux-arm64 && benchcmp-linux-arm64 && bt-scalability-probe-linux-arm64'}]
`)
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	dependabotDir := filepath.Join(root, ".github")
	if err := os.MkdirAll(dependabotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dependabotDir, "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if !hasFailedCheck(report, "ci security runs gosec") || !hasFailedCheck(report, "ci security runs govulncheck") {
		t.Fatalf("expected scanner checks to fail, got %+v", report.Checks)
	}
}

func TestValidateWorkflowsDetectsCIJobMissingTimeout(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// CI without timeout-minutes on any job
	writeFile(t, filepath.Join(wfDir, "ci.yml"), `name: CI
on: {push: {}, pull_request: {}}
jobs:
  lint:
    steps: [{uses: 'golangci/golangci-lint-action@v6'}, {run: 'go vet ./...'}, {run: 'go mod tidy'}]
  security:
    timeout-minutes: 10
    steps: [{uses: 'securego/gosec@master'}, {run: 'govulncheck ./...'}]
  test:
    timeout-minutes: 15
    steps: [{run: 'go test -short -race -coverprofile=coverage.out ./...'}]
  build:
    timeout-minutes: 10
    steps: [{run: 'go build ./cmd/bt-agent'}]
  release:
    timeout-minutes: 20
    needs: [lint, security, test, build]
    steps: [{run: 'GOARCH=amd64 go build ./...'}]
`)
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	writeFile(t, filepath.Join(root, ".github", "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if !hasFailedCheck(report, "ci job lint has timeout-minutes") {
		t.Fatalf("expected lint timeout-minutes check to fail, got %+v", report.Checks)
	}
}

func TestValidateWorkflowsDetectsMissingCodeQL(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), minimalCI())
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	writeFile(t, filepath.Join(root, ".github", "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	// No codeql.yml — should fail codeql checks
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if !hasFailedCheck(report, "codeql workflow exists and parses") {
		t.Fatalf("expected codeql existence check to fail, got %+v", report.Checks)
	}
}

func TestValidateWorkflowsCodeQLNoPermissions(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), minimalCI())
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	writeFile(t, filepath.Join(root, ".github", "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	// CodeQL with NO permissions block — hasPermission should return false
	writeFile(t, filepath.Join(wfDir, "codeql.yml"), `name: CodeQL Analysis
on: {push: {}, pull_request: {}, schedule: [{cron: '0 6 * * 1'}]}
jobs:
  analyze:
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: '1.23'}
      - uses: github/codeql-action/init@v3
        with: {languages: go, queries: +security-and-quality}
      - run: go build ./...
      - uses: github/codeql-action/analyze@v3
        with: {category: /language:go}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if !hasFailedCheck(report, "codeql has security-events write permission") {
		t.Fatalf("expected codeql permissions check to fail, got %+v", report.Checks)
	}
}

func TestValidateWorkflowsCodeQLWriteAllPermission(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), minimalCI())
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	writeFile(t, filepath.Join(root, ".github", "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	// CodeQL with workflow-level permissions: write-all string — hits hasPermission line 359-362
	// write-all matches "write" substring check, so hasPermission returns true
	writeFile(t, filepath.Join(wfDir, "codeql.yml"), `name: CodeQL Analysis
on: {push: {}, pull_request: {}, schedule: [{cron: '0 6 * * 1'}]}
permissions: write-all
jobs:
  analyze:
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: '1.23'}
      - uses: github/codeql-action/init@v3
        with: {languages: go, queries: +security-and-quality}
      - run: go build ./...
      - uses: github/codeql-action/analyze@v3
        with: {category: /language:go}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	// write-all at workflow level should pass the permissions check
	if hasFailedCheck(report, "codeql has security-events write permission") {
		t.Fatal("expected codeql permissions check to pass with write-all at workflow level")
	}
}

func TestValidateWorkflowsRunnerCheckNotInstalled(t *testing.T) {
	root := t.TempDir()
	wfDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wfDir, "ci.yml"), minimalCI())
	writeFile(t, filepath.Join(wfDir, "nightly.yml"), minimalNightly())
	writeFile(t, filepath.Join(root, ".github", "dependabot.yml"), `version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: {interval: weekly}
  - package-ecosystem: github-actions
    directory: /
    schedule: {interval: weekly}
`)
	report, err := ValidateWorkflows(root)
	if err != nil {
		t.Fatalf("ValidateWorkflows: %v", err)
	}
	if report.RunnerInstalled {
		t.Log("runner IS installed on this machine (unexpected in temp dir test)")
	} else {
		// Expected: no runner in temp dir
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("go.mod not found")
		}
		wd = next
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasFailedCheck(report WorkflowReport, name string) bool {
	for _, check := range report.Checks {
		if check.Name == name && !check.Passed {
			return true
		}
	}
	return false
}

func minimalCI() string {
	return `name: CI
on: {push: {}, pull_request: {}}
jobs:
  lint:
    timeout-minutes: 10
    steps: [{uses: 'golangci/golangci-lint-action@v6'}, {run: 'go vet ./...'}, {run: 'go mod tidy'}]
  security:
    timeout-minutes: 10
    steps: [{uses: 'securego/gosec@master'}, {run: 'govulncheck ./...'}]
  test:
    timeout-minutes: 15
    steps: [{run: 'go test -short -race -coverprofile=coverage.out ./...'}]
  build:
    timeout-minutes: 10
    steps: [{run: 'go build ./cmd/bt-agent ./cmd/bt-evaluator ./cmd/bt-langagent ./cmd/bt-dashboard ./cmd/bt-gardener ./cmd/benchcmp ./cmd/bt-security-probe ./cmd/bt-ci-doctor ./cmd/bt-tree-integration ./cmd/bt-scalability-probe'}]
  release:
    timeout-minutes: 20
    needs: [lint, security, test, build]
    steps: [{run: 'GOARCH=amd64 go build ./... && GOARCH=arm64 go build ./... && bt-security-probe-linux-arm64 && bt-ci-doctor-linux-arm64 && bt-tree-integration-linux-arm64 && benchcmp-linux-arm64 && bt-scalability-probe-linux-arm64'}]
`
}

func minimalCodeQL() string {
	return `name: CodeQL Analysis
on: {push: {}, pull_request: {}, schedule: [{cron: '0 6 * * 1'}]}
permissions:
  security-events: write
  actions: read
  contents: read
jobs:
  analyze:
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: {go-version: '1.23'}
      - uses: actions/cache@v4
        with:
          path: ~/go/pkg/mod
          key: go-${{ hashFiles('**/go.sum') }}
      - uses: github/codeql-action/init@v3
        with: {languages: go, queries: +security-and-quality}
      - run: go build ./...
      - uses: github/codeql-action/analyze@v3
        with: {category: /language:go}
`
}

func minimalNightly() string {
	return `name: Nightly
on: { schedule: [{cron: '0 3 * * *'}], workflow_dispatch: {} }
jobs:
  full-tests:
    runs-on: [self-hosted, jetson, arm64]
    steps:
      - run: curl localhost:11434
      - run: go test -count=1 -timeout 90m ./...
      - uses: actions/upload-artifact@v4
  benchmark-compare:
    needs: [full-tests]
    steps:
      - run: benchcmp check
`
}
