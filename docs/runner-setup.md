# Self-Hosted Jetson Runner

The CI/CD pipeline runs nightly Ollama-dependent tests on the Jetson via a
self-hosted GitHub Actions runner. This document covers setup, maintenance,
and troubleshooting.

## Quick Setup

```bash
# 1. Get a runner token from GitHub
#    https://github.com/nico/go-bt-evolve/settings/actions/runners/new
#    → New self-hosted runner → Linux → ARM64 → copy token

# 2. Run the setup script
make setup-runner TOKEN=AABBC...

# 3. Verify
curl -s https://github.com/nico/go-bt-evolve/settings/actions/runners | grep jetson
```

## What Gets Installed

| Component | Path | Purpose |
|---|---|---|
| Runner binary | `~/actions-runner/` | GitHub Actions runner v2.321.0 (ARM64) |
| Work directory | `~/actions-runner/_work/` | Job workspace (checkouts, artifacts) |
| systemd service | `actions.runner.nico-go-bt-evolve.*.service` | Auto-start on boot, crash recovery |
| Labels | `self-hosted, jetson, arm64, ollama` | Matched by `runs-on` in nightly.yml |

## Workflows Using Self-Hosted Runner

| Workflow | File | Schedule | Label |
|---|---|---|---|
| Nightly Full Tests | `.github/workflows/nightly.yml` | Daily 3am UTC | `[self-hosted, jetson, arm64]` |
| Benchmark Regression | `.github/workflows/nightly.yml` | After full-tests | `[self-hosted, jetson, arm64]` |

Both jobs in `nightly.yml` require Ollama at `http://localhost:11434`.
If Ollama is down, tests gracefully skip with a warning.

## Runner Lifecycle

```bash
# Check status
sudo ~/actions-runner/svc.sh status

# Stop runner (e.g., during Ollama model updates)
sudo ~/actions-runner/svc.sh stop

# Start runner
sudo ~/actions-runner/svc.sh start

# Remove service (unregister from GitHub)
sudo ~/actions-runner/svc.sh uninstall
```

## Runner Labels

The runner registers with four labels:
- `self-hosted` — standard GitHub Actions convention
- `jetson` — identifies this as a Jetson ARM64 machine
- `arm64` — architecture label for cross-platform awareness
- `ollama` — indicates Ollama is available on this runner

Workflow `runs-on` must match all listed labels (AND logic):
```yaml
runs-on: [self-hosted, jetson, arm64]
```

## Prerequisites

- **User**: `nico` (or user with sudo access for systemd installation)
- **Ollama**: Must be running at `http://localhost:11434` with `qwen3.6:35b-a3b` loaded
- **Disk**: ~10 GB free for GitHub checkout + Go module cache + benchmark datasets
- **GitHub repo**: `nico/go-bt-evolve` with Actions enabled
- **Token scope**: Repository-level runner registration token (expires after 1 hour)

## Troubleshooting

### Runner appears offline
Check: `sudo ~/actions-runner/svc.sh status`
If stopped: `sudo ~/actions-runner/svc.sh start`
If stale: remove and re-register with a fresh token.

### Nightly tests failing with "Ollama not reachable"
Verify: `curl -s http://localhost:11434/api/tags`
If Ollama is down, restart it: `ollama serve &`
Check model: `ollama list | grep qwen3.6`

### Runner disk full
The `_work/` directory accumulates job artifacts. Cleanup:
```bash
rm -rf ~/actions-runner/_work/_actions
rm -rf ~/actions-runner/_work/go-bt-evolve
```

### Token expired
Runner registration tokens expire after 1 hour. For re-registration:
1. Generate a new token at the GitHub runners settings page
2. Run `make setup-runner TOKEN=<new-token>`
3. The script detects existing installation and replaces it
