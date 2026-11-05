# D13 cheap-model gate

Python 3 standard library only. No task binary or model credentials are needed for:

```sh
python3 scripts/agent-eval.py --dry
python3 -m unittest discover -s eval -v
```

The dry run validates the ten scenarios against independently handwritten final
states in `fixtures/expected-state.json`. Repetitive fixture fields use documented
seed defaults in `seed_task`. `fixtures/failing-patches.json` defines one specific
negative final state for every scenario. Tests also remove required fields, add
lint findings, corrupt inventory, change blockers, lose file content, and test
remote mismatch and dirty worktree separately. None of these are live model
results.

Once the CLI lane lands:

```sh
go build -o "$JCODE_SCRATCH_DIR/huginn/huginn" ./cmd/huginn
python3 scripts/agent-eval.py --binary "$JCODE_SCRATCH_DIR/huginn/huginn"
```

Defaults: both CLI and MCP, exactly `gpt-6-luna`, `claude-haiku-5-5`,
`claude-sonnet-5-5`, `deepseek-flash`, three repeats, concurrency four. This is
240 real model runs. The runner uses existing jcode provider authentication and
never copies credentials. Use `--tier cli`, `--models gpt-6-luna`, `--runs 1`, or
`--scenario create-bug` for diagnosis. A one-run sample cannot pass the bar.
A filtered matrix is only evidence for the selected scope, not the full D13 gate.

## Scenarios

1. Create a bug with a goal and exactly two acceptance items.
2. Create API and form tasks, with the form depending on the API's immutable id.
3. Discover the next available task and take it, leaving the other task alone.
4. Address a task by its slug, tick both acceptance items, finish it.
5. Try to finish a task with an open dependency, leave both statuses alone, record
   the blocker in a task note. This makes the report observable in final state,
   without grading the model's prose or its chosen command sequence.
6. Put one existing task above another, preserving file contents byte-for-byte.
7. Move a task to `auth`, preserving id, rank, status and file contents.
8. Drop a duplicate without deleting either task or closing the original.
9. Take the next high-priority bug, ignoring higher-ranked features and low bugs.
10. Create a release chore and sync it to the bare remote.

## Observable contract for the CLI lane

The design does not pin JSON keys. The initial adapter is deliberately explicit:

* `huginn ls --json`: `{"tasks":[{"id":"..."}, ...]}` or a bare array. This
  inventory must include all statuses, including done and dropped.
* `huginn show ID --json`: a task object or `{"task":{...}}`. Task keys:
  `id`, `title`, `slug`, `status`, `type`, `priority`, `assignee` (array),
  `group` (empty string for root), `rank`, `goal`, `notes`, `dependencies`
  (array of ids), `acceptance` (array of `{text, checked}`). Extra fields and
  `next` hints are ignored, not rejected.
* `huginn lint --json`: `{"findings":[]}` or a bare array. Nonempty findings
  fail every scenario. Failure to query or decode JSON is an infrastructure
  error, never a pass.

`observe` is the only JSON adapter. If the CLI chooses different keys, coordinate
and change that adapter and adapter tests, not the state expectations. Seed
markdown is written using the exact DESIGN file format and linted with the real
binary before a model is called. Generated ids need not match fixtures, but
seeded ids, dependencies and all unchanged seeded fields must survive.

The scorer only receives observed task JSON, lint, content hashes and git facts.
No model text is retained or scored. Sync requires the bare remote's huginn ref
to equal local worktree HEAD and the worktree to be clean. No real remote is
used. Each run has its own repo and bare remote under
`$JCODE_SCRATCH_DIR/huginn-eval/` and is retained for diagnosis. Setup is outside
the model prompt. The prompt is the scenario's user request plus exactly
`Use the huginn CLI.` with no command documentation. Whether the model actually
used a slug, a particular command, or read help is telemetry, not a state score.

## MCP-only discovery and isolation

Installed jcode v0.99.1 has headless project-local MCP configuration support:
`jcode run -C REPO` loads `REPO/.mcp.json`. The source inspected is
`jcode-base/src/mcp/protocol.rs` (`load_for_dir`, `is_enabled`) and
`jcode-base/src/config/env_overrides.rs`. A temporary project config disables
all global server names using stdio stubs with `disabled: true`, then exposes
only `huginn-eval: {command: /absolute/path/to/huginn, args: [mcp]}`.
`JCODE_DISABLE_CLAUDE_MCP=1` prevents live external Claude MCP imports.
No global config is modified. The runner checks `jcode mcp list` after the
local override and refuses to continue if any unwanted server is enabled.

MCP calls use `--disable-base-tools --tools mcp --mcp-tools eager`.
The explicit MCP allow-list is necessary: an empty base-tool selection also hides
MCP tools in jcode. It exposes the MCP dispatcher and configured server tools,
not bash/read/write. This follows `tool/name_policy.rs::tool_name_is_allowed`.
`JCODE_TOOLS` and `JCODE_DISABLED_TOOLS` are cleared in the child environment so
an inherited allow-list cannot re-enable bash. A real `jcode mcp test
huginn-eval` handshake runs before the tier starts. If headless discovery or the
real server fails, the entire MCP tier stops before any model call and records
blocked rows, not substitute scores. Per-run checks protect the individual repo.
The protocol-only `mcp-probe.py` fixture proves jcode discovery and isolation
without a huginn binary. It is explicitly not a task server or a fake eval pass.
The literal CLI wording in the MCP prompt is awaiting coordinator clarification.

## Results and instrumentation

* `$JCODE_SCRATCH_DIR/huginn/eval/results.json`: incremental results, errors,
  CLI invocations, actual reported model, token fields, tool calls and latency.
* `results.md`: separate per-tier model-by-scenario `k/3` matrices, minimum two
  successful final states in each cell. Full gate command above covers all cells.
* Each run directory: `result.json`, `state.json`, bare remote, repo, and
  `huginn-calls.jsonl`.

A transparent PATH wrapper records real CLI argv, exit codes, help use and
unknown commands (exit 2 **and** an `unknown command` diagnostic). Scoring invokes
the real binary directly so evaluator queries do not inflate the model's help
count. Model shell commands and MCP calls are extracted from that run's jcode
session snapshot plus append journal, deduplicated by message id. Whole-session
stored token usage is summed separately from the `jcode run` JSON `usage`, which
is explicitly labeled last-usage rather than a total. Missing session data is
reported as unavailable. MCP has no CLI help or unknown-command exit metric,
so those two fields are null for that tier. Tool inputs are telemetry only.
Timeouts kill the isolated child process group, including spawned stdio servers.
