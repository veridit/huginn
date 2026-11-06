# huginn

<img src="docs/logo.svg" alt="Huginn, raven of thought" width="160">

A git-branch task tracker for people and agents. Markdown is the source of truth,
Git is the sync protocol, and the CLI, terminal UI and MCP server share the same
operations. Sister of [muninn](https://github.com/veridit/muninn), the raven of memory.
Huginn is the raven of thought: what to do next, why, and how to know it is done.
Licensed under MPL-2.0.

## Install

Requires Git 2.42 or newer on PATH. No database service or account needed.

```sh
brew install veridit/tap/huginn
```

```powershell
scoop bucket add veridit https://github.com/veridit/homebrew-tap
scoop install veridit/huginn
```

```sh
curl -fsSL https://raw.githubusercontent.com/veridit/huginn/main/install.sh | sh
# Optional: HUGINN_VERSION=v0.1.0 HUGINN_BIN_DIR=$HOME/.local/bin
```

Or with Go (see go.mod for the required toolchain):

```sh
go install github.com/veridit/huginn/cmd/huginn@latest
huginn version
```

## 60-second quickstart

Run inside an existing Git checkout. Set `git config user.name` or `HUGINN_USER`
for task ownership. With an origin, `take` claims through a remote push lock.
Without an origin, everything works locally.

```sh
huginn init
huginn new "Ship greeting" --goal "Visitors see a greeting" --accept "Greeting test passes"
huginn next
huginn take ship-greeting
# Implement and test the change, then record the observed check:
huginn check ship-greeting 1
huginn done ship-greeting
huginn sync
```

Use `huginn tui` for an interactive view, `huginn help COMMAND` for flags.
Dependencies and unchecked acceptance items block completion. `drop` preserves
history instead of deleting a task. Ranking and priority are separate concepts.

## Files, branches and groups

Tasks are `[rank.]id.slug.md`, with a stable six-character ID and readable slug.
They live on the dedicated `huginn` branch, mounted as the `.huginn/` Git worktree
inside your checkout. Folders are groups, not statuses. `huginn mv ID auth` moves
a task to the auth group. `huginn move ID --before OTHER` changes its rank.
Edit Markdown directly or use the CLI. Mutations commit the task branch, not
your application branch. `sync` commits, rebases and pushes the task branch.
The SQLite cache under the Git common directory is disposable.

## Agents

Use `huginn COMMAND --json` for structured replies. Every reply includes a
`next` hint. Errors include an actionable `fix` and `exit_code`.

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | Operational error |
| 2 | Usage error |
| 3 | Blocked |
| 4 | No next task |

`huginn mcp` speaks MCP over stdio. Configure a server named `huginn` with
`{"command":"huginn","args":["mcp"]}` and start it in the project checkout.
For Jcode, installation keeps backups and preserves other MCP servers:

```sh
huginn install jcode --dry-run
huginn install jcode
huginn install jcode --all-instances
huginn install jcode --jcode-home /path/to/.jcode
huginn uninstall jcode
```

Restart the agent after changing configuration. The `huginn` executable must be
on the agent's PATH. MCP tools use the same validation, completion checks and
next-step hints as the CLI.

## Import backlog-md

```sh
huginn import backlog ./backlog/tasks
huginn lint
huginn ls
```

The source is read-only. Source IDs remain aliases, dependencies are resolved,
and repeat imports conservatively preserve local edits. See
[importer details](internal/importer/README.md) for mapping and merge rules.

## Agent evaluation

D13 cheap-model gate at `26e82b5`: **238/240**, every cell at least **2/3**.
Four models, ten scenarios, three trials, CLI and MCP. State-only grading,
concurrency 4. These are the measured results, not a guarantee for other models.

# Huginn agent eval

Bar: every cell >= 2/3. State only. Concurrency 4.

## cli

| Model | create-bug | dependency | next-take | finish-by-slug | blocked-finish | reorder | move-group | drop-duplicate | priority-bug | sync |
|---|---|---|---|---|---|---|---|---|---|---|
| gpt-6-luna | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| claude-haiku-5-5 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| claude-sonnet-5-5 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| deepseek-flash | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |

## mcp

| Model | create-bug | dependency | next-take | finish-by-slug | blocked-finish | reorder | move-group | drop-duplicate | priority-bug | sync |
|---|---|---|---|---|---|---|---|---|---|---|
| gpt-6-luna | 3/3 | 3/3 | 3/3 | 3/3 | 2/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| claude-haiku-5-5 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| claude-sonnet-5-5 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| deepseek-flash | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 | 2/3 | 3/3 | 3/3 | 3/3 |

PASS

## Design

See [DESIGN.md](DESIGN.md) for architecture and design decisions.
