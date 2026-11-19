# ADR-001: Local multi-bot architecture

Status: accepted (2026-11-19)

## Decision

- One Go binary, `realm`: CLI, daemon (`realm daemon`) and stdio MCP server (`realm mcp`).
- Two scopes. Global: `~/.realm` (override with `REALM_HOME`). Project: `<project>/.realm`,
  registered in the global database.
- One SQLite database per scope (`realm.db`, pure-Go `modernc.org/sqlite`) for chats,
  messages, CLI sessions, the off-bot inbox and pending moderator questions.
- Each bot is a folder: `bot.yaml` (charter, model, provider, skills, tools) plus
  `knowledge/` (markdown notes and `INDEX.md`) and `workspace/`.
- Bots never self-select messages. A bot runs only when mentioned, DM'd, delegated to
  through a realm tool, or routed by the chat moderator (one LLM call per un-mentioned
  message; asks the user when the owner is ambiguous).
- Access policy is one pure function (`internal/policy`): global bots reach every chat;
  project bots reach only their project's chats and may DM global bots.
- Runtimes: `claude-cli` (subscription, headless `claude -p` with `--permission-mode dontAsk`
  and an explicit tool allowlist) and API-key providers (`anthropic`, `openai-compat`)
  with a native tool loop. API keys are read from environment variables named in
  `~/.realm/config.yaml`, never stored in bot files.

## Consequences

- No server or cloud component; everything runs on the user's machine.
- Routing cost is bounded: at most one moderator call per un-mentioned message and at
  most two bots per routed message.
