#!/usr/bin/env python3
"""Protocol-only discovery probe. NOT a huginn implementation or model eval."""
import json
import sys

for line in sys.stdin:
    request = json.loads(line)
    if 'id' not in request:
        continue
    method = request['method']
    if method == 'initialize':
        result = {'protocolVersion':request.get('params', {}).get('protocolVersion', '2024-11-05'),
                  'capabilities':{'tools':{}}, 'serverInfo':{'name':'eval-discovery-probe','version':'1'}}
    elif method == 'tools/list':
        result = {'tools':[{'name':'probe','description':'Protocol discovery probe, not a task operation.',
                           'inputSchema':{'type':'object','properties':{}}}]}
    elif method == 'ping':
        result = {}
    else:
        print(json.dumps({'jsonrpc':'2.0','id':request['id'],'error':{'code':-32601,'message':'Probe only supports discovery'}}), flush=True)
        continue
    print(json.dumps({'jsonrpc':'2.0','id':request['id'],'result':result}), flush=True)
