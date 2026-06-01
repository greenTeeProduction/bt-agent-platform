// bt-assistant is a Hermes-style personal assistant CLI for the Go BT platform.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/llm"
)

type generator interface {
	Generate(prompt string) (string, error)
}

type taskExecutor interface {
	RunTask(agentName, task, treeID string) (output string, outcome string, err error)
}

type app struct {
	in       io.Reader
	out      io.Writer
	err      io.Writer
	registry *agent.Registry
	llm      generator
	executor taskExecutor
	graph    *knowledge.KnowledgeGraph
}

func main() { os.Exit(newApp(os.Stdin, os.Stdout, os.Stderr).run(os.Args[1:])) }

func newApp(in io.Reader, out, errOut io.Writer) *app {
	home, _ := os.UserHomeDir()
	reg, regErr := agent.NewRegistry(filepath.Join(home, ".go-bt-evolve", "agents"))
	if regErr != nil {
		fmt.Fprintf(errOut, "Error initializing registry: %v\n", regErr)
	}
	return &app{in: in, out: out, err: errOut, registry: reg, executor: dashboard.NewAgentExecutor(), graph: buildGraph()}
}

func buildGraph() *knowledge.KnowledgeGraph { return knowledge.BuildKnowledgeGraph() }

func (a *app) run(args []string) int {
	if a.registry == nil {
		fmt.Fprintln(a.err, "Error: registry unavailable")
		return 1
	}
	if len(args) == 0 {
		printUsage(a.out)
		return 0
	}
	switch args[0] {
	case "help", "--help", "-h":
		printUsage(a.out)
		return 0
	case "status":
		return a.cmdStatus()
	case "agents", "list":
		return a.cmdAgents()
	case "trees":
		return a.cmdTrees(args[1:])
	case "discover":
		return a.cmdDiscover(args[1:])
	case "create":
		return a.cmdCreate(args[1:])
	case "delete":
		return a.cmdDelete(args[1:])
	case "schedule":
		return a.cmdSchedule(args[1:])
	case "logs":
		return a.cmdLogs(args[1:])
	case "run":
		return a.cmdRunAgent(args[1:])
	case "run-tree":
		return a.cmdRunTree(args[1:])
	case "ask":
		return a.cmdAsk(args[1:])
	case "chat":
		return a.cmdChat()
	default:
		parsed := parseIntent(strings.Join(args, " "))
		if len(parsed) > 0 && strings.Join(parsed, " ") != strings.Join(args, " ") {
			return a.run(parsed)
		}
		fmt.Fprintf(a.err, "Unknown command: %s\n", args[0])
		printUsage(a.err)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `bt-assistant — Personal assistant CLI for the BT platform

Usage:
  bt-assistant status
  bt-assistant agents
  bt-assistant trees [--category <name>]
  bt-assistant discover <task>
  bt-assistant create <name> --tree <tree-id> [--desc <text>]
  bt-assistant run <agent-name> --input <task>
  bt-assistant run-tree <tree-id> <task>
  bt-assistant schedule <agent-name> --every <cron-or-duration>
  bt-assistant logs <agent-name>
  bt-assistant delete <agent-name> --yes
  bt-assistant ask <question>
  bt-assistant chat

Natural language shortcuts:
  bt-assistant list agents
  bt-assistant discover tree for improve observability
  bt-assistant run tree godev add tests for scheduler`)
}

func (a *app) cmdStatus() int {
	fmt.Fprintln(a.out, "BT Platform Assistant")
	fmt.Fprintf(a.out, "Agents: %d\n", len(a.registry.List()))
	if a.graph != nil {
		fmt.Fprintf(a.out, "Trees: %d\n", len(a.graph.Trees))
		fmt.Fprintf(a.out, "Edges: %d\n", len(a.graph.Edges))
	}
	return 0
}

func (a *app) cmdAgents() int {
	agents := a.registry.List()
	if len(agents) == 0 {
		fmt.Fprintln(a.out, "No agents registered. Create one with: bt-assistant create <name> --tree <tree-id>")
		return 0
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Definition.Name < agents[j].Definition.Name })
	fmt.Fprintf(a.out, "%-28s %-10s %-32s %s\n", "NAME", "STATE", "TREE", "SCHEDULE")
	for _, inst := range agents {
		schedule := inst.Definition.Schedule
		if schedule == "" {
			schedule = "on_demand"
		}
		fmt.Fprintf(a.out, "%-28s %-10s %-32s %s\n", inst.Definition.Name, inst.State, inst.Definition.Tree, schedule)
	}
	return 0
}

func (a *app) cmdTrees(args []string) int {
	fs := flag.NewFlagSet("trees", flag.ContinueOnError)
	fs.SetOutput(a.err)
	category := fs.String("category", "", "filter by category")
	if err := fs.Parse(normalizeFlagArgs(args, nil)); err != nil {
		return 2
	}
	trees := make([]*knowledge.TreeMeta, 0, len(a.graph.Trees))
	for _, tree := range a.graph.Trees {
		if *category == "" || tree.Category == *category {
			trees = append(trees, tree)
		}
	}
	sort.Slice(trees, func(i, j int) bool { return trees[i].ID < trees[j].ID })
	for _, tree := range trees {
		fmt.Fprintf(a.out, "%s\t%s\t%s\n", tree.ID, tree.Category, tree.Description)
	}
	return 0
}

func (a *app) cmdDiscover(args []string) int {
	task := strings.TrimSpace(strings.Join(args, " "))
	if task == "" {
		fmt.Fprintln(a.err, "Error: task required")
		return 2
	}
	treeID, confidence := a.graph.Discover(task)
	if treeID == "" {
		fmt.Fprintf(a.out, "No confident match for task: %s\n", task)
		return 1
	}
	fmt.Fprintf(a.out, "Tree: %s\nConfidence: %.2f\n", treeID, confidence)
	if meta := a.graph.Trees[treeID]; meta != nil {
		fmt.Fprintf(a.out, "Name: %s\nDescription: %s\n", meta.Name, meta.Description)
	}
	return 0
}

func (a *app) cmdCreate(args []string) int {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(a.err)
	treeID := fs.String("tree", "", "tree id")
	desc := fs.String("desc", "", "description")
	if err := fs.Parse(normalizeFlagArgs(args, nil)); err != nil {
		return 2
	}
	name := fs.Arg(0)
	if name == "" || *treeID == "" {
		fmt.Fprintln(a.err, "Error: name and --tree required")
		return 2
	}
	inst, err := a.registry.Create(agent.Definition{Name: name, Description: *desc, Tree: *treeID, Schedule: "on_demand"})
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.out, "Created agent: %s (id=%s, tree=%s)\n", inst.Definition.Name, inst.ID, inst.Definition.Tree)
	return 0
}

func (a *app) cmdDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(a.err)
	yes := fs.Bool("yes", false, "confirm deletion")
	if err := fs.Parse(normalizeFlagArgs(args, map[string]bool{"--yes": true, "-yes": true})); err != nil {
		return 2
	}
	name := fs.Arg(0)
	if name == "" {
		fmt.Fprintln(a.err, "Error: agent name required")
		return 2
	}
	if !*yes {
		fmt.Fprintln(a.err, "Error: delete requires --yes")
		return 2
	}
	if err := a.registry.Delete(name); err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.out, "Deleted agent: %s\n", name)
	return 0
}

func (a *app) cmdSchedule(args []string) int {
	fs := flag.NewFlagSet("schedule", flag.ContinueOnError)
	fs.SetOutput(a.err)
	every := fs.String("every", "", "cron expression or duration")
	if err := fs.Parse(normalizeFlagArgs(args, nil)); err != nil {
		return 2
	}
	name := fs.Arg(0)
	if name == "" || *every == "" {
		fmt.Fprintln(a.err, "Error: name and --every required")
		return 2
	}
	if err := a.registry.UpdateSchedule(name, *every); err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.out, "Scheduled agent %q: %s\n", name, *every)
	return 0
}

func (a *app) cmdLogs(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.err, "Error: agent name required")
		return 2
	}
	inst, err := a.registry.Get(args[0])
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	logPath := filepath.Join(os.Getenv("HOME"), ".go-bt-evolve", "logs", "bt.log")
	fmt.Fprintf(a.out, "Agent: %s\nState: %s\nRuns: %d\nSuccess: %.0f%%\nLogs: %s\n", inst.Definition.Name, inst.State, inst.RunCount, inst.SuccessRate*100, logPath)
	return 0
}

func (a *app) cmdRunAgent(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(a.err)
	input := fs.String("input", "", "task input")
	if err := fs.Parse(normalizeFlagArgs(args, nil)); err != nil {
		return 2
	}
	name := fs.Arg(0)
	if name == "" || *input == "" {
		fmt.Fprintln(a.err, "Error: agent name and --input required")
		return 2
	}
	inst, err := a.registry.Get(name)
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	return a.runTreeWithExecutor(name, inst.Definition.Tree, *input)
}

func (a *app) cmdRunTree(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(a.err, "Error: run-tree requires <tree-id> <task>")
		return 2
	}
	return a.runTreeWithExecutor("bt-assistant", args[0], strings.Join(args[1:], " "))
}

func (a *app) runTreeWithExecutor(agentName, treeID, task string) int {
	if a.executor == nil {
		fmt.Fprintln(a.err, "Error: executor unavailable")
		return 1
	}
	fmt.Fprintf(a.out, "Running tree %s for task: %s\n", treeID, task)
	output, outcome, err := a.executor.RunTask(agentName, task, treeID)
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		if output != "" {
			fmt.Fprintln(a.out, output)
		}
		return 1
	}
	fmt.Fprintf(a.out, "Outcome: %s\n", outcome)
	if output != "" {
		fmt.Fprintln(a.out, output)
	}
	return 0
}

func (a *app) cmdAsk(args []string) int {
	question := strings.TrimSpace(strings.Join(args, " "))
	if question == "" {
		fmt.Fprintln(a.err, "Error: question required")
		return 2
	}
	model, err := a.getLLM()
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	answer, err := model.Generate(a.assistantPrompt(question))
	if err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.out, answer)
	return 0
}

func (a *app) getLLM() (generator, error) {
	if a.llm != nil {
		return a.llm, nil
	}
	cfg, err := config.Load()
	if err != nil {
		client, clientErr := llm.NewClient(llm.DefaultConfig())
		if clientErr != nil {
			return nil, fmt.Errorf("load config: %w; create default LLM: %w", err, clientErr)
		}
		a.llm = client
		return a.llm, nil
	}
	provider, err := llm.NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	a.llm = provider
	return a.llm, nil
}

func (a *app) assistantPrompt(question string) string {
	var b strings.Builder
	b.WriteString("You are the personal assistant for the Go BT platform. Answer concisely and prefer exact bt-assistant commands when actions are needed.\n\nAgents:\n")
	agents := a.registry.List()
	sort.Slice(agents, func(i, j int) bool { return agents[i].Definition.Name < agents[j].Definition.Name })
	for _, inst := range agents {
		fmt.Fprintf(&b, "- %s state=%s tree=%s schedule=%s\n", inst.Definition.Name, inst.State, inst.Definition.Tree, inst.Definition.Schedule)
	}
	if len(agents) == 0 {
		b.WriteString("- none\n")
	}
	if a.graph != nil {
		fmt.Fprintf(&b, "\nKnowledge graph: %d trees, %d edges.\n", len(a.graph.Trees), len(a.graph.Edges))
	}
	fmt.Fprintf(&b, "\nUser question: %s\n", question)
	return b.String()
}

func (a *app) cmdChat() int {
	fmt.Fprintln(a.out, "BT Assistant chat. Type /help, /quit, or natural language commands.")
	scanner := bufio.NewScanner(a.in)
	for {
		fmt.Fprint(a.out, "> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "/quit" || line == "quit" || line == "exit" {
			break
		}
		if line == "/help" {
			printUsage(a.out)
			continue
		}
		args := parseIntent(line)
		if len(args) == 0 {
			args = []string{"ask", line}
		}
		if code := a.run(args); code != 0 {
			fmt.Fprintf(a.err, "command failed with exit %d\n", code)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(a.err, "Error: %v\n", err)
		return 1
	}
	return 0
}

func parseIntent(line string) []string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return nil
	}
	lower := strings.ToLower(strings.Join(fields, " "))
	switch {
	case lower == "status" || strings.Contains(lower, "platform status"):
		return []string{"status"}
	case lower == "list agents" || lower == "show agents" || lower == "agents":
		return []string{"agents"}
	case lower == "list trees" || lower == "show trees" || lower == "trees":
		return []string{"trees"}
	case strings.HasPrefix(lower, "discover tree for "):
		return append([]string{"discover"}, fields[3:]...)
	case strings.HasPrefix(lower, "discover "):
		return append([]string{"discover"}, fields[1:]...)
	case strings.HasPrefix(lower, "run tree ") && len(fields) >= 4:
		return append([]string{"run-tree", fields[2]}, fields[3:]...)
	case strings.HasPrefix(lower, "ask "):
		return append([]string{"ask"}, fields[1:]...)
	default:
		return fields
	}
}

func normalizeFlagArgs(args []string, boolFlags map[string]bool) []string {
	if len(args) == 0 {
		return args
	}
	var flags []string
	var pos []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if boolFlags != nil && boolFlags[arg] {
				continue
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, arg)
	}
	return append(flags, pos...)
}
