# CLI Reference

This document is the public command, flag, output, stream, and exit contract for
`daem`. Resource schema and target-specific fields belong in the
[Manifest Reference](manifest.md); current user-facing coverage belongs in
[Feature Support](features.md), while operating-system and architecture support
belongs in [Platform Support](platforms.md).

For a command sequence rather than a lookup, use [Getting Started](getting-started.md)
or [Use An Existing Environment](migration.md).

## Contents

- [Command lifecycle](#command-lifecycle)
- [Platform support](#platform-support)
- [Workspace selection and storage roots](#workspace-selection)
- [Target and scope selection](#shared-selection)
- [Preview, write, and confirmation modes](#execution-modes)
- [Output flags, flag inventory, and JSON versions](#presentation-flags)
- Commands: [`version`](#version), [`init`](#init), [`import`](#import),
  [`add`](#add), [`remove`](#remove), [`unmanage extension`](#unmanage-extension),
  [`lock`](#lock), [`outdated`](#outdated), [`list`](#list), [`status`](#status),
  [`apply`](#apply), [`recover`](#recover), [`migrate state`](#migrate-state), [`doctor`](#doctor),
  [`probe mcp-server`](#probe-mcp-server), [`refresh extension`](#refresh-extension)
- [Authoring JSON](#authoring-json), [progress](#progress),
  [streams and exit codes](#streams-and-exit-codes),
  [deferred command names](#deferred-command-names)

## Command Lifecycle

| Stage | Command | Responsibility |
| --- | --- | --- |
| Start | `init` | Create a starter manifest. |
| Start | `import` | Build desired state from existing host configuration. |
| Author | `add` | Add or extend one desired resource and refresh its lock result. |
| Author | `remove` | Narrow or remove one desired resource and refresh its lock result. |
| Author | `unmanage` | Release exact daem authority for one extension while preserving host state. |
| Resolve | `lock` | Resolve the manifest into the exact adjacent lockfile. |
| Resolve | `outdated` | Check whether locked source identities can advance. |
| Inspect | `list` | Enumerate declarations or managed-output ownership. |
| Inspect | `status` | Compare desired, locked, managed, and live state. |
| Inspect | `version` | Show the running executable's embedded build identity. |
| Diagnose | `doctor` | Check passive environment prerequisites. |
| Diagnose | `probe` | Run one explicitly authorized active runtime check. |
| Operate | `refresh` | Refresh one exact declared and locked host extension relation. |
| Reconcile | `apply` | Reconcile the locked environment. |
| Reconcile | `recover` | Recover one operation or finish its retained journal cleanup. |
| Management authority | `migrate state` | Move the default user manifest's legacy state authority or recover that migration. |

`help`, `-h`, `--help`, and `--version` are meta surfaces. Use
`daem help <command>` and `daem help <group> <resource>` for progressively
scoped help. `daem --version` is the human alias for `daem version`; it rejects
additional arguments and there is no `-v` alias.

## Platform Support

The exact supported platforms and their verification requirements are
authoritative in [Platform Support](platforms.md).

On an unsupported platform, help and version remain available and `doctor`
reports the platform error in human or JSON form. Platform-gated commands fail
before path resolution or effects, including dry-run. Human refusals use stderr;
known leaves with JSON selected use the common CLI failure family.
`doctor --json` provides the diagnostic report. Doctor resolves its selected
manifest and reports platform and path-resolution errors together. Remaining
capability-bound checks are `unsupported` or `skipped`, not `ok`, and storage
failure does not erase the platform finding. See [Platform
Support](platforms.md) for the authoritative platform rules.

## Workspace Selection

`--manifest <path>` is the only public workspace-path selector. Relative paths
resolve from the current working directory. Valid filesystem whitespace and
control-bearing bytes are preserved as path identity; NUL is not a valid path
or argv byte. Human output escapes unusual bytes without changing the path used
by filesystem operations.

When omitted, manifest-backed commands check only:

1. `./daem.toml` in the current directory;
2. the OS user manifest when no cwd manifest exists.

On supported platforms the user manifest path is
`${XDG_CONFIG_HOME:-~/.config}/daem/daem.toml`. Parent directories are never
searched. Path-resolution code may describe a Windows root for diagnostics and
cross-builds, but Windows is not a supported product platform.

`init` and non-merge `import` are creation operations. Without an explicit
manifest they create `./daem.toml` rather than falling back to a user manifest.
`import --merge` uses normal existing-workspace selection.

The lockfile is always `daem.lock.toml` beside the selected manifest. State,
cache, recovery, and project-installation paths derive from that same selected
workspace. There is no independent public lockfile selector.

Implicit user-manifest selection does not establish a project root: project-scoped
resources are rejected with guidance to select a project manifest or declare
global scope. Explicit and cwd selection retain their existing placement policy;
sharing storage does not change placement admission.

### Daem Storage Roots

On supported Unix platforms, daem observes four XDG variables. An applicable,
non-empty value must be an absolute path. The `daem` directory is appended to
the configured root:

| Variable | Default daem path | When it applies | Owned content |
| --- | --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config/daem` | Default user manifest | User manifest and adjacent lockfile |
| `XDG_STATE_HOME` | `~/.local/state/daem` | Default user manifest, in every selection mode | Statefile and recovery journals |
| `XDG_CACHE_HOME` | `~/.cache/daem` | Default user manifest, in every selection mode | Resolved source cache |
| `XDG_DATA_HOME` | `~/.local/share/daem` | Every selected workspace | Shared output-ownership and carrier-claim registries |

Selecting the default user manifest through `--manifest`, cwd discovery, or
fallback uses the same state, recovery and cache roots under unchanged HOME/XDG
settings. Existing manifest-local management must first be transferred with
[`migrate state`](#migrate-state). Directory aliases identify the same manifest
entry; a separate hardlink or final-file symlink entry is not a new spelling of
that entry.

An explicit or cwd-selected manifest other than the default user manifest keeps its state and recovery data
under `<manifest-root>/.daem` and its source cache under
`<manifest-root>/.daem/cache`; changing `XDG_STATE_HOME` or `XDG_CACHE_HOME`
does not relocate those project-local paths. `XDG_DATA_HOME` remains shared
across project and user workspaces because its registries coordinate global
ownership and carrier claims.

User-config lookup is limited to `daem.toml` and case-folded spellings that could
name it. When the default user address cannot be formed because
`XDG_CONFIG_HOME` is relative or the required HOME fallback is unavailable,
explicit, cwd and creation selection keep project-local storage; user fallback
still fails.

For a default-named manifest and a valid config address, identity-observation
errors still refuse: an inaccessible directory may hide an alias of the selected
manifest. Silently choosing project storage in that case is unsupported because
it could split one user's management authority.

Changing an applicable root selects a different storage namespace. In
particular, changing `XDG_CONFIG_HOME` selects a different implicit user
manifest and lockfile, while changing `XDG_STATE_HOME` can make prior
user-workspace managed state and recovery journals unavailable to the selected
operation. Changing `XDG_DATA_HOME` selects different shared ownership and
carrier-claim records; the root itself does not grant authority.
`XDG_CACHE_HOME` changes reuse only; cache contents never grant ownership or
current-state authority.

These variables select daem-owned storage. Host-specific variables such as
`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, `OPENCODE_CONFIG`,
`OPENCODE_CONFIG_DIR`, and `PI_CODING_AGENT_DIR` select host-owned observation
or placement surfaces. They do not relocate daem's manifest, state, cache, or
data roots.

## Shared Selection

Accepted targets are `codex`, `claude-code`, `opencode`, `pi`, and
`antigravity-cli`. A target flag consumes one token and may be repeated:

```bash
daem status --target codex --target claude-code
```

Comma lists are rejected. Duplicate target, scope, and skill-group member
values collapse after validation. Ordered MCP `--arg` values are the exception:
order and duplicates are semantic and remain unchanged.

An execution selector never broadens desired state. It filters resources whose
normalized effective target set already includes the selected target.

## Execution Modes

Commands use effect-tiered modes:

| Effect class | Commands | Bare invocation | `--dry-run` | `--yes` |
| --- | --- | --- | --- | --- |
| Read-only | `list`, `outdated`, `status`, `doctor`, `version` | query | rejected | rejected |
| Desired/derived state | `init`, `import`, `add`, `remove`, `unmanage extension`, `lock` | write | preview | rejected |
| Host/runtime effect | `apply`, `recover`, `probe mcp-server`, `refresh extension` | three-stream TTY confirmation | preview | non-interactive execution |
| Management authority | `migrate state` | three-stream TTY confirmation | preview | non-interactive execution |

An empty host/runtime plan does not prompt; an already completed state migration
also needs no new prompt. Skipping a prompt does not waive command admission:
a bare state-migration repeat still requires terminal stdin/stdout/stderr;
non-interactive repeats use `--dry-run` or `--yes`. JSON cannot share an
interactive prompt, so host/runtime and state-migration JSON require `--dry-run`
or `--yes`.

Interactive authorization uses three distinct process streams: stdin accepts
the answer, stdout carries the stable effect disclosure, and stderr carries the
prompt and cancellation diagnostic. All three streams must be terminals. The
complete stdout disclosure must also have been written successfully before the
stderr prompt appears. Redirecting either output, piping or closing stdin, or
omitting any stream makes the invocation non-interactive and therefore requires
`--yes` for effects; daem never copies the plan to stderr or auto-confirms as a
fallback. The one explicit exception is non-interactive
`refresh extension --yes --json`: its complete authorization document is
written to stderr before execution so stdout can remain one final JSON result.
EOF and every answer other than `y` or `yes` decline. Context cancellation
interrupts a blocked terminal read and cannot authorize effects.

## Presentation Flags

- Default human output is concise and always retains blockers, selected
  effects, failures, destructive implications, uncertainty, and the nearest
  corrective action.
- Relation-action rows describe relation decisions, not every planned host
  effect. A no-op row means the relation itself needs no change. An Adapter
  provider package can still need reinstalling, reported separately as
  `provider_prerequisite: install_required`. During `apply`, that prerequisite
  can replay the host install route.
  Observation-only rows include the reason; neither kind implies removal or
  verified runtime readiness.
- On POSIX platforms, concrete next commands are rendered from an argument
  vector so dynamic paths remain one shell argument. Control-bearing arguments
  use a terminal-safe POSIX reconstruction and still round-trip to the exact
  argv; arguments containing NUL cannot be represented and produce no concrete
  command. Commands containing placeholders such as `<target>` are templates,
  not concrete argv claims.
- Concrete manifest-scoped hints preserve the requested selection. Implicit
  requests may omit `--manifest` when the hinted command selects the same cwd or
  user manifest. Explicit requests may elide only equivalent cwd selection,
  never user-default fallback. Creation hints use creation rules. If selection
  cannot be confirmed, the hint keeps the requested selector. Run it from the
  same directory and environment; if discovery inputs change, recheck the
  selected workspace. Rerun reminders are not full command replays: keep your
  original operands and selection flags.
- Human-readable dynamic paths and errors escape backslashes, controls, format
  characters, and invalid UTF-8 bytes. Structural line breaks remain owned by
  the presenter, so dynamic values cannot forge another diagnostic or hint.
- Human error details apply the shared redaction policy. Each dynamic detail is
  limited to 4,096 Unicode characters, with a truncation marker when needed.
  Details too large for bounded inspection are withheld instead of printing a
  partial value. Syntax locations and repair guidance remain outside that
  detail budget. Common CLI failures use these same owned diagnostic views;
  ordinary result payloads and declaration-size admission limits are unchanged.
- `--verbose` adds bounded typed causal, provenance, path, and identity
  evidence. Error types without a bounded evidence projection are reported as
  omitted instead of invoking an arbitrary error formatter. Incomplete error
  evidence is withheld rather than printing a potentially unsafe prefix.
  Verbosity never changes selection, planning, authority, or mutation.
- `--json` selects one schema-versioned result document on stdout, including
  [common CLI failures](#common-cli-failures) before an ordinary result is ready.
  Failed delivery can leave stdout empty or partial; daem does not repair or
  replace that document.
- `--json` and `--verbose` are mutually exclusive.
- `--diff` is accepted by `import`, every add/remove leaf, and `apply`. It
  requires `--dry-run` and is mutually exclusive with `--json`.
- `--verbose --diff` is allowed.
- `--check` exists only on `outdated` and `status`. It preserves normal output
  but returns non-zero when that command's clean predicate is false.

**Credential detection — NON-GOAL.** Redaction covers explicitly supplied secret
values and supported credential syntax; it does not guarantee detection of
unknown secret values or arbitrary encodings in free-form text. A complete
observation or a scanner no-match is not proof that text contains no secrets.
This does not relax command-specific non-disclosure guarantees for known secret
inputs. Rejected S3 URIs and MCP declaration transports report the source field
and rejection reason without echoing the rejected value. S3 diagnostics also
omit underlying URL parser errors.

### Input Error Guidance

TOML syntax errors include a one-based line and column when available. This is
where parsing stopped, which may be the end of the document rather than the
opening bracket or quote.

Unknown-key diagnostics include the start of the authored key expression when
available, using one-based lines and rune columns, and link to the
[manifest reference](manifest.md). Positions describe the manifest content being
validated.

Declaration read, validation and placement failures can include input-repair
guidance. Environment failures while resolving a local carrier source or host
load identity are not classified as invalid manifest or lockfile input; use
`--verbose` for bounded failure evidence.

Missing local sources include path-correction advice. Git instruction, skill,
and skill-group sources require an explicit branch, tag, or commit through `--ref`; local paths
do not need a remote reference. When a SKILL error offers `compat_repair = true`,
set it in the manifest to opt into mechanical repair. Other reported source
issues may need manual correction. Advice does not perform repairs or retry the
failed command.

### Command Flag Inventory

This table is the exhaustive long-form flag inventory for leaf commands.
Operands, `help`/`-h`/`--help`, and the root `--version` alias are not command
flags.

| Command | Accepted flags |
| --- | --- |
| `add extension` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `add hook` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--matcher`, `--scope`, `--target`, `--timeout`, `--verbose` |
| `add instruction` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--path`, `--ref`, `--scope`, `--target`, `--verbose` |
| `add mcp-server` | `--arg`, `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `add skill` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--name`, `--path`, `--ref`, `--scope`, `--target`, `--verbose` |
| `add skill-group` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--member`, `--path`, `--ref`, `--scope`, `--target`, `--verbose` |
| `apply` | `--diff`, `--dry-run`, `--json`, `--manage-existing`, `--manifest`, `--target`, `--verbose`, `--yes` |
| `doctor` | `--all-targets`, `--json`, `--manifest`, `--target`, `--verbose` |
| `import` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--merge`, `--scope`, `--source-dir`, `--target`, `--verbose` |
| `init` | `--dry-run`, `--force`, `--json`, `--manifest`, `--verbose` |
| `list outputs` | `--json`, `--manifest`, `--target`, `--verbose` |
| `list paths` | `--json`, `--manifest`, `--target`, `--verbose` |
| `list resources` | `--json`, `--manifest`, `--target`, `--verbose` |
| `lock` | `--dry-run`, `--json`, `--manifest`, `--verbose` |
| `outdated` | `--check`, `--json`, `--manifest`, `--verbose` |
| `probe mcp-server` | `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--timeout`, `--verbose`, `--yes` |
| `recover` | `--dry-run`, `--json`, `--legacy-user-state`, `--manifest`, `--verbose`, `--yes` |
| `migrate state` | `--dry-run`, `--json`, `--manifest`, `--recover`, `--yes` |
| `refresh extension` | `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--timeout`, `--verbose`, `--yes` |
| `remove extension` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `remove hook` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `remove instruction` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `remove mcp-server` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `remove skill` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `status` | `--check`, `--json`, `--manifest`, `--target`, `--verbose` |
| `unmanage extension` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `version` | `--json` |

### JSON Schema Contract

`schema_version` belongs to one command envelope. Versions from different rows
are unrelated and must not be compared as a product-wide sequence:

| Command surface | Envelope owner | Current version |
| --- | --- | ---: |
| `version` | Executable identity | `1` |
| `init` | Manifest initialization | `1` |
| `add`, `remove`, `import`, `unmanage extension` | Manifest authoring | `7` |
| `lock`, `outdated` | Lock comparison | `4` |
| `list resources` | Resource inventory | `4` |
| `list outputs` | Output inventory | `5` |
| `list paths` | Agent location inventory | `2` |
| `status`, `apply --dry-run` | Reconciliation plan | `17` |
| confirmed `apply` | Apply result | `23` |
| `recover` | Recovery plan/result | `10` |
| `migrate state` | State-authority migration | `1` |
| `doctor` | Passive diagnostics | `2` |
| `probe mcp-server` | Runtime probe | `1` |
| `refresh extension` | Extension refresh | `4` |
| Known leaf failure before an ordinary result | Common CLI failure (`result_type: "cli_error"`) | `1` |

The resource-name cutover replaces the v0.5.x envelopes for manifest authoring
(`6`), resource inventory (`2`), output inventory (`4`), agent location inventory
(`1`), reconciliation plans (`14`), apply results (`21`) and recovery (`9`).
Public resource names and references now use the authoring vocabulary below.
Each affected family emits its new version even for empty results or resources whose spelling
is unchanged. Command input syntax and persisted formats are unchanged.

Resource inventory version `4` adds `hook_asset` declaration rows. Their
`targets` field is the canonical union of declared hook-reference targets,
including an empty string for unreferenced assets, rather than a target set
declared on the asset. Every resource inventory emits version `4`, including
empty results; consumers of version `3` must update before interpreting it.

Plan and apply results retain per-binding MCP `host_prerequisite` evidence,
optional dimension `detail`, the unsupported Pi backend-transition
classification, managed Pi Git pin-change actions and `pin_change` disclosure.

Plan version `17` and apply-result version `23` distinguish an unsupported MCP
effective observer from a supported observer whose evidence is unavailable.
Both use `state: "unobserved"`; `EFFECTIVE_OBSERVER_UNSUPPORTED` replaces the
previous `EFFECTIVE_STATE_UNOBSERVED` reason only for the unsupported case.
Consumers of the earlier versions must not interpret the new reason as an
observation failure or a claim that no shadowing definition exists.

Consumers must first distinguish `result_type: "cli_error"` from the expected
ordinary command envelope, inspect that family's `schema_version`, and reject
unsupported versions before interpreting its other fields. A consumer must not
infer compatibility from a version used by another family or command.

Every consumer-visible field addition, removal, rename, type or nullability
change, or semantic reinterpretation requires a version increment before
release. A same-version correction may only restore the documented shape and
meaning. Each daem binary emits one current version per envelope; it does not
negotiate or emit legacy variants. Until a command explicitly documents a
multi-version policy, consumers must update for a new version rather than
silently accepting it. Released version changes belong in release notes.

### Resource Names And Identifiers

Public resource classifications use the token accepted by the authoring CLI:
`instruction`, `skill`, `hook`, `mcp-server` and `extension`. Authoring a group
uses `skill-group`; inventory members remain `skill`. `hook_asset` retains its
TOML spelling because it has no direct add/remove command. Naming a resource
kind does not imply that every command supports it.

Public `resource_id` values and human resource references use `kind/name`, such
as `instruction/project` or `mcp-server/context7`. The name remains literal,
including any slash, colon, percent sign or words such as `instructions`.
For commands that support the resource kind, prefer the separate `resource.kind`
and `resource.name` fields, or `list resources`' `kind` and `key`, rather than
splitting a reference. A `hook_asset` kind/key identifies its declaration, not
an add/remove operand.

Canonical `entity_id` and `subject_id` values, including structured subject
namespaces and keys, are opaque identities, not command operands. They retain
their existing grammar and may contain `instructions` or `mcp_server`. Other
`kind` fields may describe an action, subject role, source or artifact rather
than a resource. TOML keys, embedded manifest blocks, diagnostic quotations of
source/schema syntax, and summary field names also retain their own spelling.

When migrating from v0.5.x, accept the new family version and update comparisons
against public resource names/references. Do not rename manifest keys, rewrite
stored IDs or change valid command inputs. The common CLI failure family remains
version `1`; its primary message is diagnostic prose, not a stable reason code.

### Common CLI Failures

When a known leaf command selects JSON but fails before choosing an ordinary
result, it returns the `cli_error` family instead of an empty stdout plus human
error text. For example, `daem status --unknown --json` returns exit `2` with:

```json
{
  "schema_version": 1,
  "result_type": "cli_error",
  "command": "status",
  "error": {
    "kind": "usage",
    "message": "flag provided but not defined: -unknown"
  },
  "guidance": [
    {"kind": "command", "argv": ["daem", "help", "status"]}
  ]
}
```

`command` is the complete leaf name in the [flag inventory](#command-flag-inventory),
without `daem` or resource operands: for example, `add skill`, `probe mcp-server`
or `migrate state`. This does not change the command identifiers or fields in
ordinary result families. `error.kind` is `usage` for CLI usage failures (exit
`2`) and `operation` for operation failures (exit `1`). Classification belongs
to the command, not the parsing or workflow stage where an error was found.
`error.message` is the owning diagnostic's primary cause, not a machine-stable
reason code.

`guidance` is always an array, possibly empty. Each entry has one form:

| `kind` | Fields | Meaning |
| --- | --- | --- |
| `note` | nonempty `message` | Context or bounded diagnostic evidence |
| `instruction` | nonempty `message` | Manual work, a template or a syntax example |
| `command` | complete `argv`, optional `message` | An argument vector beginning with `daem`; the message describes its purpose |

Guidance preserves the relevant basic-mode repair view, not every verbose path
or the full ordinary result model. Import failures retain every admitted
action-required skip, compact benign rows and explicitly report an exhausted
skip budget. Recover's no-journal planning failure keeps its path-neutral scope
and continuing-fence advice. A command remains manual advice if its required
context cannot be represented or disclosed safely. An argv is not authorization
to execute it, proof of prerequisites, or a promise that retry is safe. Run it
with the same directory/environment and preserve any stated selection context.

Existing valid invocation grammar is unchanged. For a malformed invocation,
format recovery can recognize a later `--json` after an unknown flag or unbound
operand. Declared values stay data: `--manifest --json` or MCP `--arg --json`
does not select JSON. An unconsumed `--` ends option recognition. For commands
using boolean flag syntax, the last valid JSON binding wins; an invalid binding
does not undo an earlier valid one. `version` retains its exact `--json` form.
Help retains its existing precedence and human output. `probe mcp-server` and
`refresh extension` report a missing name/id when an option occupies that slot;
options before the required name/id do not become a new successful syntax.

There is no root `--json` prefix. Unknown commands, unknown subjects and missing
command routes remain human usage errors. Startup failures and OS signals are
not common CLI results. Once an ordinary family is chosen, its failed or blocked
result remains in that family; projection, disclosure and output faults do not
produce a replacement `cli_error`, even when no stdout bytes were delivered.
The common family makes no claim about effects: a command can fail after writes
but before returning its ordinary result. Existing signal exit identities and
[JSON stream exceptions](#json-stream-exceptions) still apply.

## `version`

`daem version` is an offline executable-identity query. It does not select a
manifest, inspect the environment, access the network, invoke a host CLI, or
require platform admission. `daem --version` emits the same one-line human
form. `daem version` accepts only `--json`, `--help`, or `-h`; the alias accepts
no arguments.

Human output is one stable line containing the embedded module version, full
revision, source state, Go toolchain, and `GOOS/GOARCH`. Missing build facts are
printed as `unknown`; a development or pseudo-version is preserved rather than
promoted to an official release version.

Version JSON schema version is `1` and has exactly these fields:

```text
schema_version version revision revision_time source_state vcs
go_version goos goarch
```

`revision_time` is source revision provenance, not a wall-clock build time.
Embedded facts identify the executable but do not prove that CI passed, that an
artifact came from a trusted publisher, or that a checksum or signature was
verified.

## `init`

```bash
daem init [--manifest <path>] [--force] [--dry-run] [--json|--verbose]
```

`init` creates a minimal manifest and writes by default. `--force` authorizes
replacement of an existing regular file after its directory-entry identity is
revalidated;
it does not require the old content to be a valid manifest. It never creates a
lockfile, statefile, cache, recovery journal, or host file.

Default human output names the action, destination, and nearest next command.
`--verbose` adds the exact starter content. Init JSON schema version is `1` and
contains `command`, `mode`, `action`, `manifest_path`, and `content`.

## `import`

```bash
daem import --target <target> [--target <target> ...] [--manifest <path>]
  [--scope <scope> ...] [--source-dir <path>] [--merge]
  [--dry-run] [--diff] [--json|--verbose]
```

At least one target is required. Import observes supported live instruction,
skill, hook, standalone MCP, and source-exact extension forms and writes a new
or explicitly merged manifest plus copied source material. It never writes the
lockfile, statefile, managed host outputs, carrier claims, cache, or recovery
journal.

`--source-dir` defaults to `<manifest-basename>.d` beside the selected manifest.
A relative source directory must stay within the manifest directory and may not
be `.daem`, contain the manifest, or overlap daem-managed metadata.

Without `--merge`, the selected manifest must not exist. With `--merge`, it
must exist and decode as a valid current manifest before live resources are
scanned. Conflicts fail before mutation. Write-mode merge conflicts still
render admitted skip rows, including every actionable skip and its next action,
before the dry-run conflict hint. JSON and `--dry-run` human output already
include those rows in the completed plan. Unsupported or lossy live forms are
reported as skipped instead of being imported approximately.

Imported instruction files must be stable regular files no larger than 128
MiB. Import follows the selected path's parent directories but does not follow
a final symlink. Hook JSON files must be stable regular files no larger than 4
MiB and contain one UTF-8 JSON value with unique object keys and at most 64
levels of nesting. Standalone MCP config files must also be stable regular
files no larger than the selected MCP codec's 4 MiB document limit. These byte
rules are the same ones used when daem observes, applies, and recovers the
corresponding managed document. Files that violate an import boundary are
reported with a stable skip reason; no partial resource is produced from the
rejected file.

Skill import may resolve a top-level directory symlink, records its absolute
route, and rejects nested symlinks; root `SKILL.md` must be regular. Each tree
admits at most 100,000 entries, 64 descendant-directory levels and 4 GiB of
regular-file bytes in planning and staging. The containing skills root does not
consume a tree depth level. Source-identity observation and final freshness
checks each admit at most 400,000 entries and 16 GiB; neither widens per-tree
limits. Distinct duplicate/conflict routes count toward observation even when
later skipped; listed names still count on a nested-symlink exit. Shared routes
are hashed once.

A planning pass retains at most 100,000 immediate names across new distinct
resolved roots, 32 MiB of aggregate name bytes and 4,096 bytes per name. Reuse
does not recharge those names but revalidates exact inventory and live-root
bindings on reuse, after revision capture and before publication. Root/alias
changes abort, not return a partial preview. Every route contributing to a
merged import remains freshness evidence; the representative route supplies
bytes. Entry, kind, executable-mode or other identity changes during copying
prevent publication. Files are streamed; `SKILL.md` remains limited to 1 MiB and
extension inventories retain their host observer's bounded evidence through
publication.

**Skill exclusion freshness — NON-GOAL.** A missing or non-directory discovery
root, or a child excluded by eligibility checks such as a missing regular
`SKILL.md` or a nested symlink, records the classification observed during that
pass. Import does not promise that these excluded inputs remain absent or
ineligible until preview completion or manifest publication, nor that discovery
is an atomic snapshot of all configured roots. Rerun import to reconsider
inputs that became eligible. Such exclusions grant no publication authority.

This boundary does not relax freshness for imported sources, contributing
routes, captured root inventories and alias bindings, or evidence retained for
merge decisions, including merge no-ops. Existing limits, cancellation,
operational-error handling and skip diagnostics also remain unchanged. The
maintainer has selected this boundary rather than universal exclusion
freshness; reopen it only if the owning product contract changes or an excluded
fact is used as authority for publication or a merge decision.

Import refuses preview and write modes while an interrupted apply journal is
active, before scanning live agent files. Run `daem recover --dry-run` first.

Default output shows target/scope totals, resources, destination, next commands
and skip counts: `action_required` needs a live-source or authoring decision,
`unsupported` is outside this version's support, and `informational` is expected
discovery/deduplication noise. Every admitted actionable skip has a next action.
One planning pass admits at most 4,096 skip rows and 256 KiB of aggregate
dynamic diagnostics; a `detail` over 4,096 bytes becomes a whole-value digest
and byte count. Ordinary producer failure/cancellation discards that producer's
rows. Aggregate exhaustion instead aborts before plan/write. Human mode prints
the bounded retained rows once with a diagnostic-budget marker on stderr. JSON
returns a common CLI failure with the basic retained view and omission advice,
not an ordinary import plan.

Unsupported/informational skips are compacted by target/reason. `--verbose`
shows every admitted per-path skip in success and no-resource failure, plus
clean scan/resource/merge rows for successful plans. Write-mode merge conflicts
print the same skips before the dry-run hint. JSON retains all admitted typed
rows with target, scope, category and optional stable action hint. Successful
import writes point to lock preview, then `apply --manage-existing --dry-run`
after lock: import itself never registers outputs or recommends `--yes`.

Extension import covers Codex global, Claude Code project/global, OpenCode
project/global, and Pi project/global exact source rows. Generated ids are
deterministic, existing exact declarations retain their ids, and merge
preserves existing manifest bytes and relative order. Conflicting ids,
duplicate host load identities, or contradictory native and manifest order
fail before mutation. Antigravity installed state does not retain enough source
provenance to reconstruct a declaration, so those rows are reported as
`source_provenance_unrecoverable` skips.

## `add`

```bash
daem add extension <id> <source> [options]
daem add instruction <name> <source> [options]
daem add hook <name> <event> <command> [--matcher <matcher>]
  [--timeout <duration>] [options]
daem add mcp-server <name> <command> [--arg <value> ...] [options]
daem add skill <source> [--path <repo-path>] [--ref <git-ref>]
  [--name <installed-name>] [options]
daem add skill-group <source-root> --member <name>
  [--member <name> ...] [--path <repo-path>] [--ref <git-ref>] [options]
```

Every leaf accepts `--manifest`, repeated `--target`, applicable `--scope`,
`--dry-run`, `--diff`, `--json`, and `--verbose`. Add writes by default. It
builds and validates a prospective manifest and lockfile, then commits both
together. On failure, neither file advances.

Repeating an already-satisfied add succeeds without an extra flag. It reports
`change: unchanged`, not a duplicate warning. The declaration identity and
requested settings must match, and its effective targets must cover the
request. Scope defaults keep their resource-specific rules; omitted targets
use manifest targets. An unchanged result retains all declaration bytes,
including manifest-only settings. A different source, identity, or conflicting requested setting
remains an error, not an overwrite request.
Adding targets to a matching explicit target list remains a real change.
Broadening inherited targets still requires a manual manifest edit.

An unchanged declaration still goes through full prospective lock validation.
Write mode may rewrite equal manifest bytes and create or refresh the lockfile;
`unchanged` is not a promise of no I/O, no source resolution, or an already-fresh
lock. Unavailable sources and other validation or transaction failures still
fail the command. Dry-run does not write either file or persistent source cache.

Common authoring is intentionally curated:

| Resource | CLI owns | Manifest owns |
| --- | --- | --- |
| Extension | id, one opaque carrier-native source, target, scope | carrier spelling and lifecycle/contribution policy |
| Instruction | name, local/Git source, Git file path/ref, target, scope | S3 source and target-specific placement |
| Hook | name, event, command, matcher, timeout, target, scope | status text, target overrides, assets, non-command handlers |
| MCP server | name, portable command, ordered args, target, scope | exact absolute command path, env refs, remote transport, auth, cwd, tool policy |
| Skill | source, Git path/ref, installed name, target, scope | resource id, install mode, repair and source policy |
| Skill group | source root, exact members, Git path/ref, target, scope | selectors, install mode, repair and per-member policy |

Git instructions require an explicit `--ref` and a repository file path:

```bash
daem add instruction project acme/guidance --path instructions/AGENTS.md --ref main
```

`acme/guidance/instructions/AGENTS.md --ref main` is equivalent shorthand; do
not combine an embedded path with `--path`. Without Git options, ordinary paths
remain local file sources. Directory and link sources are rejected during lock
preflight. S3 instruction sources remain manifest-only.

Hook `<command>` is one opaque shell-command string. Hook timeout is a positive
duration such as `30s` or `2m` and must be exactly representable as whole
seconds. MCP `<command>` is one portable executable token, not a shell command;
`--arg` preserves argv order and duplicates. Exact machine-local MCP
executables use the manifest-only `command = { path = "/absolute/path" }`
form; `daem import` also emits that form for supported host entries with an
absolute command.

For the first automatic `--target pi` choice without existing Pi bindings or an
explicit Adapter provider, add observes one bounded `pi --version`
result in disposable agent and working directories. Stable `>=1.0.2,<2.0.0`
evidence selects and records `backend = "native"` without a package. Missing,
malformed or incompatible evidence retains Adapter with a diagnostic;
cancellation is an error. An existing homogeneous Pi cohort determines new
servers' backend across names and scopes without another version query. Explicit
provider intent also takes precedence. Lock regeneration and apply never reselect
the backend; apply still qualifies the recorded Native choice before publication.

For Adapter, add ensures that one compatible explicit `pi-mcp-adapter` package
is declared at the selected scope. The default provider
selector is `npm:pi-mcp-adapter@^2.13.0`; the admitted profile accepts stable
`2.x` versions from `2.13.0` onward and rejects unbounded selectors,
prereleases, and major `3`. Project add may reuse one unambiguous explicit
global provider and reports that sharing consequence. This authoring step still
changes only the manifest and lockfile; apply owns delegated Pi installation
and config projection.

Extension source is one opaque operand validated against the selected target's
supported carrier row. Registry and host-source spelling are not separate CLI
ontologies. Global carrier mutation always requires explicit global scope.

Skill Git refs accept a strict branch, tag, or full 40/64-hex object id. Root
skills may omit `--path` or use `.`. Skill groups list exact direct children
with repeated `--member`; discovery selectors and regex/glob behavior remain
manifest-only. A named group's authoring identity is its exact member set,
independent of member order. Repeating that group can be unchanged or merge
explicit targets. Add does not split overlapping groups, regroup direct skill
declarations, or edit selector-backed membership; those edits remain manual.

Default output names the semantic resource change plus manifest and lock
outcomes, including an unchanged lockfile. It omits the full prospective TOML
and source internals. `--verbose` adds bounded normalized and lock evidence; `--diff` owns
the exact manifest delta. An empty delta is reported as
`manifest diff: unchanged` rather than a context-only diff.

## `remove`

```bash
daem remove extension <id> [options]
daem remove instruction <name> [options]
daem remove hook <name> [options]
daem remove mcp-server <name> [options]
daem remove skill <resource-key> [options]
```

Every leaf accepts the same shared authoring options as add. Remove writes by
default and transactionally refreshes the lockfile. Use keys from
`daem list resources`. A missing resource or a selection matching no declaration
is an error, including repeated removal. When the manifest supplies an exact
name under other selectors, a case variant, or a name one character edit away,
stderr includes up to three available-selection command hints. Hints use actual declaration keys and
scope/target values; they do not change or execute the removal request.

Omitted target and scope selectors remove one unambiguous whole resource.
Selectors narrow or disambiguate; they do not inherit add defaults. Removing
the last selected target removes the declaration. A partial target removal from
a multi-member skill group is rejected because its target set is shared; split
the group in the manifest first.

Remove changes desired and locked state only. Host file deletion, config
binding removal, and supported route effects are later apply work. It never
means unconditional carrier uninstall, package/cache cleanup, credential
deletion, trust reset, or contribution-level mutation.

Removing a Pi MCP row retains any explicit provider extension. A later apply
uses the stored Native or Adapter codec to remove only its config contribution,
without needing a working Pi binary for Native retirement. Remaining unowned
definitions are reported conservatively as fallback, not runtime activation.
Physical ownership and root-namespace checks still apply. Remove the provider
extension separately only when the package relation itself is also undesired.

For extension rows, removal expresses desired relation absence. Manual deletion
of the same row followed by `lock` produces the same later status/apply
semantics. If durable state proves that daem manages the exact relation, a
later supported apply plan may remove it through the disclosed host-native or
direct-config route. If that authority is absent, ambiguous, stale, or
unsupported, apply blocks without mutating visible host state.

Confirmed apply may remove a daem-managed selector-shaped Antigravity relation
only after fresh residual correlation and the last-daem-known-consumer check. It
invokes `agy plugin uninstall <plugin>`, never the marketplace selector, and
settles only when both the selected import row and plugin directory are freshly
absent. Partial or uncertain outcomes retain claim and pending state for retry;
already-absent state retires without invocation. See [host
integrations](host-integrations.md) for the remaining scope and non-claims.

Pi package rows currently admit this later apply step for project and explicit
global scope. Project removal invokes `pi remove <source> -l`; global removal
invokes `pi remove <source>`. Daem retires the exact claim only after fresh
settings-row absence plus scoped npm/Git artifact absence or unchanged local
source content. Partial removal retains recovery state for a later fresh
verification and does not authorize unrelated package, cache, trust, session,
or local-source deletion.

## `unmanage extension`

Use `unmanage extension` to release daem's exact management authority while
retaining the host relation and host-owned state:

```bash
daem unmanage extension <id> [--manifest <path>] [--target <target>]
  [--scope <scope>] [--dry-run] [--diff] [--json|--verbose]
```

Unmanage is a desired/derived-state write, not a host/runtime effect. It accepts
the same selection and presentation rules as `remove extension`, does not
accept `--yes`, and never invokes a host removal route. It removes the selected
declaration when present, its current lock entry, and the exact daem managed
claim or pending management fact while retaining host state. It may release
those retained facts after manual manifest omission. A subject with neither a
declaration nor a management fact is not found; ambiguous or stale identity
fails without partial writes.

The default human result must always include `host: retained` plus manifest,
lockfile, and management-state outcomes. Verbose output may add the exact
claim and route identities but may not imply current host usability.

Structured output uses the [manifest-authoring JSON family](#json-schema-contract);
its unmanage rows retain their existing shape:

- `command` and `operation` are `unmanage`;
- the single change has manifest `change_kind =
  "would_remove"|"removed"|"unchanged"` and management `status =
  "would_release"|"released"|"not_present"`;
- top-level `management` contains the same status plus exact `statefile` and
  shared `registry` path/status objects;
- top-level `host.state` is always `retained`; global rows additionally report
  `host.ambient_consumers = "unobservable"`.

Dry-run emits `would_release` when a claim exists. Write emits `released` only
after every affected manifest/lock/state/registry owner commits. `not_present`
is a successful claim no-op only when a selected declaration was removed in
the same transaction; if both declaration and claim are absent, no result
envelope is created and the command exits `1`.

`unmanage extension` refuses both dry-run and write before reading selected
metadata while apply recovery or incomplete journal cleanup remains. Run `daem
recover --dry-run` first when it can produce a plan; markerless residue and
published metadata-transaction markers remain afterward. If the state directory
cannot be inspected, restore access before recovery.

An interrupted `unmanage extension` write leaves a recoverable
metadata-transaction marker and other consumers fail closed while it remains.
Rerun the exact write to recover under the same authority set before
revalidation and commit. `daem recover` is reserved for apply-recovery journals
and does not consume this marker.

## Authoring JSON

Init uses schema `1`. Add, remove, import, and unmanage share the
[manifest-authoring JSON family](#json-schema-contract) with these common fields:

| Field | Meaning |
| --- | --- |
| `command`, `mode`, `operation` | operation identity and dry-run/write mode |
| `manifest_path`, optional `source_dir` | selected durable destinations |
| optional `lockfile` | adjacent lock path and would-write/written/unchanged status |
| `resource_count`, `change_count`, `changes` | addressed resources, change-row count, and typed change rows |
| add `unchanged` | optional array of already-satisfied `{kind, name}` resource identities |
| `has_errors`, optional `warnings` | result classification |
| unmanage `management`, `host` | exact management-state outcome and retained-host projection |
| import `summary`, `scans`, `skipped`, `merge_results` | exhaustive observation and merge rows |

For an already-satisfied add, `resource_count` is `1`, `change_count` is `0`,
`changes` is `[]`, and `unchanged` contains the addressed resource. It has no
prospective manifest block and does not generate a duplicate warning. Its
`lockfile.status` independently reports `would_write`, `written`, or
`unchanged`. If add must insert a missing Pi MCP provider, that insertion is
still a change even when the requested server is already declared. Changed
add/remove, import, and unmanage retain their row shapes; resource names and
references follow [Resource Names And Identifiers](#resource-names-and-identifiers).
Consumers must explicitly accept the current manifest-authoring version in the
[version table](#json-schema-contract).

Each import `skipped` row contains exact `target`, `scope`, `live_path`, and
stable `reason` code values plus one stable `category`. Optional `detail`
contains at most 4,096 bytes of non-authoritative diagnostic context and never
changes classification. `action_hint` is present only for `action_required`
rows and is a machine code rather than human next-action prose. Codex inline
Hook snapshot failures retain the same specific symlink, non-regular, size, or
changed-during-read reason used by standalone Hook snapshots. MCP canonical
invalidity and provider-lossy documents are actionable repair reasons; unknown
or untyped MCP projection codes become `mcp_projection_unclassified` and remain
actionable. Unknown future reason codes default to `action_required` so default
human output cannot silently compact them.

Imported extension changes use `resource.kind = "extension"` and include the
exact `carrier`, `target`, and `scope`. A source identity proven public by the
carrier grammar is emitted unchanged. A local or opaque source is replaced by
a deterministic `redacted:sha256:...` value and sets `source_redacted = true`.
Credential-bearing extension sources are rejected at manifest and import
ingress, so no output mode prints their values. Import summary rows include an
`extensions` count; scan rows identify their resource kind so extension
inventory evidence is not reported as a skill scan.

Projection-specific import merge rows include a canonical `subject_id` in
`kind/namespace/key` form. It distinguishes rows that share an aggregate
`resource_id`, such as project and global MCP projections with the same server
name. Aggregate-level merge rows omit `subject_id`.

Human next-command prose is deliberately absent from the current authoring
envelope. JSON CLI misuse or failure before an ordinary result uses the separate
[common CLI failure family](#common-cli-failures), including basic repair guidance.
An import conflict has a valid result envelope, so it emits JSON with
`has_errors: true` and exits `1`.

## `lock`

```bash
daem lock [--manifest <path>] [--dry-run] [--json|--verbose]
```

Lock parses and normalizes the whole manifest, resolves and validates lockable
sources, expands skill-group selectors, computes replayable skill repairs when
authorized, and writes `daem.lock.toml` beside the selected manifest. It writes
by default.

`--dry-run` follows the same resolution path but does not write the lockfile or
persistent cache. Temporary resolution data is removed before exit. The same
floating manifest can legitimately resolve to a different exact lock over time.

Default output reports subject counts plus every added, changed, or removed
lock entry under `added lock entries:`, `changed lock entries:`, or
`removed lock entries:`. Entries use public references such as
`resource="instruction/project"`, `resource="skill/review"`, and
`resource="mcp-server/context7"`. Each realized entry also includes its complete
`target` or `targets`, `scope`, and `placement`. Exact-source entries do not
invent a target; file-use entries include their scope. These fields depend on
the entry's kind, not which other entries happen to be listed, so separate
placements remain distinguishable even when their public reference matches.
Declaration IDs remain public even when a local or private source requires
redacting the structural subject or source details. Entries without an
established declaration identity retain a safely disclosed `subject=`.

Changed entries include `changed=` reasons in both default and verbose output.
When exact source identity exists before and after, the reasons distinguish
`source_id`, `resolved_ref`, `kind`, and `content_hash`, in that order; `kind`
means file or directory, not resource kind. Whole-facet additions/removals and
unclassified identity changes use `exact_supply`. Other changed facets, such
as `derivation`, remain listed. For example, a revision-only change can report
`changed=resolved_ref,derivation`, while a changed source hash can also include
`content_hash`. These are field names, not disclosed source values.

MCP dependency changes use `mcp_environment_sources` and
`mcp_provider_contribution`. If a locked contract differs but no named reason
is available, `locked_contract` replaces an empty reason list.

Each change list uses the same resource-first display order as status and apply
below. When extension order constraints exist, lock also reports class-relative
order counts and changed class ids without reordering their members. It omits
unchanged entries, source ids, hashes, resolved refs, full repair recipes, and
member details. `--verbose` adds unchanged entries, safely disclosed canonical
`subject` identities for lockfile correlation, bounded resolution evidence, and
before/after ordered members.

Change reasons describe locked input facts, not predicted output work. A
`content_hash` includes executable class, so it can change while normalized
instruction output remains current. Regenerating the lockfile removes entries
no longer represented by the manifest. It does not inspect or delete host
outputs; status and apply decide output currentness and work under
managed-state authority.

## `outdated`

```bash
daem outdated [--manifest <path>] [--check] [--json|--verbose]
```

Outdated runs the same lockable-source resolution and validation path as lock,
compares the candidate exact identities with the current lockfile, and writes
nothing persistent. Default current output is count-only; stale output lists
every added, changed, or removed identity and the lock next step. `--verbose`
adds checked current identities and bounded refs. `--check` exits `1` when any
lock identity would change. Changed entries use the same `changed=` reasons
as lock.

Lock and outdated JSON schema version is `4`. It includes command/mode,
manifest and derived lockfile paths, prior-lock presence, subject and order
constraint entry/change counts, `has_changes`, typed subject changes, and
ordered before/after extension members for each changed order class.
Carrier-derived subject names, source namespaces, relation keys, managed
instance keys, and host-load identities are emitted unchanged only when the
carrier grammar proves them public. Otherwise they use deterministic
`redacted:sha256:...` values and their adjacent `*_redacted` marker is true.
Managed-path realizations include `exact_permission_mode` when and only when
their permission policy is `exact`; the optional field representation preserves
an explicit mode `0`. Full safe source ids and hashes are automation evidence
and remain in JSON.

## `list`

```bash
daem list resources [--manifest <path>] [--target <target> ...]
  [--json|--verbose]
daem list outputs [--manifest <path>] [--target <target> ...]
  [--json|--verbose]
daem list paths [--manifest <path>] [--target <target> ...]
  [--json|--verbose]
```

Bare `list` is navigation only. `list resources` reads and normalizes the
manifest without requiring a lockfile or resolving source files. It prints every
selected kind, stable declaration key, install name, target set, and scope.
Basic rows also show known explicit group membership, such as
`group="skill_group[0]"`; ungrouped rows omit
that field. The index is a declaration-position marker, not a stable group ID
or command operand. Members remain `skill` resources and use their own remove
keys. Shared group membership can explain why a partial-target removal requires
[splitting the group first](#remove). Listing does not resolve source selectors
to discover more membership. `--verbose` adds source summaries and includes the
group field even when its value is `"-"`.

Hook assets appear once per declaration, after hooks and before MCP servers,
with `kind=hook_asset` and `install="-"`. Their `targets` is the full union of
declared targets of hooks that reference them, in the existing supported-target
order, not proof of supported execution or an installed output. Referenced
assets require overlap with a target filter, without narrowing the displayed
union. Unreferenced assets have `targets=""` and remain visible under every valid
target selection. Omitting the filter and explicitly selecting the same target
set produce the same inventory.

There is no `add hook_asset` or `remove hook_asset` command. Edit the
[asset declaration and its hook references](manifest.md#hook-assets), then
validate the change with `daem lock --dry-run` before writing the lockfile.

`list outputs` prints every selected managed output, relevant unmanaged live
destination, and ownership-blocked output. Blocked rows include the typed
reason and bounded owner/conflict detail so they agree with `status` without
being mislabeled as unmanaged. It is an ownership inventory, not a convergence
report; use status for the complete plan. It reads the statefile, ownership
claims, and selected output paths or config documents, but it does not refresh
sources or probe providers, delegated routes, runtime health, or extension
order. Aggregate rows identify the selected contribution and intentionally do
not report a hash for the whole shared config file. An unmanaged aggregate row
requires fresh codec evidence that the subject's contribution is physically
present; a projection-level block does not fabricate occupancy for absent or
ambiguous siblings. A target filter selects rows without truncating a shared
path row's complete canonical `targets` consumer set. List commands never
truncate rows.

`list paths` prints a static target -> scope -> resource tree. It includes
project and global locations even before the manifest declares resources:

- instruction and skill write, discovery, and runtime directories;
- hook config files, or an unsupported row when that target has no hook route;
- daem's private hook-asset store;
- MCP config files, or an unsupported row;
- delegated extension install, refresh, and remove routes.

Human tree headings are descriptive prose, such as `MCP servers` or `hook assets`.
JSON `resource` values use the [public kind tokens](#resource-names-and-identifiers)
instead; headings are not command operands.

Resource families select their destinations differently:

| Resource | What selects the destination |
| --- | --- |
| Instructions | The target default, or a supported target-specific `render_to`. |
| Skills and skill groups | The target default, or a supported target-specific `install_to`; the skill name is appended below that root. |
| Hooks | The target profile's fixed native config file. |
| Hook assets | Daem's fixed private same-scope asset store. |
| MCP servers | The target and scope select one fixed supported config file. |
| Extensions | The target, scope, and source select delegated host routes rather than a daem write path. |

There is no generic destination override. A resource-specific selector changes
only the dimension that resource owns; it cannot turn a discovery path,
runtime path, config file, private store, or delegated route into another kind
of destination.

`selected` marks the write path, config file, or delegated route chosen by the
manifest. `default` marks a target profile's default write path. Alternative
discovery and runtime directories explain where a host can find resources; they
do not grant daem permission to write there. An explicit skill `install_to`
selects an admitted alternative. A requested but unrecognized root is reported
as unsupported and never becomes path authority.

The inventory comes from daem's validated target catalogs plus manifest
selection facts. It does not inspect host files, execute host commands, or
report whether a path exists. Use `--verbose` for realization, catalog source,
selection source, and request details.

For skills, `doctor`, `status`, and `apply` (including dry-run) additionally
check the exact same-name child at other modeled discovery roots for the
selected target and scope. A retained-copy warning means that path exists and
the current command does not plan to migrate or remove it. The warning does not
claim which copy the host will load, does not grant ownership, and never
deletes or adopts the other copy.

The [version table](#json-schema-contract) lists the current JSON version for
each list family. In `list resources` JSON, extension source identities use
the same carrier disclosure rule as lock output and set `source_redacted` when
the value is replaced. `list outputs` JSON has separate `managed`, `unmanaged`,
and `blocked` arrays and counts. Rows retain their canonical `subject` and
complete `targets` consumer set while reporting the correlated resource identity
when one exists.
Newly classified unmanaged and blocked aggregate rows also carry
`content_path`, which identifies the selected contribution inside the shared
file, while `hash` remains empty because daem does not own the whole document.
Both commands include every selected row.

`list paths` JSON uses a flat `locations` array. Every
row contains `target`, `scope`, `resource`, `kind`, `realization`, `role`,
`selected`, `requested`, `default`, `selection_source`, and `source`. A row has
exactly one applicable payload: `path`, delegated `route` plus `operation`, or
unsupported `reason` plus optional `detail`. The JSON ordering matches the
human tree.

## `status`

```bash
daem status [--manifest <path>] [--target <target> ...] [--check]
  [--json|--verbose]
```

Status is read-only. It reports convergence across desired resources, exact
lock identity, managed-state ownership, current host observations, modeled
relation observations, and retained historical attempt diagnostics.

Read-only means no managed configuration or state mutation. For Native Pi MCP,
status may run the bounded `pi --version` observation; it does not execute
npm/pnpm/Bun location queries. See
[Pi Native And Adapter](host-integrations.md#pi-native-and-adapter).
Version observation does not request installation, updates or server startup.

Native qualification failures are per-binding `host_prerequisite = unqualified`
evidence with a closed reason and causal detail. Other targets' status remains
visible. They block `status --check` and publication, not plain status reporting.
Native bindings require current host qualification evidence; other MCP bindings
report `host_prerequisite = not_applicable`, separately from any Adapter
`provider_prerequisite`. These dimensions do not certify runtime readiness.

Unqualified host prerequisites use these reasons:

| Reason | Meaning |
| --- | --- |
| `HOST_VERSION_UNQUALIFIED` | A stable Pi version in the admitted Native range was not observed. |
| `HOST_SETTINGS_UNOBSERVED` | The applicable settings or source metadata cannot be interpreted within the bounded observation contract. |
| `HOST_BUILTIN_DISABLED` | The applicable builtin MCP selection is disabled. |
| `HOST_BUILTIN_UNOBSERVED` | Builtin selection depends on unsupported static selector syntax. |
| `HOST_ADAPTER_CONFIGURED` | Known Adapter intent remains configured, regardless of its package filters. |

Default output groups each resource's selected placement or shared consumer
set with its output/configuration transition and associated MCP observations.
Ordinary plan blocks and successful output-result rows are ordered by public
`kind/name` reference, then target, then scope. Target order follows the supported
target catalog; shared outputs use their earliest consumer in that order without
changing the displayed consumer set. Equal keys retain their relative order.
Rows with only a `subject=` fallback follow named resources in their existing
order. This is display order, not execution order: JSON arrays, extension
precedence sequences, and attempt history keep their existing order.

The header counts **output entries**: one managed-path decision, including a
shared output, or one aggregate subject. Two MCP servers in one configuration
document therefore count as two entries, not two files. `current`, `pending`,
and `blocked` partition these configuration decisions; pending excludes blocked
entries. Delegated command plans are separate and may remain when every
configuration entry is current.

Every planned mutation, blocker, drift, unsupported/ambiguous class,
binding-removal result, and uncertain host-route class remains visible. Routine
no-op rows are omitted unless an associated observation, command plan, or blocked
configuration document needs their context. State/content paths, internal
reason fields, hashes, route ids, and exhaustive evidence arrays remain
verbose-only.
`--verbose` expands every ordinary row with bounded causal fields; MCP rows
retain their canonical subject alongside the public resource reference.
Routine MCP facts include adopted ownership, unshadowed configuration, a
matching successful command attempt, and no recorded matching attempt. These
are omitted from basic output, as is structurally unsupported effective
observation. Genuine unobserved effective state, conflicts, stale/failed/blocked
attempts and selected command plans remain visible. Omission does not claim
runtime readiness; `--verbose` and JSON retain the individual facts.

Extension plans keep separate relation-action, carrier-adoption, carrier-absence,
and extension-order sections. Relation and carrier rows use the declared
`resource="extension/<id>"` established by the carrier contract, not the host
package name. A private source does not hide that declaration ID; source and
structural-subject disclosure remain independent. Rows without an established
declaration identity retain a safely disclosed `subject=`;
verbose rows retain the canonical subject alongside the resource reference.
A nonempty section always reports its total;
default output adds `(N unchanged)` instead of repeating no-op relation rows,
already-current carrier claims, retained carrier relations, and exact order
rows. Admitted bounded same-subject no-ops follow the same rule as exact-source
no-ops. Other decisions, including blockers, observe-only rows, pending claims,
removal verification, and conditional or normalization order, remain visible.
`--verbose` and JSON retain every individual fact. The same presentation applies
to apply previews and preflight refusals.

The table below describes exit codes for valid status results. Command failures
still return `1` before or while emitting their applicable result contract.

| Invocation | Exit `0` | Exit `1` |
| --- | --- | --- |
| `status` | any valid report | never because of reported state |
| `status --check` | lockfile present, no pending or blocked output action, exact extension order, and no blocked relation, adoption, or extension-removal action | lockfile missing; pending or blocked output action, including an unqualified MCP host; non-exact extension order; or blocked relation, adoption, or extension-removal action |

Diagnostic grades, including `error`, do not by themselves affect the `--check`
exit code or `check_clean`. The `diagnostics` array carries the reported grades;
error-grade diagnostics contribute to `has_errors`.

Selected missing extension relations and observe-only relation rows do not make
`--check` fail. A blocked removal of a managed extension relation does fail. A
normalize, conditional, or blocked extension-order row is also non-clean. JSON
and human modes use the same exit predicate.

Both `status --json` and `status --check --json` include the required boolean
`check_clean`. It is true exactly when the selected observed state satisfies
the check predicate above, regardless of whether `--check` was requested.
`check_clean` and `has_errors` are independent in both directions: pending changes
can make `check_clean` false while `has_errors` remains false; an otherwise clean
report with error-grade diagnostics can have both fields true and still pass
`--check`. Ordinary status still exits `0` for a valid report.

`check_clean` is not runtime readiness, permission to apply, or a prediction of
process success when usage, operation, signal or output-delivery failures occur.
Failures before an ordinary report follow the [common error contract](#common-cli-failures)
instead of fabricating an assessment. `apply --dry-run` omits this field: its
planning context, including `--manage-existing`, is not a status observation.
Both commands emit the current reconciliation-plan version in the
[version table](#json-schema-contract); other result families are unchanged.

## `apply`

```bash
daem apply [--manifest <path>] [--target <target> ...]
  [--manage-existing] [--dry-run|--yes] [--diff] [--json|--verbose]
```

`apply --dry-run` reports every selected mutation, blocker, delegated/host
route, destructive implication, and uncertain postcondition without performing
those mutations or routes. Native Pi MCP qualification may run the same
bounded observation queries described under [status](#status).
An unqualified Native binding refuses the whole selected plan before publication;
apply does not silently skip it and publish the other targets. Select independent
work explicitly with `--target` instead. This preflight rule does not promise
rollback for a later delegated failure after effects begin.
Bare apply under the three-stream terminal contract discloses the same effect
plan and asks once. Non-interactive apply requires `--yes`. Every selected
supported config action and delegated route is ordinary apply work; there is no
separate route-attempt mode.

Human plans keep a resource's own configuration transition separate from a
blocked shared document. An unchanged server can therefore have a blocked
command prerequisite because another entry conflicts in the same document.
Command plans show their planner disposition: in a dry-run they are skipped,
even when a prerequisite is also blocked. Their command, arguments, and
environment-reference bindings remain disclosed. Passive observations and
previous-attempt evidence do not become current execution outcomes or authority.

For locked Pi and OpenCode extension order, apply settles required carrier
changes first, re-reads the selected host files, and plans from that fresh
state. Pi package order is treated as runtime precedence. OpenCode plugin-array
order is configuration order only. If the fresh plan introduces managed versus
foreign precedence changes that were absent from the disclosed plan,
interactive apply discloses those new risks and asks again. Non-interactive
`--yes` stops instead of expanding consent. A declined or failed second
confirmation leaves completed carrier work intact, performs no order write,
and does not start later delegates.

Before either the initial or renewed prompt, each precedence-risk row identifies
the managed relation subject, the foreign host-load identity, and whether the
managed row moves from before to after that foreign row or from after to before
it. Confirmation authorizes exactly those bounded relations; a count alone is
never treated as informed consent. Credential-free package identities are shown
verbatim. An identity containing URL credentials, query or secret-assignment
material, a local path, or more than 256 bytes is replaced by a deterministic
`redacted:sha256:<digest>` label. JSON reports the same label and sets
`foreign_identity_redacted` without changing the exact identity used for
planning, comparison, or authorization.

`--manage-existing` registers eligible exact-match unmanaged outputs as managed.
Whole-file matches must satisfy their required metadata before registration.
For shared config, a matching contribution can still require replacing the
document to correct its file mode; review the disclosed actions before applying.
This changes future deletion authority, so it is never implicit and never
imports source material. It cannot transfer a path already owned by a different
managed subject, even when the current bytes match exactly.

The same flag can acquire one state-only claim for an already declared, locked,
source-exact external carrier relation. Current support covers Claude Code
project/global, Codex global, OpenCode project/global, and exact stored-source
Pi project/global rows. Dry-run discloses the selected owner and claim store,
`explicitly_adopted_observed` provenance, later omission/removal meaning,
route-coupled removed and retained effects, non-claims, and the fact that
ambient consumers are not proven. Adoption itself invokes no host command.

Plain status/apply suggests the manage-existing dry-run only for
`present_unclaimed`. `present_unclaimed_ineligible` reports the first lifecycle
blocker without a success hint. Name-only, normalized-only, source-inexact,
shadowed, stale, unavailable, ambiguous, or conflicting rows cannot acquire a
claim. Antigravity CLI external rows remain source-inexact because current host
state does not retain their declared marketplace source.

`apply --dry-run --diff` emits one diff per physical file: singular `target` for
one consumer, complete canonical `targets` for a shared file, never a primary
consumer. Collection allows 4 MiB total current-plus-desired payload per file,
16 MiB per operation and 4,096 managed-path decisions. Content overflow is an
explicit omission; remaining decisions are one operation-level omission count.
Cancellation is checked before each decision/content read and between rendered
files. Rendering admits 250,000 LCS cells per file and 16,000,000 per report;
exhaustion produces one aggregate textual-diff omission count.

Apply also rejects any host mutation path that equals, contains, or is contained
by a local source consumed by the same manifest. Such an operation would mutate
its own locked input and could not remain reproducible on the next run.

Nonempty ordinary output results have their own `output results: N output entries`
heading in both default and verbose output. It counts the same output-entry units
as the plan: a shared managed path counts once, while each aggregate subject
counts separately even within one configuration file. The overall `applied`
action count also includes other work, such as extension ordering and carrier
claim changes. Default extension-order results summarize exact, unchanged
sequences; changed sequences and non-exact outcomes remain visible. Carrier
adoption results continue to show recorded claim transitions and provenance.

Successful default apply output contains counts and a concise row for every
performed output-entry action. Unchanged output entries do not get result rows.
Attempted-but-unverified results, retained residue classes, and failures remain
visible, with a next action only when needed. `--verbose` adds bounded paths,
reason codes, selected source/ref, and evidence. Raw subprocess output and
secret values are never printed. Redaction checks both captured and
display-normalized spellings.

Status and apply-dry-run JSON use the
[reconciliation-plan family](#json-schema-contract). The document contains
the derived lockfile status, lock-only resources, typed actions, delegated
actions, relation actions, physical extension-order actions, carrier-adoption
actions, carrier-absence actions, `host_route_attempts` history, diagnostics, MCP
status dimensions, and `has_errors`. Each `relation_order_actions` row names
one independently mutable physical sequence, its logical class, desired and
observed managed members, foreign-row count, current revision, runtime meaning,
and any typed `foreign_precedence_change` risks. OpenCode rows describe config
order only; Pi rows describe runtime precedence. A carrier install or removal
that must settle first is reported as `conditional_after_carrier_change`
instead of an executable order mutation.

For `effective_shadowing`, `state: "unobserved"` with reason
`EFFECTIVE_OBSERVER_UNSUPPORTED` means the catalog does not provide an
effective-config observer for that placement. Only Pi placements currently
provide one, for both Adapter and Native backends. `EFFECTIVE_STATE_UNOBSERVED`
instead means current evidence from a supported observer is unavailable or
unobservable, such as an opaque Pi config layer; it remains individually
visible in basic output. Neither reason means the binding is unshadowed.

Relation-order `detail` is path-neutral prose derived from typed reasons or
outcomes; raw evidence is verbose-only. Local or opaque source namespaces,
references, subject keys, order identities, and carrier keys use deterministic
`redacted:sha256:<digest>` labels with corresponding `*_redacted=true` JSON
fields. Credential-free non-local identities remain exact only when the carrier
grammar proves their identity. Source-derived `carrier_subject.name` follows the
same projection and sets `name_redacted=true`; removing `./` does not make a
project-relative source public.

Each delegated action exposes `packages` as the canonical package inputs daem
can derive from the preserved runner argv. The set can be partial when argv
uses opaque or unknown runner syntax. `pin_policy = "pinned"` applies only when
every execution-relevant package input is explicitly represented and exact;
one floating or opaque input makes the action-level policy `floating`. The
command and `args` remain the exact execution identity and are never
reconstructed from this diagnostic package projection.

Extension-order planning rejects, rather than truncates, any physical sequence
over 4,096 rows, desired or observed managed membership over 1,024 rows, or
projection with more than 16,384 possible managed/foreign precedence pairs.
These checks run before pair enumeration and never grant mutation authority
after a limit failure.

Carrier-adoption rows expose the exact relation evidence, lifecycle blocker or
eligibility, selected claim store, install/removal route identities, bounded
effects and non-claims, and `claim_transition`. An eligible dry-run uses
`would_record`. A successful explicit adoption uses `recorded`. When an exact
pending install already owns the same transition, success instead uses
`completed_by_install_recovery`; `final_claim_provenance` then reports
`installed_observed_transition` rather than implying explicit adoption. A
failure before any durable commit or delegated host-command boundary uses
`not_recorded`. Once one of those effects was attempted, a later error preserves
`recorded` or `completed_by_install_recovery` for each exact durable result
returned by the commit boundary; only actions without such a result use
`unknown_after_error`. Run `daem status` to resolve any remaining unknown action
instead of inferring it from the error alone.

Carrier-absence rows expose `execution = "host_route"` for delegated removal,
`execution = "direct_config"` for an exact host-config edit,
`execution = "observation_only"` for pending settlement, and
`execution = "state_only"` for already-absent claim retirement.

`apply --yes --json` uses the [apply-result family](#json-schema-contract):
executed action count, statefile path, bounded delegated attempts and
`host_route_attempts`, typed errors,
carrier-adoption transitions and final claim provenance, carrier-absence
outcomes, physical `relation_order_results`, and `has_errors`. Each order result
gives target, scope, class, physical sequence, `exact` / `converged` / `failed`
/ `not_attempted`, whether changed, and failure detail when present. Earlier
document success may survive later failure: no cross-document rollback is
promised. Retry reobserves every selected sequence and continues idempotently.

Reconciliation-ready planning failures and execution failures may also include
`mcp_statuses`. These are passive facts collected during planning, not a fresh
observation of the post-failure host state or proof of runtime readiness.
Successful apply results omit this field; run `status` for current observations.

In plan and confirmed-apply JSON, `host_route_attempts` is omitted when empty.
Its bounded records are history-only diagnostics, not proof of current
convergence or permission to skip a future apply.

Known mutation codes include `stale_snapshot`, `stale_plan`, `mutation_contended`,
`mutation_cancelled`, `interrupted_apply`, `interrupted_apply_file_set_fence`,
`journal_cleanup_incomplete`, `journal_cleanup_file_set_fence`,
`interrupted_file_set_transaction`, `file_set_evidence_invalid`,
`abandoned_file_set_residue`, `file_set_fence_census_limit`, and
`file_set_access_unprovable`. Typed `recovery_barrier` preserves journal and
file-set axes independently, with `unknown` for an unclassified peer. Active
recovery differs from cleanup-only authority. Published markers, markerless
residue and census overflow are continuing fences; invalid evidence or
unprovable StateDir access needs repair/restored access before apply or recover.
These are not `apply_refused`; never delete reserved names to bypass them.

Closed `phase` and `outcome` distinguish pre-effect refusal, incomplete effects
and complete compensation. Messages come only from typed facts, not internal
error strings. `mcp_backend_transition_unsupported` identifies a direct change
between stored and desired Pi codecs, with the binding, backends and retirement
guidance. It can refuse planning before an execution envelope exists.
`rolled_back` requires compensation for every attempted effect; a
retained provider prerequisite or any effect outside the journal keeps the
result `incomplete`. Ordinary JSON retains its closed detail; common CLI errors
separate the primary cause from repair guidance over the same classified facts.
Verbose evidence is bounded and sanitized. Projection, diff,
confirmation, diagnostic and output failures retain this boundary.

Before planning completes, human diagnostics for the selected manifest and
lockfile can include their escaped input errors, syntax locations and shared
repair guidance. Before an ordinary JSON result is selected, these appear in
the [common CLI failure](#common-cli-failures). Cancellation and recovery
barriers still take precedence. Unknown internal errors retain the closed
detail, and no reconciliation summary is shown before a plan exists.

Input-remediation commands use the selected manifest path, including without
`--verbose`. A missing lockfile may direct lock creation; a malformed existing
lockfile must be corrected first rather than rerunning the same failing lock
command. Repair guidance does not perform that operation or bypass its admission.

Host-route attempt rows include the exact operation and bounded
`effect_postconditions` requirement/state summaries when the route couples
additional removal effects. They contain no raw host output, secret,
machine-local path, or current authority.

Attempts are historical diagnostics, not proof of artifact identity,
package/cache convergence, readiness, trust, contribution ownership, cleanup
authority or future skip authority. `workdir_authority` means the selected
project path no longer names the retained root around an attempt. The route
never redirects to a replacement cwd, but may already have started from the
captured root; apply fails without convergence or a final attempt write through
the replacement. This is not a sandbox for other host effects.

Process facts remain independent: timeout followed by authority loss retains
`timed_out = true` and `attempt_reason = "timeout"` with overall reason
`workdir_authority`. Delegate rows use `process_reason` and
`workdir_authority_failed`; later authority failure never overwrites timeout,
cancellation, signal or exit facts.

## `recover`

```bash
daem recover [--manifest <path>] [--legacy-user-state] [--dry-run|--yes] [--json|--verbose]
```

Recovery classifies and resolves one interrupted apply operation or finishes
the exact retained cleanup of an already retired apply journal. Bare recover
uses the shared three-stream terminal contract and asks after disclosing the
current recovery plan; non-interactive execution requires `--yes`. It does not
read desired resources from the manifest or lockfile: the manifest selects the
derived state/recovery paths. An interrupted published metadata-transaction
marker remains a separate protocol recovered by retrying the exact authoring
or `unmanage` write, or by `migrate state --recover` for a state migration. Markerless private residue under the state directory is
not recoverable by retry; preserve it for analysis as described in
[troubleshooting](troubleshooting.md#manifest-metadata-update-was-interrupted).

`--legacy-user-state` selects only the default user manifest's former local
apply-journal root, before state migration. It is not an arbitrary state-path
selector and cannot bypass a canonical migration fence or a retired source.
See [User-State Authority Migration](state-and-recovery.md#user-state-authority-migration)
for the separate legacy authoring-metadata recovery boundary.

| Classification | Meaning |
| --- | --- |
| `clean_before` | Host paths, state, and ownership are all at the pre-operation state; only journal cleanup remains. |
| `clean_after` | Host paths, state, and ownership all match the committed post-operation state; only journal cleanup remains. |
| `needs_rollback` | The interrupted operation can be restored to its pre-operation state from verified journal evidence. |
| `needs_finalize` | Host paths and state are committed, but prepared ownership claims still need finalization. |
| `retained_cleanup_residue` | The active journal is already retired; only its exact correlated retirement residue and control remain to be finalized. |
| `blocked` | Current evidence cannot be safely reconciled with either legal operation state. |

Active-journal recovery revalidates guarded observations, backup identity,
candidate fingerprint, and the complete lease set before writing. Cleanup-only
recovery acquires and revalidates only its recovery root, retirement control,
residue, and final GC name; it does not inspect or mutate host destinations,
statefile, ownership registry, manifest, or lockfile. Both forms replan after
authority; a prior dry-run grants no execution authority. Blocked or stale
recovery retains evidence and writes nothing.

Before an active journal is retired, recovery reconciles every persisted removal
intent, including entries outside a selected subset. If blocked, it reports the
logical destination and typed namespace, residue, cleanup-stage, or durability
reason without private sibling paths. Incomplete promotion, cleanup, or absence
confirmation is `cleanup_action_failed` with the selected action; raw filesystem
errors remain internal.

Active recovery output shows classification, operation identity, every action,
destination/content subject, blocker and limitation. Default output omits backup
paths/hashes and journal layout; `--verbose` adds operation directory, backup
facts, reasons and action detail. Cleanup-only output shows
`retained_cleanup_residue`, `finalize_journal_cleanup`, operation identity and
the no-host/state/ownership limitation, never private control/residue/GC paths
even in verbose mode. Pre-1.0 `.daem-tombstone-<32 lowercase hex>` evidence is
blocked before plan disclosure, neither inspected nor migrated; other
reserved-namespace names are blocked as malformed.

Both `--dry-run --json` and `--yes --json` use the
[recovery family](#json-schema-contract). Every result declares one `phase`.
Dry-run results and write attempts rejected before execution use `planned`.
After execution begins, a
freshly reclassified active journal uses `active_authority_retained`; a
freshly reclassified cleanup-only continuation uses
`cleanup_authority_retained`. These retained phases declare `authority_kind`
as `active_journal` or `journal_cleanup` and preserve only that fresh current
classification and retry authority. Active-journal output in these phases
includes its operation directory and typed action facts: resource-owned actions report
`resource` and singular `target`, while subject-owned managed paths report
`subject`, the complete `targets` consumer set, and `content_kind` without
inventing a primary target. Entity-backed projection subjects also report the
correlated resource identity for user-facing attribution.

After execution, successful write results use `completed`. A failure after
durable authority retirement uses `authority_retired`. If post-execution
inventory cannot classify whether active, cleanup-only, or no authority remains,
the result uses `authority_unknown`, exposes no retry plan, and instructs the
operator to preserve recovery evidence. Terminal and unknown results retain
only the path-neutral operation id, phase, error status, empty action and
cleanup-obligation arrays, and a continuing file-set fence when one remains.
They omit
`authority_kind`, `operation_dir`, `classification`, and the pre-execution plan
because those facts no longer describe a retryable authority. Active recovery
also reports `continuing_file_set_fence` whenever a fresh terminal observation
is non-clear or cannot be classified. Closed values include published markers,
markerless residue, census limits, invalid evidence, and access-unprovable
StateDir boundaries; `unknown` means the axis was observed but could not be
classified. The field accompanies dry-run and terminal write output, includes
path-neutral preserve/restore-access guidance in human diagnostics, and journal
completion does not imply that the separate fence was cleared. A cleanup action error is projected only
when fresh cleanup-only authority
remains; `authority_unknown` and `authority_retired` take precedence over the
action from the pre-execution plan. A
post-retirement validation error remains an error result with
`phase = "authority_retired"`; it does not advertise another recovery attempt.

Active-journal output separately reports `cleanup_obligation_count` and
`cleanup_obligations`. Each obligation contains only its logical scope and
destination, readiness, selected action, typed reason, and path-neutral detail.
This discloses residue promotion, partial cleanup continuation, durable absence
confirmation, and namespace or slot blockers without exposing the private
residue and cleanup-stage names. Execution re-observes the same facts before
performing any cleanup effect.

Planned or retained cleanup-only output contains the operation id, its cleanup
classification, and exactly one action whose only field is
`kind = "finalize_journal_cleanup"`. It reports zero cleanup obligations and
omits `operation_dir` and all resource, subject, target, scope, destination,
content, backup, and detail fields instead of emitting synthetic empty
active-plan placeholders. Terminal cleanup output follows the common terminal
shape described above.

Cleanup failures use the same path-neutral semantic error in human/JSON output.
While fresh authority remains, the error names the action and `phase=execution`,
not retirement paths or wrapped filesystem errors. After semantic retirement, GC
failure reports hidden residue and `phase = "authority_retired"` without the
former action name. It remains a command failure, but no recovery action remains
and later commands are not blocked.

## `migrate state`

```bash
daem migrate state [--manifest <path>] [--recover] [--dry-run|--yes] [--json]
```

Transfer the default user manifest's former local state authority to its XDG
state location without changing installed outputs or invoking host packages.
This is not arbitrary workspace relocation or migration between changed
HOME/XDG settings. Use `--manifest` when cwd would select another manifest.

```bash
daem migrate state --dry-run
daem migrate state --yes
```

The preview lists the source and destination statefiles and both shared
registries. Execution requires the normal three-stream confirmation or `--yes`;
`--dry-run` neither writes nor acquires mutation leases. `--dry-run` and `--yes`
are mutually exclusive; JSON requires one of them. A completed migration can be
repeated as `already_migrated` without another transfer.

An interrupted migration must be inspected with `migrate state --recover
--dry-run` and executed with `migrate state --recover --yes`. Its disclosed
`rollback` or `finalize` action applies only to that migration's exact metadata
set. It does not recover unrelated authoring transactions or apply journals.
See [User-State Authority Migration](state-and-recovery.md#user-state-authority-migration)
for retained evidence, cache behavior, refusals and legacy recovery.

JSON uses `schema_version = 1`, `mode` (`dry-run` or `write`), `action`,
`manifest_path`, `source_statefile`, `destination_statefile`,
`output_registry_path` and `carrier_registry_path`. Transfer and repeat results
also report `output_claims`, `carrier_claims`, `managed_paths` and
`managed_aggregates`; recovery omits these counts rather than inventing zeros.
Transfer actions are `migrate`, `migrated` and `already_migrated`; recovery actions
are `rollback` and `finalize`. An execution error retains the disclosed action
and adds `error`; that action is not evidence of completion or a fresh recovery
plan. Re-preview current evidence before retrying.

Exit status is `0` for a successful preview, migration, recovery or completed
repeat, `1` for refusal/execution failure, and `2` for invalid arguments or
missing non-interactive authorization. With JSON selected, planning errors use
the common CLI failure family rather than a migration result. No legacy
management to transfer is a refusal, not a new empty state.

## `doctor`

```bash
daem doctor [--manifest <path>]
  [--target <target> ...|--all-targets] [--json|--verbose]
```

With a manifest and no target selector, doctor checks only effective targets
and declared resource capabilities. Without a manifest, it can run general
diagnostics. `--all-targets` is mutually exclusive with `--target`.

Doctor is passive. It never launches host CLIs, package managers, plugins, MCP
servers, credential helpers, or network probes. It checks modeled paths,
permissions, executable discovery, capability support, local skill
compatibility, and available passive readiness facts. On admitted platforms,
disposable scratch files and trees exercise publication, replacement, removal,
artifact access, and recovery-provenance capture in selected cache, workspace,
state, data, and target directories (or their nearest existing parents).
Existing files and managed state are not changed. These checks do not certify
server durability or concurrent-writer exclusion.

On a not-admitted platform, doctor keeps the platform error after successful
path resolution. It still runs checks whose success meaning is unchanged, such
as manifest syntax after the path is known, and names remaining
capability-bound checks `unsupported` or `skipped`. It does not execute Git,
search PATH for MCP executables, or invoke durable file-set or recovery
inventory adapters. Host config grammar checks read a bounded regular-file
snapshot and admit TOML/JSON structure before decoding. A path-resolution
error is reported alongside the platform
error. Human and JSON forms use the same finding and exit with status `1`.

On an admitted platform, doctor refuses human and JSON diagnostics while an
interrupted apply journal is active, before reading live host or target
configuration; run `daem recover --dry-run` first. On a not-admitted platform
the recovery inventory is not invoked and the check is named `unsupported`.

Default output prints ok/warn/error/skipped/unsupported totals plus every
warning, error, skipped, and unsupported check. Successful `ok` checks are
count-only. `--verbose` prints every check and bounded detail. Warnings,
skipped checks, and unsupported checks do not fail doctor; errors do. Doctor
JSON schema version is `2` and contains manifest context, selected targets, all
typed checks with `status`, and `has_errors`. Check `status` is not diagnostic
`severity`; apply diagnostics keep the three-value `severity` field.

## `probe mcp-server`

```bash
daem probe mcp-server <name> [--manifest <path>] [--target <target>]
  [--scope <scope>] [--timeout <duration>] [--dry-run|--yes]
  [--json|--verbose]
```

Target and scope are needed only when the locked server name is ambiguous. Bare
probe uses the shared three-stream terminal contract and asks after disclosing
the exact probe effects; non-interactive execution requires `--yes`. The current
supported rows are documented in the feature matrix.

Dry-run discloses the exact command/args, child-to-host env reference bindings,
inherited process environment policy without values, descriptor-backed selected
project work-directory policy, timeout,
process/package/cache/network/auth/trust effects, cancellation and cleanup
expectations, and non-claims. Execution launches the exact locked stdio command
and attempts MCP initialize for supported rows. Interactive execution consumes
the same immutable request that was disclosed; manifest or lockfile edits while
confirmation is pending cannot substitute a different command. It never mutates
the manifest, lockfile, state ownership, or host config.

Output remains dimensional: runtime launcher, protocol initialize,
authentication, endpoint health, and tool inventory. No surface emits an
aggregate `ready=true` or `healthy=true`. Probe evidence is current best-effort
runtime evidence, not config convergence, package/cache convergence, host trust,
future apply-skip authority, or persistent status evidence.

Probe JSON schema version is `1` and contains the selected subject, timeout,
exact side-effect disclosure, every dimension, and `has_errors`. Environment
disclosure uses ordered `env_bindings` entries with `child_name` and
`host_source_name`; it never emits the resolved value.

## `refresh extension`

```bash
daem refresh extension <id> [--manifest <path>] [--target <target>]
  [--scope <scope>] [--timeout <duration>] [--dry-run|--yes]
  [--json|--verbose]
```

Refresh selects exactly one declared extension id and its matching locked
`refresh` operation. Optional target and scope are exact safety filters, not
alternate destinations. There is no bulk, wildcard, per-host flag, bare
`update`, or apply-refresh mode.

Dry-run passively builds and discloses the same operation identity, argv shape,
effect envelope, retained effects, observation posture, and non-claims used by
execution. Bare execution uses the shared three-stream terminal confirmation
contract. Non-interactive and JSON execution require `--yes`; `--dry-run` and
`--yes` are mutually exclusive.

`--timeout` bounds only the one delegated host CLI child-process attempt. Its
default is `10m`; accepted values are whole-second durations from `1s` through
`1h`, inclusive. Planning, disclosure, confirmation, passive observation,
attempt-history persistence, and cleanup are outside that child-process
timeout. The selected value is part of the immutable disclosure and refresh
fingerprint, so execution cannot use a different duration after confirmation.

Refresh never rewrites the manifest, lockfile, or managed-relation ownership.
It refuses a missing or stale lock, an active recovery journal, an unsupported
refresh route, or changed manifest/lock/observation/authority evidence after
disclosure. A route requiring passive evidence must prove the exact relation
present before execution and observe it again afterward. A route whose locked
contract explicitly has no observer may run only as best effort.

Claude Code currently supports project and explicit-global marketplace
relations. The command maps public global scope to host `--scope user`; project
scope uses `--scope project`. It may refresh an exact externally installed
declared relation without acquiring daem ownership. A successful result proves
only that the exact relation remains observable after the host update; the
host-selected version, cache, dependencies, restart, activation, and runtime
readiness remain outside the claim.

Codex refresh supports only explicit-global marketplace relations and acts on
the marketplace, so the host may replace its Git snapshot and refresh sibling
caches. Observation does not prove convergence; both unchanged- and
changed-revision successes are `attempted_unverified`. Partial divergence is
possible, and daem does not add another marketplace, claim rollback, or
reinterpret host refusal. See [host integrations](host-integrations.md) for
route details.

OpenCode refresh supports project and explicit-global relations; scope controls
whether `--global` is appended, and success remains `attempted_unverified`
because observation does not prove package, version, or refresh convergence.
Host-owned package, cache, config, dependency, activation, and readiness effects
remain outside that claim. Ordinary `apply` does not use `--force`. See [host
integrations](host-integrations.md) for route details.

Pi refresh supports project and explicit-global package relations but has no
update scope flag: both invoke the same host update, which may inspect and
update matching user and trusted-project rows. Success remains
`attempted_unverified`; pinned, local-path, and Git-source behavior remains
host-owned, and trust refusal or no match is a host failure. Daem adds no
approval, self-update, model-update, or bulk-update flags. See [host
integrations](host-integrations.md) for route details.

Antigravity explicit-global refresh repeats the locked install route rather than
using a dedicated update command. Local evidence can show bundle replacement and
malformed `plugin.json` rejection; remote and marketplace resolution remains
host-owned. Passive global observation proves only the selected import row and
matching bundle identity, not version or freshness, so explicit success is
`attempted_unverified`; other source forms retain unsupported observation. Daem adds no
import/link/enable/disable/project-scope or IDE behavior and makes no freshness,
rollback, ownership, or readiness claim. See [host
integrations](host-integrations.md) for route details.

The result classes are:

| Class | Meaning |
| --- | --- |
| `planned` | Dry-run built one valid immutable plan; no host attempt started. |
| `refused` | Selection, lock, evidence, authority, or stale revalidation blocked before launch. |
| `cancelled` | The operator declined or cancellation occurred before launch. |
| `attempted_unverified` | The host request succeeded through a supported route without an outcome observer. |
| `observed_relation` | The host request succeeded and fresh passive evidence proves the exact relation present. |
| `failed` | Launch or host execution failed without a timeout, cancellation, or signal after process start; `attempted` distinguishes pre-launch failures. |
| `partial` | A started request timed out, was cancelled or signaled, succeeded without required post-observation, or could not persist history or complete cleanup; host effects may remain. |

Once a process starts, refresh never retries, rolls back, invokes a separate
install fallback, updates the lock, or claims absence of host effects.
Timeout, cancellation, and signal termination after process start are
therefore `partial`, not proof of an effect-free failure.
Antigravity's disclosed refresh route itself is the host's repeat-install
operation. A sanitized,
operation-indexed attempt row is persisted only for a started process and is
history, not future skip or removal authority.

Refresh JSON schema version is `4` and has exactly these top-level fields:

```text
schema_version command mode selection route disclosure result has_errors
```

Disclosure includes deterministic command/args, environment names without
values, selected-root cwd policy, timeout, effects, retained effects and
non-claims. Success has empty `result.detail`; failure detail derives only from
closed `reason_code`, process and relation-observation facts, never underlying
error strings.

Active recovery is `interrupted_apply`; cleanup-only authority is
`journal_cleanup_incomplete`. Their continuing-fence forms are
`interrupted_apply_file_set_fence` and `journal_cleanup_file_set_fence`.
Published markers use `interrupted_file_set_transaction`, markerless residue
`abandoned_file_set_residue`, census overflow `file_set_fence_census_limit`,
StateDir access/identity loss `file_set_access_unprovable`, and
invalid/incomplete evidence `file_set_evidence_invalid`. Cancellation outranks
these during replan; none becomes `stale_plan` or generic `mutation_authority`.

`result.recovery_barrier` preserves both observed axes, with `unknown` for an
unclassified peer; the known peer still determines the actionable code. Human
failures use the same detail. `process_outcome.reason` and
`authority_outcome.workdir_failed` independently report mechanical outcome and
cwd-authority loss. Summaries exclude subprocess output, raw errors, secrets,
protocol payloads and local paths.

After a refresh result JSON document has been written to stdout, daem does not
append failure prose to stderr. Dry-run failures and write-planning failures
with a disclosed result use one stdout result document and empty stderr. Confirmed
`--yes --json` execution still writes its complete authorization document to
stderr before effects; a later failure remains only in the final stdout result,
so the authorization stream stays one valid JSON document. CLI grammar and
other failures before a result can be formed use the common CLI failure family
when JSON is selected. A result-write failure may still produce a bounded stderr
diagnostic, never a replacement result.

Current target-specific route availability remains authoritative in the
feature matrix.

## Progress

Progress is automatic only for human output when stderr is a TTY and the
workflow exposes a meaningful long-running phase. It is suppressed for JSON,
non-TTY stderr, help, CLI misuse, and before interactive confirmation.

Progress covers lock/outdated source resolution, apply execution, import
discovery and `refresh extension` host processing. Import writes also report
freshness revalidation/publication. At most one ephemeral stderr line is shown,
with escaped labels and clearing before stable output/diagnostics. Refresh
reports the selected extension and authorized timeout, not invented host
percentages. Duplicate completions never advance counts twice.

A progress write failure disables later progress but does not fail an otherwise
valid operation. A stable disclosure or confirmation-prompt write failure
blocks interactive execution. Any stable human or JSON output write failure
fails the command.

## Streams And Exit Codes

| Situation | Stdout | Stderr | Exit |
| --- | --- | --- | ---: |
| Help or successful human result | stable help/result | ephemeral progress or prompt only | `0` |
| Successful JSON result | exactly one JSON document | empty, except [refresh authorization](#json-stream-exceptions) | `0` |
| Blocking result for a command whose result contract is failure | stable human or one JSON result | no duplicate prose | `1` |
| Report-only `status`, including pending or blocked rows | normal result | empty | `0` |
| Clean `status --check` | normal result | empty | `0` |
| Non-clean `status --check` | normal result | empty | `1` |
| Non-current `outdated --check` | normal result | empty | `1` |
| Warning-only result | stable result | empty | `0` |
| CLI misuse, human mode or unknown command route | empty | problem plus nearest correction | `2` |
| Known-leaf CLI misuse with JSON selected | one `cli_error` usage document | empty unless delivery fails | `2` |
| Operation failure before an ordinary result, human mode | empty | concise error and remediation | `1` |
| Operation failure before an ordinary result, JSON selected | one `cli_error` operation document | empty unless delivery fails | `1` |
| Failure after JSON envelope exists | one typed JSON result | [refresh authorization](#json-stream-exceptions); otherwise empty unless write fails | `1` |
| Confirmation declined/canceled | disclosed plan remains | cancellation line | `1` |
| Stable output write failure | possibly partial | one bounded write diagnostic without the writer's error text | `1`, unless preserving an existing nonzero command identity |
| Interrupted by `SIGINT` | completed or partial stable output | bounded interruption diagnostic | `130` |
| Interrupted by `SIGTERM` | completed or partial stable output | bounded interruption diagnostic | `143` |

Stable confirmation disclosures are written to stdout; prompts and bounded
cancellation diagnostics are written to stderr. Human warnings that belong to
a valid result stay on stdout. Command-specific non-disclosure rules continue
to withhold raw subprocess/protocol output, authentication material, headers and
known secret inputs. The [credential-detection non-goal](#presentation-flags) does not
relax those rules or promise detection of arbitrary secrets in free-form text.

First `SIGINT`/`SIGTERM` records exit identity and cancels the operation. A
later signal arms a bounded emergency exit, preserving the first exit code and
ongoing TERM-to-KILL cleanup window; completed cancellation returns immediately.
Context cancellation without an OS signal supplies no signal exit identity.
Internally supplied interactive authorization requires stdin/stdout/stderr, all
three terminal facts and context-aware confirmation reading; otherwise execution
is non-interactive.

### JSON Stream Exceptions

- Confirmed [`refresh extension --yes --json`](#refresh-extension) writes one
  authorization JSON document to stderr before effects and one final result to
  stdout. The authorization remains on stderr whether execution succeeds or
  fails. Ordinary execution-failure prose is not appended, but an output-write
  failure can still add a bounded diagnostic.

## Deferred Command Names

There is no top-level `update`, `plan`, `diff`, `install`, `uninstall`,
`prune`, `snapshot`, `restore`, or `run` command. Use:

- `lock` to resolve current exact source identities and `outdated` to compare
  them without writing;
- `refresh extension` for one explicitly selected supported host-carrier
  refresh;
- `apply --dry-run` for a reconciliation plan and optional diff;
- manifest/add/remove desired presence plus supported apply effects for
  lifecycle work;
- `recover` for interrupted-operation cleanup; and
- native target locations reconciled by apply rather than a runtime wrapper.
