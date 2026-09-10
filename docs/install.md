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

Platform and runtime checks run before network access or destination changes.
On macOS, a translated x86-64 shell selects the Apple-silicon binary only when
macOS reports `sysctl.proc_translated=1`; Intel Macs remain unsupported. The
binary also enforces its own runtime-floor decision for supported workflows.

Latest-release discovery resolves once to an exact stable tag. The installer
resolves that tag to a commit, reads its committer timestamp, and reads the Go
toolchain from that commit's `go.mod`. It then downloads the exact tag/target
archive and checksum sidecar. No release values need to be copied from this
page, including when installing `v0.1.0`.

Before replacing an executable, the installer checks the exact checksum entry,
requires one regular executable named `daem` in the archive, and checks the
staged executable's version, commit, commit time, toolchain, native target,
Git VCS metadata, and clean source state. Failed download, validation, or
preparation leaves the current and previous executables unchanged. Cleanup of
invocation-owned temporary staging is attempted on exit and handled interruption.

These checks detect transfer errors and release-assembly mismatches. The
archive, checksum, and tag metadata share GitHub's release authority; they do
not prove publisher identity, provenance, or post-publication immutability.
Confirm that downloads and metadata come from the expected repository and
HTTPS endpoints.

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

The new executable and backup are prepared before either is published. Their
two replacements are not one atomic transaction: if final replacement fails
after backup publication, the current executable remains, but
`daem.previous` may have been refreshed to that same executable. Avoid running
multiple installers against the same directory at once.

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

Rollback stages a copy of the previous executable and requires its
`version --json` command to succeed before replacing the current executable.
A prior source build can also be restored. `--rollback` cannot be combined with `--version`.

This does not roll back manifests, lockfiles, statefiles, recovery journals,
or host mutations. If the previous executable rejects data written by the
newer version, stop and reinstall the newer verified release. Do not delete
or hand-edit managed state to force a downgrade. `daem recover` repairs
interrupted apply operations; it is not a binary or schema downgrade command.

## Release Mutability

Release `v0.1.0` was published while GitHub release immutability was disabled
and remains a mutable GitHub release. Daem does not guarantee that a published
tag or attached asset cannot be changed or deleted after publication. An exact
version and its co-published checksum are not historical immutability evidence.
Latest is a discovery aid, not an artifact identity.

## Diagnostics

If metadata or downloads fail, check connectivity and GitHub API rate limits,
then retry. If latest discovery fails, `--version` selects an exact release
without that discovery step; it still needs the selected release's metadata
and assets.

Record these facts when reporting an installation problem:

```sh
daem version --json
daem doctor --all-targets
```

For one selected workspace, also include:

```sh
daem status --json
daem list paths
```

Review output for local paths or other machine-specific data before sharing it.

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
