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

Most commands choose the current-directory manifest, then the user manifest;
there is no parent-directory search. `init` and non-merge `import` instead
create `./daem.toml`. Use `--manifest <path>` for explicit selection.
The user manifest requires global resources; it is not a project root.
See [Workspace Selection](cli.md#workspace-selection) for paths and exceptions.

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

Different manifests cannot co-own a whole global path or overlapping config,
even with equal bytes. Disjoint config contributions may have separate owners.
Carrier claims track daem-known consumers, not every ambient host user.

### Mutation Revision Evidence

Changed or unavailable required input evidence invalidates a plan: preview again
from current state. [Observation limits](state-and-recovery.md#mutation-revision-evidence)
apply to freshness checks too.

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

Instruction sources render to `CLAUDE.md` for Claude Code or `AGENTS.md` for
other project defaults; global files use the host's configured root. Shared
destinations produce one managed file. Copy output is private and non-executable;
symlink mode is not executable. See [Instructions](manifest.md#instructions)
for placements and modes.

### Skills

A skill is a directory containing `SKILL.md`. Its `name` is the agent-visible
directory and frontmatter name. Its optional `id` is daem's resource key;
without an `id`, the key is the name. Distinct ids let different targets use
different skills with the same installed name.

Targets sharing a placement share one directory; separate placements remain
independent. `install_to` selects a cataloged root, not an arbitrary path.
Only copy placement can execute; apply refuses symlink/hardlink modes before
host, state or journal mutation.

Groups share a source root/settings and lock each child separately. Selectors
expand at lock time; status/apply use locked members, not upstream rediscovery.
See [Skills](manifest.md#skills) and [Skill Groups](manifest.md#skill-groups).

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

MCP bindings configure stdio command/args, not executable provisioning. Removing
a binding leaves runtime/package residue. Pi needs an explicit admitted
`pi-mcp-adapter` package; installation/config do not prove trust or readiness.
See the [project](../examples/pi-project-mcp-stdio.toml) and
[global](../examples/pi-global-mcp-stdio.toml) examples.

`probe mcp-server` is an explicit bounded runtime check; confirmed Claude Code
project apply may also run the locked command. Past success does not replace
fresh checks. See [MCP Servers](manifest.md#mcp-servers).

### Extension Carriers

Extensions declare host plugin/package relations, not package-store ownership.
Add/remove edit manifest and lock; apply may install or remove. Removal requires
exact authority, fresh evidence and no remaining daem-known shared consumer.
Refresh is separate; unmanage retains host state; prune is unsupported.
Relation convergence does not prove exact version, enablement, trust, bundled
contributions or readiness. See [Host Integrations](host-integrations.md).

## Safety Model

Preview before each write: use `--dry-run` for authoring, lock, apply, and
recovery. `status` and `doctor` are read-only; doctor checks passive prerequisites
without launching host CLIs, package managers, credential helpers, or servers.
`probe` is the separate command for an explicitly authorized runtime check.

A preview does not authorize a later command to ignore changed inputs. Applying
and recovering retain their own validation and refusal rules. See
[Execution Modes](cli.md#execution-modes) for confirmation, output, and automation.

### NFS-Backed Homes

Single-host NFS use is best effort. Leases do not guarantee cross-node exclusion,
and final revalidation cannot prevent a later competing write. Outage/reconnect
behavior and stable-storage durability are outside the guarantee. See
[Platform Support](platforms.md) and [NFS troubleshooting](troubleshooting.md#nfs-backed-home-or-workspace).
