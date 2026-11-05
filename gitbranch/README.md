# gitbranch

Public reusable Git worktree storage for `github.com/veridit/huginn/gitbranch`.
Requires the system Git executable, version **2.42 or newer**. Older Git returns
`*Error` with the detected version and an OS-specific upgrade command. On Linux,
the distribution's Git package must actually provide >= 2.42.

```go
r, err := gitbranch.Open(repoPath, "huginn", ".huginn",
    gitbranch.WithIdentity("Alice", "alice@example.invalid"))
// For muninn use the identical API with branch "muninn", directory ".muninn".
```

`Open` and `Init` are aliases. `repoPath` may point at the host checkout root,
a subdirectory, or a linked checkout of main. The worktree directory is relative
to that checkout root, not the process cwd. The Git common directory's
`info/exclude` receives an idempotent anchored exclude. A remote branch is fetched
before its first checkout. An existing local branch is preserved rather than
silently reset. Existing worktrees are reopened without modifying their HEAD.
The remote is `origin` when present, otherwise the first remote reported by Git.

`WithIdentity(name, email)` sets author and committer only for child Git commands.
Without it, ordinary Git identity configuration applies. No host config, main
index, main files, or main branch ref is changed. Git hooks remain enabled.

## API

- `Repository.Dir`: absolute branch worktree path for caller file I/O.
- `Head() (string, error)`: object ID, or empty string for an unborn branch.
- `DiffNameStatus(old, new string) ([]Change, error)`: rename-aware, NUL-delimited
  parsing. `Change.Status` includes similarity (`R100`), `OldPath` is the rename
  source, `Path` the destination. Empty `old` lists files in the new commit tree.
- `Commit(paths []string, message string) error`: literal paths relative to `Dir`,
  including deletions and renames. Nil/empty means all worktree changes. No-op
  when nothing changed. Pre-staged changes are refused, so an explicit path list
  cannot accidentally include another staged file. Neither absolute paths nor
  parent traversal is accepted.
- `Sync() error`: commit local worktree changes, fetch and rebase onto the remote
  branch, push this branch only. It does not pull or push main.
- `Update(fn func(*Repository) error) error`: requires a clean branch worktree,
  pulls first, invokes caller's check/write callback, commits, then pushes.
  The callback must not run Git and should write atomically. Its errors are
  returned unchanged and any callback edits remain available for inspection.
  A clean but unpublished local history is included in the push, so callers
  should Sync unrelated work before using Update for a take operation.

## Push as the lock

Check the assignee inside `Update`, not before calling it. On non-fast-forward
push rejection, the handle fetches the winner and resets the branch worktree to
that remote commit. `errors.As(err, &raced)` with `var raced *gitbranch.ErrRaced`
returns `RemoteHead` and `Dir`. Read the task from `raced.Dir` to report its actual
winning assignee. The rejected local commit is not replayed. The successful push
is the lock acquisition point. The callback may run even when acquisition fails.

`*ConflictError` lists conflicted paths. The package aborts its rebase before
returning, leaving the pre-rebase local commits intact and no conflict markers.
Its `Fix` command starts a manual rebase; resolve the listed files, `git add`
them, and `git rebase --continue`, or `git rebase --abort` to cancel.
`*Error` wraps other Git/filesystem errors with `Op`, `Detail`, `Fix`, and `Cause`.
Git failures include the exact retry command. Authentication, hook, and transport
failures are not automatically classified as lost races and do not reset local
commits. Do not retry a non-idempotent callback blindly after such a failure.

## Local-only mode and concurrency

With no remote configured, initialization, Commit, Sync and Update work locally.
Sync and Update commit but never fetch or push. **Update is not a cross-clone lock
in local-only mode.** Configure a remote to coordinate multiple machines.

A handle is not safe for concurrent calls. Serialize all access to the same
worktree within a clone, including caller file edits. Separate clones use the
remote's atomic ref update to arbitrate take races. Different tool branches are
independent and can coexist in one host checkout. Do not rename or switch the
branch beneath a handle. The caller owns the safety and atomicity of callback
file operations.

## Tests

The Go tests invoke Git directly with argv, never bash. They create real temp
repositories and bare remotes, covering a synchronized two-clone take race,
different-file rebase, same-file conflict abort, two independent tool branches,
main cleanliness, idempotent init, actual subdirectory cwd, linked host worktrees,
local-only updates, explicit commit paths, and rename detection. User/global Git
configuration is isolated in tests for Linux/macOS/Windows CI.

```
go vet ./gitbranch/...
go test -race ./gitbranch/...
```
