#!/usr/bin/env bash

set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
cd "${repository_root}"

if ! command -v jq >/dev/null 2>&1; then
  echo "productive API verification requires jq" >&2
  exit 1
fi

evidence_dir=$(mktemp -d)
trap 'rm -rf "${evidence_dir}"' EXIT

go run golang.org/x/tools/cmd/deadcode@v0.48.0 -json ./... \
  >"${evidence_dir}/main.json"
go run golang.org/x/tools/cmd/deadcode@v0.48.0 -test -json ./... \
  >"${evidence_dir}/test.json"

for roots in main test; do
  jq -r '
    .[] as $package |
    $package.Funcs[] |
    select((.Name | split(".")[-1]) | test("^[A-Z]")) |
    [$package.Path, .Position.File, (.Position.Line | tostring), .Name] |
    @tsv
  ' "${evidence_dir}/${roots}.json" |
    sort -u >"${evidence_dir}/${roots}.tsv"
done

comm -23 \
  "${evidence_dir}/main.tsv" \
  "${evidence_dir}/test.tsv" \
  >"${evidence_dir}/test-only.tsv"

go list -json ./... |
  jq -r 'select((((.GoFiles // []) + (.CgoFiles // [])) | length) > 0) | .ImportPath' |
  sort -u >"${evidence_dir}/production-packages.tsv"
go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./... |
  sed '/^$/d' |
  sort -u >"${evidence_dir}/main-packages.tsv"
test -s "${evidence_dir}/main-packages.tsv"
xargs go list -deps <"${evidence_dir}/main-packages.tsv" |
  sort -u >"${evidence_dir}/main-dependencies.tsv"
comm -23 \
  "${evidence_dir}/production-packages.tsv" \
  "${evidence_dir}/main-dependencies.tsv" \
  >"${evidence_dir}/test-tool-packages.tsv"

awk -F '\t' '
  FILENAME == ARGV[1] { test_tools[$1] = 1; next }
  !($1 in test_tools) { print }
' "${evidence_dir}/test-tool-packages.tsv" \
  "${evidence_dir}/test-only.tsv" \
  >"${evidence_dir}/unadmitted.tsv"

if test -s "${evidence_dir}/unadmitted.tsv"; then
  echo "Exported production callables reachable only from tests:"
  cat "${evidence_dir}/unadmitted.tsv"
  exit 1
fi

echo "No unadmitted exported production callable is reachable only from tests."
