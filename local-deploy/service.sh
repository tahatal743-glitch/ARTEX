#!/bin/bash
set -euo pipefail
BASE=/opt/artex/runtime
PIDFILE="$BASE/launcher.pid"
exec 9>/run/lock/artex-local.lock
flock -w 30 -x 9
case "${1:-status}" in
  start)
    pg_ctlcluster 18 artex status >/dev/null 2>&1 || pg_ctlcluster 18 artex start 9>&-
    if curl -fsS --max-time 3 http://127.0.0.1:18787/api/health >/dev/null 2>&1; then
      echo 'ARTEX is already running.'
      exit 0
    fi
    test -x "$BASE/artex"
    install -o artex -g artex -m 755 /mnt/f/ARTEX/local-deploy/run.sh "$BASE/run.sh"
    if [ ! -f "$BASE/config.json" ]; then
      python3 - <<'PY'
import json, os, pwd
from pathlib import Path
p = Path('/opt/artex/runtime/config.json')
p.write_text(json.dumps({'database': {'dsn': 'host=/var/run/postgresql port=5434 user=artex dbname=artex sslmode=disable'}, 'skill_dir': '/opt/artex/runtime/skills'}, indent=2))
u = pwd.getpwnam('artex')
os.chown(p, u.pw_uid, u.pw_gid)
os.chmod(p, 0o600)
PY
    fi
    runuser -u artex -- python3 - 9>&- <<'PY'
import os, subprocess
from pathlib import Path
base = Path('/opt/artex/runtime')
with (base/'service.log').open('ab', buffering=0) as log:
    p = subprocess.Popen([str(base/'run.sh')], stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True, close_fds=True)
(base/'launcher.pid').write_text(str(p.pid))
PY
    for attempt in $(seq 1 45); do
      if curl -fsS --max-time 2 http://127.0.0.1:18787/api/health 2>/dev/null; then
        echo; echo 'ARTEX started on http://127.0.0.1:18787/'; exit 0
      fi
      sleep 1
    done
    echo 'ARTEX startup failed. Inspect /opt/artex/runtime/service.log' >&2
    exit 1
    ;;
  stop)
    python3 - <<'PY'
from pathlib import Path
import os, signal, time
pidfile = Path('/opt/artex/runtime/launcher.pid')
if pidfile.exists():
    pid = int(pidfile.read_text())
    proc = Path(f'/proc/{pid}')
    if proc.exists() and (proc/'cwd').resolve() == Path('/opt/artex/runtime'):
        cmd = (proc/'cmdline').read_bytes()
        if b'start.sh' not in cmd and b'run.sh' not in cmd:
            raise SystemExit('PID identity mismatch; refusing to stop.')
        os.kill(pid, signal.SIGTERM)
        for _ in range(200):
            if not proc.exists() or ') Z ' in (proc/'stat').read_text():
                break
            time.sleep(0.1)
        else:
            raise SystemExit('ARTEX is still shutting down; database left running.')
    pidfile.unlink(missing_ok=True)
PY
    if curl -fsS --max-time 2 http://127.0.0.1:18787/api/health >/dev/null 2>&1; then
      echo 'ARTEX still responds; database left running.' >&2; exit 1
    fi
    pg_ctlcluster 18 artex status >/dev/null 2>&1 && pg_ctlcluster 18 artex stop 9>&- || true
    echo 'ARTEX and its dedicated database are stopped.'
    ;;
  status)
    pg_lsclusters | head -n 1
    pg_lsclusters | grep ' artex ' || true
    curl -fsS --max-time 3 http://127.0.0.1:18787/api/health
    echo
    ;;
  *) echo 'Usage: service.sh start|stop|status'; exit 2 ;;
esac
