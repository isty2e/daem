# Manifest Schema

`daem.toml` is the desired-state boundary for managed agent environment resources. The parser is strict: unknown TOML keys are rejected, and every resource is normalized before lock or apply logic sees it.

This file is the authoritative public reference for the current implemented
manifest schema.

The current schema version is `1`.

For a minimal first-run starting point, see the
[example manifest](../examples/daem.toml).
For a larger local-source example, see the
[representative project manifest](../examples/representative-project.toml).

## Contents

- [Top-Level Fields](#top-level-fields)
- [Defaults](#defaults)
- [Sources](#sources)
- [Skills](#skills)
- [Skill Groups](#skill-groups)
- [Manifest Authoring Scenarios](#manifest-authoring-scenarios)
- [MCP Servers](#mcp-servers)
- [Hooks](#hooks)
- [Hook Assets](#hook-assets)
- [Instructions](#instructions)
- [Lock Behavior](#lock-behavior)
- [Complete Example](#complete-example)
- [Extension Carriers](#extension-carriers)
- [Current Non-Goals](#current-non-goals)

```toml
version = 1
targets = ["codex", "claude-code"]
```

Minimal valid manifest:

```toml
version = 1
targets = ["codex"]
```

## Top-Level Fields

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `version` | integer | yes | Must be `1`. |
| `targets` | array of strings | yes | Default target set for resources that omit `targets`. Must contain at least one target. |
| `defaults` | table | no | Default resource scope and install mode. |
| `instructions` | table of tables | no | Instruction resources keyed by instruction name. |
| `skill` | array of tables | no | Skill resources. |
| `skill_group` | array of tables | no | Explicit or selector-backed skill groups under one source root. |
| `hook` | array of tables | no | Hook resources. |
| `hook_asset` | table of tables | no | Source-backed executable file assets referenced explicitly from supported Codex and Claude Code hook commands. |
| `mcp_server` | array of tables | no | Narrow standalone stdio MCP server relations for supported target/scope slices: Codex project or explicit-global command/args-only scope, Claude Code project scope or explicit-global command/args-only scope, OpenCode strict project or explicit-global `type = "local"` command/args-only scope, and Antigravity CLI explicit global command/args-only scope. |
| `extension` | array of tables | no | Narrow host plugin/package carrier declaration for supported Codex explicit-global marketplace-selector rows, Claude Code project and explicit-global marketplace rows, OpenCode project/global host-source rows, Pi project/global package host-source rows, and Antigravity CLI explicit-global host-source rows. Codex project plugin scope is product `unsupported` with reason `host-unavailable` in the current native host route. Claude Code explicit-global plugin scope is public daem `scope = "global"` and projects to host `--scope user` only inside the supported delegated host route. Claude Code local plugin scope is product `deferred` with reason `not-modeled`. `add extension` and `remove extension` cover all five supported rows and update manifest plus lock only; `unmanage extension` releases exact daem management while retaining host state. Mutating `apply` may run only lifecycle routes supported for the exact target/scope/operation row. |

Unimplemented executable lifecycle declaration families such as `[[local_parameter]]`,
`[[package_runner]]`, `[[executable_artifact]]`, and
`command = { local_parameter = "..." }` are not public syntax and are rejected
before Desired normalization. Current
`[[mcp_server]]` `command` and `args` fields are launch-vector data for the
managed config projection; they are not provisioning, package installation,
runtime readiness, or cleanup syntax.

Supported target identifiers:

- `codex`
- `claude-code`
- `opencode`
- `pi`
- `antigravity-cli`

`antigravity-cli` currently supports project and global instruction file
rendering, Agent Skills-compatible directory packages through `[[skill]]` and
`[[skill_group]]`, plus explicit-global standalone stdio MCP config projection
through `[[mcp_server]]` when the entry uses only `command` and `args`.
Markdown slash-command skills, hooks, project-local MCP, remote MCP,
plugin-bundled MCP, plugin rows outside the explicit-global host-source carrier
slice, rules/workflows as separate surfaces, settings, runtime readiness, and
Antigravity IDE remain outside current product support. See
[Instructions](#instructions), [MCP Servers](#mcp-servers), and the
[Feature Support](features.md) for the supported slices.

For current target and resource coverage, see
[Feature Support](features.md). That page is a derived view of the current typed
target/resource surface registry. Future target support must extend that
registry and its invariant-bearing tests rather than adding a separate boolean
support matrix.

OpenCode, Pi, and Antigravity CLI are valid target identifiers. Their skill
resources reconcile through the shared Agent Skills directory surface.
Instruction rendering is supported for OpenCode project/global, Pi
project/global, and Antigravity CLI project/global scope as documented under
[Instructions](#instructions).
Antigravity CLI global instructions render to `~/.gemini/GEMINI.md` by default.
Command hook
configuration for OpenCode, Pi, and Antigravity CLI remains unavailable until
the relevant host schema, validation, discovery, ownership semantics, and
implementation gates are complete. Manually declared hook resources for
unsupported hook targets remain lock-only diagnostics; they do not create
managed hook projections, path-state ownership, or host mutations. `add hook`
rejects `antigravity-cli` because Antigravity CLI direct hooks are not supported
as a native command-hook surface. Codex and Claude Code command hook
configuration reconciliation is supported. Source-backed hook executable file
payloads are supported only through explicit `[hook_asset.<name>]`
declarations and same-scope `{hook_file:<name>}` placeholders for supported
Codex and Claude Code hooks.
`doctor` reports the same registry-derived target/resource capability matrix
as `target=<target> capability=<resource-kind>` checks.

Duplicate targets are rejected.

## Defaults

```toml
[defaults]
scope = "project"
install_mode = "copy"
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `scope` | string | `project` | Default install scope for skills, hooks, and instructions. |
| `install_mode` | string | `copy` | Default placement mode for skill resources. |

Supported `scope` values:

- `project`
- `global`

Supported `install_mode` values:

- `copy`
- `symlink`
- `hardlink`

## Sources

Skills and skill groups use a structured `source` object. Instruction
resources may use either a local file path string or a structured source object.
A source is Git-backed, local filesystem-backed, or S3 object-backed. Hook
declarations do not have a supported `source` object in the current product
contract; hook `command` values are opaque host commands, not managed executable
payloads.

Git source:

```toml
source = { git = "https://github.com/owner/repo.git", path = "skills/oracle", ref = "main" }
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `git` | string | yes | Admitted credential-free Git repository locator. |
| `path` | string | yes | Repository-relative path to export. Use `"."` only when the repository root is the intended source boundary. |
| `ref` | string | yes | Unqualified branch-or-tag name, qualified branch/tag, or full 40/64-hex commit id. |

Git locators admit `https://`, username-only `ssh://`, Git scp-like
`[user@]host:path`, absolute `file:///` URLs, and native absolute paths. HTTP,
the unauthenticated `git://` transport, remote-helper forms, relative repository
locators, URL query/fragment fields, HTTP userinfo, and URL passwords are
rejected. Authentication stays in the user's Git/SSH environment and is never
written to the manifest or lockfile.

All locator, repository-path, and ref values must be non-empty valid UTF-8 with
no surrounding whitespace, Unicode control character, or Unicode format
character. URL path text is checked again after percent decoding. A locator may
not begin with `-` or use a remote-helper `::` form. The supported locator forms
have these additional rules:

| Form | Required | Rejected |
| --- | --- | --- |
| `https://host/path` | Host and non-root repository path. | Userinfo, query, or fragment. |
| `ssh://[user@]host[:port]/path` | Host, non-root repository path, and an optional non-empty username. | Password, query, or fragment. |
| `[user@]host:path` | Non-empty host and path; no slash before the first colon. | Empty user/host/path, multiple `@` characters, whitespace, or an option-shaped host. |
| `file:///absolute/path` | Absolute decoded path. | Host, userinfo, query, or fragment. |
| Native absolute path | Absolute platform path, normalized with the platform path cleaner. | Relative paths and `~` expansion. |

An unqualified ref resolves only when exactly one matching branch or tag exists;
qualify collisions as `refs/heads/<name>` or `refs/tags/<name>`. Abbreviated
object ids, pseudo-refs, revision expressions, refspecs, and option-shaped refs
are rejected. Full object ids are exactly 40 or 64 hexadecimal characters.
Symbolic refs reject leading or trailing `/`, repeated `/`, `..`, `@{`,
whitespace, `~`, `^`, `:`, `?`, `*`, `[`, backslash, trailing `.`, path
components beginning with `.`, and path components ending with `.lock`.
Qualified refs are limited to `refs/heads/<name>` and `refs/tags/<name>`.

`path` is a POSIX path independent of the host filesystem. It must already be
clean, relative, and slash-separated; it may not be `..`, traverse above the
repository root, or contain a backslash. `.` is the canonical repository-root
path. These rules define the public Git source grammar and security boundary.

Git sources cannot set `mode`. For individual `[[skill]]` entries,
`path = "."` means the repository root itself is the skill artifact and must
contain an exact `SKILL.md` file. Because the whole root is hashed, unrelated
files at the repository root are also part of the locked content. Use a
subdirectory path when you want a narrower artifact boundary. For
`[[skill_group]]`, `source.path` is a source root that is expanded with explicit
`names` entries or lock-time `include` selectors before each child skill is
locked. Git `[[skill_group]]` sources may use `path = "."` when the repository
root itself contains direct child skill directories.

Git lock identity uses this exact key order:

```text
git:locator=<query-escaped-locator>&path=<query-escaped-path>&ref=<query-escaped-canonical-selector>
```

The canonical selector is `name:<name>`, `branch:<name>`, `tag:<name>`, or
`commit:<lowercase-full-object-id>`. The resolved commit remains separate in
`resolved_ref`, so a floating selector can advance without changing declaration
identity. Lockfiles created with the earlier delimiter-based Git `source_id`
format must be regenerated with `daem lock`; there is no legacy identity alias.

Local source:

```toml
source = { path = "/Users/me/.config/daem/daem.d/skills/local-review", mode = "vendor" }
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `path` | string | yes | Local filesystem path. Global local sources must be absolute. Project-scoped local sources may be relative to the manifest directory for portable project-vendored resources. |
| `mode` | string | yes | Local reproducibility mode. |

Supported local source modes:

- `vendor`: copy-like source identity for reproducible content hashing.
- `link`: local link source. Project-scoped local link skills must set `portable = false`.

Local sources cannot set `git`, `s3`, `ref`, `version_id`, `region`, or
`format`.
Global local sources represent host filesystem identity, so relative paths are
rejected for global instruction, skill, and skill group resources. Use Git or S3
for portable remote sources, or project-scoped local sources for repo-vendored
project artifacts.

S3 source:

```toml
source = { s3 = "s3://daem/skills/oracle.tar.gz", format = "tar.gz", version_id = "3Lg...", region = "us-east-1" }
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `s3` | string | yes | Single S3 object URI in `s3://bucket/key` form. |
| `format` | string | no | Object materialization format. Defaults to `file`. Supported values: `file`, `tar`, `tar.gz`, and alias `tgz`. |
| `version_id` | string | no | S3 object VersionId to request. |
| `region` | string | no | AWS region override for this source. If omitted, the AWS SDK default config chain supplies the region. |

S3 sources cannot set `git`, `path`, `ref`, or `mode`. The URI must identify a
single object, not a bucket or prefix directory. Query strings, fragments,
embedded credentials, empty object keys, and keys ending in `/` are rejected.
Credentials and profiles are intentionally external to the manifest and
lockfile. `daem lock` records the returned S3 VersionId as `resolved_ref`
when S3 returns one, and always records the materialized content hash.

### Source Resource Limits

Direct regular-file sources are limited to 128 MiB. The same limit applies to
local files, plain S3 `format = "file"` objects, instruction files, and hook
assets during source resolution, lock, status, and apply. Known filesystem or
S3 sizes are only early rejection evidence; daem also enforces the limit while
hashing, reading exact locked bytes, and streaming downloads. An oversized
source is rejected without truncation, cache completion, lock publication, or
host mutation.

Archive extraction has separate input, expansion, entry, and path budgets.
Those limits do not imply that a plain S3 object may use the larger archive
input allowance.

| Archive dimension | Limit |
| --- | ---: |
| Raw tar or compressed gzip input | 256 MiB |
| Decompressed tar stream | 768 MiB |
| Total extracted regular-file bytes | 512 MiB |
| One regular file | 128 MiB |
| Logical entries | 100,000 |
| Canonical path | 4,096 bytes and 64 components |

Known transport sizes and archive headers are early rejection evidence only.
Streaming readers and extraction accounting enforce the same limits. Limit
failure does not publish a source cache completion record, lockfile result, or
host mutation. Links, special files, traversal, backslashes, and parent path
segments in archives are rejected independently of these budgets.

## Skills

Skills are declared with `[[skill]]`.

```toml
[[skill]]
name = "oracle"
source = { git = "https://github.com/steipete/oracle.git", path = "skills/oracle", ref = "main" }
scope = "global"
compat_repair = true

[[skill]]
id = "codex_global_review"
name = "review"
source = { path = "/Users/me/.config/daem/daem.d/skills/local-review", mode = "vendor" }
targets = ["codex"]
scope = "global"
install_mode = "copy"
portable = false
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | none | Agent-visible skill directory and frontmatter name. |
| `id` | string | no | `name` | Stable daem resource id used by lockfile, status, state, and manifest editing commands. Use this only when multiple resources install under the same skill name. |
| `source` | source object | yes | none | Git source, local source, or S3 archive source. |
| `targets` | array of strings | no | top-level `targets` | Target hosts for this skill. |
| `scope` | string | no | `defaults.scope` | Install scope. |
| `install_mode` | string | no | `defaults.install_mode` | Placement mode. |
| `portable` | boolean | no | `true` | Whether the declaration is expected to be portable across machines. |
| `compat_repair` | boolean | no | `false` | Opt in to daem-defined mechanical skill compatibility repair during lock. |

Validation rules:

- `name` is required and must be usable as one safe directory component. Empty names, `.`, `..`, names starting with `~`, and names containing `/` or `\` are rejected.
- `id`, when present, must be usable as one safe resource id component and must be unique across normalized skill resources. When omitted, `name` is used as the resource id.
- Two skill resources may use the same `name` only when their target/scope destinations do not overlap and their `id` values are distinct. This permits target-specific resources such as Codex and Claude Code skills that both install as `review`, while still rejecting two Codex global skills that would both write `review`.
- `source` is required.
- S3 skill sources must use `format = "tar"` or `format = "tar.gz"`. S3 file
  objects are for instructions, not skill directories.
- `targets`, if present, must contain supported target values and no duplicates.
- Project-scoped local link skills must set `portable = false`.
- `compat_repair = true` permits only the replayable mechanical repairs
  defined in [Skill Compatibility](compatibility.md#repair-scope). Omitted or
  false means repair is not permitted.
- During lock generation, resolved skill sources must be directories containing
  a regular `SKILL.md`.
- During lock generation, target-specific skill metadata policy is enforced
  before any manifest-lock authoring transaction or host mutation completes.
  Codex, OpenCode, and Pi require `SKILL.md` frontmatter with `name` and
  `description`. OpenCode also requires `name` to match the installed directory
  name and to use its strict lowercase hyphenated naming form. Claude Code
  requires YAML frontmatter; `description` is treated as a `doctor` warning
  rather than a lock-blocking error because Claude Code documents the field as
  optional but important for automatic selection.

`daem add skill` can append a Git or local skill declaration, or merge new
targets into an existing matching `[[skill]]`. It updates the manifest and
selected lockfile together, but does not install payloads, apply host changes,
or mutate state. See the [CLI Reference](cli.md#add) and
[Manifest Authoring Scenarios](#manifest-authoring-scenarios).
`daem add skill-group` can append a compact `[[skill_group]]` when several
explicit skill names share one Git or local source root, target set, and scope.
`daem remove skill` removes a skill declaration, removes a `[[skill_group]]`
member, or removes selected targets from a skill declaration, then refreshes the
lockfile from the prospective manifest. Host deletion still requires explicit
`apply`.

Current skill reconciliation paths:

| Target | Project scope | Global scope |
| --- | --- | --- |
| `codex` | `.agents/skills/<name>` | `~/.agents/skills/<name>` |
| `claude-code` | `.claude/skills/<name>` | `~/.claude/skills/<name>` |
| `opencode` | `.opencode/skills/<name>` | `~/.config/opencode/skills/<name>` |
| `pi` | `.pi/skills/<name>` | `~/.pi/agent/skills/<name>` |

These are preferred install roots, not a complete discovery list. Import and
doctor do not infer write destinations from every directory a target can read.
Codex's documented Agent Skills authoring roots are `.agents/skills` and
`~/.agents/skills`; `CODEX_HOME` state may contain runtime skill material, but
that does not make `~/.codex/skills` the default `daem` write destination.
OpenCode and Pi also load compatible `.agents` or `.claude` skill roots, while
their native placement roots remain the paths shown above.

During `daem import`, modeled placement and discovery roots may both be
scanned, but only imported content becomes manifest-owned. Direct `.agent/skills`
source-pool scans are intentionally skipped; a symlinked skill reached through
a modeled agent-visible root is copied as a vendored local source. Supplied,
system, plugin, admin, and runtime roots are reported as skipped instead of
being converted into user-owned manifest resources.
When multiple imported skills have distinct names, no explicit `id`, the same
normalized target set, and the same scope, import may emit one `[[skill_group]]`
with a content-addressed local source root under `daem.d/skill-groups/<hash>`.
The copied group root contains one direct child directory per skill name, so
normal `skill_group` expansion still locks each skill separately. Divergent
same-name imports remain individual `[[skill]]` entries with explicit `id`
values.

`daem` copies the full skill directory to the direct child named by manifest
`skill.name`. The lockfile uses `skill.id` when present and otherwise
`skill.name` as the resource id. Frontmatter keys that are specific to one
target, such as Claude Code tool-permission fields, are not lock-blocking for
other targets, but `doctor` reports that they may be ignored by targets whose
skill metadata model does not recognize them.

This `name`/`id` split preserves practical same-name cases. If Codex and Claude
Code expose the same skill name with identical content, import emits one
multi-target `[[skill]]` with no explicit `id`. If they expose the same skill
name with different content, import emits entries such as
`id = "codex_global_review", name = "review"` and
`id = "claude_code_global_review", name = "review"`.

## Skill Groups

Skill groups are declared with `[[skill_group]]`. They keep one source root,
target set, scope, install mode, and portability setting for several child
skills. Each locked child is one generic `[[locked.subject]]` row. A
selector-backed child records the canonical identity of the `[[skill_group]]`
declaration that selected it; explicit groups normalize to ordinary Skills.

There are two group forms:

- Explicit groups set `names`. Manifest normalization expands every listed
  name into an ordinary per-skill resource before lock, status, or apply.
- Selector-backed groups set `include` and optional `exclude`. The group stays
  canonical until `daem lock`; lock resolves the source root, lists its direct
  child directories, applies selectors, validates each selected child as a
  skill artifact, and writes expanded Skill `locked.subject` entries.

```toml
[[skill_group]]
names = ["foo", "bar", "lorem", "ipsum"]
source = { git = "https://github.com/owner/skills.git", path = "skills", ref = "main" }
targets = ["codex", "claude-code"]
scope = "project"
install_mode = "copy"
portable = true
compat_repair = true
```

The shared `source.path` is a root directory. Each `names` entry is appended as
a direct child path:

| Group name | Expanded source path |
| --- | --- |
| `foo` | `skills/foo` |
| `bar` | `skills/bar` |

Local source groups expand the same way:

```toml
[[skill_group]]
names = ["local-review", "local-debug"]
source = { path = "skills", mode = "vendor" }
targets = ["codex"]
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `names` | array of strings | for explicit groups | none | Explicit skill names to expand. Mutually exclusive with `include`. |
| `include` | array of strings | for selector-backed groups | none | Selectors to expand at lock time. Mutually exclusive with `names`. |
| `exclude` | array of strings | no | none | Selectors removed from the included child set. Valid only with `include`. |
| `source` | source object | yes | none | Git or local source root. |
| `targets` | array of strings | no | top-level `targets` | Target hosts for each expanded skill. |
| `scope` | string | no | `defaults.scope` | Install scope for each expanded skill. |
| `install_mode` | string | no | `defaults.install_mode` | Placement mode for each expanded skill. |
| `portable` | boolean | no | `true` | Whether each expanded declaration is expected to be portable across machines. |
| `compat_repair` | boolean | no | `false` | Opt each expanded child skill into daem-defined mechanical compatibility repair during lock. |

Validation rules:

- A group must set exactly one of `names` or `include`.
- `names` and `include` must contain at least one entry when present.
- Every group name must be a safe single path segment. Empty names, `.`, `..`,
  names starting with `~`, and names containing `/` or `\` are rejected.
- Expanded group names share the same unique skill-name namespace as regular
  `[[skill]]` entries.
- Project-scoped local link groups must set `portable = false`.
- `compat_repair = true` is inherited by every expanded skill. Selector-backed
  groups record repair data on the expanded child lock entries, not on the
  selector itself.
- Selector-backed child lock entries record
  `skill_set_member.declaration_identity`. Together with the row's canonical
  `entity_id`, `subject_id`, and `exact_supply`, this is sufficient to
  reconstruct membership without relisting upstream source roots. Group array
  position is diagnostic syntax only and is never persisted as identity.
- S3 skill groups are unsupported. S3 remains a single object source, not a
  prefix-directory filesystem; list S3-backed skills individually as archive
  objects.
- Duplicate resource ids or duplicate target/scope/install-name destinations
  fail during lock validation before a lockfile is written.

Selector-backed groups use explicit selector expressions:

```toml
[[skill_group]]
include = ["glob:review-*", "regex:^lint-[a-z0-9_-]+$"]
exclude = ["glob:review-experimental-*"]
source = { git = "https://github.com/owner/skills.git", path = "skills", ref = "main" }
targets = ["codex"]
```

Current implementation boundary:

- The implemented selector-backed declaration spelling is `[[skill_group]]`.
  `[[skill_set]]` is not public syntax and is rejected by the current parser.
- Implemented selector kinds are `glob:` and `regex:`. Canonical future
  `name:` selectors and `names = [...]` normalization to exact `name:`
  selectors are not implemented yet.
- Current `lock` and `outdated` relist selector-backed skill-group source roots
  at lock-comparison time. `apply` and `status` do not inspect upstream source
  roots for new matches. Any future lock/update split requires an explicit
  documented migration; it must not be inferred from schema syntax.

Selector syntax:

- `glob:<pattern>` uses Go `path.Match` against direct child names. It supports
  `*`, `?`, and character classes such as `[abc]`. Escape literal glob
  metacharacters with bracket forms such as `[[]` for `[` and `[*]` for `*`.
- `regex:<pattern>` uses Go regular expressions against direct child names.
  Use `^` and `$` when exact-name matching is required.
- Selector patterns must match child names, not paths. `/` and `\` are
  rejected in glob patterns, and source roots are not recursively walked.
- Each `include` selector must match at least one direct child during `lock`.
  After `exclude` is applied, the final selected set must be non-empty.
  `exclude` selectors may match zero names.
- Expanded lockfile entries are ordered deterministically by skill resource
  name, then source identity.

`apply` and `status` reconstruct selector-backed children only from existing
Skill `locked.subject` rows whose declaration identity still matches the
current selector expression and other declaration facts. They do not inspect
upstream source roots. New source directories are invisible to `apply` and
`status` until `lock` or `outdated` reports them, while names removed by a
selector edit make the old correlation stale and require `lock` before normal
statefile-owned deletion planning can proceed.

`daem add skill-group` writes only explicit `names` groups from repeated
command-line `--member` values. Selector-backed groups are hand-authored TOML.

## Manifest Authoring Scenarios

Use `daem list resources` as the inventory step before editing an existing manifest:

```bash
daem list resources --manifest daem.toml
```

The output shows each resource key, installed skill name, targets, scope, and
whether a skill came from a `skill_group`. Use the resource key for `remove`.
For skills, the key is `id` when present and otherwise `name`.

For a same-name skill that has different content per target, keep the
agent-visible install name stable and give each declaration a distinct `id` in
TOML. Explicit resource ids are manifest-only because choosing long-lived
identity is not common CLI authoring:

```toml
[[skill]]
id = "codex_global_review"
name = "review"
source = { git = "https://github.com/acme/codex-review.git", path = "skills/review", ref = "main" }
targets = ["codex"]
scope = "global"

[[skill]]
id = "claude_code_global_review"
name = "review"
source = { git = "https://github.com/acme/claude-review.git", path = "skills/review", ref = "main" }
targets = ["claude-code"]
scope = "global"
```

Remove one of these by `id`, not by the shared install name:

```bash
daem remove skill codex_global_review --manifest daem.toml --dry-run --diff
```

When several skill directories share one source root, author a group instead
of repeating nearly identical `[[skill]]` entries:

```bash
daem add skill-group acme/agent-skills \
  --path skills \
  --ref main \
  --member review \
  --member debug \
  --member oracle \
  --target codex \
  --target claude-code \
  --scope project \
  --dry-run --diff
```

This writes one compact group:

```toml
[[skill_group]]
names = ["review", "debug", "oracle"]
source = { git = "https://github.com/acme/agent-skills.git", path = "skills", ref = "main" }
targets = ["codex", "claude-code"]
scope = "project"
```

Each group member still locks and applies as its own skill. Remove a whole
member through the same `remove skill` surface:

```bash
daem remove skill debug --manifest daem.toml --dry-run --diff
```

Removing only one target from one member of a multi-member `skill_group` is
intentionally rejected because the group has one shared target set. Split that
member into a standalone `[[skill]]` first, then remove the selected target.

Target selection uses one token per repeated flag. Comma-separated values are
rejected:

```bash
daem add skill acme/agent-skills/review --ref main --target codex --target claude-code --dry-run
```

For `add` and `remove`, `--scope` is a single resource scope or filter. Use two
declarations with distinct `id` values, or edit TOML directly, when the same
installed skill name must exist in both `project` and `global` scopes. `import`
is different: it may inspect multiple scopes in one command because it is
scanning live roots rather than declaring one resource.

A typical authoring loop is:

```bash
daem add skill-group acme/agent-skills --path skills --ref main --member review --member debug --target codex --dry-run --diff
daem add skill-group acme/agent-skills --path skills --ref main --member review --member debug --target codex
daem list resources --manifest daem.toml
daem apply --manifest daem.toml --dry-run
daem apply --manifest daem.toml --yes
```

Edit TOML directly when the desired declaration is outside the authoring
commands: S3 sources, broad comment-preserving edits, splitting a
group for per-member target changes, setting less common fields such as
`portable`, or changing resource identity deliberately. Direct edits are still
validated by `daem lock --dry-run`; they do not bypass the manifest, lockfile,
or apply contracts.

## MCP Servers

Standalone MCP servers are declared with `[[mcp_server]]`.

```toml
[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "project"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }
```

`[[mcp_server]]` is currently limited to seven standalone stdio
exact-projection slices:

- Codex project scope, rendered into project `.codex/config.toml` under
  `/mcp_servers/<name>` with `command` and `args` only.
- Codex explicit global scope, rendered into `~/.codex/config.toml` under
  `/mcp_servers/<name>` with `command` and `args` only.
- Claude Code project scope, rendered into project `.mcp.json` with `command`,
  `args`, and structured `env` references.
- Claude Code explicit global scope, rendered into top-level `~/.claude.json`
  under `/mcpServers/<name>` with `command` and `args` only.
- OpenCode project scope, rendered into strict project `opencode.json` as
  `type = "local"` with `command` and `args` only.
- OpenCode explicit global scope, rendered into strict default user
  `~/.config/opencode/opencode.json` as `type = "local"` with `command` and
  `args` only.
- Antigravity CLI explicit global scope, rendered into
  `~/.gemini/config/mcp_config.json` with `command` and `args` only.

Any standalone MCP row that writes shared user-level global host config must
put `scope = "global"` directly on that `[[mcp_server]]` block. A top-level
`[defaults].scope = "global"` value may default other resource families, but it
does not authorize global MCP config mutation.

For all supported rows, `command` and `args` are launch-vector data. They may be
rendered into host config and modeled as a non-owned executable requirement, but
they are not package installation, provisioning, runtime readiness, or cleanup
syntax.

Pi has no supported standalone `[[mcp_server]]` row. Current Pi MCP evidence is
extension/package-backed and must stay provider-scoped; daem does not flatten
Pi package or adapter MCP behavior into standalone MCP declarations. The same
non-flattening rule applies to plugin-bundled MCP, skills, hooks,
instructions, apps, commands, rules, and similar provider contributions: a
source-declared contribution may be reported only as a provider-scoped
diagnostic row with `provided_by`, kind/key, provenance, currentness, freshness,
artifact identity, and reason qualifiers unless a future import/adopt route
explicitly admits standalone ownership.

`daem import` may generate `[[mcp_server]]` declarations only for those same
supported standalone config rows. Import preserves accepted child-to-host env
bindings as references, skips unsupported or credential-bearing shapes, creates no source
files, and never edits host MCP config, starts servers, probes runtime
readiness, installs packages, or flattens plugin-bundled MCP contributions into
standalone resources.

OpenCode MCP requires effective `targets = ["opencode"]` and either effective
project scope or explicit global scope. `daem add mcp-server --target opencode`
writes an explicit project-scoped row; `--scope global` is required for the
default user config row. The OpenCode adapter writes only strict
`opencode.json` entries under `/mcp/<name>` as `type = "local"` with command
plus ordered args. It rejects `env`, custom/JSONC/remote config authority,
`cwd`, `enabled`, `timeout`, auth/session fields, tool-policy fields, and
unknown managed-entry
fields rather than silently preserving them inside the managed entry.

Codex MCP requires effective `targets = ["codex"]` and either effective project
scope or explicit row-local `scope = "global"`. `daem add mcp-server --target
codex` writes an explicit project-scoped row; `daem add mcp-server --target
codex --scope global` writes an explicit-global row. The Codex adapter writes
only `.codex/config.toml` or `~/.codex/config.toml` entries under
`/mcp_servers/<name>` with `command` and `args` only. It rejects `env`,
`env_vars`, custom/profile/system/admin/managed config roots, `cwd`, remote HTTP
config, auth/session fields, tool-policy fields, plugin-provided MCP, and
unknown managed-entry fields rather than silently preserving them inside the
managed entry.

Claude Code MCP requires effective `targets = ["claude-code"]` and either
effective project scope or explicit row-local `scope = "global"`.
Project-scoped rows render into project `.mcp.json` and may carry structured
`env` references. Explicit-global rows render only the top-level
`~/.claude.json` `/mcpServers/<name>` entry with `command` and `args` only.
`daem add mcp-server --target claude-code --scope global` writes the
explicit-global row. The Claude global adapter rejects `env`, local/project
scope state, OAuth/session/trust fields inside the managed entry, HTTP/remote
transport fields, headers, `cwd`, timeout/tool-policy fields, and unknown
managed-entry fields rather than silently preserving them inside the managed
entry. Unrelated top-level user config, project-local `projects` entries,
OAuth/session/trust siblings, and same-name project shadows are preserved but
not owned by the global row.

Antigravity CLI MCP requires `targets = ["antigravity-cli"]` and an explicit
`scope = "global"` on the `[[mcp_server]]` block. The Antigravity adapter
rejects `env`, `serverUrl`, `url`, `headers`, `oauth`, `authProviderType`,
`disabled`, `disabledTools`, `enabledTools`, `tools`, `cwd`, and unknown
managed-entry fields rather than silently preserving them inside the managed
entry.

`daem add mcp-server` and `daem remove mcp-server` are authoring helpers for the
supported Codex project row, Codex explicit-global row, Claude Code project row,
Claude Code explicit-global row, OpenCode project row, OpenCode explicit-global
row, and Antigravity CLI explicit-global row. Omitted target/scope succeeds only
when manifest inheritance and supported-row compatibility identify one row.
Claude global authoring requires
`--target claude-code --scope global` because defaults do not authorize global
MCP, and rejects `env` plus all remote/auth/tool-policy fields. Codex authoring requires
`--target codex` and rejects `env` plus env-vars/cwd/remote/auth/tool-policy
fields; Codex global authoring additionally requires `--scope global` because
defaults do not authorize global MCP. OpenCode authoring requires
`--target opencode`, permits project scope or explicit `--scope global`, and
rejects `env` plus custom/JSONC/remote/auth/tool-policy fields. Antigravity authoring requires `--target antigravity-cli --scope global`
because defaults do not authorize global MCP, and rejects `env` plus all
remote/auth/tool-policy fields. Authoring helpers update the manifest and
adjacent lockfile together; they do not write host config, start a server,
install packages, remove credentials, or change approval/trust state.
`remove mcp-server` removes the whole selected declaration block, and a later
explicit `apply` reconciles only the managed MCP projection. It does not delete
project executables, package-manager caches, daem store objects, credentials,
trust records, sessions, logs, or runtime state. `doctor` may passively report
ambient executable prerequisite diagnostics for selected supported MCP
declarations by checking command-token discoverability and modeled host-source
env names only; it does not execute the command or prove package/cache/runtime
convergence. Claude Code rows additionally lock a delegated executable plan
identity. That identity stores each exact child variable name together with its
host `from_env` source name; values remain runtime-only and are never locked.
Codex, OpenCode, and Antigravity CLI direct config projections have no
delegated executable claim in this slice. Codex has no runtime probe in this
slice. OpenCode runtime checks are
available only through the separate explicit `probe mcp-server --target
opencode --scope project` surface for locked project local-command rows.
Package-backed MCP commands may be floating or pinned;
`add mcp-server` warns for floating package identity, while lock records the
declared projection semantics without installing or probing the package.
Runtime MCP checks are a separate explicit surface: `daem probe mcp-server`
with `--dry-run` discloses the selected locked subject and side effects without
execution, while `--yes` may launch the exact locked stdio command and attempt
MCP initialize. That probe does not update the manifest, lockfile, statefile,
or host config. For the current stdio slice, endpoint health is
`not_applicable`, runtime authentication is `unsupported`, and tool inventory is
`unsupported`.

## Hooks

Hooks are declared with `[[hook]]`.

```toml
[[hook]]
name = "bd-prime-session"
event = "SessionStart"
matcher = "startup|resume"
type = "command"
command = "bd prime"
timeout = 30
status_message = "Preparing session"
targets = ["codex", "claude-code"]

[[hook.target_override]]
target = "claude-code"
matcher = "startup"
```

`[[hook.target_override]]` belongs to the most recent `[[hook]]` table,
following TOML array-of-table nesting rules.

Hooks manage native agent hook configuration only. The `command` string must
already be executable in the target host runtime environment. `daem` does
not fetch, install, hash, rewrite, or clean up hook scripts, helper directories,
binaries, plugin bundles, or trust records.
`daem add hook` and `daem remove hook` are authoring helpers for these command
declarations. They update the manifest and selected lockfile together; host
hook files change later through `apply`.
`status`, `apply --dry-run`, mutating `apply`, and manifest-aware `doctor`
surface warning-only diagnostics for selected supported hook commands when the
command has no explicit timeout, uses shell-like syntax, starts with a broad
interpreter, relies on host `PATH` lookup, or requires host trust review such as
Codex `/hooks`.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | none | Unique hook name. |
| `event` | string | yes | none | Lifecycle event name. Event values are not enumerated yet. |
| `matcher` | string | no | empty | Host-specific event matcher expression. |
| `type` | string | no | `command` | Hook handler type. |
| `command` | string | yes | none | Command to run. |
| `timeout` | integer | no | `0` | Timeout in seconds. `0` means no manifest-level timeout value. |
| `status_message` | string | no | empty | Optional status text. |
| `targets` | array of strings | no | top-level `targets` | Target hosts for this hook. |
| `scope` | string | no | `defaults.scope` | Install scope. |
| `target_override` | array of tables | no | none | Target-specific hook fields. |

Supported hook `type` values:

- `command`

Validation rules:

- `name`, `event`, and `command` are required.
- `name` must be unique across `[[hook]]` entries.
- `targets`, if present, must contain valid target identifiers and no
  duplicates.
- `[[hook.target_override]]` entries must reference a target declared for that hook.
- Duplicate target overrides for the same hook are rejected.
- Hook `command` values are rendered as configuration strings. The command
  itself is not a source-resolved artifact and does not create a lockfile
  entry.
- Source-backed hook executable file payloads are declared separately under
  `[hook_asset.<name>]`. `[[hook]]` has no `source` field. A hook command may
  reference a selected same-scope asset with `{hook_file:<name>}` for supported
  Codex and Claude Code hook targets.
- Command strings without `{hook_file:<name>}` placeholders remain opaque host
  command text. `daem` does not infer, copy, hash, install, rewrite, or remove
  executable files from command paths such as `python3 hooks/foo.py`, `./foo`,
  `/opt/foo`, or `npx foo`.
  See [Hook Assets](#hook-assets) for the explicit managed payload boundary.
- `add hook` validates Codex and Claude Code hook shapes that daem can render
  today. OpenCode and Pi hook targets are accepted with lock-only warnings
  because native hook reconciliation is not implemented for those targets.
  Antigravity CLI hook targets are rejected by `add hook`; edit the manifest
  manually only when an explicit lock-only diagnostic is the intended state.

Hook target override fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `target` | string | yes | Target this override applies to. |
| `if` | string | no | Optional target-specific condition. |
| `matcher` | string | no | Optional target-specific matcher. |

Target overrides are manifest-only. The common hook authoring surface keeps
event, command, matcher, timeout, target, and scope without exposing a nested
target-override mini-language.

Documented hook host paths:

| Target | Project scope | Global scope | Adapter status |
| --- | --- | --- | --- |
| `claude-code` | `.claude/settings.json` | `~/.claude/settings.json` | implemented |
| `codex` | `.codex/hooks.json` | `~/.codex/hooks.json` | implemented |

Codex and Claude Code hooks are rendered into the top-level `hooks` aggregate
in the target host file. Multiple manifest hook entries for the same scope and
target share one physical aggregate document, but each entry keeps its canonical
`hook/<manifest-name>` subject identity in lock, state, status, and progress.
There is no synthetic target/scope aggregate resource id. One physical document
read or write may therefore produce several subject-level status rows.

State ownership for supported hook aggregates is partial and subject-owned.
Each managed Hook row records its lossless aggregate contribution contract,
including the physical document address, `content_path = "/hooks"`, codec, and
canonical contribution. Current convergence always comes from a fresh codec
snapshot of `/hooks`, never from state alone or a whole-file hash. `apply`
preserves unrelated top-level keys; edits to those unrelated keys do not count
as managed hook drift. Existing unmanaged `hooks` entries conflict unless they
exactly match the rendered desired Hook set and the user runs
`apply --manage-existing`. Removing one Hook removes only that contribution;
removing the final managed contribution removes `/hooks`, and the now-empty host
file is removed only when no unrelated settings remain.

Codex hook reconciliation manages `content_path = "/hooks"` inside `hooks.json`
and rejects same-layer inline `[hooks]` in `config.toml` as unmanaged hook
content. Codex hooks written by `daem` are still non-managed Codex hooks,
so Codex may require the user to review and trust them through `/hooks` before
they run. The current support row is summarized under
[Command Hooks](host-integrations.md#command-hooks).

OpenCode and Pi hook targets also remain lock-only, but for a different reason:
their hook-like workflows are plugin or extension event bridges, not
declarative command hook files. Their product row is `diagnostic`; the reason
is `bridge-required`, and doctor exposes it through the detail `command hook
reconciliation requires an extension bridge surface`. The current support rows
are summarized under
[Command Hooks](host-integrations.md#command-hooks).

Antigravity CLI direct hook declarations remain lock-only because direct CLI
hook schema, merge, precedence, removal, and trust evidence are missing. `add
hook` rejects `antigravity-cli`; `remove hook` can still clean existing
manifest declarations without host mutation.

## Hook Assets

Hook assets are declared under `[hook_asset.<name>]`.

```toml
[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "guard"
event = "PreToolUse"
command = "{hook_file:guard} --check"
targets = ["codex", "claude-code"]
```

Fields:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| table key | string | yes | none | Stable asset identity used by lockfile, statefile, placeholders, and removal. |
| `source` | string or table | yes | none | Source location for the file bytes. A plain string is shorthand for a local source path. |
| `kind` | string | yes | none | Must be `file` in the current implementation slice. |
| `scope` | string | no | `defaults.scope` | Asset install scope. A hook may reference only an asset with the same effective scope. |
| `executable` | boolean | no | `false` | When true, the installed file is required to be executable. |

Current validation rules:

- `kind = "file"` is required. Directory assets, `entrypoint`,
  `{hook_dir:<name>}`, plugin-bundled hooks, standalone executable tools, and
  command path rewriting are not current product surfaces.
- A selected supported Codex or Claude Code hook may reference a selected
  same-scope hook asset with `{hook_file:<name>}`. Missing, malformed,
  cross-scope, or unsupported placeholders fail before host mutation.
- Lock records the hook asset source identity, artifact kind, content hash,
  executable flag, effective scope, declaration provenance, and managed-path
  exact permission mode (`0600` for non-executable files or `0700` for
  executable files).
- `status` and `apply` materialize only hook assets referenced by selected
  supported hook commands. An unreferenced hook asset may remain declared and
  locked without creating a host output.
- Removing the final selected hook reference removes the owned installed asset
  through normal statefile-owned deletion rules. Removing the declaration while
  a selected hook still references it is rejected before mutation.
- `apply --manage-existing` may record an unmanaged hook asset output only when
  the live file content and required file mode exactly match the locked desired
  output.
- Import remains command-config-only. It does not infer `[hook_asset.<name>]`
  declarations from absolute, relative, shell, or `PATH` command text.
- Runtime hook trust and approval remain host-owned. Codex may still require
  `/hooks` review before running a rendered command.

## Instructions

Instructions are declared under `[instructions.<name>]`.

```toml
[instructions.project]
source = "AGENTS.md"
targets = ["codex", "claude-code"]

[instructions.project.target.claude-code]
render_to = "CLAUDE.md"
mode = "copy"
```

S3 file object instructions use a structured source:

```toml
[instructions.project]
source = { s3 = "s3://daem/instructions/AGENTS.md", version_id = "3Lg...", region = "us-east-1" }
targets = ["codex"]
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `source` | string or source object | yes | none | Local source instruction file path, local source object, or S3 file object source. |
| `targets` | array of strings | no | top-level `targets` | Target hosts for this instruction resource. |
| `scope` | string | no | see below | Install scope. |
| `target` | table of tables | no | none | Target-specific rendering options. |

Instruction scope defaults:

- `[instructions.project]` defaults to `scope = "project"`.
- `[instructions.global]` defaults to `scope = "global"`.
- Other instruction names default to `defaults.scope`.

Instruction target rendering fields:

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `render_to` | string | no | target default | Target-specific supported instruction placement filename or path, relative to the target scope root. |
| `mode` | string | no | `copy` | Rendering mode for that target. |

Instruction output defaults:

- Codex project instructions render to `AGENTS.md`.
- Codex global instructions render to `~/.codex/AGENTS.md`.
- Claude Code project instructions render to `CLAUDE.md`.
- Claude Code global instructions render to `~/.claude/CLAUDE.md`.
- OpenCode project/global instructions render to `AGENTS.md` and
  `~/.config/opencode/AGENTS.md`.
- Pi project/global instructions render to `AGENTS.md` and
  `~/.pi/agent/AGENTS.md`.
- Antigravity CLI project instructions render to `AGENTS.md` by default.
  `render_to = "GEMINI.md"` is a supported project-scope alternate.
  Antigravity CLI global instructions render to `~/.gemini/GEMINI.md`.

When `render_to` is set, it must be a canonical slash-separated relative path
that resolves to a supported placement row for the selected target and scope.
It is not an arbitrary output path. Discovery-only and runtime-only instruction
rows are not write destinations. For project scope, placement is relative to
the selected project manifest root. For global scope, `render_to` is still
written relative to the target's global instruction root, for example
`AGENTS.md` rather than `~/.codex/AGENTS.md`. Absolute paths,
parent-directory traversal, backslashes, `~` expansion, and non-canonical forms
such as `./AGENTS.md` or `nested/../AGENTS.md` are rejected during instruction
output planning.

Recognized instruction rendering `mode` values:

- `copy`
- `symlink`

`copy` is the only mode currently executable by `apply --dry-run` and
`apply --yes`. It applies to supported instruction file targets and supported
skill directory targets. Skill directory placement is supported for Codex,
Claude Code, OpenCode, Pi, and Antigravity CLI. Instruction `symlink` and Skill
`symlink`/`hardlink` are accepted by manifest parsing so desired state can be
represented, but their executable actions are rejected before dry-run output
or host mutation until placement, recovery-journal, and statefile semantics are
implemented together.

Validation rules:

- `source` is required.
- Git instruction sources are not supported in the current product surface.
- Structured local instruction sources must use `mode = "vendor"`.
- S3 instruction sources must use `format = "file"` or omit `format`.
- `targets`, if present, must contain supported target values and no duplicates.
- `[instructions.<name>.target.<target>]` must reference a target declared for that instruction resource.

`daem add instruction` can append a local file instruction declaration or merge
new targets into an existing matching `[instructions.<name>]`. The authoring
command updates the manifest and selected lockfile together, but does not render
host instruction files, apply host changes, or mutate state. It accepts local
file sources only; S3 instruction file objects remain explicit manifest edits.
OpenCode, Pi, and Antigravity CLI project/global default instruction placements
can be selected through `--target`. Not-yet-implemented target/scope
combinations fail during prospective lock preflight before writing either file.
`daem remove instruction` removes an instruction declaration or selected
targets, including matching target-specific rendering tables, then refreshes
the lockfile from the prospective manifest. Host deletion still requires
explicit `apply`.

## Lock Behavior

`daem lock` reads the selected manifest, resolves lockable sources, and writes
`daem.lock.toml` next to that manifest. There is no independent public lockfile
selector. `daem add` and `daem remove` run the same prospective lockfile build
and write the manifest and lockfile as one authoring transaction; direct TOML
edits and imports still use `daem lock` as the explicit lock refresh step.

Current lock behavior:

- Project-scoped local skill and instruction sources are resolved relative to the manifest directory unless their paths are absolute. Global local skill and instruction sources must already be absolute.
- Git sources are resolved through the user's system `git`.
- Git refs are resolved to immutable commits and stored as `resolved_ref`.
- Individual Git skill source `path` values may name a skill directory or `path = "."` when the repository root itself is the skill artifact and contains exact `SKILL.md`. Git `[[skill_group]]` source roots may also use `path = "."`, but there it means the root whose selected direct children are locked as separate skill artifacts.
- Git repository paths are exported from the resolved commit. Archive entries that escape the artifact directory or resolve to links are rejected.
- S3 sources are resolved through the AWS SDK default config chain, with
  per-source `region` as an optional override.
- S3 VersionId is stored as `resolved_ref` when the service returns one. The
  materialized artifact `content_hash` remains the integrity check whether or
  not bucket versioning is enabled.
- An S3 source with an explicit `version_id` may reuse a persistent cache entry
  before AWS client creation only after both its exact source/version lookup
  record and referenced artifact bytes pass local cache verification. A source
  without `version_id` is fetched again on each sequential resolution; an ETag
  never grants immutable-cache identity.
- S3 archive extraction rejects path traversal, symlinks, hardlinks, and special
  files. S3 prefix directory sources are unsupported.
- Skill sources are validated as directories with a regular `SKILL.md`.
- Supported command-only hook declarations are not represented in the lockfile.
  Their rendered host aggregate content is planned and state-tracked separately
  from source resolution.
- Instruction sources are locked as file sources and must resolve to regular files.
- Selector-backed `[[skill_group]]` entries are expanded during lock. The
  generated dry-run delta reports selected child additions, removals, and
  content changes as ordinary per-skill lockfile entry changes.
- Every lockable resource is encoded as a canonical `[[locked.subject]]` row
  ordered by `entity_id` and `subject_id`. Selector-backed Skill children carry
  `skill_set_member.declaration_identity`; direct Skills carry no declaration
  provenance facet.
- Each Skill locks one exact-Supply resource subject and one managed-path
  projection subject per distinct physical placement. Targets sharing the same
  profile placement coalesce into one projection with canonical
  `consumer_targets`; the projection also records scope, portable destination,
  content kind, placement mode, permission policy, the explicit complete
  permission bits when that policy is `exact`, and adapter contract version.
  An explicit exact mode `0000` is distinct from an absent mode. The projection
  does not record a primary target, current filesystem state, or
  machine-expanded path.
- Each Instructions resource locks one exact-Supply resource subject with an
  exact non-executable file-use contract and deterministic file materialization,
  plus one managed-file projection subject per distinct supported placement.
  Targets sharing a physical file coalesce into one canonical consumer set.
  A schema-v3 Instructions Supply without at least one structurally valid file
  projection is rejected; apply/status additionally require the lock projection
  set to equal the current manifest/profile refinement.
- Existing lockfile entries that are no longer declared by the manifest are removed by normal lockfile regeneration.
- Lockfile rows are sorted by canonical `entity_id`, then `subject_id`.
- Lockfile readers accept only schema version 3 and reject unsupported versions,
  invalid UTF-8, unknown keys, incompatible TOML table shapes, duplicate subject
  identities, zero-facet subjects, unknown realization variants, invalid
  cross-facet correlation, unsupported exact-Supply family shapes, malformed
  exact identities or operation contracts, an `exact` managed-path projection
  without `exact_permission_mode`, and persisted values that would need
  trimming, sorting, or deduplication to become canonical.
- Schema version 3 does not admit `generated_at`; readers reject it as an
  unknown key rather than treating timestamp metadata as lock authority.
- Existing lockfiles are not replaced when lock generation fails.
- Resolver cache artifacts live under the selected source cache, but cache paths are not serialized into the lockfile.

Lockfile exactness does not delete host outputs. If a manifest resource is removed, `daem lock` removes the stale lockfile entry, but any previously rendered host file remains governed by the selected statefile, live drift checks, and `apply` reconciliation.

During apply, a local source is immutable input authority for that operation.
Daem rejects a host mutation whose physical path equals, contains, or is
contained by any local source consumed by the manifest; it does not delete or
rewrite an input and then claim convergence from the prior lock.

## Complete Example

```toml
version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "project"
install_mode = "copy"

[instructions.project]
source = "AGENTS.md"
targets = ["codex", "claude-code"]

[instructions.project.target.claude-code]
render_to = "CLAUDE.md"
mode = "copy"

[[skill]]
name = "oracle"
source = { git = "https://github.com/steipete/oracle.git", path = "skills/oracle", ref = "main" }
scope = "global"

[[skill]]
name = "local-review"
source = { path = "skills/local-review", mode = "vendor" }
targets = ["codex"]

[[skill_group]]
names = ["foo", "bar"]
source = { git = "https://github.com/example/skills.git", path = "skills", ref = "main" }
targets = ["codex", "claude-code"]

[[hook]]
name = "bd-prime-session"
event = "SessionStart"
matcher = "startup|resume"
type = "command"
command = "bd prime"
timeout = 30

[[hook.target_override]]
target = "claude-code"
matcher = "startup"
```

## Extension Carriers

`[[extension]]` currently admits five narrow host carrier relation rows: Claude
Code project or explicit-global marketplace plugins, Codex explicit-global
marketplace selectors, OpenCode project/global host-native plugin sources, Pi
project/global host-native package sources, and Antigravity CLI explicit-global
host-native plugin sources. Broader carrier families, targets, scopes, and
source forms remain future rows. Codex project-scoped plugin install is product
`unsupported` with reason `host-unavailable` in the current native Codex plugin
route. Claude Code explicit-global syntax is public `scope = "global"` and
projects to host `--scope user` only inside the supported delegated lifecycle
route. Claude Code `local` plugin scope is product `deferred` with reason
`not-modeled`.

Refresh is an operation over this declaration and its exact current lock
contract, not another manifest field. `daem refresh extension <id>` selects one
supported row explicitly and never rewrites manifest or lock bytes. Host,
scope, evidence strength, and broader-effect details are listed in the
[Host Integration Contract](host-integrations.md#explicit-carrier-refresh) and
[CLI Reference](cli.md#refresh-extension).

```toml
[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "global"
source = { marketplace = "context7@market" }

[[extension]]
id = "documents-managed"
carrier = "codex-plugin"
targets = ["codex"]
scope = "global"
source = { marketplace = "documents@openai-primary-runtime" }

[[extension]]
id = "formatter-managed"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "global"
source = { host_source = "@acme/opencode-formatter" }

[[extension]]
id = "tools-managed"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "github:acme/pi-tools" }

[[extension]]
id = "guidance-managed"
carrier = "antigravity-cli-plugin"
targets = ["antigravity-cli"]
scope = "global"
source = { host_source = "modern-web-guidance@google" }
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `id` | string | yes | none | Stable daem declaration id and managed-instance key component. |
| `carrier` | string | yes | none | Must be `claude-code-plugin`, `codex-plugin`, `opencode-plugin`, `pi-package`, or `antigravity-cli-plugin` in the current implementation. |
| `targets` | array of strings | no | top-level `targets` | Effective target set must be exactly `["claude-code"]` for `claude-code-plugin`, `["codex"]` for `codex-plugin`, `["opencode"]` for `opencode-plugin`, `["pi"]` for `pi-package`, or `["antigravity-cli"]` for `antigravity-cli-plugin`; broad inherited targets are rejected. |
| `scope` | string | no | carrier-specific | Must be `project` or explicit `global` for `claude-code-plugin` in the current implementation. Public `scope = "global"` projects to host `--scope user` only inside the supported delegated host route; public `scope = "user"` is rejected, and Claude Code `local` is product `deferred` with reason `not-modeled`. Defaults do not authorize Claude Code global host mutation. Must be explicit `global` for `codex-plugin` and `antigravity-cli-plugin`; Codex project plugin scope is product `unsupported` with reason `host-unavailable` for the current native host route. Must be `project` or explicit `global` for `opencode-plugin` and `pi-package`; defaults do not authorize global host mutation. Other carrier scopes are unsupported. |
| `source` | table | yes | none | Must be `{ marketplace = "<plugin>@<marketplace>" }` for Claude Code and Codex, or `{ host_source = "<host-native-source>" }` for OpenCode/Pi/Antigravity CLI. Claude uses this canonical selector as both host argv and the installed-inventory key; bare plugin names are rejected. The selected value is passed as one structured argv element and must not begin with `-`; marketplace and host_source are mutually exclusive. URL passwords, HTTP userinfo, all query fields, assignment-style fragments, other inline secrets, and raw host config are rejected before lock creation. Inert source fragments such as `#v1` remain valid. |

Pi local package sources are context-resolved before lock creation without
requiring the path to exist and without following symlinks. Native paths,
`file://` URLs, dot segments, and `~` therefore collapse to one lexical path
identity. Project scope records the clean manifest-root-relative spelling;
global scope records the clean absolute spelling so two manifests cannot claim
the same global local package through different aliases. Two same-scope
declarations that collapse to one identity are rejected. This lock
normalization does not make a differently stored external Pi settings row
source-exact or adoptable.

During lock, the admitted row lowers to a `host_relation` lock subject with a
delegated host plugin carrier route request identity and operation contracts.
`status` and `apply --dry-run` report plugin carrier relation facts from
selected locked subjects. Those rows disclose the locked subject, target, scope,
route request identity, route-admission row, passive relation evidence
source/availability/freshness, replay boundary, retained effects, and explicit
non-claims. For selected Claude Code rows, status and apply obtain current
relation evidence by statically reading version-2
`<CLAUDE_CONFIG_DIR>/plugins/installed_plugins.json`, falling back to
`~/.claude/plugins/installed_plugins.json`. Project rows are filtered by the
canonical selected manifest root and host `projectPath`; explicit-global rows
correlate only with host `scope = "user"`. Missing inventory is fresh empty
evidence, while malformed, unsupported-version, ambiguous, or invalid selected
rows fail closed. The observer does not inspect plugin bundles, enabled settings,
trust, readiness, or contributions and never invokes `claude`.

Mutating `apply --yes` may invoke the supported host-delegated
install/create route. Claude Code requires fresh observed absence and then runs
`claude plugin install <plugin>@<marketplace> --scope project` for project rows,
`claude plugin install <plugin>@<marketplace> --scope user` for Claude Code
explicit-global rows, `codex plugin add <plugin>@<marketplace> --json` for
Codex, `opencode plugin <host-source>` with
`--global` only for global OpenCode scope, `pi install <host-source>` with `-l`
only for project Pi scope, or `agy plugin install <host-source>` for Antigravity
CLI explicit-global scope. Codex, Claude Code, OpenCode, and Pi require fresh
exact selected-scope absence before invoking their install route and create
removal authority only after fresh exact presence. Selector-shaped
`PLUGIN@MARKETPLACE` Antigravity sources require fresh complete-pair absence
before invocation, then combine the exact pending install identity with fresh
bounded plugin-name/import/bundle presence. The Antigravity evidence remains
source-inexact and does not independently establish marketplace provenance.
Codex correlates the exact
`[plugins."PLUGIN@MARKETPLACE"]` row in `$CODEX_HOME/config.toml`; cache and
marketplace visibility are not relation evidence. OpenCode correlates the exact
host-source row across the selected server and TUI JSON or JSONC documents,
preferring `.json` over `.jsonc` per document kind. For selector-shaped
Antigravity sources, daem correlates the plugin name across
`~/.gemini/config/import_manifest.json` and
`~/.gemini/config/plugins/<plugin>/plugin.json`; both must be consistently
present and identity-matching. Malformed, duplicated, mismatched, partial,
unstable, or symlinked selected state blocks. Other Antigravity source forms
retain the no-observer attempt posture: status/dry-run preserve unsupported
evidence rather than calling it missing, and mutating apply retries the route.
Because the host drops marketplace provenance, lock rejects distinct
Antigravity structural sources that collapse to the same host-visible plugin
name; declarations sharing one identical structural carrier remain valid.
Attempt records remain
history-only diagnostics and never grant future skip authority. Before an
supported host command runs, apply durably records one exact lock-bound pending
carrier install. Fresh exact observed presence promotes it to a managed carrier
claim for source-exact hosts. Selector-shaped Antigravity rows instead require
fresh bounded complete-pair presence, with the exact pending fact retaining the
source and route identity that passive evidence cannot prove. Project claims
live in that manifest's statefile; global claims commit first to the shared
global registry and then retire the project-state pending fact. A normally
completed invocation that does not establish a claim retires its pending fact,
including failed, observed-absent, and attempted-unverified outcomes. Pending
may survive an interrupted invocation or lost state authority; a later fresh
route-supported presence observation can complete that interrupted correlation.
Rows with supported observers later reread current host inventory; a no-observer
row retries while observation remains unsupported. Pending state alone never
skips or grants destructive authority. Managed carrier claims are created from
daem install transitions with the required current postcondition evidence, or
from explicit state-only adoption of an already present source-exact relation
through `apply --manage-existing`.
Either claim kind records relation provenance, not exact artifact state, and
does not prove enabled, trusted, ready, exact convergence, package/cache, or
contribution-inventory state. The Antigravity observer cannot recover
marketplace provenance from host state and does not claim it, artifact
freshness, bundled contributions, trust, runtime readiness, or ambient
non-daem consumers.
Mutating apply holds one complete cross-process
lease set across logical inputs, state/recovery paths, shared destinations, and
conservative host-route surfaces. It rebuilds and revalidates the candidate
plan under those leases before any state, journal, host, route, or delegate
effect, so shared routes and destinations also serialize across different
manifests.
Explicit `refresh extension` uses its own locked operation contract and is
never selected by ordinary apply. Managed-relation removal, unmanage, prune,
runtime probes, and plugin-bundled contribution import remain separate
operation rows. Unmanage is executable and host preserving; target-specific
host removal routes remain governed by the feature matrix.

For Claude Code project and explicit-global rows, exact desired absence backed
by a durable managed claim may invoke
`claude plugin uninstall <plugin>@<marketplace> --scope project --keep-data` or
the corresponding `--scope user --keep-data` host route. Apply first requires
fresh exact selected-relation presence and no remaining daem-known consumer of
the structural carrier. It retires removal authority only after a fresh
observation proves that exact relation absent. Command success, failure, or
timeout alone is not convergence. The route retains marketplace declarations,
versioned or orphaned caches, host metadata, plugin data, dependencies,
credentials, trust/session state, siblings, and unrelated resources; it does
not prune residue, remove individual bundled contributions, prove ambient
global consumers, or claim runtime unload/readiness.

For the Codex explicit-global row, exact desired absence backed by a durable
managed claim may invoke
`codex plugin remove <plugin>@<marketplace> --json`. Apply requires zero
remaining daem-known consumers and verifies two separate current facts after
execution: the exact selected config relation is absent and
`$CODEX_HOME/plugins/cache/<marketplace>/<plugin>` is absent. Codex removes the
cache before rewriting config, so partial or uncertain outcomes retain the
claim and write-ahead pending removal until fresh observation proves both
facts. A present config relation with a missing cache still invokes the exact
route. If the config relation is already absent before a pending removal is
created, apply retires the eligible claim without invocation and leaves any
orphan cache outside implicit prune authority. Marketplace declarations and
snapshots, sibling and same-name-other-marketplace relations and caches,
credentials, trust/session state, unrelated config, and external stores are
retained. Daem does not prove ambient non-daem consumers or runtime
unload/readiness.

For OpenCode project and explicit-global rows, exact desired absence backed by
a durable managed claim removes only the exact host-source row from the
selected server and TUI config documents. This is direct structured-config
actuation, not an `opencode` host command: `opencode uninstall` would remove the
host program. Each selected file uses bounded JSONC parsing, retained-root
authority, and compare-and-swap replacement while preserving comments,
whitespace, tuple options, sibling rows, unknown fields, empty arrays, and the
config file itself. A partial multi-file success retains durable pending state;
retry treats already-absent rows as no-ops and continues the remaining files.
The managed claim retires only after fresh absence from every selected
document. The other scope, package-manager installations, `node_modules`,
lockfiles, caches, local source directories, data, credentials, sessions,
runtime activation, and unrelated config remain untouched. Default-global
removal cannot prove ambient non-daem consumers, and custom
`OPENCODE_CONFIG`/`OPENCODE_CONFIG_DIR`, managed/remote layers, and non-selected
shadow files are outside this route.

For an Antigravity CLI explicit-global row whose locked
`source.host_source` is a safe `PLUGIN@MARKETPLACE` selector, exact desired
absence backed by a durable managed claim may invoke
`agy plugin uninstall <plugin>`. Apply requires fresh selected residual-state
correlation and zero remaining daem-known consumers. It passes the host-visible
plugin name, never the marketplace selector, and does not trust exit zero or
success prose. Claim and write-ahead pending authority retire only after fresh
observation proves both the selected import-manifest row and
`~/.gemini/config/plugins/<plugin>` absent. Partial state remains live for
retry; an already-absent pair retires eligible authority without invocation.
Sibling plugins and import rows, credentials, trust/session state, unrelated
stores, marketplace/source setup, and Antigravity IDE state are retained.
Marketplace provenance and ambient non-daem consumers remain non-claims.
Opaque and local Antigravity host sources do not receive removal authority.

`daem add extension <id> <source>` and `daem remove extension <id>` are
authoring helpers for all five supported rows. Add accepts one opaque
carrier-native source operand; selected-target validation determines its
marketplace or host-native interpretation. Omitted target succeeds only when
manifest targets and source compatibility identify one supported row. Codex and
Antigravity CLI require explicit global scope and OpenCode/Pi default to
project. Remove selects the globally unique declaration id; optional target and
scope are safety filters and do not inherit add defaults. These helpers update
manifest and lockfile only. Removing a row expresses desired absence; manual
TOML omission followed by `lock` expresses the same state. Authoring never
invokes a host extension command. A later mutating `apply` may invoke only an
supported install/create or managed-relation removal route backed by durable
exact management authority and fresh route evidence. The explicit
`daem unmanage extension <id>` operation releases daem management while
retaining host state. Neither desired absence nor unmanage grants prune,
credential, trust, session, data, or contribution authority. Plugin-bundled
contributions remain provider-scoped facts, not standalone `[[mcp_server]]`,
`[[skill]]`, `[[hook]]`, instruction, command, rule, or app declarations.
`daem import` does not import extension carriers in the current product slice.
External carrier adoption is an `apply --manage-existing` state-only claim
transition and requires no additional manifest field. It requires a declared
and locked relation, fresh source-exact passive correlation independent of
pending/claim state, no claim conflict, and fully supported install and removal
lifecycle contracts. It invokes no host route. Current support is limited to
the source-exact Claude Code, Codex, OpenCode, and exact-spelling Pi rows in the
[Host Integration Contract](host-integrations.md#external-carrier-adoption);
Antigravity CLI remains source-inexact. Carrier relation visibility alone is
still not adoption authority.

Directly authored and CLI-authored extension rows lower to identical lock,
status, and apply facts. Authoring support alone does not admit a host removal
route: generic managed-absence planning and host-preserving unmanage are
current; Codex plugin rows additionally admit exact explicit-global managed
removal, Claude Code and Pi package rows admit exact project/global host-route
removal, and OpenCode plugin rows admit exact project/global direct
config-relation removal. Selector-shaped Antigravity CLI rows admit exact
explicit-global host-route removal. Other target-specific removal,
external-store prune, runtime readiness, current contribution inventory, and
bundled contribution import remain independently matrix-controlled.

## Current Non-Goals

The manifest schema already models targets, scopes, instructions, skills,
hooks, and the supported MCP exact-projection relations, but
some product surfaces and downstream actions are intentionally not implemented
yet:

- MCP server declarations beyond the supported Codex project and explicit-global
  command/args-only slices, Claude Code project stdio and explicit-global
  command/args-only slices, OpenCode project and explicit-global
  command/args-only slices, and Antigravity CLI explicit-global
  command/args-only slice. The
  Claude slice renders one standalone server relation into the project
  `.mcp.json` config, locks its delegated executable plan identity, and reports
  config convergence,
  passive MCP executable prerequisite diagnostics, and last delegate attempt
  diagnostics as separate dimensions. The Claude global slice renders one
  standalone server relation into top-level `~/.claude.json`, locks the
  command/args config projection, may report passive ambient executable
  prerequisites, and has no delegated executable or runtime readiness claim.
  The Codex slices render one standalone
  server relation into project `.codex/config.toml` or default user
  `~/.codex/config.toml`, lock the command/args config projection, may report
  passive ambient executable prerequisites, and have no delegated executable or
  runtime readiness claim.
  The OpenCode slices render one standalone server relation into project
  `opencode.json` or default user `~/.config/opencode/opencode.json`, lock the
  command/args config projection, may report passive ambient executable
  prerequisites, and have no delegated executable claim. Runtime probe support
  remains limited to the separate explicit project-scope
  `probe mcp-server --target opencode --scope project` launch+initialize check
  with no state, lock, or host config mutation. The Antigravity slice renders
  one standalone server relation into
  `~/.gemini/config/mcp_config.json`, locks the command/args config projection,
  may report passive ambient executable prerequisites, and has no delegated
  executable or runtime readiness claim. Runtime startup, package/cache
  ownership, credential availability, approval/trust state, endpoint health,
  tool inventory, tool policy, and broader host config ownership remain outside
  the managed aggregate contribution contract.
Minimal manifests are available at
[`examples/claude-project-mcp-stdio.toml`](../examples/claude-project-mcp-stdio.toml),
[`examples/claude-global-mcp-stdio.toml`](../examples/claude-global-mcp-stdio.toml),
[`examples/codex-project-mcp-stdio.toml`](../examples/codex-project-mcp-stdio.toml),
[`examples/codex-global-mcp-stdio.toml`](../examples/codex-global-mcp-stdio.toml),
[`examples/opencode-project-mcp-stdio.toml`](../examples/opencode-project-mcp-stdio.toml),
[`examples/opencode-global-mcp-stdio.toml`](../examples/opencode-global-mcp-stdio.toml),
and
[`examples/antigravity-global-mcp-stdio.toml`](../examples/antigravity-global-mcp-stdio.toml).

- Hook asset directory payloads, plugin-bundled hook installation, standalone
  executable installation, and command path rewriting. [Hook Assets](#hook-assets)
  documents the narrower supported regular-file payload boundary.
- Public `[[extension]]` carrier declarations outside the supported Codex global
  marketplace-selector, Claude Code project or explicit-global marketplace,
  OpenCode host-source, Pi package host-source, and Antigravity CLI explicit-global host-source
  slices. Codex currently admits only
  `carrier = "codex-plugin"`, target `codex`, explicit `scope = "global"`,
  and `source.marketplace = "<plugin>@<marketplace>"`; Codex project plugin
  scope is product `unsupported` with reason `host-unavailable` in the current
  native route. Claude Code currently
  admits only `carrier = "claude-code-plugin"`, target `claude-code`, project
  or explicit-global scope, and `source.marketplace`; public explicit-global
  Claude rows map to host `--scope user`, while public `scope = "user"` is
  rejected and Claude `local` remains product `deferred` with reason
  `not-modeled`. OpenCode
  currently admits only `carrier = "opencode-plugin"`, target `opencode`,
  project or explicit-global scope, and `source.host_source`; Pi currently admits only
  `carrier = "pi-package"`, target `pi`, project or explicit-global scope, and
  `source.host_source`; Antigravity CLI currently admits only
  `carrier = "antigravity-cli-plugin"`, target `antigravity-cli`, explicit
  `scope = "global"`, and `source.host_source`. Pi direct extension
  `carrier = "pi-extension"`, Antigravity project-scope plugin rows, and
  Antigravity import/link source-provenance rows are not public manifest syntax.
- Symlink placement in mutating apply.
- OpenCode, Pi, or Antigravity CLI hook rendering semantics.
