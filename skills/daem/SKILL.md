---
name: daem
description: Manage agent environments with daem. Use when asked to install, add, remove, update, import, inspect, repair, or synchronize agent instructions, skills, skill groups, hooks, MCP servers, plugins, or extensions. Route managed changes through the selected manifest, lockfile, and apply workflow instead of editing agent configuration or installation directories directly.
---

# Daem

Use `daem` as the authority for declared agent-environment changes. Translate
the user's intent into the current CLI surface, preview the resulting plan, and
verify convergence. Do not reproduce host-specific configuration or installer
logic in this skill.

## Operating Contract

- Check `command -v daem` and `daem version` before planning a mutation. If the
  executable is unavailable, explain the bootstrap requirement and stop rather
  than bypassing daem.
- Run `daem help <command>` or `daem help <command> <resource>` before relying
  on a leaf command. The installed executable, not this skill, owns current
  target, scope, source, and capability support.
- Preserve an explicit `--manifest` supplied by the user. Otherwise omit it and
  let daem select an existing `./daem.toml`, then its user manifest. Never
  search parent directories or invent another workspace.
- Do not add target or scope selectors unless the user requested them or daem
  requires them to resolve ambiguity. Omission may preserve manifest
  inheritance.
- Prefer `daem add` and `daem remove` for curated authoring. They validate a
  prospective manifest and lockfile and commit both together.
- Never write agent installation directories or host configuration directly,
  and never invoke a host-native plugin installer as an unreported fallback.
- Do not hardcode agent paths, host commands, or a capability matrix. Report
  daem's diagnostics when a route is unsupported or ambiguous.

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

Do not force an ambiguous object into the nearest resource kind. Inspect
`daem help add <resource>` and ask only when the user's intent still cannot be
represented without guessing.

For removal, obtain the exact resource key from `daem list resources`, then use
`daem remove <resource> <resource-key>`. Skill groups are removed through
`daem remove skill <resource-key>`.

For Pi MCP, `daem add mcp-server --target pi` may author an explicit
`pi-mcp-adapter` extension alongside the binding. Treat both declarations as
intentional: removing the MCP row keeps the provider, and removing the provider
is a separate extension lifecycle decision. Never describe this as Pi
core-native MCP or infer trust/runtime readiness from successful projection.

For extensions, distinguish two removal intents:

- Use `daem remove extension` when the user wants the declared relation absent.
  A later apply may execute a supported removal route under daem authority.
- Use `daem unmanage extension` when the user wants daem to release management
  while retaining host-installed state.

## Safety Gates

- Successful authoring does not authorize host effects. Always inspect the
  apply or runtime-operation preview before execution.
- An explicit request to perform an exact install, update, or removal
  authorizes only its ordinary matching effects.
- Obtain additional approval before adopting existing state with
  `--manage-existing`, deleting shared or global state not named in the
  request, or accepting a materially different destructive plan.
- Never use `--yes` to bypass a blocker, stale state, failed validation, or
  unsupported capability.
- Do not infer current installation, ownership, runtime readiness, or removal
  success from historical command evidence.
- Do not silently fall back to direct file writes or host-native commands.

## Perform A Change

1. Inspect the selected workspace and current identity:

   ```bash
   daem list resources
   daem status
   ```

   Use `--json` when structured output is needed. If workspace selection fails,
   preserve the diagnostic and establish whether the user wants a project or
   user workspace before running `daem init`.

2. Preview curated authoring with the exact intended selectors:

   ```bash
   daem add <resource> ... --dry-run --diff
   daem remove <resource> ... --dry-run --diff
   ```

   Inspect the resource change, manifest path, lockfile result, and errors.
   Then rerun the same command without `--dry-run --diff`. Do not manually
   repeat `daem lock`; successful add/remove already refreshes the lockfile in
   the same transaction.

3. If required fields appear to be manifest-only, re-read
   `daem help add <resource>` before editing. When no curated flag represents
   the request:

   - Read the selected manifest and record the exact old text for every intended
     hunk.
   - Apply only narrow patches that require that old text to still match. Never
     replace the whole file or use a whole-file backup and restore sequence.
   - Run `daem lock --dry-run`.
   - On failure, apply the inverse patch only while every new hunk still matches
     exactly. If any hunk changed, stop and report the concurrent edit instead
     of overwriting it.
   - After a successful preview, run `daem lock` to persist the exact result.

   Never continue to apply with a stale or failed lock.

4. Preview host effects:

   ```bash
   daem apply --dry-run --diff
   ```

   Read every blocker, delegated route, destructive implication, retained
   effect, and uncertain postcondition. Do not treat a clean authoring result
   as proof that apply is safe or supported.

5. Execute only after the Safety Gates are satisfied. In non-interactive
   execution, use `daem apply --yes` only after the required authorization.

6. Verify with:

   ```bash
   daem status --check
   ```

   Use `daem doctor` for prerequisite diagnostics. Report remaining drift,
   unsupported routes, and uncertain postconditions rather than claiming
   convergence from process exit alone.

## Update, Import, And Recovery

- For lockable source updates, run `daem outdated`, preview with
  `daem lock --dry-run`, write with `daem lock`, then follow the apply
  workflow.
- Use `daem refresh extension` only for an explicitly selected extension route
  supported by the installed daem. Preview it before execution.
- To bring existing host state under declaration, start with
  `daem import --dry-run`. Import does not write a lockfile. After a successful
  import, run the lock and apply previews separately.
- Use `daem apply --manage-existing --dry-run` only when the user explicitly
  wants daem to adopt eligible exact existing state. Never infer adoption from
  matching files alone.
- If an apply was interrupted, stop ordinary work and run
  `daem recover --dry-run`. Execute recovery only after its actions are
  disclosed and authorized.

## Failure Rules

- Do not repair manifest or host state by hand after an add/remove failure;
  those authoring commands are transactional.
- When daem rejects or cannot represent a route, explain the unsupported
  boundary and preserve the user's environment.
