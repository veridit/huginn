#!/usr/bin/env python3
"""D13 state-only cheap-model gate. Python 3 standard library, MPL-2.0."""
import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
MODELS = ('gpt-6-luna', 'claude-haiku-5-5', 'claude-sonnet-5-5', 'deepseek-flash')
ACCEPTANCE = ['Regression test passes', 'Existing behavior preserved']


def load(path):
    return json.loads(Path(path).read_text())


def dump(path, value):
    Path(path).write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')


def slug(title):
    return '-'.join(re.findall(r'[a-z0-9]+', title.lower())[:6])


def seed_task(seed):
    return dict(id=seed['id'], title=seed['title'], slug=slug(seed['title']),
                status='todo', type=seed.get('type', 'chore'),
                priority=seed.get('priority', 'medium'), assignee=[],
                group='', rank=seed.get('rank', ''), goal=seed['title'], notes='',
                dependencies=seed.get('dependencies', []),
                acceptance=[dict(text=t, checked=seed.get('checked', False)) for t in ACCEPTANCE])


def validate(scenarios):
    ids = set()
    for s in scenarios:
        assert re.fullmatch('[a-z0-9-]+', s['id']) and s['id'] not in ids, 'duplicate/unsafe scenario id'
        ids.add(s['id'])
        assert isinstance(s['prompt'], str) and s['prompt'].strip()
        assert 'huginn ' not in s['prompt'], 'prompt must not teach commands'
        assert s['expect']['tasks'], 'scenario needs exact inventory'
        titles = [t['title'] for t in s['expect']['tasks']]
        assert len(titles) == len(set(titles))
        seed_ids = [t['id'] for t in s['seed']]
        assert len(seed_ids) == len(set(seed_ids))
        for t in s['seed']:
            assert re.fullmatch('[0-9a-hjkmnp-tv-z]{6}', t['id'])
            assert all(d in seed_ids for d in t.get('dependencies', []))
        for t in s['expect']['tasks']:
            assert all(x in titles for x in t.get('depends_on_titles', []))
        if 'order' in s['expect']:
            assert set(s['expect']['order']) == set(titles)
    return scenarios


def score(scenario, state):
    """Only observed task JSON, lint, file hashes and git facts enter this function."""
    failures = []
    tasks = state.get('tasks', [])
    expected = scenario['expect']
    by_title = {t.get('title'): t for t in tasks}
    if len(by_title) != len(tasks) or set(by_title) != {t['title'] for t in expected['tasks']}:
        failures.append('exact task inventory')
    if state.get('lint') != []:
        failures.append('lint clean')
    ids = [t.get('id') for t in tasks]
    if any(not x for x in ids) or len(set(ids)) != len(ids):
        failures.append('unique task ids')
    seeds = {t['title']: seed_task(t) for t in scenario['seed']}
    for want in expected['tasks']:
        title = want['title']
        got = by_title.get(title, {})
        baseline = seeds.get(title, {})
        # Seed fields not intentionally changed must survive, including identity/deps.
        wanted = dict(baseline, **want)
        if title in seeds:
            wanted.pop('notes', None)
            if 'assigned' in want:
                wanted.pop('assignee', None)
            if expected.get('order'):
                wanted.pop('rank', None)
        for key, value in wanted.items():
            if key == 'assigned':
                ok = 'assignee' in got and bool(got['assignee']) == value
            elif key == 'notes_contain':
                ok = all(x.casefold() in got.get('notes', '').casefold() for x in value)
            elif key == 'depends_on_titles':
                dep_ids = [by_title.get(x, {}).get('id') for x in value]
                ok = None not in dep_ids and got.get('dependencies') == dep_ids
            else:
                ok = got.get(key) == value
            if not ok:
                failures.append(f'{title}: {key}')
    if 'order' in expected:
        # ls ordering and on-disk ranks must agree, including id tie-breaks.
        ordered = sorted(tasks, key=lambda t: (not bool(t.get('rank')), t.get('rank', ''), t.get('id', '')))
        if [t.get('title') for t in tasks] != expected['order'] or [t.get('title') for t in ordered] != expected['order']:
            failures.append('global rank order')
    if expected.get('preserve_content') and state.get('content_before') != state.get('content_after'):
        failures.append('task content preserved')
    if expected.get('preserve_content') and not state.get('content_before'):
        failures.append('content evidence missing')
    if expected.get('pushed') and not (state.get('remote_matches') is True and state.get('worktree_clean') is True):
        failures.append('huginn branch pushed with clean worktree')
    return {'pass': not failures, 'failures': failures}


def run(args, cwd, env=None, timeout=60, check=True):
    p = subprocess.Popen([str(x) for x in args], cwd=cwd, env=env, text=True,
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                         start_new_session=True)
    try:
        stdout, stderr = p.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(p.pid, signal.SIGKILL)
        p.communicate()
        raise
    p = subprocess.CompletedProcess(args, p.returncode, stdout, stderr)
    if check and p.returncode:
        raise RuntimeError(f'{args[0]} exited {p.returncode}: {p.stderr.strip()}')
    return p


def git(repo, *args, check=True):
    return run(['git', *args], repo, check=check).stdout.strip()


def write_seed(worktree, task):
    p = worktree / f"{task['rank']}.{task['id']}.{task['slug']}.md"
    front = '\n'.join(f'{k}: {json.dumps(task[k])}' for k in
                      ('id', 'title', 'status', 'type', 'priority', 'assignee'))
    deps = ''.join(f'- {d}\n' for d in task['dependencies'])
    checks = ''.join(f"- [{'x' if x['checked'] else ' '}] {x['text']}\n" for x in task['acceptance'])
    p.write_text(f'---\n{front}\ncreated: 2026-11-05\nupdated: 2026-11-05\n---\n\n## Goal\n{task["goal"]}\n\n## Depends on\n{deps}\n## Acceptance\n{checks}\n## Notes\n')


def hashes(worktree):
    out = {}
    for p in worktree.rglob('*.md'):
        # filename identity is immutable in these preservation scenarios
        parts = p.name.split('.')
        task_id = parts[-3] if len(parts) >= 4 else parts[0]
        out[task_id] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def inventory(env, cwd):
    p = run(['jcode', 'mcp', 'list'], cwd, env)
    entries = re.findall(r'^\s+(\S+) \[[^\]\n]*; (enabled|disabled)\]', p.stdout, re.M)
    if not entries and 'Configured MCP servers' not in p.stdout and 'No MCP servers' not in p.stdout:
        raise RuntimeError('Cannot verify MCP isolation: unrecognized jcode mcp list output')
    return {name: status for name, status in entries}


def isolate_mcp(repo, env, wrapper, tier):
    # Source: jcode-base/src/mcp/protocol.rs load_for_dir, is_enabled.
    # Keep normal provider authentication but disable every unrelated server locally.
    env['JCODE_DISABLE_CLAUDE_MCP'] = '1'
    names = inventory(env, repo)
    servers = {name: {'command': '/usr/bin/false', 'disabled': True} for name in names}
    if tier == 'mcp':
        servers['huginn-eval'] = {'command': str(wrapper), 'args': ['mcp']}
    dump(repo / '.mcp.json', {'mcpServers': servers})
    observed = inventory(env, repo)
    enabled = {k for k, v in observed.items() if v == 'enabled'}
    if enabled != ({'huginn-eval'} if tier == 'mcp' else set()):
        raise RuntimeError(f'MCP isolation failed, enabled server names: {sorted(enabled)}')
    if tier == 'mcp':
        # Stops this run before any paid model call when MCP cannot handshake headlessly.
        run(['jcode', 'mcp', 'test', 'huginn-eval'], repo, env, timeout=45)


def preflight_mcp(binary, root):
    """No paid call: stop the entire tier if the real stdio server cannot start."""
    folder = Path(tempfile.mkdtemp(prefix='mcp-preflight-', dir=root))
    repo = folder / 'repo'
    repo.mkdir()
    git(folder, 'init', '--bare', str(folder / 'remote.git'))
    git(repo, 'init', '-b', 'main')
    git(repo, 'config', 'user.name', 'Huginn Eval')
    git(repo, 'config', 'user.email', 'eval@example.invalid')
    git(repo, 'config', 'commit.gpgsign', 'false')
    git(repo, 'config', 'core.hooksPath', '/dev/null')
    git(repo, 'remote', 'add', 'origin', str(folder / 'remote.git'))
    git(repo, 'commit', '--allow-empty', '-m', 'Eval MCP preflight')
    git(repo, 'push', '-u', 'origin', 'main')
    env = dict(os.environ, GIT_TERMINAL_PROMPT='0')
    run([binary, 'init'], repo, env)
    isolate_mcp(repo, env, Path(binary), 'mcp')
    return str(folder)


WRAPPER = '''#!{python}
import json, os, subprocess, sys, time
args = sys.argv[1:]
t = time.monotonic()
if args == ['mcp']:
    with open({log!r}, 'a') as f:
        f.write(json.dumps({{'args': args, 'transport': 'stdio'}}) + '\\n')
    sys.exit(subprocess.call([{binary!r}] + args))
p = subprocess.run([{binary!r}] + args, capture_output=True)
with open({log!r}, 'a') as f:
    f.write(json.dumps({{'args': args, 'exit': p.returncode, 'latency_s': time.monotonic()-t,
        'unknown_command': p.returncode == 2 and b'unknown command' in (p.stderr+p.stdout).lower()}}) + '\\n')
sys.stdout.buffer.write(p.stdout)
sys.stderr.buffer.write(p.stderr)
sys.exit(p.returncode)
'''


def session_messages(session_id, env):
    home = Path(env.get('JCODE_HOME', str(Path.home() / '.jcode')))
    if not re.fullmatch(r'[A-Za-z0-9_-]+', session_id or ''):
        return [], False
    path = home / 'sessions' / (session_id + '.json')
    if not path.exists():
        return [], False
    messages = load(path).get('messages', [])
    journal = path.with_suffix('.journal.jsonl')
    if journal.exists():
        for line in journal.read_text().splitlines():
            messages.extend(json.loads(line).get('append_messages', []))
    # Snapshot and journal may overlap during shutdown. Message ids deduplicate.
    unique = {}
    for index, message in enumerate(messages):
        unique[message.get('id', f'position-{index}')] = message
    return list(unique.values()), True


def session_telemetry(session_id, env):
    commands, calls = [], []
    messages, available = session_messages(session_id, env)
    totals = {}
    for message in messages:
        for key, value in (message.get('token_usage') or {}).items():
            if isinstance(value, int):
                totals[key] = totals.get(key, 0) + value
    def walk(value):
        if isinstance(value, dict):
            if value.get('type') == 'tool_use' and value.get('name'):
                inp = value.get('input', value.get('arguments', {}))
                if isinstance(inp, str):
                    try:
                        inp = json.loads(inp)
                    except ValueError:
                        inp = {}
                calls.append({'name': value['name'], 'input': inp})
                if value['name'] == 'bash' and isinstance(inp, dict) and isinstance(inp.get('command'), str):
                    commands.append(inp['command'])
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)
    walk(messages)
    return dict(commands=commands, tool_calls=calls, session_trace_available=available,
                session_tokens=totals or None)


def commands_from_session(session_id, env):
    telemetry = session_telemetry(session_id, env)
    return telemetry['commands'], telemetry['session_trace_available']


def observe(binary, repo, before):
    def query(*args):
        return json.loads(run([binary, *args, '--json'], repo).stdout)
    listing = query('ls')
    rows = listing if isinstance(listing, list) else listing['tasks']
    tasks = []
    for row in rows:
        shown = query('show', row['id'])
        tasks.append(shown.get('task', shown))
    lint_proc = run([binary, 'lint', '--json'], repo, check=False)
    lint = json.loads(lint_proc.stdout)
    findings = lint if isinstance(lint, list) else lint['findings']
    if lint_proc.returncode and not findings:
        raise RuntimeError('lint failed without findings')
    local = git(repo / '.huginn', 'rev-parse', 'HEAD')
    remote = git(repo, 'ls-remote', 'origin', 'refs/heads/huginn').split()
    return dict(tasks=tasks, lint=findings, content_before=before,
                content_after=hashes(repo / '.huginn'),
                remote_matches=bool(remote) and remote[0] == local,
                worktree_clean=not git(repo / '.huginn', 'status', '--porcelain'))


def model_command(model, tier, repo, prompt):
    cmd = ['jcode', 'run', '-m', model, '--quiet', '--json']
    # An empty allow-list also hides MCP in jcode. Explicit mcp admits only the
    # unified MCP dispatcher and configured MCP tools, never bash/read/write.
    cmd += (['--tools', 'bash'] if tier == 'cli' else
            ['--disable-base-tools', '--tools', 'mcp', '--mcp-tools', 'eager'])
    return cmd + ['-C', str(repo), prompt + '\n\nUse the huginn CLI.']


def job(scenario, model, repeat, tier, args, root):
    folder = Path(tempfile.mkdtemp(prefix=f'{tier}-{scenario["id"]}-{model}-{repeat}-', dir=root))
    repo = folder / 'repo'
    repo.mkdir()
    result = dict(scenario=scenario['id'], model=model, run=repeat, tier=tier,
                  directory=str(folder), pass_=False)
    start = time.monotonic()
    env = dict(os.environ)
    # Do not inherit the parent agent's base-tool allow-list or exclusions.
    env['JCODE_TOOLS'] = ''
    env['JCODE_DISABLED_TOOLS'] = ''
    # Force deterministic, noninteractive git identity only inside the throwaway clone.
    env['GIT_TERMINAL_PROMPT'] = '0'
    try:
        git(folder, 'init', '--bare', str(folder / 'remote.git'))
        git(repo, 'init', '-b', 'main')
        git(repo, 'config', 'user.name', 'Huginn Eval')
        git(repo, 'config', 'user.email', 'eval@example.invalid')
        git(repo, 'config', 'commit.gpgsign', 'false')
        git(repo, 'config', 'core.hooksPath', '/dev/null')
        git(repo, 'remote', 'add', 'origin', str(folder / 'remote.git'))
        git(repo, 'commit', '--allow-empty', '-m', 'Eval initial commit')
        git(repo, 'push', '-u', 'origin', 'main')
        run([args.binary, 'init'], repo, env)
        for seed in scenario['seed']:
            write_seed(repo / '.huginn', seed_task(seed))
        git(repo / '.huginn', 'add', '.')
        git(repo / '.huginn', 'commit', '--allow-empty', '-m', 'Eval seed tasks')
        git(repo / '.huginn', 'push', '-u', 'origin', 'huginn')
        before = hashes(repo / '.huginn')
        # Check seed schema through the actual binary before letting a model run.
        initial = observe(args.binary, repo, before)
        if initial['lint']:
            raise RuntimeError('Seed tasks fail lint: ' + json.dumps(initial['lint']))
        bindir = folder / 'bin'
        bindir.mkdir()
        wrapper = bindir / 'huginn'
        log = folder / 'huginn-calls.jsonl'
        wrapper.write_text(WRAPPER.format(python=sys.executable, log=str(log), binary=args.binary))
        wrapper.chmod(0o755)
        env['PATH'] = str(bindir) + os.pathsep + env.get('PATH', '')
        isolate_mcp(repo, env, wrapper, tier)
        # Config is outside task branch, excluded so no accidental main changes matter.
        with (repo / '.git/info/exclude').open('a') as f:
            f.write('\n.mcp.json\n')
        cmd = model_command(model, tier, repo, scenario['prompt'])
        result['invocation'] = cmd
        model_start = time.monotonic()
        proc = run(cmd, repo, env, timeout=args.timeout, check=False)
        result['model_latency_s'] = time.monotonic() - model_start
        # Do not store or score model prose.
        output = json.loads(proc.stdout)
        result['jcode_exit'] = proc.returncode
        result['usage'] = output.get('usage')
        result['usage_scope'] = 'jcode run last_usage (not assumed whole-session total)'
        result['actual_model'] = output.get('model')
        result.update(session_telemetry(output.get('session_id'), env))
        calls = [json.loads(x) for x in log.read_text().splitlines()] if log.exists() else []
        result['huginn_calls'] = calls
        result['called_help'] = (any(any(a in ('help', '--help', '-h') for a in c['args']) or not c['args'] for c in calls)
                                 if tier == 'cli' else None)
        result['invented_commands'] = [c for c in calls if c.get('unknown_command')] if tier == 'cli' else None
        state = observe(args.binary, repo, before)
        dump(folder / 'state.json', state)
        result.update(score(scenario, state))
        # Transport failures do not turn an untouched blocked scenario into a pass.
        if proc.returncode:
            result['pass'] = False
            result['failures'].append('jcode process failed')
    except Exception as exc:
        result.update({'pass': False, 'error': str(exc)})
    result.pop('pass_', None)
    result['latency_s'] = time.monotonic() - start
    dump(folder / 'result.json', result)
    return result


def table(results, scenarios, models, runs, tiers):
    lines = ['# Huginn agent eval', '', f'Bar: every cell >= 2/{runs}. State only. Concurrency 4.', '']
    passed = runs == 3
    for tier in tiers:
        lines += [f'## {tier}', '', '| Model | ' + ' | '.join(s['id'] for s in scenarios) + ' |',
                  '|---|' + '---|' * len(scenarios)]
        for model in models:
            cells = []
            for scenario in scenarios:
                rows = [r for r in results if r['tier'] == tier and r['model'] == model and r['scenario'] == scenario['id']]
                k = sum(r.get('pass', False) for r in rows)
                passed &= k >= 2 and len(rows) == runs
                cells.append(f'{k}/{runs}')
            lines.append('| ' + model + ' | ' + ' | '.join(cells) + ' |')
        lines.append('')
    lines.append('PASS' if passed else 'FAIL (or non-gating sample with fewer than 3 runs)')
    return '\n'.join(lines) + '\n', passed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dry', action='store_true', help='validate scenarios and handwritten fixtures, no binary/models')
    parser.add_argument('--binary', default=str(ROOT / 'huginn'))
    parser.add_argument('--runs', type=int, default=3)
    parser.add_argument('--models', nargs='+', choices=MODELS, default=list(MODELS))
    parser.add_argument('--tier', choices=('cli', 'mcp', 'both'), default='both')
    parser.add_argument('--scenario', action='append', help='restrict by scenario id (repeatable)')
    parser.add_argument('--timeout', type=int, default=600)
    args = parser.parse_args()
    scenarios = validate(load(ROOT / 'eval/scenarios.json'))
    if args.scenario:
        unknown = set(args.scenario) - {s['id'] for s in scenarios}
        if unknown:
            parser.error('unknown scenario: ' + ', '.join(sorted(unknown)))
        scenarios = [s for s in scenarios if s['id'] in args.scenario]
    if args.runs < 1:
        parser.error('--runs must be positive')
    if args.dry:
        fixtures = load(ROOT / 'eval/fixtures/expected-state.json')
        for scenario in scenarios:
            state = fixture_state(fixtures[scenario['id']])
            verdict = score(scenario, state)
            if not verdict['pass']:
                raise SystemExit(f'{scenario["id"]}: {verdict}')
            print(f'{scenario["id"]}: PASS (handwritten fixture)')
        print(f'DRY PASS: {len(scenarios)} scenarios, no model calls')
        return 0
    args.binary = str(Path(args.binary).resolve())
    if not os.access(args.binary, os.X_OK):
        parser.error('build the binary first: go build -o huginn ./cmd/huginn')
    scratch = os.environ.get('JCODE_SCRATCH_DIR')
    if not scratch:
        parser.error('JCODE_SCRATCH_DIR must be set')
    root = Path(scratch) / 'huginn-eval'
    root.mkdir(parents=True, exist_ok=True)
    out = Path(scratch) / 'huginn/eval'
    out.mkdir(parents=True, exist_ok=True)
    tiers = ['cli', 'mcp'] if args.tier == 'both' else [args.tier]
    blocked = {}
    if 'mcp' in tiers:
        try:
            preflight_mcp(args.binary, root)
        except Exception as exc:
            blocked['mcp'] = str(exc)
            print('MCP tier STOPPED before model calls: ' + str(exc), file=sys.stderr)
    jobs = [(s, m, n, tier) for tier in tiers if tier not in blocked
            for s in scenarios for m in args.models for n in range(1, args.runs + 1)]
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        futures = [pool.submit(job, s, m, n, tier, args, root) for s, m, n, tier in jobs]
        results = [dict(tier=tier, scenario=s['id'], model=m, run=n,
                        **{'pass':False, 'blocked':reason})
                   for tier, reason in blocked.items() for s in scenarios
                   for m in args.models for n in range(1, args.runs + 1)]
        dump(out / 'results.json', results)
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
            print(f"{result['tier']} {result['model']} {result['scenario']} #{result['run']}: {'PASS' if result['pass'] else 'FAIL'}", flush=True)
            dump(out / 'results.json', results)
    markdown, passed = table(results, scenarios, args.models, args.runs, tiers)
    (out / 'results.md').write_text(markdown)
    print(markdown)
    return 0 if passed else 1


def fixture_state(fixture):
    """Handwritten fixtures use seed defaults for repetitive, unchanged task fields."""
    state = dict(fixture)
    state['tasks'] = [dict(seed_task(t), **t) for t in fixture['tasks']]
    state.setdefault('lint', [])
    return state


if __name__ == '__main__':
    sys.exit(main())
