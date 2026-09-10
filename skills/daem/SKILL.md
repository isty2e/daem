---
name: daem
description: Manage agent environments with daem. Use when asked to install, add, remove, update, import, inspect, repair, or synchronize agent instructions, skills, skill groups, hooks, MCP servers, plugins, or extensions. Route managed changes through the selected manifest, lockfile, and apply workflow instead of editing agent configuration or installation directories directly.
---

# Daem

Use `daem` as the authority for declared agent-environment changes: translate user intent into the current CLI surface, preview the plan, and verify convergence. Do not reproduce host-specific configuration or installer logic in this skill.

## Operating Contract

- Before planning a mutation, check `command -v daem` and `daem version`. If the executable is unavailable, explain the bootstrap requirement and stop; do not bypass daem.
- Before relying on a leaf command, run `daem help <command>` or `daem help <command> <resource>`. The installed executable owns current target, scope, source, and capability support—not this skill.
- Preserve the user's explicit `--manifest`; otherwise omit it. Existing-workspace commands select `./daem.toml`, then the user manifest. `init` and non-merge `import` instead create `./daem.toml`; `import --merge` uses existing-workspace selection. Never search parent directories or invent a workspace.
- Add target/scope selectors only if requested or required by daem to resolve ambiguity; omission may preserve manifest inheritance.
- Never write agent installation directories or host configuration directly. Do not silently fall back to direct file writes or host-native commands, including plugin installers.
- Do not hardcode agent paths, host commands, or a capability matrix. Report daem's diagnostics for unsupported or ambiguous routes.

## Safety Gates

- Successful authoring does not authorize host effects. Always inspect the apply or runtime-operation preview before execution.
- An explicit request to perform an exact install, update, or removal authorizes only its ordinary matching effects.
- Obtain additional approval before adopting existing state with `--manage-existing`, deleting shared/global state not named in the request, or accepting a materially different destructive plan.
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

## Map User Intent

Use these public resource names:

| User intent | Daem surface |
| --- | --- |
| Instruction or agent rules | `instruction` |
| Skill | `skill` |
| Related skills selected together | `skill-group` when authoring, `skill` when removing by resource key |
| Hook | `hook` |
| MCP server | `mcp-server` |
| Host plugin, extension, or package | `extension` when current daem supports the selected host and source |

Do not force an ambiguous object into the nearest kind. Inspect `daem help add <resource>`; ask only if intent still cannot be represented without guessing.

For removal, get the exact resource key from `daem list resources`, then use `daem remove <resource> <resource-key>`. Remove skill groups with `daem remove skill <resource-key>`.

For Pi MCP, `daem add mcp-server --target pi` may author an explicit `pi-mcp-adapter` extension alongside the binding. Treat both as intentional: removing the MCP row keeps the provider; removing the provider is a separate extension lifecycle decision. Never describe this as Pi core-native MCP or infer trust/runtime readiness from successful projection.

For extensions, distinguish the user's removal intents:

- To make the declared relation absent, use `daem remove extension`; a later apply may execute a supported removal route under daem authority.
- To release management while retaining host-installed state, use `daem unmanage extension`.

## Manual Manifest Edits

Before editing, re-read `daem help add <resource>`. Proceed only if no curated flag represents the request.

For syntax, consult the [Manifest Reference](https://github.com/isty2e/daem/blob/main/docs/manifest.md). That link follows `main`; select the matching Git tag for a released executable, or the matching source revision for a source build (`daem version`).

- Read the selected manifest and record each intended hunk's exact old text.
- Apply narrow patches requiring that old text to match. Never replace the whole file or use a whole-file backup-and-restore sequence.
- Run `daem lock --dry-run`.
- On failure, apply the inverse patch only while every new hunk still matches exactly. If any changed, stop and report the concurrent edit; do not overwrite it.
- After a successful preview, run `daem lock` to persist the exact result.

Never apply with a stale or failed lock.

## Update, Import, And Recovery

- Lockable source updates: run `daem outdated`, preview with `daem lock --dry-run`, write with `daem lock`, then follow the apply workflow.
- Use `daem refresh extension` only for an explicitly selected extension route supported by the installed daem. Preview before execution.
- Existing host state under declaration: start with `daem import --target <target> --dry-run`; repeat `--target` when needed. Import writes no lockfile. After successful import, run lock and apply previews separately.
- Use `daem apply --manage-existing --dry-run` only for explicit intent to adopt eligible exact existing state. Never infer adoption from matching files alone.
- Interrupted apply: stop ordinary work and run `daem recover --dry-run`. Disclose and authorize its actions before recovery execution.

## Failure Rules

- Do not repair manifest or host state by hand after add/remove failure; these authoring commands are transactional.
- When daem rejects or cannot represent a route, explain the unsupported boundary and preserve the user's environment.
