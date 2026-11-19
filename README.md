# realm-ai

Local, Grok-style bots for Claude. Summon a bot with `@name` in a chat, or let
the chat moderator route your message to the bot that owns it. Bots run on your
Claude subscription through the `claude` CLI, or on API keys for other
providers (Anthropic API, OpenAI, OpenRouter, Ollama, any OpenAI-compatible
endpoint). Everything runs on your machine.

## Install

Requires Go 1.27+ and, for subscription bots, the `claude` CLI logged in.

```bash
go install github.com/blackmo18/realm-ai/cmd/realm@latest
```

## Quick start

```bash
realm init --global                       # ~/.realm (override with REALM_HOME)
cd ~/code/my-app && realm init            # registers the project as scope "my-app"

realm bot create reviewer --model sonnet --topics go \
  --charter "Reviews Go code in this repo. Refuses everything else."
realm bot create db --model haiku --topics schema,sql \
  --charter "Owns the database schema and migrations."
realm bot create ops --global --model haiku --topics deploy \
  --charter "Answers deployment questions for all projects."

realm dm @reviewer "is internal/auth/jwt.go idiomatic?"

realm chat create dev                     # moderated group chat for this project
realm chat join dev                       # interactive: type, @mention, /pick, /quit
```

The daemon starts on demand (`realm daemon start|stop|status`).

## Concepts

**Scopes.** A bot is global (`~/.realm/bots/<name>`, handle `@global/<name>`) or
belongs to a project (`<project>/.realm/bots/<name>`, handle `@<project>/<name>`).
`@name` resolves in the current project first, then global.

**Bots.** Each bot folder holds `bot.yaml` and `knowledge/`:

```yaml
name: reviewer
role: bot
status: active          # active | off | archived
provider: claude-cli    # claude-cli (subscription) or a provider from config.yaml
model: sonnet
effort: medium          # low | medium | high | xhigh | max
charter: |              # the only work this bot acts on
  Reviews Go code in this repo. Refuses everything else.
topics: [go]
skills: [tdd-guide]     # allowed skills; "*" = all visible at the bot's scope
tools: [Read, Grep, Glob]
lineage:
  - inherited_from: '@global/old-reviewer'
    at: "2026-11-19"
```

**Context awareness.** A project bot runs `claude` inside the project folder
with project settings, so it sees the project's `CLAUDE.md`, project skills and
global skills. A global bot runs in its own workspace and gets the project
folder added only when it works in that project's chat.

**Knowledge.** Markdown notes in `knowledge/` with an `INDEX.md` that is added to
every turn. Bots write it with the `remember` tool; you can too
(`realm knowledge add|list|show|forget`). Knowledge survives `off` and
`archive`, and can be copied into a new or existing bot
(`realm bot create new --inherit @old`, `realm bot inherit @bot --from @old`).

**Lifecycle.** `realm bot on|off|archive|restore`. Off bots queue messages and
answer them when turned back on. Archived bots reject messages.
`realm bot set` changes model, effort, provider, charter, topics, skills, tools.

## Messaging and routing

Bots never pick up messages on their own. A bot turn runs only when the bot is:

1. mentioned (`@reviewer ...`),
2. sent a direct message (`realm dm`),
3. the target of another bot's `delegate` / `send_dm` tool call, or
4. picked by the chat moderator.

In a `moderated` chat (the default), a message without `@` triggers **one**
moderator call (default model `haiku`, low effort, no tools, small prompt). It
returns one of:

| Decision | What runs |
| --- | --- |
| `route` | Only the owning bot. Two clearly separate requests may be split to 2 bots, each getting its part. |
| `ask` | Nothing. The moderator asks which bot; reply with a handle (`@db`) or `realm chat pick dev @db`. |
| `none` | Nothing (chit-chat, or no bot owns it). |

Picks, reroutes (`realm chat reroute dev <msg-id> @db`) and misroute refusals are
saved as routing hints in the moderator's knowledge and used on later calls.
`realm chat routing dev mention` turns the moderator off for a chat: only
`@mentions` reach bots, at zero model cost.

Mentions inside code blocks, inline code and quotes never trigger bots, and an
`@name` written in a bot's reply is display only.

Message envelope (SQLite row and SSE event):

```json
{"id":"…","chat":"my-app/dev","thread":"…","parent":"…","from":"@user",
 "kind":"text|command|inquiry|reply|refusal|question|system",
 "mentions":["@my-app/reviewer"],"hop":0,"text":"…","ts":"…"}
```

## Scope enforcement

- **Charter gate.** Every reply starts with a decision line: `act`, `refuse`
  (with a suggestion of who owns it), or `delegate` (Realm forwards the request;
  the target applies its own gate).
- **Access policy.**

| Actor | Global chats | Own project chats | Other projects' chats | Can message |
| --- | --- | --- | --- | --- |
| Global bot | yes | yes | yes | anyone |
| Project bot | no | yes | no | own project's bots, global bots |
| You | yes | yes | yes | anyone |

- **Hard limits.** `claude-cli` bots run with `--permission-mode dontAsk`, an
  explicit tool allowlist (default read-only `Read, Grep, Glob`) and
  `--strict-mcp-config`; never `--dangerously-skip-permissions`. Realm tools reach
  the daemon with a per-turn token, and the daemon checks the policy on every call.
- **Loop guards.** One turn at a time per bot, 3 concurrent turns overall, hop
  limit 4 for delegation chains, delegation cycles rejected.

Realm tools available to bots: `delegate` (ask another bot and wait),
`send_dm`, `post`, `read_chat`, `list_bots`, `remember`, `recall`, `forget`.
API-key bots also get read-only `read_file`, `grep`, `glob` confined to the
project folder.

## API-key providers

`~/.realm/config.yaml` (created by `realm init --global`):

```yaml
claude_bin: claude
moderator_model: haiku
providers:
  anthropic:  {type: anthropic,     base_url: https://api.anthropic.com,    api_key_env: ANTHROPIC_API_KEY}
  openai:     {type: openai-compat, base_url: https://api.openai.com/v1,    api_key_env: OPENAI_API_KEY}
  openrouter: {type: openai-compat, base_url: https://openrouter.ai/api/v1, api_key_env: OPENROUTER_API_KEY}
  ollama:     {type: openai-compat, base_url: http://localhost:11434/v1}
limits: {max_concurrent: 3, max_hops: 4, history_messages: 20, turn_timeout_sec: 600}
```

Keys are read from the named environment variables of the daemon process and
are never written to bot files.

```bash
realm bot create researcher --provider openrouter --model openai/gpt-5 \
  --charter "Summarizes papers the user links."
realm bot set @moderator --provider ollama --model llama3.2   # cheaper routing
```

## Commands

```
realm init [--global]                    realm status
realm daemon run|start|stop|status
realm bot create|list|show|set|on|off|archive|restore|inherit
realm knowledge list|show|add|forget <bot>
realm chat create|list|post|tail|join|pick|reroute|add|routing
realm dm <@bot> <text>
```

## Layout

```
cmd/realm                 CLI entry
internal/engine           routing, turns, tool calls (the core)
internal/router           moderator prompt and decision parsing
internal/gate             charter decision parsing
internal/policy           access policy (pure)
internal/runtime          claude-cli and API-key runners
internal/store            per-scope SQLite
internal/bot, knowledge   bot files and knowledge notes
internal/daemon, client   unix-socket HTTP API and client
internal/mcpserver        stdio MCP server exposing realm tools
```

Design decisions: [docs/adr](docs/adr).

## License

MIT
