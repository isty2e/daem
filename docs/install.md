# Install, Upgrade, And Roll Back

Daem publishes binaries on [GitHub Releases](https://github.com/isty2e/daem/releases)
for macOS 26 or newer on Apple silicon and Linux on x86-64. Windows and other
targets are not supported releases. See [Platform Support](platforms.md) for
the full support policy.

## Install

If daem is already installed, follow [Upgrade](#upgrade). For a new
installation, download the installer and run it:

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
sh daem-install.sh --version v0.7.2
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

Before replacing an executable, the installer checks:

- The downloaded archive matches its exact SHA-256 checksum entry.
- The archive has exactly one entry: a regular executable named `daem`.
- The staged executable's [`version --json`](cli.md#version) matches the selected
  release tag and its resolved commit, that commit's committer timestamp and
  `go.mod` toolchain, and the native OS/architecture. It must report
  `vcs = "git"` and `source_state = "clean"`.

Download, validation or preparation failure preserves both current and previous
executables. The installer attempts to clean its temporary staging on exit and
handled interruption.

These checks detect transfer/assembly errors, not publisher provenance or
immutability: archive, checksum and metadata share GitHub's authority. Verify
the expected repository and HTTPS endpoints.

## Upgrade

Before upgrading from v0.7.2, run `daem status` with v0.7.2 for each manifest
that uses the same daem data directory. By default, this includes your user
manifest and any project manifests you use. Select each one with `--manifest`.
If an `apply` was interrupted, run `daem recover --dry-run` for that manifest,
review the plan, and finish recovery there with `daem recover` before switching
versions. A write in another project can upgrade the shared ownership registry
to a format v0.7.2 cannot read, even if the projects manage different files.
For journals from earlier releases, follow the
[version-specific recovery precautions](troubleshooting.md#pre-10-durable-authority-schemas).

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

### Lockfile Schema 7

This version writes schema 7 for all lockfiles, including manifests with
`agent_config`. `status`, `apply`, and `outdated` can read a valid schema 6
lockfile without rewriting it or changing its pinned sources. If your lockfile
is schema 3–5, or daem reports that the schema 6 contents cannot be used as a
locked plan, preview and regenerate it explicitly:

```sh
daem lock --dry-run
daem lock
daem status
daem apply --dry-run --diff
```

Relocking resolves the current manifest sources again. A readable schema 6
lock works as a comparison baseline for `outdated`; `lock --dry-run` previews
the new resolution without comparing it to the older file. Review the new lock
and the apply preview before applying. Relocking does not change host
configuration. Older daem versions cannot read the regenerated lockfile. If a
team shares that file, coordinate the daem upgrade before committing the new
lockfile. Rolling back only the executable does not restore lockfile
compatibility.

Other unsupported schemas remain refusals; these commands do not grant a generic
lockfile migration or downgrade path.

If you automate `lock` or `outdated`, update your decoder for
[lock comparison JSON v6](cli.md#outdated). It reports `previous_status` and
uses `null` for comparisons it could not perform.

## Roll Back The Executable

Restore `daem.previous` without network access:

```sh
sh daem-install.sh --rollback
```

The previous executable must pass `version --json`, including when it is a
source build. `--rollback` cannot be combined with `--version`.

Rollback restores only the executable. Manifests, lockfiles, statefiles, recovery
journals, shared ownership records, and host changes remain in place. If you
find a pending v0.7.2 journal after switching binaries and have made no writes
with the new version in any workspace sharing the data directory, switch back
to v0.7.2 to finish recovery, then upgrade again. If `daem.previous` is
v0.7.2, use `--rollback`. Otherwise, install v0.7.2 in a separate directory
with `--version v0.7.2` and `--bin-dir`, and run that copy for recovery.

If the previous executable rejects data written by the newer version, use the
newer verified release for that data. If the old version refuses the shared
registry **and** the new version refuses the journal, stop and preserve the
journal, its backups, the statefile and the shared registry for diagnosis.
Do not delete or hand-edit them to force a downgrade.

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

Use the Go version and toolchain in [`go.mod`](../go.mod). A source build normally
reports a development or pseudo-version rather than an official release tag.
