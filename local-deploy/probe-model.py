from pathlib import Path
import json, yaml, uuid, time, urllib.request, urllib.error
cfg = yaml.safe_load(Path(r'F:\CyberStrikeAI\config.yaml').read_text(encoding='utf-8'))['ai']
c = cfg['channels'][cfg['default_channel']]
sid = str(uuid.uuid4())
turn = str(uuid.uuid4())
installation = str(uuid.uuid5(uuid.NAMESPACE_URL, 'artex/local-responses-adapter'))
meta = json.dumps({'installation_id': installation, 'session_id': sid, 'thread_id': sid, 'turn_id': turn, 'window_id': sid + ':0', 'request_kind': 'turn', 'thread_source': 'user', 'sandbox': 'none', 'turn_started_at_unix_ms': int(time.time()*1000)}, separators=(',', ':'))
body = {'model': c['model'], 'instructions': 'Reply only OK.', 'input': [{'type': 'message', 'role': 'user', 'content': [{'type': 'input_text', 'text': 'ping'}]}], 'tools': [], 'tool_choice': 'auto', 'parallel_tool_calls': False, 'reasoning': {'effort': 'high', 'summary': 'auto', 'context': 'all_turns'}, 'store': False, 'stream': True, 'include': ['reasoning.encrypted_content'], 'prompt_cache_key': sid, 'text': {'verbosity': 'low'}, 'client_metadata': {'turn_id': turn, 'x-codex-installation-id': installation, 'x-codex-window-id': sid + ':0', 'thread_id': sid, 'session_id': sid, 'x-codex-turn-metadata': meta}}
headers = {'Authorization': 'Bearer '+c['api_key'], 'Content-Type': 'application/json', 'Accept': 'text/event-stream', 'originator': 'codex_exec', 'User-Agent': 'codex_exec/0.144.1 (Windows; x86_64)', 'x-openai-internal-codex-responses-lite': 'true', 'session-id': sid, 'thread-id': sid, 'x-client-request-id': sid, 'x-codex-window-id': sid+':0', 'x-codex-turn-metadata': meta}
try:
    with urllib.request.urlopen(urllib.request.Request(c['base_url'].rstrip('/')+'/responses', data=json.dumps(body).encode(), headers=headers), timeout=45) as r:
        output = ''
        events = []
        for raw in r:
            if not raw.startswith(b'data: '): continue
            try: ev = json.loads(raw[6:])
            except ValueError: continue
            typ = ev.get('type','')
            if typ == 'response.output_text.delta': output += ev.get('delta','')
            if typ in ['response.failed','error']: print(json.dumps(ev)[:400])
            if typ == 'response.completed':
                events = [x.get('type') for x in ev.get('response',{}).get('output',[])]
                break
        print(json.dumps({'status': r.status, 'reply': output[:100], 'output_types': events}))
except urllib.error.HTTPError as e:
    print(json.dumps({'status': e.code, 'error': e.read().decode()[:500]}))
