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

Journal-bearing mutation requires `STATX_MNT_ID_UNIQUE` and a canonical boot
UUID from verified procfs `kernel/random/boot_id`. Without that evidence, daem
refuses before provider-prerequisite state publication or delegated provider
installation, and validates provenance again before journal-covered host writes.
This applies even to state-only journals with no host entries.

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
Darwin requires nonzero birth time or generation for artifact views; Linux
requires `STATX_MNT_ID` and `STATX_BTIME` on each component. Missing identity
fails the affected operation rather than falling back to inode or pathname
alone. Read-only witnesses grant no mutation or durable recovery authority.

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
available. Storage failure cannot erase the platform finding, and diagnostics
grant no mutation capability. `outdated` is read-only for desired/host state but
still requires supported path and source-cache behavior.

## Path Descriptions

Platform admission does not weaken filesystem-specific caveats. In particular,
network filesystems such as NFS may not provide the same crash-durability and
cross-process exclusion guarantees as a tested local filesystem even on an
admitted OS/architecture row. The same caveat applies to journal-retirement
control publication, residue cleanup, and control-to-GC finalization.

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
