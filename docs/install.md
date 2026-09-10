# Install, Upgrade, And Roll Back

Daem publishes binaries on [GitHub Releases](https://github.com/isty2e/daem/releases)
for macOS 26 or newer on Apple silicon and Linux on x86-64. Windows and other
targets are not supported releases. See [Platform Support](platforms.md) for
the full support policy.

## Install

Download the installer and run it:

```sh
curl -fsSL https://raw.githubusercontent.com/isty2e/daem/main/install.sh -o daem-install.sh &&
  sh daem-install.sh
```

The default is the latest stable release, installed at `~/.local/bin/daem`.
The script requires `curl` and standard Unix tools, including `tar`, `awk`,
`install`, and `shasum` on macOS or `sha256sum` on Linux. Go is not required.
It does not use `sudo` or edit shell profiles.

Choose a release or installation directory explicitly:

```sh
sh daem-install.sh --version v0.1.0
sh daem-install.sh --bin-dir "$HOME/bin"
sh daem-install.sh --help
```

`--version` selects the binary release; the download command above gets the
installer from the repository's `main` branch. Keep the downloaded script if
you want to use it later for offline rollback.

If the installation directory is not on `PATH`, the installer prints a
reminder. For the default directory, add this to the appropriate shell startup
file, or run it in the current shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
daem version --json
daem --help
```

### What The Installer Checks

Platform/runtime checks precede network access and destination changes. A
translated x86-64 macOS shell selects Apple silicon only with
`sysctl.proc_translated=1`; Intel Macs remain unsupported. The binary also
checks its runtime floor.

The installer verifies checksum, archive shape and executable release identity
before replacement. Download, validation or preparation failure preserves both
current and previous executables. It attempts to clean its temporary staging
on exit and handled interruption.

These checks detect transfer/assembly errors, not publisher provenance or
immutability: archive, checksum and metadata share GitHub's authority. Verify
the expected repository and HTTPS endpoints.

## Upgrade

Run the installer again to select the latest stable release, or pass an exact
`--version`. You can also repeat the download command above to get the current
installer first.

```sh
sh daem-install.sh
```

The replaced executable is retained as `daem.previous` in the same directory.
Reinstalling identical executable bytes leaves that backup alone. If you used
`--bin-dir`, pass the same directory when upgrading or rolling back.

Executable and backup replacement is not one atomic transaction. If final
replacement fails after backup publication, the current executable remains but
`daem.previous` may now contain that same executable. Do not run concurrent
installers against one directory.

Before running a mutating command with the new binary:

```sh
daem version --json
daem status
daem apply --dry-run --diff
```

Upgrading the executable does not rewrite manifests, lockfiles, statefiles,
host configuration, recovery state, or managed output. Normal daem commands
may migrate or reject persisted formats according to their own contracts.

## Roll Back The Executable

Restore `daem.previous` without network access:

```sh
sh daem-install.sh --rollback
```

The previous executable must pass `version --json`, including when it is a
source build. `--rollback` cannot be combined with `--version`.

This does not roll back manifests, lockfiles, statefiles, recovery journals,
or host mutations. If the previous executable rejects data written by the
newer version, stop and reinstall the newer verified release. Do not delete
or hand-edit managed state to force a downgrade. `daem recover` repairs
interrupted apply operations; it is not a binary or schema downgrade command.

## Release Mutability

`v0.1.0` remains mutable. Published tags/assets may change or disappear; an
exact version and co-published checksum do not prove historical immutability.
Latest is discovery, not artifact identity.

## Diagnostics

If metadata or downloads fail, check connectivity and GitHub API rate limits,
then retry. If latest discovery fails, `--version` selects an exact release
without that discovery step; it still needs the selected release's metadata
and assets.

Start an installation report with `daem version --json` and platform details
from `daem doctor --all-targets`. For workspace problems, use
[Troubleshooting](troubleshooting.md). Review local paths and machine-specific
information before sharing diagnostics.

## Build From Source

Source builds are intended for contributors and unreleased development
testing. They are not substitutes for native release-lane evidence:

```sh
git clone https://github.com/isty2e/daem.git
cd daem
go install ./cmd/daem
```

Use a current security patch of Go 1.25 or later. A source build normally
reports a development or pseudo-version rather than an official release tag.
