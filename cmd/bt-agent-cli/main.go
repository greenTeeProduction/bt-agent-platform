// bt-agent-cli manages behavior tree agents — create, list, run, test, and schedule.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/agent"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	home, _ := os.UserHomeDir()
	agentDir := filepath.Join(home, ".go-bt-evolve", "agents")
	reg, err := agent.NewRegistry(agentDir)
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
  bt-agent-cli run <name> --input <text>
  bt-agent-cli test <name>
  bt-agent-cli schedule <name> --every <cron-expr>
  bt-agent-cli logs <name>
  bt-agent-cli delete <name>
  bt-agent-cli templates

Templates: code-reviewer, daily-researcher, system-monitor, meeting-summarizer, data-pipeline, notification-router`)
}

func cmdCreate(reg *agent.Registry) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	tree := fs.String("tree", "", "Tree ID (e.g., domain:code_review)")
	desc := fs.String("desc", "", "Description")
	tmpl := fs.String("from-template", "", "Create from template")
	fs.Parse(os.Args[2:])

	var def agent.Definition

	if *tmpl != "" {
		tmplPath := filepath.Join(os.Getenv("HOME"), "go-bt-evolve", "agents", "templates", *tmpl+".yaml")
		data, err := os.ReadFile(tmplPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Template %q not found: %v\n", *tmpl, err)
			fmt.Fprintf(os.Stderr, "Run 'bt-agent-cli templates' to see available templates\n")
			os.Exit(1)
		}
		// Simple YAML parser for templates — use the agent package to parse
		reg2, _ := agent.NewRegistry(filepath.Dir(tmplPath))
		inst, err := reg2.Get(*tmpl)
		if err != nil {
			_ = data // template loaded but not parsed via registry — create manually
			def = agent.Definition{
				Name:        *tmpl,
				Description: fmt.Sprintf("Created from template: %s", *tmpl),
				Tree:        "domain:default",
			}
		} else {
			def = inst.Definition
			def.Name = *tmpl
		}
	} else {
		name := fs.Arg(0)
		if name == "" {
			fmt.Fprintln(os.Stderr, "Error: agent name required")
			os.Exit(1)
		}
		if *tree == "" {
			fmt.Fprintln(os.Stderr, "Error: --tree required")
			os.Exit(1)
		}
		def = agent.Definition{
			Name:        name,
			Description: *desc,
			Tree:        *tree,
			Schedule:    "on_demand",
		}
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
	fs.Parse(os.Args[2:])

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

	fmt.Printf("Running agent %q (tree=%s)...\n", name, inst.Definition.Tree)
	fmt.Printf("Input: %s\n", *input)
	fmt.Println("\n--- Use bt-agent MCP to execute: bt_delegate_to_tree ---")
	fmt.Printf("  tree: %s\n  task: %s\n", inst.Definition.Tree, *input)
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
	fmt.Println("Test command: go test -v -run TestAgent ./internal/agent/")
	_ = inst
}

func cmdSchedule(reg *agent.Registry) {
	fs := flag.NewFlagSet("schedule", flag.ExitOnError)
	every := fs.String("every", "", "Cron expression (e.g., '0 9 * * *' or 'every 1h')")
	fs.Parse(os.Args[2:])

	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(os.Stderr, "Error: agent name required")
		os.Exit(1)
	}
	if *every == "" {
		fmt.Fprintln(os.Stderr, "Error: --every required")
		os.Exit(1)
	}

	fmt.Printf("Scheduling agent %q: %s\n", name, *every)
	fmt.Printf("Use: hermes cron create --name 'bt-agent-%s' --schedule '%s' --prompt '<task>'\n", name, *every)
}

func cmdLogs(reg *agent.Registry) {
	name := os.Args[2]
	inst, err := reg.Get(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	logPath := filepath.Join(os.Getenv("HOME"), ".go-bt-evolve", "logs", "bt.log")
	fmt.Printf("Agent %q logs from: %s\n", name, logPath)
	fmt.Printf("State: %s | Runs: %d | Success: %.0f%%\n", inst.State, inst.RunCount, inst.SuccessRate*100)
}

func cmdDelete(reg *agent.Registry) {
	name := os.Args[2]
	if err := reg.Delete(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted agent: %s\n", name)
}

func cmdTemplates() {
	tmplDir := filepath.Join(os.Getenv("HOME"), "go-bt-evolve", "agents", "templates")
	entries, err := os.ReadDir(tmplDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot list templates: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Available agent templates:")
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".yaml" {
			name := e.Name()[:len(e.Name())-5] // strip .yaml
			fmt.Printf("  %s\n", name)
		}
	}
	fmt.Println("\nCreate from template: bt-agent-cli create --from-template <name>")
}
