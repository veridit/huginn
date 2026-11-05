# huginn: design draft

Huginn is Odin's raven of thought. muninn remembers; huginn keeps you heading
straight. A fast, crash-proof task tracker for people and agents, one markdown
file per task, kept on its own git branch.

Status: DRAFT. Q1-Q5 decided by the user 2026-11-05 13:36 (see Decisions).

## Decisions (user, 2026-11-05 13:36)

- **Built for AI first**, not the world's best ticket system. Standalone: install,
  run, use from the CLI. Same for muninn.
- **D1 statuses are fixed**: `todo | doing | done | dropped`. Not configurable.
- **D2 deps block by default** ("AI will often ignore warnings"). Configurable
  in `huginn.toml` as `deps = "block" | "warn"`. `take` and `done` refuse with
  exit code 3 and name the open deps.
- **D3 `next` exists, with filters**: `huginn next --type bug`, `--label`,
  `--mine`. Adds a `type:` field: `feature | bug | chore | spike | docs`,
  fixed list, each with a one-line description shown by `huginn types`.
- **D4 project memories move to the same branch**: muninn writes
  `memories/` in the `.huginn/` worktree (one shared branch, call it the
  project-notes branch). Main gets no memory commits, no churn, no pollution.
- **D5 TUI now, web UI later** for both tools. The web UI runs locally
  (`huginn serve`, `muninn serve`) so it can be opened from vendor apps
  (Claude, ChatGPT/Codex, ...) to view and edit tasks and memories.
- **Later, maybe**: a hosted service (small fee) to view, edit and sync without
  self-hosting. Its point: not being locked in to one company's tool. Separate
  design; first make a great tool and use it ourselves.
- Agent interface rule (from an earlier decision, muninn memory
  mem_1793616201092): non-interactive commands, `--json` output, clear exit
  codes and an agent instructions file matter more than MCP. Ship both.

## Evidence from real use (statbus backlog-md, 2026-11-05)

- 178 task files, 1.5 MB. 134 Done, 13 In Progress, 31 To Do. Done tasks stay
  in `tasks/` (0 in `completed/`), so listing scans every file.
- Filename embeds the title: `statbus-093 - <long-title>.md`. A title edit
  renames the file and breaks links.
- `ordinal:` for ordering: 168 files, 6 share `ordinal: 1`, 2 share `486204`.
  Ties are real; ordering needs a tie-break.
- Body sections people actually write: Acceptance Criteria 177, Description
  175, Implementation Notes 163, Zoom in 104, Zoom out 95, Closing review 91,
  North Star 87, Evidence 47.
- `priority:` high 95, medium 63, low 7. More than half are "high", so priority
  alone does not order work; rank does.
- `dependencies:` used (e.g. STATBUS-357 -> STATBUS-363).

## Principles (carried over from muninn)

1. Files are the truth; the SQLite cache is disposable and rebuilt from files.
2. Incremental: (mtime, size) skip, then content hash. Never reparse unchanged.
3. Never crash on bad input: a broken file is a lint finding, not a panic.
4. Atomic writes (temp + rename). A crash leaves the old or the new file.
5. One binary, pure Go, CGO off, brew/scoop/install.sh like muninn.
6. Agents and humans use the same files: CLI, MCP, TUI, or a text editor.

## Storage

- Branch `huginn` (orphan), checked out as a worktree at `.huginn/` in the repo.
- `huginn init` creates the branch (or fetches it), adds the worktree, and
  writes `.huginn/` into `.git/info/exclude`, so main never sees it and no
  `.gitignore` edit is needed.
- Layout inside the worktree:
  - `tasks/<id>.md`, one per task, done or not (status is a field, not a dir).
  - `huginn.toml` project config.
  - `docs/`, `decisions/` optional, same id scheme.
- `huginn sync`: commit local changes, `pull --rebase`, push. Only the huginn
  branch. Conflicts are per file and rare because each task is its own file.
- Memories live on the same branch under `memories/` (D4).

### Verified in a throwaway repo (git 2.54.0, 2026-11-05)

- `git worktree add --orphan -b huginn .huginn` plus `.huginn/` in
  `.git/info/exclude`: main `git status` stays empty, 0 files tracked on main.
- A second clone runs `git fetch origin huginn && git worktree add .huginn
  huginn` and sees the same tasks.
- Two people adding different task files: `pull --rebase` merges cleanly
  (3 commits, all 3 files present).
- `take` race: Alice and Bob both set the assignee of `k7m2qx` from the same
  base. Alice's push succeeds; Bob's is rejected (`! [rejected] ... (fetch
  first)`) and his rebase conflicts on that file. So `take` must: pull, check
  the assignee, write, commit, push; on rejection reset to the remote and
  report "taken by alice". Push is a real lock.

## Identity

- id: 6 chars of Crockford base32 (lowercase, no i/l/o/u), random. Example
  `k7m2qx`. 32^6 = 1.07e9 ids.
  - `new` checks every id it can see and retries, so a clash needs two people
    to create tasks between syncs. 100 unsynced tasks against 10k known ones:
    100 x 10k / 1.07e9 = 0.09 %.
  - A clash that still happens is caught by `lint` (duplicate id) at sync and
    fixed by `huginn reid <file>`.
- Never changes. File name = id. Title lives only inside the file.
- Short refs: any unique prefix works (`huginn show k7m`).
- Imported ids (`STATBUS-093`) kept as `aliases:`, resolvable everywhere.

## Ordering

- `rank:` a lexorank string (base62, `a0`, `a0V`, ...). Moving a task between
  two others writes a key between theirs: one file changes.
- Ties (seen in statbus) break by id, so order is total and stable.
- Rebalance only when a key grows past 12 chars, as one commit.
- `priority:` kept as a label for filtering, not for order.

## File format

```markdown
---
id: k7m2qx
title: Recall gate drops hits when embedding fails
status: todo            # todo | doing | done | dropped   (fixed, D1)
type: bug               # feature | bug | chore | spike | docs (fixed, D3)
rank: a0V
priority: high          # optional
assignee: []            # names; `take` sets it
labels: [recall, bug]
deps: [p3rt9a]          # ids or aliases
aliases: [STATBUS-093]  # optional
created: 2026-11-05
updated: 2026-11-05
---

## Goal
One paragraph: what done looks like and why.

## Acceptance
- [ ] observable check 1
- [ ] observable check 2

## Notes
Free text, evidence, decisions. Any other `##` sections are kept as-is.
```

- Required: id, title, status, rank, Goal, Acceptance with >= 1 item.
- `huginn lint` reports `file:line: problem` (missing field, unknown status,
  dangling dep, dep cycle, duplicate id, done with unchecked acceptance).
- Unknown frontmatter keys and sections round-trip untouched.

## Commands

| Command | Effect |
|---|---|
| `init` | branch + worktree + exclude |
| `new "title"` | create, rank at bottom (or `--top`, `--after id`) |
| `ls [--status --label --mine]` | ranked list, from cache |
| `show id` | print file |
| `edit id` | `$EDITOR`, then lint |
| `move id --before/--after/--top` | rerank, one file |
| `take id` | pull, set assignee + doing, commit, push (push is the lock) |
| `done id` | status done; warns on unchecked acceptance |
| `next [--type --label --mine]` | top unblocked, untaken todo (the compass) |
| `types` | the fixed types with one-line descriptions |
| `lint` | format check |
| `sync` | commit, pull --rebase, push |
| `import backlog <dir>` | idempotent, keeps old ids as aliases |
| `mcp` | stdio MCP: list/show/new/update/take/done/next/lint |
| `tui` | board + list, never crashes on bad files |

## Speed targets

- `ls` on 10k tasks: < 50 ms warm (cache), < 1 s cold.
- `new`/`move`/`done`: < 20 ms excluding git push.

## Open questions (proposed answers, awaiting the user)

- **Q6** branch name now that it holds tasks and memories. Proposal: `ravens`
  (Huginn and Muninn are Odin's ravens). Neutral for muninn-only users, and
  unlikely to clash with an existing branch, unlike `notes`. Worktree stays
  `.huginn/`... or `.ravens/` to match.
- **Q7** `next` ordering. Proposal: rank only; priority is a filter
  (`--priority high`). Evidence: statbus has priority high on 95 of 165 tasks,
  so priority does not separate work; rank does.

## Build plan (proposed lanes, needs approval before any spawn)

Phase 1, two lanes in parallel, disjoint write paths:

| Lane | Writes | Delivers | Gate |
|---|---|---|---|
| core | `internal/task`, `internal/id`, `internal/rank` | parse/serialise (round-trips unknown keys), lint with `file:line`, ids, lexorank, deps cycle check | fuzz: no panic on any bytes; round-trip byte-identical |
| git | `internal/gitstore` | init (orphan worktree + exclude), sync, `take` lock via push | the throwaway two-clone race above as a Go test |

Phase 2, three lanes in parallel, after phase 1 lands:

| Lane | Writes | Delivers | Gate |
|---|---|---|---|
| cli+mcp | `cmd/huginn`, `internal/mcp` | all commands, `--json`, exit codes (3 = blocked dep), stdio MCP | e2e script over the real binary |
| tui | `internal/tui` | list + board, never crashes on bad files | pty test like muninn's |
| import | `internal/importer` | backlog-md import, idempotent, aliases | the 178 statbus tasks import, re-run is a no-op |

Then: muninn writes `memories/` into the same branch (muninn ROADMAP).
Each lane reports with verbatim test output; the foreman reviews before merge.
