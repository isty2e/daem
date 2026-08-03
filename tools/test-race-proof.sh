#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

export GORACE=atexit_sleep_ms=0
set +e
output=$(tools/test-go.sh \
	-mod=readonly \
	-race \
	-count=1 \
	-run '^TestIntentionalRaceDetectorProof$' \
	./test/tooling/testdata/raceprobe 2>&1)
status=$?
set -e

if ((status == 0)); then
	echo "intentional race probe passed; race detector evidence is absent" >&2
	exit 1
fi
if [[ ${output} != *"WARNING: DATA RACE"* ]]; then
	echo "intentional race probe failed without race detector evidence" >&2
	printf '%s\n' "${output}" >&2
	exit 1
fi

echo "race detector proof passed"
