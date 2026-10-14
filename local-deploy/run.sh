#!/bin/bash
set -euo pipefail
umask 077
export HOME=/opt/artex/runtime
export PATH=/opt/artex/toolchains/node/bin:/usr/local/bin:/usr/bin:/bin
export npm_config_cache=/opt/artex/cache/npm
export XDG_CACHE_HOME=/opt/artex/cache
export ARTEX_CONFIG=/opt/artex/runtime/config.json
export ARTEX_RESPONSES_COMPAT=anyrouter
cd /opt/artex/runtime
exec ./start.sh -addr 127.0.0.1:18787 -proxy 127.0.0.1:18788
