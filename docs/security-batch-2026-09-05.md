# HTTP authentication and agent storage migration

This batch addresses review findings C01, H01 and H02.

## Launch settings

Both HTTP listeners now bind to `127.0.0.1` by default. Configure the existing
platform credential with `BT_API_KEY`, or the `api_key` field in the JSON file
selected by `BT_CONFIG_FILE`. Environment values take precedence. Supply your
existing deployment credential through your usual secret manager; there is no
new token, generated execution credential, or unauthenticated development mode.

| Setting | Default | Purpose |
| --- | --- | --- |
| `BT_API_KEY` | empty | Platform credential used by dashboard and A2A execution |
| `BT_CONFIG_FILE` | unset | Optional existing JSON configuration, including `api_key` |
| `BT_DASHBOARD_BIND` | `127.0.0.1` | Dashboard listener IP |
| `BT_DASHBOARD_PORT` | `9800` | Dashboard listener port |
| `BT_A2A_BIND` | `127.0.0.1` | A2A listener IP |
| `BT_A2A_PORT` | `8686` | A2A listener port |
| `BT_A2A_BASE_URL` | `http://localhost:8686` (using the selected port) | Advertised A2A origin; also scopes built-in client credentials |
| `BT_TLS_CERT`, `BT_TLS_KEY` | unset | Dashboard HTTPS certificate and key paths |

With the credential already provided in the process environment or configuration:

```sh
# Loopback listeners
 go run ./cmd/bt-dashboard/
 go run ./cmd/bt-agent/
```

For remote access, explicitly set `BT_DASHBOARD_BIND` and/or `BT_A2A_BIND` to a
literal interface IP, `0.0.0.0`, or `::`. `localhost` is also accepted and resolves
to IPv4 loopback; other DNS bind names are rejected. A remote listener refuses
to start without a configured platform key. For A2A, set `BT_A2A_BASE_URL` to the
reachable advertised origin and use the same setting in the dashboard and daemon.
Use dashboard TLS or a trusted TLS proxy for remote transport; A2A's listener
itself still serves HTTP.

## Compatibility impact

- Empty-key dashboard installations now return HTTP 401 on privileged routes.
  The public UI and existing public health/discovery routes remain accessible.
  Configure a key before using agent, task, pipeline, or blackboard operations.
- Dashboard clients can continue sending `X-API-Key`. Browser login accepts the
  same environment/config credential and issues a session cookie. HTTPS login
  and protected routes share one TLS-aware session store. Existing CSRF rules
  are unchanged.
- A2A JSON-RPC requests to `/agents/<name>` now require `X-API-Key`, including
  task queries and cancellation. Missing, wrong, or unconfigured keys return
  HTTP 401 before SDK dispatch. GET on that path serves the public agent card;
  `/health`, global card discovery, and the agent list remain public.
- Built-in delegation and auction clients receive the configured platform key
  at startup. They attach it only when both the requested discovery origin and
  the advertised RPC origin match `BT_A2A_BASE_URL`. They do not attach it to
  public discovery or unrelated external agents, and RPC redirects are refused.
  Third-party callers must add the header themselves. Separate peer credentials
  for unrelated external origins are not configured by this change.
- Clients that previously reached either listener through a non-loopback IP must
  opt into the remote bind settings above.

## Agent names and files

Names may contain Unicode letters/digits, dashes, underscores, and dots after
the first character. Existing names such as `code-reviewer`, `Agent_2`, and
`agent.v2` continue to work. Empty names, leading dots, whitespace, control
characters, separators, traversal, absolute paths, and other punctuation are
rejected by one shared validator. Invalid stored YAML names and symlink entries
are ignored during registry loading. Rename incompatible definitions manually
before reloading; no automatic name migration is performed.

Create/save use a rooted, exclusive temporary file followed by atomic rename.
Delete uses rooted removal; neither writes through nor deletes a symlink target.
Existing symlink or non-regular definition entries are rejected. The dashboard's
legacy `.yml` deletion fallback remains supported with the same checks. The
configured registry directory is the trusted root; this is not isolation from
an attacker who can replace that directory, control its ancestors, or mount
filesystems beneath it.

The remainder of the security review is outside this batch, including execution
policy/concurrency unification, CSRF redesign, public diagnostic routes, and
engine/gardener findings.
