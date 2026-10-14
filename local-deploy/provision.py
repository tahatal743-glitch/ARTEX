"""Initialize only this local ARTEX installation; never print credentials."""
from pathlib import Path
import json
import secrets
import string
import urllib.request
import urllib.error
import yaml

BASE = 'http://127.0.0.1:18787'
PRIVATE = Path(r'F:\ARTEX\private')
TOKEN = ''


def request(path, body=None, method=None, timeout=60):
    headers = {'Content-Type': 'application/json'}
    if TOKEN:
        headers['Authorization'] = 'Bearer ' + TOKEN
    req = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as response:
        return json.load(response)


def login():
    global TOKEN
    credentials_file = PRIVATE / 'login.json'
    initialized = request('/api/auth/status')['initialized']
    if not credentials_file.exists():
        if initialized:
            raise RuntimeError('Existing installation has no local credential file; initialization refused.')
        password = 'Ax9_' + ''.join(secrets.choice(string.ascii_letters + string.digits) for _ in range(22))
        credentials_file.write_text(json.dumps({'username': 'ARTEX', 'password': password}), encoding='utf-8')
        (PRIVATE / '本机登录信息.txt').write_text('ARTEX 本机登录\n地址：http://127.0.0.1:18787/\n用户名：ARTEX\n密码：' + password + '\n', encoding='utf-8-sig')
    credentials = json.loads(credentials_file.read_text(encoding='utf-8'))
    if not initialized:
        TOKEN = request('/api/auth/init', {'password': credentials['password']})['token']
    else:
        TOKEN = request('/api/auth/login', credentials)['token']
    return credentials


def main():
    login()
    config = yaml.safe_load(Path(r'F:\CyberStrikeAI\config.yaml').read_text(encoding='utf-8'))['ai']
    channel = config['channels'][config['default_channel']]
    profile = {
        'name': 'AnyRouter GPT-6 Astra（本机）',
        'format': 'openai-responses',
        'model': channel['model'],
        'base_url': channel['base_url'],
        'api_key': channel['api_key'],
        'context_window_k': 32,
        'reasoning_effort': 'high',
        'rate_per_minute': 10,
        'rate_per_second': 0,
        'streaming': True,
        'max_tokens': 0,
    }
    profiles = request('/api/llm/profiles')
    profiles = profiles.get('profiles', []) if isinstance(profiles, dict) else profiles
    for existing in profiles or []:
        if existing['name'] == profile['name']:
            profile['id'] = int(existing['id'])
    pid = request('/api/llm/profiles', profile)['id']
    request('/api/llm/profiles/active', {'id': pid})
    request('/api/settings', {'workers': 1, 'task_concurrency_enabled': True, 'task_concurrency_limit': 1}, method='PUT')
    (PRIVATE / 'installation.json').write_text(json.dumps({'profile_id': pid, 'model': channel['model'], 'source_commit': '5c0dbd4bec940f3c0faf20705a56d879b57d3dfe'}, indent=2), encoding='utf-8')
    print(json.dumps({'health': request('/api/health'), 'initialized': request('/api/auth/status')['initialized'], 'active_model': request('/api/llm')['model'], 'profile_id': pid}, ensure_ascii=False), flush=True)
    result = request('/api/llm/test', {'provider': profile['format'], 'model': profile['model'], 'base_url': profile['base_url'], 'profile_id': pid, 'streaming': True, 'reasoning_effort': profile['reasoning_effort']}, timeout=50)
    (PRIVATE / 'connection-test.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({'connection_test': result}, ensure_ascii=False), flush=True)


if __name__ == '__main__':
    main()
