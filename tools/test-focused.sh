#!/usr/bin/env bash

set -euo pipefail

if (($# < 1 || $# > 2)); then
	echo "usage: tools/test-focused.sh <single-package> [test-regex]" >&2
	exit 2
fi

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

requested_package=$1
test_pattern=${2:-}
if [[ ${test_pattern} == */* ]]; then
	echo "focused test regex must select top-level tests without '/'" >&2
	exit 2
fi

# Resolve Go-owned state before HOME changes. Host test selection and workspace
# policy must not influence package resolution or focused execution.
original_home=${HOME:-}
export GOENV=off
unset GOFLAGS
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
module_path=$(go list -m -f '{{.Path}}')

package_record=$(go list -f '{{.ImportPath}}|{{if .Module}}{{.Module.Path}}{{end}}|{{len .TestGoFiles}}|{{len .XTestGoFiles}}' "${requested_package}")
if [[ -z ${package_record} || ${package_record} == *$'\n'* ]]; then
	echo "focused tests require one exact package, got ${requested_package}" >&2
	exit 2
fi

IFS='|' read -r package_path package_module internal_tests external_tests <<<"${package_record}"
if [[ ${package_module} != "${module_path}" ]]; then
	echo "focused package ${package_path} is outside module ${module_path}" >&2
	exit 2
fi
if ((internal_tests + external_tests == 0)); then
	echo "focused package ${package_path} has no tests on this platform" >&2
	exit 2
fi

identity=$(printf '%s\0%s\n' "${repository_root}" "${package_path}" | git hash-object --stdin)
workspace_fingerprint=$(
	{
		git rev-parse --verify HEAD
		git diff --no-ext-diff --no-textconv --binary HEAD --
		while IFS= read -r -d '' path; do
			if [[ ! -f ${path} && ! -L ${path} ]]; then
				echo "focused cache cannot fingerprint non-file input ${path}" >&2
				exit 1
			fi
			if [[ -f ${path} ]]; then
				bytes=$(wc -c <"${path}")
				if ((bytes > 64 * 1024 * 1024)); then
					echo "focused cache input ${path} exceeds 64 MiB" >&2
					exit 1
				fi
			fi
			printf '%s\0' "${path}"
			git hash-object -- "${path}"
		done < <(git ls-files --others --exclude-standard -z)
	} | git hash-object --stdin
)
temporary_base=${TMPDIR:-/tmp}
focused_base="${temporary_base%/}/daem-focused-test-v1"
lock_root="${focused_base}/${identity}.lock"
test_root="${focused_base}/${identity}.root"
package_root="${test_root}/package"

umask 077
prepare_private_base() {
	local root=$1
	if mkdir -m 700 "${root}" 2>/dev/null; then
		return 0
	fi
	if [[ -L ${root} || ! -d ${root} ]] || ! chmod 700 "${root}"; then
		echo "focused test base ${root} must be a private directory owned by the current user" >&2
		return 1
	fi
}

if ! prepare_private_base "${focused_base}"; then
	exit 2
fi
if ! mkdir "${lock_root}"; then
	echo "focused tests for ${package_path} are already active or left a lock at ${lock_root}" >&2
	exit 2
fi
printf '%s\n' "$$" >"${lock_root}/pid"

remove_private_tree() {
	local root=$1
	if [[ ! -e ${root} && ! -L ${root} ]]; then
		return 0
	fi
	if [[ -L ${root} || ! -d ${root} ]]; then
		echo "refusing to clean non-directory focused test root ${root}" >&2
		return 1
	fi
	find "${root}" -xdev -mindepth 1 -depth -delete
	rmdir "${root}"
}

cleanup() {
	status=$?
	trap - EXIT HUP INT QUIT TERM
	if ! remove_private_tree "${test_root}"; then
		if ((status == 0)); then
			status=1
		fi
	fi
	if ! remove_private_tree "${lock_root}"; then
		if ((status == 0)); then
			status=1
		fi
	fi
	rmdir "${focused_base}" 2>/dev/null || true
	exit "${status}"
}
trap cleanup EXIT

child_pid=
starting_child=1
pending_signal=
pending_status=
forward_signal() {
	local signal=$1
	local status=$2
	if ((starting_child)); then
		pending_signal=${signal}
		pending_status=${status}
		return
	fi
	trap '' HUP INT QUIT TERM
	if [[ -n ${child_pid} ]]; then
		kill "-${signal}" -- "-${child_pid}" 2>/dev/null || true
		(
			sleep 2
			kill -KILL -- "-${child_pid}" 2>/dev/null || true
		) &
		escalation_pid=$!
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

remove_private_tree "${test_root}"
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
export DAEM_TEST_PACKAGE_ROOT="${package_root}"
export DAEM_TEST_ORIGINAL_HOME="${original_home}"
export DAEM_TEST_ORIGINAL_GOCACHE="${go_cache}"
export DAEM_TEST_ORIGINAL_GOMODCACHE="${go_mod_cache}"
export DAEM_TEST_ORIGINAL_GOPATH="${go_path}"

selected_tests=${test_pattern:-.*}
cache_pattern="(${selected_tests})|^__daem_focused_${workspace_fingerprint}$"
arguments=(-mod=readonly -run "${cache_pattern}" "${package_path}")

set -m
go test "${arguments[@]}" &
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
