#!/usr/bin/env bash
# Real-binary acceptance, isolated repositories and a real bare remote only.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
if [ "$#" -gt 0 ]; then
  binary="$1"
else
  scratch="${JCODE_SCRATCH_DIR:-${TMPDIR:-/tmp}}"
  binary="$scratch/huginn-e2e-binary"
  (cd "$root" && go build -o "$binary" ./cmd/huginn)
fi
python3 - "$binary" <<'PY'
import json, os, pathlib, pty, select, signal, subprocess, sys, tempfile, time
binary=str(pathlib.Path(sys.argv[1]).resolve())
base=pathlib.Path(tempfile.mkdtemp(prefix='huginn-e2e-',dir=os.environ.get('JCODE_SCRATCH_DIR')))
env=dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=str(base/'no-global'), GIT_AUTHOR_NAME='Test', GIT_COMMITTER_NAME='Test', GIT_AUTHOR_EMAIL='test@example.invalid', GIT_COMMITTER_EMAIL='test@example.invalid', HUGINN_USER='tester', TERM='xterm-256color')
def git(cwd,*args):
    return subprocess.run(['git','-C',str(cwd),*args],env=env,text=True,capture_output=True,check=True).stdout.strip()
repo=base/'a';repo.mkdir();remote=base/'remote.git'
git(base,'init','--bare',str(remote));git(repo,'init','-b','main');git(repo,'commit','--allow-empty','-m','init');git(repo,'remote','add','origin',str(remote));git(repo,'push','-u','origin','main');git(remote,'symbolic-ref','HEAD','refs/heads/main')
def run(*args,code=0,cwd=repo,json_mode=True,extra=None):
    argv=[binary,*args]+(['--json'] if json_mode else [])
    p=subprocess.run(argv,cwd=cwd,env=dict(env,**(extra or {})),text=True,capture_output=True,timeout=30)
    assert p.returncode==code,(args,p.returncode,p.stdout,p.stderr)
    if json_mode:
        result=json.loads(p.stdout);assert result.get('next'),(args,result);return result
    lines=(p.stdout if code==0 else p.stderr).strip().splitlines()
    assert (lines[-1].startswith('next: huginn ') if code==0 else len(lines)==2 and lines[-1].startswith('fix: ')),(args,lines)
    return p.stdout
run(json_mode=False);help_text=run('help',json_mode=False);assert len(help_text.splitlines())<40
run('types');run('init');run('init');run('next',code=4);run('nex',code=2);run('next','--typ','bug',code=2);run('ls','--status','don',code=2);run('next','--type','bugs',code=2)
a=run('new','API','--goal','API ready','--accept','First','--accept','Second','--type','bug','--priority','high','--label','api')['task']['id']
b=run('new','Form','--goal','Form ready','--accept','UI works','--dep',a)['task']['id']
assert run('show',b)['task']['dependencies']==[a]
run('dep',b,'--add',a);run('dep',b,'--remove',a);run('dep',b,'--remove',a);run('dep',b,'--add','api');run('dep',a,'--add',b,code=3)
assert run('show',b)['task']['dependencies']==[a]
run('dep',b,'--remove',a,'--add','missing-task',code=2)
assert run('show',b)['task']['dependencies']==[a]
run('move',b,code=2)
run('take',b,code=3);run('done',a,code=3);run('done',a,code=3,json_mode=False)
assert run('next','--type','bug','--priority','high','--label','api')['task']['id']==a
run('take',a);run('take',a);assert run('next','--mine')['task']['id']==a
run('check',a,'1');run('check',a,'1');run('check',a,'2');run('note',a,'Evidence retained');run('done',a);run('done',a)
assert run('show','api')['task']['notes']=='Evidence retained'
run('set',b,'--title','Better form','--priority','high','--type','docs','--label','web');run('set',b,'--status','todo')
run('move',b,'--before',a);run('move',b,'--after',a);run('move',b,'--top');run('mv',b,'auth');run('rebalance');run('drop',b);run('drop',b)
run('edit',a,extra={'EDITOR':'true'})
assert len(run('ls')['tasks'])==2
assert run('ls','--group','auth')['tasks'][0]['id']==b
cache=repo/'.git/huginn/cache.db';cache.unlink();assert len(run('ls')['tasks'])==2
backlog=base/'backlog';backlog.mkdir();(backlog/'task-1.md').write_text('---\nid: TASK-1\ntitle: Imported task\nstatus: To Do\n---\n\n## Description\nImport goal\n\n## Acceptance Criteria\n- [ ] Import check\n')
run('import','backlog',str(backlog));run('import','backlog',str(backlog));assert len(run('ls')['tasks'])==3
assert run('lint')['findings']==[]
run('sync');assert git(repo/'.huginn','rev-parse','HEAD')==git(remote,'rev-parse','huginn')
# Run the real TUI in a controlling terminal, then quit after its first render.
run('tui','--help',json_mode=False);run('tui',code=2)
pid,fd=pty.fork()
if pid==0:
    os.chdir(repo);os.execve(binary,[binary,'tui'],env)
screen=b'';quit_sent=False;deadline=time.monotonic()+12;status=None
try:
    while time.monotonic()<deadline:
        if select.select([fd],[],[],0.1)[0]:
            try: screen+=os.read(fd,65536)
            except OSError: pass
        if b'Huginn' in screen and not quit_sent:
            os.write(fd,b'q');quit_sent=True
        done,value=os.waitpid(pid,os.WNOHANG)
        if done: status=value;break
    if status is None:
        os.kill(pid,signal.SIGKILL);os.waitpid(pid,0)
    assert status is not None and os.waitstatus_to_exitcode(status)==0 and b'Huginn' in screen,screen[-4000:]
finally: os.close(fd)
# Real standard-I/O MCP handshake and one tool request.
p=subprocess.Popen([binary,'mcp'],cwd=repo,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
def rpc(method,params,ident=None):
    message={'jsonrpc':'2.0','method':method,'params':params}
    if ident is not None: message['id']=ident
    p.stdin.write(json.dumps(message)+'\n');p.stdin.flush()
    if ident is None:return
    while True:
        line=p.stdout.readline();assert line, p.stderr.read()
        reply=json.loads(line)
        if reply.get('id')==ident:return reply
try:
    assert 'result' in rpc('initialize',{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'e2e','version':'1'}},1)
    rpc('notifications/initialized',{})
    tools=rpc('tools/list',{},2)['result']['tools'];assert len(tools)==10
    result=rpc('tools/call',{'name':'list','arguments':{}},3)['result'];assert not result.get('isError');assert 'next' in json.loads(result['content'][0]['text'])
    before_group=run('show',b)['task']
    result=rpc('tools/call',{'name':'update','arguments':{'id':b,'action':'move_group','group':'board'}},4)['result'];assert not result.get('isError'),result
    after_group=run('show',b)['task'];assert after_group['group']=='board' and after_group['rank']==before_group['rank']
    result=rpc('tools/call',{'name':'update','arguments':{'id':b,'remove_deps':[a]}},5)['result'];assert not result.get('isError'),result
    assert run('show',b)['task']['dependencies']==[]
    result=rpc('tools/call',{'name':'update','arguments':{'id':b,'add_deps':[a]}},6)['result'];assert not result.get('isError'),result
    assert run('show',b)['task']['dependencies']==[a]
    result=rpc('tools/call',{'name':'lint'},7)['result'];assert not result.get('isError'),result
    result=rpc('tools/call',{'name':'update','arguments':{'id':b,'action':'move','group':'oops'}},8)['result'];assert result.get('isError'),result
    assert run('show',b)['task']['rank']==before_group['rank']
finally:
    p.stdin.close();p.wait(timeout=10)
# Two concurrent takes start from the same remote HEAD. A pre-push barrier proves both callbacks ran.
race=run('new','Race task','--goal','One owner','--accept','Unique owner')['task']['id'];run('sync')
clone=base/'b';git(base,'clone',str(remote),str(clone));run('init',cwd=clone)
barrier=base/'barrier';barrier.mkdir()
for directory in (repo,clone):
    hook=directory/'.git/hooks/pre-push'
    hook.write_text('#!/bin/sh\ntouch "$RACE_BARRIER/$HUGINN_USER"\ni=0\nwhile [ ! -e "$RACE_BARRIER/alice" ] || [ ! -e "$RACE_BARRIER/bob" ]; do\n i=$((i+1)); [ "$i" -lt 200 ] || exit 1\n sleep 0.05\ndone\n')
    hook.chmod(0o755)
procs=[subprocess.Popen([binary,'take',race,'--json'],cwd=d,env=dict(env,HUGINN_USER=u,RACE_BARRIER=str(barrier)),text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE) for d,u in ((repo,'alice'),(clone,'bob'))]
results=[]
for p in procs:
    out,err=p.communicate(timeout=30);results.append((p.returncode,out,err))
assert sorted(r[0] for r in results)==[0,1],results
loser=next(r for r in results if r[0]);assert 'taken by ' in loser[1],results
for d in (repo,clone):(d/'.git/hooks/pre-push').unlink()
run('sync');run('sync',cwd=clone)
winner=run('show',race)['task']['assignee'];assert winner==run('show',race,cwd=clone)['task']['assignee'] and len(winner)==1
assert not git(repo/'.huginn','status','--porcelain')
assert not git(repo,'status','--porcelain')
print('e2e: PASS (all commands, JSON contract, exit 2/3/4, stdio MCP, two-clone push-lock race)')
print('e2e artifacts: '+str(base))
PY
