#!/bin/sh

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
      if (lead == "0.") return 1
      return length(lead) >= 3 && substr(lead, length(lead) - 2) == ".0."
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
    daem_admitted_release_timestamp "$2" &&
    daem_admitted_release_go_version "$3"
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
  daem_admitted_release_requirement "$2" "$4" "$5" || return 1
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

daem_resolve_release_revision() {
  daem_metadata_revision="$2.tag-commit"
  if ! curl --fail --silent --show-error --connect-timeout 10 --max-time 60 --max-filesize 64 \
    --header 'Accept: application/vnd.github.sha' \
    --header 'X-GitHub-Api-Version: 2022-11-28' \
    --output "$daem_metadata_revision" \
    "${DAEM_ORIGIN_API}/commits/refs/tags/$1"; then
    return 1
  fi
  daem_metadata_bytes="$(/usr/bin/wc -c < "$daem_metadata_revision")"
  [ "$daem_metadata_bytes" -eq 40 ] || return 1
  daem_revision="$(/bin/cat "$daem_metadata_revision")"
  daem_admitted_release_revision "$daem_revision" || return 1
  printf '%s\n' "$daem_revision"
}

daem_commit_revision_time() {
  [ "$(/usr/bin/wc -c < "$1")" -le 65536 ] || return 1
  daem_commit_time="$(/usr/bin/awk -v expected_sha="$2" '
    function fail() { exit 1 }
    function whitespace() {
      while (substr(document, position, 1) ~ /^[ \t\r\n]$/) position++
    }
    function string_token(key, character, escaped, digits) {
      if (substr(document, position++, 1) != "\"") fail()
      token = ""
      token_escaped = 0
      while (position <= length(document)) {
        character = substr(document, position++, 1)
        if (character == "\"") return
        if (character ~ /[[:cntrl:]]/) fail()
        if (character == "\\") {
          if (key) fail()
          token_escaped = 1
          escaped = substr(document, position++, 1)
          if (escaped == "u") {
            digits = substr(document, position, 4)
            if (length(digits) != 4 || digits ~ /[^0-9a-fA-F]/) fail()
            position += 4
          } else if (escaped !~ /^["\\\/bfnrt]$/) fail()
          character = "?"
        }
        token = token character
      }
      fail()
    }
    function value(depth, path, character, key, object, selected, rest) {
      if (depth > 32) fail()
      whitespace()
      character = substr(document, position, 1)
      selected = (path == SUBSEP "sha" || path == SUBSEP "committer" SUBSEP "date")
      if (selected && character != "\"") fail()
      if (character == "\"") {
        string_token(0)
        if (selected && token_escaped) fail()
        if (path == SUBSEP "sha") sha = token
        if (path == SUBSEP "committer" SUBSEP "date") date = token
        return
      }
      if (character == "{") {
        position++
        object = ++object_count
        whitespace()
        if (substr(document, position, 1) == "}") { position++; return }
        while (1) {
          whitespace()
          string_token(1)
          key = token
          if (seen[object SUBSEP key]++) fail()
          whitespace()
          if (substr(document, position++, 1) != ":") fail()
          value(depth + 1, path SUBSEP key)
          whitespace()
          character = substr(document, position++, 1)
          if (character == "}") return
          if (character != ",") fail()
        }
      }
      if (character == "[") {
        position++
        whitespace()
        if (substr(document, position, 1) == "]") { position++; return }
        while (1) {
          value(depth + 1, path SUBSEP "[]")
          whitespace()
          character = substr(document, position++, 1)
          if (character == "]") return
          if (character != ",") fail()
        }
      }
      rest = substr(document, position)
      if (match(rest, /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?/)) {
        position += RLENGTH
        return
      }
      if (substr(rest, 1, 4) == "true" || substr(rest, 1, 4) == "null") {
        position += 4
        return
      }
      if (substr(rest, 1, 5) == "false") { position += 5; return }
      fail()
    }
    { document = document $0 "\n" }
    END {
      position = 1
      whitespace()
      if (substr(document, position, 1) != "{") fail()
      value(0, "")
      whitespace()
      if (position <= length(document) || sha != expected_sha || date == "") fail()
      print date
    }
  ' "$1")" || return 1
  daem_admitted_release_timestamp "$daem_commit_time" || return 1
  printf '%s\n' "$daem_commit_time"
}

daem_release_toolchain() {
  [ "$(/usr/bin/wc -c < "$1")" -le 65536 ] || return 1
  daem_toolchain="$(/usr/bin/awk '
    $1 == "toolchain" {
      if (seen++) exit 1
      directive = $0
      comment = index(directive, "//")
      if (comment > 0) directive = substr(directive, 1, comment - 1)
      sub(/^[[:space:]]*toolchain[[:space:]]+/, "", directive)
      sub(/[[:space:]]+$/, "", directive)
      if (directive == "" || directive ~ /[[:space:]]/) exit 1
      toolchain = directive
    }
    END {
      if (seen != 1 || toolchain == "") exit 1
      print toolchain
    }
  ' "$1")" || return 1
  daem_admitted_release_go_version "$daem_toolchain" || return 1
  printf '%s\n' "$daem_toolchain"
}

daem_latest_release() {
  daem_latest_url="$(curl --fail --silent --show-error --location --head \
    --connect-timeout 10 --max-time 60 --max-redirs 5 \
    --output /dev/null --write-out '%{url_effective}' \
    "$DAEM_ORIGIN_WEB/releases/latest")" || return 1
  case "$daem_latest_url" in
    "$DAEM_ORIGIN_WEB/releases/tag/"*) ;;
    *) return 1 ;;
  esac
  daem_latest_version="${daem_latest_url#"$DAEM_ORIGIN_WEB/releases/tag/"}"
  daem_admitted_release_version_token "$daem_latest_version" || return 1
  case "$daem_latest_version" in *-*) return 1 ;; esac
  printf '%s\n' "$daem_latest_version"
}

daem_download() {
  curl --fail --silent --show-error --location --connect-timeout 10 \
    --max-time 300 --max-redirs 5 --max-filesize "$3" --output "$2" "$1"
}

daem_install_fail() {
  printf 'daem install: %s\n' "$*" >&2
  exit 1
}

daem_install_help() {
  printf '%s\n' \
    'Install or upgrade daem from GitHub Releases.' \
    '' \
    "Usage: sh ${0##*/} [--version TAG] [--bin-dir DIR]" \
    "       sh ${0##*/} --rollback [--bin-dir DIR]" \
    '' \
    '  --version TAG  Install an exact release (default: latest stable)' \
    '  --bin-dir DIR  Install directory (default: ~/.local/bin)' \
    '  --rollback     Restore daem.previous without network access' \
    '  --help         Show this help' \
    '' \
    'Supports macOS 26+ on Apple silicon and Linux on x86-64.' \
    'Does not edit PATH, shell profiles, manifests or managed state.'
}

daem_install_cleanup() {
  if [ -n "$DAEM_DEST_STAGE" ]; then rm -rf "$DAEM_DEST_STAGE"; fi
  if [ -n "$DAEM_STAGE" ]; then rm -rf "$DAEM_STAGE"; fi
}

daem_install_platform() {
  DAEM_SYSTEM="$(uname -s)" || daem_install_fail 'cannot observe the operating system'
  DAEM_MACHINE="$(uname -m)" || daem_install_fail 'cannot observe the architecture'
  DAEM_TRANSLATED=''
  if [ "$DAEM_SYSTEM:$DAEM_MACHINE" = Darwin:x86_64 ]; then
    DAEM_TRANSLATED="$(sysctl -in sysctl.proc_translated 2>/dev/null || true)"
  fi
  DAEM_TARGET="$(daem_release_target "$DAEM_SYSTEM" "$DAEM_MACHINE" "$DAEM_TRANSLATED")" ||
    daem_install_fail "unsupported daem release target: $DAEM_SYSTEM/$DAEM_MACHINE"
  if [ "$DAEM_TARGET" = darwin_arm64 ]; then
    sw_vers --productVersion > "$DAEM_STAGE/macos-product-version" ||
      daem_install_fail 'cannot observe macOS version; requires macOS 26.0 or newer'
    daem_admitted_macos_product_version < "$DAEM_STAGE/macos-product-version" >/dev/null ||
      daem_install_fail 'unsupported or malformed daem macOS runtime; requires macOS 26.0 or newer'
  fi
}

daem_install_destination() {
  DAEM_BIN="$DAEM_BIN_DIR/daem"
  for daem_destination in "$DAEM_BIN" "$DAEM_BIN.previous"; do
    if [ -e "$daem_destination" ] && [ ! -f "$daem_destination" ]; then
      daem_install_fail "not a regular file: $daem_destination"
    fi
  done
  install -d "$DAEM_BIN_DIR" || daem_install_fail "cannot create $DAEM_BIN_DIR"
  DAEM_DEST_STAGE="$(mktemp -d "$DAEM_BIN_DIR/.daem-install.XXXXXX")" ||
    daem_install_fail "cannot stage an executable in $DAEM_BIN_DIR"
}

daem_install_rollback() {
  [ -f "$DAEM_BIN_DIR/daem.previous" ] && [ -x "$DAEM_BIN_DIR/daem.previous" ] ||
    daem_install_fail "no previous executable at $DAEM_BIN_DIR/daem.previous"
  daem_install_destination
  install -m 0755 "$DAEM_BIN.previous" "$DAEM_DEST_STAGE/daem" ||
    daem_install_fail 'cannot prepare the previous executable; current executable unchanged'
  "$DAEM_DEST_STAGE/daem" version --json ||
    daem_install_fail 'previous executable did not report its identity; current executable unchanged'
  mv -f "$DAEM_DEST_STAGE/daem" "$DAEM_BIN" ||
    daem_install_fail 'cannot restore the previous executable'
  printf 'Restored %s from daem.previous. Managed data was not rolled back.\n' "$DAEM_BIN"
}

daem_install_main() {
  set -eu
  LC_ALL=C
  export LC_ALL
  DAEM_VERSION=''
  DAEM_BIN_DIR=''
  DAEM_ROLLBACK=0
  DAEM_HELP=0
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --help)
        [ "$DAEM_HELP" -eq 0 ] || daem_install_fail '--help may be given only once'
        DAEM_HELP=1
        shift ;;
      --version)
        [ "$#" -ge 2 ] && [ -n "$2" ] && [ -z "$DAEM_VERSION" ] ||
          daem_install_fail '--version requires one release tag and may be given only once'
        DAEM_VERSION="$2"
        shift 2 ;;
      --bin-dir)
        [ "$#" -ge 2 ] && [ -n "$2" ] && [ -z "$DAEM_BIN_DIR" ] ||
          daem_install_fail '--bin-dir requires one directory and may be given only once'
        case "$2" in
          -*) daem_install_fail '--bin-dir requires a directory; prefix a leading-dash name with ./' ;;
        esac
        DAEM_BIN_DIR="$2"
        shift 2 ;;
      --rollback)
        [ "$DAEM_ROLLBACK" -eq 0 ] || daem_install_fail '--rollback may be given only once'
        DAEM_ROLLBACK=1
        shift ;;
      *) daem_install_fail "unknown argument: $1; use --help" ;;
    esac
  done
  if [ -n "$DAEM_VERSION" ]; then
    [ "$DAEM_ROLLBACK" -eq 0 ] || daem_install_fail '--version cannot be combined with --rollback'
    daem_admitted_release_version_token "$DAEM_VERSION" || daem_install_fail 'invalid release tag'
  fi
  if [ "$DAEM_HELP" -eq 1 ]; then daem_install_help; return 0; fi
  if [ -z "$DAEM_BIN_DIR" ]; then
    [ -n "${HOME:-}" ] || daem_install_fail 'set HOME or pass --bin-dir'
    DAEM_BIN_DIR="$HOME/.local/bin"
  fi
  case "$DAEM_BIN_DIR" in
    /*) ;;
    *) DAEM_BIN_DIR="$(pwd)/$DAEM_BIN_DIR" ;;
  esac
  DAEM_STAGE=''
  DAEM_DEST_STAGE=''
  trap 'daem_install_cleanup' 0
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
  DAEM_STAGE="$(mktemp -d "${TMPDIR:-/tmp}/daem-install.XXXXXX")" ||
    daem_install_fail 'cannot create temporary download directory'
  daem_install_platform
  if [ "$DAEM_ROLLBACK" -eq 1 ]; then daem_install_rollback; return 0; fi

  command -v curl >/dev/null 2>&1 || daem_install_fail 'curl is required'
  DAEM_ORIGIN_API='https://api.github.com/repos/isty2e/daem'
  DAEM_ORIGIN_WEB='https://github.com/isty2e/daem'
  if [ -z "$DAEM_VERSION" ]; then
    DAEM_VERSION="$(daem_latest_release)" ||
      daem_install_fail 'cannot discover the latest stable release; retry or use --version TAG'
  fi
  printf 'Installing daem %s for %s\n' "$DAEM_VERSION" "$DAEM_TARGET"
  DAEM_REVISION="$(daem_resolve_release_revision "$DAEM_VERSION" "$DAEM_STAGE/metadata")" ||
    daem_install_fail "cannot resolve release tag $DAEM_VERSION; check connectivity or API rate limits"
  curl --fail --silent --show-error --connect-timeout 10 --max-time 60 --max-filesize 65536 \
    --header 'Accept: application/vnd.github+json' \
    --header 'X-GitHub-Api-Version: 2022-11-28' \
    --output "$DAEM_STAGE/commit.json" "$DAEM_ORIGIN_API/git/commits/$DAEM_REVISION" ||
    daem_install_fail 'cannot read the selected release commit'
  DAEM_REVISION_TIME="$(daem_commit_revision_time "$DAEM_STAGE/commit.json" "$DAEM_REVISION")" ||
    daem_install_fail 'release commit metadata is malformed or does not match the resolved commit'
  daem_download "https://raw.githubusercontent.com/isty2e/daem/$DAEM_REVISION/go.mod" \
    "$DAEM_STAGE/go.mod" 65536 || daem_install_fail 'cannot read the selected release toolchain'
  DAEM_GO_VERSION="$(daem_release_toolchain "$DAEM_STAGE/go.mod")" ||
    daem_install_fail 'release go.mod must contain exactly one canonical toolchain directive'

  DAEM_ARCHIVE="daem_${DAEM_VERSION#v}_${DAEM_TARGET}.tar.gz"
  DAEM_BASE_URL="$DAEM_ORIGIN_WEB/releases/download/$DAEM_VERSION"
  daem_download "$DAEM_BASE_URL/$DAEM_ARCHIVE" "$DAEM_STAGE/$DAEM_ARCHIVE" 268435456 ||
    daem_install_fail 'cannot download the requested release archive; current executable unchanged'
  daem_download "$DAEM_BASE_URL/$DAEM_ARCHIVE.sha256" "$DAEM_STAGE/$DAEM_ARCHIVE.sha256" 512 ||
    daem_install_fail 'cannot download the requested release checksum; current executable unchanged'
  daem_verify_archive_checksum "$DAEM_STAGE/$DAEM_ARCHIVE" "$DAEM_STAGE/$DAEM_ARCHIVE.sha256" \
    "$DAEM_ARCHIVE" "$DAEM_SYSTEM" ||
    daem_install_fail 'downloaded archive does not match its exact checksum entry'
  daem_extract_release_binary "$DAEM_STAGE/$DAEM_ARCHIVE" "$DAEM_STAGE/extracted" \
    "$DAEM_STAGE/archive-entries" ||
    daem_install_fail 'downloaded archive must contain one regular executable named daem'
  DAEM_STAGED_BINARY="$DAEM_STAGE/extracted/daem"
  "$DAEM_STAGED_BINARY" version --json > "$DAEM_STAGE/version.json" ||
    daem_install_fail 'downloaded daem binary did not report its release identity'
  daem_release_binary_matches "$DAEM_STAGE/version.json" "$DAEM_VERSION" "$DAEM_REVISION" \
    "$DAEM_REVISION_TIME" "$DAEM_GO_VERSION" "$DAEM_TARGET" ||
    daem_install_fail 'downloaded daem binary does not match the requested release identity'

  if [ -f "$DAEM_BIN_DIR/daem" ] && [ -x "$DAEM_BIN_DIR/daem" ] &&
    cmp -s "$DAEM_STAGED_BINARY" "$DAEM_BIN_DIR/daem"; then
    printf 'daem %s is already installed at %s/daem; previous executable retained.\n' "$DAEM_VERSION" "$DAEM_BIN_DIR"
    return 0
  fi
  daem_install_destination
  install -m 0755 "$DAEM_STAGED_BINARY" "$DAEM_DEST_STAGE/daem" ||
    daem_install_fail 'cannot prepare the new executable; current and previous executables unchanged'
  if [ -x "$DAEM_BIN" ]; then
    install -m 0755 "$DAEM_BIN" "$DAEM_DEST_STAGE/previous" ||
      daem_install_fail 'cannot prepare the backup; current and previous executables unchanged'
    mv -f "$DAEM_DEST_STAGE/previous" "$DAEM_BIN.previous" ||
      daem_install_fail 'cannot save the backup; current executable unchanged'
  fi
  mv -f "$DAEM_DEST_STAGE/daem" "$DAEM_BIN" ||
    daem_install_fail 'cannot replace current executable; previous may have been refreshed to current'
  printf 'Installed daem %s at %s\n' "$DAEM_VERSION" "$DAEM_BIN"
  case ":${PATH:-}:" in
    *":$DAEM_BIN_DIR:"*) ;;
    *) printf 'Add %s to PATH to run daem by name. No shell profile was changed.\n' "$DAEM_BIN_DIR" ;;
  esac
}

daem_install_main "$@"
