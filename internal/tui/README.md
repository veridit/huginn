# Terminal task browser

The CLI calls `tui.Run(ctx, store, tui.Options{User: user, Commit: commit})`.
`Commit` runs after a successful changed mutation. Store refreshes expose external
file edits. Git synchronization and identity resolution belong to the caller.
The TUI does not claim a distributed take lock from a commit-only callback.

## Keys

| Key | Action |
| --- | --- |
| `j/k`, arrows | Select task; in detail select acceptance item |
| `enter`, `esc` | Open/close detail |
| `space` | Toggle selected acceptance item, default first item |
| `t`, `d`, `x` | Take, done, drop |
| `J/K` | Move down/up in the **global** rank order, including from filtered views |
| `n` | Prompt for title; create goal equal to title and one unchecked verification item |
| `e` | Run `$EDITOR` (default `vi`) on selected file, then refresh and lint |
| `s`, `y`, `g`, `l` | Prompt for exact status, type, group, label filter |
| `/` | Prompt for case-insensitive slug/title/id search |
| `c` | Clear all filters and search |
| `b`, `tab` | Toggle list/board |
| `PgUp/PgDown` | Scroll detail, board, or help |
| `r`, `?`, `q` | Refresh, help, quit |

Filter/search/title prompts submit with `enter` and cancel with `esc`. Empty
filter values clear that filter. Filters combine. Broken tasks remain visible,
with a lint marker and finding, and remain editable even without a valid id.
Detail shows all findings, goal, acceptance, resolved dependency status and notes.
Blocked completion errors stay inline, including the core's exact repair commands.
Control bytes from task files cannot inject terminal escape sequences.

## Standalone acceptance

`python3 scripts/tui-pty.py "$JCODE_SCRATCH_DIR/huginn/tui-pty"` builds
`internal/tui/cmd/pty-driver`, seeds synthetic tasks in a disposable store, drives
a real pseudo-terminal, and writes text dumps/raw transcript to the artifact path.
It checks navigation, persisted acceptance toggle and move, blocked completion,
board rendering, clean quit, alternate-screen exit, canonical input, and all
terminal attributes after the next canonical read. That read consumes BSD's
transient PENDIN state, rather than masking any attribute difference.

Model tests cover the key actions, malformed documents, editor confinement,
commit/editor failures, combined filters, tied ranks, global filtered moves, and
acceptance cursor bounds. `FuzzMalformedTaskView` feeds arbitrary Markdown bytes
through rendering and action handling.
