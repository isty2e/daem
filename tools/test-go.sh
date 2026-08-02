#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

# Resolve Go-owned state before HOME changes so isolation does not create a
# second toolchain or module cache inside the disposable test environment.
original_home=${HOME:-}
go_cache=$(go env GOCACHE)
go_mod_cache=$(go env GOMODCACHE)
go_path=$(go env GOPATH)
go_environment=$(go env GOENV)
go_toolchain=$(go env GOTOOLCHAIN)

temporary_base=${TMPDIR:-/tmp}
test_root=$(mktemp -d "${temporary_base%/}/daem-go-test.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if ! rm -rf -- "${test_root}"; then
    echo "failed to remove isolated Go test root ${test_root}" >&2
    if ((status == 0)); then
      status=1
    fi
  fi
  exit "${status}"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Keep explicit fixture modes deterministic. The mktemp-owned root remains
# private even though descendants retain the modes requested by tests.
umask 022
mkdir -p \
  "${test_root}/home" \
  "${test_root}/xdg/cache" \
  "${test_root}/xdg/config" \
  "${test_root}/xdg/data" \
  "${test_root}/xdg/state"

export HOME="${test_root}/home"
export XDG_CACHE_HOME="${test_root}/xdg/cache"
export XDG_CONFIG_HOME="${test_root}/xdg/config"
export XDG_DATA_HOME="${test_root}/xdg/data"
export XDG_STATE_HOME="${test_root}/xdg/state"

# Host-specific variables are optional overrides. Clearing them lets every
# host derive its default below the isolated HOME and avoids pinning nested
# test fixtures to the harness root when they replace HOME themselves.
unset CLAUDE_CONFIG_DIR CODEX_HOME PI_CODING_AGENT_DIR

export GOCACHE="${go_cache}"
export GOMODCACHE="${go_mod_cache}"
export GOPATH="${go_path}"
export GOENV="${go_environment}"
export GOTOOLCHAIN="${go_toolchain}"

export DAEM_TEST_HARNESS=1
export DAEM_TEST_ROOT="${test_root}"
export DAEM_TEST_ORIGINAL_HOME="${original_home}"
export DAEM_TEST_ORIGINAL_GOCACHE="${go_cache}"
export DAEM_TEST_ORIGINAL_GOMODCACHE="${go_mod_cache}"
export DAEM_TEST_ORIGINAL_GOPATH="${go_path}"

go test "$@"
