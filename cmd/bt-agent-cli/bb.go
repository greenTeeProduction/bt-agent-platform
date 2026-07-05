package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func cmdBB() {
	if len(os.Args) < 3 {
		printBBUsage()
		os.Exit(1)
	}
	switch os.Args[2] {
	case "list":
		cmdBBList()
	case "read":
		cmdBBRead()
	case "scopes":
		cmdBBScopes()
	default:
		printBBUsage()
		os.Exit(1)
	}
}

func printBBUsage() {
	fmt.Println(`bt-agent-cli bb — Inspect scoped blackboard storage

Usage:
  bt-agent-cli bb list --scope agent|session|run --id <scope_id> [--prefix key/] [--limit 50]
  bt-agent-cli bb read --scope agent|session|run --id <scope_id> --key <key>
  bt-agent-cli bb scopes --scope agent|session`)
}

func bbManager() *blackboard.Manager {
	return bbManagerAt(agent.BlackboardDir())
}

func bbManagerAt(dir string) *blackboard.Manager {
	mgr := blackboard.DefaultManager()
	_ = mgr.EnablePersistence(dir)
	return mgr
}

func parseBBScopeFlag(scope, id string) (blackboard.Scope, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return blackboard.Scope{}, fmt.Errorf("--id required")
	}
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case string(blackboard.ScopeRun):
		return blackboard.Scope{Kind: blackboard.ScopeRun, ID: id}, nil
	case string(blackboard.ScopeSession):
		return blackboard.Scope{Kind: blackboard.ScopeSession, ID: id}, nil
	case string(blackboard.ScopeAgent):
		return blackboard.Scope{Kind: blackboard.ScopeAgent, ID: id}, nil
	default:
		return blackboard.Scope{}, fmt.Errorf("--scope must be run, session, or agent")
	}
}

func cmdBBList() {
	fs := flag.NewFlagSet("bb list", flag.ExitOnError)
	scope := fs.String("scope", "agent", "Scope kind: run, session, agent")
	id := fs.String("id", "", "Scope identifier")
	prefix := fs.String("prefix", "", "Key prefix filter")
	limit := fs.Int("limit", 50, "Max entries")
	_ = fs.Parse(os.Args[3:])

	sc, err := parseBBScopeFlag(*scope, *id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	entries, err := bbManager().List(sc, *prefix, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		fmt.Println("(no keys)")
		return
	}
	for _, e := range entries {
		summary := e.Summary
		if summary == "" {
			summary = e.Value
		}
		if len(summary) > 120 {
			summary = summary[:120] + "..."
		}
		fmt.Printf("%s (%d bytes) %s\n", e.Key, e.SizeBytes, summary)
	}
}

func cmdBBRead() {
	fs := flag.NewFlagSet("bb read", flag.ExitOnError)
	scope := fs.String("scope", "agent", "Scope kind: run, session, agent")
	id := fs.String("id", "", "Scope identifier")
	key := fs.String("key", "", "Blackboard key")
	_ = fs.Parse(os.Args[3:])

	if strings.TrimSpace(*key) == "" {
		fmt.Fprintln(os.Stderr, "Error: --key required")
		os.Exit(1)
	}
	sc, err := parseBBScopeFlag(*scope, *id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	e, err := bbManager().Get(sc, *key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(e.Value)
}

func cmdBBScopes() {
	fs := flag.NewFlagSet("bb scopes", flag.ExitOnError)
	scope := fs.String("scope", "agent", "Scope kind: session or agent")
	asJSON := fs.Bool("json", false, "JSON output")
	_ = fs.Parse(os.Args[3:])

	kind := blackboard.ScopeKind(strings.ToLower(strings.TrimSpace(*scope)))
	if kind != blackboard.ScopeSession && kind != blackboard.ScopeAgent {
		fmt.Fprintln(os.Stderr, "Error: --scope must be session or agent")
		os.Exit(1)
	}
	ids, err := bbManager().ListPersistedScopeIDs(kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]interface{}{"scope": kind, "ids": ids})
		return
	}
	if len(ids) == 0 {
		fmt.Println("(none)")
		return
	}
	for _, id := range ids {
		fmt.Println(id)
	}
}
