# Platform and distribution

## Purpose

Define where daem commands and release assets are supported. The [platform model](../../../internal/platformsupport/model.go), [runtime check](../../../internal/platformsupport/runtime.go), [release workflow](../../../.github/workflows/release-artifact.yml), and [installer](../../../install.sh) own their concrete rules. The [install guide](../../../docs/install.md) provides instructions and version-specific recovery advice.

## Requirements

### Requirement: Separate platform admission from buildability

Daem SHALL admit Apple silicon macOS 26 or later and `linux/amd64` as product targets. It does not infer support from an adapter, a successful compile or a diagnostic path. The product matrix distinguishes supported rows, compile-only rows, and unverified targets. Release claims require the admitted row's native checks, CLI smoke, and artifact reproducibility evidence. Gated macOS commands require an observed product version of 26.0 or later; an unsupported version, malformed reading, failed observation or timeout does not qualify.

Platform-gated commands refuse before workspace lookup, resolution, confirmation, or effects, including dry-run. This includes `migrate` recovery/cleanup; an internal no-op branch does not add a Windows command. Help and offline version are available. An operation not covered by the ingress gate still needs its own admitted capabilities; it is not a fallback mode for unsupported platforms.

#### Scenario: Try to preview metadata recovery on Windows

- **WHEN** `migrate metadata --recover --dry-run` runs on a compile-only Windows build
- **THEN** it reports platform refusal before examining that workspace's recovery work

### Requirement: Preserve the unsupported-platform diagnostic boundary

`doctor` SHALL report the unsupported platform and keep independent path errors visible. It may report checks whose meaning is unchanged; checks requiring unavailable capabilities are skipped or unsupported rather than successful. It does not run Git, perform the MCP executable search, or run durable file-set/recovery inventory on an unadmitted platform. A platform finding is not erased by another diagnostic failure.

#### Scenario: Both platform and workspace selection are invalid

- **WHEN** an unadmitted invocation of doctor cannot resolve the selected manifest path
- **THEN** it reports both reasons and does not present unavailable checks as completed

### Requirement: Qualify artifact paths for exact source operations

On admitted Unix targets, an artifact view SHALL retain stable root/ancestor identity through reads, listings, hashing and copying. Use the required object incarnation and mount evidence for the platform. Darwin artifact reads require birth time or generation; Linux uses a mount ID and birth time or an opaque file handle. Missing the required evidence is not permission to infer identity from inode or path alone. Process-local read evidence does not confer mutation or durable recovery authority.

An absolute root uses native name lookup and may resolve a case variant on a case-insensitive filesystem. Nested paths require exact stored spelling and unchanged identity. Ordinary locator traversal may resolve parent symlinks; no-follow views reject symlink components. This is the artifact boundary, not a blanket Darwin mutation-root rule. Journal continuity and filesystem mutation constraints are described in [Platform Support](../../../docs/platforms.md).

#### Scenario: A nested name differs only in case

- **WHEN** an artifact query uses a different stored spelling for a nested path
- **THEN** native case-insensitive lookup at the absolute root does not waive the exact nested-name check

### Requirement: Install a verified executable without changing managed data

The bundled installer for daem's published GitHub releases SHALL check platform before downloading or changing the destination. A translated x86-64 macOS shell qualifies for the Apple silicon artifact only with Rosetta evidence. The selected release defaults to latest stable and the destination to `~/.local/bin`; explicit version and directory options override discovery. The installer requires normal Unix tools, not Go or sudo, and does not edit shell profiles. This contract is for the published release installer, not for third-party distribution or source builds.

Before replacement, verify the matching SHA-256 entry, one executable `daem` archive member, and staged build identity against the release: tag, resolved revision, committer time, toolchain, native target, Git VCS and clean source. Admission and validation failures preserve current and previous executables; cleanup of temporary files is attempted on exit. These checks share the release provider's authority; neither a checksum nor a tag proves publisher provenance or immutability.

An upgrade retains a previous executable unless the installed bytes are unchanged. Backup and final replacement are not one transaction: after backup publication a final-write failure can leave current and previous identical. Offline rollback selects a valid previous executable (including a source build), conflicts with `--version`, and does not restore data written by a newer daem. Executable changes do not rewrite manifests, locks, settings, state, or journals.

#### Scenario: The archive checksum matches but the staged binary has the wrong revision

- **WHEN** installer metadata and `version --json` disagree
- **THEN** the installer refuses replacement even though the downloaded bytes match the checksum

#### Scenario: Roll back after a newer binary wrote a newer metadata format

- **WHEN** the prior executable is selected
- **THEN** the installer restores only that executable; format compatibility is evaluated by the invoked daem, not restored by the installer
