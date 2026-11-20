# Usage guide

Step-by-step flows for everyday work. Each flow shows the CLI. Where the web UI
(`realm ui`) or terminal UI (`realm tui`) does the same thing, the steps are
noted. Endpoint and tool details are in [API.md](API.md).

## 1. Install and set up

1. Install Go 1.27 or later.
2. For subscription bots, install the `claude` CLI and log in.
3. Install Realm:

   ```bash
   go install github.com/blackmo18/realm-ai/cmd/realm@latest
   ```

4. Create the global home (`~/.realm`, or `$REALM_HOME`) and the default `config.yaml`:

   ```bash
   realm init --global
   ```

5. Check the setup:

   ```bash
   realm status
   ```

The daemon starts by itself when a command needs it. To control it directly:

```bash
realm daemon start
```

```bash
realm daemon status
```

```bash
realm daemon stop
```

`realm daemon run` keeps it in the foreground, with logs on the terminal.

## 2. Add a project

A project is a folder, usually a git repo. Its bots and chats live in
`<project>/.realm`.

**CLI.** Run `realm init` inside the folder:

```bash
cd ~/code/my-app
realm init
```

The project's scope is the folder name, here `my-app`. Its bots are addressed
as `@my-app/<name>`.

**Web UI.**

1. Run `realm ui` and click **Add project** in the sidebar footer.
2. Pick a suggested folder or type a path. The wizard shows whether the folder is a git repo, has a `CLAUDE.md`, and has project skills.
3. Choose whether to add `.realm/realm.db` to `.gitignore`. This is recommended, because chat history should not go into git.
4. Choose starter bots: `reviewer`, `docs`, `tester`.
5. Choose global bots to add to the first chat.
6. Name the first chat and choose its routing.

**TUI.** Press `p` in the scope tree. The steps match the web wizard.

**Claude Code.** With the admin MCP set up (see [section 11](#11-let-claude-code-manage-realm)),
ask: "add ~/code/my-app to Realm with a reviewer and a tester".

## 3. Create bots

Bots are created in the current project unless you pass `--global`.

```bash
realm bot create reviewer --model sonnet --effort high --topics go \
  --charter "Reviews Go code in this repo. Refuses everything else."

realm bot create db --model haiku --topics schema,sql \
  --charter "Owns the database schema and migrations."

realm bot create ops --global --model haiku --topics deploy \
  --charter "Answers deployment questions for all projects."
```

Write the charter as what the bot owns and what it refuses. The bot acts only
on requests inside it, and refuses the rest with a suggestion of who owns it.

Options:

| Flag | Default | Notes |
| --- | --- | --- |
| `--provider` | `claude-cli` | Or a provider name from `config.yaml`. See [section 9](#9-use-an-api-key-provider). |
| `--model` | provider default | `haiku`, `sonnet`, `opus`, or a provider model id. |
| `--effort` | runtime default | `low`, `medium`, `high`, `xhigh`, `max`. |
| `--topics` | none | Help the moderator route. |
| `--skills` | none | Allowed skills. `"*"` allows every skill visible at the bot's scope. |
| `--tools` | `Read,Grep,Glob` | Built-in tools for `claude-cli` bots. |
| `--inherit` | none | Copy knowledge from an existing bot. |

> **Tool warning.** `Edit`, `Write` and `Bash` give the bot your user's full
> file and shell access. Grant them only to bots whose charter and inputs you
> trust.

**Web UI:** **Bots**, then **New bot**. **TUI:** press `b` for the bots pane, then `a`.

Inspect and change bots:

```bash
realm bot list            # this project's bots and global bots
realm bot list --global   # every bot in every project
realm bot show @reviewer
realm bot set @reviewer --model opus --effort high
realm bot set @db --tools Read,Grep,Glob,Edit
```

## 4. Talk to one bot directly

```bash
realm dm @reviewer "is internal/auth/jwt.go idiomatic?"
```

`realm dm` waits for the reply and prints it. A short `@name` resolves in the
current project first, then in the global scope. Use the full handle to be
explicit: `@global/ops`.

**Web UI:** open the bot and use its DM panel. **TUI:** in the bots pane, press `d`.

## 5. Group chats

Create a chat in the current project. It includes every active project bot.

```bash
realm chat create dev
```

Add global bots, or choose mention-only routing, when you create it:

```bash
realm chat create dev --members @global/ops --routing moderated
```

Add a global bot to an existing chat:

```bash
realm chat add dev @global/ops
```

Project bots cannot join global chats or other projects' chats. A global chat
takes global bots only:

```bash
realm chat create lobby --global
```

Join the chat interactively:

```bash
realm chat join dev
```

In `join`, type a message to post it. These commands are also available:

| Command | Does |
| --- | --- |
| `/pick @bot` | Answer the moderator's latest question. |
| `/reroute <message-id> @bot` | Send a message to a different bot. |
| `/quit` | Leave. |

Scripted alternatives:

```bash
realm chat post dev "@db add an index on users.email"
realm chat tail dev --history 50
realm chat list
```

**Web UI:** click `+` next to a scope in the sidebar to create a chat. In a chat, type `@` to open the bot picker. Press `Enter` to send and `Shift+Enter` for a new line.

**TUI:** in the scope tree, press `n` to create a chat and `Enter` to open one.

## 6. How messages reach bots

A bot runs only when it is one of these:

1. mentioned with `@name`
2. sent a DM
3. the target of another bot's `delegate` or `send_dm`
4. picked by the moderator

### Mentions

```
@db add an index on users.email
@reviewer @tester check the new handler
```

Each mentioned bot gets the message. A mention inside a code block, inline code
or a quote does not trigger a bot.

### Moderated routing (default)

A message without `@` goes to the moderator. It makes one cheap call and then
does one of these:

| Moderator decision | What you see | What to do |
| --- | --- | --- |
| route | A `system` line: `→ @db (schema change)`, then the bot's reply. | Nothing. |
| route, split | Two routing lines. Each bot gets its part of the message. | Nothing. |
| ask | A `question` message with bot options. | Reply with a bare `@db`, click a pick button, press `1`–`9` in the TUI, or run `realm chat pick dev @db`. |
| none | Nothing runs. | Mention a bot if you meant one. |

### Fix a misroute

When the moderator routes to the wrong bot, send the message to the right one:

```bash
realm chat reroute dev <message-id> @db
```

The web UI has **Reroute to…** on each message. In the TUI, press `r` with an
empty composer.

Picks, reroutes and misroute refusals are saved as routing hints. The moderator
reads them on every later call, so routing improves.

### Mention-only routing

Turn the moderator off for a chat. Only `@mentions` reach bots, at no model
cost:

```bash
realm chat routing dev mention
```

Switch back with `realm chat routing dev moderated`. The web UI has a
**Routing** toggle in the chat header. In the TUI, press `m` with an empty
composer.

## 7. Delegation between bots

Bots pass work to each other with the `delegate` tool, or their decision line.

Example: in a project chat, ask the project's `reviewer` about deployment.

1. `@my-app/reviewer` sees deployment is outside its charter.
2. It delegates to `@global/ops`, which owns deploys.
3. `@global/ops` checks its own charter, answers, and the answer goes back to the reviewer.
4. The reviewer replies in the chat.

The access rules apply:

- Project bots can reach their own project's bots and global bots.
- Global bots can reach anyone.

Loop guards also apply:

- The hop limit is 4.
- Delegation cycles are rejected.
- A turn can make at most 3 `delegate` or `send_dm` calls.
- One user message can cause at most 12 bot turns.

## 8. Knowledge

Each bot has its own notes. An index of them goes into every turn.

Bots save notes themselves with the `remember` tool. You can manage them too:

```bash
realm knowledge add @reviewer "Wrap errors with %w; never log and return." --title "Error style"
realm knowledge list @reviewer
realm knowledge show @reviewer <id>
realm knowledge forget @reviewer <id>
```

`forget` is permanent.

**Web UI:** open the bot; its knowledge list has add, edit and delete. **TUI:** in the bots pane, press `Enter` or `K`.

### Turn bots off, archive them, inherit knowledge

```bash
realm bot off @db       # queues messages; answers them when turned back on
realm bot on @db        # dispatches the queue
realm bot archive @db   # rejects messages; knowledge is kept
realm bot restore @db
```

Start a new bot with an old bot's knowledge:

```bash
realm bot create db2 --inherit @db --charter "Owns the schema and migrations."
```

Copy knowledge into an existing bot:

```bash
realm bot inherit @db2 --from @db
```

The copy is recorded in the bot's `lineage`.

**TUI keys in the bots pane:**

| Key | Action |
| --- | --- |
| `o` | Toggle on and off, or restore an archived bot. |
| `x` | Archive. Press `y` to confirm. |
| `i` | Inherit knowledge. |

## 9. Use an API-key provider

1. Check that the provider is in `~/.realm/config.yaml`. The defaults are `anthropic`, `openai`, `openrouter` and `ollama`.
2. Export the provider's key variable in the shell that starts the daemon. Realm reads keys from the daemon's environment and never writes them to bot files.

   ```bash
   export OPENROUTER_API_KEY=...
   realm daemon stop && realm daemon start
   ```

3. Create a bot with that provider, or switch an existing one:

   ```bash
   realm bot create researcher --provider openrouter --model openai/gpt-5 \
     --charter "Summarizes papers the user links."
   realm bot set @moderator --provider ollama --model llama3.2
   ```

The web UI **Settings** page shows each provider and whether its key variable
is set.

API-key bots get the realm tools and, in a project, read-only `read_file`,
`grep` and `glob` tools confined to the project folder. They do not get
`claude` built-in tools or skills.

## 10. Web UI and terminal UI

```bash
realm ui
```

`realm ui` prints a `http://127.0.0.1:PORT/#token=…` URL and opens it. Use
`--no-open` to print it only. The token changes each time the daemon starts.
If the page says live updates stopped, run `realm ui` again.

```bash
realm tui
```

TUI keys:

| Where | Keys |
| --- | --- |
| Scope tree | `↑`/`↓` or `j`/`k` move, `Enter` opens, `Tab` switches pane, `n` new chat, `p` add project, `b` bots, `q` quit |
| Chat | `Enter` sends, `Alt+Enter` new line. With an empty composer: `1`–`9` pick bot, `r` reroute, `m` routing. `Esc` goes back. |
| Bots pane | `a` add, `o` on/off, `x` archive, `i` inherit, `Enter`/`K` knowledge, `d` DM, `Esc` back |
| Forms | `Tab` next field, `←`/`→` choose, `Space` toggle, `Ctrl+S` save, `Esc` cancel |

`Ctrl+C` quits from anywhere.

## 11. Let Claude Code manage Realm

Register the admin MCP server once:

```bash
claude mcp add realm -- realm mcp-admin
```

Then ask Claude Code in plain language. For example:

- "Create a global bot called ops on haiku that answers deployment questions."
- "Add @global/ops to the my-app/dev chat."
- "DM @my-app/reviewer and ask whether jwt.go is idiomatic."
- "Turn off @my-app/db."

The tools act as you. Archiving a bot and forgetting a note are marked
destructive, so Claude Code asks before it runs them. The full tool list is in
[API.md](API.md#admin-mcp-tools).

## Troubleshooting

| Symptom | Check |
| --- | --- |
| `claude not found on PATH` on the Settings page | Install the `claude` CLI, or set `claude_bin` in `config.yaml`, then restart the daemon. |
| A bot never replies | `realm bot show @bot`: is it `off` or `archived`? With `realm daemon run`, watch the logs for turn errors. |
| The moderator keeps asking | Add clearer `--topics` and charters, or answer a few questions: picks become routing hints. |
| A bot refuses everything | Its charter is too narrow. Widen it with `realm bot set @bot --charter "..."`. |
| `a realm daemon is already running` | Another daemon owns the socket. Run `realm daemon stop` first. |
| Web UI shows 401 | The daemon restarted, so the token changed. Run `realm ui` again. |
