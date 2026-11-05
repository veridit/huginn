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
- Layout inside the worktree (D7, user 15:48: folders are groups):
  ```
  huginn.toml
  auth/                                         a group is a folder; nesting allowed
    a0.k7m2qx.recall-gate-drops-hits.md
    a1.p3rt9a.embedding-client-retries.md
  recall/
    a0V.m4xv2c.short-memory-ids.md
  k9zz1a.release-checklist.md                   no rank prefix = unranked (last)
  ```
  - **File name = `[<rank>.]<id>.<slug>.md`** (D9, user 15:48). Dots
    separate the parts because slugs contain dashes. **The id is the only
    thing that links anything together.** It is also in the frontmatter
    (`id:`), which wins if a file name gets mangled; `lint` flags a mismatch.
  - The file system is the interface. Each of these is a plain rename that
    keeps the id, so nothing breaks:
    - group: move the file to another folder (`huginn mv <id> <group>` or
      `git mv`, a file manager, an editor);
    - priority: change the rank prefix (`huginn move <id> --before <id>`
      writes a rank between the neighbours; a human can type `100.`, `150.`);
    - slug: rename it to anything, or let `huginn` follow the title.
  - Rank lives only in the file name, not in the frontmatter, so
    reprioritising changes no file content: git records a pure rename and
    two people reordering different tasks never conflict.
  - Ranks compare as plain strings across all folders, so `next` and `ls`
    give one global order; a group view is the same order, filtered.
  - Done tasks stay where they are; status is a field, not a folder.
  - `huginn.toml` project config.
- `huginn sync`: commit local changes, `pull --rebase`, push. Only the huginn
  branch. Conflicts are per file and rare because each task is its own file.
- Memories live on their own `muninn` branch (D6).

## Local cache (D8, user 15:48-15:49)

A small SQLite file mirrors the disk state so structure questions are
instant: what depends on this, what is blocked, `next`, `ls`, a group view.
The markdown tree is the only truth. **Delete the cache and the next command
rebuilds it from disk**; nothing is ever lost with it.

Every command first syncs the cache with disk, then answers from it.
Commands that change things (`move`, `mv`, `take`, `done`, retitle) work out
the right file edit or rename from the cache, apply it to disk, and update
the cache in the same step.

- Location: `.git/huginn/cache.db` (per clone, never committed, outside the
  worktree so the branch never sees it).
- Tables: `tasks` (id, path, group, rank, slug, title, status, type,
  priority, assignee, updated), `deps` (from, to, line), `aliases`, `files`
  (the change markers below), `meta` (schema version, worktree HEAD).
- **Change markers live in the cache, not in the files.** Putting a marker in
  each task file would make every sync rewrite files and churn git. Instead,
  per file the cache stores `(path, mtime_ns, size, sha256)`, same as muninn:
  1. The worktree HEAD equals the cached HEAD and no file mtime/size changed:
     nothing to do (one `stat` per file, about 1 ms for 1k tasks).
  2. mtime or size changed: hash it; if the hash changed, reparse that one
     file and update its rows (and only the deps edges that touch it).
  3. HEAD moved (pull, checkout): `git diff --name-status old..new` lists
     exactly the changed files, even if git preserved mtimes. A rename
     (new folder, rank or slug) only updates path/group/rank/slug: same id,
     no reparse, no other row touched.
  4. Schema version changed (new huginn): full rebuild.
- Every command runs the check first, so an edit made in a text editor or by
  another agent is visible on the next command. No daemon needed.

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
- Never changes. It is in the file name and the frontmatter; title, slug,
  folder and rank are all free to change around it.
- **Every task has two names (D10, user 15:50): the id for machines, the
  slug for humans.** `new` derives the slug from the title (lowercase,
  dashes, at most 6 words); it stays put when the title is later edited
  unless someone renames it.
  - **Humans see the slug.** TUI, web UI, and default CLI output lead with
    the slug and title; the id appears dimmed for copying
    (`recall-gate-drops-hits  Recall gate drops hits...  k7m2qx`).
  - **Under the hood it is always the id.** Files, dependencies, the cache,
    git commit messages, `--json`, MCP results, exit messages for agents,
    and every write path resolve to the id first and use only the id.
  - Input accepts either: an id, a unique id prefix, or a unique slug. A slug
    is resolved to its id once, at the edge; an ambiguous slug lists the
    matching `slug  id` pairs and exits 2.
- Short refs: any unique prefix works (`huginn show k7m`).
- Imported ids (`STATBUS-093`) kept as `aliases:`, resolvable everywhere.

## Ordering

- Rank is the optional file name prefix: any string, compared bytewise.
  huginn generates lowercase base36 lexorank keys (`0-9a-z`) between neighbours,
  so a move
  renames one file. Humans may use numbers; pad them (`100`, `150`).
- Ties (seen in statbus) break by id, so order is total and stable.
- **D19 (foreman 2026-11-05 23:31): generated ranks are lowercase base36.**
  Existing human-typed uppercase ranks retain bytewise ordering. Automatic
  repair rewrites touched ranks with lowercase keys, avoiding case-only name
  changes on macOS, Windows and git with `core.ignorecase`. Defensive case-only
  renames use a temporary name and verify source/destination identity. Lint
  reports filenames that collide under case folding.
- Rebalance only when a key grows past 12 chars, as one rename commit.
- `priority:` kept as a label for filtering, not for order.

## File format

```markdown
---
id: k7m2qx
title: Recall gate drops hits when embedding fails
status: todo            # todo | doing | done | dropped   (fixed, D1)
type: bug               # feature | bug | chore | spike | docs (fixed, D3)
priority: high          # optional
assignee: []            # names; `take` sets it
labels: [recall, bug]
aliases: [STATBUS-093]  # optional
created: 2026-11-05
updated: 2026-11-05
---

## Goal
One paragraph: what done looks like and why.

## Depends on
- p3rt9a Embedding client retries

## Acceptance
- [ ] observable check 1
- [ ] observable check 2

## Notes
Free text, evidence, decisions. Any other `##` sections are kept as-is.
```

- Required: id, title, status, Goal, Acceptance with >= 1 item.
- **Dependencies are the id only** (D9). Each `## Depends on` item starts
  with an id (or alias); the rest of the line is a free note for humans. No
  path is stored, so moving, renaming, re-slugging or reprioritising a task
  never touches any other file, and never makes a dependency stale. `huginn
  show` prints each dependency with its current title, status and path,
  resolved through the cache. Links elsewhere in the body are references,
  not dependencies.
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
| `done id` | status done; blocks (exit 3) on unchecked acceptance (D17) |
| `check id n` | tick acceptance item n (D13) |
| `note id "text"` | append to Notes (D13) |
| `set id --priority/--type/--label/--status/--title` | change one field (D13) |
| `drop id` | status dropped |
| `next [--type --priority --label --group --mine]` | top unblocked, untaken todo in rank order within the filter (D12) |
| `types` | the fixed types with one-line descriptions |
| `lint` | format check |
| `sync` | commit, pull --rebase, push |
| `import backlog <dir>` | idempotent, keeps old ids as aliases |
| `mcp` | stdio MCP: list/show/new/update/take/done/next/lint |
| `tui` | board + list, never crashes on bad files |

## Speed targets

- `ls` on 10k tasks: < 50 ms warm (cache), < 1 s cold.
- `new`/`move`/`done`: < 20 ms excluding git push.

## Decisions log continues above; open questions: none (2026-11-05 15:52)

- **D6 (user, 15:47) one branch per tool, not a shared `ravens` branch.**
  Tasks live on branch `huginn` (worktree `.huginn/`), memories on branch
  `muninn` (worktree `.muninn/`). Separate things stay separate, even though
  the same git-branch code drives both (one shared Go package, used by both
  binaries). Consequence for muninn: today `.muninn/memories.jsonl` is tracked
  on main; migration removes it from main in one commit and moves it onto the
  `muninn` branch. This supersedes D4's "same branch" wording.
- **D11 (user 15:52) rank stays in the file name.**
- **D12 (user 15:52) `next` orders by rank; filters narrow it.** `huginn
  next` returns the top-ranked unblocked, untaken todo. Filters combine:
  `--priority high`, `--type bug`, `--label recall`, `--group auth`,
  `--mine`. So "next high-priority bug" is `next --type bug --priority
  high`, still in rank order within that set. `--json` for agents; exit 4
  when nothing matches.

- **D13 (user 22:36) cheap models must drive it.** Luna, Haiku, Sonnet and
  DeepSeek Flash must use huginn correctly from the CLI or MCP alone, with
  no prompt beyond "use huginn". Rules that follow from it (muninn format
  tests 2026-11-05: cheap models call `help` readily, 24/24, but invent
  commands when only given a pointer, and mangle anything they must
  hand-format):
  1. **Self-guiding output.** `huginn` alone and `huginn help` print a
     short command list with one example each (under 40 lines). Every
     command's text output ends with one `next:` line naming the likely
     follow-up command, filled in with the real id
     (`next: huginn take k7m2qx`). `--json` carries it as `"next"`.
  2. **Errors say the fix.** One line of what is wrong, then the exact
     command that fixes it. Unknown values (status, type, flag) exit 2,
     list the valid values and the closest match; no silent guessing.
     A blocked dep (exit 3) names each open dep with its id and title.
  3. **No hand-formatting needed.** Everything an agent does is one
     non-interactive command with flat flags: `new "title" --type bug
     --goal "..." --accept "..." --accept "..." --dep k7m2qx --group auth`,
     `check <id> <n>` ticks acceptance item n, `note <id> "text"` appends
     to Notes, `set <id> --priority high --label x`. Editing the file by
     hand stays allowed; `lint` catches mistakes.
  4. **Forgiving where it is safe.** Ids, unique id prefixes, slugs and
     aliases all resolve, case-insensitively. Repeating a finished action
     is a no-op with exit 0 (`done` on a done task, `take` on your own).
  5. **Small, flat MCP.** At most ~10 tools, flat string/enum arguments,
     enums for status and type, one-sentence descriptions, results that
     carry the same `next` hint.
  6. **Gate: a cheap-model eval** (`scripts/agent-eval.py`). Scenarios
     run through `jcode run -m <model> --tools bash` in a throwaway repo
     with only "use huginn" in the prompt: create two tasks with a dep,
     find next, take, tick acceptance and finish, reorder, move group,
     handle a blocked dep. Scored on the final repo state (lint clean,
     expected fields), not on what the model says. Models: gpt-6-luna,
     claude-haiku-5-5, claude-sonnet-5-5, deepseek-flash. Bar: every
     model passes every scenario in at least 2 of 3 runs. Same scenarios
     again via MCP only.
- **D14 (foreman 22:40, reversible) the shared git package is public:**
  `github.com/veridit/huginn/gitbranch` (top-level, not `internal/`), so
  muninn can import it for its own `muninn` branch.
- **D15 (foreman 22:53, from D13) `move` always succeeds.** When no key fits
  between the neighbours (`A` vs `A0`, tied keys) or the key would pass 12
  chars, huginn re-keys the smallest contiguous window around the insertion
  point, as pure renames in the same commit, and reports the renamed ids.
  `huginn rebalance` exists for humans; agents never need it.
- **D16 (foreman 22:53) `next --mine`** = assigned to me, `doing` before
  `todo`, unblocked, then rank. Plain `next` stays untaken `todo` only.
- **D17 (foreman 22:53, user rule "agents ignore warnings, so block")**
  `done` with unchecked acceptance blocks, exit 3, listing each item with
  `huginn check <id> <n>`. `acceptance = "block" | "warn"` in huginn.toml.
  No `--force`. `drop` is always allowed.
- **D18 (foreman 22:53)** the cache stores dep edges by canonical id;
  unresolvable refs stay as dangling rows for lint.
- **D19 (foreman 23:32) names must be safe on case-insensitive file systems.**
  A tie repair produced ranks `A` and `a`, and macOS refused the case-only
  rename. huginn generates lowercase-only rank keys (base36, 0-9a-z, like the
  lowercase ids), renames to the same file via a temp name, and lint flags
  names that differ only by case. Human-typed uppercase ranks still read.
- **D20 (foreman 23:32)** the TUI takes an optional `Take` hook from the CLI
  so `t` goes through the same push-lock as `huginn take`.

## Build plan (approved 22:36, lanes started)

Phase 1, two lanes in parallel, disjoint write paths:

| Lane | Writes | Delivers | Gate |
|---|---|---|---|
| core | `internal/task`, `internal/id`, `internal/rank`, `internal/cache` | parse/serialise (round-trips unknown keys), `[rank.]id.slug.md` names, id+slug resolver, lexorank, deps by id + cycle check, lint `file:line`, SQLite cache synced from disk (markers in cache, rename = row update) | fuzz: no panic on any bytes; round-trip byte-identical; delete cache -> rebuild equals incremental; move/re-slug/rerank never edits another file |
| git | `gitbranch/` (public, shared with muninn, D14) | orphan branch + worktree + exclude, sync, `take` lock via push | the two-clone race as a Go test; same package drives a second branch name |

Phase 2, three lanes in parallel, after phase 1 lands:

| Lane | Writes | Delivers | Gate |
|---|---|---|---|
| cli+mcp | `cmd/huginn`, `internal/mcp` | all commands, `--json`, exit codes (3 = blocked dep), stdio MCP | e2e script over the real binary |
| tui | `internal/tui` | list + board, never crashes on bad files | pty test like muninn's |
| import | `internal/importer` | backlog-md import, idempotent, aliases | the 178 statbus tasks import, re-run is a no-op |

Phase 3: the cheap-model eval (D13 gate) over the real binary; failures
go back to the cli+mcp lane as wording/hint fixes until the bar holds.

Then: muninn moves `memories/` onto its own `muninn` branch via `gitbranch`
(muninn ROADMAP).
Each lane reports with verbatim test output; the foreman reviews before merge.
