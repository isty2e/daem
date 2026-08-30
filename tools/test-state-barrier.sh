#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

usage() {
	cat >&2 <<'EOF'
usage: tools/test-state-barrier.sh [--race]
EOF
}

race=0
case ${1:-} in
"")
	;;
--race)
	race=1
	;;
*)
	usage
	exit 2
	;;
esac
if (($# > 1)); then
	usage
	exit 2
fi

export GOENV=off
unset GOFLAGS
export GOWORK=off

packages=(
	./internal/recoverygate
	./internal/effect/fileset
	./internal/workflow/apply
	./internal/workflow/refresh
	./internal/workflow/recover
)
arguments=(-mod=readonly -count=1)
if ((race)); then
	tools/test-race-proof.sh
	export GORACE=atexit_sleep_ms=0
	arguments+=(-race)
fi

if [[ $(go env GOOS) == windows ]]; then
	exec go test "${arguments[@]}" "${packages[@]}"
fi
exec tools/test-go.sh "${arguments[@]}" "${packages[@]}"
