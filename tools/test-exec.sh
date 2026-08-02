#!/usr/bin/env bash

set -euo pipefail

if [[ ${DAEM_TEST_HARNESS:-} != 1 || -z ${DAEM_TEST_ROOT:-} ]]; then
	echo "tools/test-exec.sh must run through tools/test-go.sh" >&2
	exit 2
fi

package_root=$(mktemp -d "${DAEM_TEST_ROOT%/}/package.XXXXXX")

cleanup() {
	status=$?
	trap - EXIT HUP INT TERM
	if ! rm -rf -- "${package_root}"; then
		echo "failed to remove isolated package test root ${package_root}" >&2
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

mkdir -p \
	"${package_root}/home" \
	"${package_root}/xdg/cache" \
	"${package_root}/xdg/config" \
	"${package_root}/xdg/data" \
	"${package_root}/xdg/state"

export HOME="${package_root}/home"
export XDG_CACHE_HOME="${package_root}/xdg/cache"
export XDG_CONFIG_HOME="${package_root}/xdg/config"
export XDG_DATA_HOME="${package_root}/xdg/data"
export XDG_STATE_HOME="${package_root}/xdg/state"
export DAEM_TEST_PACKAGE_ROOT="${package_root}"

unset CLAUDE_CONFIG_DIR CODEX_HOME PI_CODING_AGENT_DIR

"$@"
