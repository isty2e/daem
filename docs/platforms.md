# Platform Support

This page is the public authority for operating-system and architecture support
in `daem`. Agent and resource coverage is a separate axis documented in
[Feature Support](features.md).

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

Product support and verification are different facts. An admitted row is a
target the product is designed to run on. `native required` means a release may
claim that support only after the repository's native test, CLI smoke, and
artifact-reproducibility lane passes for the row in the release run. A declared
workflow or another row's result is not that evidence. This does not claim that
every filesystem, distribution, kernel, host CLI version, or machine
configuration has been exercised.

Linux rooted-path authority has two levels. Operation-local checks use the
ordinary mount identity retained with the selected root. A journal-bearing
mutation additionally requires `STATX_MNT_ID_UNIQUE` and a canonical boot UUID
from the verified procfs `kernel/random/boot_id` entry. This evidence is stored
only as recovery provenance; it is not part of the manifest, lockfile, or
ordinary operation fingerprint. Every journal records the selected manifest
root provenance, including a state-only journal with no host entries. A clean
invocation therefore is not rejected merely because the machine rebooted. An
active journal is different: recovery after a reboot fails closed before
effects because the persisted mount authority belonged to the earlier boot. If
the running Linux kernel cannot provide the unique mount identity, daem rejects
the durable-provenance preflight before provider-prerequisite state publication
or delegated provider installation. Final journal capture validates that
provenance again before its covered host mutations rather than weakening
durable recovery authority.

The admitted Darwin target has a macOS 26 runtime floor. Earlier macOS releases
are outside the support contract because their directory rename semantics can
reject write-disabled trees before daem's atomic publication or logical-removal
visibility point. The target-level `darwin/arm64` admission identity does not
claim support for an older Darwin runtime.

Daem reads `/usr/bin/sw_vers --productVersion` through a bounded, timed
observation and requires a canonical product version at or above `26.0`. For a
platform-gated command, a lower version, malformed output, command failure, or
timeout fails closed before workspace, source, storage, or host effects.
`doctor` performs the same observation but retains its diagnostic path
resolution exception; after path resolution it reports the running target,
observed runtime or failure reason, required floor, verification lane, and next
step, then continues with independent remaining checks and named
`unsupported`/`skipped` coverage instead of treating the rest of doctor as one
atomic cut. Target admission and runtime observation remain separate facts.

`compile only` means the source is cross-built to detect portability failures.
It does not establish native execution, durable filesystem behavior, host
integration, or product support. A successful local build on an unlisted target
does not promote that target.

## Unsupported Builds

Cross-built binaries keep help, executable identity, and diagnostics available.
`daem --help`, `daem help <command>`, command-specific help, `daem version`, and
`daem --version` do not require an admitted platform. Version reads embedded
build facts only; it does not turn a compile-only build into a supported
product. `daem doctor` reports the exact running `GOOS/GOARCH`, its verification
class, and the admitted targets; it exits nonzero on a not-admitted platform.
Doctor validates target selection and resolves the selected manifest path so
that path errors remain actionable. After successful path resolution on a
not-admitted platform, it keeps the platform error, runs only checks whose
success meaning is unchanged, and emits named `unsupported` or `skipped`
results for capability-bound remaining checks. It does not invoke durable
file-set or recovery inventory adapters, so a storage abort cannot erase the
platform finding. If path resolution itself fails, the platform and path
findings are both reported and remaining checks are not invented. This
diagnostic exception grants no storage or mutation capability.

The platform-gated command families `add`, `apply`, `import`, `init`, `lock`,
`outdated`, `recover`, `refresh`, `remove`, and `unmanage` reject a
not-admitted platform before path resolution, manifest or cache access,
confirmation, host writes, delegated command execution, or durable metadata
publication. `outdated` remains read-only with respect to desired and host
state, but it consumes the same path and source-cache semantics. Dry-run forms
use the same gate because planning depends on the same platform contracts.

`list`, `status`, and explicit `probe` behavior are not a partial unsupported-
platform product mode. Their existing lower-level adapters continue to fail
closed when a required storage, project-root, process, or host guarantee is not
available.

## Path Descriptions

Some path-resolution code can describe roots for a not-admitted platform, such
as Windows user configuration directories. That allows deterministic parsing,
cross-building, and diagnostics. It does not make the platform supported and
does not authorize durable mutation there.

Platform admission does not weaken filesystem-specific caveats. In particular,
network filesystems such as NFS may not provide the same crash-durability and
cross-process exclusion guarantees as a tested local filesystem even on an
admitted OS/architecture row. The same caveat applies to journal-retirement
control publication, residue cleanup, and control-to-GC finalization.

On Darwin, path authority follows the backing filesystem rather than a single
macOS-wide rule. Daem obtains stored spelling for existing components and asks
each parent directory namespace whether names are case-sensitive. Mixed mount
paths are evaluated component by component; a missing suffix inherits the
deepest existing directory's case behavior. An unavailable or contradictory
capability is an error, not a case-insensitive fallback. Directory-entry
authority keeps the final symlink itself, while referent authority follows it.

This does not emulate APFS or HFS+ Unicode normalization in user space or infer
that two spellings are equivalent. Existing entries use the stored spelling
reported by the operating system. For an absent, normalization-sensitive
Darwin destination, daem records the selected spelling and its exact existing
parent namespace as provisional comparison and exclusion evidence. That record
does not grant exact path authority. After creation, daem accepts only a fresh
observation inside the same namespace, at the same depth, with the same
filesystem semantics. Recovery also fails before effects if the captured root
directory is replaced or the destination crosses onto a different descendant
mount.
