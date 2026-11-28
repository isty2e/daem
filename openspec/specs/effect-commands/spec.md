# Effect command interface

## Purpose

Define the public selection, disclosure and result boundaries for applying, recovering, migrating, probing and refreshing. The [CLI mode rules](../cli-protocol/spec.md) govern authorization and streams. [Apply lifecycle](../apply-lifecycle/spec.md), [interrupted operations](../interrupted-operations/spec.md) and [metadata migration](../metadata-migration/spec.md) define which disclosed work can proceed. Observing a result is not authority to retry or widen it.

## Requirements

### Requirement: Preview the complete selected apply work

Apply SHALL disclose selected mutations, blockers, route effects, destructive consequences and uncertain postconditions before an interactive decision. Dry-run runs no projected effects, although qualification can observe Pi's version and settings. `--manage-existing` is exact supported adoption, never a takeover of conflicting content. An unqualified selected binding blocks the whole selected plan. `--target` can narrow work; the command does not silently finish a subset. Source handoffs disclose retirement, fresh replan and conditional acquisition rather than an atomic swap.

Verified retired-journal and completed selected declaration cleanup are distinct from active recovery. Preview names cleanup in order; confirmed apply clears that work before normal actions and rechecks the plan. Interactive apply asks even if cleanup is the only work. If cleanup fails or the plan changes, apply stops and a retry needs a fresh preview; already cleared records stay cleared. Qualified metadata conversion can precede a non-no-op write, not read-only planning or a simple no-op, and does not itself prove apply success. An invocation that first clears recovery records skips that separate conversion.

For whole-tree skill replacement or removal, warn that extra entries will also be removed without claiming that extras were found. A diff is per physical file with one target or the complete consumer set. Collection admits 4 MiB of total current/desired bytes per file, 16 MiB per operation, and 4,096 managed-path decisions. Rendering admits 250,000 LCS cells per file and 16,000,000 per report. Overflow gets explicit omission facts or one aggregate omission count, not a seemingly complete prefix. Cancellation is checked before decisions and reads and between files. `--diff` grants no write permission.

#### Scenario: Verified cleanup is the only selected work

- **WHEN** a preview shows a retired record that can be cleared and no normal host action
- **THEN** a confirmed cleanup still needs the admitted effect authorization and its outcome is reported separately from ordinary apply

### Requirement: Preserve apply outcome and diagnostic distinctions

The [plan and apply result families](../../../internal/cli/present) SHALL retain distinct projection, delegate, relation, adoption, absence, order and attempt facts. An already absent native key still reports `delete`/`removed_from_manifest`; retiring an absent dedicated file reports `record` with that reason and no file mode. Moving an absent file within one agent is `update` with `previous_state`; across agents the old record is a separate `record`/`state_stale`. Narrowing a native document is a separate `native_narrowing_actions` row with desired key count (zero can release) and `values_omitted`, not another projection action. A row for shipped-directory replacement has `removes_additional_entries: true`; an unchanged copied skill's hash is for its verified shipped content. Claude global hook `resolved_path` is separate from its portable destination.

Order rows name a physical sequence and its logical class, revision, managed members, foreign count and meaning (Pi runtime precedence; OpenCode config order). Limits refuse rather than truncate: 4,096 rows per sequence, 1,024 managed members on either side, and 16,384 possible managed/foreign precedence pairs. Conditional work waits for carrier change and fresh observation. Newly discovered managed/foreign precedence changes require new interactive consent; `--yes` stops instead of expanding consent. If the second confirmation fails or is declined, keep completed effects and stop the [remaining order work](../apply-lifecycle/spec.md#requirement-reconcile-order-from-the-current-selected-relations).

Each precedence-risk row names the managed relation, foreign identity and before/after direction. A count alone cannot authorize the change. A credential-bearing, local or over-256-byte foreign identity is redacted in presentation without changing authorization identity.

Each `relation_order_results` row reports `exact` when no reorder was needed, `converged` when a reorder completed, `failed` when attempted order work failed, or `not_attempted` when it did not run. Where selected OpenCode sequences settle independently, a later failure preserves completed writes for the other sequences. A retry reobserves the current files; a prior success row does not prove current convergence.

Previewed claim adoption uses `would_record`; use `recorded` after a verified commit or `completed_by_install_recovery` with installed-observed provenance when existing install evidence completes it. Preserve the confirmed transitions if later work fails; `not_recorded` is for pre-effect refusal, and an indeterminate successor is `unknown_after_error`, not a receipt. Absence distinguishes `host_route`, `direct_config`, `observation_only` and `state_only`.

The optional `carrier_handoffs` has basis `plan_time_conditional_remainder`. Before and conditional rows differ; retirement array indexes are not identities. `settled`, `requalified` and `apply_complete` exist only on write results and represent independent confirmed facts; a completed unverified Antigravity attempt need not provide a new claim. `recovery_cleanup` preview rows give `kind`, with independent not-attempted/retired/still-fenced/unconfirmed outcomes on writes. Normal action counts and rollback are separate. `rolled_back` requires compensation for all attempted effects. Successful confirmed apply omits prior plan `mcp_statuses`; facts retained on failure are not inferred post-failure evidence.

Phase, outcome and typed causal facts drive JSON errors. Distinguish cancellation, stale evidence, active journal, cleanup-only authority and continuing file-set fences, including unknown observations. Do not project raw errors, secrets, local attempt internals or unverified outcomes as facts. A retained host-route attempt is not present authority, and cwd authority loss does not overwrite timeout, signal or exit evidence. Local and opaque carrier sources, subjects and order identities use `redacted:sha256:<digest>` and accompanying `*_redacted` flags. A source-derived `carrier_subject.name` uses the same rule, even if its relative path loses a `./` prefix. [CLI recovery and apply output](../../../docs/cli.md#apply) shows the result shapes.

#### Scenario: A claim commits before a later route fails

- **WHEN** confirmed apply returns an error after one verified claim transition
- **THEN** the result keeps the confirmed receipt without describing unconfirmed claims or later effects as completed

### Requirement: Select only the admitted recovery authority

`recover` SHALL select the workspace-wide state/recovery paths, not desired resources or a target filter. Active journal rollback/finalization, verified retained cleanup, permission-only recovery and explicit missing-state ownership release are distinct. Apply-journal recovery leaves independent declaration transactions alone; a [native management release journal](../interrupted-operations/spec.md#requirement-recover-native-management-release-without-replaying-host-changes) can settle its own file-set. `--legacy-user-state` selects only the old default-user operation, and `--baseline-tree` supplies [verified old skill evidence](../interrupted-operations/spec.md#requirement-recover-an-active-journal-from-recorded-authority-rather-than-desired-state). Missing-state release selects exact settled claims and bindings, can include multiple manifests sharing the statefile, and does not mutate host content.

`--declarations` selects only [the ordinary declaration pair](../interrupted-operations/spec.md#requirement-settle-a-declaration-pair-without-replaying-the-edit). It cannot be combined with `--legacy-user-state`, `--baseline-tree`, or `--release-output-ownership`. If the pair is absent or unqualified, do not fall back to another recovery kind.

Authorization of a current interactive plan is not equivalent to retaining a previous `--dry-run` result. `recover --yes` reselects and can choose a different legal kind; it has no `--expect-plan`. [Active and cleanup-only recovery](../interrupted-operations/spec.md) retain distinct evidence and revalidation requirements.

Follow the [permission recovery selection rule](../permission-protection/spec.md#requirement-recover-access-without-recovering-file-contents-or-replaying-the-command) when journal evidence and pending protection coexist.

#### Scenario: Journal cleanup takes precedence after a permission preview

- **WHEN** current eligible recovery changes between `--dry-run` and `--yes`
- **THEN** the write selects the current operation and reports its actual kind, rather than treating the old preview as an authority token

### Requirement: Report recovery phases without claiming unavailable retry authority

The [recovery family](../../../internal/cli/present/recover.go) SHALL distinguish planned active and cleanup-only work from retained, completed, retired and unknown authority. A freshly reclassified retained phase can disclose only that current authority. Known retired or unknown authority cannot reuse the preceding action. Terminal journal results omit `authority_kind`, operation directory, classification and old retry actions; continuing file-set fence evidence remains separate. Cleanup-only planning/retained work reports just the cleanup action and zero active obligations, without private retirement paths. GC failure after retirement is still an error but not a retry plan.

Permission recovery reports record settlement and per-destination facts; `observed_access_expansion` describes the current recovery observation, not earlier exposure. An unknown settlement does not prove the old record remains. Partial containment and failed publication return nonzero. A completed record phase can coexist with a later validation or lease error. Ownership release counts recorded claims and bindings, not changed host files. `unconfirmed` requires renewed observation rather than assuming no write, and `released` plus an error does not invite replay. Paths and typed reasons distinguish logical targets from private residue.

Declaration recovery reports `authority_kind = "declaration_transaction"` and the action (`rollback` or `finalize`) in the `planned` or `completed` phase only. `removes_lockfile: true` appears only when rollback removes a lockfile created by the interrupted write. A selected write may be `refused` before settlement or `unconfirmed` after a possible effect. Planning refusal uses `cli_error`. A completed action describes settlement, not retry authority.

#### Scenario: Journal authority was retired but garbage collection failed

- **WHEN** a final cleanup failure follows proven retirement
- **THEN** the result reports `authority_retired` and failure without presenting the already-retired recovery action as retryable

### Requirement: Distinguish state transfer from metadata format conversion

`migrate state` SHALL transfer only the default user's legacy management authority to its current XDG location. It needs exact recorded recovery for an interrupted transfer and is not arbitrary relocation, a host replay, or a cure for missing management. A completed repeat can report `already_migrated`. Preview is read-only and does not acquire mutation leases. Its result distinguishes selected state/registry locations and transfer counts from recovery directions, whose counts are omitted.

`migrate metadata` qualifies the selected State and reachable registry cohort without moving management or editing desired or host state. The [migration rules](../metadata-migration/spec.md) define reference and baseline inputs, recovery of the original selection, and inactive cleanup. The result states whether collection was incomplete; it does not grant further cleanup authority. Both commands report planning refusal through the common error family and keep a disclosed action on execution error without implying completion or retry authority. Their [result presenters](../../../internal/cli/present) own exact fields and independent versions.

#### Scenario: An operator supplies a reference for a legacy identity

- **WHEN** the current path proves the required identity but had no recorded lookup location
- **THEN** the result distinguishes an observed asserted location from a proven historical path

### Requirement: Disclose and execute only the selected MCP probe

`probe mcp-server` SHALL choose one admitted locked Claude Code or OpenCode project stdio subject, narrowing by target/scope when needed. Preview discloses exact command/args, environment bindings without values, selected-root cwd policy, timeout, possible external effects and cleanup. Confirmed interactive execution uses that request even if declaration or lock bytes change while awaiting the answer; it still needs the selected project root and current execution barrier. It can launch the exact locked stdio command to attempt protocol initialize, not edit manifest, lock, managed state or host config. Ordinary status, lock, doctor and apply do not start this probe.

The probe reports launcher, initialize, authentication, endpoint health and tool inventory separately. Unsupported or inapplicable dimensions do not become an aggregate ready/healthy result. None of the evidence is a persistent install/config/trust, package/cache or skip guarantee. [Probe JSON](../../../internal/cli/present/probe.go) preserves the selected operation, effect disclosure and ordered bindings without exposing values.

#### Scenario: Lock contents change while probe waits for confirmation

- **WHEN** a reviewed interactive probe is authorized
- **THEN** the approved command cannot be replaced with a newly locked command by that edit

### Requirement: Refresh one selected locked extension without broadening authority

`refresh extension` SHALL select one exact declaration and matching refresh route. Target and scope are filters, not alternate destinations. No bulk/wildcard, ordinary apply refresh, or generic update alias is implied. The timeout defaults to `10m` and admits whole-second `1s` through `1h`; it bounds one delegated child attempt, not planning, confirmation, observation or cleanup. Immutable disclosure contains the selected timeout, root, exact argv, environment names and host effects. Runtime outcomes use the [host route contract](../carrier-routes/spec.md), not a generic product update claim.

After disclosure, changes to applicable authority, lock or host facts refuse. [Carrier refresh](../carrier-lifecycle/spec.md#requirement-treat-refresh-as-a-distinct-one-attempt-route) defines required observations, protection of already-applied settings and what can remain after one attempt.

The result distinguishes planned, refused, cancelled, observed relation, attempted-unverified, failed and partial. Timeout/cancellation/signals after start or failed required post-observation/persistence may be partial. Attempted is separate from completion and from the current relation. Preserve independent process outcome, workdir authority and both recovery-barrier axes. The detail comes from typed facts, without raw process/error output or secret values. Before a result is selected, follow the common failure rule; afterward the result remains in its own stream/family, including under the explicit stderr authorization exception.

#### Scenario: The host process times out after launch

- **WHEN** refresh observed start but the child could not finish within the disclosed timeout
- **THEN** its result does not claim no host effects or automatically retry a different route
