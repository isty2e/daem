#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

# Resolve Go-owned state before HOME changes so isolation does not create a
# second toolchain or module cache inside the disposable test environment.
# Host test-selection and workspace policy must not influence these queries or
# the mandatory repository test invocation below.
original_home=${HOME:-}
export GOFLAGS=
export GOWORK=off
go_cache=$(go env GOCACHE)
go_mod_cache=$(go env GOMODCACHE)
go_path=$(go env GOPATH)
go_toolchain=$(go env GOTOOLCHAIN)
go_proxy=$(go env GOPROXY)
go_sumdb=$(go env GOSUMDB)
go_private=$(go env GOPRIVATE)
go_no_proxy=$(go env GONOPROXY)
go_no_sumdb=$(go env GONOSUMDB)

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
	"${test_root}/coordinator/home" \
	"${test_root}/coordinator/xdg/cache" \
	"${test_root}/coordinator/xdg/config" \
	"${test_root}/coordinator/xdg/data" \
	"${test_root}/coordinator/xdg/state"

export HOME="${test_root}/coordinator/home"
export XDG_CACHE_HOME="${test_root}/coordinator/xdg/cache"
export XDG_CONFIG_HOME="${test_root}/coordinator/xdg/config"
export XDG_DATA_HOME="${test_root}/coordinator/xdg/data"
export XDG_STATE_HOME="${test_root}/coordinator/xdg/state"

# Host-specific variables are optional overrides. Clearing them lets every
# host derive its default below the isolated HOME and avoids pinning nested
# test fixtures to the harness root when they replace HOME themselves.
unset CLAUDE_CONFIG_DIR CODEX_HOME PI_CODING_AGENT_DIR

export GOCACHE="${go_cache}"
export GOMODCACHE="${go_mod_cache}"
export GOPATH="${go_path}"
export GOENV=off
export GOTOOLCHAIN="${go_toolchain}"
export GOPROXY="${go_proxy}"
export GOSUMDB="${go_sumdb}"
export GOPRIVATE="${go_private}"
export GONOPROXY="${go_no_proxy}"
export GONOSUMDB="${go_no_sumdb}"
unset GOFLAGS
export GOWORK=off

export DAEM_TEST_HARNESS=1
export DAEM_TEST_ROOT="${test_root}"
export DAEM_TEST_ORIGINAL_HOME="${original_home}"
export DAEM_TEST_ORIGINAL_GOCACHE="${go_cache}"
export DAEM_TEST_ORIGINAL_GOMODCACHE="${go_mod_cache}"
export DAEM_TEST_ORIGINAL_GOPATH="${go_path}"

for argument in "$@"; do
	case "${argument}" in
	-exec | -exec=*)
		echo "tools/test-go.sh owns -exec to enforce per-package isolation" >&2
		exit 2
		;;
	esac
done

go test -exec "${repository_root}/tools/test-exec.sh" "$@"
