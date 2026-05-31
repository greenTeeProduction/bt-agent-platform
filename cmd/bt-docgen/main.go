// Command bt-docgen generates complete arc42 architecture documentation
// for the go-bt-evolve platform using GOAP planning to sequence the 12
// arc42 sections and specialized Behavior Trees to populate each section
// with codebase-introspective content.
//
// Usage:
//
//	# Full regeneration (all 12 sections + assembly)
//	cd /home/nico/go-bt-evolve && go run ./cmd/bt-docgen/
//
//	# Incremental: only regenerate sections whose source data changed
//	go run ./cmd/bt-docgen/ --incremental
//
//	# Target specific sections (comma-separated, 1-12)
//	go run ./cmd/bt-docgen/ --section=1,5,9
//
// Output: /mnt/ssd/clawd/wiki/bt-research/docs/arc42/
//
// The tool requires:
//   - graphify installed and accessible
//   - go-bt-evolve as working directory
//   - Optional: Ollama running for LLM-generated content
//     (falls back to template data if unavailable)
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/llm"
)

var (
	incremental = flag.Bool("incremental", false, "Only regenerate sections whose sources changed")
	sections    = flag.String("section", "", "Comma-separated section numbers to regenerate (1-12)")
)

// docgenState tracks the last document generation for incremental mode.
type docgenState struct {
	LastRun     string            `json:"last_run"`     // ISO timestamp
	GraphHash   string            `json:"graph_hash"`   // sha256 of GRAPH_REPORT.md
	SourceHash  map[string]string `json:"source_hash"`  // section → sha256 of source files
	SectionHash map[string]string `json:"section_hash"` // section → sha256 of generated output
}

func (ds docgenState) isStale(section int) bool {
	// If no source hash recorded, consider stale
	if ds.SourceHash == nil || ds.SectionHash == nil {
		return true
	}
	key := fmt.Sprintf("%d", section)
	srcHash, ok := ds.SourceHash[key]
	if !ok {
		return true
	}
	currentHash := hashSectionSources(section, nil)
	return srcHash != currentHash
}

func (ds docgenState) isGraphStale() bool {
	currentHash := fileHash("graphify-out/GRAPH_REPORT.md")
	return ds.GraphHash != currentHash
}

// sectionSourceFiles maps each arc42 section to the files it depends on.
var sectionSourceFiles = map[int][]string{
	1: {"graphify-out/GRAPH_REPORT.md", "docs/adr/INDEX.md"},
	2: {"go.mod", "config.yaml"},
	3: {"internal/mcp/server.go", "internal/a2a/server.go"},
	4: {"docs/adr/ADR-001-behavior-trees.md", "docs/adr/ADR-002-mcp-interface.md", "docs/adr/ADR-003-file-persistence.md"},
	5: {"internal/engine/tree.go", "internal/engine/chains.go", "internal/engine/registry.go",
		"internal/evolution/mutate.go", "internal/evolution/stockfish.go"},
	6: {"internal/engine/tree.go", "internal/gardener/gardener.go", "internal/reliability/panic_handler.go"},
	7: {"cmd/bt-dashboard/main.go", "cmd/bt-agent/main.go"},
	8: {"internal/engine/chains.go", "internal/mcp/server.go", "internal/reliability/panic_handler.go",
		"internal/evolution/mutate.go"},
	9:  {"docs/adr/ADR-001-behavior-trees.md", "docs/adr/ADR-002-mcp-interface.md", "docs/adr/ADR-003-file-persistence.md"},
	10: {"internal/reliability/panic_handler.go", "internal/security/security.go", "internal/engine/validate.go"},
	11: {"graphify-out/GRAPH_REPORT.md", "internal/evolution/mutate.go"},
	12: {"internal/engine/tree.go", "internal/engine/chains.go", "internal/domains/trees.go",
		"internal/evolution/stockfish.go", "internal/evolution/multi_objective.go"},
}

func main() {
	flag.Parse()

	fmt.Println("=== bt-docgen: Arc42 Documentation Generator ===")
	fmt.Println()

	outputDir := "/mnt/ssd/clawd/wiki/bt-research/docs/arc42"
	os.MkdirAll(outputDir, 0755)

	statePath := filepath.Join(outputDir, ".docgen-state.json")

	// Determine which sections to generate
	var targetSections []int
	if *sections != "" {
		targetSections = parseSections(*sections)
		fmt.Printf("Targeting sections: %v\n", targetSections)
	} else {
		targetSections = allSections()
	}

	// Load previous state for incremental mode
	var state docgenState
	if data, err := os.ReadFile(statePath); err == nil {
		json.Unmarshal(data, &state)
	}

	if *incremental && *sections == "" {
		// Filter to only stale sections
		var stale []int
		for _, s := range targetSections {
			if state.isStale(s) {
				stale = append(stale, s)
			}
		}
		if len(stale) == 0 && !state.isGraphStale() {
			fmt.Println("No sections need updating. All up to date.")
			fmt.Printf("Last run: %s\n", state.LastRun)
			return
		}
		fmt.Printf("Stale sections: %v (graph stale: %v)\n", stale, state.isGraphStale())
		targetSections = stale
		if state.isGraphStale() {
			// Graph changed → sections 1, 4, 5, 11 that depend on it become stale
			for _, s := range []int{1, 4, 5, 11} {
				if !contains(targetSections, s) {
					targetSections = append(targetSections, s)
				}
			}
			sort.Ints(targetSections)
		}
	}

	if len(targetSections) == 0 {
		fmt.Println("No sections to generate.")
		return
	}

	// Step 1: Update graphify
	fmt.Println("[1/3] Updating graphify...")
	cmd := exec.Command("graphify", "update", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Warning: graphify update failed: %v (continuing)\n", err)
	}
	// Record graph hash after update
	state.GraphHash = fileHash("graphify-out/GRAPH_REPORT.md")

	// Initialize Ollama client for LLM-generated content
	fmt.Println("\n[2/3] Connecting to Ollama...")
	llmCfg := llm.DefaultConfig()
	llmClient, err := llm.NewClient(llmCfg)
	if err != nil {
		fmt.Printf("Warning: Ollama unavailable (%v) — output will be template-only\n", err)
		llmClient = nil
	} else {
		fmt.Printf("  Using %s @ %s\n", llmCfg.Model, llmCfg.ServerURL)
	}

	// Load arc42 trees and build section mapping
	allTrees := domains.Arc42Trees()
	sectionMap := buildSectionMap()

	// Step 3: Execute targeted sections
	fmt.Println("\n[3/3] Generating sections...")
	worldState := goap.DocPlannerWorldState{GraphFresh: true}

	// Check which sections already exist on disk
	for i := 1; i <= 12; i++ {
		filename := sectionMap[goap.SectionMappings[i-1].ActionName].Filename
		if _, err := os.Stat(filepath.Join(outputDir, filename)); err == nil {
			// Mark as done so dependencies don't block
			setSectionDone(&worldState, i)
		}
	}

	successCount := 0
	for _, sm := range goap.SectionMappings {
		if !contains(targetSections, sm.Number) {
			fmt.Printf("  [skip] Section %d — not in target list\n", sm.Number)
			continue
		}

		fmt.Printf("\n  Section %d/%d: %s\n", sm.Number, 12, sm.TreeID)

		// Check dependencies
		for _, dep := range sm.DependsOn {
			if !isSectionDone(worldState, dep) {
				fmt.Printf("    ⚠ Dependency section %d not done — skipping\n", dep)
				continue
			}
		}

		tree, ok := allTrees[sm.TreeID]
		if !ok {
			fmt.Printf("    ❌ Tree not found: %s\n", sm.TreeID)
			continue
		}

		bb := engine.Blackboard{
			Task:       fmt.Sprintf("Generate arc42 Section %d for go-bt-evolve", sm.Number),
			ChainState: map[string]any{},
			LLM:        llmClient,
		}
		setChainState(&bb, "arc42_section", fmt.Sprintf("%d", sm.Number))
		setChainState(&bb, "arc42_section_file", sm.Filename)

		// Pass dependency state
		ws := worldState.ToWorldState()
		for k, v := range ws {
			bb.ChainState[k] = v
		}

		cmd := engine.BuildTree(tree, &bb)
		result := engine.RunTask(&bb, cmd)
		if result != "" {
			bb.Result = result
		}

		if bb.Outcome != "" {
			fmt.Printf("    Outcome: %s\n", bb.Outcome)
		}

		if bb.Result != "" {
			path := filepath.Join(outputDir, sm.Filename)
			os.WriteFile(path, []byte(bb.Result), 0644)
			fmt.Printf("    ✅ Saved: %s (%d bytes)\n", sm.Filename, len(bb.Result))

			// Record source hash for incremental tracking
			if state.SourceHash == nil {
				state.SourceHash = make(map[string]string)
			}
			if state.SectionHash == nil {
				state.SectionHash = make(map[string]string)
			}
			key := fmt.Sprintf("%d", sm.Number)
			state.SourceHash[key] = hashSectionSources(sm.Number, nil)
			state.SectionHash[key] = fmt.Sprintf("%x", sha256.Sum256([]byte(bb.Result)))
		}

		setSectionDone(&worldState, sm.Number)
		successCount++
	}

	// Assemble if all sections exist
	if contains(targetSections, 99) || *sections == "" { // 99 = "assemble" pseudo-section
		fmt.Println("\n  Assembling final document...")
		assembleTree, ok := allTrees["arc42:assemble"]
		if ok {
			bb := engine.Blackboard{
				Task:       "Merge all arc42 sections into final document",
				ChainState: map[string]any{},
				LLM:        llmClient,
			}
			for k, v := range worldState.ToWorldState() {
				bb.ChainState[k] = v
			}
			cmd := engine.BuildTree(assembleTree, &bb)
			result := engine.RunTask(&bb, cmd)
			if result != "" {
				bb.Result = result
			}
			if bb.Result != "" {
				path := filepath.Join(outputDir, "go-bt-evolve-arc42.md")
				os.WriteFile(path, []byte(bb.Result), 0644)
				fmt.Printf("    ✅ Final document: %s (%d bytes)\n", path, len(bb.Result))
			}
			successCount++
		}
	}

	// Save state
	state.LastRun = time.Now().Format(time.RFC3339)
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(statePath, data, 0644)

	// Summary
	fmt.Println()
	fmt.Printf("=== Complete: %d sections generated ===\n", successCount)
	fmt.Printf("Output: %s/\n", outputDir)

	files, _ := filepath.Glob(filepath.Join(outputDir, "*.md"))
	fmt.Printf("Files: %d markdown files\n", len(files))
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func allSections() []int {
	s := make([]int, 12)
	for i := range s {
		s[i] = i + 1
	}
	return s
}

func parseSections(s string) []int {
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 || n > 12 {
			fmt.Printf("Warning: invalid section number %q, skipping\n", p)
			continue
		}
		result = append(result, n)
	}
	sort.Ints(result)
	return result
}

func buildSectionMap() map[string]goap.SectionMapping {
	m := make(map[string]goap.SectionMapping)
	for _, sm := range goap.SectionMappings {
		m[sm.ActionName] = sm
	}
	return m
}

func setChainState(bb *engine.Blackboard, key string, val interface{}) {
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState[key] = val
}

func setSectionDone(ws *goap.DocPlannerWorldState, section int) {
	switch section {
	case 1:
		ws.Section1Done = true
	case 2:
		ws.Section2Done = true
	case 3:
		ws.Section3Done = true
	case 4:
		ws.Section4Done = true
	case 5:
		ws.Section5Done = true
	case 6:
		ws.Section6Done = true
	case 7:
		ws.Section7Done = true
	case 8:
		ws.Section8Done = true
	case 9:
		ws.Section9Done = true
	case 10:
		ws.Section10Done = true
	case 11:
		ws.Section11Done = true
	case 12:
		ws.Section12Done = true
	}
}

func isSectionDone(ws goap.DocPlannerWorldState, section int) bool {
	switch section {
	case 1:
		return ws.Section1Done
	case 2:
		return ws.Section2Done
	case 3:
		return ws.Section3Done
	case 4:
		return ws.Section4Done
	case 5:
		return ws.Section5Done
	case 6:
		return ws.Section6Done
	case 7:
		return ws.Section7Done
	case 8:
		return ws.Section8Done
	case 9:
		return ws.Section9Done
	case 10:
		return ws.Section10Done
	case 11:
		return ws.Section11Done
	case 12:
		return ws.Section12Done
	}
	return false
}

func contains(slice []int, n int) bool {
	for _, v := range slice {
		if v == n {
			return true
		}
	}
	return false
}

// fileHash returns the SHA-256 hash of a file (empty string if missing).
func fileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// hashSectionSources computes a combined hash of all source files a section depends on.
func hashSectionSources(section int, _ interface{}) string {
	files, ok := sectionSourceFiles[section]
	if !ok {
		return fmt.Sprintf("section-%d-no-sources", section)
	}
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			h.Write([]byte(f + ":missing"))
			continue
		}
		h.Write([]byte(f + ":"))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
