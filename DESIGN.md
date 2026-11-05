# huginn: design draft

Huginn is Odin's raven of thought. muninn remembers; huginn keeps you heading
straight. A fast, crash-proof task tracker for people and agents, one markdown
file per task, kept on its own git branch.

Status: DRAFT for discussion. Open questions are marked **Q**.

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
- **Q** memories: should `muninn sync` move `.muninn/memories.jsonl` onto the
  same branch (a `memories/` dir), so main stays clean too?

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
status: todo            # todo | doing | done | dropped   (Q: configurable?)
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
| `next` | top unblocked, untaken todo (the compass) |
| `lint` | format check |
| `sync` | commit, pull --rebase, push |
| `import backlog <dir>` | idempotent, keeps old ids as aliases |
| `mcp` | stdio MCP: list/show/new/update/take/done/next/lint |
| `tui` | board + list, never crashes on bad files |

## Speed targets

- `ls` on 10k tasks: < 50 ms warm (cache), < 1 s cold.
- `new`/`move`/`done`: < 20 ms excluding git push.

## Open questions

- **Q1** statuses fixed or per project?
- **Q2** deps: `take` blocks or warns when deps are not done?
- **Q3** `next` as the agent compass: yes/no, and does it skip `priority`?
- **Q4** memories on the same branch?
- **Q5** web UI like muninn, or TUI only at first?
