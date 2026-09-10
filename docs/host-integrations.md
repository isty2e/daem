# Host Integration Contract

Use this reference to check which native operations daem runs, what it can
verify and what remains afterward. Start with [Feature Support](features.md)
for host selection; [Manifest](manifest.md) owns syntax, [CLI](cli.md) owns
commands and [Platform Support](platforms.md) owns OS/architecture coverage.

## Product Status Labels

| Label | Meaning |
| --- | --- |
| `supported` | The exact resource or operation row works, subject to lock/state checks. |
| `authoring-only` | Edits manifest/lock; host effects still need apply. |
| `explicit` | Requires a separate opt-in; not normal reconciliation. |
| `diagnostic` | Reports facts or blockers without mutating the surface. |
| `deferred` | Not current syntax or behavior. |
| `unsupported` | Not reconciled by this product. |
| `blocked` | A required gate prevents support or mutation. |

## Target Surface And Operation Matrix

| Surface | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Instructions | `supported` | `supported` | `supported` | `supported` | `supported` |
| Skills and skill groups | `supported` | `supported` | `supported` | `supported` | `supported` |
| Command hooks | `supported` | `supported` | `diagnostic` | `diagnostic` | `unsupported` |
| MCP config | project/global | project/global | project/global | project/global via explicit provider | global |
| Delegated executable execution | `deferred` | project MCP | `deferred` | `deferred` | `deferred` |
| Carrier declaration, observation and lifecycle | global | project/global | project/global | project/global package | global; observation/removal require selector |
| Provider contribution diagnostics | cache diagnostics | `deferred` | `deferred` | admitted MCP provider only | `deferred` |
| Destructive cleanup/prune | `blocked` | `blocked` | `blocked` | `blocked` | `blocked` |
| Runtime probes | `deferred` | explicit project MCP | explicit project MCP | `deferred` | `deferred` |

### Explicit Carrier Refresh

`refresh extension` selects one extension explicitly; ordinary apply does not
select refresh routes. Where no observer is admitted, ordinary apply instead
retries its locked install/create route on every run, which may repair or update
host-selected artifacts. Neither path grants bulk refresh, uninstall, prune,
contribution control, rollback or runtime-readiness guarantees. Exact commands are in the
host summaries below.

Refresh bounds only the child process: default `10m`, or `--timeout` from `1s`
through `1h` in whole seconds, disclosed and fingerprinted before authorization.
Planning, confirmation, observation, history persistence and cleanup are outside
that timeout. Timeout after start may leave partial host state.

### Checked Host Versions

| Host | Destructive lifecycle checked | Later config-path-only smoke check (2026-07-29) |
| --- | --- | --- |
| Claude Code | `2.1.216` | `2.1.220` |
| Codex | `0.144.5` | `0.145.0` |
| OpenCode | `1.18.4` | `1.18.7` |
| Pi | `0.80.10` | `0.82.1` |
| Antigravity CLI | `1.1.4` | `1.1.8` |

A newer host is not automatically unsupported, but path compatibility is not
lifecycle verification. Verify install, refresh and removal for that host/version;
incompatible behavior is a failed attempt, not permission to substitute a route.

## How To Read The Matrix

Support applies only to the named target, scope, source and operation.
`admitted` means accepted by a governing route/platform contract, not necessarily
supported product behavior. Config management, host execution, runtime probing,
relation removal and prune are separate operations.

- MCP command/args are a launch vector, not executable provisioning.
- Project declarations do not authorize deleting global state.
- Historical attempts do not prove current convergence or justify skipping work.
- Removal retains packages, caches, credentials, trust, sessions, logs and other
  host state except for the exact coupled effects named in a removal row.

### Instructions

Locked sources render to supported project/global files. Antigravity CLI uses
project `AGENTS.md` by default, optionally project `GEMINI.md`, and global
`~/.gemini/GEMINI.md`. This does not cover runtime reload, effective host memory,
arbitrary instruction directories or plugin rules. See [placements](manifest.md#instructions).

### Skills And Skill Groups

Skills install locked Agent Skills directories at a cataloged default or
compatible `install_to` root. Groups lock children separately with the same root
selection. Other discovery, system, builtin, admin and plugin roots are not
owned. Marketplace discovery, Markdown slash commands and host execution success
are outside directory-skill support.

### Command Hooks

Codex/Claude Code manage native command-hook aggregates and same-scope
`{hook_file:<name>}` assets. OpenCode/Pi hook surfaces need extension code;
Antigravity direct hooks are unsupported. Daem does not infer or install
undeclared scripts, directories, tools, trust approval or bundled hooks.

### MCP Server Config

Supported stdio rows manage one entry and preserve unrelated configuration.
The [MCP schema](manifest.md#mcp-servers) defines exact destinations, fields and
rejections. Import covers core-native rows, not inferred Pi provider relations;
`apply --manage-existing` may register exact matching projections.

| Host/scope | Environment references |
| --- | --- |
| Codex project | None; command/args only. |
| Codex global | Same-name references rendered as `env_vars`. |
| Claude Code project/global | Child/source aliases; global values render as `${SOURCE}`. |
| OpenCode project | None; command/args only. |
| OpenCode global | Aliases rendered as `{env:SOURCE}` in strict `opencode.json`. |
| Antigravity CLI global | Same-name ambient requirements; no native `env` is written. |
| Pi project/global | Aliases rendered as `${SOURCE}` in provider config. |

Lock and durable state retain names, never values. Apply checks fresh source
presence before selected mutation; an empty present value is valid. Values are
resolved later by the host/provider. Antigravity inherits its own process
environment, which daem cannot establish for a future independent invocation;
import cannot infer ambient names. Native Antigravity interpolation is rejected
because it is passed literally, not expanded. Claude's own handling of an absent
source is not a substitute for daem's presence check.

These projections do not own the executable, package/cache, credentials, trust,
session, runtime health, effective merged state, remote transports or bundled
MCP. Pi's explicit provider has its own extension relation; removing an MCP row
removes only its config contribution and retains the provider declaration.

#### Pi Provider

The admitted npm package is `pi-mcp-adapter`, with a canonical exact or
caret-bounded stable selector in `>=2.13.0` and `<3.0.0`. `2.13.0` is the
verified profile floor; inspected `2.15.0` is not a ceiling. Apply observes the
installed stable in-range `2.x` version. Source tags, registry integrity,
`gitHead`, dependency resolution and installed version are distinct evidence.
Provider installation/version and effective config are observed separately.

Project config is `.pi/mcp.json`; global config is the Pi agent-root `mcp.json`.
The provider reads these layers in ascending precedence:

1. `~/.config/mcp/mcp.json`
2. `~/.agents/mcp.json`
3. `~/.agents/mcp/mcp.json`
4. `<Pi agent root>/mcp.json`
5. Project `.mcp.json`
6. Project `.pi/mcp.json`

Daem observes active layers and explicit imports for equivalence, shadowing and
fallback, but writes only the selected Pi-owned file. It observes host-config
discovery when enabled and never enables it silently.

A project provider is ignored without project trust, but a global provider can
read project MCP files and execute unowned eager entries before trust, even
under `--no-approve`. Daem authors only lazy entries and warns on global-provider
sharing. Review unowned project MCP files before using it; daem neither owns nor
sanitizes them. Config convergence proves neither trust, activation, connectivity,
authentication, runtime health nor tools.

### Delegated Executable Execution

Only Claude Code project MCP can execute its exact locked server command during
confirmed apply, with sanitized attempt diagnostics and bounded post-observation.
That does not establish package/cache convergence, runtime/auth readiness, trust,
tools or future skip authority. Other rows remain deferred.

### Carrier Declaration And Relation Diagnostics

`[[extension]]` describes a plugin/package relation, not its store.
`add`/`remove extension` edit manifest and lock only. Omission means desired
relation absence; `unmanage extension` instead releases management and retains
host state. There is no extension `on_absent` field.

### Passive Carrier Observation

Each host summary names the selected current evidence. Malformed, ambiguous,
wrong-scope or unavailable selected state is not absence. Relation presence
alone proves neither ownership, exact artifact/version, enablement, trust,
readiness nor contribution inventory.

### Provider-Scoped Contribution Diagnostics

Codex `doctor` can inspect bounded configured plugin cache manifests, producing
one row per safely enumerated contribution with `provided_by`, kind/key,
`source_artifact_inspection`, `current = non-current`, `freshness = fresh`,
artifact identity and a stable source/blocker reason. Blockers remain provider
rows. This is source-declared information, not current contribution inventory
or standalone resource ownership, and grants no install/readiness/removal/skip
authority.

Pi's admitted provider exposes only the correlated `mcp-client/default`
capability. Other Pi and OpenCode package contributions, and Claude/Antigravity
bundled contributions, remain deferred.

### Host-Delegated Carrier Lifecycle Routes

For observed rows, confirmed apply runs only the locked supported install route
after fresh absence. A managed claim requires its fresh postcondition; command success
alone is insufficient. Selector-shaped Antigravity additionally needs the exact
pending install identity because its passive inventory lacks source provenance.
Other Antigravity source forms retain no-observer retries and history-only
`attempted_unverified` results.

### Managed Carrier Absence

Removal requires exact managed authority, fresh selected evidence and no
remaining daem-known shared consumer. Only the listed removal rows may run;
others block before invocation. Partial or uncertain outcomes retain the claim
and pending state for fresh retry. Ambient non-daem consumers are not discoverable.

### Carrier Residue Prune

Prune is blocked for every target. Coupled artifact deletion in one exact
managed-removal route does not authorize external-store prune, unrelated
package/dependency/cache cleanup, credential/trust cleanup, contribution
disablement or retained-state deletion.

### Runtime Probes

`probe mcp-server` explicitly attempts stdio launch and MCP initialization for
locked Claude Code/OpenCode project rows. Dry-run discloses the launch; `--yes`
uses timeout, cleanup and redaction rules without changing manifest, lock, state
or host config. Ordinary lock/status/doctor/apply do not probe. Endpoint health
is inapplicable to stdio; authentication and tool inventory remain unsupported.

### External Carrier Adoption

`apply --manage-existing` may record a state-only claim for a declared, locked,
source-exact relation: Claude Code project/global, Codex global, OpenCode
project/global or exact stored-source Pi project/global. It invokes no host
route. Fresh correlation, no conflicting claim, an available scope-selected
claim store and complete install/removal contracts are required.

Eligible rows report `present_unclaimed` and suggest a manage-existing preview;
lifecycle-incomplete rows report `present_unclaimed_ineligible` with a blocker.
Name-only, normalized-equivalent, shadowed, ambiguous, stale or source-inexact
rows cannot be adopted. Antigravity lacks recoverable marketplace provenance
and remains ineligible, even though install-created claims support removal.

## Codex Plugin Carrier Route Summary

Only explicit-global `codex-plugin` marketplace selectors are supported.
Project scope is `unsupported` (`host-unavailable`); defaults do not authorize
global mutation. Observation reads only the exact
`$CODEX_HOME/config.toml` `[plugins."<plugin>@<marketplace>"]` table, not cache,
marketplace visibility or `plugin list`. Missing exact config is fresh absence;
malformed selected data blocks. An external exact row needs explicit adoption.

| Operation | Native route | Verification and remaining effects |
| --- | --- | --- |
| Install | `codex plugin add <plugin>@<marketplace> --json` | Fresh exact config presence establishes the claim; a later converged apply is a no-op. No exact cache/artifact or contribution claim. |
| Refresh | `codex plugin marketplace upgrade <marketplace> --json` | Marketplace-wide, not plugin-local: may replace its Git snapshot and refresh sibling caches. Changed and unchanged revisions both return `attempted_unverified`. Non-upgrade-capable marketplaces fail; no plugin-add follow-up or alternate marketplace. Partial snapshot/cache changes remain. |
| Managed removal | `codex plugin remove <plugin>@<marketplace> --json` | Requires fresh absence of both the exact config relation and `$CODEX_HOME/plugins/cache/<marketplace>/<plugin>`. Cache removal precedes config removal; partial effects stay retryable. Config already absent before a pending attempt retires the claim without invocation, deliberately retaining orphan cache. |

Removal retains marketplace registration/snapshot, other marketplaces' same-name
plugins, siblings/caches, credentials, trust/sessions, unrelated config and
external stores. Ordinary update, contribution disablement, prune, exact artifact
convergence and runtime readiness remain unsupported/deferred. Doctor's cache
inspection uses configured
`~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/.codex-plugin/plugin.json`
and is separate from relation authority.

## Claude Code Plugin Carrier Route Summary

`claude-code-plugin` marketplace rows support project and explicit global scope.
Daem global maps to host `--scope user`; public `scope = "user"` is rejected.
Claude `local` scope is deferred (`not-modeled`). Observation reads selected
version-2 `plugins/installed_plugins.json` rows without invoking Claude. Project
rows require a canonical-path match to the selected manifest root; global rows
correlate only host user scope. Unselected schema drift and history do not
block or authorize the selected relation. Project claims use the statefile;
global claims use the shared registry.

| Operation | Native route | Verification and remaining effects |
| --- | --- | --- |
| Install | `claude plugin install <plugin>@<marketplace> --scope project` or `--scope user` | Fresh exact presence establishes a claim. Normal completion without a claim retires pending installation, including failed, absent or unverified outcomes; interruption/authority loss may retain it. Pending alone never grants destructive authority. Fresh absence retries. |
| Refresh | `claude plugin update <plugin>@<marketplace> --scope project` or `--scope user` | Requires exact presence before execution, managed or external, and observes the same relation afterward. Success proves relation presence, not exact version/artifact. Marketplace access, version/cache, old cache, dependencies and restart/reload remain host-owned. |
| Managed removal | `claude plugin uninstall <plugin>@<marketplace> --scope project --keep-data` or `--scope user --keep-data` | Requires fresh exact absence, not exit status. Re-observation can settle absence after a failed/timed-out/wrong-scope attempt without blind reinvocation; ambiguity retains pending state. |

Removal retains marketplace declarations, versioned/orphaned caches, host
metadata, plugin data, dependencies, credentials, trust/sessions, siblings and
unrelated resources. It does not prove runtime unload or approve trust. Bundled
contributions, ordinary update reconciliation, prune and runtime readiness remain
deferred or blocked.

## OpenCode/Pi Plugin-Package Route Summary

`opencode-plugin` and `pi-package` use `source.host_source` for project or
explicit-global scope. `pi-extension` is a rejected future candidate, not syntax.

| Host/operation | Native route | Verification and remaining effects |
| --- | --- | --- |
| OpenCode install | `opencode plugin <host-source>`; add `--global` only globally | Fresh exact selected-scope absence before invocation and presence before claim creation. |
| Pi install | `pi install <host-source>`; add `-l` only for project | Same absence/presence requirement; no implicit `pi -e`, trust or prompt-policy flags. |
| OpenCode refresh | `opencode plugin <host-source> --force`; add `--global` only globally | `attempted_unverified`: relation observation does not prove package/version/refresh convergence. Package resolution, caches, same-family replacement/deduplication, multi-target config, dependencies and activation remain host-owned. |
| Pi refresh | `pi update --extension <host-source>` for either scope | No update scope flag: may update matching user and trusted-project rows. `attempted_unverified`; pins may stay fixed, local paths have no updater, Git may reset/clean and install dependencies. Trust refusal/no match is a host failure. No approval, self-update, model-update or bulk flags. |
| OpenCode removal | Direct config edit; no `opencode uninstall` | Remove only the exact source row from every existing server/TUI JSON/JSONC candidate. Missing candidates are not created; all four paths remain guarded. Partial per-file success stays pending until fresh absence from every loaded candidate. |
| Pi removal | Selected-scope package remove route | Verify relation absence plus source-kind-specific coupled effects; do not infer broad package/cache/store cleanup. |

OpenCode observes all four selected project/default-global server/TUI JSON and
JSONC candidate paths, including absent default JSON candidates. Pi observes
only the selected settings layer with npm/Git/local-source identity. Authored
local aliases normalize before lock, but external settings still need exact
adapter-derived stored spelling for adoption; equivalent package names, Git
identities or local spellings are insufficient.

OpenCode removal preserves comments, whitespace, tuple options, siblings,
unknown fields, empty arrays and files. Other scope, installed artifacts,
caches, local sources, credentials, trust/sessions/runtime and unrelated rows
remain outside that removal. Pi retains other-scope state and unrelated stores;
its selected source-kind effects do not grant prune. Neither explicit refresh
becomes ordinary apply update, and no per-contribution disable is supported.

## Antigravity CLI Plugin Carrier Route Summary

Only explicit-global `antigravity-cli-plugin` host-source rows are supported.
For safe `PLUGIN@MARKETPLACE` selectors, observation correlates plugin name in
`~/.gemini/config/import_manifest.json` with matching
`~/.gemini/config/plugins/<plugin>/plugin.json`. Complete identity-matching
presence is required; missing state is absence, while malformed, duplicated,
partial, unstable or symlinked state blocks. Different structural sources that
collapse to one host-visible name are rejected; identical shared carriers remain
valid. These files do not prove marketplace provenance, version or freshness.

| Operation | Native route | Verification and remaining effects |
| --- | --- | --- |
| Install | `agy plugin install <host-source>` | Selectors require fresh pair absence, then exact pending source identity plus fresh complete-pair presence for a claim. Other forms retry with unsupported observation and `attempted_unverified`. No `import` or `link` setup. |
| Refresh | Repeat locked `agy plugin install <host-source>` | Explicit reinstall, not a dedicated update. Local-source evidence covers bundle replacement without duplicate import rows and malformed `plugin.json` rejection before replacement; other sources/failure stages remain host-owned. Success is `attempted_unverified`. |
| Managed removal | `agy plugin uninstall <plugin>` | Selectors only; pass plugin name, never marketplace selector. Verify absence of both import row and plugin directory. Partial/uncertain outcomes stay pending; already-absent pairs retire without invocation. |

Removal retains siblings/import rows, credentials, trust/sessions, unrelated
stores, marketplace/source setup and IDE state. Opaque/local-source removal,
project plugins, import/link, enable/disable, dedicated or ordinary update,
prune, bundled contribution ownership and runtime readiness are not supported.

## Deferred Product Surfaces

The supported rows above do not extend to other targets, scopes, sources or
operations. In particular:

- Remote/bundled MCP, active OAuth, HTTP health checks and tools/list checks are
  not part of the supported stdio config/probe slices.
- Pi direct extensions and non-admitted bundled contributions remain deferred;
  global provider use does not confer ownership of unowned project config.
- Antigravity IDE, Markdown slash-command skills, direct hooks, project/remote
  MCP, rules, workflows and settings as separate resources remain unsupported.
- `[[local_parameter]]`, `[[package_runner]]`, `[[executable_artifact]]` and
  command-object MCP references are design-only and rejected before normalization.
  Adding one requires its own parser, lock, status, apply, authoring, docs and tests.
- Hook-asset directories, implicit command-path inference, bundled-hook and
  standalone executable installation remain outside current support.
