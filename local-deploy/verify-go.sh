#!/bin/bash
set -euo pipefail
export PATH=/opt/artex/toolchains/go/bin:/usr/bin:/bin
export GOCACHE=/opt/artex/cache/go-build
export GOMODCACHE=/opt/artex/cache/go-mod
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org
cd /opt/artex/src
go test ./agent -run 'Test.*(Provider|Responses|Quota|Capture)' -count=1
