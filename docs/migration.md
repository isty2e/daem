# Use An Existing Environment

You can start using daem with files already on your machine. Choose whether to
import configuration from an agent's known locations or declare your own local
source paths. Neither approach requires a hosted Git repository.

| Starting point | Use |
| --- | --- |
| Agent instruction files, skills, or supported native configuration already in place | [Import live configuration](#import-live-configuration). |
| Instruction files or skill directories kept elsewhere | [Declare local sources](#declare-local-sources). |
| Skills tracked in a local Git repository | [Use a local Git source](#use-a-local-git-source). |
| An extension already installed by its host | [Register an existing extension](#register-an-existing-extension). |

Keep a backup of files you intend to migrate. Import is not a backup of an
agent's entire configuration, and `recover` is not historical restore.

## Import Live Configuration

Run these commands from the project whose agent files you want to import. For
Codex, a simple starting point is an existing `AGENTS.md` and skills under
`.agents/skills/`. Import uses each target's modeled discovery locations; it
does not recursively search arbitrary directories.

### Preview A New Manifest

When the project has no `daem.toml`, use import instead of `init`:

```bash
daem import --target codex --scope project --dry-run --diff
```

Review the resources, source copies, and skipped entries. Then write:

```bash
daem import --target codex --scope project
```

This creates `daem.toml` and copies imported source material into `daem.d/` by
default. For example, an imported Codex project instruction is copied to
`daem.d/instructions/codex-project.md`. The original host file is left alone.
Import does not create a lockfile or register the live output as daem-owned.

The copied files become your editable sources. Keep them with the manifest;
`daem.d/` is not disposable cache. It is distinct from `.daem/`, which holds
project-local managed state, cache, and recovery data.

### Import Into An Existing Manifest

Use `--merge` when `daem.toml` already exists:

```bash
daem import --target codex --scope project --merge --dry-run --diff
daem import --target codex --scope project --merge
```

Merge requires a valid existing manifest. Conflicts fail before writes; review
and resolve them rather than deleting the manifest or forcing a replacement.
Use `--manifest <path>` on every command if you select a different workspace.

### Read Skipped Entries

Import can observe supported instructions, skills, hooks, standalone MCP
configuration, and source-exact extension declarations. It skips forms that
cannot be represented without loss.

| Category | What it means for your migration |
| --- | --- |
| `action_required` | Correct the source or make the stated authoring decision, then preview again. |
| `unsupported` | This daem version cannot manage that surface. Keep managing it outside daem. |
| `informational` | Discovery or deduplication information; check whether the intended resource was imported elsewhere. |

Use `--verbose` for per-path detail, or `--json` for structured rows; they cannot
be combined. A successful import does not mean every host setting was imported.
See [import limits and skip behavior](cli.md#import) and
[troubleshooting skipped files](troubleshooting.md#import-skipped-an-instruction-hook-or-mcp-file).

### Lock And Review Ownership

Resolve the imported sources:

```bash
daem lock --dry-run
daem lock
daem status
daem apply --manage-existing --dry-run --diff
```

`--manage-existing` asks daem to register eligible outputs that already match
the desired result. Check every planned action: other missing resources can
still be created by the same apply. When the complete plan is acceptable:

```bash
daem apply --manage-existing
daem status --check
```

Bare apply asks for terminal confirmation. Non-interactive execution requires
`--yes`.

Registration requires an exact match, including required metadata for
mode-sensitive files. It grants later reconciliation authority to update or
remove that managed output. It neither imports content nor forces an overwrite.
If an instruction source has been combined with other declarations, or a live
file has changed since import, compare the rendered result rather than assuming
it still matches. Another manifest's claim is an
[ownership conflict](troubleshooting.md#ownership_conflict), even when bytes match.

After adoption, edit the copied source and refresh the lock before applying
changes. Direct edits to a managed output are reported as drift.

## Declare Local Sources

Use this route for files outside the agent's discovery locations, or when you
want to choose the source layout yourself. Keep sources separate from daem's
output paths. For example:

```text
project/
  daem.toml
  instructions/team.md
  skills/review/SKILL.md
```

If there is no manifest yet, run `daem init`. With those source files present:

```bash
daem add instruction team ./instructions/team.md --target codex --dry-run --diff
daem add instruction team ./instructions/team.md --target codex
daem add skill ./skills/review --target codex --dry-run --diff
daem add skill ./skills/review --target codex
```

The skill directory must contain a valid `SKILL.md`; see
[Skill Compatibility](compatibility.md). Each write updates the manifest and
lockfile, not the host files. Continue with `daem apply --dry-run --diff`, then
confirm `daem apply` if the plan is acceptable. For an existing exact matching
output, use the [registration step above](#lock-and-review-ownership).

Project-local paths are relative to the manifest directory. They do not need
`--ref`. Global local sources must be absolute. Local source `mode` and skill
`install_mode` are different fields: selecting a local source does not enable
symlink installation. Only copy placement is currently executable. See
[Sources](manifest.md#sources) and [Skills](manifest.md#skills) for the exact
options.

## Use A Local Git Source

Skills and skill groups can use a local Git repository through an absolute
repository path or `file:///` URL. A ref is still required because this locks
committed content, not your uncommitted working files. For example, in a
project-scoped manifest:

```toml
[[skill]]
name = "review"
source = { git = "/absolute/path/to/agent-skills", path = "skills/review", ref = "main" }
```

Replace the repository path, artifact path, and ref with ones that exist, then
run `daem lock --dry-run` and `daem lock`. The locked source records the resolved
commit. No remote hosting is needed. For live local files instead, use a local
source as above. Git-backed instruction sources are not supported; use a local
instruction file or a supported S3 file source.

## Import Global Configuration

Global scope affects the agent's user-level configuration across projects.
Select both the scope and the user manifest explicitly:

```bash
DAEM_USER_MANIFEST="${XDG_CONFIG_HOME:-$HOME/.config}/daem/daem.toml"
daem import --target codex --scope global --manifest "$DAEM_USER_MANIFEST" --dry-run --diff
daem import --target codex --scope global --manifest "$DAEM_USER_MANIFEST"
```

If that manifest exists, add `--merge` to both import commands. Do not run
`init` before a new-manifest import. Continue with lock, status, and ownership
review, passing `--manifest "$DAEM_USER_MANIFEST"` each time.

Without `--scope`, import selects project scope. Without `--manifest`, a new
import creates `./daem.toml`; `--scope global` does not select the user manifest
for you. `--source-dir` changes where copied sources are stored, not where
import looks for live configuration. Its default is `<manifest-basename>.d`
beside the manifest; see [workspace selection](cli.md#workspace-selection).

## Register An Existing Extension

Declare the installed extension through `daem add extension`, or edit the
manifest and run `daem lock`. Then inspect `daem status`. Continue only when it
reports `carrier adoption available` and the dry-run identifies the exact
source, target, and scope you intend to manage:

```bash
daem apply --manage-existing --dry-run
daem apply --manage-existing
daem status --check
```

Carrier adoption invokes no host install command. Its new claim grants only
the bounded future relation-removal authority shown by the plan, not ownership
of the package store or caches. A lifecycle blocker or source-inexact relation
is not adoptable. See [Host Integrations](host-integrations.md) for supported
routes.
