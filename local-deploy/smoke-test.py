from pathlib import Path
import json, time, urllib.request, urllib.error, re
import provision as p

p.login()
pages = ['/', '/login/', '/dashboard/', '/system/llm/', '/function/tasks/']
page_results = {}
for path in pages:
    with urllib.request.urlopen(p.BASE + path, timeout=10) as r:
        html = r.read().decode()
        page_results[path] = {'status': r.status, 'has_app_scripts': '/_next/static/' in html}
with urllib.request.urlopen(p.BASE + '/login/', timeout=10) as r:
    html = r.read().decode()
assets = sorted(set(re.findall(r'(?:src|href)="([^" ]*/_next/static/[^" ]+)"', html)))
for asset in assets:
    with urllib.request.urlopen(p.BASE + asset, timeout=10) as r:
        assert r.status == 200
print(json.dumps({'pages': page_results, 'static_assets_ok': len(assets)}), flush=True)
try:
    urllib.request.urlopen(p.BASE + '/api/tasks', timeout=10)
    raise RuntimeError('Unauthenticated API unexpectedly accessible')
except urllib.error.HTTPError as e:
    assert e.code == 401, e.code
print('Authentication boundary: PASS', flush=True)

conversation = p.request('/api/conversations', {'agent_key': 'pentest', 'title': '本机部署验收', 'llm_profile_id': 1})
cid = int(conversation['id'])
print(json.dumps({'conversation_id': cid}), flush=True)
message = "这是一项本机部署验收。只执行一次 Bash 命令：printf 'ARTEX_LOCAL_OK\\n'，根据真实输出回复 ARTEX_LOCAL_OK。不要联网、不要扫描或读取文件，不要创建其他任务。不要调用其他工具。"
p.request(f'/api/conversations/{cid}/messages', {'message': message})
deadline = time.time() + 180
while time.time() < deadline:
    data = p.request(f'/api/conversations/{cid}/messages')
    if not data.get('running', False):
        break
    time.sleep(2)
else:
    p.request(f'/api/conversations/{cid}/stop', {})
    raise RuntimeError('Local tool smoke test timed out; conversation stopped')
(p.PRIVATE / 'tool-smoke.json').write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')
items = data.get('items', [])
print(json.dumps({'conversation_id': cid, 'running': data.get('running'), 'steps': [{'seq': x.get('seq'), 'kind': x.get('kind'), 'summary': x.get('summary', '')[:220]} for x in items]}, ensure_ascii=True), flush=True)
