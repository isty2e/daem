#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

repository_packages=(./internal/archguard)
tooling_packages=(./test/tooling)
scale_packages=(
	./internal/adopt
	./internal/adopt/hook
	./internal/adopt/skill
	./internal/assurance/observe/codexplugin
	./internal/declaration
	./internal/effect/mutation
	./internal/realization/aggregate/codec/hook
	./internal/realization/aggregate/codec/mcp
	./internal/realization/lockfile
)

product_package_paths() {
	local module_path
	module_path=$(env GOENV=off GOFLAGS='' GOWORK=off go list -m -f '{{.Path}}')
	env GOENV=off GOFLAGS='' GOWORK=off \
		go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... |
		while IFS= read -r package_path; do
			case "${package_path}" in
			"${module_path}/internal/archguard" | "${module_path}/test/tooling")
				continue
				;;
			esac
			if [[ -n ${package_path} ]]; then
				printf '%s\n' "${package_path}"
			fi
		done
}

core_package_paths() {
	local module_path
	module_path=$(env GOENV=off GOFLAGS='' GOWORK=off go list -m -f '{{.Path}}')
	product_package_paths |
		while IFS= read -r package_path; do
			case "${package_path}" in
			"${module_path}/internal/supply/source/backend/gitcli" | \
				"${module_path}/test/cli" | \
				"${module_path}/test/cli/"*)
				continue
				;;
			esac
			printf '%s\n' "${package_path}"
		done
}

usage() {
	cat >&2 <<'EOF'
usage: tools/test.sh <lane>

lanes:
  focused     cacheable isolated iteration for one exact package
  core        fresh hermetic product tests without external Git/CLI journeys
  full        fresh hermetic product and CLI correctness
  race        fresh hermetic product and CLI race detection
  repository  semantic architecture contracts
  tooling     test-runner lifecycle and isolation contracts
  scale       allocation and maximum-size resource contracts

inspection:
  packages <lane>  print the package selectors owned by a lane
EOF
}

require_no_arguments() {
	if (($# != 0)); then
		usage
		exit 2
	fi
}

print_packages() {
	local lane=${1:-}
	case "${lane}" in
	core)
		core_package_paths
		;;
	full)
		product_package_paths
		;;
	race)
		product_package_paths
		;;
	repository)
		printf '%s\n' "${repository_packages[@]}"
		;;
	tooling)
		printf '%s\n' "${tooling_packages[@]}"
		;;
	scale)
		printf '%s\n' "${scale_packages[@]}"
		;;
	*)
		usage
		exit 2
		;;
	esac
}

lane=${1:-}
if (($# > 0)); then
	shift
fi

case "${lane}" in
focused)
	if (($# < 1 || $# > 2)); then
		usage
		exit 2
	fi
	exec tools/test-focused.sh "$@"
	;;
core)
	require_no_arguments "$@"
	core_packages=$(core_package_paths)
	if [[ -z ${core_packages} ]]; then
		echo "core lane has no owned product test packages" >&2
		exit 1
	fi
	# Package import paths cannot contain whitespace.
	# shellcheck disable=SC2086
	exec tools/test-go.sh -mod=readonly -count=1 ${core_packages}
	;;
full)
	require_no_arguments "$@"
	full_packages=$(product_package_paths)
	if [[ -z ${full_packages} ]]; then
		echo "full lane has no owned product test packages" >&2
		exit 1
	fi
	# Package import paths cannot contain whitespace.
	# shellcheck disable=SC2086
	exec tools/test-go.sh -mod=readonly -count=1 ${full_packages}
	;;
race)
	require_no_arguments "$@"
	tools/test-race-proof.sh
	race_packages=$(product_package_paths)
	if [[ -z ${race_packages} ]]; then
		echo "race lane has no owned test packages" >&2
		exit 1
	fi
	export GORACE=atexit_sleep_ms=0
	# Package import paths cannot contain whitespace.
	# shellcheck disable=SC2086
	exec tools/test-go.sh -mod=readonly -race -count=1 ${race_packages}
	;;
repository)
	require_no_arguments "$@"
	exec tools/test-go.sh -mod=readonly -count=1 "${repository_packages[@]}"
	;;
tooling)
	require_no_arguments "$@"
	exec tools/test-go.sh -mod=readonly -count=1 "${tooling_packages[@]}"
	;;
scale)
	require_no_arguments "$@"
	export DAEM_TEST_SCALE=1
	exec tools/test-go.sh -mod=readonly -count=1 "${scale_packages[@]}"
	;;
packages)
	if (($# != 1)); then
		usage
		exit 2
	fi
	print_packages "$1"
	;;
*)
	usage
	exit 2
	;;
esac
