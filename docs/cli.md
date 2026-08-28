# CLI Reference

This document is the public command, flag, output, stream, and exit contract for
`daem`. Resource schema and target-specific fields belong in the
[Manifest Reference](manifest.md); current user-facing coverage belongs in
[Feature Support](features.md), while operating-system and architecture support
belongs in [Platform Support](platforms.md).

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

`help`, `-h`, `--help`, and `--version` are meta surfaces. Use
`daem help <command>` and `daem help <group> <resource>` for progressively
scoped help. `daem --version` is the human alias for `daem version`; it rejects
additional arguments and there is no `-v` alias.

## Platform Support

The exact supported platforms and their verification requirements are
authoritative in [Platform Support](platforms.md).

On an unsupported platform, all help and version routes remain available and
`doctor` reports the platform error in human or JSON form. Commands identified
as platform-gated in [Platform Support](platforms.md) fail before path
resolution or effects, including in dry-run mode. Their normal command errors
use stderr; `doctor --json` is the structured platform diagnostic. Doctor
resolves its selected manifest path. After successful path resolution on a
not-admitted platform, doctor keeps the platform error and continues with
checks whose success meaning is unchanged. Capability-bound remaining checks
are named `unsupported` or `skipped` rather than `ok`. A storage abort must not
erase the platform finding. Path-resolution errors are reported alongside the
platform error.

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

A user manifest is not a project root. Project-scoped resources selected from
the user manifest are rejected with guidance to select a project manifest or
declare global scope.

### Daem Storage Roots

On supported Unix platforms, daem observes four XDG variables. A non-empty value
must be an absolute path. The `daem` directory is appended to the configured
root:

| Variable | Default daem path | When it applies | Owned content |
| --- | --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config/daem` | Implicit user workspace only | User manifest and adjacent lockfile |
| `XDG_STATE_HOME` | `~/.local/state/daem` | Implicit user workspace only | Statefile and recovery journals |
| `XDG_CACHE_HOME` | `~/.cache/daem` | Implicit user workspace only | Resolved source cache |
| `XDG_DATA_HOME` | `~/.local/share/daem` | Every selected workspace | Shared output-ownership and carrier-claim registries |

An explicit or cwd-selected project manifest keeps its state and recovery data
under `<manifest-root>/.daem` and its source cache under
`<manifest-root>/.daem/cache`; changing `XDG_STATE_HOME` or `XDG_CACHE_HOME`
does not relocate those project-local paths. `XDG_DATA_HOME` remains shared
across project and user workspaces because its registries coordinate global
ownership and carrier claims.

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
| Desired/derived state | `init`, `import`, `add`, `remove`, `lock` | write | preview | rejected |
| Host/runtime effect | `apply`, `recover`, `probe mcp-server`, `refresh extension` | three-stream TTY confirmation | preview | non-interactive execution |

An empty host/runtime plan does not prompt. JSON cannot share an interactive
prompt, so host/runtime JSON requires `--dry-run` or `--yes`.

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
- On POSIX platforms, concrete next commands are rendered from an argument
  vector so dynamic paths remain one shell argument. Control-bearing arguments
  use a terminal-safe POSIX reconstruction and still round-trip to the exact
  argv; arguments containing NUL cannot be represented and produce no concrete
  command. Commands containing placeholders such as `<target>` are templates,
  not concrete argv claims.
- Human-readable dynamic paths and errors escape backslashes, controls, format
  characters, and invalid UTF-8 bytes. Structural line breaks remain owned by
  the presenter, so dynamic values cannot forge another diagnostic or hint.
- `--verbose` adds bounded typed causal, provenance, path, and identity
  evidence. Error types without a bounded evidence projection are reported as
  omitted instead of invoking an arbitrary error formatter. Verbosity never
  changes selection, planning, authority, or mutation.
- `--json` emits exactly one schema-versioned JSON document to stdout.
- `--json` and `--verbose` are mutually exclusive.
- `--diff` is accepted by `import`, every add/remove leaf, and `apply`. It
  requires `--dry-run` and is mutually exclusive with `--json`.
- `--verbose --diff` is allowed.
- `--check` exists only on `outdated` and `status`. It preserves normal output
  but returns non-zero when that command's clean predicate is false.

### Command Flag Inventory

This table is the exhaustive long-form flag inventory for leaf commands.
Operands, `help`/`-h`/`--help`, and the root `--version` alias are not command
flags.

| Command | Accepted flags |
| --- | --- |
| `add extension` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
| `add hook` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--matcher`, `--scope`, `--target`, `--timeout`, `--verbose` |
| `add instruction` | `--diff`, `--dry-run`, `--json`, `--manifest`, `--scope`, `--target`, `--verbose` |
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
| `recover` | `--dry-run`, `--json`, `--manifest`, `--verbose`, `--yes` |
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
| `add`, `remove`, `import`, `unmanage extension` | Manifest authoring | `5` |
| `lock`, `outdated` | Lock comparison | `4` |
| `list resources` | Resource inventory | `2` |
| `list outputs` | Output inventory | `4` |
| `list paths` | Agent location inventory | `1` |
| `status`, `apply --dry-run` | Reconciliation plan | `12` |
| confirmed `apply` | Apply result | `19` |
| `recover` | Recovery plan/result | `9` |
| `doctor` | Passive diagnostics | `2` |
| `probe mcp-server` | Runtime probe | `1` |
| `refresh extension` | Extension refresh | `4` |

Consumers must select the expected command envelope, inspect
`schema_version`, and reject unsupported versions before interpreting any
other field. A consumer must not infer compatibility from a version used by
another command.

Every consumer-visible field addition, removal, rename, type or nullability
change, or semantic reinterpretation requires a version increment before
release. A same-version correction may only restore the documented shape and
meaning. Each daem binary emits one current version per envelope; it does not
negotiate or emit legacy variants. Until a command explicitly documents a
multi-version policy, consumers must update for a new version rather than
silently accepting it. Released version changes belong in release notes.

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

Skill import may resolve the selected top-level skill directory through a
symlink, but records the resulting absolute route and does not follow any
symlink within the skill tree. Daem proves that the root `SKILL.md` is regular
in the same descriptor-bound traversal that hashes the tree. It hashes each
shared resolved route once per planning pass. Every distinct route observed for
same-name duplicate or conflict classification is charged to the same
400,000-entry/16-GiB source-identity observation budget even when that route is
later skipped and therefore does not become publication freshness authority.
A classified nested-symlink exit still charges every listed name the traversal
had already materialized but had not yet consumed, including remaining names in
ancestor directories, so listing work cannot bypass that envelope.
When identical content from several target routes becomes one multi-target
declaration, every contributing route remains freshness evidence and the
representative target's canonical route supplies the bytes. Daem then streams
that exact planned directory
identity into private staging. Any identity-changing replacement or mutation,
including an entry, executable-mode, or entry-kind change during copying,
fails before manifest publication. Import planning and private staging both
accept at most 100,000 entries, 64 descendant-directory levels, and 4 GiB of
regular-file bytes, matching the cleanup traversal that owns failed stages. A
dry run therefore never recommends writing a skill tree that staging would
reject. The complete import freshness observation also admits at most 400,000
entries and 16 GiB of regular-file content across all observed trees and cannot
widen that per-tree publication ceiling. The containing skills root is observed
as an immediate inventory and does not reduce an individual skill's depth
allowance. One import planning pass retains at most 100,000 immediate names
across newly observed distinct resolved skills roots, with at most 32 MiB of
aggregate name bytes and 4,096 bytes per name. Reused roots do not consume that
retained-name allowance again, but daem revalidates both the exact directory
inventory identity and every live-root symlink binding on reuse, after revision
capture, and before publication. Child reads are derived from the captured
resolved root rather than re-resolving the mutable live alias. A changed root or
alias aborts the observation instead of returning an incomplete preview. Extension inventory
files retain bounded content evidence through publication using each host
observer's ingress limit. Files are streamed. The `SKILL.md` compatibility
document retains its 1 MiB limit.

Import refuses preview and write modes while an interrupted apply journal is
active, before scanning live agent files. Run `daem recover --dry-run` first.

Default human output contains target/scope totals, resource counts, skip counts
by `action_required`, `unsupported`, and `informational` category, the
destination, and nearest next commands. `action_required` identifies a live
source or explicit authoring decision the user can resolve; `unsupported`
identifies a surface this daem version cannot manage; `informational`
identifies expected discovery or deduplication noise. Every actionable skip in
an admitted completed result remains visible with a next action. One import
planning pass retains at most 4,096 skip rows and 256 KiB of aggregate dynamic
skip diagnostics; one `detail` value is at most 4,096 bytes and a larger value
is replaced by a whole-value digest and byte count. Every input-scaled producer
admits a skip synchronously at this boundary before retaining another
producer-local row. MCP codecs admit each sorted server rejection during
classification, and Antigravity extension inventory admits each proven complete
import/bundle pair before observing the next plugin. The Hook parser's
all-or-one document buffer is separately fixed at the same row and diagnostic
limits. An ordinary producer failure or cancellation rolls back that producer's
rows; only aggregate exhaustion retains the bounded prefix for diagnostics.
Exceeding either aggregate limit aborts before a plan or write, emits the
already-retained rows once plus an explicit diagnostic-budget marker on
stderr, and emits no JSON result envelope. Unsupported and informational skips
are compacted by target and reason; `--verbose` retains every admitted exact
per-path skip in successful and
no-resource failure output, in addition to individual clean scan, resource, and
merge rows on successful plans. Write-mode merge conflicts use the same skip
report before the dry-run conflict hint. JSON retains every admitted typed row with
target, scope, category, and an optional stable action hint. After a
successful write with imported resources, human output points to lock preview
and then to
`apply --manage-existing --dry-run` after the lockfile is written. The latter
only previews registration of eligible exact matching live outputs; import does
not register them and never recommends `--yes`.

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

Common authoring is intentionally curated:

| Resource | CLI owns | Manifest owns |
| --- | --- | --- |
| Extension | id, one opaque carrier-native source, target, scope | carrier spelling and lifecycle/contribution policy |
| Instruction | name, local source, target, scope | remote source and target-specific placement |
| Hook | name, event, command, matcher, timeout, target, scope | status text, target overrides, assets, non-command handlers |
| MCP server | name, portable command, ordered args, target, scope | exact absolute command path, env refs, remote transport, auth, cwd, tool policy |
| Skill | source, Git path/ref, installed name, target, scope | resource id, install mode, repair and source policy |
| Skill group | source root, exact members, Git path/ref, target, scope | selectors, install mode, repair and per-member policy |

Hook `<command>` is one opaque shell-command string. Hook timeout is a positive
duration such as `30s` or `2m` and must be exactly representable as whole
seconds. MCP `<command>` is one portable executable token, not a shell command;
`--arg` preserves argv order and duplicates. Exact machine-local MCP
executables use the manifest-only `command = { path = "/absolute/path" }`
form; `daem import` also emits that form for supported host entries with an
absolute command.

For `--target pi`, add also ensures that one compatible explicit
`pi-mcp-adapter` package is declared at the selected scope. The default provider
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
manifest-only.

Default output names the semantic resource change plus manifest and lock
outcomes. It omits the full prospective TOML, unchanged lock rows, and source
internals. `--verbose` adds bounded normalized and lock evidence; `--diff` owns
the exact manifest delta.

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
`daem list resources`.

Omitted target and scope selectors remove one unambiguous whole resource.
Selectors narrow or disambiguate; they do not inherit add defaults. Removing
the last selected target removes the declaration. A partial target removal from
a multi-member skill group is rejected because its target set is shared; split
the group in the manifest first.

Remove changes desired and locked state only. Host file deletion, config
binding removal, and supported route effects are later apply work. It never
means unconditional carrier uninstall, package/cache cleanup, credential
deletion, trust reset, or contribution-level mutation.

Removing a Pi MCP row retains its explicit provider extension. A later apply
removes the managed Pi config contribution and reports any lower-layer fallback
that becomes effective; remove the provider extension separately only when the
package relation itself is also undesired.

For extension rows, removal expresses desired relation absence. Manual deletion
of the same row followed by `lock` produces the same later status/apply
semantics. If durable state proves that daem manages the exact relation, a
later supported apply plan may remove it through the disclosed host-native or
direct-config route. If that authority is absent, ambiguous, stale, or
unsupported, apply blocks without mutating visible host state.

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

Structured output uses authoring schema `5` without changing existing
add/remove rows:

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

Both dry-run and write refuse before reading selected metadata while an active
apply recovery or incomplete journal cleanup remains. Write mode checks the
same recovery fence again under mutation authority before recovering a
metadata transaction, rebuilding the candidate, or committing. Run
`daem recover --dry-run` first when recover can produce a plan; markerless
residue and published metadata-transaction markers remain after recover and
are not cleared by it. If the state directory cannot be inspected, restore
access first instead of running recover.

An interrupted write leaves a recoverable metadata transaction marker. Other
manifest/lock/state consumers fail closed while it exists; rerunning the exact
`unmanage extension` write recovers under the same complete authority set
before revalidating and committing. `daem recover` is reserved for apply
recovery journals and does not consume this marker.

## Authoring JSON

Init uses schema `1`. Add, remove, import, and unmanage use schema `5` with
these common fields:

| Field | Meaning |
| --- | --- |
| `command`, `mode`, `operation` | operation identity and dry-run/write mode |
| `manifest_path`, optional `source_dir` | selected durable destinations |
| optional `lockfile` | adjacent lock path and would-write/written/unchanged status |
| `resource_count`, `change_count`, `changes` | typed affected resources and manifest blocks |
| `has_errors`, optional `warnings` | result classification |
| unmanage `management`, `host` | exact management-state outcome and retained-host projection |
| import `summary`, `scans`, `skipped`, `merge_results` | exhaustive observation and merge rows |

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

Human next-command prose is deliberately absent from schema `5`. CLI misuse or
a failure before a result envelope exists goes to stderr and produces no JSON.
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
subject identity. When extension order constraints exist, it also reports
class-relative order counts and changed class ids. It omits unchanged
identities, source ids, hashes, resolved refs, full repair recipes, and member
details. `--verbose` adds unchanged identities, bounded resolution evidence,
and before/after ordered members.

Regenerating the lockfile removes entries no longer represented by the
manifest. It does not inspect or delete host outputs; that belongs to status
and apply under managed-state authority.

## `outdated`

```bash
daem outdated [--manifest <path>] [--check] [--json|--verbose]
```

Outdated runs the same lockable-source resolution and validation path as lock,
compares the candidate exact identities with the current lockfile, and writes
nothing persistent. Default current output is count-only; stale output lists
every added, changed, or removed identity and the lock next step. `--verbose`
adds checked current identities and bounded refs. `--check` exits `1` when any
lock identity would change.

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
manifest without requiring a lockfile. It prints every selected kind, stable
remove key, install name, target set, and scope. `--verbose` adds source and
declaration provenance.

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

`list resources` JSON uses schema version `2`. Extension source identities use
the same carrier disclosure rule as lock output and set `source_redacted` when
the value is replaced. `list outputs` JSON uses schema
version `4`, with separate `managed`, `unmanaged`, and `blocked` arrays and
counts. Rows retain their canonical `subject` and complete `targets` consumer
set while reporting the correlated resource identity when one exists.
Newly classified unmanaged and blocked aggregate rows also carry
`content_path`, which identifies the selected contribution inside the shared
file, while `hash` remains empty because daem does not own the whole document.
Both commands include every selected row.

`list paths` JSON uses schema version `1` and a flat `locations` array. Every
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

Default output prints totals, every planned mutation, blocker, drift,
unsupported/ambiguous class, binding-removal result, and uncertain host-route
class. It omits routine no-ops, state/content paths, internal reason fields,
hashes, route ids, and exhaustive evidence arrays. `--verbose` adds those
bounded causal fields and every ordinary row.

The table below describes exit codes for valid status results. Command failures
still return `1` before or while emitting their applicable result contract.

| Invocation | Exit `0` | Exit `1` |
| --- | --- | --- |
| `status` | any valid report | never because of reported state |
| `status --check` | lockfile present, no pending output action, exact extension order, and no blocked relation, adoption, or extension-removal action | lockfile missing; pending output action; non-exact extension order; or blocked relation, adoption, or extension-removal action |

Warning-only diagnostics, selected missing extension relations, and observe-only
relation rows do not make `--check` fail. A blocked removal of a managed
extension relation does fail. A normalize, conditional, or blocked
extension-order row is also non-clean. JSON and human modes use the same exit
predicate.

## `apply`

```bash
daem apply [--manifest <path>] [--target <target> ...]
  [--manage-existing] [--dry-run|--yes] [--diff] [--json|--verbose]
```

`apply --dry-run` reports every selected mutation, blocker, delegated/host
route, destructive implication, and uncertain postcondition without executing.
Bare apply under the three-stream terminal contract discloses the same effect
plan and asks once. Non-interactive apply requires `--yes`. Every selected
supported config action and delegated route is ordinary apply work; there is no
separate route-attempt mode.

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

`--manage-existing` records exact-match unmanaged outputs as managed without
rewriting them. This changes future deletion authority, so it is never implicit
and never imports source material. It cannot transfer a path already owned by a
different managed subject, even when the current bytes match exactly.

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

`apply --dry-run --diff` emits one diff per physical managed file. A file with
one consumer reports singular `target`; a shared file reports the complete
canonical `targets` set and never invents a primary consumer. Diff collection
retains at most 4 MiB across the current and desired payload for one file and
16 MiB across the operation. It inspects at most 4,096 managed-path decisions
and reports the remaining decision count as one operation-level omission. A
file or operation that exceeds a content limit is reported with an explicit
omission instead of materializing an unbounded diff. Cancellation is checked
before each admitted decision and content read. Line cardinality is checked
before line arrays are allocated, and line content is canonicalized once before
the bounded LCS pass. Rendering admits at most 250,000 LCS cells per file and
16,000,000 cells across the report, checks cancellation between files, and
reports one aggregate count for textual diffs omitted after that work budget is
exhausted.

Apply also rejects any host mutation path that equals, contains, or is contained
by a local source consumed by the same manifest. Such an operation would mutate
its own locked input and could not remain reproducible on the next run.

Successful default output contains counts, up to three executed-subject
examples per successful action kind, every attempted-but-unverified or retained
residue class, failures, and a next action only when more work is needed.
`--verbose` adds state/content paths, reason codes, selected source/ref, and
bounded evidence. Raw subprocess output and secret values are never printed.
Diagnostic redaction observes both the captured spelling and the final
display-normalized spelling, so format or whitespace normalization cannot
create a newly visible credential form after inspection.

Status and apply-dry-run JSON use plan schema version `12`. The document contains
the derived lockfile status, lock-only resources, typed actions, delegated
actions, relation actions, physical extension-order actions, carrier-adoption
actions, carrier-absence actions, host-route attempt history, diagnostics, MCP
status dimensions, and `has_errors`. Each `relation_order_actions` row names
one independently mutable physical sequence, its logical class, desired and
observed managed members, foreign-row count, current revision, runtime meaning,
and any typed `foreign_precedence_change` risks. OpenCode rows describe config
order only; Pi rows describe runtime precedence. A carrier install or removal
that must settle first is reported as `conditional_after_carrier_change`
instead of an executable order mutation.

Relation-order `detail` fields contain path-neutral prose derived from their
typed reason or outcome; raw observation and execution evidence is available
only in verbose human output. Relation source namespaces, source references,
subject keys, order-member load identities, carrier source namespaces, and
carrier relation-subject keys that carry local or opaque host provenance are
replaced by deterministic
`redacted:sha256:<digest>` labels. Their corresponding `*_redacted` field is
set to `true` in JSON. Credential-free non-local source identities remain
exact only when the selected carrier grammar proves their package, marketplace,
or remote identity. A source-derived `carrier_subject.name` uses the same
projection and sets `name_redacted = true`; normalized project-relative sources
do not become public merely because their leading `./` was removed at
declaration ingress.

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

`apply --yes --json` uses result schema version `19`. It adds executed action
count, statefile path, bounded delegated and host-route attempt results, typed
errors, carrier-adoption transitions and final claim provenance,
carrier-absence outcomes, physical `relation_order_results`, and final
`has_errors`. Each order result reports target, scope, class, physical sequence,
`exact` / `converged` / `failed` / `not_attempted`, whether that sequence
changed, and a failure detail when present. An earlier OpenCode
document may remain converged when a later document fails; apply makes no
cross-document rollback claim. Retry reobserves every selected sequence and
continues idempotently from current files. Known mutation codes include
`stale_snapshot`, `stale_plan`, `mutation_contended`,
`mutation_cancelled`, `interrupted_apply`,
`interrupted_apply_file_set_fence`, `journal_cleanup_incomplete`,
`journal_cleanup_file_set_fence`, `interrupted_file_set_transaction`,
`file_set_evidence_invalid`, `abandoned_file_set_residue`,
`file_set_fence_census_limit`, and `file_set_access_unprovable`. A typed
`recovery_barrier` object on an apply error preserves each observed journal and
file-set axis independently; an unclassified peer is `unknown` rather than
hiding a known actionable axis. Active apply recovery and cleanup-only journal
authority remain distinct. A valid published
marker, markerless residue, and a bounded census limit are continuing file-set
fences; invalid evidence or unprovable StateDir access requires repair or
restore-access before apply or recover. These states are not `apply_refused`
and must not be repaired by deleting reserved names by prefix. Each error also
reports a closed `phase` and `outcome`;
its bounded message is derived only from those typed facts and never from an
internal error string. Outcomes distinguish work refused before effects,
incomplete effects, and effects that were fully rolled back before returning.
`rolled_back` applies only when every attempted apply effect is covered by the
completed compensation. A retained provider prerequisite or other effect
outside the managed journal keeps the operation `incomplete`, even when the
managed path portion was restored.
Default human output uses the same typed detail.
`--verbose` may add separately bounded and credential-sanitized causal
evidence. Planning, projection, diff, confirmation, diagnostics, and output
failures use the same closed apply boundary before an execution envelope exists.
Default remediation commands use a `<manifest>` placeholder; `--verbose` may
show the selected manifest path and exact bounded command evidence.

Host-route attempt rows include the exact operation and bounded
`effect_postconditions` requirement/state summaries when the locked route
couples additional removal effects. They contain no raw host output, secret,
machine-local path, or current authority.

Attempt records are historical diagnostics only. Process success does not prove
exact artifact identity, package/cache convergence, runtime readiness, trust,
contribution ownership, cleanup authority, or future skip authority.
For project-selected host routes, `workdir_authority` means the selected path no
longer named the retained physical project root around the attempt. The route
is never redirected to the replacement cwd, but it may already have started
from the captured root; apply therefore reports failure, makes no convergence
claim, and refuses to write the final attempt record through the replacement
root. Mechanical process facts remain independent: a timed-out command followed
by authority loss retains `timed_out = true` and `attempt_reason = "timeout"`
while the overall result reason is `workdir_authority`. This cwd binding is not
a sandbox for other host-command effects.
Delegate attempt rows use the same partition through `process_reason` and
`workdir_authority_failed`; timeout, cancellation, signal, and exit facts are
not overwritten by a later cwd-authority failure.

## `recover`

```bash
daem recover [--manifest <path>] [--dry-run|--yes] [--json|--verbose]
```

Recovery classifies and resolves one interrupted apply operation or finishes
the exact retained cleanup of an already retired apply journal. Bare recover
uses the shared three-stream terminal contract and asks after disclosing the
current recovery plan; non-interactive execution requires `--yes`. It does not
read desired resources from the manifest or lockfile: the manifest selects the
derived state/recovery paths. An interrupted published metadata-transaction
marker remains a separate protocol recovered by retrying the exact authoring
or `unmanage` write. Markerless private residue under the state directory is
not recoverable by retry; preserve it for analysis as described in
[troubleshooting](troubleshooting.md#manifest-metadata-update-was-interrupted).

| Classification | Meaning |
| --- | --- |
| `clean_before` | Host paths, state, and ownership are all at the pre-operation state; only journal cleanup remains. |
| `clean_after` | Host paths, state, and ownership all match the committed post-operation state; only journal cleanup remains. |
| `needs_rollback` | The interrupted operation can be restored to its pre-operation state from verified journal evidence. |
| `needs_finalize` | Host paths and state are committed, but prepared ownership claims still need finalization. |
| `retained_cleanup_residue` | The active journal is already retired; only its exact correlated retirement residue and control remain to be finalized. |
| `blocked` | Current evidence cannot be safely reconciled with either legal operation state. |

Active-journal recovery validates guarded host and statefile observations,
backup identity, candidate fingerprint, and the full lease set again before
writing. Cleanup-only recovery acquires and revalidates only the selected
recovery root, retirement control, residue, and final GC name; it does not read
or mutate host destinations, the statefile, ownership registry, manifest, or
lockfile. Both forms replan after acquiring authority. A prior dry-run grants
no execution authority. Blocked or stale recovery keeps the current evidence
and writes nothing.

Before any active journal is retired, the retirement gate reconciles every
persisted removal intent, including intents for entries outside a selected
recovery subset. The gate reports the logical destination and a typed
namespace, residue, cleanup-stage, or durability reason when reconciliation is
blocked or must be retried; it never exposes either private sibling path. A
clean visible classification alone is not a retirement guarantee. A retained
cleanup-stage directory may contain only the unremoved part of its original
tree; its exact preselected name records cleanup progress, while the original
whole-state hash remains mandatory before residue promotion.
An attempted promotion, cleanup, or absence confirmation that does not finish
is reported as `cleanup_action_failed` together with the selected action; raw
filesystem error text and private sibling paths remain internal.

For active recovery, default output shows classification, operation identity,
every action, destination/content subject, blocker, and recovery limitation.
It omits backup paths/hashes and journal layout; `--verbose` adds operation
directory, backup facts, reasons, and action detail. Cleanup-only output shows
`retained_cleanup_residue` with `finalize_journal_cleanup`, plus the operation
identity and no-host/state/ownership limitation. The disclosed cleanup plan and
action payload do not expose private control, residue, or GC paths, including
in verbose mode. Pre-1.0 `.daem-tombstone-<32 lowercase hex>` evidence is
blocked before a plan is disclosed; current daem does not inspect or migrate
it. Other names in that reserved namespace are blocked as malformed.

Recovery JSON schema version is `9` for both `--dry-run --json` and
`--yes --json`. Every result declares one `phase`. Dry-run results and write
attempts rejected before execution use `planned`. After execution begins, a
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

Cleanup execution failures use the same path-neutral semantic error in human
and JSON output. While fresh cleanup authority remains, the error names the
cleanup action and `phase=execution`; it does not include retirement paths or
wrapped filesystem errors. A garbage-collection failure occurs after semantic
retirement has committed, so its terminal error reports retired authority and
hidden GC residue without naming the former cleanup action. It remains a
command failure and JSON reports `phase = "authority_retired"`, but no recovery
action remains and later commands are not blocked by the private GC residue.

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
compatibility, and available passive readiness facts.

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

Codex supports only explicit-global marketplace relations. The selected
`PLUGIN@MARKETPLACE` relation authorizes
`codex plugin marketplace upgrade <marketplace> --json`, but the execution
subject is the marketplace, not the one plugin: the host may replace the Git
marketplace snapshot and refresh every configured installed sibling cache
sourced from it. The exact config-relation observer does not prove marketplace
snapshot or cache refresh convergence, so no-new-revision and changed-revision
successes are both `attempted_unverified`. Snapshot replacement and per-plugin
cache refresh may partially diverge; daem does not append `codex plugin add`,
substitute another marketplace, parse host JSON as convergence evidence, or
claim rollback. The host upgrade route applies only when the named marketplace
is upgrade-capable; for example, a local non-Git marketplace may produce a
started failed attempt. Daem reports that host refusal and does not reinterpret
it as another route.

OpenCode supports project and explicit-global host-source relations. It invokes
`opencode plugin <host-source> --force` from the selected project and appends
`--global` only for explicit global scope. The selected-config relation
observer does not prove package, version, or refresh convergence, so success is
only `attempted_unverified`. Package resolution, package/cache writes,
same-family config replacement and deduplication, multi-target config writes,
dependencies, activation, and runtime readiness remain host-owned. Ordinary
`apply` continues to use install/create without `--force`.

Pi supports project and explicit-global package relations, but both selections
invoke the same `pi update --extension <host-source>` command from the selected
project. Pi has no update scope flag: the host may inspect and update matching
user and trusted-project package rows with the same identity, so the selected
daem scope does not narrow the host mutation envelope. The selected-scope
relation observer does not prove package, version, or refresh convergence, so
success is `attempted_unverified`. Pinned npm sources may remain fixed,
local-path sources are live references with no scheduled updater, and Git
updates may reset and clean their checkout and install dependencies. Trust
refusal or no match remains a host failure. Daem never adds Pi approval,
self-update, model-update, or bulk update flags.

Antigravity CLI supports explicit-global plugin host-source relations by
repeating the exact locked `agy plugin install <host-source>` route. This is an
explicit repeat-install refresh, not a dedicated host update command. Bounded
local-source evidence shows bundle replacement without duplicate import rows
and rejection of malformed `plugin.json` before replacing the prior valid
bundle; remote and marketplace source resolution remains host-owned. There is
a passive global relation observer for safe `PLUGIN@MARKETPLACE` sources, but
it proves only the selected import row and matching installed bundle identity,
not version or bundle freshness. Explicit refresh success therefore remains
`attempted_unverified`. Other source forms retain unsupported observation.
Daem does not add import, link, enable, disable, project-scope, or Antigravity
IDE behavior, and it does not claim exact artifact freshness, rollback,
contribution inventory, or runtime readiness.

Ordinary confirmed apply can remove a daem-managed selector-shaped
Antigravity relation after fresh residual-state correlation and the
last-daem-known-consumer check. It invokes
`agy plugin uninstall <plugin>`, never the marketplace selector, and settles
only when both the selected import row and plugin directory are freshly absent.
Exit zero and success prose are not evidence. Partial or uncertain outcomes
retain claim and pending state for retry; already-absent state retires without
invocation. Opaque/local sources, marketplace/source setup, sibling plugins,
credentials, trust/session state, unrelated stores, IDE state, and ambient
non-daem consumers remain outside this guarantee.

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

The nested disclosure contains deterministic command/args, environment names
without values, selected-root cwd policy, timeout, effect and retained-effect
classes, and non-claims. `result.detail` is empty on success. For an error
class, it is derived only from the closed `reason_code`, process-outcome, and
relation-observation values already present in the result. It is never built by
sanitizing an underlying parser, filesystem, subprocess, or adapter error
string; those errors remain internal causes. Active apply recovery uses
`interrupted_apply`; cleanup-only authority uses `journal_cleanup_incomplete`.
Their joint continuing-fence forms use `interrupted_apply_file_set_fence` and
`journal_cleanup_file_set_fence`. A valid published marker uses
`interrupted_file_set_transaction`, markerless residue uses
`abandoned_file_set_residue`, bounded census exhaustion uses
`file_set_fence_census_limit`, and StateDir access or identity loss uses
`file_set_access_unprovable`. Invalid or incomplete published evidence uses
`file_set_evidence_invalid`. Cancellation outranks all of these during replan.
None is flattened to `stale_plan` or generic `mutation_authority`. Process and observation summaries
contain no subprocess output, raw errors, secret values, protocol payloads, or
machine-local paths. When recovery-barrier observation is relevant,
`result.recovery_barrier` preserves each observed journal and file-set axis;
an unclassified peer is `unknown` while the known peer continues to determine
the actionable `reason_code`. Human refresh failures use the same typed detail
instead of printing the underlying error.
`process_outcome.reason` describes only the mechanical command result.
`authority_outcome.workdir_failed` independently reports a failed post-attempt
cwd-authority check.

After a refresh result JSON document has been written to stdout, daem does not
append failure prose to stderr. Dry-run and pre-execution planning failures
therefore use one stdout result document and empty stderr. Confirmed
`--yes --json` execution still writes its complete authorization document to
stderr before effects; a later failure remains only in the final stdout result,
so the authorization stream stays one valid JSON document. CLI grammar and
other failures that occur before a result can be formed continue to use empty
stdout and a concise stderr diagnostic. A result-write failure is the only
post-result attempt that may fall back to a bounded stderr diagnostic.

Current target-specific route availability remains authoritative in the
feature matrix.

## Progress

Progress is automatic only for human output when stderr is a TTY and the
workflow exposes a meaningful long-running phase. It is suppressed for JSON,
non-TTY stderr, help, CLI misuse, and before interactive confirmation.

Current progress-capable workflows are lock/outdated source resolution, apply
execution, import discovery, and the delegated host-process phase of
`refresh extension`. Import dry runs report discovery; write mode also reports
freshness revalidation and publication. They render at most one ephemeral
stderr line, escape untrusted labels, and clear
the line before stable output or diagnostics. Refresh reports only the selected
extension and authorized timeout; it does not invent percentages or host
progress that the child process does not expose. Duplicate completion events
do not advance counts twice.

A progress write failure disables later progress but does not fail an otherwise
valid operation. A stable disclosure or confirmation-prompt write failure
blocks interactive execution. Any stable human or JSON output write failure
fails the command.

## Streams And Exit Codes

| Situation | Stdout | Stderr | Exit |
| --- | --- | --- | ---: |
| Help or successful human result | stable help/result | ephemeral progress or prompt only | `0` |
| Successful JSON result | exactly one JSON document | empty | `0` |
| Blocking result for a command whose result contract is failure | stable human or one JSON result | no duplicate prose | `1` |
| Report-only `status`, including pending or blocked rows | normal result | empty | `0` |
| Clean `status --check` | normal result | empty | `0` |
| Non-clean `status --check` | normal result | empty | `1` |
| Non-current `outdated --check` | normal result | empty | `1` |
| Warning-only result | stable result | empty | `0` |
| CLI misuse | empty | problem plus nearest correction | `2` |
| Failure before result envelope | empty | concise error and remediation | `1` |
| Failure after JSON envelope exists | one typed JSON result | empty unless write fails | `1` |
| Confirmation declined/canceled | disclosed plan remains | cancellation line | `1` |
| Stable output write failure | possibly partial | one bounded write diagnostic without the writer's error text | `1`, unless preserving an existing nonzero command identity |
| Interrupted by `SIGINT` | completed or partial stable output | bounded interruption diagnostic | `130` |
| Interrupted by `SIGTERM` | completed or partial stable output | bounded interruption diagnostic | `143` |

Stable confirmation disclosures are written to stdout; prompts and bounded
cancellation diagnostics are written to stderr. Human warnings that belong to
a valid result stay on stdout. Raw subprocess/protocol output, auth material,
headers, secret values, and unredacted credentials are prohibited from every
output mode.

The first `SIGINT` or `SIGTERM` records the process exit identity and cancels
the root operation. A subsequent signal arms a bounded emergency-exit deadline
while preserving the first signal's exit code; it does not bypass the
TERM-to-KILL window already cleaning daem-owned process groups. If normal
cancellation finishes first, daem returns immediately. Internal composition or
tests that cancel `RunWithOptions` through a context do not acquire an OS-signal
exit identity. To enable interactive authorization, those internal callers must
supply stdin, stdout, stderr, all three terminal facts, and a context-aware
`ReadConfirmationLine` capability. An invocation without those capabilities is
non-interactive.

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
