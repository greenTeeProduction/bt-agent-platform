# Unattended NotebookLM authentication

`Ensure` is the shared policy for `CheckNotebookLMAuth`,
`CheckNotebookLMAuthAndRefresh`, `notebooklm_refresh_auth`, and
`scripts/notebooklm-auth-rotate.sh` via `bt-notebooklm-auth`.

1. Acquire the shared nonblocking file lock and consult durable cooldown state.
2. Check saved auth with the installed CLI's `login --check`. Only a successful
   check with explicit valid-auth output is success. Network and unknown failures
   preserve credentials and never request browser restoration.
3. Only missing/stale/expired auth permits one bounded existing-session restore.
   GET `/json/list`, select a recognized existing page and pass its exact target
   WebSocket to the embedded Python helper. The target must belong to the
   configured CDP server. There is no browser launch, target creation, navigation,
   reload, activation, or tab close. A target that closes or changes session
   during extraction fails safely; it is never replaced with a new tab.
4. Read URL/HTML and origin-scoped cookies using installed CDP helpers. Require
   CSRF and the authenticated account field (session ID is optional on current
   NotebookLM pages), and confirm the saved
   profile's account. Missing identity or mismatched accounts fail closed.
5. Validate candidate credentials through the installed client's notebook-list
   RPC, without disk reload, token-cache writes, browser fallback or candidate
   token refresh. Reject malformed RPC data. Only then call `AuthManager.save_profile`
   with `force=False`, using the CLI's configured default profile/store.
6. Recheck saved auth. A restore's own success message is insufficient. Failures
   return non-success status and persist a 15-minute cooldown. State is written
   before subprocess work so interrupted attempts are throttled as well. Every
   caller, including cron and separate daemon processes, uses the same lock.

The installed 0.10.1 external provider recognizes `notebook.google.com`, but
still calls `find_or_create_notebooklm_page_by_cdp_url` and saves extracted auth
before validation. This adapter uses its lower-level read helpers to enforce
existing-only behavior. Supported page hosts match 0.10.1: `notebook.google.com`,
`notebooklm.google.com`, `notebooklm.cloud.google.com`, `notebook.cloud.google.com`,
and `vertexaisearch.cloud.google.com`.

All engine CLI calls use `Command`, backed by the upgraded uv interpreter at
`/home/nico/.local/share/uv/tools/notebooklm-mcp-cli/bin/python3`, rather than
resolving potentially obsolete installations on cron/daemon PATH. The embedded
adapter requires 0.10.1. CLI mode forbids interactive login, disables automatic
headless and CDP browser recovery, and keeps auth metadata updates in memory.
A Python audit hook also forbids child-process creation. No changes are made to
installed package code. Auth verdicts exclude CLI transcripts and credential data.

Configuration:

- `BT_NOTEBOOKLM_CDP_URL`: existing browser's HTTP CDP endpoint; defaults to
  `http://localhost:9222`.
- `BT_NOTEBOOKLM_AUTH_STATE_DIR`: policy state only; defaults to
  `~/.go-bt-evolve/notebooklm-auth`. Use identical values for daemon and cron.
- `BT_NOTEBOOKLM_AUTH_BIN`: optional location of the built cron policy binary;
  otherwise the script uses `../bin/bt-notebooklm-auth`. Missing binary fails
  closed. It never falls back to login.
- Profile defaults and credential storage remain owned by installed CLI config,
  including its existing `NLM_PROFILE`/`NOTEBOOKLM_MCP_CLI_PATH` behavior.

Checks take at most 30 seconds each; CDP preflight at most 5 seconds; restoration
including preflight at most 45 seconds. Parent cancellation kills the helper
subprocess. The helper cannot spawn descendants. An in-flight API request may
have completed when cancellation arrives, but no browser action is performed.
Validated credentials may already have been saved if a subsequent saved-auth
recheck fails (for example a new network outage); the final verdict still fails
and enters cooldown. Profiles are never deleted or force-replaced. The installed
AuthManager owns its normal file-write semantics.

Tests:

```sh
go test -race -short ./internal/notebooklmauth ./internal/engine ./cmd/bt-notebooklm-auth
/home/nico/.local/share/uv/tools/notebooklm-mcp-cli/bin/python3 -B internal/notebooklmauth/testdata/installed_api_test.py
```

Go integration tests execute the embedded helper in real subprocesses using fake
CLI/CDP APIs and temporary profiles. The installed-API test uses the real 0.10.1
parsers, WebSocket helper, client and AuthManager with a fake socket/RPC, isolated
storage and a network-denying audit hook. Neither test reads real credentials or
contacts the user's browser. Tests require Python 3; installed-API compatibility
checks additionally require the pinned CLI interpreter.
