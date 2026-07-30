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

The admitted Darwin target has a macOS 26 runtime floor. Earlier macOS releases
are outside the support contract because their directory rename semantics can
reject write-disabled trees before daem's atomic publication or logical-removal
visibility point. The target-level `darwin/arm64` admission identity does not
claim support for an older Darwin runtime.

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

The platform-gated command families `add`, `apply`, `import`, `init`, `lock`,
`outdated`, `recover`, and `remove` reject a not-admitted platform before path
resolution, manifest or cache access, confirmation, host writes, or delegated
command execution. `outdated` remains read-only with respect to desired and host
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
