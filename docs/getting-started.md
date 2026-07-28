# Getting Started

This guide takes a project from no manifest to one reconciled Codex instruction
file. It also shows the alternate import path and interrupted-operation
recovery. Target-specific MCP, hook, extension, skill-group, and advanced source
configuration belongs in the [Manifest Reference](manifest.md).

## 1. Install

Install the checksum-verified `v0.1.0` artifact for the current supported
platform by following [Install, Upgrade, And Roll Back](install.md). Return
here after these commands report the expected release identity and help:

```bash
daem version
daem --help
```

`daem version` reports the executable's embedded module version, full source
revision, clean/modified/unknown source state, Go toolchain, and build target.
The authoritative platform rows and native evidence requirements are in
[Platform Support](platforms.md).

## 2. Start A Project

Change to the project you want to manage. The following creates a standalone
example project:

```bash
mkdir -p ~/daem-example
cd ~/daem-example
```

Create `./daem.toml`:

```bash
daem init --dry-run
daem init
```

`init` writes by default. It never creates a lockfile, statefile, cache,
recovery journal, or host file. Use `--manifest <path>` to select another
destination.

To start from existing host configuration instead, use import and skip the
`init` command:

```bash
daem import --target codex --dry-run --diff
daem import --target codex
```

Import also writes by default. Repeat `--target` for more hosts; comma lists are
not accepted. Use `--merge` only when importing into an existing selected
manifest. Import writes desired-state files but does not lock or manage the
live files it observed.

## 3. Add One Resource

Create one instruction source:

```bash
mkdir -p instructions
printf '%s\n' '# Project instructions' 'Use concise, direct answers.' > instructions/project.md
```

Preview and then author the instruction:

```bash
daem add instruction project ./instructions/project.md --target codex --dry-run
daem add instruction project ./instructions/project.md --target codex
```

The write updates `daem.toml` and `daem.lock.toml` together. It does not touch
`AGENTS.md` or any other host file. Use `--diff` with `--dry-run` for the exact
manifest delta, `--verbose` for bounded causal evidence, or `--json` for one
schema-versioned automation result.

## 4. Lock Manual Changes

Skip this step when the preceding `add` write succeeded: authoring already
refreshed the lockfile. Run it after hand-editing `daem.toml` or after import:

```bash
daem outdated --check
daem lock --dry-run
daem lock
```

`outdated` is read-only. `lock` writes by default and always uses
`daem.lock.toml` beside the selected manifest. Default output reports changed
identities; `lock --dry-run --verbose` also shows unchanged identities and
bounded resolution evidence.

## 5. Inspect And Apply

Inspect convergence and the exact pending effects:

```bash
daem status
daem apply --dry-run --diff
```

If the plan is acceptable, reconcile non-interactively:

```bash
daem apply --yes
daem status --check
```

Bare `apply` is also available when stdin, stdout, and stderr are all terminals:
it writes the selected effects completely to stdout, then asks once on stderr
before execution. Redirected output, piped or closed input, and other
non-interactive invocations require `--yes`.

If an existing `AGENTS.md` exactly matches the rendered desired content, review
and register that ownership explicitly instead of overwriting it:

```bash
daem apply --manage-existing --dry-run
daem apply --manage-existing --yes
```

`--manage-existing` does not import content. It records exact matching output as
managed, which gives later reconciliation deletion authority for that output.

The same flow can manage a supported plugin or package that is already installed
outside daem. First declare it with `daem add extension` or edit the manifest
and run `daem lock`, then inspect `daem status`. Continue only when status
reports `carrier adoption available` and the dry-run says it would record the
exact source, target, and scope:

```bash
daem apply --manage-existing --dry-run
daem apply --manage-existing --yes
daem status --check
```

Carrier adoption invokes no host install command, but the new claim grants the
bounded future removal authority shown by dry-run. A reported lifecycle blocker
or source-inexact relation is not adoptable.

## 6. Diagnose A Failure

`doctor` checks passive local prerequisites and never launches host CLIs,
package managers, MCP servers, credential helpers, or network probes:

```bash
daem doctor
daem doctor --target codex --verbose
```

Use `--json` for automation. Warnings do not fail doctor; errors do.

## 7. Recover An Interrupted Apply

When apply leaves an active recovery journal, inspect it first:

```bash
daem recover --dry-run
```

Then either confirm interactively with bare `daem recover`, or execute
non-interactively:

```bash
daem recover --yes
```

Recovery revalidates the active operation before writing. It is interrupted
operation cleanup, not historical restore. `recover --yes --json` emits one
schema-versioned execution result when automation needs the final status.

## Next References

- [CLI Reference](cli.md): exact grammar, output layers, JSON, streams, and exit
  behavior.
- [Manifest Reference](manifest.md): advanced fields and target-specific
  resource configuration.
- [Feature Support](features.md): current target and resource support.
- [Host Integration Contract](host-integrations.md): exact host operations and
  safety limits.
- [Concepts](concepts.md): manifest, lock, state ownership, and recovery model.
