# Platform support

For agent and resource coverage, see [Feature Support](features.md). This
page helps diagnose operating-system and filesystem restrictions.

## Current Matrix

Daem runs on Apple silicon with macOS 26 or newer, and on `linux/amd64`.
See [platform admission and release verification](../openspec/specs/platform-and-distribution/spec.md#requirement-separate-platform-admission-from-buildability)
for the distinction between a supported target, a compile-only target, and an
unverified target. Use [Install](install.md) to select a release executable.

### Linux Recovery

Finish pending recovery before rebooting, remounting an affected filesystem,
or changing kernels. If a journal survives from another boot, follow
[Recovery Journal From An Earlier Boot](troubleshooting.md#recovery-journal-from-an-earlier-boot).
See [Linux journal provenance](../openspec/specs/host-mutation/spec.md#requirement-bind-linux-journal-work-to-boot-and-mount-provenance)
for required evidence and the legacy kernel fallback.

### Artifact Paths

Use the actual stored spelling of names selected inside a source directory or
archive. The leading absolute path is resolved by the native filesystem.
[Artifact path admission](../openspec/specs/platform-and-distribution/spec.md#requirement-qualify-artifact-paths-for-exact-source-operations)
explains the identity evidence daem needs to read and verify an artifact.

### macOS Runtime Floor

Older macOS releases can refuse a directory rename when the directory has no
write bit. If `doctor` reports an unsupported macOS version, upgrade macOS or
use a supported machine.

### macOS Metadata During Writes

If replacing a file fails on an ACL or file flag, check the
[metadata-preservation rules](../openspec/specs/host-mutation/spec.md#requirement-preserve-supported-metadata-when-changing-file-contents-or-access)
before retrying. File replacement and
[permission-only recovery](troubleshooting.md#permission-only-recovery)
do not preserve the same attributes: recovery changes access on the existing
file.

### Linux Metadata During Writes

If replacement cannot establish the original SELinux context, check whether
the invoking user can set that context. Review the
[Linux metadata rules](../openspec/specs/host-mutation/spec.md#requirement-preserve-supported-metadata-when-changing-file-contents-or-access)
before changing policy or retrying. A dry-run success does not test these
filesystem writes.

## Unsupported Builds

Use `daem version` to identify the build and `daem doctor` for the platform
finding and any independent path errors. See the
[`doctor` diagnostic boundary](../openspec/specs/platform-and-distribution/spec.md#requirement-preserve-the-unsupported-platform-diagnostic-boundary).
`--dry-run` does not bypass the
[platform gate](../openspec/specs/platform-and-distribution/spec.md#requirement-separate-platform-admission-from-buildability).

## Path Descriptions

On NFSv3, keep to a single client and writer against a manifest or destination.
If a rename reports an indeterminate result, keep the candidate artifacts
and follow [NFS troubleshooting](troubleshooting.md#nfs-backed-home-or-workspace).
The [supported NFSv3 scope](../openspec/specs/host-mutation/spec.md#requirement-preserve-the-supported-nfsv3-single-client-boundary)
does not include exclusion of other clients or guarantees about a server
outage. For Darwin case and normalization rules, see
[native namespace revalidation](../openspec/specs/host-mutation/spec.md#requirement-revalidate-native-namespace-and-mutation-identity).
