# Concepts

Daem separates what you want, what source content was resolved, and what it has
permission to manage. Keeping these separate lets you preview a change without
applying it, and detect changes made outside daem.

```text
manifest -> lockfile -> status / apply preview -> apply -> managed outputs
```

## Manifest

`daem.toml` declares resources, sources, targets, and scopes. You can edit it
directly or use the authoring commands. The parser rejects unknown keys and
invalid declarations; the [Manifest Reference](manifest.md) owns the schema.

Most commands select an existing `./daem.toml`, then the user manifest at
`${XDG_CONFIG_HOME:-~/.config}/daem/daem.toml`. They do not search parent
directories. `init` and a new, non-merge `import` instead create `./daem.toml`.
Use `--manifest <path>` to select a workspace explicitly.

The user manifest is not a project root. Its target-visible resources must use
global scope. For project resources, select a project manifest. See
[Workspace Selection](cli.md#workspace-selection) for exact path rules.

## Targets And Scopes

Targets name agent hosts: `codex`, `claude-code`, `opencode`, `pi`, and
`antigravity-cli`. A resource can select one or several targets.

- `project` places resources under the selected project manifest root.
- `global` places resources under the target's user-level root, affecting its
  use across projects.

An execution selector such as `--target codex` filters declarations already
assigned to that host; it does not add Codex to a resource's target set.
Support differs by resource, scope, and operation. See [Feature Support](features.md)
for the summary and [Host Integrations](host-integrations.md) for exact routes.

## Sources

Sources supply the content daem locks:

- **Local files and directories** need no remote repository. Project paths may
  be relative to the manifest; global local paths must be absolute.
- **Git repositories** supply committed skill or skill-group content. A branch,
  tag, or full commit id is required, and locking records the resolved commit.
  The repository can be local. Git-backed instructions are not supported.
- **S3 objects** supply supported file or archive content. Credentials remain
  outside the manifest and lockfile.

Not every resource uses a source object. A hook command or MCP launch vector
is configuration, not an instruction to install the executable it names.
[Sources](manifest.md#sources) defines the admitted forms and resource limits;
[Use An Existing Environment](migration.md) shows the local and import paths.

## Lockfile

`daem.lock.toml`, beside the manifest, records resolved source identities,
content hashes, declaration provenance, and supported operation identities.
It does not record ownership of live host files.

`add` and `remove` update the manifest and lockfile together. After editing
TOML or importing configuration, run `lock` explicitly. Removing a declaration
removes its stale lock entry on the next successful lock; it does not delete
the host output. That requires a separately reviewed apply.

## Statefile

The statefile records outputs daem wrote or explicitly registered, plus
managed extension claims and pending transitions. When an output changes
outside daem, apply reports drift instead of overwriting it. An existing output
with no daem claim is unmanaged, even if it looks like a declared resource.

`import` copies supported configuration into source files and declarations;
it does not claim the original outputs. `apply --manage-existing` can register
an eligible exact match. That grants later authority to update or remove it,
not permission to overwrite a mismatch.

Project metadata lives under `.daem/`. The implicit user workspace uses the
state and cache locations described in [Storage Roots](cli.md#daem-storage-roots).
Imported `daem.d/` files are user-owned sources, not cache or managed-state
metadata. Do not delete state or recovery files to bypass a refusal.

### Shared Global Ownership

Different project manifests can request the same global destination. A shared
registry prevents them from owning the same whole path or overlapping config
entries. Equal bytes do not permit co-ownership; disjoint config contributions
may have different owners. Carrier relations use a separate claim registry,
which tracks daem-known consumers but not every ambient host user.

The [state and ownership reference](state-and-recovery.md#statefile) explains
claim transitions, permission evidence, and diagnostic records. Those records
do not prove current runtime health or package-cache convergence.

### Mutation Revision Evidence

A plan retains evidence of inputs that must remain current until its effect.
If required evidence changes or cannot be established within the supported
bounds, daem refuses rather than treating the old plan as current. See
[Mutation Revision Evidence](state-and-recovery.md#mutation-revision-evidence)
for observation modes and limits.

## Recovery Journal

Before a journaled apply changes host files or state, daem records the operation
and the evidence needed for recovery. `recover` can then classify interrupted
work and perform the permitted rollback, finalization, or retained cleanup.
It handles one active operation, not a historical snapshot or arbitrary restore.

Start with `daem recover --dry-run` after an interrupted apply. Recovery can
refuse when its evidence is no longer valid; on Linux, a journal from an earlier
boot is deliberately refused. Preserve the journal and backups rather than
editing them to bypass the check. See [Troubleshooting](troubleshooting.md#apply-was-interrupted)
for actions and the [Recovery Journal reference](state-and-recovery.md#recovery-journal)
for persistence, cleanup, and resource limits.

## Managed Resource Types

### Instructions

Instruction sources render into supported host files. Project defaults are
`CLAUDE.md` for Claude Code and `AGENTS.md` for Codex, OpenCode, Pi, and
Antigravity CLI. Global destinations use each host's configured root.

Targets selecting the same physical file share one projection and managed-state
row. A source's executable bit does not make the rendered instruction executable;
copy publication writes a private, non-executable file. Instruction symlink mode
can be represented in the manifest but is not currently executable.
See [Instructions](manifest.md#instructions) for alternate placements and modes.

### Skills

A skill is a directory containing `SKILL.md`. Its `name` is the agent-visible
directory and frontmatter name. Its optional `id` is daem's resource key;
without an `id`, the key is the name. Distinct ids let different targets use
different skills with the same installed name.

Targets sharing a placement share one directory and state row. Different
placements are independent, so applying or removing one does not mutate its
siblings. `install_to` selects only a cataloged alternative root, not an
arbitrary directory. Copy placement is executable; symlink and hardlink
placement are represented in desired state and lockfiles but refused by apply
before host, state, or journal mutation.

Skill groups share a source root and settings. Explicit members expand into
individual skills; selector-backed groups expand at lock time. Status and apply
use locked members rather than rediscovering the upstream tree. See
[Skills](manifest.md#skills) and [Skill Groups](manifest.md#skill-groups).

### Hooks

Hooks manage supported Codex and Claude Code command-hook configuration, not
the scripts themselves. Daem owns the `hooks` subtree and preserves unrelated
top-level settings. The command must already be available to the host.
Manually declared unsupported hook targets remain lock-only diagnostics;
`add hook` rejects targets without supported authoring. See [Hooks](manifest.md#hooks).

### Hook Assets

Hook assets are source-backed executable files referenced explicitly through
`{hook_file:<name>}` placeholders in supported hooks. Hook and asset scope must
agree. Daem does not infer assets from arbitrary command strings or `PATH`.
See [Hook Assets](manifest.md#hook-assets).

### MCP Server Bindings

MCP declarations manage supported stdio configuration bindings. Commands and
arguments describe how the host launches the server; they do not provision it.
Removing a binding does not remove packages, caches, credentials, trust state,
logs, or other runtime residue.

Pi MCP needs an explicit admitted `pi-mcp-adapter` package declaration; it is
not a Pi core-native surface. Provider installation, effective config, project
trust, and runtime readiness remain separate. See the [project](../examples/pi-project-mcp-stdio.toml)
and [global](../examples/pi-global-mcp-stdio.toml) examples.

`probe mcp-server` offers an explicit bounded runtime check for supported rows.
Confirmed apply may also run the locked server command for the supported Claude
Code project row. A past successful probe is not proof of current readiness or
permission to skip fresh checks. See [MCP Servers](manifest.md#mcp-servers).

### Extension Carriers

An extension declaration identifies a host-native plugin or package relation,
not ownership of its package store. `add extension` and `remove extension`
change desired state and the lockfile. A later apply may invoke the supported
host install or removal route. It does not establish exact installed version,
enablement, trust, bundled contributions, or runtime readiness.

Refresh is a separate operation. Managed removal requires exact durable
authority, fresh evidence, and no remaining daem-known shared consumer.
`unmanage extension` retains host state; package-store pruning is unsupported.
See [Host Integrations](host-integrations.md) for each lifecycle row.

## Safety Model

Preview before each write: use `--dry-run` for authoring, lock, apply, and
recovery. `status` and `doctor` are read-only; doctor checks passive prerequisites
without launching host CLIs, package managers, credential helpers, or servers.
`probe` is the separate command for an explicitly authorized runtime check.

A preview does not authorize a later command to ignore changed inputs. Applying
and recovering retain their own validation and refusal rules. See
[Execution Modes](cli.md#execution-modes) for confirmation, output, and automation.

### NFS-Backed Homes

Ordinary single-host NFS use is best effort, not a guarantee across all server,
client, mount, cache, and failure combinations. Do not rely on daem leases for
cross-node exclusion. Revalidation can detect changes visible before an effect,
but cannot exclude another write after the final check. NFS outage/reconnect
behavior and stable-storage durability are outside the current guarantee.
See [Platform Support](platforms.md) and
[NFS troubleshooting](troubleshooting.md#nfs-backed-home-or-workspace).
