#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

full_packages=(./...)
race_packages=(./...)
repository_packages=(./internal/archguard)

usage() {
	cat >&2 <<'EOF'
usage: tools/test.sh <lane>

lanes:
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
		printf '%s\n' "${race_packages[@]}"
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
full)
	require_no_arguments "$@"
	exec tools/test-go.sh -mod=readonly -count=1 "${full_packages[@]}"
	;;
race)
	require_no_arguments "$@"
	exec tools/test-go.sh -mod=readonly -race -count=1 "${race_packages[@]}"
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
