---
name: check
description: Run the project quality gate (gofmt, vet, golangci-lint, doc drift, ci-doctor, short tests) with the correct Go PATH. Use before committing or when asked to run checks.
disable-model-invocation: true
---

# Quality Gate

Go lives at `/usr/local/go/bin/go` and is NOT on the non-interactive shell PATH.
Every go/make command must be prefixed:

```bash
PATH=/usr/local/go/bin:$PATH make check-quick
```

## Levels

| Argument | Command | Use when |
|----------|---------|----------|
| (none) / `quick` | `PATH=/usr/local/go/bin:$PATH make check-quick` | Before every commit (mirrors the pre-commit hook and CI Lint job) |
| `full` | `PATH=/usr/local/go/bin:$PATH make check-full` | Before pushing or after large merges |
| `test` | `PATH=/usr/local/go/bin:$PATH make test` | Fast test-only loop (`-short -race`) |

If `$ARGUMENTS` is given, pick the matching level; default is `quick`.

## Known flake

`TestCMAESOptimizer_Convergence` (`internal/evolution/cmaes_test.go`) is stochastic and
unseeded — it occasionally fails in full runs. If it is the ONLY failure, retry once:

```bash
PATH=/usr/local/go/bin:$PATH go test -count=1 -run TestCMAESOptimizer_Convergence ./internal/evolution/
```

If it passes on retry, treat the run as green but mention the flake in your report.
Any other failure is real — do not retry it away; investigate.

## Reporting

Report the actual command output (pass/fail per stage). Never claim green without
having seen the exit status.
