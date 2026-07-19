// Package engine — arc42 documentation generation nodes.
//
// Registers 18 actions and 4 conditions for the arc42 documentation
// generator trees defined in internal/domains/arc42_trees.go. The monolith
// assembly nodes (SaveDocument, CollectAllSections, GenerateTOC,
// MarkDocAssembled, AllSectionsDone) were retired with the arc42:assemble
// tree when the per-section files became the source of truth.
// All nodes use the global registry (RegisterAction/RegisterCondition)
// so domain trees can reference them by name.
package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// execWithTimeout runs a command with a 5-second timeout to prevent hangs.
func execWithTimeout(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func init() {
	registerArc42Nodes()
}

// arc42OutputDir is the writable directory for arc42 doc assembly (override via BT_ARC42_OUTPUT_DIR).
func arc42OutputDir() string {
	if d := os.Getenv("BT_ARC42_OUTPUT_DIR"); d != "" {
		return d
	}
	return filepath.Join(goModuleRoot(), "testdata", "arc42")
}

func registerArc42Nodes() {
	// ─── Data Gathering Actions ──────────────────────────────────────────

	RegisterAction("ReadGraphReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		data, err := os.ReadFile(goapFusionGraphReport)
		if err != nil {
			bb.CachedResult = fmt.Sprintf("graphify not available: %v", err)
			return 1 // non-fatal: use fallback
		}
		bb.CachedResult = sectionAwareGraphContext(string(data))
		return 1
	})

	RegisterAction("ReadGitHistory", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, err := execWithTimeout("git", "log", "--oneline", "-30")
		if err != nil {
			setChainState(bb, "git_history", fmt.Sprintf("git unavailable: %v", err))
			return 1
		}
		setChainState(bb, "git_history", string(out))
		return 1
	})

	RegisterAction("ReadADRs", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		// The ADR log lives in the arc42 decisions section since the docs/adr
		// directory was folded into it (ADR-131..133 carry the old numbers).
		data, err := os.ReadFile("docs/arc42/09-decisions.md")
		if err != nil {
			setChainState(bb, "adrs", fmt.Sprintf("ADR log unavailable: %v", err))
			return 1
		}
		setChainState(bb, "adrs", string(data))
		return 1
	})

	RegisterAction("ReadGoMod", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		data, err := os.ReadFile("go.mod")
		if err != nil {
			setChainState(bb, "go_version", "unknown")
			return 1
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "go ") {
				setChainState(bb, "go_version", strings.TrimSpace(line))
				break
			}
		}
		setChainState(bb, "go_mod", string(data))
		return 1
	})

	RegisterAction("ReadConfigFiles", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		data, _ := os.ReadFile("config.yaml")
		setChainState(bb, "config", string(data))
		return 1
	})

	RegisterAction("DetectHardware", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var parts []string
		if cpu, _ := os.ReadFile("/proc/cpuinfo"); cpu != nil {
			model := "unknown"
			for _, line := range strings.Split(string(cpu), "\n") {
				if strings.Contains(line, "model name") || strings.Contains(line, "Model") {
					model = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				}
			}
			parts = append(parts, fmt.Sprintf("CPU: %s (%d cores)", model, countCPUCores(string(cpu))))
		}
		if mem, _ := os.ReadFile("/proc/meminfo"); mem != nil {
			for _, line := range strings.Split(string(mem), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					parts = append(parts, fmt.Sprintf("Memory: %s", strings.TrimSpace(line)))
					break
				}
			}
		}
		df, _ := execWithTimeout("df", "-h", "/")
		if df != nil {
			parts = append(parts, fmt.Sprintf("Disk: %s", strings.TrimSpace(string(df))))
		}
		uname, _ := execWithTimeout("uname", "-a")
		if uname != nil {
			parts = append(parts, fmt.Sprintf("Kernel: %s", strings.TrimSpace(string(uname))))
		}
		setChainState(bb, "hardware", strings.Join(parts, "\n"))
		return 1
	})

	RegisterAction("DetectProcesses", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", "ps aux | grep -E 'bt-|hermes' | grep -v grep")
		setChainState(bb, "processes", string(out))
		return 1
	})

	RegisterAction("ListPackages", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		entries, _ := os.ReadDir("internal")
		var pkgs []string
		for _, e := range entries {
			if e.IsDir() {
				pkgs = append(pkgs, "internal/"+e.Name())
			}
		}
		setChainState(bb, "packages", strings.Join(pkgs, "\n"))
		return 1
	})

	RegisterAction("ListBinaries", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		files, _ := filepath.Glob("cmd/*/main.go")
		bins := make([]string, 0, len(files))
		for _, f := range files {
			bins = append(bins, filepath.Dir(f))
		}
		setChainState(bb, "binaries", strings.Join(bins, "\n"))
		return 1
	})

	RegisterAction("ListExternalAPIs", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", `grep -rn 'http\.NewRequest\|http\.Get\|http\.Post\|http\.Client\|net\.Dial\|jsonrpc' internal/ cmd/ --include='*.go' | head -30`)
		setChainState(bb, "external_apis", string(out))
		return 1
	})

	RegisterAction("ListMCPTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", `grep -rn 'RegisterTool\|AddTool\|tools.Register' internal/mcp/ internal/a2a/ --include='*.go' | wc -l`)
		setChainState(bb, "mcp_tools", fmt.Sprintf("Registered MCP tools: %s", strings.TrimSpace(string(out))))
		return 1
	})

	RegisterAction("ScanCodeComments", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", `grep -rn '^// Package ' internal/ --include='*.go' | head -40`)
		setChainState(bb, "comments", string(out))
		return 1
	})

	RegisterAction("ScanTypes", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", `grep -rn '^type [A-Z]' internal/ --include='*.go' | grep -v '_test.go' | head -50`)
		setChainState(bb, "types", string(out))
		return 1
	})

	RegisterAction("ReadEngineCode", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		data, err := os.ReadFile("internal/engine/tree.go")
		if err != nil {
			bb.CachedResult = fmt.Sprintf("engine code unavailable: %v", err)
			return 1
		}
		lines := strings.Split(string(data), "\n")
		end := 150
		if len(lines) < end {
			end = len(lines)
		}
		bb.CachedResult = strings.Join(lines[:end], "\n")
		return 1
	})

	RegisterAction("ReadSection1", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		data, err := os.ReadFile(filepath.Join(arc42OutputDir(), "01-introduction-goals.md"))
		if err != nil {
			bb.CachedResult = "section 1 not yet generated"
			return 1
		}
		bb.CachedResult = string(data)
		return 1
	})

	RegisterAction("ReadTestCoverage", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if strings.HasSuffix(os.Args[0], ".test") {
			setChainState(bb, "coverage", "coverage skipped: running inside go test to avoid recursive test execution")
			return 1
		}
		out, _ := execWithTimeout("bash", "-c", `go test ./... -coverprofile=/tmp/arc42_cover.out -count=1 -timeout 60s 2>&1 | tail -10`)
		setChainState(bb, "coverage", string(out))
		return 1
	})

	RegisterAction("ReadErrorLogs", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out, _ := execWithTimeout("bash", "-c", `tail -30 ~/.hermes/logs/errors.log 2>/dev/null || echo "no error log found"`)
		setChainState(bb, "errors", string(out))
		return 1
	})

	// ─── Validation & Persistence Actions ────────────────────────────────

	RegisterAction("SetupDocTools", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.ChainTools = []any{
			newFileReadTool(),
			newFileWriteTool(),
			newShellExecTool(),
		}
		return 1
	})

	RegisterAction("ValidateSection", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if len(bb.Result) < 100 {
			bb.Outcome = fmt.Sprintf("validation_failed: section too short (%d chars)", len(bb.Result))
			return 0 // fail
		}
		if strings.Contains(bb.Result, "<insert") || strings.Contains(bb.Result, "TODO") {
			bb.Outcome = "validation_warning: contains placeholder text"
			return 1 // pass with warning
		}
		bb.Outcome = "validation_passed"
		return 1
	})

	RegisterAction("SaveSection", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		filename, ok := bb.ChainState["arc42_section_file"].(string)
		if !ok || filename == "" {
			bb.Outcome = "save_failed: no filename in chain state"
			return 0
		}
		dir := arc42OutputDir()
		_ = os.MkdirAll(dir, 0755)
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(bb.Result), 0644); err != nil {
			bb.Outcome = fmt.Sprintf("save_failed: %v", err)
			return 0
		}
		bb.Outcome = fmt.Sprintf("saved: %s (%d bytes)", filename, len(bb.Result))
		return 1
	})

	RegisterAction("MarkSectionDone", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		section, ok := bb.ChainState["arc42_section"].(string)
		if ok {
			setChainState(bb, "section_"+section+"_done", true)
		}
		return 1
	})

	// ─── Conditions ──────────────────────────────────────────────────────

	RegisterCondition("GraphIsFresh", func(bb *Blackboard) bool {
		if getBoolChainState(bb, "graph_fresh") {
			return true
		}
		data, err := os.ReadFile(goapFusionGraphReport)
		if err != nil {
			return false
		}
		builtSHA := graphReportBuiltCommit(string(data))
		if builtSHA == "" {
			return false
		}
		headOut, err := runGoapGit(goapFusionRepo, 5*time.Second, "rev-parse", "HEAD")
		if err != nil {
			return false
		}
		return strings.HasPrefix(strings.TrimSpace(headOut), builtSHA)
	})

	RegisterCondition("Section1Done", func(bb *Blackboard) bool {
		return getBoolChainState(bb, "section_1_done") || sectionFileExists("01-introduction-goals.md")
	})

	RegisterCondition("Section4Done", func(bb *Blackboard) bool {
		return getBoolChainState(bb, "section_4_done") || sectionFileExists("04-solution-strategy.md")
	})

	RegisterCondition("Section5Done", func(bb *Blackboard) bool {
		return getBoolChainState(bb, "section_5_done") || sectionFileExists("05-building-blocks.md")
	})

}

// ─── Helpers ────────────────────────────────────────────────────────────────

func setChainState(bb *Blackboard, key string, val interface{}) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState[key] = val
}

func getBoolChainState(bb *Blackboard, key string) bool {
	if bb.ChainState == nil {
		return false
	}
	v, ok := bb.ChainState[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func sectionFileExists(filename string) bool {
	_, err := os.Stat(filepath.Join(arc42OutputDir(), filename))
	return err == nil
}

func countCPUCores(cpuinfo string) int {
	count := 0
	for _, line := range strings.Split(cpuinfo, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "processor") {
			count++
		}
	}
	if count == 0 {
		count = 1
	}
	return count
}
