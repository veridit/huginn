# Backlog-md importer

```go
report, err := importer.Run(ctx, sourceDirectory, &task.Store{Root: huginnRoot}, importer.Options{DryRun: true})
```

`Report` and its per-file `Files` are JSON-ready. Counts describe destination task
writes, not metadata writes. Dry run performs parsing, resolution and merge but
creates no directories or files. Invalid source YAML, missing source ids and
nonregular Markdown files are skipped with findings. Duplicate source ids or
ambiguous destination identities stop the import instead of guessing. Cancellation
is checked between files. A partially completed import can safely be retried.

## Mapping decisions

* Canonical source identity is the trimmed uppercase source id. SHA-256 of
  `backlog:<SOURCE-ID>:<attempt>` supplies 30 bits encoded as six lowercase
  Crockford characters. Occupied ids are checked, and collisions retry the next
  attempt. Allocation is ordered by source ordinal, then source id. Existing
  aliases and persisted import baselines take precedence over generated ids.
* Source id becomes `aliases: [SOURCE-ID]`. The canonical id is both in the
  filename and the frontmatter. A title-derived six-word slug is generated only
  on first import. Existing paths, groups, slugs and ranks are never renamed.
* Exact statuses `To Do`, `In Progress`, `Done` become `todo`, `doing`, `done`.
  Other values are kept raw and remain lint-visible.
* The complete Description section becomes Goal. Acceptance Criteria becomes
  Acceptance, retaining checkbox states, item numbers, comments and markers.
  Only level-two headings outside code fences are translated. No goals or
  acceptance items are invented when the source lacks them. Other sections,
  including Implementation Notes, remain byte-for-byte unchanged.
* Frontmatter dependencies remain available verbatim. A new Depends on section
  contains canonical target ids, resolved through the whole input set and
  destination aliases. Missing targets keep their source reference and produce
  a per-file finding as well as ordinary dangling-dependency lint.
* Numeric ordinal sorts ascending, with source-id ties. Ranked tasks receive
  fixed-width twelve-digit position prefixes, so tied ordinals also preserve
  source-id order even though Huginn's general rank ties use canonical ids.
  Missing/invalid ordinals are unranked. Once imported, all path/rank changes
  belong to Huginn, including when upstream changes its ordinal. The original
  ordinal is kept in frontmatter. Unranked tasks use Huginn's usual id order.
* Only an exact case-insensitive `bug`, `docs`, `chore`, or `spike` label implies
  type. First matching label wins when several are present. Otherwise type is
  feature. `import_type_inference` records the chosen label or default reason.
  An original source type is retained separately as `backlog_type`.
* `created_date`/`updated_date` remain untouched and are additionally copied to
  `created`/`updated`. No wall-clock timestamp is added. All other frontmatter
  spans are preserved, including multiline values, nested unknown keys and
  comments. Known transformed fields use the core task editor.

## Re-import and local edits

This is a conservative three-way merge, not a destructive mirror. The previous
upstream-generated document is stored in `.backlog-import/<id>.json` under the
Huginn root. These are durable import baselines and should be committed on the
Huginn branch with tasks. They contain imported content, never put them on the
application source branch. They are not the disposable SQLite cache.

Each frontmatter field and each level-two section is a merge unit. If the local
unit still equals the previous upstream unit, an upstream edit replaces it. If
local differs, local wins even when upstream changed that same unit. Thus local
checkbox/note/goal edits protect the entire corresponding section. Local
removal of a field or section is also retained. New upstream fields/sections
are appended safely, with fields inside the YAML block. Source deletion never
deletes a destination task. Existing aliases without baselines are adopted
conservatively without changing task contents. Invalid local frontmatter is
left untouched. Missing or corrupt metadata is never used to overwrite local
content. Deleting a local alias alone does not create a duplicate task because
baselines retain the import identity.

Task writes and baseline writes use atomic replacement independently. A crash
between them can leave an old baseline, but the next merge conservatively keeps
any differing local unit. Unchanged tasks and unchanged baselines are not
written at all, preserving their bytes and mtimes. There is no cross-process
import lock. Callers must serialize concurrent mutations, as for local Store
operations. Destination symlinks and overlapping source/destination trees are
rejected. Source files are read only.

## Tests

Synthetic fixtures are inline in importer_test.go. `TestRealStatbus` copies
`~/ssb/statbus/.backlog/tasks` into a test temp directory outside the repository
before importing into another temp root. It skips only when that directory is
absent. Expected count comes from that snapshot: the design's original 178 has
grown to 181 as of 2026-11-05. It checks every alias, resolved/reported deps,
ranked ordinal order and SHA-256 identity of every destination file on rerun.
No real task content is committed to this package.
