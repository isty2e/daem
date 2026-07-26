# Host Integration Contract

This advanced reference records the exact target, scope, native command,
observation, and cleanup boundaries behind daem's host integrations. Most
users should start with [Feature Support](features.md).

This contract is implementation and test input. Use the
[Manifest Reference](manifest.md) for public syntax, the
[CLI Reference](cli.md) for commands, [Platform Support](platforms.md) for
operating-system and architecture coverage, and the [Glossary](glossary.md)
for daem-specific terms.

## Product Status Labels

The matrix uses these public status labels:

- `supported`: `lock`, `status`, and `apply` can reconcile the declared
  resource for that target, or the named operation works for its scoped row,
  subject to normal lock/state safety checks.
- `authoring-only`: `add` or `remove` can edit the manifest and lockfile, but
  host state changes still require `apply`.
- `explicit`: the operation runs only through an explicit opt-in flag and keeps
  its guarantees separate from normal reconciliation.
- `diagnostic`: daem can report a typed fact, warning, or blocker, but does not
  mutate that surface.
- `deferred`: not current product syntax or behavior.
- `unsupported`: known not to be reconciled by the current product.
- `blocked`: evidence or authority exists for discussion, but a required gate
  prevents product support or mutation.

## Non-Status Vocabulary

These terms may explain a row but are not additional product statuses:

- `not-modeled`: the product ontology does not currently represent the surface.
- `host-unavailable`: the host does not expose the required operation or scope.
- `bridge-required`: support requires a plugin or extension bridge.
- `observe-only`: policy permits observation but not mutation.
- `rejected`: ingress refuses the declaration or request.
- `out-of-coverage`: the carrier engineering state is outside the reviewed row.

Their relation to a status is row-specific rather than a global conversion
rule.

## Roadmap Posture

Plugin, extension, package, and executable lifecycle support is added one
target, scope, and operation at a time. For host-delegated installation, daem
locks the host request, shows it in dry-run, executes it only after normal apply
confirmation, and reports the attempt at the evidence strength the host makes
available. A row remains blocked only when that bounded contract cannot be
implemented honestly or safely.

## Target Surface And Operation Matrix

| Surface | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Instructions | `supported` | `supported` | `supported` | `supported` | `supported` |
| Skills | `supported` | `supported` | `supported` | `supported` | `supported` |
| Skill groups | `supported` | `supported` | `supported` | `supported` | `supported` |
| Command hooks | `supported` | `supported` | `diagnostic` | `diagnostic` | `unsupported` |
| Standalone MCP server config | `supported` project/global | `supported` project/global | `supported` project/global | `unsupported` core; `deferred` bundled | `supported` global |
| Delegated executable execution | `deferred` | `supported` project MCP | `deferred` | `deferred` | `deferred` |
| Carrier declaration and relation diagnostics | `supported` global | `supported` project/global | `supported` project/global | `supported` project/global | `supported` global |
| Passive carrier observation | `supported` global config | `supported` project/global | `supported` config project/global | `supported` package project/global | `supported` global selector |
| Provider-scoped contribution diagnostics | `diagnostic` cache | `deferred` | `deferred` | `deferred` | `deferred` |
| Host-delegated carrier lifecycle routes | `supported` global | `supported` project/global | `supported` project/global | `supported` project/global | `supported` global |
| Carrier destructive cleanup and prune | `blocked` | `blocked` | `blocked` | `blocked` | `blocked` |
| Runtime probes | `deferred` | `explicit` project MCP | `explicit` project MCP | `deferred` | `deferred` |

### Explicit Carrier Refresh

| Host | Declared scope | Native command | Success evidence | Known broader effects |
| --- | --- | --- | --- | --- |
| Claude Code | project/global | `claude plugin update <plugin>@<marketplace> --scope project/user` | observed relation | cache, dependencies, restart |
| Codex | global | `codex plugin marketplace upgrade <marketplace> --json` | attempted, unverified | marketplace and sibling caches |
| OpenCode | project/global | `opencode plugin <source> --force [--global]` | attempted, unverified | package, config, cache |
| Pi | project/global | `pi update --extension <source>` | attempted, unverified | matching cross-scope packages |
| Antigravity CLI | global | `agy plugin install <source>` | attempted, unverified | bundle and import registry |

Every row is an explicit single-selection operation. Ordinary apply never
selects a refresh route. For carrier rows without an admitted observer,
however, mutating apply
retries the separately locked install/create route on every run; the host may
repair or update host-selected artifacts as a retained effect of that install
attempt. Refresh still does not enable bulk refresh, uninstall, prune,
contribution control, rollback, or runtime-readiness claims. Target-specific
details and retained effects are listed below.

Destructive route behavior was last checked against Claude Code `2.1.216`,
Codex `0.144.5`, OpenCode `1.18.4`, Pi `0.80.10`, and Antigravity CLI `1.1.4`.
A 2026-07-26 non-destructive local smoke check used Claude Code `2.1.220`,
Codex `0.144.5`, OpenCode `1.18.5`, Pi `0.82.0`, and Antigravity CLI `1.1.7`;
their expected default config paths remained available.

The newer local version is not automatically unsupported, but config-path
compatibility is not proof of destructive lifecycle behavior. Exercise
install, refresh, and managed removal one target at a time until that target
has matching-version operation evidence. Incompatible command syntax or
behavior is reported as a failed attempt rather than silently reinterpreted.

## How To Read The Matrix

`supported` means the exact row works; it does not widen the claim to another
scope, source form, operation, or provider-bundled contribution. `admitted` is
reserved for a route or platform shape accepted by its governing contract. An
admitted row may still be diagnostic, deferred, unsupported, or blocked at the
product level.

The following boundaries apply throughout this page:

- managed config projection, delegated lifecycle execution, runtime probing,
  relation removal, and destructive cleanup are independent operations;
- MCP `command` and `args` describe a launch vector, not package installation;
- project declarations do not delete global state, and global declarations
  affect only their managed binding or relation;
- a prior delegated attempt is diagnostic history, not current convergence or
  authority to skip a later attempt; and
- removing a declaration does not remove packages, caches, credentials, trust,
  sessions, logs, or other retained host state unless a separate row says so.

### Instructions

Daem renders locked instruction sources to the documented project or global
instruction file. OpenCode and Pi support project and global defaults.
Antigravity CLI uses project `AGENTS.md` by default, allows project `GEMINI.md`
as an explicit alternate, and uses global `~/.gemini/GEMINI.md` by default.
This does not claim runtime reload, effective host memory, arbitrary instruction
directories, or plugin-provided rules.

### Skills And Skill Groups

Skills install locked Agent Skills-compatible directories into the target's
preferred placement root. `[[skill_group]]` expands one source root into
separately locked child skills. System, builtin, administrator, and
plugin-supplied roots remain outside ownership, as do marketplace discovery,
markdown slash-command files, and host-specific execution success.

### Command Hooks

Codex and Claude Code support managed native command-hook aggregates and
same-scope file assets referenced by `{hook_file:<name>}`. OpenCode and Pi are
diagnostic because their current hook surfaces require extension code.
Antigravity CLI direct hooks are unsupported. Daem does not infer or install
undeclared scripts, directories, standalone tools, trust approval, or
plugin-bundled hooks.

### Standalone MCP Server Config

Supported rows manage one command/args-only stdio entry while preserving
unrelated host configuration. Codex and OpenCode support project and explicit
global rows; Claude Code supports project stdio and command/args-only explicit
global rows; Antigravity CLI supports only the explicit global row. Pi has no
core standalone row. `import` can author these rows, and
`apply --manage-existing` can register exact matching projections.

These rows do not own the executable, package, cache, credentials, trust,
session, runtime health, effective merged host state, remote transports, or
plugin-bundled MCP. Removing a row reconciles only its managed config entry.
Target-specific paths and rejected fields are listed in the
[Manifest Reference](manifest.md#mcp-servers).

### Delegated Executable Execution

Claude Code project MCP rows can disclose and execute the exact locked command
during confirmed apply, then retain sanitized attempt diagnostics and a bounded
post-attempt projection observation. This does not establish package/cache
convergence, runtime or auth readiness, host trust, tool inventory, or future
skip authority. The other target rows remain deferred.

### Carrier Declaration And Relation Diagnostics

`[[extension]]` represents the currently supported host-native plugin or
package relation rows. `add extension` and `remove extension` edit the manifest
and lockfile without host effects. Codex, Claude Code, OpenCode, and Pi package
rows have current exact-relation observers. Antigravity CLI
`PLUGIN@MARKETPLACE` sources have a bounded global relation observer; other
Antigravity host-source forms retain the disclosed no-observer posture. Exact
artifact identity, enabled or trusted state, runtime readiness, refresh,
managed removal, unmanage, and prune are separate rows.
Extension rows have no `on_absent` field: removing a row is desired relation
absence, while the supported `unmanage extension` operation explicitly
preserves host state and releases only daem management.

### Passive Carrier Observation

Claude Code can correlate selected project and explicit-global plugin rows with
fresh version-2 installed-relation data. Codex can correlate an exact
explicit-global `PLUGIN@MARKETPLACE` row from
`$CODEX_HOME/config.toml` `[plugins]`; it never uses marketplace availability,
plugin cache presence, or `codex plugin list` as relation authority. Malformed,
ambiguous, wrong-scope, or unsupported selected data blocks rather than
becoming evidence of absence. Codex doctor also reports observe-only plugin
config diagnostics, while marketplace observation remains deferred.
Pi can correlate exact project or global package rows from the selected
scope's settings file using source-kind-aware npm, Git, and local-path
identity. OpenCode can correlate exact project or default-global host-source
rows across the selected server and TUI JSON or JSONC documents. Antigravity
CLI can correlate a selector-shaped global source by plugin name across
`~/.gemini/config/import_manifest.json` and the matching
`~/.gemini/config/plugins/<plugin>/plugin.json`. Both must be consistently
present for desired-state convergence; malformed, duplicated, mismatched,
partial, unstable, or symlinked selected state fails closed. The host does not
retain marketplace provenance in those files, so source provenance, artifact
version, bundled contributions, trust, and runtime readiness remain unproved.
Lock rejects two distinct Antigravity structural sources that collapse to the
same host-visible plugin name; identical shared carriers remain valid.

### Provider-Scoped Contribution Diagnostics

Codex doctor can report safely enumerated, source-declared contributions from
bounded configured plugin cache manifests. Each row retains `provided_by`,
kind and key, `source_artifact_inspection` provenance,
`current = non-current`, `freshness = fresh`, artifact identity, and a stable
source or blocker reason. Current contribution inventory and
standalone `[[mcp_server]]`, `[[skill]]`, `[[hook]]`, or instruction ownership
are not claimed, and the diagnostic grants no install, readiness, removal, or
apply-skip authority.

### Host-Delegated Carrier Lifecycle Routes

Confirmed apply can invoke the supported host-native install/create command.
Codex, Claude Code, OpenCode, and Pi do so after fresh observed absence and
create removal authority only after fresh exact presence. Antigravity CLI
selector-shaped sources instead combine the exact pending install identity
with fresh bounded plugin-name/import/bundle presence; that passive evidence
remains source-inexact. Opaque or local Antigravity sources retain a locked
attempt route and retry because a successful command alone does not prove
current presence. The target route summaries below list exact commands,
scopes, evidence, and retained effects.

### Managed Carrier Absence

Manifest omission and `remove extension` have the same desired-state meaning.
Managed-claim persistence, generic removal planning and recovery sequencing,
and host-preserving `unmanage extension` are implemented. Codex plugins admit
exact explicit-global managed removal; Claude Code and Pi package rows admit
exact project and global host-route removal; OpenCode plugin rows admit exact
project and global direct config-relation removal; selector-shaped Antigravity
CLI rows admit exact explicit-global host-route removal. Other target-specific
removal plans block before host invocation until their corresponding operation
row is implemented.

### Carrier Residue Prune

All targets are blocked. External-store prune, unrelated dependency/cache
cleanup, credential or trust cleanup, contribution disablement, and
retained-state deletion require independent authority and recovery behavior.
An admitted host-native managed-relation removal may include necessarily
coupled selected artifact deletion, but that exact route envelope does not
admit residue prune.

### Runtime Probes

`probe mcp-server` is explicit for locked Claude Code and OpenCode project stdio
rows. Dry-run discloses the launch; `--yes` attempts MCP initialization under
timeout, cleanup, and redaction rules. Ordinary `lock`, `status`, `doctor`, and
`apply` do not probe. Endpoint health is not applicable to the current stdio
slice; authentication and tool inventory are unsupported.

Explicit extension refresh uses one host-agnostic child-process timeout:
`10m` by default and configurable with `--timeout` from `1s` through `1h` in
whole seconds. The selected value is disclosed and fingerprinted before
authorization. It does not bound planning, confirmation, observation,
history persistence, or cleanup. A timeout after process start is potentially
partial host state and never proves rollback or absence of effects.

### External Carrier Adoption

`apply --manage-existing` can acquire one state-only claim for an already
declared, locked, source-exact external carrier relation. Current support covers
Claude Code project/global, Codex global, OpenCode project/global, and exact
stored-source Pi project/global rows. Adoption requires fresh exact correlation,
no conflicting claim, an available scope-selected claim store, and complete
install/removal lifecycle contracts. It invokes no host route.

Plain status/apply reports an eligible exact row as `present_unclaimed` and
suggests a manage-existing dry-run. A lifecycle-incomplete row is
`present_unclaimed_ineligible` and reports its blocker without suggesting
adoption. Name-only, normalized-equivalent, shadowed, ambiguous, stale, and
source-inexact rows cannot acquire a claim. Antigravity CLI remains in that
last category because its installed state does not retain the declared
marketplace source.

## Codex Plugin Carrier Route Summary

Codex plugin carrier work admits one narrow host-delegated install/create row,
one explicit marketplace-wide refresh row, and exact managed removal for the
explicit-global relation. It does not admit exact plugin artifact convergence,
ordinary update, marketplace prune, runtime readiness, or contribution
ownership. Project-scoped Codex plugin install is product `unsupported` with
reason `host-unavailable`: the current Codex plugin route has no native project
scope.

| Route family | Current product state | What that means |
| --- | --- | --- |
| Carrier declaration and relation diagnostics | `supported` for explicit global `[[extension]]` marketplace selector rows | `carrier = "codex-plugin"` is accepted only with target `codex`, explicit `scope = "global"`, and `source.marketplace = "<plugin>@<marketplace>"`. Defaults do not authorize this global host mutation. `scope = "project"` is product `unsupported` with reason `host-unavailable` for the currently observed native Codex plugin host route, not merely unimplemented in daem. |
| Passive carrier observation | `supported` for the exact explicit-global config relation; `diagnostic` for doctor config entries | `status` and `apply` parse `$CODEX_HOME/config.toml` and select only the exact `[plugins."<plugin>@<marketplace>"]` table. A missing exact row is fresh absence. Exact config correlation is independent of pending installs and claims. Reconciliation separately requires a matching pending install or claim for no-op; an exact external row is `present_unclaimed` until explicit manage-existing acquires its claim. Malformed selected data blocks. Observation never invokes `codex`, treats cache or marketplace visibility as relation presence, or uses `plugin list`; doctor and bounded cache-manifest diagnostics remain separate observe-only surfaces. |
| External carrier adoption | `supported` for the exact explicit-global config relation | `apply --manage-existing --dry-run` discloses the exact claim, selected global claim store, later managed-removal envelope, retained effects, and ambient-consumer limit. Confirmed apply revalidates the same facts and records `explicitly_adopted_observed` provenance without invoking `codex`. |
| Host-delegated carrier lifecycle routes: install/create | `supported` for the explicit global marketplace selector row | Fresh exact config absence selects `codex plugin add <plugin>@<marketplace> --json`. Apply records write-ahead pending authority and creates an exact managed claim only after fresh post-observation sees the selected config row. A converged later apply is a no-op. Command success alone grants no claim, cache convergence, readiness, or contribution ownership. |
| Host-delegated carrier lifecycle routes: explicit refresh | `supported` for the explicit global marketplace selector row | `daem refresh extension <id>` derives the selected marketplace from the validated `PLUGIN@MARKETPLACE` source and invokes `codex plugin marketplace upgrade <marketplace> --json`. The selected relation remains visible separately from the marketplace execution subject. The host may replace the marketplace snapshot and refresh configured installed sibling caches from that root; no-new-revision and changed-revision successes are both `attempted_unverified`. A non-upgrade-capable marketplace, such as a local non-Git source, is a reported host failure rather than a fallback. Partial snapshot/cache effects are retained host state. Daem never follows with plugin add or substitutes another marketplace. |
| Provider-scoped contribution diagnostics | `diagnostic` for doctor-only source-inspected, source-declared, non-current diagnostics from configured plugin cache manifests; current inventory remains `deferred` | `doctor --target codex` may statically inspect bounded `~/.codex/plugins/cache/<marketplace>/<plugin>/<version>/.codex-plugin/plugin.json` artifacts for configured plugins and report provider-scoped source-inspected, source-declared, non-current contribution diagnostics. Each safely enumerated contribution is its own diagnostic row with `provided_by`, kind/key, source marker, `source_artifact_inspection` provenance, `current = non-current`, and `freshness = fresh`; blockers remain provider-level rows with stable reasons. Plugin-bundled MCP, skills, hooks, apps, tools, and commands remain provider-scoped contributions; current Codex passive contribution collection is not admitted. |
| Host-delegated carrier lifecycle routes: managed removal | `supported` for the explicit global marketplace selector row | After exact managed authority and zero remaining daem-known consumers, confirmed apply invokes `codex plugin remove <plugin>@<marketplace> --json`. Convergence requires fresh absence of both the exact config relation and `$CODEX_HOME/plugins/cache/<marketplace>/<plugin>`; exit status or JSON alone is insufficient. Codex removes cache before config, so partial effects retain claim and pending authority. Exact config absence before any pending attempt retires the claim without invocation and intentionally leaves an orphan cache outside implicit prune authority. Marketplace registration/snapshot, same-name plugins from other marketplaces, sibling relations and caches, credentials, trust/session state, unrelated config, and external stores are retained. Ambient non-daem consumers are not discoverable. |
| Host-delegated carrier lifecycle routes: ordinary update | `blocked` by operation | Explicit marketplace refresh does not become ordinary apply reconciliation or exact selected-plugin convergence. |
| Carrier residue cleanup and prune | `blocked` for ordinary mutation | The exact managed-removal envelope does not grant external-store prune, unrelated package/cache cleanup, credential cleanup, trust/session cleanup, or retained-state deletion authority. |
| Runtime readiness and trust | `deferred` | Runtime probes require separate route dossiers; plugin relation or contribution visibility is not runtime health. |

## Claude Code Plugin Carrier Route Summary

Claude Code marketplace `[[extension]]` declarations are implemented for the
project and explicit-global host-delegated install, explicit-refresh, and exact
managed-removal slices.
The public authoring rows remain narrow: `carrier = "claude-code-plugin"`,
target `claude-code`, scope `project` or explicit `scope = "global"`, and
`source.marketplace`. The explicit-global Claude Code plugin row is public daem
`scope = "global"` and projects to host `--scope user` only inside the admitted
delegated lifecycle route. Public `scope = "user"` remains rejected as leaked
host vocabulary. Claude Code `local` plugin scope is product `deferred` with
reason `not-modeled`.

| Route family | Current product state | What that means |
| --- | --- | --- |
| Carrier declaration and relation diagnostics | `supported` for project or explicit-global marketplace rows | `lock`, `status`, and `apply --dry-run` can represent the locked delegated host relation, passive correlation state, replay boundary, retained effects, and non-claims for project and explicit-global rows. Fresh source-exact observations with matching claims are no-op. Source-inexact, shadowed, ambiguous, stale, wrong-scope, or unsupported observations remain blocked or observe-only according to their row. |
| Passive carrier observation | `supported` for selected project or explicit-global installed relations | Status and apply read only selected canonical `PLUGIN@MARKETPLACE` rows from Claude's version-2 `plugins/installed_plugins.json` without invoking Claude. Project rows must carry a canonical-path match for the selected manifest root; explicit-global daem rows correlate only with host `scope = "user"`. Exact factual correlation is independent of pending installs and claims. Reconciliation separately requires a matching pending install or managed carrier claim for no-op; an exact external row is `present_unclaimed` until explicit manage-existing acquires its claim. Project claims live in that manifest's statefile; global claims live in the shared global registry. Unselected row schema drift and attempt history do not block or authorize the selected relation. |
| External carrier adoption | `supported` for source-exact project or explicit-global rows | Manage-existing revalidates the exact selector, host scope, and selected project path where applicable, then records only the project-state or global-registry claim. It invokes no Claude command and does not claim packages, caches, runtime readiness, or ambient exclusivity. |
| Host-delegated carrier lifecycle routes: install/create | `supported` for project or explicit-global marketplace rows; `deferred` for local | When fresh current observation says the locked relation is missing, mutating `apply --yes` delegates to `claude plugin install <plugin>@<marketplace> --scope project` for project rows or `claude plugin install <plugin>@<marketplace> --scope user` for explicit-global rows. Before host execution, daem durably records one exact lock-bound pending carrier install. Fresh exact observed presence promotes it to a managed carrier claim; project claims live in the statefile and global claims in the shared registry. A normally completed invocation that does not establish a claim retires its pending fact, including failed, absent, and attempted-unverified outcomes. Pending may survive an interrupted invocation or lost state authority, but never grants destructive authority by itself. Every later run rereads current host state, and fresh absence retries the route. `host_route_attempts` remain history-only. Exact artifact convergence is not claimed. Local scope carries reason `not-modeled`. |
| Host-delegated carrier lifecycle routes: update/repeat | `supported` for explicit project or global refresh | `daem refresh extension <id>` invokes `claude plugin update <plugin>@<marketplace> --scope project` for project scope or `--scope user` for public daem global scope. Fresh passive evidence must first prove the one exact selected relation present; this may be daem-managed or an exact external relation. The same observer runs afterward, so success means the selected relation is observed present, not that an exact version or artifact converged. Marketplace access, selected version/cache, retained old cache, dependencies, and restart/reload effects remain host-owned and are disclosed before confirmation. |
| Host-delegated carrier lifecycle routes: managed removal | `supported` for project or explicit-global marketplace rows | After exact managed authority, fresh selected-relation presence, and zero daem-known shared consumers are established, confirmed apply invokes `claude plugin uninstall <plugin>@<marketplace> --scope project --keep-data` for project scope or `--scope user --keep-data` for public daem global scope. Completion requires fresh exact relation absence; exit status alone is not convergence. Wrong-scope, failed, and timed-out attempts are re-observed, so an already achieved absence can settle without blind reinvocation while ambiguous evidence retains the claim and pending boundary. Marketplace declarations, versioned or orphaned caches, host metadata, plugin data, dependencies, credentials, trust/session state, sibling relations, and unrelated resources are retained. Daem does not prune, delete individual bundled contributions, prove ambient global consumers, or claim runtime unload/readiness. |
| Provider-scoped contribution diagnostics and bundled MCP/skills/hooks/tools/apps/commands | `deferred` | Plugin-bundled contributions remain provider-scoped future facts and are not imported as standalone `[[mcp_server]]`, `[[skill]]`, `[[hook]]`, or instruction resources. |
| Carrier residue cleanup and prune | `blocked` for ordinary mutation | The scoped host removal envelope does not grant external-store prune, unrelated package/cache cleanup, credential cleanup, trust/session cleanup, or retained-state deletion authority. |
| Runtime readiness and trust | `deferred` | Plugin relation observation or install attempt diagnostics do not prove runtime health, trust approval, command availability, or contribution activation. |

## OpenCode/Pi Plugin-Package Route Summary

OpenCode and Pi plugin/package carrier work admits two narrow host-delegated
install/create rows: OpenCode `opencode-plugin` and Pi `pi-package`. OpenCode
also admits explicit project and global refresh through its force/reinstall
route plus direct exact-relation removal from selected config. Pi admits
explicit project and global package refresh through one cross-scope
selected-source route. Pi direct extension remains deferred.

| Route family | Current product state | What that means |
| --- | --- | --- |
| Carrier declaration and relation diagnostics | `supported` for `opencode-plugin` and `pi-package`; `deferred` for `pi-extension` | `carrier = "opencode-plugin"` is accepted only with target `opencode`, project or explicit-global scope, and `source.host_source`. `carrier = "pi-package"` is accepted only with target `pi`, project or explicit-global scope, and `source.host_source`. Defaults do not authorize global host mutation. `carrier = "pi-extension"` remains a document-only future candidate and current parser behavior rejects it. |
| Host-delegated carrier lifecycle routes: install/create | `supported` for the OpenCode plugin and Pi package rows | Both rows require fresh exact selected-scope absence before invocation and fresh exact presence before creating removal authority. OpenCode invokes `opencode plugin <host-source>` with `--global` only for explicit global scope. Pi invokes `pi install <host-source>` with `-l` only for project scope. `pi -e`, `pi update`, OpenCode `--force`, trust flags, and prompt-policy flags are separate rows. |
| Host-delegated carrier lifecycle routes: explicit refresh | `supported` for OpenCode and Pi project/global host-source rows | OpenCode invokes `opencode plugin <host-source> --force` and adds `--global` only for explicit daem global scope. Pi invokes `pi update --extension <host-source>` for either public scope because the host has no update scope flag; it may inspect and update matching user and trusted-project package rows with the same identity. Neither host has an admitted refresh-specific outcome or version observer, so exit zero is reported only as `attempted_unverified`; Pi's selected-scope relation observer does not prove refresh convergence. History never suppresses a later explicit retry. Pi pins may remain fixed, local paths have no scheduled updater, Git updates may reset/clean and install dependencies, and trust refusal/no match remains a host failure. Ordinary apply/install never uses either refresh route. |
| Passive carrier observation | `supported` for OpenCode plugin and Pi package relations; `deferred` for Pi direct extensions | OpenCode reads the selected project or default-global server and TUI config files, preferring `.json` over `.jsonc` per file kind, and correlates the exact host-source row. Pi reads only the selected project or global settings layer and correlates the exact npm, Git, or local package source. Authored Pi local-path aliases collapse before lock, while external settings still require the exact adapter-derived stored spelling. Malformed, ambiguous, wrong-scope, unsafe, or unreadable state is unavailable, never absence. Visibility alone is not ownership, trust, readiness, contribution inventory, or broad package/cache/store convergence. |
| External carrier adoption | `supported` for OpenCode project/global exact source rows and Pi project/global exact stored-source rows | Manage-existing records a state-only claim only after the current source spelling, target, scope, claim store, and full future removal route remain eligible. Equivalent package names, Git identities, or external local-path spellings that differ from the expected stored row remain inexact and cannot be adopted. No OpenCode or Pi host command runs for claim acquisition. |
| Provider-scoped contribution diagnostics and bundled MCP | `deferred` / `blocked`; no current enumeration | Any future diagnostic row must remain anchored to an admitted passive carrier and preserve `provided_by`. OpenCode native MCP and Pi extension-backed MCP are not standalone `[[mcp_server]]` support. |
| Carrier lifecycle routes: managed removal | `supported` for OpenCode and Pi project/global rows | OpenCode performs no host command: after exact managed authority and last-daem-consumer checks, it removes only the exact source row from the selected server/TUI JSON or JSONC files through per-file compare-and-swap. Partial multi-file success retains durable pending state and retries remaining rows; claim retirement requires fresh absence from every selected file. Pi invokes its selected-scope remove route and verifies relation plus source-kind-specific effects. The other scope, package/cache stores, local source directories, credentials, trust/session/runtime state, unrelated rows, and ambient global consumers remain outside the guarantee. |
| Host-delegated carrier lifecycle routes: disable and ordinary update | `blocked` for ordinary mutation | Explicit refresh does not become ordinary apply update, and no per-contribution disable route is admitted. `opencode uninstall` targets the host program rather than one managed plugin relation. |
| Carrier destructive cleanup and prune | `blocked` for ordinary mutation | Desired absence does not grant external-store prune, unrelated package/cache/store cleanup, trust/session deletion, or retained-state deletion. |
| Runtime readiness and trust | `deferred` | Startup, extension/plugin execution, project trust, auth, tool inventory, and MCP readiness require explicit route/probe dossiers. |

## Antigravity CLI Plugin Carrier Route Summary

Antigravity CLI plugin carrier work supports narrow host-delegated
install/create and explicit repeat-install refresh rows for explicit-global
host-source declarations. Selector-shaped `PLUGIN@MARKETPLACE` sources also
support passive relation observation and exact managed removal. There is still
no Antigravity IDE coverage, project-scope support, import/link support,
dedicated host update command, prune, runtime readiness, or bundled
contribution ownership.

| Route family | Current product state | What that means |
| --- | --- | --- |
| Carrier declaration and relation diagnostics | `supported` for the explicit-global host-source row | `carrier = "antigravity-cli-plugin"` is accepted only with target `antigravity-cli`, explicit `scope = "global"`, and `source.host_source`. Defaults do not authorize this global host mutation. |
| Passive carrier observation | `supported` for selector-shaped explicit-global sources; otherwise unsupported | Daem reads the selected plugin name from the import manifest and matching installed `plugin.json` under `~/.gemini/config`. Desired presence requires a complete, identity-matching pair. Missing state is fresh absence; malformed, duplicated, partial, unstable, or symlinked selected state blocks. Marketplace provenance, artifact freshness, contributions, trust, readiness, and ambient consumers are not observed. |
| External carrier adoption | `blocked` for current Antigravity CLI rows | Complete name/import/bundle evidence does not reconstruct the declared marketplace source. Manage-existing therefore reports source-inexact evidence and never acquires an external claim. Install-created claims and managed removal remain separate supported paths. |
| Provider-scoped contribution diagnostics and bundled MCP/hooks/skills/rules | `deferred` / design state | Plugin-bundled capabilities are provider-scoped contributions with `provided_by` provenance; they are not standalone `[[mcp_server]]`, `[[hook]]`, `[[skill]]`, or `[[skill_group]]` support, and no current command enumerates them. |
| Host-delegated carrier lifecycle routes: install/create | `supported` for the explicit-global host-source row only | Mutating apply invokes `agy plugin install <host-source>`. Selector-shaped sources run only after fresh pair absence. The exact pending install identity plus fresh complete-pair presence creates the managed claim without pretending that the passive pair proves source provenance; later converged apply combines that claim with the same bounded evidence for a no-op. Other source forms preserve unsupported observation, record history-only `attempted_unverified`, and retry. `agy plugin import` and `agy plugin link` remain separate source/provenance setup rows and are not invoked. |
| Host-delegated carrier lifecycle routes: explicit refresh | `supported` for the explicit-global host-source row only | `daem refresh extension <id>` repeats the exact locked `agy plugin install <host-source>` route as an explicit reinstall. Bounded local-source evidence shows selected-bundle replacement without duplicate import rows and malformed-manifest rejection before prior-bundle replacement; other source and failure stages remain host-owned. Relation presence does not prove selected version or bundle freshness, so success remains `attempted_unverified`. |
| Host-delegated carrier lifecycle routes: enable/disable/ordinary update | `blocked` / `unsupported` for ordinary mutation | Enable/disable is whole-carrier activation pressure, not per-contribution control. Explicit repeat-install refresh does not become a dedicated host update command, ordinary apply update, import/link setup, uninstall, or contribution control. |
| Host-delegated carrier lifecycle routes: managed removal | `supported` for selector-shaped explicit-global sources | After an exact managed claim, fresh bounded plugin-name/import/bundle evidence, and zero remaining daem-known consumers, confirmed apply invokes `agy plugin uninstall <plugin>`. The passive evidence remains source-inexact; the claim and locked route dossier provide the separate authority and source identity. Daem never passes the marketplace selector or trusts success prose. Convergence requires both the selected import row and plugin directory to be absent; partial, failed, or uncertain outcomes retain claim and pending authority for retry. Already-absent state retires authority without invocation. Sibling plugins and import rows, credentials, trust/session state, unrelated stores, and IDE state are retained. Marketplace provenance and ambient consumers remain non-claims. |
| Carrier residue prune | `blocked` for ordinary mutation | Desired absence does not prune retained state or delete plugin-bundled contributions; no external-store-prune authority is admitted. |
| Runtime readiness and trust | `deferred` | Plugin-loaded MCP, slash commands, hooks, agents, rules, workflows, auth, trust, and running-session behavior require explicit route/probe dossiers. |

## Operation Matrix

| Operation | Current product status |
| --- | --- |
| `init` | Supported for creating a starter manifest. |
| `add instruction`, `remove instruction` | Authoring helpers that update manifest and lockfile together. |
| `add skill`, `remove skill` | Authoring helpers for direct `[[skill]]` declarations. |
| `add skill-group` | Authoring helper for current `[[skill_group]]` declarations. |
| `add hook`, `remove hook` | Authoring helpers for command-hook declarations; they do not infer hook scripts. `add hook` rejects Antigravity CLI hooks; `remove hook` can clean existing Antigravity CLI hook declarations from the manifest/lockfile without host mutation. |
| `add mcp-server`, `remove mcp-server` | Authoring helpers for supported standalone stdio `[[mcp_server]]` rows. Target omission succeeds only when manifest inheritance and row compatibility identify one supported row. Explicit target/scope forms cover Claude Code project/global, Codex project/global, OpenCode project/global, and Antigravity CLI global rows; defaults never authorize global MCP. They update manifest and lockfile only; host config changes still require `apply`, and command/args authoring is not provisioning. |
| `add extension`, `remove extension` | Authoring helpers for all five supported carrier rows. Add accepts one opaque carrier-native source operand and validates it against target/scope; remove selects the globally unique id with optional safety filters. Both update manifest and lockfile only. Remove expresses desired relation absence; manual row omission plus lock is equivalent. A later confirmed apply executes only removal rows admitted above, currently Codex and selector-shaped Antigravity explicit-global plus Claude Code, OpenCode, and Pi project/global removal. |
| `unmanage extension` | Supported for one exact extension id with optional target/scope safety filters. It atomically removes the declaration/current lock entry and exact daem claim while retaining host state; it supports dry-run/diff/JSON/verbose and has no per-host flags or `--yes`. |
| `lock` | Resolves declared sources and writes current exact lockfile identities. The same floating declaration may resolve differently later; lock does not refresh host carrier state. |
| `outdated` | Read-only source freshness check. |
| `status` | Passive convergence/status reporting for selected locked subjects; runtime readiness remains separate and requires the explicit `probe mcp-server` surface. |
| `apply` | Reconciles supported files/directories/config projections, with rollback/recovery safeguards and state ownership checks. For supported Codex, Claude Code, OpenCode, Pi, and Antigravity CLI extension rows, mutating apply may run the host-delegated install/create route and persist bounded attempt diagnostics. Managed absence is planned generically; Codex and selector-shaped Antigravity rows admit exact explicit-global host-route removal, Claude Code and Pi admit exact project/global host-route removal, and OpenCode admits exact project/global direct config-relation removal with verified claim retirement. |
| `probe mcp-server` | Explicit runtime-readiness command for supported Claude Code project stdio and OpenCode project local-command stdio launch+initialize slices; no state, lock, or host config mutation. |
| `refresh extension` | Explicit single-extension host-refresh workflow with immutable disclosure, confirmation, stale revalidation, bounded attempt history, and no manifest/lock rewrite. Codex explicit-global marketplace, Claude Code project/global marketplace, OpenCode project/global host-source, Pi project/global package, and Antigravity CLI explicit-global host-source relations are supported; every other target/scope/source row remains unavailable until its exact refresh adapter is listed as supported above. |
| `apply --manage-existing` | Registers exact-match state ownership for supported managed outputs and MCP projections; mode-sensitive outputs such as hook assets must also match their required file mode. It also acquires a state-only external carrier claim for the seven source-exact Claude Code, Codex, OpenCode, and Pi rows described above after full lifecycle revalidation. |
| `doctor` | Capability, environment, and passive prerequisite diagnostics; no runtime probes or host mutation. |
| `import` | Imports modeled target-visible roots and supported standalone MCP config rows into manifest-owned declarations when ownership can be reviewed; import itself creates no host ownership. Extension carrier import remains unsupported. External carrier adoption is a separate `apply --manage-existing` path for an already authored and locked declaration. |
| `recover` | Recovery journal replay for interrupted apply transactions. |

## Deferred Product Surfaces

- Public `[[extension]]` parser, lock, status, and apply support outside the
  supported Codex global marketplace-selector, Claude Code project or
  explicit-global marketplace, OpenCode host-source, Pi package host-source,
  and Antigravity CLI explicit-global host-source install/diagnostic slices. The
  `carrier = "pi-extension"` shape remains a document-only future candidate,
  and Antigravity CLI project-scope/import/link/IDE shapes remain future
  candidates, not current parser behavior.
- Codex plugin carrier support outside explicit-global `PLUGIN@MARKETPLACE`
  install/create and explicit marketplace refresh, including ordinary update,
  contribution-disable, prune, runtime-readiness routes, exact-artifact
  convergence, or cleanup authority. Exact explicit-global managed removal is
  current but does not widen into those adjacent operations.
- Claude Code plugin refresh outside the explicit `refresh extension` project
  or global marketplace rows, plus contribution disablement and prune. The
  current install/create, refresh, and exact managed-relation removal support
  does not become ordinary upgrade reconciliation, residue-prune authority,
  runtime readiness, exact artifact convergence, or contribution import.
- Future OpenCode/Pi plugin/package/extension surfaces outside the supported
  OpenCode `opencode-plugin` host-source install/create and explicit-refresh
  rows and Pi `pi-package` host-source install/create, explicit-refresh,
  passive relation observation, and managed-removal rows, including Pi direct
  extension, contribution inventory, ordinary update, prune, runtime
  readiness, trust, and exact artifact/package-store ownership.
- Antigravity CLI surfaces beyond Agent Skills-compatible directory packages,
  the explicit-global command/args-only standalone MCP config projection, and
  the explicit-global host-source plugin install/create and repeat-install
  refresh rows, including markdown slash-command skills, hooks, project-local
  MCP, remote MCP, plugin-bundled MCP, import/link/project-scope plugin rows,
  rules, workflows, settings, runtime readiness, removal for opaque or local
  host sources, and residue prune. Exact managed removal is current only for
  safe selector-shaped explicit-global sources. Antigravity IDE remains
  outside current product coverage.
- Cross-target MCP config projection beyond the Codex project and
  explicit-global command/args-only slices, Claude project stdio and
  explicit-global command/args-only slices, OpenCode project and
  explicit-global command/args-only slices, and the Antigravity CLI
  explicit-global command/args-only slice.
- MCP runtime probes beyond the supported Claude Code project stdio and OpenCode
  project local-command stdio launch+initialize slices and their stdio
  endpoint/auth/tool-inventory support classification, including active OAuth
  flows, HTTP endpoint health checks, and tools/list inventory payload checks.
- Manifest-authored executable lifecycle declaration families such as
  `[[local_parameter]]`, `[[package_runner]]`, `[[executable_artifact]]`, and
  command-object MCP references. These are rejected before Desired
  normalization and remain design-only until an exact
  route/admission row implements parser, normalization, lock, status, apply,
  add/remove scope, docs, and tests for one operation. Current `command` and
  `args` remain launch-vector data, not provisioning or package/cache
  ownership.
- Hook asset directory payloads, plugin-bundled hook installation, standalone
  executable installation, and implicit command path inference.
