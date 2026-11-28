---
name: daem
description: Use daem for declared agent-environment management when requested or no other method is specified. Do not trigger on mere mentions, skill-source editing/review, or temporary local experiments; honor the user's chosen method.
---

# Daem

Use `daem` by default for persistent declared environment management. This skill governs daem operations, not other user-authorized workflows, including temporary patches to installed packages.

## Operating Contract

- Before a daem mutation, check `command -v daem` and `daem version`. If unavailable, report that limitation; use another method if the user has authorized it.
- Before relying on a leaf command, run `daem help <command>` or `daem help <command> <resource>`. The installed executable owns current target, scope, source, and capability support—not this skill.
- Preserve the user's explicit `--manifest`; otherwise omit it. Existing-workspace commands select `./daem.toml`, then the user manifest. `init` and non-merge `import` instead create `./daem.toml`; `import --merge` uses existing-workspace selection. Never search parent directories or invent a workspace.
- Add target/scope selectors only if requested or required by daem to resolve ambiguity; omission may preserve manifest inheritance.
- Do not silently substitute direct edits or host-native commands for daem; follow an explicitly authorized choice of method.
- Do not hardcode agent paths, host commands, or a capability matrix. Report daem's diagnostics for unsupported or ambiguous routes.

## Safety Gates

- Successful authoring does not authorize host effects. Always inspect the apply or runtime-operation preview before execution.
- An explicit request to perform an exact install, update, or removal authorizes only its ordinary matching effects.
- Obtain approval for adoption with `--manage-existing`, unnamed shared/global deletion, or materially different destructive effects unless already authorized.
- Never use `--yes` to bypass a blocker, stale state, failed validation, or unsupported capability.
- Do not infer current installation, ownership, runtime readiness, or removal success from historical command evidence.

## Perform A Change

1. Inspect the selected workspace and current identity:

   ```bash
   daem list resources
   daem status
   ```

   Use `--json` when structured output is needed. If workspace selection fails, preserve the diagnostic and establish project versus user workspace intent before `daem init`.

2. Prefer `daem add` and `daem remove` for curated authoring: they validate the prospective manifest and lockfile and commit both together. Use the [public resource names](#map-user-intent) and preview with the exact intended selectors:

   ```bash
   daem add <resource> ... --dry-run --diff
   daem remove <resource> ... --dry-run --diff
   ```

   Inspect the resource change, manifest path, lockfile result, and errors; rerun the same command without `--dry-run --diff`. Do not manually repeat `daem lock`: successful add/remove already refreshes the lockfile in the same transaction.

   MCP `--env-from CHILD=SOURCE` takes names, not other tools' `KEY=value` literals: keep the value in the host environment and pass its uppercase variable name literally, without shell expansion. Existing-server edits use the manual-manifest flow, not remove/re-add.

3. For apparently manifest-only fields, follow [Manual Manifest Edits](#manual-manifest-edits) before continuing.

4. Preview host effects:

   ```bash
   daem apply --dry-run --diff
   ```

   Read every blocker, delegated route, destructive implication, retained effect, and uncertain postcondition. Clean authoring is not proof that apply is safe or supported.

5. Execute only after the Safety Gates are satisfied. For non-interactive execution, use `daem apply --yes` only after required authorization.

6. Verify with:

   ```bash
   daem status --check
   ```

   Use `daem doctor` for prerequisite diagnostics. Report remaining drift, unsupported routes, and uncertain postconditions; do not claim convergence from exit alone.

   A passing check can still carry error-grade diagnostics; report them rather than claiming success.
   When status JSON includes `check_clean`, read it and `has_errors` as independent fields under the [status contract](https://github.com/isty2e/daem/blob/main/docs/cli.md#status) for the installed release or source revision.
   Neither establishes runtime readiness.

## Map User Intent

Use these public resource names:

| User intent | Daem surface |
| --- | --- |
| Instruction or agent rules | `instruction` |
| Skill | `skill` |
| Related skills selected together | `skill-group` when authoring, `skill` when removing by resource key |
| Hook | `hook` |
| Source-backed hook payload | `hook_asset` manifest declaration; no direct add/remove command |
| MCP server | `mcp-server` |
| Host plugin, extension, or package | `extension` when current daem supports the selected host and source |

Do not force an ambiguous object into the nearest kind. For kinds with curated authoring, inspect `daem help add <resource>`; ask only if intent still cannot be represented without guessing.

For a kind supported by `remove`, get the exact kind and key from `daem list resources`, then use `daem remove <kind> <key>`. Preserve keys literally. Do not derive operands from `resource_id`, canonical `entity_id`/`subject_id`, or a `skill_group[index]` membership anchor. Group members remain `skill` and are removed with `daem remove skill <key>`; there is no remove skill-group command.

An inventory row with `kind=hook_asset` identifies a manifest declaration, not a remove operand. Use the manual-manifest flow below for it and any affected hook references; do not substitute `remove hook` or invent an asset authoring command. Unreferenced asset rows have `targets=""` and remain visible under target filters; listed reference targets do not prove runtime support or installed state.

When consuming JSON, check the envelope family's `schema_version` against the [CLI Reference](https://github.com/isty2e/daem/blob/main/docs/cli.md#json-schema-contract) matching the installed executable's release or source revision; reject unsupported versions rather than assuming future versions are compatible. Check installed help before reusing kinds from older inventory output, which may use internal rather than CLI spelling.

For Pi MCP, inspect the recorded backend and current daem diagnostics. New automatic authoring may record Native from admitted Pi version evidence or retain Adapter with a diagnostic; existing declarations never migrate automatically. Adapter may author an explicit `pi-mcp-adapter` extension alongside the binding. Removing the MCP row keeps that provider; package removal is a separate lifecycle decision. Native qualification never authorizes settings repair, provider removal or backend fallback. Do not infer trust, activation or runtime readiness from version or config convergence.

For extensions, distinguish the user's removal intents:

- To make the declared relation absent, use `daem remove extension`; a later apply may execute a supported removal route under daem authority.
- To release management while retaining host-installed state, use `daem unmanage extension`.

## Manual Manifest Edits

Prefer curated authoring when it represents the request, unless the user chooses manual editing. `hook_asset` has no curated add/remove route; preserve declarations and references when editing it.

For syntax, follow the resource specification linked from the [manifest guide](https://github.com/isty2e/daem/blob/main/docs/manifest.md). That link follows `main`; select the matching Git tag for a released executable, or the matching source revision for a source build (`daem version`). In older revisions without OpenSpec, use that revision's manifest reference.

- Read the selected manifest and record each intended hunk's exact old text.
- Apply narrow patches requiring that old text to match. Never replace the whole file or use a whole-file backup-and-restore sequence.
- Run `daem lock --dry-run`.
- On failure, apply the inverse patch only while every new hunk still matches exactly. If any changed, stop and report the concurrent edit; do not overwrite it.
- If the preview succeeds, review it before running `daem lock`. The write checks recovery separately and resolves sources again.

Never apply with a stale or failed lock.

## Update, Import, And Recovery

- Lockable source updates: start with `daem outdated`. If it reports a pending local file-set record, follow Interrupted Work below before attempting a write. Otherwise, preview with `daem lock --dry-run`, write with `daem lock`, then follow the apply workflow.
- Managed Pi Git commit-pin changes: when supported by the installed daem, keep the extension ID, repository locator and scope unchanged, then use manual manifest edit → lock → apply. Inspect the disclosed old/new pins, checkout reset/clean and dependency-script effects. Do not silently substitute refresh, unmanage, or remove/reinstall.
- An uncertain pin attempt retains management and its exact target. Obtain a new preview and authorization before retrying that same target; settings alone do not complete it. Report unreadable or conflicting settings instead of guessing. Metadata recovery is not native package rollback.
- Use `daem refresh extension` only for an explicitly selected extension route supported by the installed daem. Preview before execution.
- Existing host state under declaration: start with `daem import --target <target> --dry-run`; repeat `--target` when needed. Import writes no lockfile. After successful import, run lock and apply previews separately.
- Use `daem apply --manage-existing --dry-run` only for explicit intent to adopt eligible exact existing state. Never infer adoption from matching files alone.

### Interrupted Work

Keep the original workspace and applicable recovery options. Show the user the recovery plan before asking for approval; approval for the interrupted write does not approve a follow-up recovery. For `daem recover`, use `--yes` only if the user authorizes the work selected when the command runs, not just a previous `--dry-run`. The work can change even within the same recovery kind. If approval covers only the earlier preview, use interactive `recover` (omit `--dry-run`, `--json`, and `--yes`) and confirm only work the user approves from the current plan. With that limited approval, stop without writing if interactive confirmation is unavailable.

- Interrupted apply or an Apply journal left by `replace agent-config`: stop ordinary work and run `daem recover --dry-run`.
- Interrupted `import agent-config`: do not resend the write. Preview the same request with `--dry-run --json`, even if another command reports invalid file-set evidence or says a newer daem wrote marker version 4. Read `operation` only from a complete, successful preview. For `recovery`, review the files and direction, obtain separate approval, and pass that plan's `fingerprint` to `--yes --expect-plan`. For `capture`, inspect the earlier outcome before seeking approval for a new capture. See [Retrying `import agent-config`](https://github.com/isty2e/daem/blob/main/docs/cli.md#retrying-import-agent-config).
- Interrupted `add` or `remove`: use `daem recover --declarations --dry-run` with the same manifest selection. Authorize settlement separately from any edit the user still wants to make.
- If `outdated` stops at a local file-set record, do not infer that the edit completed. For a requested lock refresh, review `daem lock --dry-run` before writing. A `lock` write can clear a verified completed declaration pair and re-resolves floating sources. For a requested apply, preview with `daem apply --dry-run` to see eligible cleanup. Follow the [interrupted-operations guide](https://github.com/isty2e/daem/blob/main/docs/troubleshooting.md#manifest-metadata-update-was-interrupted) for any remaining record.
- Default-user-manifest legacy state: use `daem migrate state --dry-run`, preserving explicit `--manifest`, before authorizing metadata-only transfer. Interrupted migration uses `migrate state --recover --dry-run`; old apply journals use `recover --legacy-user-state --dry-run`. Do not copy/delete state or reinstall outputs to bypass the fence. For legacy authoring metadata, use the matching previous writer's recovery route with the original workspace selection. Changed HOME/XDG namespaces and cache relocation are outside this migration.

## Failure Rules

- After failed daem authoring, inspect the transaction before proposing repairs; do not make unapproved manifest or host changes.
- A daem limitation is not a prohibition on the user's task; explain it and honor an authorized alternative.
