# PR 83 check corrections

The recorded GitHub failures were the webhook panic-recovery test and the
separate gosec code-scanning check. The webhook fixture now observes delivery
with a bounded channel wait; the apply-lock parent directory now uses `0750`.
Both changes address the actual findings, without suppressing the scanner.

Original head: `db2c116f7cce1ab921eda65cc81c3fbdba53359f`.
[Original check identities and annotation](../2026-10-01-sprint-admission/github-checks.json)
include the successful Actions Security Scan and failing separate gosec check.
The old webhook failure expected three successful posts but observed one.
The gosec annotation points to `superpowers_apply.go:112`, directory permissions.

## Local evidence

| Command | Outcome |
|---|---|
| `go test -short -count=10 -race -coverprofile=<private>/coverage.out -covermode=atomic ./internal/agent -run '^TestWebhookPublisherLoop_PanicRecovered$'` | Ten repetitions passed; [webhook.log](webhook.log). |
| `go test -short -count=1 ./internal/engine -run 'Test.*(Apply\|Landing\|MainPreservation\|Rebase)'` | Passed; [engine.log](engine.log). |
| `gosec -no-fail -fmt sarif -out <private>/results.sarif -severity medium ./...` | Valid SARIF produced; the recorded line-112 G301 finding is absent. [Sanitized summary](sarif-summary.json) retains 313 remaining scanner findings by rule and other target-file findings. Zero exit under `-no-fail` does not establish a clean scanner. |
| Codex conventions review after engine change | Clean import, bounded-lock and persistence review; no edits or providers invoked. |
| `graphify update .`, `git diff --check` | Passed; AST-only extraction, no model/API cost. |

Go 1.26.5/Linux ARM64; gosec module v2.27.1. Tests use isolated state and
`BT_SKIP_LLM_TESTS=1`. The [full short race/atomic coverage checkpoint](../2026-10-01-restart/README.md)
passed before this one-literal permission change. [Source identity](source-snapshot.json)
binds the final engine file to base `7e6c7584`.

Concurrent unfinished Sol-policy edits appeared in the original shared
checkout after the recovery checkpoint. They were preserved, and remaining
checks moved to an isolated worktree based on `7e6c7584`. An intervening
compile-failed check against that changing checkout is excluded from this
evidence. No unowned changes were reverted or included in this PR snapshot.

GitHub outcomes for the updated PR head remain separate acceptance. The local
high-severity gate and Actions Security Scan success cannot establish that
GitHub's SARIF comparison passes.
