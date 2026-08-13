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

Select the complete release requirement instead of relying on a moving
latest-release URL. A release requirement includes the tag, commit, commit
time, Go toolchain, and native target. This example installs `v0.1.0` under
`~/.local/bin`:

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
    function go_pseudo_version(value, minor, patch, position, last_hyphen, before_hash, hash, timestamp, lead) {
      for (position = 1; position <= length(value); position++) {
        if (substr(value, position, 1) == "-") last_hyphen = position
      }
      if (last_hyphen == 0) return 0
      before_hash = substr(value, 1, last_hyphen - 1)
      hash = substr(value, last_hyphen + 1)
      if (hash == "" || hash ~ /[^0-9A-Za-z]/ || length(before_hash) < 14) return 0
      timestamp = substr(before_hash, length(before_hash) - 13)
      if (timestamp ~ /[^0-9]/) return 0
      lead = substr(before_hash, 1, length(before_hash) - 14)
      if (minor == "0" && patch == "0" && lead == "") return 1
      return length(lead) >= 2 && substr(lead, length(lead) - 1) == "0."
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
      if (prerelease_offset != 0 && go_pseudo_version(prerelease, components[2], components[3])) next
      valid = 1
      next
    }
    { valid = 0 }
    END { if (NR != 1 || valid != 1) exit 1 }
  '
}

daem_admitted_release_revision() {
  printf '%s\n' "$1" | /usr/bin/awk '
    NR == 1 {
      if (length($0) == 40 && $0 !~ /[^0-9a-f]/) valid = 1
      next
    }
    { valid = 0 }
    END { if (NR != 1 || valid != 1) exit 1 }
  '
}

daem_admitted_release_timestamp() {
  printf '%s\n' "$1" | /usr/bin/awk '
    function digits(value) { return value != "" && value !~ /[^0-9]/ }
    function leap_year(year) { return year % 400 == 0 || (year % 4 == 0 && year % 100 != 0) }
    BEGIN { valid = 0 }
    NR == 1 {
      value = $0
      if (length(value) < 20 || length(value) > 30 ||
          substr(value, 5, 1) != "-" || substr(value, 8, 1) != "-" ||
          substr(value, 11, 1) != "T" || substr(value, 14, 1) != ":" ||
          substr(value, 17, 1) != ":" || substr(value, length(value), 1) != "Z") next

      year_text = substr(value, 1, 4)
      month_text = substr(value, 6, 2)
      day_text = substr(value, 9, 2)
      hour_text = substr(value, 12, 2)
      minute_text = substr(value, 15, 2)
      second_text = substr(value, 18, 2)
      if (!digits(year_text) || !digits(month_text) || !digits(day_text) ||
          !digits(hour_text) || !digits(minute_text) || !digits(second_text)) next

      if (length(value) == 20) {
        if (substr(value, 20, 1) != "Z") next
      } else {
        if (substr(value, 20, 1) != ".") next
        fraction = substr(value, 21, length(value) - 21)
        if (length(fraction) < 1 || length(fraction) > 9 || !digits(fraction) ||
            substr(fraction, length(fraction), 1) == "0") next
      }

      year = year_text + 0
      month = month_text + 0
      day = day_text + 0
      hour = hour_text + 0
      minute = minute_text + 0
      second = second_text + 0
      if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) next
      maximum_day = 31
      if (month == 4 || month == 6 || month == 9 || month == 11) maximum_day = 30
      if (month == 2) maximum_day = leap_year(year) ? 29 : 28
      if (day < 1 || day > maximum_day) next
      valid = 1
      next
    }
    { valid = 0 }
    END { if (NR != 1 || valid != 1) exit 1 }
  '
}

daem_admitted_release_go_version() {
  printf '%s\n' "$1" | /usr/bin/awk '
    function canonical_number(value) { return value ~ /^(0|[1-9][0-9]*)$/ }
    BEGIN { valid = 0 }
    NR == 1 {
      if (substr($0, 1, 2) != "go") next
      if (split(substr($0, 3), components, ".") != 3) next
      if (!canonical_number(components[1]) || !canonical_number(components[2]) ||
          !canonical_number(components[3])) next
      valid = 1
      next
    }
    { valid = 0 }
    END { if (NR != 1 || valid != 1) exit 1 }
  '
}

daem_admitted_release_requirement() {
  daem_admitted_release_version_token "$1" &&
    daem_admitted_release_revision "$2" &&
    daem_admitted_release_timestamp "$3" &&
    daem_admitted_release_go_version "$4"
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
    Darwin) shasum -a 256 < "$1" > "$2.actual" || return 1 ;;
    Linux) sha256sum < "$1" > "$2.actual" || return 1 ;;
    *) return 1 ;;
  esac
  actual="$(/usr/bin/awk '
    BEGIN { valid = 0 }
    NR == 1 {
      if (length($1) == 64 && $1 !~ /[^0-9a-f]/) {
        actual = $1
        valid = 1
      }
      next
    }
    { valid = 0 }
    END {
      if (NR != 1 || valid != 1) exit 1
      print actual
    }
  ' "$2.actual")" || return 1
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
  daem_admitted_release_requirement "$2" "$3" "$4" "$5" || return 1
  case "$6" in
    darwin_arm64) expected_goos=darwin; expected_goarch=arm64 ;;
    linux_amd64) expected_goos=linux; expected_goarch=amd64 ;;
    *) return 1 ;;
  esac
  /usr/bin/awk -v expected_version="$2" -v expected_revision="$3" \
    -v expected_revision_time="$4" -v expected_go_version="$5" \
    -v expected_goos="$expected_goos" -v expected_goarch="$expected_goarch" '
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
              revision_value != expected_revision) exit 1
          revision = 1
        } else if (key == "\"revision_time\"") {
          revision_time_value = substr(value, 2, length(value) - 2)
          if (substr(value, 1, 1) != "\"" || substr(value, length(value), 1) != "\"" ||
              revision_time_value != expected_revision_time) exit 1
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
              go_version_value != expected_go_version) exit 1
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
DAEM_REVISION=2bf957187f9f847aa87b0e807d6ca960589f1083
DAEM_REVISION_TIME=2026-07-28T02:19:30Z
DAEM_GO_VERSION=go1.26.5
if ! daem_admitted_release_requirement \
  "$DAEM_VERSION" "$DAEM_REVISION" "$DAEM_REVISION_TIME" "$DAEM_GO_VERSION"; then
  echo "invalid daem release requirement" >&2
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
if ! daem_release_binary_matches \
  "$DAEM_VERSION_JSON" \
  "$DAEM_VERSION" \
  "$DAEM_REVISION" \
  "$DAEM_REVISION_TIME" \
  "$DAEM_GO_VERSION" \
  "$DAEM_TARGET"; then
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
an installed binary, it verifies that the staged executable exactly matches the
selected tag, commit, commit time, Go toolchain, and native target, and that it
reports Git VCS metadata and a clean source state. These checks detect transfer
errors and release-assembly mismatches. The archive and its checksum sidecar
share the same mutable GitHub release authority. This does not prove publisher
identity, provenance, or post-publication immutability. Confirm that both
downloads came from the expected GitHub repository and HTTPS endpoint.

## Release Mutability

Release `v0.1.0` was published while GitHub release immutability was disabled
and remains a mutable GitHub release. Daem currently does not guarantee that a
published tag or attached asset cannot be changed or deleted after publication.
Pin an exact version when installing or rolling back, and verify its checksum
sidecar, but do not treat the version and co-published checksum as historical
immutability evidence. The moving "Latest" label is a discovery aid, not an
artifact identity.

## Upgrade

Use the complete install recipe from the target release's tagged documentation.
Do not change `DAEM_VERSION` alone: `DAEM_VERSION`, `DAEM_REVISION`,
`DAEM_REVISION_TIME`, and `DAEM_GO_VERSION` form one release requirement. The
release build checks these values against the binary before assembling its
archive. The staged binary reports the same identity before it replaces the
current executable. A pre-existing executable is retained as
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
