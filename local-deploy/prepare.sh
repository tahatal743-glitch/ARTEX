#!/bin/bash
set -euo pipefail
umask 022
BASE=/opt/artex
SOURCE=/mnt/f/ARTEX/local-deploy/source.tar
if ! id artex >/dev/null 2>&1; then
  useradd --system --user-group --home-dir "$BASE/runtime" --shell /bin/bash artex
fi
usermod --home "$BASE/runtime" artex
install -d -m 755 "$BASE" "$BASE/toolchains" "$BASE/downloads"
install -d -o artex -g artex -m 750 "$BASE/runtime" "$BASE/src" "$BASE/cache" "$BASE/tmp"
cd "$BASE/downloads"
if [ ! -x "$BASE/toolchains/go/bin/go" ]; then
  curl -fL --retry 3 --connect-timeout 20 -o go1.26.9.linux-amd64.tar.gz https://go.dev/dl/go1.26.9.linux-amd64.tar.gz
  echo '42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d  go1.26.9.linux-amd64.tar.gz' | sha256sum -c -
  tar -xzf go1.26.9.linux-amd64.tar.gz -C "$BASE/toolchains"
fi
if [ ! -x "$BASE/toolchains/node/bin/node" ]; then
  curl -fsSL --retry 3 -o node-SHASUMS256.txt https://nodejs.org/dist/v24.13.0/SHASUMS256.txt
  curl -fL --retry 3 --connect-timeout 20 -o node-v24.13.0-linux-x64.tar.xz https://nodejs.org/dist/v24.13.0/node-v24.13.0-linux-x64.tar.xz
  grep ' node-v24.13.0-linux-x64.tar.xz$' node-SHASUMS256.txt | sha256sum -c -
  mkdir -p "$BASE/toolchains/node"
  tar -xJf node-v24.13.0-linux-x64.tar.xz --strip-components=1 -C "$BASE/toolchains/node"
fi
tar -xf "$SOURCE" -C "$BASE/src"
chown -R artex:artex "$BASE/src"
if [ ! -f /etc/postgresql/18/artex/postgresql.conf ]; then
  pg_createcluster 18 artex --port 5434 --datadir "$BASE/postgres" --start-conf manual -- --auth-local=peer --auth-host=scram-sha-256
fi
python3 - <<'PY'
from pathlib import Path
import re
p = Path('/etc/postgresql/18/artex/postgresql.conf')
p.write_text(re.sub(r'(?m)^listen_addresses\s*=.*$', "listen_addresses = '127.0.0.1'", p.read_text()))
PY
pg_ctlcluster 18 artex status >/dev/null 2>&1 || pg_ctlcluster 18 artex start
if ! runuser -u postgres -- psql -p 5434 -tAc "SELECT 1 FROM pg_roles WHERE rolname='artex'" | grep -q 1; then
  runuser -u postgres -- createuser -p 5434 artex
fi
if ! runuser -u postgres -- psql -p 5434 -tAc "SELECT 1 FROM pg_database WHERE datname='artex'" | grep -q 1; then
  runuser -u postgres -- createdb -p 5434 --owner artex artex
fi
runuser -u artex -- psql -p 5434 -d artex -tAc 'SELECT current_user'
echo 'ARTEX isolated runtime and PostgreSQL cluster ready.'
