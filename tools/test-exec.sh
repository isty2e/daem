#!/usr/bin/env bash

set -euo pipefail

if [[ ${DAEM_TEST_HARNESS:-} != 1 || -z ${DAEM_TEST_ROOT:-} ]]; then
	echo "tools/test-exec.sh must run through tools/test-go.sh" >&2
	exit 2
fi

package_root=$(mktemp -d "${DAEM_TEST_ROOT%/}/package.XXXXXX")

# Invoked by the EXIT trap.
# shellcheck disable=SC2329
cleanup() {
	status=$?
	trap - EXIT HUP INT QUIT TERM
	if ! rm -rf -- "${package_root}"; then
		echo "failed to remove isolated package test root ${package_root}" >&2
		if ((status == 0)); then
			status=1
		fi
	fi
	exit "${status}"
}
trap cleanup EXIT

child_pid=
starting_child=1
pending_signal=
pending_status=
# Invoked by the signal traps below.
# shellcheck disable=SC2329
forward_signal() {
	local signal=$1
	local status=$2

	if ((starting_child)); then
		pending_signal=${signal}
		pending_status=${status}
		return
	fi
	trap - HUP INT QUIT TERM
	if [[ -n ${child_pid} ]]; then
		kill "-${signal}" -- "-${child_pid}" 2>/dev/null || true
		(
			sleep 2
			kill -KILL -- "-${child_pid}" 2>/dev/null || true
		) &
		local escalation_pid=$!
		wait "${child_pid}" 2>/dev/null || true
		kill "${escalation_pid}" 2>/dev/null || true
		wait "${escalation_pid}" 2>/dev/null || true
	fi
	exit "${status}"
}

trap 'forward_signal HUP 129' HUP
trap 'forward_signal INT 130' INT
trap 'forward_signal QUIT 131' QUIT
trap 'forward_signal TERM 143' TERM

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

# A dedicated process group lets cmd/go cancellation reach the test binary and
# any subprocesses it started before the package root is removed.
set -m
"$@" &
child_pid=$!
set +m
starting_child=0

if [[ -n ${pending_signal} ]]; then
	forward_signal "${pending_signal}" "${pending_status}"
fi

set +e
wait "${child_pid}"
status=$?
set -e
exit "${status}"
