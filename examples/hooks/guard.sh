#!/bin/sh
set -eu

if [ "${1:-}" != "--check" ]; then
    printf '%s\n' 'usage: guard.sh --check' >&2
    exit 2
fi
