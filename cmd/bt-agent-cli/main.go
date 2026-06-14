package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/agentexec"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	reg, err := agent.NewRegistry(agent.RegistryDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "create":
		cmdCreate(reg)
	case "list":
		cmdList(reg)
	case "run":
		cmdRun(reg)
	case "test":
		cmdTest(reg)
	case "schedule":
		cmdSchedule(reg)
	case "logs":
		cmdLogs(reg)
	case "delete":
		cmdDelete(reg)
	case "templates":
		cmdTemplates()
	case "install-templates":
		cmdInstallTemplates()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`bt-agent-cli — Manage BT agents

Usage:
  bt-agent-cli create <name> --tree <tree-id> [--desc <description>]
  bt-agent-cli create --from-template <template-name>
  bt-agent-cli list
  bt-agent-cli run <name> --input <text> [--param key=value ...] [--json]
  bt-agent-cli test <name>
  bt-agent-cli schedule <name> --every <cron-expr> [--timeout 2h]
  bt-agent-cli logs <name>
  bt-agent-cli delete <name>
  bt-agent-cli templates
  bt-agent-cli install-templates

Templates: code-reviewer, daily-researcher, system-monitor, meeting-summarizer, data-pipeline, notification-router`)
}

func cmdCreate(reg *agent.Registry) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	tree := fs.String("tree", "", "Tree ID (e.g., domain:code_review)")
	desc := fs.String("desc", "", "Description")
	tmpl := fs.String("from-template", "", "Create from template")
	_ = fs.Parse(os.Args[2:])

	if *tmpl != "" {
		cat := agent.NewCatalog(reg, agent.TemplatesDir())
		inst, err := cat.InstallFromTemplate(*tmpl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "Run 'bt-agent-cli templates' or copy repo agents/templates to %s\n", agent.TemplatesDir())
			os.Exit(1)
		}
		fmt.Printf("Created agent: %s (id=%s, tree=%s)\n", inst.Definition.Name, inst.ID, inst.Definition.Tree)
		return
	}

	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: agent name required")
		os.Exit(1)
	}
	if *tree == "" {
		fmt.Fprintln(os.Stderr, "Error: --tree required")
		os.Exit(1)
	}

	def := agent.Definition{
		Name:        name,
		Description: *desc,
		Tree:        *tree,
		Schedule:    "on_demand",
	}

	inst, err := reg.Create(def)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created agent: %s (id=%s, tree=%s)\n", inst.Definition.Name, inst.ID, inst.Definition.Tree)
}

func cmdList(reg *agent.Registry) {
	agents := reg.List()
	if len(agents) == 0 {
		fmt.Println("No agents registered. Create one with: bt-agent-cli create <name> --tree <tree-id>")
		return
	}
	fmt.Printf("%-25s %-10s %-30s %s\n", "NAME", "STATE", "TREE", "SCHEDULE")
	for _, a := range agents {
		fmt.Printf("%-25s %-10s %-30s %s\n", a.Definition.Name, a.State, a.Definition.Tree, a.Definition.Schedule)
	}
}

func cmdRun(reg *agent.Registry) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	input := fs.String("input", "", "Task input text")
	asJSON := fs.Bool("json", false, "Output result as JSON")
	var params []string
	fs.Func("param", "Named input (key=value); repeatable", func(s string) error {
		params = append(params, s)
		return nil
	})
	_ = fs.Parse(os.Args[2:])

	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: agent name required")
		os.Exit(1)
	}

	inst, err := reg.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	inputValues, err := agent.ParseInputParams(params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if *input != "" {
		if len(inst.Definition.Inputs) == 1 && inst.Definition.Inputs[0].Name != "task" {
			inputValues[inst.Definition.Inputs[0].Name] = *input
		} else {
			inputValues["task"] = *input
		}
	}

	runner, err := agentexec.NewRunDeps()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot start agent runner: %v\n", err)
		fmt.Fprintln(os.Stderr, "Ensure LLM provider is configured (Ollama running or API keys set).")
		os.Exit(1)
	}

	task := strings.TrimSpace(*input)
	if task == "" {
		task = inst.Definition.Description
	}
	if task == "" {
		task = name
	}

	fmt.Fprintf(os.Stderr, "Running agent %q (tree=%s)...\n", name, inst.Definition.Tree)
	res, runErr := runner.RunOnce(context.Background(), name, task, agent.RunOptions{
		InjectMemory:   true,
		EnforceQuality: true,
		RecordHistory:  true,
		InputValues:    inputValues,
	})
	if res == nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", runErr)
		os.Exit(1)
	}

	if *asJSON {
		out := map[string]interface{}{
			"agent":          name,
			"tree":           res.TreeID,
			"outcome":        res.Outcome,
			"result":         res.Output,
			"quality":        res.Quality,
			"quality_passed": res.QualityPassed,
			"output_passed":  res.OutputPassed,
			"duration":       res.Duration.String(),
		}
		if len(res.QualityReasons) > 0 {
			out["quality_reasons"] = res.QualityReasons
		}
		if len(res.OutputReasons) > 0 {
			out["output_reasons"] = res.OutputReasons
		}
		if runErr != nil {
			out["error"] = runErr.Error()
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	} else {
		fmt.Printf("Outcome: %s (quality=%.2f, duration=%s)\n", res.Outcome, res.Quality, res.Duration.Truncate(time.Second))
		if len(res.QualityReasons) > 0 {
			fmt.Printf("Quality: %s\n", strings.Join(res.QualityReasons, "; "))
		}
		if len(res.OutputReasons) > 0 {
			fmt.Printf("Output: %s\n", strings.Join(res.OutputReasons, "; "))
		}
		fmt.Println("\n--- Output ---")
		fmt.Println(res.Output)
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "\nError: %v\n", runErr)
			os.Exit(1)
		}
	}
	if res.Outcome != "success" {
		os.Exit(1)
	}
}

func cmdTest(reg *agent.Registry) {
	name := os.Args[2]
	inst, err := reg.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Testing agent %q (tree=%s)...\n", name, inst.Definition.Tree)
	fmt.Println("Quality gates:", inst.Definition.Quality)
	fmt.Println("Test command: go test -v ./internal/agent/ -run TestAllTemplatesResolveTree")
	_ = inst
}

func cmdSchedule(reg *agent.Registry) {
	fs := flag.NewFlagSet("schedule", flag.ExitOnError)
	every := fs.String("every", "", "Cron expression (e.g., '0 9 * * *' or 'every 1h')")
	timeout := fs.String("timeout", "2h", "Max run duration (e.g., 30m, 2h)")
	_ = fs.Parse(os.Args[2:])

	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: agent name required")
		os.Exit(1)
	}
	if *every == "" {
		fmt.Fprintln(os.Stderr, "Error: --every required")
		os.Exit(1)
	}

	job, err := agent.ApplySchedule(reg, name, *every, *timeout, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Updated schedule for %q to %s\n", name, *every)
	fmt.Printf("Agent YAML: %s/%s.yaml\n", agent.RegistryDir(), name)
	fmt.Printf("Scheduler job: %s (next run ~%s)\n", job.ID, job.NextRun.Format(time.RFC3339))
	if *every != "on_demand" {
		fmt.Println("Running bt-agent will pick this up on the next scheduler tick (~1m).")
	}
}

func cmdLogs(reg *agent.Registry) {
	name := os.Args[2]
	inst, err := reg.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	logPath := filepath.Join(agent.LogsDir(), "bt.log")
	fmt.Printf("Agent %q logs from: %s\n", name, logPath)
	fmt.Printf("State: %s | Runs: %d | Success: %.0f%%\n", inst.State, inst.RunCount, inst.SuccessRate*100)
}

func cmdDelete(reg *agent.Registry) {
	name := os.Args[2]
	if err := agent.DeleteRegisteredAgent(reg, name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted agent: %s\n", name)
}

func cmdTemplates() {
	cat := agent.NewCatalog(nil, agent.TemplatesDir())
	entries, err := cat.ListTemplates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot list templates: %v\n", err)
		fmt.Fprintf(os.Stderr, "Expected directory: %s\n", agent.TemplatesDir())
		os.Exit(1)
	}
	if len(entries) == 0 {
		fmt.Printf("No templates in %s\n", agent.TemplatesDir())
		fmt.Println("Run: bt-agent-cli install-templates  (from repo root)")
		return
	}

	fmt.Println("Available agent templates:")
	for _, e := range entries {
		fmt.Printf("  %s\n", e.Name)
	}
	fmt.Println("\nCreate from template: bt-agent-cli create --from-template <name>")
}

func cmdInstallTemplates() {
	src := filepath.Join("agents", "templates")
	if _, err := os.Stat(src); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot find repo templates at %s (run from repo root)\n", src)
		os.Exit(1)
	}
	dst := agent.TemplatesDir()
	if err := os.MkdirAll(dst, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		n++
	}
	wfSrc := filepath.Join("agents", "workflows")
	wfDst := agent.WorkflowsDir()
	_ = os.MkdirAll(wfDst, 0755)
	if wfEntries, err := os.ReadDir(wfSrc); err == nil {
		for _, e := range wfEntries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(wfSrc, e.Name()))
			_ = os.WriteFile(filepath.Join(wfDst, e.Name()), data, 0644)
		}
	}
	fmt.Printf("Installed %d templates to %s\n", n, dst)
}
