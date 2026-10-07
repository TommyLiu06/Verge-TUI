#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir"
export GOPATH="$project_dir/.tools/gopath"
export GOMODCACHE="$project_dir/.tools/modcache"
export GOCACHE="$project_dir/.tools/buildcache"
export GOTMPDIR="$project_dir/.tools/tmp"
export TMPDIR="$project_dir/.tools/tmp"
export XDG_CONFIG_HOME="$project_dir/.tools/config"
export XDG_CACHE_HOME="$project_dir/.tools/cache"
export GOENV=off GOTOOLCHAIN=local GOTELEMETRY=off CGO_ENABLED=0
export GIT_CONFIG_GLOBAL="$project_dir/.tools/gitconfig" GIT_CONFIG_NOSYSTEM=1
mkdir -p "$GOPATH" "$GOMODCACHE" "$GOCACHE" "$GOTMPDIR" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME"
exec /usr/local/go/bin/go "$@"
