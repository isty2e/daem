# Host carrier routes

## Purpose

Define the selected evidence and effects of each admitted native route. [Managed carrier lifecycle](../carrier-lifecycle/spec.md) determines when daem may run a route or settle a claim. The [route adapters](../../../internal/effect/execute/hostroute) construct the native argv, and [host observations](../../../internal/assurance/observe) qualify their results.

## Requirements

### Requirement: Use only the selected global Codex marketplace relation

For explicit-global `codex-plugin`, daem SHALL observe the exact `[plugins."<plugin>@<marketplace>"]` relation in `$CODEX_HOME/config.toml`. Cache, marketplace visibility and `plugin list` do not prove installation. Missing exact config is absence; malformed selected evidence blocks.

| Operation | Route | Required evidence and retained effects |
| --- | --- | --- |
| Install | `codex plugin add <plugin>@<marketplace> --json` | Fresh exact config presence supports a claim and a later converged no-op, not an exact artifact claim. |
| Refresh | `codex plugin marketplace upgrade <marketplace> --json` | Can change the marketplace snapshot and sibling caches. Either changed or unchanged revisions are `attempted_unverified`. An unsupported marketplace fails without a substitute route. |
| Remove | `codex plugin remove <plugin>@<marketplace> --json` | Requires absence of the exact relation and selected `plugins/cache/<marketplace>/<plugin>` path in Codex's root. Cache removal precedes config removal; effects may be partial. Already-absent config before a pending uninstall can settle the claim without invoking Codex or removing orphan cache. |

Removal retains marketplace registration and snapshot, sibling and other-marketplace state, credentials, trust and unrelated stores. Doctor may inspect bounded configured `.codex-plugin/plugin.json` cache manifests. Each enumerated contribution has its provider, kind/key, identity and source or blocker reason with `source_artifact_inspection`, `current = non-current`, and `freshness = fresh`; it does not prove a current installed contribution or authorize managing it separately.

#### Scenario: The exact Codex relation is already absent before removal

- **WHEN** a fresh observation proves the selected config entry absent and there is no pending uninstall that needs coupled proof
- **THEN** the claim can retire without using that absence as permission to delete an orphan cache

### Requirement: Settle Claude Code plugins by exact selected scope

For Claude Code, daem SHALL read the selected version-2 `plugins/installed_plugins.json` inventory without invoking the host for that observation. Project rows require a canonical match to the manifest root; global rows match only host user scope. Unselected schema drift and history neither defeat nor supply selected evidence. Host commands take `--scope project` or `--scope user` according to the locked scope.

| Operation | Route | Required evidence and retained effects |
| --- | --- | --- |
| Install | `claude plugin install <plugin>@<marketplace> --scope <scope>` | Fresh exact presence supports the claim. Normal completion without a claim clears the pending install, including failed, absent or unverified outcomes; interruption or authority loss may retain it. |
| Refresh | `claude plugin update <plugin>@<marketplace> --scope <scope>` | Exact before and after relation evidence, whether managed or external. Does not establish artifact version, cache convergence or restart. |
| Remove | `claude plugin uninstall <plugin>@<marketplace> --scope <scope> --keep-data` | Fresh exact absence settles removal. Later observation can settle a failed or timed-out attempt without invoking again; ambiguity retains pending state. |

Uninstall retains plugin data, marketplace declarations, versioned/orphaned caches, host metadata, credentials, trust, siblings and unrelated dependencies and resources. It does not prove runtime unload.

#### Scenario: A Claude project row is still present in a different project

- **WHEN** the selected managed relation meets exact absence in its own scope after uninstall
- **THEN** retire only that claim and keep the other project's relation outside the result

### Requirement: Keep OpenCode package paths and Pi settings layers separate

Daem SHALL use the four selected OpenCode server and TUI candidates (`opencode.json`, `opencode.jsonc`, `tui.json`, `tui.jsonc`) under the project `.opencode` or default-global settings root, guarding even absent candidates. The manifest-root `opencode.json` used by managed keys and MCP is not a project plugin candidate. OpenCode observation, removal and order do not include its `plugin` rows.

Pi observes the selected settings layer with npm, Git or local-source identity. Authored aliases can normalize before lock; exact adapter-derived stored spelling is still needed for external adoption. An equivalent package, Git identity or local spelling is not enough.

| Host and operation | Route | Required evidence and retained effects |
| --- | --- | --- |
| OpenCode install | `opencode plugin <source>` with `--global` only globally | Fresh exact selected-scope absence before invocation and presence before claim creation. |
| OpenCode refresh | `opencode plugin <source> --force` with `--global` only globally | `attempted_unverified`; resolution, caches, deduplication, dependencies, cross-document host effects and activation remain host-owned. |
| OpenCode remove | Direct config edit, not `opencode uninstall` | Remove the exact row from each existing selected candidate; do not create missing files. Retain comments, whitespace, tuple options, siblings, unknown fields, empty arrays and files. Partial success stays pending until fresh absence from every candidate. |
| Pi install | `pi install <source>` with `-l` only for project scope | New management uses exact absence and presence; no trust or prompt-policy flags. [A managed Git pin change](../carrier-lifecycle/spec.md#requirement-keep-a-managed-pi-git-pin-transition-exact-and-resumable) has its own settlement. |
| Pi refresh | `pi update --extension <source>` in either scope | No scope flag: it may affect matching user and trusted-project rows. `attempted_unverified`; pins may stay fixed, local paths have no updater, and Git may reset/clean or install dependencies. No approval, self-update, model-update or bulk flags. |
| Pi remove | `pi remove <source>` with `-l` only for project scope | Prove selected relation absence and the source-kind-specific coupled effects; a local source isn't itself a managed installation artifact to delete. |

These operations do not grant removal of unrelated stores, local sources, credentials, trust or other-scope state. Pi's correlated provider exposes only the `mcp-client/default` capability. Retire old managed Pi MCP bindings using their stored codec without requiring the new backend's runtime. Retire a provider separately only when no managed consumer remains; external Adapter intent must be handled separately before it can cease blocking Native admission. Changing a backend setting does not migrate the installed contribution.

#### Scenario: An OpenCode installation appears in more than one selected candidate

- **WHEN** removal changes one candidate but cannot verify absence in another
- **THEN** keep the removal pending without creating missing alternatives or deleting an unrelated root config entry

### Requirement: Distinguish selector-shaped and unobserved Antigravity installations

For explicit-global Antigravity CLI selectors, daem SHALL correlate the exact plugin name between `~/.gemini/config/import_manifest.json` and `~/.gemini/config/plugins/<plugin>/plugin.json`. Require a complete matching pair for presence; partial, duplicate, malformed, unstable or symlinked evidence blocks. Different sources collapsing to the same visible name conflict; identical shared carriers do not. Selected file evidence alone has no marketplace provenance.

| Operation | Route | Required evidence and retained effects |
| --- | --- | --- |
| Install | `agy plugin install <source>` | For a safe `PLUGIN@MARKETPLACE` selector require fresh pair absence, then exact pending identity and fresh matching pair presence. Other forms use unobserved retries and `attempted_unverified`. |
| Refresh | Repeat the locked `agy plugin install <source>` | Explicit reinstall, not a distinct update command. Return `attempted_unverified`, not a particular installed version. |
| Remove | `agy plugin uninstall <plugin>` | Selectors only; the operand is the plugin name. Verify absence of the selected import row and plugin directory. A freshly absent pair can retire without invocation; partial or uncertain effects stay pending. |

In the checked local-source refresh route, the host replaces the bundle without duplicating its import row and rejects a malformed `plugin.json` before replacement. This is evidence for that route, not a version claim or a guarantee about other source forms or failure stages.

Retain other import rows, siblings, credentials, trust, unrelated stores, marketplace/source setup and IDE state. No opaque/local-source removal, external adoption, project mutation, import/link setup or contribution enablement/disablement follows from these routes.

#### Scenario: Antigravity CLI reports success but the plugin directory remains

- **WHEN** the exact import row disappeared but the directory did not
- **THEN** removal stays pending rather than treating a partial pair as absence
