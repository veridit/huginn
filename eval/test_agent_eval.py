"""State scorer and harness tests. No paid calls, no real huginn binary required."""
import copy
import importlib.util
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('agent_eval', ROOT / 'scripts/agent-eval.py')
eval = importlib.util.module_from_spec(spec)
spec.loader.exec_module(eval)
SCENARIOS = eval.validate(eval.load(ROOT / 'eval/scenarios.json'))
FIXTURES = eval.load(ROOT / 'eval/fixtures/expected-state.json')
BAD = eval.load(ROOT / 'eval/fixtures/failing-patches.json')


def state(scenario):
    return eval.fixture_state(copy.deepcopy(FIXTURES[scenario['id']]))


def replace(obj, path, value):
    for key in path[:-1]:
        obj = obj[key]
    obj[path[-1]] = value


class Scorers(unittest.TestCase):
    def test_fixture_coverage(self):
        self.assertEqual(set(FIXTURES), {s['id'] for s in SCENARIOS})
        self.assertEqual(set(BAD), set(FIXTURES))

    def test_all_required_fields_fail_closed(self):
        mutations = 0
        for s in SCENARIOS:
            good = state(s)
            for index, task in enumerate(s['expect']['tasks']):
                for key in task:
                    if key == 'title':
                        continue
                    broken = copy.deepcopy(good)
                    field = {'assigned':'assignee', 'notes_contain':'notes', 'depends_on_titles':'dependencies'}.get(key, key)
                    broken['tasks'][index if s['id'] != 'reorder' else 1-index].pop(field, None)
                    with self.subTest(scenario=s['id'], field=key):
                        self.assertFalse(eval.score(s, broken)['pass'])
                    mutations += 1
        self.assertGreater(mutations, 40)

    def test_d22_goal_only_normalizes_outer_whitespace_and_one_period(self):
        scenario = next(s for s in SCENARIOS if s['id'] == 'create-bug')
        for goal in ('Allow valid email addresses to sign in', '  Allow valid email addresses to sign in.\n', 'Allow valid email addresses to sign in. '):
            observed = state(scenario)
            observed['tasks'][0]['goal'] = goal
            self.assertTrue(eval.score(scenario, observed)['pass'], goal)
        for goal in ('Allow INVALID email addresses to sign in.', 'Allow valid email addresses to sign in!', 'Allow valid email addresses to sign in..', '', None, 1):
            observed = state(scenario)
            observed['tasks'][0]['goal'] = goal
            self.assertFalse(eval.score(scenario, observed)['pass'], goal)

    def test_d22_finish_accepts_only_unassigned_or_captured_actor(self):
        scenario = next(s for s in SCENARIOS if s['id'] == 'finish-by-slug')
        for assignee in ([], ['Acting User']):
            observed = state(scenario)
            observed['acting_user'] = 'Acting User'
            observed['tasks'][0]['assignee'] = assignee
            self.assertTrue(eval.score(scenario, observed)['pass'], assignee)
        for assignee in (['Other User'], ['Acting User', 'Other User'], [''], None):
            observed = state(scenario)
            observed['acting_user'] = 'Acting User'
            observed['tasks'][0]['assignee'] = assignee
            self.assertFalse(eval.score(scenario, observed)['pass'], assignee)
        observed = state(scenario)
        observed.pop('acting_user')
        observed['tasks'][0]['assignee'] = ['Acting User']
        self.assertFalse(eval.score(scenario, observed)['pass'])

    def test_d22_does_not_relax_assignee_in_other_scenarios(self):
        scenario = next(s for s in SCENARIOS if s['id'] == 'move-group')
        observed = state(scenario)
        observed['tasks'][0]['assignee'] = ['Huginn Eval']
        self.assertFalse(eval.score(scenario, observed)['pass'])

    def test_lint_findings_fail_every_scenario(self):
        for s in SCENARIOS:
            broken = state(s)
            broken['lint'] = [{'file':'bad.md', 'line':1, 'problem':'bad task'}]
            self.assertFalse(eval.score(s, broken)['pass'])

    def test_inventory_extra_missing_duplicate(self):
        for s in SCENARIOS:
            good = state(s)
            for tasks in (good['tasks'][:-1], good['tasks'] + [good['tasks'][0]],
                          good['tasks'] + [dict(good['tasks'][0], title='Unexpected', id='z00001')]):
                self.assertFalse(eval.score(s, dict(good, tasks=tasks))['pass'])

    def test_blocked_status_and_dependency_survive(self):
        s = next(s for s in SCENARIOS if s['id'] == 'blocked-finish')
        for path, value in [(['tasks',1,'status'], 'done'), (['tasks',0,'status'], 'done'),
                            (['tasks',1,'dependencies'], []), (['tasks',1,'notes'], 'blocked')]:
            broken = state(s)
            replace(broken, path, value)
            self.assertFalse(eval.score(s, broken)['pass'])

    def test_preserve_content_requires_real_evidence(self):
        for s in SCENARIOS:
            if s['expect'].get('preserve_content'):
                good = state(s)
                for after in ({}, {'wrong':'hash'}, None):
                    self.assertFalse(eval.score(s, dict(good, content_after=after))['pass'])
                good.pop('content_before')
                good.pop('content_after')
                self.assertFalse(eval.score(s, good)['pass'])

    def test_sync_requires_both_clean_and_matching_remote(self):
        s = next(s for s in SCENARIOS if s['id'] == 'sync')
        for key in ('remote_matches', 'worktree_clean'):
            self.assertFalse(eval.score(s, dict(state(s), **{key:False}))['pass'])

    def test_model_prose_cannot_change_verdict(self):
        for s in SCENARIOS:
            good = state(s)
            good['text'] = 'I failed'
            self.assertTrue(eval.score(s, good)['pass'])
            good['tasks'] = []
            good['text'] = 'All done, verified and pushed'
            self.assertFalse(eval.score(s, good)['pass'])

    def test_matrix_bar_requires_each_cell_two_of_three(self):
        s = SCENARIOS[:1]
        rows = [dict(tier='cli', scenario=s[0]['id'], model=eval.MODELS[0], **{'pass': p}) for p in (True, True, False)]
        self.assertTrue(eval.table(rows, s, eval.MODELS[:1], 3, ['cli'])[1])
        rows[1]['pass'] = False
        self.assertFalse(eval.table(rows, s, eval.MODELS[:1], 3, ['cli'])[1])
        self.assertFalse(eval.table(rows, s, eval.MODELS[:2], 3, ['cli'])[1])
        self.assertFalse(eval.table(rows[:1], s, eval.MODELS[:1], 1, ['cli'])[1])


# Separate named tests keep the pass/fail evidence for every scenario visible.
def pass_test(s):
    def test(self):
        verdict = eval.score(s, state(s))
        self.assertTrue(verdict['pass'], verdict)
    return test


def fail_test(s):
    def test(self):
        broken = state(s)
        for item in BAD[s['id']]:
            replace(broken, item['path'], item['value'])
        self.assertFalse(eval.score(s, broken)['pass'])
    return test


for scenario in SCENARIOS:
    setattr(Scorers, 'test_pass_' + scenario['id'].replace('-', '_'), pass_test(scenario))
    setattr(Scorers, 'test_fail_' + scenario['id'].replace('-', '_'), fail_test(scenario))


class Harness(unittest.TestCase):
    def test_model_invocations_have_no_command_documentation(self):
        for model in eval.MODELS:
            for tier in ('cli', 'mcp'):
                cmd = eval.model_command(model, tier, Path('/repo'), 'Take my next task.')
                self.assertEqual(cmd[:7], ['jcode','run','-m',model,'--quiet','--json','--tools'] if tier == 'cli'
                                 else ['jcode','run','-m',model,'--quiet','--json','--disable-base-tools'])
                self.assertEqual(cmd[cmd.index('--tools')+1], 'bash' if tier == 'cli' else 'mcp')
                self.assertEqual(cmd[-1], 'Take my next task.\n\n' +
                                 ('Use the huginn CLI.' if tier == 'cli' else 'Use huginn.'))
                if tier == 'mcp':
                    self.assertNotIn('bash', cmd)
                    self.assertIn('--disable-base-tools', cmd)

    @unittest.skipUnless(shutil.which('jcode'), 'jcode not installed')
    def test_real_jcode_headless_project_mcp_discovery(self):
        with tempfile.TemporaryDirectory(dir=os.environ.get('JCODE_SCRATCH_DIR')) as td:
            root = Path(td)
            wrapper = root / 'probe'
            wrapper.write_text(f'#!{sys.executable}\nimport os,sys\nos.execv(sys.executable, [sys.executable, {str(ROOT / "eval/fixtures/mcp-probe.py")!r}])\n')
            wrapper.chmod(0o755)
            env = dict(os.environ)
            eval.isolate_mcp(root, env, wrapper, 'mcp')
            enabled = {k for k, v in eval.inventory(env, root).items() if v == 'enabled'}
            self.assertEqual(enabled, {'huginn-eval'})

    def test_session_journal_deduplicates_commands_and_sums_usage(self):
        with tempfile.TemporaryDirectory() as td:
            sessions = Path(td) / 'sessions'
            sessions.mkdir()
            first = {'id':'m1', 'token_usage':{'input_tokens':10,'output_tokens':2},
                     'content':[{'type':'tool_use','name':'bash','input':{'command':'huginn help'}}]}
            second = {'id':'m2', 'token_usage':{'input_tokens':20,'output_tokens':3},
                      'content':[{'type':'tool_use','name':'mcp__huginn__list','input':{}}]}
            eval.dump(sessions / 'test.json', {'messages':[first]})
            (sessions / 'test.journal.jsonl').write_text(json.dumps({'append_messages':[first,second]}) + '\n')
            data = eval.session_telemetry('test', {'JCODE_HOME':td})
            self.assertEqual(data['session_tokens'], {'input_tokens':30,'output_tokens':5})
            self.assertEqual(data['commands'], ['huginn help'])
            self.assertEqual(len(data['tool_calls']), 2)

    def test_wrapper_records_help_unknown_exit_and_stdout(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            fake = root / 'real-huginn'
            fake.write_text(f'#!{sys.executable}\nimport sys\nprint("response")\nif "invented" in sys.argv:\n print("unknown command invented", file=sys.stderr)\n sys.exit(2)\n')
            fake.chmod(0o755)
            wrapper = root / 'huginn'
            wrapper.write_text(eval.WRAPPER.format(python=sys.executable, log=str(root / 'calls'), binary=str(fake)))
            wrapper.chmod(0o755)
            for args in (['help'], ['invented']):
                p = subprocess.run([str(wrapper), *args], capture_output=True, text=True)
                self.assertEqual(p.stdout, 'response\n')
            calls = [json.loads(x) for x in (root / 'calls').read_text().splitlines()]
            self.assertEqual(calls[0]['args'], ['help'])
            self.assertFalse(calls[0]['unknown_command'])
            self.assertEqual(calls[1]['exit'], 2)
            self.assertTrue(calls[1]['unknown_command'])

    def test_mcp_isolation_failure_stops_before_handshake(self):
        with tempfile.TemporaryDirectory() as td:
            with patch.object(eval, 'inventory', side_effect=[{'other':'enabled'}, {'other':'enabled'}]):
                with self.assertRaisesRegex(RuntimeError, 'MCP isolation failed'):
                    eval.isolate_mcp(Path(td), {}, Path('/huginn'), 'mcp')

    def test_mcp_project_config_only_exposes_huginn(self):
        with tempfile.TemporaryDirectory() as td:
            with patch.object(eval, 'inventory', side_effect=[{'other':'enabled'}, {'other':'disabled', 'huginn-eval':'enabled'}]), patch.object(eval, 'run') as run:
                env = {}
                eval.isolate_mcp(Path(td), env, Path('/huginn'), 'mcp')
                cfg = eval.load(Path(td) / '.mcp.json')['mcpServers']
                self.assertTrue(cfg['other']['disabled'])
                self.assertEqual(cfg['huginn-eval'], {'command':'/huginn', 'args':['mcp']})
                self.assertEqual(env['JCODE_DISABLE_CLAUDE_MCP'], '1')
                self.assertEqual(run.call_args.args[0], ['jcode', 'mcp', 'test', 'huginn-eval'])

    def test_seed_bytes_and_rank_hash_stable_on_rename(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            eval.write_seed(root, eval.seed_task(SCENARIOS[2]['seed'][0]))
            before = eval.hashes(root)
            file = next(root.glob('*.md'))
            (root / 'auth').mkdir()
            file.rename(root / 'auth' / file.name.replace('a0.', 'Zz.', 1))
            self.assertEqual(before, eval.hashes(root))
            self.assertIn('a00001', before)

    def test_session_commands_extracted_without_prose(self):
        with tempfile.TemporaryDirectory() as td:
            sessions = Path(td) / 'sessions'
            sessions.mkdir()
            eval.dump(sessions / 'session_test.json', {'messages':[{'content':[{'type':'tool_use','name':'bash','input':{'command':'huginn help'}}, {'type':'text','text':'I ran made-up stuff'}]}]})
            commands, available = eval.commands_from_session('session_test', {'JCODE_HOME':td})
            self.assertEqual(commands, ['huginn help'])
            self.assertTrue(available)


if __name__ == '__main__':
    unittest.main(verbosity=2)
