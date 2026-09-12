# Platform Support

This page owns operating-system and architecture support. Agent/resource
coverage is separate: see [Feature Support](features.md).

## Current Matrix

| Operating system | Architecture | Product support | Verification lane |
| --- | --- | --- | --- |
| macOS 26 or newer (`darwin`) | `arm64` | admitted | native required |
| Linux | `amd64` | admitted | native required |
| macOS (`darwin`) | `amd64` | not admitted | compile only |
| Linux | `arm64` | not admitted | compile only |
| Linux | `386` | not admitted | compile only |
| Windows | `amd64` | not admitted | compile only |
| Every other target | any | not admitted | unverified |

An admitted row is a supported design target, not a test result. Release claims
require that row's native tests, CLI smoke and artifact-reproducibility lane in
the release run. This does not cover every filesystem, distribution, kernel,
host CLI or machine configuration. Compile-only builds detect portability
failures; they do not establish native behavior or promote support.

### Linux Recovery

Journal-bearing mutation binds mount identity to the canonical boot UUID from
verified procfs `kernel/random/boot_id`. Linux uses `STATX_MNT_ID_UNIQUE` when
available. Older kernels, including 5.15, use a separately tagged
`STATX_MNT_ID` witness. This fallback supports recovery within the same boot
without unmounting or remounting the affected filesystems; it does not guarantee
rejection of a reused legacy mount ID after remount.

Missing mount or boot evidence still blocks journal-bearing mutation, including
state-only journals. Provenance is validated again before journal-covered host
writes. Existing unique-ID journal tokens retain their meaning; recovery refuses
rather than converts a token from a different mount-identity scheme. Finish
pending recovery before reboot, remount, or a kernel transition.

An active journal from an earlier boot is refused before recovery effects.
The boot identity is recovery provenance, not part of a manifest, lockfile or
ordinary operation fingerprint; reboot alone does not invalidate a clean new
invocation. Operation-local rooted checks need not establish durable recovery
provenance. On admitted rows, platform-scoped stable-storage publication
preserves durable evidence across an OS crash or power loss, subject to the
filesystem caveats below; it does not make post-reboot recovery executable.

### Artifact Paths

On admitted Darwin/Linux targets, artifact reads, listings, hashes and copies
require stable root/ancestor object and mount identity through completion.
Darwin requires nonzero birth time or generation for artifact views. Linux
requires `STATX_MNT_ID` and either `STATX_BTIME` or an opaque file handle from
`name_to_handle_at` on each component. Handle comparison does not require the
privilege to reopen by handle. Missing both incarnation mechanisms fails the
affected operation rather than falling back to inode or pathname alone.
Read-only locators and process-local witnesses grant no mutation, lease,
durable-comparison or durable recovery authority.

Absolute artifact-root components use native filesystem lookup: a case variant
may resolve on a case-insensitive filesystem, while an absent spelling remains
absent on a case-sensitive one. Nested relative selections require exact stored
spelling and stable full identity, including change time, mode and size.
Ordinary views resolve parent symlinks; no-follow views reject every symlink
component, but do not impose case-sensitive lookup on absolute roots.

Universal exact-input-spelling rejection for absolute artifact roots is outside
this contract: a root is an input locator, not a required inventory-entry name.
Adding that refusal requires a separate compatibility decision. The distinct
[deferred Darwin mutation-root policy](../ARCHITECTURE.md#platform-maintenance-decisions)
does not weaken the artifact-view rule.

### macOS Runtime Floor

macOS 26 is required because earlier releases can reject write-disabled
directory renames before atomic publication or logical removal. Daem checks
`/usr/bin/sw_vers --productVersion`; a version below `26.0`, malformed output,
command failure or timeout blocks platform-gated commands before workspace,
source, storage or host effects. `doctor` retains the diagnostic exceptions below.

## Unsupported Builds

| Command | Behavior on a not-admitted platform |
| --- | --- |
| `--help`, `help <command>`, command-specific help, `version`, `--version` | Available; version uses embedded build facts only. |
| `doctor` | Nonzero with running `GOOS/GOARCH`, runtime/failure where applicable, required floor, verification class, admitted targets and next step. Resolves target/manifest selection to report path errors as well. |
| `add`, `apply`, `import`, `init`, `lock`, `outdated`, `recover`, `refresh`, `remove`, `unmanage` | Refused before path resolution, manifest/cache access, confirmation, host/delegated effects or durable metadata publication. Dry-run uses the same gate. |
| `list`, `status`, explicit `probe` | Not a partial unsupported-platform mode; required storage, project-root, process or host capabilities still fail closed. |

After successful path resolution, `doctor` retains the platform finding and
runs only checks whose meaning is unchanged. Capability-bound checks report
`unsupported` or `skipped`, never `ok`; path-resolution failure reports both
findings without inventing remaining results. On not-admitted platforms it
does not run Git, search PATH for MCP executables, or invoke durable file-set
or recovery-inventory adapters. Bounded host-config grammar checks remain
available. Storage failure cannot erase the platform finding, and diagnostic
findings never authorize an apply. `outdated` is read-only for desired/host state
but still requires supported path and source-cache behavior.

## Path Descriptions

Linux amd64 NFSv3 supports ordinary unprivileged single-client `lock`, `apply`,
and same-boot interruption/recovery, including on kernel 5.15. Run one writer
at a time against a manifest or destination, on trusted user-controlled paths.
Multiple Git sources may share a fresh cache within one command; internal
workers coordinate shared ancestor publication within the process.
Where NFS lacks atomic no-replace rename, daem checks destination absence and
uses ordinary rename. Atomic exclusion of a concurrent external writer is
outside this NFS contract; native no-replace publication remains in use where
supported. Namespace revalidation remains a useful check, not a promise to
exclude hostile interference.

An NFS rename error can follow a completed server-side rename. Daem reports an
indeterminate outcome and retains candidate staging/recovery artifacts rather
than treating the operation as uncommitted and cleaning them automatically.
Recovery may require manual analysis; automatic rollback of every uncertain
result is not guaranteed. Multi-node exclusion and server-outage, reconnect,
or power-loss durability guarantees are outside this support envelope, including
journal retirement and residue cleanup. These are support boundaries, not
promised future capabilities.

Metadata preservation covers mode, ownership, and supported metadata exposed
through the client. Unsupported NFS file-flag ioctls do not block ordinary
operations; other inspection failures still do. Server-only ACLs or other
metadata invisible to the client are not inspected or reproduced.

Metadata-based revalidation compares filesystem-reported identities and
attributes, not a universal revision counter. Rapid in-place changes can share
one timestamp; unlink/recreation can reuse an inode before timestamps advance.
These limits apply to local filesystems too. Keep observed inputs and namespaces
stable during a command; revalidation detects observable drift but does not
promise to detect every competing write or inode-reuse cycle.

On Linux, cleanup of private files and directories with restrictive modes uses
retained descriptors and verified procfs rather than requiring newer flagged
`fchmodat` support. Missing descriptor/procfs evidence still leaves an error and
retained residue rather than permitting unchecked pathname chmod.

Darwin mutation, lease and durable-comparison authority follows each parent
namespace's case behavior, including mixed mounts; a missing suffix inherits
the deepest existing parent's behavior. Unavailable or contradictory evidence
is an error, not a case-insensitive fallback. Entry authority keeps the final
symlink itself; referent authority follows it.

Daem uses OS-reported stored spelling, not user-space APFS/HFS+ Unicode
normalization. An absent normalization-sensitive destination provides only
provisional comparison/exclusion evidence, not exact path authority. After
creation it must be observed afresh in the same namespace, depth and filesystem
semantics. Recovery refuses root replacement or a different descendant mount
before effects.
