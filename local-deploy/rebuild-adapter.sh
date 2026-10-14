#!/bin/bash
set -euo pipefail
export PATH=/opt/artex/toolchains/go/bin:/usr/bin:/bin
export GOCACHE=/opt/artex/cache/go-build
export GOMODCACHE=/opt/artex/cache/go-mod
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org
cd /opt/artex/src
cp /mnt/f/ARTEX/agent/local_responses_compat.go agent/
cp /mnt/f/ARTEX/agent/provider.go agent/
gofmt -w agent/local_responses_compat.go
CGO_ENABLED=0 go build -trimpath -tags embedui -ldflags '-s -w -X main.version=v0.3.15-local' -o /opt/artex/runtime/artex.new ./cmd/artex
echo 'Adapter build ready.'
