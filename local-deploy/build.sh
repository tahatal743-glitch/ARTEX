#!/bin/bash
set -euo pipefail
export PATH=/opt/artex/toolchains/go/bin:/opt/artex/toolchains/node/bin:/usr/local/bin:/usr/bin:/bin
export GOCACHE=/opt/artex/cache/go-build
export GOMODCACHE=/opt/artex/cache/go-mod
export GOPROXY=https://proxy.golang.org
export GOTOOLCHAIN=local
export NEXT_TELEMETRY_DISABLED=1
export HUSKY=0
export npm_config_cache=/opt/artex/cache/npm
export HOME=/opt/artex/runtime
export TMPDIR=/opt/artex/tmp
cd /opt/artex/src
case "${1:-all}" in
  deps)
    go mod download
    echo 'Go dependencies downloaded.'
    ;;
  frontend)
    cd web
    npm ci --no-audit --no-fund
    NEXT_EXPORT=1 npm exec -- next build
    ;;
  backend)
    test -f web/out/index.html
    mkdir -p server/webui/dist
    cp -a web/out/. server/webui/dist/
    CGO_ENABLED=0 go build -trimpath -tags embedui -ldflags '-s -w -X main.version=v0.3.15-local' -o /opt/artex/runtime/artex ./cmd/artex
    cp -a skills /opt/artex/runtime/
    cp start.sh /opt/artex/runtime/start.sh
    chmod +x /opt/artex/runtime/start.sh
    sha256sum /opt/artex/runtime/artex
    ;;
  *) echo 'Use deps, frontend, or backend'; exit 2 ;;
esac
