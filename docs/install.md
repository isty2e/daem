# Install, Upgrade, And Roll Back

GitHub Releases is the canonical source for published `daem` binaries. Every
supported release asset has a SHA-256 sidecar produced from the same immutable
tag and native verification lane.

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

DAEM_VERSION=v0.1.0
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) DAEM_TARGET=darwin_arm64 ;;
  Linux:x86_64) DAEM_TARGET=linux_amd64 ;;
  *)
    echo "unsupported daem release target: $(uname -s)/$(uname -m)" >&2
    exit 1
    ;;
esac

DAEM_ARCHIVE="daem_${DAEM_VERSION#v}_${DAEM_TARGET}.tar.gz"
DAEM_BASE_URL="https://github.com/isty2e/daem/releases/download/${DAEM_VERSION}"
DAEM_STAGE="$(mktemp -d)"
trap 'rm -rf "$DAEM_STAGE"' EXIT

curl --fail --location \
  --output "$DAEM_STAGE/$DAEM_ARCHIVE" \
  "$DAEM_BASE_URL/$DAEM_ARCHIVE"
curl --fail --location \
  --output "$DAEM_STAGE/$DAEM_ARCHIVE.sha256" \
  "$DAEM_BASE_URL/$DAEM_ARCHIVE.sha256"

(
  cd "$DAEM_STAGE"
  case "$(uname -s)" in
    Darwin) shasum -a 256 -c "$DAEM_ARCHIVE.sha256" ;;
    Linux) sha256sum -c "$DAEM_ARCHIVE.sha256" ;;
  esac
  tar -xzf "$DAEM_ARCHIVE"
)

"$DAEM_STAGE/daem" version --json

DAEM_BIN="$HOME/.local/bin/daem"
install -d "$HOME/.local/bin"
if [ -x "$DAEM_BIN" ]; then
  install -m 0755 "$DAEM_BIN" "$DAEM_BIN.previous"
fi
install -m 0755 "$DAEM_STAGE/daem" "$DAEM_BIN.new"
mv -f "$DAEM_BIN.new" "$DAEM_BIN"

export PATH="$HOME/.local/bin:$PATH"
daem version
daem --help
```

Add `export PATH="$HOME/.local/bin:$PATH"` to the appropriate shell startup
file if `~/.local/bin` is not already on `PATH`.

The checksum detects accidental or malicious byte changes relative to the
published sidecar. It is not a signature, publisher-authenticity proof, or
substitute for verifying that the download came from the expected GitHub
repository and HTTPS endpoint.

## Release Immutability

Published release tags and attached assets are immutable. If a release is bad,
it is superseded by a new version; the existing tag and assets are not replaced.
Pin an exact version when installing or rolling back, and verify its checksum
sidecar. The moving "Latest" label is a discovery aid, not an artifact identity.

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
