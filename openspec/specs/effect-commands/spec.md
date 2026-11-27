# Effect command interface

## Purpose

Define the public selection, disclosure and result boundaries for applying, recovering, migrating, probing and refreshing. The [CLI mode rules](../cli-protocol/spec.md) govern authorization and streams. The implementation's lifecycle, recovery, and state rules determine which disclosed work is legal; observing a result is not authority to retry or widen it.

## Requirements

### Requirement: Preview the complete selected apply work

Apply SHALL disclose selected mutations, blockers, route effects, destructive consequences and uncertain postconditions before an interactive decision. Dry-run runs no projected effects, although qualification can observe Pi's version and settings. `--manage-existing` is exact supported adoption, never a takeover of conflicting content. An unqualified selected binding blocks the whole selected plan. Scope and target can narrow work; the command does not silently finish a subset. Source handoffs disclose retirement, fresh replan and conditional acquisition rather than an atomic swap.

Verified retired-journal and completed selected declaration cleanup are distinct from active recovery. Preview names cleanup in order; confirmed apply can clear that disclosed work before normal actions and rechecks the plan. Cleanup alone can require confirmation. A later refusal does not resurrect completed cleanup. Qualified metadata conversion can precede a non-no-op write, not read-only planning or a simple no-op, and does not itself prove apply success. An invocation that first clears recovery records skips that separate conversion.

For whole-tree skill replacement or removal, warn that extra entries will also be removed without claiming that extras were found. A diff is per physical file with one target or the complete consumer set. Bounded collection/rendering, cancellation and omissions keep a text diff from becoming an incomplete, apparently complete plan. `--diff` does not add another permission to write. Human command plans disclose skipped route effects independently of passive observation and source facts.

#### Scenario: Verified cleanup is the only selected work

- **WHEN** a preview shows a retired record that can be cleared and no normal host action
- **THEN** a confirmed cleanup still needs the admitted effect authorization and its outcome is reported separately from ordinary apply

### Requirement: Preserve apply outcome and diagnostic distinctions

The [plan and apply result families](../../../internal/cli/present) SHALL retain distinct projection, delegate, relation, adoption, absence, order and attempt facts. Native key removal, record-only whole-file retirement, and a move of an absent output are not all physical deletion. Active plan rows include prior placements when they determine the transition. Physical order rows identify runtime vs configuration meaning and conditional work waiting on carrier change; recorded foreign/managed precedence changes need the additional applicable consent.

Report confirmed claim transitions even if later work fails; a possible successor after an indeterminate commit is not a receipt. Classify absence as a host route, exact direct config edit, observation-only pending settlement, or state-only retirement. Separate cleanup outcomes from action counts and normal rollback. A `rolled_back` result requires compensation for all attempted effects; an effect outside the compensated set can keep it `incomplete`. Successful confirmed apply omits planning-derived `mcp_statuses`; where failure estimates include them, they are not new post-failure evidence.

Phase, outcome and typed causal facts drive JSON errors. Distinguish cancellation, stale evidence, active journal, cleanup-only authority and continuing file-set fences, including unknown observations. Do not project raw errors, secrets, local attempt internals or unverified process outcomes as facts. A retained historical host-route attempt is not present authority, and cwd authority loss does not overwrite timeout/signal or exit evidence. Public carrier identity disclosure is independent of a source-derived private name. [CLI recovery and apply output](../../../docs/cli.md#apply) shows the result shapes.

#### Scenario: A claim commits before a later route fails

- **WHEN** confirmed apply returns an error after one verified claim transition
- **THEN** the result keeps the confirmed receipt without describing unconfirmed claims or later effects as completed

### Requirement: Select only the admitted recovery authority

`recover` SHALL select the workspace-wide state/recovery paths, not desired resources or a target filter. Active journal rollback/finalization, verified retained cleanup, permission-only recovery and explicit missing-state ownership release are distinct. A continuing declaration file-set fence is not cleared by ordinary journal recovery. `--legacy-user-state` selects only the old default-user operation, and `--baseline-tree` requires verified old skill evidence; backups, not the supplied proof tree, provide restored contents. Missing-state release selects exact settled claims and bindings, can include multiple manifests sharing the statefile, and does not mutate host content.

Authorization of a current interactive plan is not equivalent to retaining a previous `--dry-run` result. `recover --yes` reselects and can choose a different legal kind; it has no `--expect-plan`. Revalidate active backups/leases and every persisted removal intent before retirement. Cleanup-only limits verification and effects to the selected recovery root/control/residue and does not inspect host destinations, state or lock. Blocked evidence and private residue do not authorize cleanup or active replay.

#### Scenario: Journal cleanup takes precedence after a permission preview

- **WHEN** current eligible recovery changes between `--dry-run` and `--yes`
- **THEN** the write selects the current operation and reports its actual kind, rather than treating the old preview as an authority token

### Requirement: Report recovery phases without claiming unavailable retry authority

The [recovery family](../../../internal/cli/present/recover.go) SHALL distinguish planned active and cleanup-only work from retained, completed, retired and unknown authority. A freshly reclassified retained phase can disclose only that current authority. Known retired or unknown authority cannot reuse the preceding action. Terminal journal results omit `authority_kind`, operation directory, classification and old retry actions; continuing file-set fence evidence remains separate. Cleanup-only planning/retained work reports just the cleanup action and zero active obligations, without private retirement paths. GC failure after retirement is still an error but not a retry plan.

Permission recovery reports record settlement and per-destination facts, not earlier exposure or proof of content repair. A completed record phase can coexist with a later validation/lease error. Ownership release counts recorded claims/bindings, not changed host files. `unconfirmed` requires renewed observation rather than assuming no write, and `released` plus an error does not invite replay. Paths and typed reasons distinguish logical targets from private implementation residue.

#### Scenario: Journal authority was retired but garbage collection failed

- **WHEN** a final cleanup failure follows proven retirement
- **THEN** the result reports `authority_retired` and failure without presenting the already-retired recovery action as retryable

### Requirement: Distinguish state transfer from metadata format conversion

`migrate state` SHALL transfer only the default user's legacy management authority to its current XDG location. It needs exact recorded recovery for an interrupted transfer and is not arbitrary relocation, a host replay, or a cure for missing management. A completed repeat can report `already_migrated`. Preview is read-only and does not acquire mutation leases. Its result distinguishes selected state/registry locations and transfer counts from recovery directions, whose counts are omitted.

`migrate metadata` qualifies the selected State and reachable registry cohort without moving management or editing desired/host state. `--reference` validates an asserted current identity rather than proving history or overriding a locator. `--baseline-tree` derives missing shipped-path evidence from a matching original tree, not the current declaration, and is separate from reference or recovery. A recovery for a shared data directory can report the recorded original selection. A known inactive cleanup action is different from active recovery; incomplete inventory and collection are disclosed, not authorization to inspect/delete every residue. Both commands report planning refusal through the common error family and keep a disclosed action on execution error without implying completion or retry authority. Their [result presenters](../../../internal/cli/present) own exact fields and independent versions.

#### Scenario: An operator supplies a reference for a legacy identity

- **WHEN** the current path proves the required identity but had no recorded lookup location
- **THEN** the result distinguishes an observed asserted location from a proven historical path

### Requirement: Disclose and execute only the selected MCP probe

`probe mcp-server` SHALL choose one admitted locked subject, narrowing by target/scope when needed. Preview discloses exact command/args, environment bindings without values, selected-root cwd policy, timeout, possible external effects and cleanup. Confirmed interactive execution uses that request even if declaration or lock bytes change while awaiting the answer. It can launch the exact locked stdio command to attempt protocol initialize, not edit manifest, lock, managed state or host config. Read-only status does not start this probe.

The probe reports launcher, initialize, authentication, endpoint health and tool inventory separately. Unsupported or inapplicable dimensions do not become an aggregate ready/healthy result. None of the evidence is a persistent install/config/trust, package/cache or skip guarantee. [Probe JSON](../../../internal/cli/present/probe.go) preserves the selected operation, effect disclosure and ordered bindings without exposing values.

#### Scenario: Lock contents change while probe waits for confirmation

- **WHEN** a reviewed interactive probe is authorized
- **THEN** the approved command cannot be replaced with a newly locked command by that edit

### Requirement: Refresh one selected locked extension without broadening authority

`refresh extension` SHALL select one exact declaration and matching refresh route. Target and scope are filters, not alternate destinations. No bulk/wildcard, ordinary apply refresh, or generic update alias is implied. The timeout defaults to `10m` and admits whole-second `1s` through `1h`; it bounds one delegated child attempt, not planning, confirmation, observation or cleanup. Immutable disclosure contains the selected timeout, root, exact argv, environment names and host effects. Runtime outcomes use the [host route contract](../../../docs/host-integrations.md), not a generic product update claim.

After disclosure, changes to applicable authority, lock or host facts refuse. A route that requires exact presence observes it before and after; an explicitly unobserved route reports success only as attempted-unverified. Already-applied native keys and whole files are qualified under their rewrite or physical-separation boundaries. Refresh neither adopts them nor applies pending changes. Permissions observed just before launch give the access limit; daem removes access added by the child even on failure, retains tightening, and does not automatically repair changed content. Once the selected process starts, no retry, automatic rollback or substitute install is implied.

The result distinguishes planned, refused, cancelled, observed relation, attempted-unverified, failed and partial. Timeout/cancellation/signals after start or failed required post-observation/persistence may be partial. Attempted is separate from completion and from the current relation. Preserve independent process outcome, workdir authority and both recovery-barrier axes. The detail comes from typed facts, without raw process/error output or secret values. Before a result is selected, follow the common failure rule; afterward the result remains in its own stream/family, including under the explicit stderr authorization exception.

#### Scenario: The host process times out after launch

- **WHEN** refresh observed start but the child could not finish within the disclosed timeout
- **THEN** its result does not claim no host effects or automatically retry a different route
