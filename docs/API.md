# API reference

Realm has three programmatic surfaces:

1. [Daemon HTTP API](#daemon-http-api): used by the CLI, TUI, web UI and admin MCP.
2. [Bot tools](#bot-tools): what bots call during a turn.
3. [Admin MCP tools](#admin-mcp-tools): what Claude Code or Claude Desktop calls to manage Realm for you.

How these fit together is in [ARCHITECTURE.md](ARCHITECTURE.md).

## Daemon HTTP API

### Transports

| Transport | Address | Auth | Routes |
| --- | --- | --- | --- |
| Unix socket | `~/.realm/realm.sock` (mode `0600`) | File permissions | All routes below |
| Web listener | `http://127.0.0.1:<port>/api` | `Authorization: Bearer <token>` | Shared routes only |

- The web listener starts on the first `POST /v1/ui` and stops with the daemon.
- Its token is printed in the URL fragment by `realm ui`.
- Paths on the web listener carry an `/api` prefix: `GET /api/v1/bots?scope=global`.
- The web listener rejects:
  - a `Host` other than `127.0.0.1:<port>` or `localhost:<port>` with `403`
  - an `Origin` other than `http://127.0.0.1:<port>` or `http://localhost:<port>` with `403`
  - a missing or wrong token with `401`
- `GET /api/v1/stream` also accepts the token as a `token` query parameter, because `EventSource` cannot set headers.

Calling the socket from a shell:

```bash
curl --unix-socket ~/.realm/realm.sock http://realm/v1/health
curl --unix-socket ~/.realm/realm.sock http://realm/v1/messages \
  -H 'Content-Type: application/json' -d '{"chat":"my-app/dev","text":"@reviewer look at jwt.go"}'
```

### Envelope and errors

Every response except the SSE stream is JSON:

```json
{"ok": true,  "data": <result>}
{"ok": false, "error": "bot @my-app/db not found"}
```

| Status | Meaning |
| --- | --- |
| `200` | Success. |
| `400` | Bad body or a rejected request: validation error, policy denial, archived bot. |
| `401` | Web listener only: missing or wrong token. |
| `403` | Web listener only: `Host` or `Origin` not allowed. |
| `404` | Chat, bot, project or note not found. |
| `501` | `POST /v1/ui` when the web UI is not available. |

Request bodies are JSON, limited to 1 MiB.

### Conventions

- **Scope** is `global` or a project slug.
- **Handle** is `@<scope>/<name>`. Most endpoints also accept a short `@name` or `name`. A short name resolves in the request's `scope` first, then in the global scope.
- **Chat id** is `<scope>/<name>`. A DM id is `<scope>/dm:<a>+<b>`.
- Some list endpoints return `null` instead of `[]` when nothing exists. Treat `null` as empty.

### Message envelope

Messages are stored in SQLite and sent on the SSE stream in this shape:

```json
{
  "id": "0193f2a1c0de0001ab12",
  "chat": "my-app/dev",
  "thread": "0193f2a1c0de0001ab12",
  "parent": "0193f2a1b9aa0000cd34",
  "from": "@user",
  "kind": "text",
  "mentions": ["@my-app/reviewer"],
  "hop": 0,
  "text": "@reviewer is internal/auth/jwt.go idiomatic?",
  "ts": "2026-11-20T09:12:44.123Z"
}
```

| Field | Notes |
| --- | --- |
| `id` | Monotonic. Sorts in creation order. |
| `thread` | Id of the message that started the exchange. |
| `parent` | The message this one answers. Omitted when empty. |
| `from` | `@user`, a bot handle, or `@<scope>/moderator`. |
| `kind` | `text` (user), `command` or `inquiry` (bot to bot), `reply`, `refusal`, `question` (moderator asks you), `system` (routing lines, notices). |
| `mentions` | Resolved handles. Omitted when empty. |
| `hop` | Delegation depth. `0` for user messages. |

### Object shapes

**Chat**

```json
{"id":"my-app/dev","kind":"group","scope":"my-app","name":"dev","routing":"moderated","created":"2026-11-20T09:00:00Z"}
```

`kind` is `group` or `dm`. `routing` is `moderated` or `mention`.

**BotView**

```json
{
  "handle": "@my-app/reviewer", "name": "reviewer", "scope": "my-app",
  "role": "bot", "status": "active",
  "provider": "claude-cli", "model": "sonnet", "effort": "high",
  "charter": "Reviews code in this repo...", "topics": ["code-review"],
  "skills": [], "tools": ["Read", "Grep", "Glob"],
  "lineage": [{"inherited_from": "@global/old-reviewer", "at": "2026-11-19"}],
  "knowledge_count": 4, "dir": "/home/me/code/my-app/.realm/bots/reviewer"
}
```

`role` is `bot` or `moderator`. `status` is `active`, `off` or `archived`.

**Knowledge entry**

```json
{"id":"error-style-1c0de0001ab12","title":"Error style","body":"Wrap errors with %w.","created":"2026-11-20T09:00:00Z","origin":"@global/old-reviewer"}
```

The id is the title as a slug plus a unique suffix. `origin` is set on notes copied by inheritance.

### Health and control

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/health` | none | `{"pid": 1234}` |
| `POST /v1/shutdown` | none | `{"stopping": true}`. Socket only. |
| `POST /v1/ui` | none | `{"url": "http://127.0.0.1:PORT/#token=…"}`. Starts the web listener once, then returns the same URL. Socket only. |
| `GET /v1/settings` | none | `{"home", "moderator_model", "limits": {"max_concurrent", "max_hops", "history_messages", "turn_timeout_sec"}, "max_fanout_per_turn", "max_turns_per_message"}` |
| `GET /v1/providers` | none | `[{"name", "type", "key_env", "ready", "note"}]`. `ready` reports whether `claude` is on `PATH` or the key env var is set. The key itself is never returned. |

### Projects and onboarding

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/projects` | none | `[{"slug", "root"}]` |
| `POST /v1/projects` | `{"dir", "slug"?, "ignore_db"}` | `{"slug", "root"}`. Creates `<dir>/.realm`. `slug` defaults to the folder name. With `ignore_db: true`, adds `.realm/realm.db` to the project's `.gitignore`. |
| `GET /v1/projects/inspect` | `?path=~/code/app` | `{"path", "exists", "is_git", "has_claude_md", "skill_count", "registered", "slug"}` |
| `GET /v1/projects/suggest` | none | Up to 8 git folders under your home, not yet registered. |
| `GET /v1/templates` | none | Starter bots: `[{"name", "model", "effort", "charter", "topics"}]` for `reviewer`, `docs` and `tester`. |
| `POST /v1/onboard` | See below | `{"project", "root", "bots": [handles], "chat": Chat}` |

`POST /v1/onboard` registers the folder, creates starter bots, and creates the
first group chat with the chosen global bots as members:

```json
{
  "dir": "~/code/my-app",
  "slug": "my-app",
  "ignore_db": true,
  "starters": ["reviewer", "tester"],
  "global_members": ["@global/ops"],
  "chat_name": "general",
  "routing": "moderated"
}
```

### Bots

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/bots` | `?scope=<scope>` | `[BotView]`. From `global`, the list includes every project's bots. From a project, it includes that project's bots and global bots. |
| `GET /v1/bot` | `?handle=@reviewer&scope=<scope>` | `BotView` |
| `POST /v1/bots` | BotInput (below) | `BotView` |
| `POST /v1/bots/update` | `{"handle", "patch": BotPatch}` | `BotView` |
| `POST /v1/bots/status` | `{"handle", "action": "on" \| "off" \| "archive" \| "restore"}` | `{"bot": BotView, "dispatched": n}`. `dispatched` counts queued inbox items delivered on `on`. |
| `POST /v1/bots/inherit` | `{"handle", "from"}` | Number of notes copied. |
| `POST /v1/bots/changed` | `{"handle"}` | Number of queued items dispatched. Call this after editing `bot.yaml` by hand. |

BotInput:

```json
{
  "scope": "my-app",
  "name": "db",
  "provider": "claude-cli",
  "model": "haiku",
  "effort": "low",
  "charter": "Owns the database schema and migrations.",
  "topics": ["schema", "sql"],
  "skills": ["*"],
  "tools": ["Read", "Grep", "Glob"],
  "inherit": "@global/old-db"
}
```

- `scope`, `name` and `charter` are required.
- `name` is lowercase letters, digits and dashes.
- `provider` defaults to `claude-cli`.
- Omitting `tools` gives the default `Read, Grep, Glob`. An empty array `[]` gives no built-in tools.
- `skills: ["*"]` allows every skill visible at the bot's scope.
- `inherit` copies knowledge from that bot after creation.

BotPatch has the same fields as BotInput except `scope`, `name` and `inherit`.
Omitted fields stay unchanged.

### Knowledge

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/knowledge` | `?handle=@reviewer` | `[Entry]` |
| `POST /v1/knowledge` | `{"handle", "title", "body"}` | `Entry` |
| `POST /v1/knowledge/update` | `{"handle", "id", "title", "body"}` | `Entry` |
| `POST /v1/knowledge/forget` | `{"handle", "id"}` | The deleted id. Permanent. |

### Skills

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/skills` | `?scope=<scope>` | `[{"name", "source": "global" \| "project", "description"}]` from `~/.claude/skills` and, for a project, `<project>/.claude/skills`. |

### Chats and messages

| Method and path | Body or query | Returns |
| --- | --- | --- |
| `GET /v1/chats` | `?scope=<scope>` | `[Chat]` |
| `GET /v1/chat` | `?id=<chat>` | `Chat` |
| `POST /v1/chats` | `{"scope", "name", "routing", "members": [handles]}` | `Chat`. `routing` defaults to `moderated`. Project chats include every active project bot. `members` adds extras, such as global bots. |
| `GET /v1/chats/members` | `?chat=<chat>` | `[BotView]` |
| `POST /v1/chats/members` | `{"chat", "bot"}` | The added handle. Project bots cannot join global chats or other projects' chats. |
| `POST /v1/chats/routing` | `{"chat", "routing"}` | The new routing mode. |
| `GET /v1/messages` | `?chat=<chat>&limit=50` | `[Message]`, oldest first. `limit` defaults to 50. |
| `POST /v1/messages` | `{"chat", "text"}` | The stored user `Message`. Routing and bot turns run after the response; replies arrive on the stream. |
| `POST /v1/dm` | `{"to", "scope", "text"}` | The stored user `Message` in the DM chat. `scope` is the caller's context, used to resolve a short handle. |
| `POST /v1/pick` | `{"chat", "ask"?, "bot"}` | The picked handle. Answers a moderator question. An empty `ask` means the latest open question in the chat. |
| `POST /v1/reroute` | `{"chat", "message", "bot"}` | The new target handle. Sends an existing message to another bot and saves a routing hint. |

### Live stream

`GET /v1/stream?chat=<chat>` returns `text/event-stream`:

```
: connected

data: {"id":"…","chat":"my-app/dev","from":"@my-app/reviewer","kind":"reply",…}

: ping
```

- One `data:` frame is sent per new message in the chat.
- A `: ping` comment is sent every 15 seconds.
- The stream does not replay history. Fetch `GET /v1/messages` after connecting, and merge by `id`.

### Bot tool endpoint

`POST /v1/tools/{name}` with `Authorization: Bearer <turn token>` and the tool
arguments as the body. It exists only on the unix socket, and only `realm mcp`
calls it. A token is valid only while its turn runs.

## Bot tools

`claude-cli` bots reach these through the `realm` MCP server, as
`mcp__realm__<name>`. API-key bots get them as native tools. The daemon checks
the access policy on every call.

| Tool | Arguments | Does |
| --- | --- | --- |
| `delegate` | `to` (required), `text` (required), `kind`: `command` \| `inquiry` | Asks another bot and waits for its answer. The target applies its own charter. Counts toward the 3-per-turn fan-out limit and the hop limit. |
| `send_dm` | `to`, `text` (both required) | Sends a direct message without waiting. Counts toward the fan-out limit. |
| `post` | `chat`, `text` (both required) | Posts to a chat the bot can access. Does not trigger other bots. |
| `read_chat` | `chat` (required), `limit` (default 20) | Reads recent messages of an accessible chat. |
| `list_bots` | none | Lists bots this bot can reach, with charters. |
| `remember` | `title`, `body` (both required) | Saves a note to the bot's own knowledge. |
| `recall` | `query` (required) | Searches the bot's own knowledge. |
| `forget` | `id` (required) | Deletes one of the bot's own notes. |

API-key bots working in a project also get read-only file tools. They are
confined to the project folder and never follow symlinks. A global bot outside
a project gets none:

| Tool | Arguments | Does |
| --- | --- | --- |
| `read_file` | `path` (required, relative) | Reads a text file. |
| `grep` | `pattern` (required, Go regexp), `path` | Searches files. |
| `glob` | `pattern` (required) | Lists matching files, for example `internal/*.go`. |

### Reply format

Every bot reply starts with one JSON decision line. The rest is the message
body.

```
{"decision":"act"}
{"decision":"refuse","reason":"Schema changes belong to @db"}
{"decision":"delegate","target":"@global/ops","reason":"Deploy question"}
```

A reply without a valid decision line counts as `act`. On `delegate`, Realm
forwards the request to `target`.

## Admin MCP tools

Start the server with `realm mcp-admin`. Register it with Claude Code:

```bash
claude mcp add realm -- realm mcp-admin
```

The tools act as you, through the daemon's unix socket. Results are the
daemon's `data` as JSON text, except `read_messages`, which returns one
formatted line per message. Errors are returned as MCP tool errors.

| Tool | Required | Optional | Notes |
| --- | --- | --- | --- |
| `list_projects` | none | none | |
| `add_project` | `dir` | `name`, `ignore_db` (default false), `starters`, `global_members`, `chat_name` (default `general`), `routing` | Runs onboarding. Use only a folder the user named. |
| `list_bots` | `scope` | none | |
| `get_bot` | `handle` | none | |
| `create_bot` | `scope`, `name`, `charter` | `provider`, `model`, `effort`, `topics`, `skills`, `tools`, `inherit` | |
| `update_bot` | `handle` | `provider`, `model`, `effort`, `charter`, `topics`, `skills`, `tools` | Omitted fields stay unchanged. |
| `set_bot_status` | `handle`, `action` | none | `archive` is marked destructive. |
| `inherit_knowledge` | `handle`, `from` | none | |
| `list_knowledge` | `handle` | none | |
| `add_knowledge` | `handle`, `title`, `body` | none | |
| `forget_knowledge` | `handle`, `id` | none | Destructive and permanent. |
| `list_chats` | `scope` | none | |
| `create_chat` | `scope`, `name` | `routing`, `members` | |
| `add_chat_member` | `chat`, `bot` | none | |
| `set_chat_routing` | `chat`, `routing` | none | |
| `post_message` | `chat`, `text` | none | Replies arrive later. Use `read_messages`. |
| `read_messages` | `chat` | `limit` (default 30) | |
| `dm_bot` | `handle`, `text` | `timeout_sec` (default 300) | Waits for the bot's reply. |
| `pick_bot` | `chat`, `bot` | none | Answers the latest moderator question. |
| `reroute_message` | `chat`, `message`, `bot` | none | |

Destructive tools say so in their description, so the MCP client asks you before calling them.
