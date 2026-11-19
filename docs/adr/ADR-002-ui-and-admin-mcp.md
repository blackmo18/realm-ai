# ADR-002: Web UI, terminal UI and admin MCP share one management API

Status: accepted (2026-11-20)

## Decision

- All frontends (web UI, terminal UI, admin MCP, CLI) use the engine's management API
  (`internal/engine/manage.go`, `onboarding.go`). The daemon exposes it on its unix socket.
- The web UI is a React + Vite + TypeScript app in `web/`, built into `internal/webui/dist`
  and embedded with `go:embed`. The built files are committed so `go install` needs no Node;
  CI fails if they are stale.
- Browsers cannot reach a unix socket, so `realm ui` asks the daemon to start a TCP listener
  on `127.0.0.1:<random port>`. It requires a per-launch token (URL fragment, then
  `Authorization: Bearer`; SSE uses a query token), checks `Host` and `Origin`, sets a CSP,
  and serves only the shared routes: never `/v1/tools/*`, `/v1/shutdown` or `/v1/ui`.
- The terminal UI is Bubble Tea over the daemon client, behind a small `API` interface so its
  model is tested with a fake.
- `realm mcp-admin` is a separate stdio MCP server with admin tools that act as the user.
  It is distinct from `realm mcp`, which bots use with a per-turn token and scope policy.
- Project onboarding is one call: register the folder (optional `.gitignore` entry for the
  chat database), create starter bots from templates, create the first chat.

## Consequences

- One code path for every frontend; behaviour cannot drift between UIs.
- The web UI is local-only by design; there is no remote access mode.
- Moderator and limit settings are read-only in the UIs; they stay in `config.yaml`.
