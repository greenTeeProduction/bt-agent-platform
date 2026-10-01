# Sol 6.1 inference policy

Owner instruction, clarified 2026-10-01: ordinary BT LLM roles use `gpt-6.1-sol`
through the existing Codex ChatGPT login. No separately billed API connection
or alternate-model fallback is configured by this change.

## Enforcement and entry points

`BT_LLM_SOL_ONLY=true` is the deployment setting. Unset or malformed values
also enable the policy. `config.SolModel` owns the model ID. Ordinary
inference, planning, reflection, evaluation, dashboard chat, assistant,
benchmark/integration inference, documentation generation and the LangChain
ReAct loops use `internal/llm.NewProvider` and `CodexClient`. Legacy direct
HTTP/ACP adapters reject calls before transport under this policy.

Coding implementation and review retain their existing permission separation;
the model selector pins Sol regardless of old `auto`, Spark or other model
overrides. Its review model is also pinned and nested coding agents are disabled
to prevent separate model configuration. Codex-only provider enforcement also overrides an explicit legacy
coding-provider opt-out while the global policy is on.

General inference uses `codex exec --model gpt-6.1-sol`, an empty temporary
working directory, `--ignore-user-config`, read-only sandbox, disabled shell,
web search, apps, hooks, remote plugins and subagents, and an ephemeral
session. Authentication still uses the operator's Codex login. Prompts go
through stdin; only the final-response file is returned. A failed command,
missing/empty final response or expired caller deadline is an error. Process
groups and inherited pipes receive bounded cancellation cleanup.

The LangChain adapter retains textual ReAct decisions and stop markers; BT
executes the tools. Native function-call schemas and non-text message parts
are rejected. CLI generation does not expose a verified hard output-token
cap: LangChain token hints are prompt guidance, not a billing/token guarantee.

## External-model features

The owner explicitly exempts NotebookLM generation/research, external embeddings
and session indexing, and legacy memory extraction from the Sol requirement.
These integrations retain their existing provider/model configuration, including
Ollama embeddings and the external memory-extraction script. The global Sol
switch stays enabled; it must not block these exceptions. This clarification
supersedes the initial same-day decision to disable those integrations.

NotebookLM uses the existing Google account and pinned `notebooklm-mcp-cli`
0.14.x integration (verified on the owner-upgraded 0.14.0). Its CLI and Codex MCP share the profile. The
[authentication adapter](../internal/notebooklmauth/README.md) renews the session
and validates it before persistence; a user timer requests renewal every 15
minutes. Revoked sessions still require an interactive `nlm login`. There is no
permanent consumer NotebookLM API key. Ordinary BT provider-review fallback
continues to use Sol.

The boundary covers built-in inference adapters and known external model
helpers. Arbitrary operator-provided shell commands or external MCP services
are not a model-access security boundary and must follow the same owner policy.

## Host deployment

The final environment file for `bt-agent`, `bt-dashboard` and `bt-gardener`
is `~/.config/bt/codex-only.env`; BT variables in the Hermes launch environment
match it. Both coding and ordinary inference select
`/home/nico/.local/bin/codex`, currently CLI 0.159.3. The prior npm-global CLI
0.153.4 rejected the requested model with this login. A one-response probe
through the current CLI succeeded with model `gpt-6.1-sol`, provider `openai`
and high reasoning. This establishes one successful inference, not quotas,
whole-workflow delivery or recovery guarantees.

All 14 command binaries (including the separately built documentation generator)
were installed in the existing `bin/` paths on 2026-10-01 after validation.
Each installed SHA-256 was compared with its staged binary; previous binaries
were preserved. The build includes the current uncommitted model-policy change
on base revision `7e6c7584`, so the source fingerprint and binary hashes, rather
than that revision alone, identify this installation. The services remain
inactive and disabled during the concurrent cleanup; no complete running
service cycle is claimed. Login health probes validate the executable and
check authentication without generating tokens; they do not prove model entitlement.

## Verification

`internal/llm/codex_test.go` exercises stdin/argument separation, explicit
model/permission selection, final-output integrity, transport failure,
deadline cleanup, legacy-provider rejection and ReAct stop behavior.
`internal/engine/sol_policy_test.go` covers coding model/failover.
`internal/engine/nlm_transport_test.go` verifies the NotebookLM exception,
bounded retries, no replay of ambiguous generation, and complete response JSON.
`internal/knowledge/sol_policy_test.go` checks configured embedding transport
while Sol-only is enabled; script-node tests cover the legacy script exception.

Legacy transport fixtures explicitly opt out using `BT_LLM_SOL_ONLY=false`;
production launch configurations must not do so. A single real adapter probe
is opt-in with `BT_LIVE_SOL=1 go test ./internal/llm -run TestCodexLoginSolLive`.

## Observed validation, 2026-10-01

The full short suite passed 43 packages and found one engine fixture still
expecting the prior model. After correcting that expectation, the complete
engine, llm, knowledge, benchmark, gardener and domains packages passed.
The complete llm package also passed with `-race` after the final executable
validation change. The opt-in live adapter test returned `BT_SOL61_OK` through
Codex login. No alternate live model was used for validation.

`make build` passed vet, formatting, dependency consistency, golangci-lint
(zero issues), the configured high-severity security scan, all 13 release
binaries and `graphify update .`. The documentation generator was built as
the 14th binary after those gates passed. Documentation drift checks passed
with the existing stale-walkthrough warning. The conventions review found no
new higher-layer engine imports, removed-package imports or shared-state
persistence changes in this policy patch.

Host evidence, source fingerprint, binary hashes and previous-binary backups:
`/home/nico/.local/state/go-bt-evolve/sol-6.1-20261001T073922Z/`.

## NotebookLM and external-integration correction, 2026-10-01

The owner exceptions above are restored without changing their provider settings.
The complete engine short suite passed, as did authentication and knowledge with
`-race`. Fifteen installed-library authentication tests passed with network
connections prohibited. A real embedding request to the existing Ollama endpoint
returned 768 dimensions from `nomic-embed-text`. The MCP stdio handshake and tool
inventory passed (46 tools, including research and Studio generation; native auth
writers hidden). Legacy extraction was exercised through the fake Python fixture,
not against personal memories.

The saved consumer NotebookLM session redirected to Google sign-in during the
live check. Renewal did not recover it; saved credentials were preserved. That first attempt required
recovery before live query/generation could be confirmed.
No paid artifact was generated as a validation side effect. This later correction
supersedes the initial same-day external-feature restrictions and installation.

### Upgrade and headless recovery, 2026-10-01

The owner upgraded the external CLI/MCP to 0.14.0. The adapter now supports the
0.14 patch series, reports unsupported minor versions clearly, disables the new
metadata-write path during read-only checks, and uses the upstream profile lock
and storage-mode-aware atomic writer. Renewal checks for concurrent login changes
under the upstream lock before saving.

Because the owner has only phone/tablet access, recovery used the upgraded
headless browser flow against an isolated copy of the existing saved browser
profile. The active account and notebook-list RPC were validated before importing
credentials. The live list returned 29 notebooks. This supersedes the earlier
same-day login-required finding; no interactive user sign-in was needed.

The rebuilt 0.14 adapter passed 17 installed-API tests and the Go auth race suite.
A real MCP session initialized with 48 tools, listed the 29 notebooks, and returned
a nonempty answer from the configured BT research notebook. The MCP calls left
the profile files unchanged. The release build and graph update passed; binaries
were replaced with hash verification and backups. The existing renewal timer was
retained, while bt-agent, bt-dashboard and bt-gardener remain stopped.

A subsequent live keepalive received Google HTTP 429 while notebook operations
still worked. The coordinator now persists a 15-minute renewal interval after
success and defers transient renewal failures without invalidating a separately
validated login. It continues to check saved authentication during renewal
backoff; an earlier valid result cannot conceal later expiry. Regression tests
cover all three states: renewed, renewal deferred, and expired authentication.
