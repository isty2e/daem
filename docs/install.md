# Install, Upgrade, And Roll Back

GitHub Releases is the canonical source for published `daem` binaries. Every
supported release asset has a SHA-256 sidecar generated from the archive by the
same native verification lane and published alongside it.

## Supported Release Assets

| Platform | Asset suffix | Runtime requirement |
| --- | --- | --- |
| macOS on Apple silicon | `darwin_arm64` | macOS 26 or newer |
| Linux on x86-64 | `linux_amd64` | Supported native Linux environment |

Other builds are not release installation targets even when the source
cross-compiles for them. See [Platform Support](platforms.md) for the complete
support and evidence contract.

## Install

Set the exact version instead of relying on a moving latest-release URL. This
example installs `v0.1.0` under `~/.local/bin`:

```bash
set -eu

daem_admitted_release_version_token() {
  printf '%s\n' "$1" | /usr/bin/awk '
    function canonical_number(value) {
      return value ~ /^(0|[1-9][0-9]*)$/
    }
    function canonical_prerelease(value, count, identifiers, identifier_index, identifier) {
      if (value == "") return 0
      count = split(value, identifiers, ".")
      for (identifier_index = 1; identifier_index <= count; identifier_index++) {
        identifier = identifiers[identifier_index]
        if (identifier == "" || identifier ~ /[^0-9A-Za-z-]/) return 0
        if (identifier ~ /^[0-9]+$/ && !canonical_number(identifier)) return 0
      }
      return 1
    }
    BEGIN { valid = 0 }
    NR == 1 {
      if (length($0) > 255 || substr($0, 1, 1) != "v" || index($0, "+") != 0) next
      version = substr($0, 2)
      prerelease_offset = index(version, "-")
      if (prerelease_offset == 0) {
        core = version
      } else {
        core = substr(version, 1, prerelease_offset - 1)
        prerelease = substr(version, prerelease_offset + 1)
      }
      if (split(core, components, ".") != 3) next
      if (!canonical_number(components[1]) || !canonical_number(components[2]) ||
          !canonical_number(components[3])) next
      if (prerelease_offset != 0 && !canonical_prerelease(prerelease)) next
      valid = 1
      next
    }
    { valid = 0 }
    END { if (NR != 1 || valid != 1) exit 1 }
  '
}

daem_release_target() {
  case "$1:$2:$3" in
    Darwin:arm64:*) printf '%s\n' darwin_arm64 ;;
    Darwin:x86_64:1) printf '%s\n' darwin_arm64 ;;
    Linux:x86_64:*) printf '%s\n' linux_amd64 ;;
    *) return 1 ;;
  esac
}

daem_admitted_macos_product_version() {
  /usr/bin/awk -F. '
    BEGIN { valid = 1 }
    NR > 1 { valid = 0; next }
    NF < 2 || NF > 3 { valid = 0; next }
    {
      for (field = 1; field <= NF; field++) {
        if ($field !~ /^(0|[1-9][0-9]*)$/) { valid = 0; next }
        if (length($field) > 10 || (length($field) == 10 && ($field + 0) > 4294967295)) { valid = 0; next }
      }
      if (($1 + 0) < 26) { valid = 0; next }
      version = $0
    }
    END {
      if (NR != 1 || valid != 1) exit 1
      print version
    }
  '
}

daem_verify_archive_checksum() {
  case "$4" in
    Darwin) shasum -a 256 "$1" > "$2.actual" || return 1 ;;
    Linux) sha256sum "$1" > "$2.actual" || return 1 ;;
    *) return 1 ;;
  esac
  actual="$(/usr/bin/awk 'NR == 1 { print $1; next } { exit 1 } END { if (NR != 1) exit 1 }' "$2.actual")" || return 1
  printf '%s  %s\n' "$actual" "$3" > "$2.expected" || return 1
  cmp -s "$2.expected" "$2"
}

daem_extract_release_binary() {
  tar -tzf "$1" > "$3" || return 1
  printf 'daem\n' > "$3.expected" || return 1
  cmp -s "$3.expected" "$3" || return 1
  mkdir "$2" || return 1
  tar -xzf "$1" -C "$2" daem || return 1
  test -f "$2/daem" && test ! -L "$2/daem" && test -x "$2/daem"
}

daem_release_binary_matches() {
  case "$3" in
    darwin_arm64) expected_goos=darwin; expected_goarch=arm64 ;;
    linux_amd64) expected_goos=linux; expected_goarch=amd64 ;;
    *) return 1 ;;
  esac
  /usr/bin/awk -v expected_version="$2" -v expected_goos="$expected_goos" -v expected_goarch="$expected_goarch" '
    function compact_json(input, output, position, character, quoted) {
      for (position = 1; position <= length(input); position++) {
        character = substr(input, position, 1)
        if (character == "\\") { invalid = 1; return "" }
        if (character == "\"") quoted = !quoted
        if (quoted || character !~ /[[:space:]]/) output = output character
      }
      if (quoted) invalid = 1
      return output
    }
    { document = document $0 "\n" }
    END {
      document = compact_json(document)
      if (invalid || substr(document, 1, 1) != "{" || substr(document, length(document), 1) != "}") exit 1
      document = substr(document, 2, length(document) - 2)
      if (split(document, fields, ",") != 9) exit 1

      for (field_index = 1; field_index <= 9; field_index++) {
        separator = index(fields[field_index], ":")
        if (separator == 0) exit 1
        key = substr(fields[field_index], 1, separator - 1)
        value = substr(fields[field_index], separator + 1)
        if (seen[key]++) exit 1

        if (key == "\"schema_version\"") {
          if (value != "1") exit 1
          schema = 1
        } else if (key == "\"version\"") {
          if (value != "\"" expected_version "\"") exit 1
          version = 1
        } else if (key == "\"revision\"") {
          revision_value = substr(value, 2, length(value) - 2)
          if (substr(value, 1, 1) != "\"" || substr(value, length(value), 1) != "\"" ||
              length(revision_value) != 40 || revision_value ~ /[^0-9a-f]/) exit 1
          revision = 1
        } else if (key == "\"revision_time\"") {
          revision_time_value = substr(value, 2, length(value) - 2)
          if (substr(value, 1, 1) != "\"" || substr(value, length(value), 1) != "\"" ||
              revision_time_value == "" || revision_time_value == "unknown" ||
              revision_time_value ~ /[^0-9TZ:+.-]/) exit 1
          revision_time = 1
        } else if (key == "\"source_state\"") {
          if (value != "\"clean\"") exit 1
          source = 1
        } else if (key == "\"vcs\"") {
          if (value != "\"git\"") exit 1
          vcs = 1
        } else if (key == "\"go_version\"") {
          go_version_value = substr(value, 2, length(value) - 2)
          if (substr(value, 1, 1) != "\"" || substr(value, length(value), 1) != "\"" ||
              go_version_value !~ /^go[0-9]/ || go_version_value ~ /[^0-9A-Za-z.+-]/) exit 1
          go_version = 1
        } else if (key == "\"goos\"") {
          if (value != "\"" expected_goos "\"") exit 1
          goos = 1
        } else if (key == "\"goarch\"") {
          if (value != "\"" expected_goarch "\"") exit 1
          goarch = 1
        } else {
          exit 1
        }
      }

      if (!schema || !version || !revision || !revision_time || !source ||
          !vcs || !go_version || !goos || !goarch) exit 1
    }
  ' "$1"
}

DAEM_VERSION=v0.1.0
if ! daem_admitted_release_version_token "$DAEM_VERSION"; then
  echo "invalid daem release version token: $DAEM_VERSION" >&2
  exit 1
fi

DAEM_STAGE="$(mktemp -d)"
trap 'rm -rf "$DAEM_STAGE"' EXIT

DAEM_SYSTEM="$(uname -s)"
DAEM_MACHINE="$(uname -m)"
DAEM_TRANSLATED=""
if [ "$DAEM_SYSTEM:$DAEM_MACHINE" = Darwin:x86_64 ]; then
  DAEM_TRANSLATED="$(/usr/sbin/sysctl -in sysctl.proc_translated 2>/dev/null || true)"
fi
if ! DAEM_TARGET="$(daem_release_target "$DAEM_SYSTEM" "$DAEM_MACHINE" "$DAEM_TRANSLATED")"; then
  echo "unsupported daem release target: $DAEM_SYSTEM/$DAEM_MACHINE" >&2
  exit 1
fi

case "$DAEM_TARGET" in
  darwin_arm64)
    DAEM_MACOS_VERSION_FILE="$DAEM_STAGE/macos-product-version"
    if ! /usr/bin/sw_vers --productVersion > "$DAEM_MACOS_VERSION_FILE"; then
      echo "cannot observe the macOS product version; daem requires macOS 26.0 or newer" >&2
      exit 1
    fi
    if ! daem_admitted_macos_product_version < "$DAEM_MACOS_VERSION_FILE" > /dev/null; then
      echo "unsupported or malformed daem macOS runtime; requires macOS 26.0 or newer" >&2
      exit 1
    fi
    ;;
esac

DAEM_ARCHIVE="daem_${DAEM_VERSION#v}_${DAEM_TARGET}.tar.gz"
DAEM_BASE_URL="https://github.com/isty2e/daem/releases/download/${DAEM_VERSION}"

curl --fail --location \
  --output "$DAEM_STAGE/$DAEM_ARCHIVE" \
  "$DAEM_BASE_URL/$DAEM_ARCHIVE"
curl --fail --location \
  --output "$DAEM_STAGE/$DAEM_ARCHIVE.sha256" \
  "$DAEM_BASE_URL/$DAEM_ARCHIVE.sha256"

if ! daem_verify_archive_checksum \
  "$DAEM_STAGE/$DAEM_ARCHIVE" \
  "$DAEM_STAGE/$DAEM_ARCHIVE.sha256" \
  "$DAEM_ARCHIVE" \
  "$DAEM_SYSTEM"; then
  echo "downloaded archive does not match its exact checksum entry" >&2
  exit 1
fi

DAEM_EXTRACT="$DAEM_STAGE/extracted"
DAEM_ARCHIVE_ENTRIES="$DAEM_STAGE/archive-entries"
if ! daem_extract_release_binary \
  "$DAEM_STAGE/$DAEM_ARCHIVE" \
  "$DAEM_EXTRACT" \
  "$DAEM_ARCHIVE_ENTRIES"; then
  echo "downloaded archive must contain one regular executable named daem" >&2
  exit 1
fi

DAEM_STAGED_BINARY="$DAEM_EXTRACT/daem"
DAEM_VERSION_JSON="$DAEM_STAGE/version.json"
if ! "$DAEM_STAGED_BINARY" version --json > "$DAEM_VERSION_JSON"; then
  echo "downloaded daem binary did not report its release identity" >&2
  exit 1
fi
if ! daem_release_binary_matches "$DAEM_VERSION_JSON" "$DAEM_VERSION" "$DAEM_TARGET"; then
  echo "downloaded daem binary does not match the requested release identity" >&2
  exit 1
fi
cat "$DAEM_VERSION_JSON"

DAEM_BIN="$HOME/.local/bin/daem"
install -d "$HOME/.local/bin"
if [ -x "$DAEM_BIN" ]; then
  install -m 0755 "$DAEM_BIN" "$DAEM_BIN.previous"
fi
install -m 0755 "$DAEM_STAGED_BINARY" "$DAEM_BIN.new"
mv -f "$DAEM_BIN.new" "$DAEM_BIN"

export PATH="$HOME/.local/bin:$PATH"
daem version
daem --help
```

Add `export PATH="$HOME/.local/bin:$PATH"` to the appropriate shell startup
file if `~/.local/bin` is not already on `PATH`.

The macOS preflight runs before network access. A translated x86-64 shell is
treated as Apple silicon only when macOS reports `sysctl.proc_translated=1`;
Intel Macs remain unsupported. The installed binary repeats the runtime-floor
decision for supported workflows, so the shell check cannot bypass the binary
gate.

The recipe accepts exactly one checksum entry for the requested archive, then
requires one regular executable named `daem` in that archive. Before replacing
an installed binary, it verifies that the staged executable reports the exact
requested version and target, a full Git revision, a known revision time, Git
VCS metadata, and a clean source state. These checks detect transfer errors and
release-assembly mismatches. The archive and its checksum sidecar share the same
mutable GitHub release authority. This does not prove publisher identity,
provenance, or post-publication immutability. Confirm that both downloads came
from the expected GitHub repository and HTTPS endpoint.

## Release Mutability

Release `v0.1.0` was published while GitHub release immutability was disabled
and remains a mutable GitHub release. Daem currently does not guarantee that a
published tag or attached asset cannot be changed or deleted after publication.
Pin an exact version when installing or rolling back, and verify its checksum
sidecar, but do not treat the version and co-published checksum as historical
immutability evidence. The moving "Latest" label is a discovery aid, not an
artifact identity.

## Upgrade

Read the target release notes, set `DAEM_VERSION` to the exact newer tag, and
repeat the install procedure. The staged binary reports its identity before it
replaces the current executable. A pre-existing executable is retained as
`~/.local/bin/daem.previous`.

Before running a mutating command with the new binary:

```bash
daem version --json
daem status
daem apply --dry-run --diff
```

Upgrading the executable does not itself rewrite a manifest, lockfile,
statefile, host configuration, or managed output. Normal commands may migrate
or reject persisted formats according to their own current contracts.

## Roll Back The Executable

Rollback restores only the previous executable:

```bash
set -eu

DAEM_BIN="$HOME/.local/bin/daem"
test -x "$DAEM_BIN.previous"
"$DAEM_BIN.previous" version --json
install -m 0755 "$DAEM_BIN.previous" "$DAEM_BIN.rollback"
mv -f "$DAEM_BIN.rollback" "$DAEM_BIN"
"$DAEM_BIN" version
```

This does not roll back manifests, lockfiles, statefiles, recovery journals, or
host mutations. If the previous executable rejects data written by the newer
version, stop and restore the newer verified binary. Do not delete or hand-edit
managed state to force a downgrade. `daem recover` repairs interrupted apply
operations; it is not a binary or schema downgrade command.

## Diagnostics

Record these facts when reporting an installation problem:

```bash
daem version --json
daem doctor --all-targets
```

For one selected workspace, also include:

```bash
daem status --json
daem list paths
```

Review output for local paths or other machine-specific data before sharing it.

## Build From Source

Source builds are intended for contributors and unreleased development
testing. They are not substitutes for native release-lane evidence:

```bash
git clone https://github.com/isty2e/daem.git
cd daem
go install ./cmd/daem
```

Use a current security patch of Go 1.25 or later. A source build normally
reports a development or pseudo-version rather than an official release tag.
