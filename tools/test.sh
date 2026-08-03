#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

full_packages=(./...)
repository_packages=(./internal/archguard)

race_package_paths() {
	local module_path
	module_path=$(GOENV=off GOFLAGS= GOWORK=off go list -m -f '{{.Path}}')
	GOENV=off GOFLAGS= GOWORK=off go list -f '{{.ImportPath}}' ./... |
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

usage() {
	cat >&2 <<'EOF'
usage: tools/test.sh <lane>

lanes:
  focused     cacheable isolated iteration for one exact package
  full        fresh hermetic repository correctness
  race        fresh hermetic repository race detection
  repository  architecture, documentation, and repository contracts

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
	full)
		printf '%s\n' "${full_packages[@]}"
		;;
	race)
		race_package_paths
		;;
	repository)
		printf '%s\n' "${repository_packages[@]}"
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
full)
	require_no_arguments "$@"
	exec tools/test-go.sh -mod=readonly -count=1 "${full_packages[@]}"
	;;
race)
	require_no_arguments "$@"
	tools/test-race-proof.sh
	race_packages=$(race_package_paths)
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
