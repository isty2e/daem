# Manifest Schema

`daem.toml` declares the resources daem should manage. This is the authoritative
public schema reference; the current schema version is `1`. Unknown TOML keys
are rejected, and declarations are normalized before lock or apply planning.

For setup steps, use [Getting Started](getting-started.md) or
[Use An Existing Environment](migration.md). For complete inputs, see the
[minimal example](../examples/daem.toml) and
[representative local project](../examples/representative-project.toml).

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
| `mcp_server` | array of tables | no | Supported MCP exact-projection relations; see [MCP Servers](#mcp-servers). |
| `extension` | array of tables | no | Supported host plugin/package carrier declarations; see [Extension Carriers](#extension-carriers). |

Unimplemented executable lifecycle declaration families such as `[[local_parameter]]`,
`[[package_runner]]`, `[[executable_artifact]]`, and
`command = { local_parameter = "..." }` are not public syntax and are rejected
before Desired normalization. The only supported MCP command object is the
exact absolute-path form documented under [MCP Servers](#mcp-servers). Current
`[[mcp_server]]` `command` and `args` fields are launch-vector data for the
managed config projection; they are not provisioning, package installation,
runtime readiness, or cleanup syntax.

Supported target identifiers:

- `codex`
- `claude-code`
- `opencode`
- `pi`
- `antigravity-cli`

`antigravity-cli` supports project/global instructions and Agent
Skills-compatible `[[skill]]`/`[[skill_group]]`, plus explicit-global stdio MCP
command/args and optional same-name ambient environment requirements. Markdown
slash-command skills, hooks, project/remote/plugin-bundled MCP, other plugin
rows, separate rules/workflows, settings, readiness and the IDE remain
unsupported. See [Instructions](#instructions), [MCP Servers](#mcp-servers) and
[Feature Support](features.md).

For current target and resource coverage, see [Feature Support](features.md),
the derived view of the typed target/resource surface.

OpenCode, Pi, and Antigravity CLI are valid targets; their skills use the shared
Agent Skills directory surface, and their supported instruction scopes are
documented under [Instructions](#instructions). Command hooks remain unavailable
for these targets; unsupported hook declarations are lock-only diagnostics, and
`add hook` rejects `antigravity-cli`. Codex and Claude Code command hooks and
explicitly declared source-backed hook assets remain supported. `doctor` reports
registry-derived capability checks; see [Feature Support](features.md). Hook
assets require `[hook_asset.<name>]` and same-scope `{hook_file:<name>}`
placeholders. Doctor labels checks `target=<target> capability=<resource-kind>`.

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
| Logical archive entries | 100,000 |
| Extracted files and directories | 100,000 |
| Canonical path | 4,096 bytes and 64 components |

Source-cache wrapper directories and completion metadata have separate reserved
capacity and do not consume these archive entry, depth, or byte limits.
Implicit parent directories count toward the extracted-entry limit. Archive
entries and extracted descendants are limited independently, so duplicate
logical entries cannot bypass materialized-tree bounds.

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
| `target` | table of tables | no | target defaults | Target-specific skill-root selection. Each selected target may set `install_to`. |

Target-specific skill placement uses a nested table:

```toml
[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex", "opencode", "pi"]

[skill.target.opencode]
install_to = ".agents/skills"

[skill.target.pi]
install_to = ".agents/skills"
```

`install_to` selects the skill root; daem appends the skill `name` as one
direct child. In this example Codex uses its default `.agents/skills/review`,
while OpenCode and Pi explicitly select the same supported compatible root.
The table key must name a target declared by that skill.

Codex also supports its modeled global compatible root:

```toml
[[skill]]
name = "humanizer"
source = { git = "https://github.com/blader/humanizer.git", ref = "main" }
targets = ["codex"]
scope = "global"

[skill.target.codex]
install_to = "~/.codex/skills"
```

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
- `target.<target>.install_to`, when present, must exactly match a supported
  skill root for that target and scope. Project values are canonical
  project-relative slash paths. Global values begin with `~/` and remain
  inside the home directory. Absolute paths, parent traversal, backslashes,
  unrecognized roots, and overrides for undeclared targets are rejected.
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

`daem add skill` appends or merges a `[[skill]]`, updating manifest and selected
lockfile without installing payloads or applying host changes. `add skill-group`
creates a compact group for shared source/targets/scope. `remove skill` removes
a declaration, group member, or selected targets and refreshes the prospective
lockfile; host deletion requires explicit `apply`. See [CLI
Reference](cli.md#add) and [Manifest Authoring
Scenarios](#manifest-authoring-scenarios).

Current skill-root catalog:

| Target | Project default | Project alternatives | Global default | Global alternatives |
| --- | --- | --- | --- | --- |
| `codex` | `.agents/skills` | none | `~/.agents/skills` | `~/.codex/skills` |
| `claude-code` | `.claude/skills` | none | `~/.claude/skills` | none |
| `opencode` | `.opencode/skills` | `.agents/skills`, `.claude/skills` | `~/.config/opencode/skills` | `~/.agents/skills`, `~/.claude/skills` |
| `pi` | `.pi/skills` | `.agents/skills` | `~/.pi/agent/skills` | `~/.agents/skills` |
| `antigravity-cli` | `.agents/skills` | none | `~/.gemini/config/skills` | none |

The catalog contains write roots, not every location a target may discover or
use at runtime. Omission selects the target default. An explicit `install_to`
may select only an alternative in the same row and scope; it cannot register
an arbitrary directory. Use `daem list paths` to inspect write, discovery, and
runtime locations separately.

Codex's documented Agent Skills authoring roots are `.agents/skills` and
`~/.agents/skills`; `~/.codex/skills` is a modeled compatible global
alternative, not the default. OpenCode and Pi also load the compatible roots
listed above while retaining their native defaults.

Import scans modeled placement/discovery roots, not direct `.agent/skills`
source pools; supplied/system/plugin/admin/runtime roots are skipped. Modeled
symlinked skills become vendored local sources, not managed live outputs.
Distinct names with no explicit `id`, the same normalized targets and the same
scope may form one `[[skill_group]]` under `daem.d/skill-groups/<hash>`, one
direct child per name, each locked separately. Divergent same-name imports
remain individual `[[skill]]` entries with explicit ids.

`daem` copies the full skill directory to the direct child named by manifest
`skill.name`. The lockfile uses `skill.id` when present and otherwise
`skill.name` as the resource id. Frontmatter keys that are specific to one
target, such as Claude Code tool-permission fields, are not lock-blocking for
other targets, but `doctor` reports that they may be ignored by targets whose
skill metadata model does not recognize them.

Import merges same-name Codex/Claude Code skills only for identical content,
into one multi-target `[[skill]]` without `id`. Every contributing route remains
freshness evidence through publication. Different content stays separate, for
example `id = "codex_global_review", name = "review"` and `id =
"claude_code_global_review", name = "review"`.

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
| `target` | table of tables | no | target defaults | Target-specific `install_to` inherited by every expanded child. |

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
- `target.<target>.install_to` follows the same target, scope, and catalog
  validation as an ordinary skill. Every expanded child inherits the selected
  root, and its own skill name is appended as the direct child.
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
- `doctor` lists local skill-group roots once per diagnostic run and shares the
  resulting view across compatibility and retained-discovery checks. It does
  not resolve remote skill-group sources for those checks.

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
- `exclude` selectors may match zero names.

Selector-backed expansion is bounded before candidate lists or selected Skills
can grow without limit. The limits apply to one complete skill-group listing
and expansion phase, across all groups in a `lock`, `outdated`, or local
`doctor` inspection phase:

| Resource | Limit |
| --- | ---: |
| Skill-group declarations | 1,024 |
| Distinct source roots listed | 1,024 |
| Direct entries observed | 100,000 |
| Direct-entry name bytes | 32 MiB |
| One direct-entry name | 4 KiB |
| Selectors per group | 128 |
| Selector pattern bytes per group | 64 KiB |
| Selector match evaluations | 1,000,000 |
| Matcher work (pattern bytes x (child-name bytes + 1)) | 134,217,728 units |
| Newly selected skills | 4,096 |

Git/local sources share these limits. Canonical roots are listed and counted once, but
every declaration counts toward the group ceiling and every direct entry,
including files/links, consumes source budget. First selection charges a name;
later exclusion never refunds it. Matcher work charges every pattern byte
against each potentially scanned child-name byte plus one pattern-only unit for
an empty name, before matching; cancellation is checked around each evaluation.
Exact limits pass; one unit over fails the whole lock without partial output.
Doctor reports overflow and omits selector-expanded checks, while direct checks
still run. These limits are package policy, not manifest fields.
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
TOML. Set explicit resource ids in the manifest; the authoring CLI has no id flag:

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

The preview proposes one group; omit `--dry-run --diff` to write it:

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

MCP servers are declared with `[[mcp_server]]`.

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

The string form is a portable executable token resolved through the agent
process environment:

```toml
command = "npx"
```

Use the explicit object form when host configuration must preserve one exact,
machine-local executable path:

```toml
command = { path = "/opt/example/bin/codegraph" }
args = ["serve", "--mcp"]
```

The `path` value must be an absolute, lexically canonical path under the
current host's path rules. Relative paths, traversal, surrounding whitespace,
control or bidirectional formatting characters, unknown command-object keys,
and redundant flags such as `portable = false` are rejected. The path is the
literal `argv[0]`; daem does not parse it as a shell command or resolve
symlinks while locking. Lock and host projection preserve the exact path, and
import emits this object form when a supported host entry already contains an
absolute command. The executable remains a non-owned prerequisite: declaring
the path does not make daem install, update, remove, or claim runtime readiness
for it.

`daem add mcp-server` intentionally accepts only the common portable string
form. Author an exact absolute path directly in the manifest, or obtain it
through `daem import`.

`[[mcp_server]]` is currently limited to nine stdio exact-projection slices:

- Codex project scope, rendered into project `.codex/config.toml` under
  `/mcp_servers/<name>` with `command` and `args` only.
- Codex explicit global scope, rendered into `~/.codex/config.toml` under
  `/mcp_servers/<name>` with `command`, `args`, and same-name `env_vars`.
- Claude Code project scope, rendered into project `.mcp.json` with `command`,
  `args`, and structured `env` references.
- Claude Code explicit global scope, rendered into top-level `~/.claude.json`
  under `/mcpServers/<name>` with `command`, `args`, and exact aliased
  `${SOURCE}` `env` references.
- OpenCode project scope, rendered into strict project `opencode.json` as
  `type = "local"` with `command` and `args` only.
- OpenCode explicit global scope, rendered into strict default user
  `~/.config/opencode/opencode.json` as `type = "local"` with `command` and
  `args`, plus optional exact child-to-source environment references lowered to
  `{env:SOURCE}` strings.
- Pi project scope, mediated by one explicitly declared admitted
  `pi-mcp-adapter` package and rendered into project `.pi/mcp.json` under
  `/mcpServers/<name>`.
- Pi explicit global scope, mediated by the same provider contract and rendered
  into `mcp.json` under the current Pi agent root, `~/.pi/agent` by default or
  `PI_CODING_AGENT_DIR` when set.
- Antigravity CLI explicit global scope, rendered into
  `~/.gemini/config/mcp_config.json` with `command` and `args` only; optional
  same-name ambient source names remain lock-only prerequisites.

Any core-native MCP row that writes shared user-level global host config must
put `scope = "global"` directly on that `[[mcp_server]]` block. A top-level
`[defaults].scope = "global"` value may default other resource families, but it
does not authorize global MCP config mutation.

For all supported rows, `command` and `args` are launch-vector data. They may be
rendered into host config and modeled as a non-owned executable requirement, but
they are not package installation, provisioning, runtime readiness, or cleanup
syntax.

Pi MCP is provider-mediated rather than core-native. Each Pi binding must
correlate with one explicit `[[extension]]` using `carrier = "pi-package"`,
target `pi`, the same scope or a reusable explicit-global scope, and an exact
or caret-bounded `npm:pi-mcp-adapter@<version>` source. The current profile
accepts canonical stable versions `>=2.13.0` and `<3.0.0`; `2.13.0` is the
verified contract floor, while `2.15.0` is the deeply inspected artifact and
not an exact-version ceiling. Unbounded selectors, tags, prereleases,
below-floor versions, and the next major are rejected before mutation.

For Pi MCP, the provider relation and MCP projection remain separate. Apply
establishes and observes the selected `pi-mcp-adapter` package/version, then
writes only the selected `mcpServers/<name>` contribution to `.pi/mcp.json`
(project) or `<Pi agent root>/mcp.json` (global). Other Pi-owned/imported layers
may be observed for collision/fallback diagnostics but are not written. See
[Host Integration Contract](host-integrations.md#mcp-server-config).

Pi entries accept canonical `command`, ordered `args`, exact child-to-source
`${SOURCE}` aliases, `lifecycle = "lazy"`, and `disabled = false`; the two
semantic defaults may be omitted. The unambiguous `mcp-servers` alias and JSONC
are accepted. Malformed, duplicate, conflicting-alias, credential-bearing, or
unsupported managed fields are rejected, and environment values remain
runtime-local. Convergence does not prove Pi trust, provider activation,
connectivity, authentication, runtime health, or discovered tools. See [Host
Integration Contract](host-integrations.md#mcp-server-config).

Pi project-scoped provider packages are ignored when the project is untrusted,
but an installed global provider may still read project `.mcp.json` and
`.pi/mcp.json`; eager entries can execute before trust, including under `pi
--no-approve`. Daem authors only lazy entries and warns about the sharing
consequence; it does not own or sanitize unowned project MCP files. See [Host
Integration Contract](host-integrations.md#mcp-server-config).

Provider admission does not flatten package-bundled MCP, skills, hooks,
instructions, apps, commands, or rules into standalone declarations; those
contributions remain provider-scoped unless an exact profile admits ownership.

`daem import` may create `[[mcp_server]]` only for supported core-native rows.
It preserves accepted child-to-host environment references, skips unsupported or
credential-bearing shapes, creates no source files, and never edits host MCP
config, starts servers, probes readiness, installs packages, or flattens
plugin-bundled MCP. Provider-mediated Pi MCP requires its explicit package
relation and is not inferred. See [Host Integration
Contract](host-integrations.md#mcp-server-config).

OpenCode MCP requires effective `targets = ["opencode"]` with project or
explicit global scope. The add helper writes an explicit project row unless
`--scope global` is supplied. Strict `opencode.json` `/mcp/<name>` entries use
`type = "local"`, command and ordered args. Project rows reject `env`; global
aliases such as `env = { CHILD_TOKEN = { from_env = "SOURCE_TOKEN" } }` render
as `"environment": {"CHILD_TOKEN": "{env:SOURCE_TOKEN}"}`. Import accepts only
that exact whole-value form. Literals, `$NAME`, `${NAME}`, file interpolation,
compound/malformed forms, custom/JSONC/remote authority, `cwd`, `enabled`,
`timeout`, auth/session/tool-policy and unknown fields are rejected.

An existing cataloged `opencode.jsonc` alternate skips the strict MCP document
without declarations. If absent, its appearance invalidates import without
reading its contents. Primary file content/object identity also remains
authoritative through publication, including merge no-ops. See [Host
Integrations](host-integrations.md#mcp-server-config).

Codex MCP requires effective `targets = ["codex"]` and project or explicit
row-local `scope = "global"`. Add writes a project row unless `--scope global`
is supplied. Project `.codex/config.toml` uses command/args only. Global
same-name references such as `env = { CODEX_TOKEN = { from_env = "CODEX_TOKEN" }
}` become sorted, deduplicated `env_vars`. Native string names, `{ name =
"CODEX_TOKEN" }` and `{ name = "CODEX_TOKEN", source = "local" }` import
equivalently. Project env, aliases, literal env, remote sources, unknown
env-object keys, custom roots, `cwd`, HTTP/auth/session/tool-policy and plugin
MCP are unsupported.

Claude Code MCP requires effective `targets = ["claude-code"]` and project or
explicit row-local global scope. Project `.mcp.json` accepts structured env
references; global top-level `~/.claude.json` `/mcpServers/<name>` uses
command/args and exact `"CHILD": "${SOURCE}"`. The global add helper needs
`--scope global`; add env references by manifest edit or accepted native import.
Global native values must be exact `${VALID_NAME}`. Literals,
`${NAME:-default}`, compounds, `user_config` interpolation, malformed names,
local/project state, OAuth/session/trust, HTTP/remote, headers, `cwd`,
timeout/tool-policy and unknown managed fields are rejected. Unrelated top-level
config, `projects`, OAuth/session/trust siblings and same-name project shadows
are preserved, not owned.

Antigravity MCP requires `targets = ["antigravity-cli"]` and explicit `scope =
"global"`. Only same-name references such as `env = { TOKEN = { from_env =
"TOKEN" } }` are accepted: names are locked and checked before mutation, but no
native env is written; the child inherits the CLI environment. Aliases and every
native `env` form are rejected, including literals, `${TOKEN}`, `$TOKEN` and
`{env:TOKEN}`. Import infers no ambient references. Rejected managed fields
include `serverUrl`, `url`, `headers`, `oauth`, `authProviderType`, `disabled`,
`disabledTools`, `enabledTools`, `tools`, `cwd` and unknown fields.

Add/remove MCP helpers author the nine supported rows. Omitted target/scope
works only when inheritance and supported-row compatibility select one row.
Claude global needs `--target claude-code --scope global`; Codex needs `--target
codex` and explicit global scope; OpenCode/Pi need their target and project or
explicit global scope; Antigravity needs `--target antigravity-cli --scope
global`. Defaults do not authorize global MCP. Helpers author command/args: add
supported env references manually or through accepted import (Antigravity only
manually; project OpenCode rejects them).

Pi add creates/reuses an explicit scoped `pi-mcp-adapter`: project prefers
project provider, otherwise one unambiguous explicit-global provider; global
requires global provider. Ambiguous/incompatible providers fail before
manifest/lock changes. MCP removal retains the provider declaration. Helpers
update manifest/adjacent lock together, never host config, server execution,
package installation, credentials or trust/approval. Remove deletes the whole
declaration; later apply removes only its managed projection, retaining
executables, caches/stores, credentials, trust, sessions, logs and runtime
state.

Doctor passively checks ambient commands through `PATH`, absolute commands at
their exact path and modeled env names, without executing or proving
convergence. Claude project locks delegated executable identity with
child/source pairs; Claude global locks `${SOURCE}` mapping; Codex global locks
canonical `env_vars` names. Values remain runtime-only, never locked.

Lock and dry-run validate only symbolic names and supported mappings; they do
not require those names to be present in the current process environment.
Before a normal apply can mutate anything, daem requires every source used by
the selected desired MCP bindings to be present. It repeats that presence check
before acquiring mutation authority and after rebuilding the plan under the
lease. An empty value counts as present. The check also applies when a selected
binding is already current, but not after that binding has been removed from
the desired manifest. Missing-source diagnostics are sorted, deduplicated,
bounded, and contain source names only. Claude project values are resolved
again only for delegated subprocess launch; Claude global and Codex global
values are resolved later by the host when it launches the configured server.
Antigravity values are inherited from the environment of the Antigravity CLI
process that starts the server; daem cannot prove the environment of a future
independently launched process.
Values are never added to the
lockfile, statefile, recovery data, plan fingerprint, rendered config, or CLI
output.
Codex, OpenCode, and Antigravity CLI direct config projections have no
delegated executable claim in this slice. Codex has no runtime probe in this
slice. OpenCode runtime checks are
available only through the separate explicit `probe mcp-server --target
opencode --scope project` surface for locked project local-command rows.
Package-backed MCP commands may be floating or pinned. Pin policy is derived
from the package ecosystem rather than from nonempty selector text:

- npm is pinned only by a complete valid SemVer version. Partial versions,
  ranges, tags, and wildcards remain floating, following the selector forms in
  the [npm package specification](https://docs.npmjs.com/cli/v11/using-npm/package-spec/).
- Python is pinned only by a validated exact PEP 440 version, including valid
  epoch, pre-release, post-release, development, and local forms. Ranges,
  compatible-release clauses, exclusions, arbitrary equality, and wildcard
  equality remain floating, following the
  [PyPA version-specifier contract](https://packaging.python.org/en/latest/specifications/version-specifiers/).
- A container image is pinned only by a complete canonical lowercase
  `sha256:<64 hexadecimal digits>` digest. Every tag, including a version-like
  tag, remains floating because the
  [OCI distribution contract](https://github.com/opencontainers/distribution-spec/blob/main/spec.md)
  distinguishes tag lookup from digest identity.

Unknown or malformed selectors do not gain pinned assurance. They remain
eligible for delegated host execution when their structured argv is otherwise
valid, and daem does not rewrite that argv while classifying it. `add
mcp-server` warns for floating package identity, while lock and JSON plan output
record the directly declared package inputs that daem can derive and their
aggregate `pin_policy` without installing or probing packages. Repeated npx
`--package` options and uvx `--with` inputs all contribute to that set; one
floating input makes the whole delegated plan floating. Opaque or unknown
runner syntax may leave the diagnostic package set partial and always prevents
pinned assurance. Inputs whose package contents come from an external
requirements file likewise prevent pinned assurance because argv alone does
not enumerate them. uvx extras and direct-URL requirements that daem cannot
losslessly project into its canonical package-reference model follow the same
opaque, floating path while their exact argv remains unchanged. An exact
package version constrains only that direct package input; it is not a
transitive dependency lock or a reproducible-build claim.
Recognized options that take a separate value are invalid when that value is
missing, empty, or the `--` command delimiter. This is malformed argv structure,
not an opaque package selector, so daem rejects it before deriving pin policy.
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

Git instruction files use the same repository locator and explicit ref as skills:

```toml
[instructions.project]
source = { git = "https://github.com/acme/guidance.git", path = "instructions/AGENTS.md", ref = "main" }
```

The path must resolve to a regular file at the locked commit, not a directory or
link. S3 file object instructions also use a structured source:

```toml
[instructions.project]
source = { s3 = "s3://daem/instructions/AGENTS.md", version_id = "3Lg...", region = "us-east-1" }
targets = ["codex"]
```

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `source` | string or source object | yes | none | Local instruction file path, local source object, Git file source, or S3 file object source. |
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
- Git instruction sources require a repository-relative file path and an explicit ref; locking records the resolved commit.
- Structured local instruction sources must use `mode = "vendor"`.
- S3 instruction sources must use `format = "file"` or omit `format`.
- `targets`, if present, must contain supported target values and no duplicates.
- `[instructions.<name>.target.<target>]` must reference a target declared for that instruction resource.

`daem add instruction` can append a local or Git file instruction declaration or merge
new targets into an existing matching `[instructions.<name>]`. The authoring
command updates the manifest and selected lockfile together, but does not render
host instruction files, apply host changes, or mutate state. For Git, supply
`--ref` and a file path through `--path` or `owner/repo/path` shorthand.
Bare paths without Git options retain local-file interpretation. S3 instruction
file objects remain explicit manifest edits.
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
- Git uses the system `git`; SHA-1 works without `--object-format`, SHA-256
  requires it. Capable Git always gets explicit `--object-format`;
  `GIT_DEFAULT_HASH` is not inherited. Network format observation contacts only
  the declared locator from a rooted repository, so enclosing `url.*.insteadOf`
  cannot redirect `ls-remote`.
- Git refs are resolved to immutable commits and stored as `resolved_ref`.
- Individual Git skill source `path` values may name a skill directory or `path = "."` when the repository root itself is the skill artifact and contains exact `SKILL.md`. Git `[[skill_group]]` source roots may also use `path = "."`, but there it means the root whose selected direct children are locked as separate skill artifacts.
- Git repository paths are exported from the resolved commit. Archive entries that escape the artifact directory or resolve to links are rejected.
- S3 sources are resolved through the AWS SDK default config chain, with
  per-source `region` as an optional override.
- S3 VersionId is stored as `resolved_ref` when the service returns one. The
  materialized artifact `content_hash` remains the integrity check whether or
  not bucket versioning is enabled.
- Explicit S3 `version_id` permits cache reuse before AWS client creation only
  after exact source/version record and artifact-byte verification. Unversioned
  sources refetch each sequential resolution; ETags do not confer immutable
  identity.
- S3 archive extraction rejects path traversal, symlinks, hardlinks, and special
  files. S3 prefix directory sources are unsupported.
- Skill sources are validated as directories with a regular `SKILL.md`.
- Command-only supported Hooks lock no source artifact; each admitted target
  locks one canonical managed-aggregate contribution. Schema version 6 admits it
  only when the Hook codec can produce a structurally valid host document within
  enforced byte, depth, event, group, and handler bounds; rendered aggregate
  content is tracked separately.
- Instruction sources are locked as file sources and must resolve to regular files.
- Selector-backed `[[skill_group]]` entries are expanded during lock. The
  generated dry-run delta reports selected child additions, removals, and
  content changes as ordinary per-skill lockfile entry changes.
- Each lockable resource is a canonical `[[locked.subject]]` row ordered by
  `entity_id` then `subject_id`; selector-backed Skill children carry
  `skill_set_member.declaration_identity`, while direct Skills carry no
  declaration provenance facet.
- Collection-owned `[[locked.order_constraint]]` rows store profile `class_id`,
  host-load-identity `contract_version`, `runtime_meaning` and ordered `member`
  rows with exact `subject_id` and canonical `host_load_identity`. Rows sort by
  class id; members retain manifest order, never declaration-id order.
- Classes below two locked members are omitted. Persisted classes must contain
  all admitted members exactly once; duplicate host identities,
  dangling/cross-class members and profile contract/runtime drift are rejected.
  OpenCode locks config order, Pi runtime precedence. This neither records
  current host sequence nor mutates config, subject identity or route identity.
- Confirmed apply settles selected carrier install/removal before rereading each
  order class, then converges Pi package or OpenCode server/TUI sequences
  independently. Newly observed precedence changes require renewed confirmation;
  non-interactive apply stops. Partial multi-document success is reported
  without rollback claims, and retry derives work from fresh host files.
- Commands that consume a lock as current authority rederive every persisted
  host-load identity from the locked carrier source and the selected manifest
  context. A mismatch blocks `status`, `apply`, `refresh`, and `probe` until
  `daem lock` regenerates the snapshot. `lock` and `outdated` deliberately
  treat the previous snapshot as comparison input, so they can repair stale
  path context rather than being blocked by it.
- Skills lock one exact-Supply subject and one managed-path projection per
  physical placement. Shared placements coalesce with canonical
  `consumer_targets`, scope, portable destination, content kind, placement mode,
  permission policy, full permission bits when `exact`, and adapter version.
  Exact `0000` differs from omission. No primary target, current filesystem
  state or machine-expanded path is recorded.
- Each Instructions resource locks one exact Supply subject with a
  non-executable file-use contract and deterministic materialization, plus one
  managed-file projection per supported placement. Shared files coalesce into
  one consumer set. Schema-v6 Supply without a structurally valid file
  projection is rejected; apply/status require the projection set to match the
  manifest/profile refinement.
- Existing lockfile entries that are no longer declared by the manifest are removed by normal lockfile regeneration.
- Lock readers accept only schema version 6 and reject unsupported versions,
  invalid UTF-8, unknown keys/shapes, duplicate or zero-facet subjects, unknown
  realizations, invalid correlations, unsupported exact-Supply forms, malformed
  identities/contracts, missing `exact_permission_mode` for exact projections,
  and noncanonical persisted values. Schemas 3–5 are not interpreted: `status`,
  `apply`, and `outdated` require regeneration; `daem lock` and transactional
  authoring may atomically replace them. Schema 3 was published by daem v0.1.0;
  earlier unknown and future schemas are not replacement-authorized, and future
  schemas require newer daem.
- Schema version 6 does not admit `generated_at`; readers reject it as an
  unknown key rather than treating timestamp metadata as lock authority.
- Schema v6 records canonical delegated package inputs recognized from argv and
  an ecosystem-derived `pin_policy`. Legacy rows are not interpreted because
  version-like tags, ranges, or unmodeled inputs can overstate `pinned`; the
  public manifest remains schema version 1 and needs no migration.
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
| `source` | table | yes | none | Must be `{ marketplace = "<plugin>@<marketplace>" }` for Claude Code and Codex, or `{ host_source = "<host-native-source>" }` for OpenCode/Pi/Antigravity CLI. Claude uses this canonical selector as both host argv and the installed-inventory key; bare plugin names are rejected. The selected value is passed as one structured argv element and must not begin with `-`; marketplace and host_source are mutually exclusive. The selected source grammar determines punctuation semantics: marketplace `:` and `@` bytes remain selector data, while a scheme-less password-bearing Git or host locator, including an optional host port, is credential userinfo only in a host-source. URL passwords, HTTP userinfo, all query fields, assignment-style fragments, credential-shaped key/value fields, other inline secrets, and raw host config are rejected before lock creation. Inert source fragments such as `#v1` remain valid. |

Credential inspection uses raw and bounded canonical source forms; a marketplace
source gains selector authority only when both retain one `plugin@marketplace`
partition, with no extra delimiter or URL userinfo. See [Host Integration
Contract](host-integrations.md) for source grammar.

Pi local package sources are context-resolved without requiring existence or
following symlinks. Native paths, `file://`, dot segments, and `~` collapse to
one lexical identity; project scope stores clean manifest-root-relative spelling
and global scope clean absolute spelling. Same-scope collisions are rejected.
Normalization does not make an external settings row source-exact or adoptable.

Pi npm sources preserve credential-free local, remote, and opaque package
arguments. Only direct registry specs and npm aliases establish public package
identity; the outer name is the relation identity. Git sources include
`git+http`, `git+https`, and `git+ssh` with a valid host and non-empty
repository path; percent-encoded `@` in that path remains repository data. These
use Git checkout identity and removal semantics, not local-source semantics.

Lock stores a `host_relation` subject with route identity and operation
contracts. Status/apply preview disclose subject, target/scope, route/admission,
evidence source/availability/freshness, replay boundary, retained effects and
non-claims. Claude observations read version-2
`<CLAUDE_CONFIG_DIR>/plugins/installed_plugins.json`, falling back to
`~/.claude/plugins/installed_plugins.json`. Project rows match canonical
manifest root/`projectPath`; global rows match host `scope = "user"`. Missing
inventory is fresh empty evidence; malformed/unsupported/ambiguous/invalid
selected rows fail closed. The observer never invokes Claude or inspects
bundles, enabled settings, trust, readiness or contributions.

Confirmed apply may execute the exact host/scope install route in [Host
Integrations](host-integrations.md#host-delegated-carrier-lifecycle-routes):
Claude `claude plugin install ... --scope project|user`, Codex `codex plugin add
... --json`, OpenCode `opencode plugin` (`--global` only globally), Pi `pi
install` (`-l` only for project), Antigravity `agy plugin install`.
Codex/Claude/OpenCode/Pi require fresh exact selected-scope absence before
invocation and fresh exact presence for a managed claim. Antigravity selectors
instead require complete-pair absence, then bounded matching import/bundle
presence correlated with the exact pending source/route identity; passive
evidence cannot prove marketplace provenance. Malformed, duplicated, mismatched,
partial, unstable or symlinked selected Antigravity state blocks. Distinct
structural sources collapsing to one plugin name are rejected; identical shared
carriers remain valid.

Attempts and pending facts alone never authorize skip/removal. Interrupted
installs may complete correlation from fresh route-supported evidence; normally
finished attempts without a claim retire pending state. No-observer Antigravity
forms remain unsupported, not missing, and apply retries. Explicit
`--manage-existing` requires source-exact existing relations. Claims record
relation provenance, not artifact/version/cache convergence, enablement, trust,
readiness or contributions. Complete leases serialize shared routes/destinations
across manifests; plans rebuild and revalidate before any effect. Refresh,
removal, host-preserving unmanage, unsupported prune, probes and
bundled-contribution import are separate operations. [State And
Recovery](state-and-recovery.md) describes pending/global claim commits.

For Claude Code project/global rows, exact desired absence with a durable
managed claim may invoke `claude plugin uninstall <plugin>@<marketplace> --scope
project|user --keep-data`. Apply requires fresh exact presence and no remaining
daem-known consumer, and retires authority only after fresh absence; command
result alone is insufficient. The route retains marketplace declarations, caches, metadata,
data, dependencies, credentials, trust/session state, siblings, and unrelated
resources; it does not prune residue, bundled contributions, ambient consumers,
or runtime state.

For Codex explicit-global desired absence with a durable claim and zero
remaining daem-known consumers, apply may invoke `codex plugin remove
<plugin>@<marketplace> --json`. Both selected config relation and
`$CODEX_HOME/plugins/cache/<marketplace>/<plugin>` must be freshly absent.
Partial/uncertain results retain claim and pending removal; present config still
invokes with missing cache. Only config already absent before pending removal
allows no-invocation retirement, leaving orphan cache outside prune authority.
Marketplace declarations/snapshots, siblings and other-marketplace
relations/caches, credentials, trust/sessions, unrelated config and external
stores remain. No ambient-consumer or runtime unload/readiness claim is made.

For OpenCode project/global rows, exact desired absence with a durable claim
removes only the exact host-source row from every existing server/TUI JSON or
JSONC candidate; it does not run `opencode uninstall`. Missing candidates are
no-ops and are not created. Bounded structured edits preserve comments,
whitespace, tuples, siblings, unknown fields, empty arrays, and files; partial
success remains retryable until fresh absence from every candidate. Other
scopes, installations, caches, sources, data, credentials, sessions, runtime
activation, unrelated config, and non-selected layers remain untouched.

For a safe Antigravity explicit-global `PLUGIN@MARKETPLACE`, desired absence
with a durable claim, fresh residual correlation and zero remaining daem-known
consumers may invoke `agy plugin uninstall <plugin>`—the name, not selector.
Exit zero is insufficient: claim/pending authority retires only after fresh
absence of both import-manifest row and plugin directory. Partial results remain
retryable; already-absent pairs retire without invocation. Siblings,
credentials, trust/sessions, stores, source setup and IDE state remain;
marketplace provenance and ambient consumers are not proven. Opaque/local
sources receive no removal authority.

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
`daem import` authors exact extension declarations for Codex global, Claude
Code project/global, OpenCode project/global, and Pi project/global rows. It
preserves exact host-native source spelling and relative order without
installing, claiming, locking, or mutating the host. Antigravity CLI inventory
does not recover the exact source needed for a declaration, so those rows are
reported as source-provenance skips.
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

The manifest models targets, scopes, instructions, skills, hooks, and supported
MCP exact-projection relations; intentionally unimplemented surfaces are
summarized in [Feature Support](features.md) and the owning host contracts.

- MCP remains limited to the supported Codex, Claude Code, OpenCode, Pi, and
  Antigravity CLI stdio slices and their documented project/global destinations,
  environment-reference rules, and rejection conditions. Pi requires an explicit
  admitted `pi-mcp-adapter`; import does not infer ambient names. Config
  convergence, provider version, trust, approval, runtime readiness, endpoint
  health, tool inventory/policy, credentials, package/cache ownership, and
  broader host ownership remain separate or unsupported concerns. See the [MCP
  Servers](#mcp-servers) table, [Host Integration
  Contract](host-integrations.md#mcp-server-config), and the linked examples.

- Hook asset directory payloads, plugin-bundled hook installation, standalone
  executable installation, and command path rewriting. [Hook Assets](#hook-assets)
  documents the narrower supported regular-file payload boundary.
- Public `[[extension]]` carriers are limited to Codex global marketplace
  selectors, Claude Code project/global marketplace rows, OpenCode host-source
  rows, Pi package host-source rows, and Antigravity CLI explicit-global
  host-source rows. Codex project scope is `unsupported` (`host-unavailable`);
  Claude public global maps to host `--scope user`, public `scope = "user"` is
  rejected, and Claude `local` is `deferred` (`not-modeled`). Pi direct
  `pi-extension`, Antigravity project scope, and Antigravity import/link
  provenance rows are not public syntax. See [Extension
  Carriers](#extension-carriers).
- Symlink placement in mutating apply.
- OpenCode, Pi, or Antigravity CLI hook rendering semantics.
