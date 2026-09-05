=== BT Platform Walkthrough — 2026-06-02T10:45:58Z ===

## Setup Verification
```
Go version:
go version go1.26.3 linux/arm64

Build check:
```

## Quick Tests
```
ok  	github.com/nico/go-bt-evolve/cmd/bt-assistant	0.035s
ok  	github.com/nico/go-bt-evolve/cmd/bt-security-probe	0.051s
ok  	github.com/nico/go-bt-evolve/cmd/bt-tree-integration	0.024s
ok  	github.com/nico/go-bt-evolve/internal/a2a	0.047s
ok  	github.com/nico/go-bt-evolve/internal/agent	1.506s
ok  	github.com/nico/go-bt-evolve/internal/api	0.126s
ok  	github.com/nico/go-bt-evolve/internal/benchmark	0.044s
ok  	github.com/nico/go-bt-evolve/internal/benchreg	0.036s
ok  	github.com/nico/go-bt-evolve/internal/cicd	0.016s
ok  	github.com/nico/go-bt-evolve/internal/config	5.877s
ok  	github.com/nico/go-bt-evolve/internal/domains	0.220s
ok  	github.com/nico/go-bt-evolve/internal/engine	14.135s
ok  	github.com/nico/go-bt-evolve/internal/eval	0.066s
ok  	github.com/nico/go-bt-evolve/internal/evaluator	0.027s
ok  	github.com/nico/go-bt-evolve/internal/evolution	0.083s
ok  	github.com/nico/go-bt-evolve/internal/factory	0.024s
ok  	github.com/nico/go-bt-evolve/internal/finance	0.014s
ok  	github.com/nico/go-bt-evolve/internal/gardener	0.109s
ok  	github.com/nico/go-bt-evolve/internal/goap	0.082s
ok  	github.com/nico/go-bt-evolve/internal/knowledge	0.017s
ok  	github.com/nico/go-bt-evolve/internal/langagent	0.042s
ok  	github.com/nico/go-bt-evolve/internal/llm	0.527s
ok  	github.com/nico/go-bt-evolve/internal/log	0.018s
ok  	github.com/nico/go-bt-evolve/internal/mcp	0.024s
ok  	github.com/nico/go-bt-evolve/internal/metrics	0.009s
ok  	github.com/nico/go-bt-evolve/internal/monitoring	0.010s
ok  	github.com/nico/go-bt-evolve/internal/reflection	0.011s
ok  	github.com/nico/go-bt-evolve/internal/reliability	11.316s
ok  	github.com/nico/go-bt-evolve/internal/research	0.020s
ok  	github.com/nico/go-bt-evolve/internal/security	3.841s
ok  	github.com/nico/go-bt-evolve/internal/startup	0.019s
ok  	github.com/nico/go-bt-evolve/internal/thinktank	0.012s
ok  	github.com/nico/go-bt-evolve/internal/tools	0.137s
ok  	github.com/nico/go-bt-evolve/internal/tracing	5.212s
ok  	github.com/nico/go-bt-evolve/internal/util	0.005s
ok  	github.com/nico/go-bt-evolve/internal/validate	0.005s
ok  	github.com/nico/go-bt-evolve/internal/workflow	0.026s
```

## Binary Size Overview
```
-rwxrwxr-x 1 nico nico  13M Jun  2 08:32 /home/nico/go-bt-evolve/bin/bt-agent
-rwxrwxr-x 1 nico nico  13M Mai 31 09:05 /home/nico/go-bt-evolve/bin/bt-agent.1780211101.bak
-rwxrwxr-x 1 nico nico 7,6M Jun  1 10:17 /home/nico/go-bt-evolve/bin/bt-agent-cli
-rwxrwxr-x 1 nico nico  11M Mai 29 14:22 /home/nico/go-bt-evolve/bin/bt-agent-v4
-rwxrwxr-x 1 nico nico 9,3M Jun  1 10:17 /home/nico/go-bt-evolve/bin/bt-assistant
-rwxrwxr-x 1 nico nico 3,6M Jun  2 10:08 /home/nico/go-bt-evolve/bin/bt-ci-doctor
-rwxrwxr-x 1 nico nico  12M Jun  1 19:18 /home/nico/go-bt-evolve/bin/bt-dashboard
-rwxrwxr-x 1 nico nico 7,5M Jun  1 10:17 /home/nico/go-bt-evolve/bin/bt-evaluator
-rwxrwxr-x 1 nico nico  17M Jun  1 10:17 /home/nico/go-bt-evolve/bin/bt-gardener
-rwxrwxr-x 1 nico nico  17M Jun  1 10:17 /home/nico/go-bt-evolve/bin/bt-langagent
-rwxrwxr-x 1 nico nico 8,1M Jun  2 06:25 /home/nico/go-bt-evolve/bin/bt-scalability-probe
-rwxrwxr-x 1 nico nico 9,7M Jun  2 06:38 /home/nico/go-bt-evolve/bin/bt-tree-integration
```

## MCP Tools Overview
```
bt-agent:
  32 MCP tools (tree execution, evolution, agents, knowledge graph, factory)
bt-langagent:
  3 MCP tools (ReAct agent wrapping 7 BT tools)
bt-evaluator:
  5 MCP tools (Stockfish-style tree evaluation)
```

## Package Coverage Summary
```
coverage: 100.0
coverage: 100.0
coverage: 100.0
coverage: 100.0
coverage: 39.3
coverage: 42.4
coverage: 53.6
coverage: 55.6
coverage: 56.3
coverage: 58.2
coverage: 74.7
coverage: 75.0
coverage: 76.9
coverage: 77.1
coverage: 77.4
coverage: 79.0
coverage: 80.2
coverage: 80.8
coverage: 80.9
coverage: 84.3
coverage: 84.3
coverage: 87.0
coverage: 87.2
coverage: 87.6
coverage: 88.2
coverage: 88.6
coverage: 88.7
coverage: 89.5
coverage: 89.7
coverage: 90.3
coverage: 90.8
coverage: 91.9
coverage: 92.3
coverage: 92.8
coverage: 93.2
coverage: 95.7
coverage: 96.3
```

## Dashboard Health
```
{"packages":19,"status":"ok","trees":38,"uptime":"operational","version":"1.0.0"}
```

## Doc Drift Check
```
=== Doc Drift Validation ===
Root: /home/nico/go-bt-evolve

--- API_REFERENCE.md package listing ---
[32m  All code packages are documented[0m
[32m  ✓ package listing consistent[0m

--- GETTING_STARTED.md binary listing ---
[32m  All core binaries mentioned in Getting Started[0m

--- TUTORIAL.md command validation ---
[32m  All tutorial command references are valid[0m

--- TROUBLESHOOTING.md command validation ---
[32m  All troubleshooting command references are valid[0m

--- ADR catalog validation ---
[32m  All ADR files are indexed[0m
[32m  All ADRs have status markers[0m

--- VIDEO_WALKTHROUGH.md command syntax check ---
[32m  All video walkthrough command references are valid[0m

=== Results ===
[32m  ✓ Documentation is fully in sync with codebase[0m

Exit code: 0
```
