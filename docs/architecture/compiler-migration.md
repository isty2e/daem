# Compiler Migration Ledger

Status: active implementation-evidence ledger derived from
[`ARCHITECTURE.md`](../../ARCHITECTURE.md). It records the current source
layout, compatibility fixtures, finite migration handoffs, and provisional
closeout census. It is not a runtime registry, public support matrix, persisted
schema, or independent architecture authority. Bounded MCP consumer cutover,
non-MCP shadow-surface compilation, apply/refresh/recover authority and
reservation-demand, State Barrier, and readiness pilots are implemented. The
full program remains open for non-MCP consumer cutover, every remaining
mutating operation compiler, the ordered effect-obligation and settlement
model, and semantic guard replacement.

Current executable behavior remains owned by canonical Go models and tests.
Current user-visible and persisted contracts remain owned by `docs/`, versioned
codecs, and strict consumers. A conflict with those owners stops the migration
and requires explicit disposition rather than a ledger-only update.

Baseline: `main` at `c0fd11f937c27936e175fb955168fdef3e539e37`.

## Program Boundary

Two orthogonal compilation problems are in scope:

```text
owner-local host facts
-> coherent host-surface views

normalized reconciliation/current facts
-> authority, freshness, effect, reservation, and recovery plan
```

One effectful protocol consumes the second compiler's output:

```text
operation plan + current journal/file-set state
-> retained StateDir/RecoveryDir execution authority
```

The migration preserves Desired, Topology, Supply, the three closed
Realization forms, Assurance evidence distinctions, pure Reconciliation,
Effect execution, journal, rooted filesystem authority, storage commit, and
independent persisted/public contracts.

## Ledger Schemas

### Invariant row

Every retained invariant records:

```text
id
normative statement and applicability
status: guarantee | non-goal | unknown
normative or decision authority
primary semantic owner
fact custodians and transition/effect roles when split
canonical observable predicate
current enforcement
verification projection
compatibility class
implementing phase
reopen condition
```

### Surface row

Every logical surface row records:

```text
target
scope
artifact family
variant or current placement/correlation id
representation form
current owner-local sources
durable ids referenced
physical-placement sharing
observation-purpose facets
operation/dispatch facets
parity evidence
```

The final opaque `SurfaceID` spelling is deliberately not frozen here. The
identity invariant is the one-to-one semantic key and its exact references to
existing durable ids. The concrete private spelling is an implementation decision for the
surface-schema phase and may not become a persisted id in this program.

### Operation row

Every operation records:

```text
operation and mode
normalized planning inputs
current authority/fingerprint compiler
revision/freshness roles
barrier or file-set preconditions
effect phases and commit points
recovery/terminal outcomes
sibling effect paths
parity evidence
```

## Adaptive Property Ledger

| ID | Status | Normative statement and predicate | Authority and primary owner | Current enforcement / evidence | Compatibility | Implementing owner | Reopen condition |
| --- | --- | --- | --- | --- | --- | --- | --- |
| ARCH-G01 | guarantee | Existing Desired, Topology, Supply, Realization, Assurance, Reconciliation, Effect, journal, rooted-authority, and storage-commit cores retain their accepted semantic ownership. A migration conforms only when none gains a refused responsibility. | User-accepted architecture direction; `ARCHITECTURE.md`. | Package contracts, import guards, current tests. | Exact semantic boundary. | all migration phases. | Evidence shows one retained core cannot enforce its accepted contract without a deliberate owner change. |
| ARCH-G02 | guarantee | One logical host surface is identified by target, scope, artifact family, and variant; one semantic key selects exactly one logical surface identity. | User decision; `ARCHITECTURE.md`. | Current target/profile, topology, placement, route, and capability rows. | Internal structural parity; no persisted id change. | surface-schema phase. | A supported surface cannot be distinguished by these coordinates without adding an independently justified axis. |
| ARCH-G03 | guarantee | Owner-local facets retain their facts and validation; one compiler alone owns cross-facet coherence and immutable view compilation. | User decision; `ARCHITECTURE.md`. | Current local constructors plus distributed cross-checks. | Behavioral parity. | surface-schema phase/surface-cutover phase. | A facet's semantic meaning requires another facet owner to decide its validity. |
| ARCH-G04 | guarantee | Representation and actuation remain orthogonal. Realization stays exactly managed path, managed aggregate contribution, or delegated relation. | `ARCHITECTURE.md` and current lifecycle contracts. | `realization.RealizationSpec`, profile routes, effect adapters. | Exact locked/persisted meaning. | surface-schema phase/surface-cutover phase. | A supported host contract proves a genuinely new representation form rather than a new route or capability. |
| ARCH-G05 | guarantee | Product support and runtime/platform capability remain separate facts. Capability absence cannot silently become product unsupported, and support cannot manufacture capability. | User decision; `ARCHITECTURE.md` and platform contracts. | `profile.Support`, capability rows, platform support model. | Behavioral/public support parity. | surface-schema phase/surface-cutover phase. | Product policy explicitly changes the support/capability distinction. |
| ARCH-G06 | guarantee | Observation cardinality is per `(surface, purpose)` and actuation cardinality is per `(surface, operation, dispatch class)`; a surface may have several independent observations and conditional actuators. | User decision; Assurance and current lifecycle contracts. | Separate probe/provider/config/relation rows and route operations. | Behavioral parity. | surface-schema phase/surface-cutover phase. | A current consumer proves two purposes or dispatch classes have identical identity, lifecycle, and failure semantics and should be merged. |
| ARCH-G07 | guarantee | Several logical surfaces may reference one physical placement while retaining target-relative support and selection. | Current supported behavior; `ARCHITECTURE.md`. | Shared instruction/skill placements and HookAsset placement. | Exact placement ids and physical addresses. | surface-schema phase/surface-cutover phase. | Physical sharing can no longer preserve each surface's independent support or operation contract. |
| ARCH-G08 | guarantee | Existing placement, topology, codec, route, adapter-contract, lock/state projection, and subject ids are not renumbered or reinterpreted by this migration. | Compatibility authority and user decision. | Current constructors, codecs, tests, persisted artifacts. | Exact parity. | all migration phases. | Separately authorized versioned compatibility migration. |
| ARCH-G09 | guarantee | Surface compilation is static, deterministic, immutable, and I/O-free; it stores contract references rather than adapter objects or callbacks. | User decision; `ARCHITECTURE.md`. | Current static catalogs and constructor validation. | Internal behavioral parity. | surface-schema phase. | A real external behavior cannot be represented by a stable contract reference and must remain private boundary code. |
| ARCH-G10 | guarantee | Operation authority facts, canonical ordering, deduplication/conflicts, mutation domains, lifecycle-named revision sets, and operation fingerprints have one pure compiler owner. | User decision; `ARCHITECTURE.md`. | `internal/operationplan` compiles canonical authority subprojections for apply/refresh/recover, adopt/import domain requests and full/stable revision roles, init, lock, authoring, and unmanage domain/revision ordering, refresh persistence revision roles, plus the exact apply, provider-stable, remaining-execution, refresh, active-recovery, and cleanup-only recovery fingerprints. The final production caller census finds no competing workflow-local compiler. | Exact domains, revisions, ordering, and fingerprint values. | operation-authority phase complete; effect-envelope handoff remains separate. | A parity failure proves two apparently shared facts have different canonical semantics. |
| ARCH-G11 | guarantee | Operation compilation performs no filesystem, subprocess, persistence, capability acquisition, or presentation work; all flexible boundary facts are normalized before lowering. | User decision; `ARCHITECTURE.md`. | Operation-specific compilers and the shared authority Builder emit pure `DomainStep` values; apply/refresh/recover/adopt/init/lock/authoring workflows lower path requests and retain owner-compiled barrier/route domains before lease acquisition. | Behavioral parity. | operation-authority phase/readiness phase. | A required semantic input cannot be observed before compilation without changing the operation contract. |
| ARCH-G12 | guarantee | Every effectful operation has a typed, ordered effect envelope whose complete demand is admitted before its first external or visibility effect. | User decision; `ARCHITECTURE.md` transaction/recovery contract. | `operationplan` provides the passive immutable effect-structure algebra and legacy demand projection; Effect and Refresh own schedules. Apply composes one structure across provider and final continuations, rejects a post-provider final suffix that differs from the reserved structure, and both Apply and Refresh hand their full structure to the State Barrier. Refresh consumes the exact reserved structure through an operation-local cursor for barrier, external-attempt, observation, StateDir, descendant binding/validation/publication, persistence, failure-terminal, and success-terminal checkpoints. The cursor owns and drives the reserved physical StateDir and descendant authorities directly. Refresh production reservation lowers every cursor-reachable alternative and admits their deterministic componentwise physical frontier, while supplying the selected Statefile path separately to the State Barrier; the flat `CompileRefresh` demand remains only a differential test oracle. Apply runtime consumption plus rollback and cleanup settlement remain pending. | Exact refusal timing and effect ordering. | effect-envelope phase remains active. | Supported dynamic work cannot be bounded before effects and the product contract must be reduced or separately changed. |
| ARCH-G13 | guarantee | Exclusive branches reserve the maximum reachable demand rather than the sum; sequential and repeated obligations preserve exact bounded demand and cannot add work after effects begin. | User decision; `ARCHITECTURE.md`. | `operationplan` derives a bounded deterministic nondominated frontier of cursor-reachable semantic demands. `recoverygate` lowers each alternative with the selected StateDir state and descendant path, then takes the maximum physical path/entry/byte work. The resulting branch-aware lower is a mandatory shadow oracle. Apply still receives the dominating legacy scalar authority; Refresh reserves exactly the componentwise physical frontier of cursor-reachable alternatives, without combining mutually exclusive semantic counts, duplicate legacy semantic counters, or a production flat-demand input. | Behavioral parity and pre-effect refusal parity. | effect-envelope phase. | Runtime evidence proves branches are not closed/exclusive or promotion can create unplanned work. |
| ARCH-G14 | guarantee | The operation plan describes semantic obligations; the State Barrier alone lowers them with selected paths and platform facts into physical StateDir/RecoveryDir capacity and consumable authority. | User decision; `ARCHITECTURE.md` and current lifecycle contracts. | Apply calls `recoverygate.ReserveForwardEffectStructure`; Refresh calls `ReserveForwardEffectExecution` with the exact structure and a separate selected descendant path; recoverygate derives the cursor-reachable physical frontier, proves it dominates every reachable branch, admits that frontier once, and couples branch-exclusive raw `StateDirExecutionAuthority` plus descendant binding, validation, publication access, and close to the exact schedule cursor without a synthetic semantic-count maximum, second semantic counter layer, or separately transferred runtime reservation. Apply still uses the transitional legacy-shaped physical reservation. Direct `ReserveForwardEffects` use is fixture-only. | Behavioral parity. | effect-envelope phase/state-barrier phase. | Physical resource cost cannot be derived without moving operation semantics into the barrier owner. |
| ARCH-G15 | guarantee | One logical State Barrier owns StateDir/RecoveryDir authority, journal and file-set axes, first-incarnation evidence, reservation consumption, phase revalidation, cancellation precedence, and terminal barrier classification. | User decision; `ARCHITECTURE.md` and recovery contracts. | `recoverygate` owns identity, joint axes, and reservation; workflows sequence only. | Exact failure precedence and visibility classification. | state-barrier phase. | A fact has a different lifecycle/authority and must remain in a lower mechanics owner. |
| ARCH-G16 | guarantee | Generic file-set marker, snapshot, publication, and recovery mechanics remain below State Barrier policy; storage, journal, execute, and subprocess remain effect mechanisms rather than semantic planners. | User decision; `ARCHITECTURE.md`. | `internal/effect/fileset`, storage/commit, journal, execute. Declaration/transaction is the manifest/lockfile adapter only. | Exact marker/journal/persistence behavior. | state-barrier phase. | Dependency evidence shows the proposed lower mechanics boundary creates a reverse import or public facade. |
| ARCH-G17 | guarantee | Workflows gather boundary evidence, invoke pure owners, acquire capabilities, sequence effects, and project results; they do not retain competing surface, authority, fingerprint, or capacity grammars. | User decision; `ARCHITECTURE.md`. | MCP callers plus list inventory/selection, Instruction/Skill import and diagnosis, Hook diagnostics and authoring support, HookAsset payload placement, Extension import ordering, host-route command selection, and selected readiness/apply/presentation order consumers invoke `catalog.Product`; apply/refresh/recover, adopt/import, init, lock, authoring, and unmanage callers invoke `operationplan` and `recoverygate`. Remaining profile calls are consumer-local importability or owner-local realization, codec, observation-value, and durable-validation contracts. Remaining mutation constructors are classified as workflow boundary lowering, State Barrier ownership, source-specific freshness, or post-effect rollback evidence rather than competing compilers. | Behavioral parity. | surface and operation cutover complete; effect closeout remains active. | A use case owns a genuinely operation-specific semantic contract not representable by the shared compiler. |
| ARCH-G18 | guarantee | Old implementation remains authoritative during shadow comparison. A caller flips only after exact required parity, and the old source is deleted after its last consumer moves. | User decision; architecture change rules. | Bounded surface joins; apply/refresh/recover authority subprojections; exact apply/refresh/recovery fingerprints; and adopt/import, init, lock, authoring, and unmanage domain/revision-role compilation have flipped. Workflow-local compiler grammars are removed; ordered effect settlement remains under the shadow-then-delete rule, so final program closeout has not occurred. | Exact or behavioral as classified below. | surface-schema phase through closeout phase. | A separately authorized migration requires staged dual-version support. |
| ARCH-G19 | guarantee | OS specialization remains at platform admission, physical authority, storage/filesystem, and subprocess boundaries; surface and operation compiler semantics are OS-independent. | `ARCHITECTURE.md`. | Current platform-specialized file census and report-only `compiler-os-specialization` shadow findings. | Behavioral parity. | surface-schema phase through guard-shadow phase. | A supported semantic distinction is proved to depend on OS rather than capability or physical adapter. |
| ARCH-G20 | guarantee | Architecture policy remains in active contracts; archguard provides semantic dependency/effect-boundary evidence and never becomes runtime or normative authority. Prose, symbol names, exact path catalogues, and density are review evidence only. Report-only compiler-shadow findings never fail the blocking baseline. | `ARCHITECTURE.md`. | Current import-graph and effect-boundary guards plus `Report.Shadow`. | Repository behavior only. | Governance deliberately assigns policy authority to another canonical document. |
| ARCH-G21 | guarantee | Form-specific transition ownership remains explicit: Reconciliation owns decisions; Effect owns decision-to-effect, journal-before-mutation, outcome, rollback, and durable-successor semantics; State Barrier grants authority but does not reinterpret the transition. | `ARCHITECTURE.md` and current lifecycle contracts. | `reconcile`, `effect/execute`, journal, workflows. | Exact effect/recovery behavior. | effect-envelope phase/state-barrier phase, with existing Effect owner retained. | Evidence shows a transition is currently owned by another canonical model or needs a new form-specific algebra. |
| ARCH-G22 | guarantee | Manifest, lockfile, statefile, journal, registries, and CLI DTOs retain independent compatibility lifecycles and are never unified as compiler IR. | `ARCHITECTURE.md` and user decision. | Versioned codecs and strict consumers. | Exact parity unless separately versioned. | all migration phases. | Separately authorized boundary migration. |

### Retained non-goals

| ID | Disposition | Statement and rationale | Decision authority / owner | Reopen condition |
| --- | --- | --- | --- | --- |
| ARCH-N01 | reject | Persist or publicly expose `SurfaceID`. Existing durable identities remain authoritative; a new id would add an unnecessary compatibility axis. | User decision; `ARCHITECTURE.md`. | A durable query cannot be answered by existing placement/topology/route ids and requires a versioned identity. |
| ARCH-N02 | reject | One mega SurfaceContract, AgentProfile interface, generic plugin registry, callback catalog, or `map[string]any` IR. These collapse independent identity, lifecycle, invalid-state, and failure semantics. | User decision; `ARCHITECTURE.md`. | No reopen under the current ownership contract. |
| ARCH-N03 | reject | Treat delegate/host route as a fourth realization form. It is actuation over a locked representation or relation. | `ARCHITECTURE.md` and current lifecycle contracts. | A new representation has independent identity and state, not merely a new operation mechanism. |
| ARCH-N04 | defer | Exact Go package names and the private spelling of internal Surface ids. Logical owner and dependency direction are fixed; physical placement follows package/cycle evidence. | `ARCHITECTURE.md`; implementing phase. | Each phase reaches implementation design. |
| ARCH-N05 | defer | Rename `recoverygate` to `statebarrier`. Primary ownership consolidation is implemented under `recoverygate`; a rename is allowed only if it shortens the public/import graph without a facade. The current census found one package either way, so a rename is churn rather than a shorter graph. | `ARCHITECTURE.md`; closeout phase. | A later graph change makes the current name a misleading extra hop. |
| ARCH-N06 | reject | Unify or rewrite apply, refresh, and recover fingerprint wire projections during this migration. Their exact formats remain distinct, but canonical serializer ownership must still move out of workflows and into the operation compiler before program closeout. | Compatibility authority. | Separately authorized fingerprint-version migration. |
| ARCH-N07 | reject | Immediate broad Paths, adopt-family, payload, findings, SubjectKind, or persisted DTO overhaul. These require independent owners and compatibility decisions. This does not exclude migrating adopt or another workflow's operation authority and fingerprint grammar into `operationplan`. | User decision; `ARCHITECTURE.md`. | Search finds a distinct active owner or architecture closeout creates one from new evidence. |
| ARCH-N08 | reject | Use package, file, LOC, or density reduction as acceptance. Numeric changes are evidence only. | `ARCHITECTURE.md` and user decision. | No reopen under current architecture policy. |
| ARCH-N09 | accept | Named persistence/recovery CI runs the same bounded State Barrier contract on native Linux, Darwin, minimum-Go, and race lanes, with a capability-sliced Windows cell plus the existing Windows storage/replacement oracle. This evidence does not expand product-platform admission. | State Barrier verification contract and CI. | Reopen if a named selector is removed, weakened, or stops exercising native replacement/residue authority. |

### Blocking unknowns

None. Exact private type names, package names, Surface id spellings, and effect
stage enums are intentionally deferred implementation choices whose plausible
answers do not change the accepted boundary, owners, compatibility classes, or
dependency direction.

## Surface Cell Ledger

A cell is logical and target-relative. The `variant/current ref` column names
the current placement or carrier correlation used to distinguish variants.
Shared physical placement is explicit; it does not merge logical cells.

### Instructions — managed path, 11 cells

| Target | Scope | Variant/current placement | Default | Shared physical placement | Current sources |
| --- | --- | --- | --- | --- | --- |
| codex | project | `instructions.project.agents` | yes | shared with OpenCode, Pi, Antigravity | profile instruction placement/admission/route rows |
| opencode | project | `instructions.project.agents` | yes | shared with Codex, Pi, Antigravity | same |
| pi | project | `instructions.project.agents` | yes | shared with Codex, OpenCode, Antigravity | same |
| antigravity-cli | project | `instructions.project.agents` | yes | shared with Codex, OpenCode, Pi | same |
| claude-code | project | `instructions.project.claude` | yes | no | same |
| antigravity-cli | project | `instructions.project.gemini` | no | no | same |
| codex | global | `instructions.global.codex` | yes | no | same |
| claude-code | global | `instructions.global.claude` | yes | no | same |
| opencode | global | `instructions.global.opencode` | yes | no | same |
| pi | global | `instructions.global.pi` | yes | no | same |
| antigravity-cli | global | `instructions.global.antigravity` | yes | no | same |

Facets: write/remove routes for all eight physical placements; 17 discovery
rows keyed by target/scope/purpose; one Claude project runtime-only row. Runtime
and discovery locations never gain write authority.

### Skill — managed path, 17 cells

| Target | Scope | Variant/current placement | Default | Shared physical placement | Current sources |
| --- | --- | --- | --- | --- | --- |
| codex | project | `skill.project.agents` | yes | shared with OpenCode, Pi, Antigravity | profile skill placement/admission/route rows |
| opencode | project | `skill.project.agents` | no | shared with Codex, Pi, Antigravity | same |
| pi | project | `skill.project.agents` | no | shared with Codex, OpenCode, Antigravity | same |
| antigravity-cli | project | `skill.project.agents` | yes | shared with Codex, OpenCode, Pi | same |
| claude-code | project | `skill.project.claude` | yes | no | same |
| opencode | project | `skill.project.claude` | no | no | same |
| opencode | project | `skill.project.opencode` | yes | no | same |
| pi | project | `skill.project.pi` | yes | no | same |
| codex | global | `skill.global.agents` | yes | shared with OpenCode and Pi | same |
| opencode | global | `skill.global.agents` | no | shared with Codex and Pi | same |
| pi | global | `skill.global.agents` | no | shared with Codex and OpenCode | same |
| codex | global | `skill.global.codex` | no | no | same |
| claude-code | global | `skill.global.claude` | yes | no | same |
| opencode | global | `skill.global.claude` | no | no | same |
| opencode | global | `skill.global.opencode` | yes | no | same |
| pi | global | `skill.global.pi` | yes | no | same |
| antigravity-cli | global | `skill.global.antigravity` | yes | no | same |

Facets: write/remove routes for all ten physical placements; 17 discovery
rows; one Codex global runtime-only row.

### Hook — managed aggregate, 4 cells

| Target | Scope | Placement/topology id | Codec | Physical root | Current sources |
| --- | --- | --- | --- | --- | --- |
| codex | project | `codex.project.hooks` | `codex-project-hook-json-v1` | `.codex/hooks.json` | topology Hook namespace switch, aggregate Hook placement, profile routes |
| codex | global | `codex.global.hooks` | `codex-global-hook-json-v1` | `~/.codex/hooks.json` | same |
| claude-code | project | `claude-code.project.hooks` | `claude-project-hook-json-v1` | `.claude/settings.json` | same |
| claude-code | global | `claude-code.global.hooks` | `claude-global-hook-json-v1` | `~/.claude/settings.json` | same |

Each cell has write/remove routes, shared-set cardinality, `/hooks` content
path, semantic sibling preservation, and canonical semantic equivalence.

### HookAsset — managed path, 4 logical cells over 2 physical placements

| Target | Scope | Logical variant | Shared physical placement | Current sources |
| --- | --- | --- | --- | --- |
| codex | project | referenced Hook asset | `hook-asset.project.data` at `.daem/hook-assets` | complete Hook topology plus dynamic HookAsset placement |
| claude-code | project | referenced Hook asset | `hook-asset.project.data` at `.daem/hook-assets` | same |
| codex | global | referenced Hook asset | `hook-asset.global.data` at `@data/hook-assets` | same |
| claude-code | global | referenced Hook asset | `hook-asset.global.data` at `@data/hook-assets` | same |

Consumer targets are derived from the complete Hook topology. The physical
placement is scope-relative and content-addressed; logical target surfaces must
not create duplicate files or a primary-target identity.

### MCP server — managed aggregate, 9 cells

| Target | Scope | Placement id | Topology namespace | Codec | Current sources |
| --- | --- | --- | --- | --- | --- |
| claude-code | project | `claude-code.project.project-config` | `claude-code.project.mcp-server` | `claude-project-mcp-stdio-v1` | topology MCP catalog, aggregate MCP placement, codec operations, profile routes |
| claude-code | global | `claude-code.global.user-shared-json` | `claude-code.global.mcp-server` | `claude-code-user-mcp-stdio-env-v1` | same |
| antigravity-cli | global | `antigravity-cli.global.default-config` | `antigravity-cli.global.mcp-server` | `antigravity-global-mcp-ambient-env-v1` | same |
| opencode | project | `opencode.project.project-config` | `opencode.project.mcp-server` | `opencode-project-mcp-local-command-v1` | same |
| opencode | global | `opencode.global.default-json` | `opencode.global.mcp-server` | `opencode-global-mcp-local-env-v1` | same |
| codex | project | `codex.project.project-config` | `codex.project.mcp-server` | `codex-project-mcp-stdio-command-v1` | same |
| codex | global | `codex.global.default-config` | `codex.global.mcp-server` | `codex-global-mcp-stdio-env-vars-v1` | same |
| pi | project | `pi.project.pi-config` | `pi.project.mcp-server` | `pi-mcp-adapter-stdio-v1` | same |
| pi | global | `pi.global.agent-config` | `pi.global.mcp-server` | `pi-mcp-adapter-stdio-v1` | same |

All nine cells retain exact current config layer/path, merge unit, content path,
sibling retention, absence, compared-field, environment-reference, and
write/remove route facts in their existing owners. Runtime-probe capability is
an independent observation-purpose facet only for Claude project (delegate
correlation required) and OpenCode project (delegate correlation not required).
Pi provider authoring is a target-level policy facet used by the Pi project and
global bindings; it is not a second MCP placement or runtime observation.

### Extension relation — delegated relation, 8 cells

| Carrier / target | Scope | Correlation/placement id | Operations | Current sources |
| --- | --- | --- | --- | --- |
| claude-code-plugin / claude-code | project | `claude-code-plugin` | install, refresh, remove | Desired carrier contract, topology relation, delegated route profile/dossiers |
| claude-code-plugin / claude-code | global | `claude-code-plugin` | install, refresh, remove | same |
| codex-plugin / codex | global | `codex-plugin` | install, refresh, remove | same |
| opencode-plugin / opencode | project | `opencode-plugin` | install, refresh, remove | same |
| opencode-plugin / opencode | global | `opencode-plugin` | install, refresh, remove | same |
| pi-package / pi | project | `pi-package` | install, refresh, remove | same |
| pi-package / pi | global | `pi-package` | install, refresh, remove | same |
| antigravity-cli-plugin / antigravity-cli | global | `antigravity-cli-plugin` | install, refresh, remove | same; removal coverage remains intentionally partial by dossier |

The five carrier families remain distinct despite shared route mechanics.
Verified relation fields and operation-specific removal dossiers are actuation
or verification facets, not representation fields. Extension-order capability
is an independent observation/mutation facet for OpenCode project/global and Pi
project/global only.

### Support and capability summary

The current explicit support table has 15 target/family rows for Instructions,
Skill, and Hook. MCP support is currently inferred from placement presence and
Extension support from delegated-route presence. The surface-schema phase must
preserve this behavior before any later decision to normalize support facts;
absence of a row may not silently acquire a new public meaning.

## Current Cross-Facet Source Ledger

| Facet | Current canonical/local owner | Current duplicate or join sites | Target compiled view | Must not move |
| --- | --- | --- | --- | --- |
| Structural projection namespace | `topology/hook`, `topology/mcp` | aggregate subject classification, lock/refine, workflow tests | Topology view selecting owner-local namespace contract | SubjectID construction and edge legality |
| Managed path placement | `realization/profile` placement rows | admissions, routes, discovery/runtime, target profiles | Placement/target views | Portable destination and Realization validation |
| Hook aggregate placement | `realization/aggregate` | topology namespace switch, profile routes, codec catalog | Realization/operation views | Aggregate contract and codec-local behavior |
| MCP aggregate placement | `realization/aggregate` | owner catalog remains the fact source; `catalog.Product` is the consumer join for list/authoring/adopt/probe/readiness/observe/help; `realization/lock` keeps `MCPPlacementForSubject` because those packages cannot import catalog | Realization/topology/operation/observation views | Exact placement and codec contract ids |
| Support | `realization/profile` | readiness/help/docs | Target support view | Public product wording and platform capability |
| Discovery/runtime | `realization/profile` | adopt/list/diagnose | Observation/discovery views | Filesystem observation and import authority |
| Operation routes | `realization/profile` | effect adapters, apply/refresh, lock | Operation view | Adapter behavior and effect outcomes |
| Runtime probe | `realization/profile` + runtime-probe adapter | help/probe/readiness | Observation-purpose view | Probe evidence and execution |
| Provider/effective policy | profile/provider and Assurance/readiness owners | authoring, readiness, status | Purpose-specific views | Current evidence and provider lifecycle semantics |
| Extension order | `realization/profile` + relation evidence | readiness/apply | Observation and actuation views | Current physical sequence and revision evidence |

## Operation And Transition Ledger

| Operation / mode | Current planning and identity owner | Current barrier/freshness | Effects and transition owner | Required migration disposition |
| --- | --- | --- | --- | --- |
| Apply dry-run | `workflow/apply` readiness and operation inputs; `operationplan` exact fingerprint and authority facts | EffectAuthority captured; no effect authority consumed | no host mutation; disclosure only | Exact fingerprint ownership has cut over. Represent no effect explicitly without changing disclosure. |
| Apply write | `workflow/apply` and `workflow/readiness` inputs; `operationplan` exact fingerprint and authority facts | project root, leases, revisions, EffectAuthority, provider and final replans | `effect/execute`, host-route/delegate adapters, journal, state/claim stores | Compile one typed ordered envelope spanning every subpath and settlement. |
| Apply provider phase | workflow-local provider action preparation; `operationplan` exact provider-stable fingerprint | reserved pre-effect StateDir envelope, barrier replans, renewed consent | host route then replanning | One operation envelope spans provider and final phases; no later fresh reservation. |
| Managed path/aggregate mutation | Reconciliation decisions plus apply-local authority facts | physical/logical leases, revisions, retained roots | `effect/execute`, journal, storage/commit | Effect remains form-specific transition owner; compiler supplies typed obligations and facts. |
| Host relation install | Reconciliation relation action plus apply route facts | pre/post command authority and post-observation | hostroute adapter, attempt/pending/claim persistence | Every command and durable write remains in the same envelope and authority. |
| Carrier removal | carrier-absence plan plus apply-local fingerprints/counts | claim/pending baselines, relation/effect observations, retained StateDir | host route or direct relation mutation, attempt/retirement persistence | Preserve current write-ahead, verification, retirement, and retained-pending semantics. |
| Relation order | relation-order decisions and workflow class-count logic | physical sequence revisions and retained roots | bounded config sequence mutation | Reserve by runtime mutation class and preserve foreign rows; Effect owns transition. |
| Delegate attempt | delegate decisions plus `operationplan` exact remaining-execution fingerprint | pre-invocation and post-attempt authority | delegate adapter and bounded attempt persistence | One obligation per invocation and persistence result; history never becomes currentness. |
| Statefile/claim persistence | workflow-local statefile plans and exact stores | descendant reservation, StateDir and peer revisions | Effect/store commit | All descendant writes are planned and reserved before first effect. |
| Refresh dry-run | `workflow/refresh` fingerprint and authority compiler | EffectAuthority plus project root/revisions | disclosure only | Current execution consumes no forward reservation and does not call `CompileNone`. Move the exact fingerprint serializer and revision-role projection into operationplan, then represent no effect explicitly. |
| Refresh execute | refresh plan/authority, host timeout, observation | leases/revisions, EffectAuthority before/after host, persistence subset | host adapter, post-observation, attempt-history persistence | Canonically compile the full fingerprint and persistence revision role while preserving partial-result and post-host semantics. |
| Recover active journal | `workflow/recover` recovery/authority fingerprints | retained journal, StateDir and project/root authority, recovery leases | journal recovery plan, execute/storage, guarded retirement | The exact active-recovery serializer now compiles in operationplan from owner-supplied opaque action/state projections; State Barrier owns capability, not recovery semantics. |
| Recover cleanup-only | recovery cleanup fingerprint/authority | retained RecoveryDir authority; StateDir-independent | journal retirement | Move the exact cleanup fingerprint serializer into operationplan while preserving cleanup-only independence and continuing fence disclosure. |
| Import/adopt dry-run | adopt CandidateSet/Plan identity and workflow plan | physical source authorities, revisions, recovery barrier | no publication | CandidateSet/Plan semantic identity remains in adopt; operationplan now compiles the barrier-domain prefix, admission-ordered logical/physical domain requests, and full/stable revision roles. No-effect disposition remains part of the typed effect-envelope follow-up. |
| Import/adopt write | workflow/adopt plus `Plan.IdentityBytes` | barrier, manifest/source freshness, final pre-publication validation | source/skill publication then manifest transaction | Operationplan now owns mutation-domain ordering, physical-domain deduplication, revision conflicts/authority, MCP no-revision disposition, and exact canonical fingerprint hashing while workflow retains path lowering, freshness, leases, and publication. Ordered publication obligations remain effect-envelope work. |
| Authoring metadata write | workflow/authoring | barrier, manifest/lock revisions, project/root authority | recoverable manifest/lock/state/registry transaction | Operationplan now owns exact manifest/lockfile target pairs, marker/local/barrier domain order and bounded-document/marker/local/barrier revision order. Declaration semantics, transaction recovery, StateDir establishment, lock build, and atomic publication remain owner-local; no fingerprint is manufactured. |
| Unmanage | workflow/authoring unmanage | barrier and durable owner revisions | journaled manifest/lock/state/registry release, no host effect | Operationplan now owns declaration-then-persistence target pairs, marker/local/barrier domain order and declaration/persistence/marker/local/barrier revision order. Selection, host-state preservation, StateDir establishment, revalidation, commit-point ordering, and result semantics remain owner-local; no fingerprint is manufactured. |
| Init | workflow/init plus declaration transaction | direct file-set gate then EffectAuthority | manifest publication | Operationplan now owns exact manifest entry/referent, metadata transaction, and trailing barrier domain order plus manifest/metadata/barrier revision order. BuildPlan's journal-independent file-set gate, re-observation, leases, and publication remain local; init has no existing fingerprint, and no-op/write envelopes remain effect-envelope work. |
| Lock dry-run/outdated | workflow/lock | file-set fence and source/declaration freshness as currently required | no lockfile publication | Journal-independent compatibility remains unchanged; these paths do not invoke the write compiler or manufacture mutation authority. |
| Lock write | workflow/lock mutation | StateDirAuthority/file-set fence, source/build revisions | lockfile publication | Operationplan now owns exact manifest/lockfile/metadata/local-source/StateDir domain and revision ordering. Workflow retains StateDir authority and first-incarnation checks, cache preparation, source build, currentness, cancellation, and publication; no fingerprint is manufactured. |
| Prepared MCP probe | workflow/probe | EffectAuthority prevents concurrent recovery/file-set ambiguity | explicit subprocess probe, no durable readiness persistence | Compile the subprocess obligation and authority/freshness projection canonically while retaining runtime evidence as operation-local and non-persisted. |
| Status/list/diagnose/help | read-only workflows | some call `recoverygate.RequireClear`; no EffectAuthority | no mutation | Classify each existing read barrier as compatibility behavior; do not route through mutating authority. |

### Apply subpath closure

The operation-authority through state-barrier phases must cover, without
independent local compilers:

```text
provider prerequisites
managed path effects
aggregate effects
host-route install
carrier removal and direct config relation removal
relation-order normalization
delegate attempts
ownership promotions/finalization
statefile, carrier-claim, and ownership-registry persistence
journal capture, rollback, compensation, and retirement
post-effect observation and final peer/barrier validation
```

## Current State Barrier Caller Ledger

| Caller class | Current API | Mutation/effect posture | state-barrier phase requirement |
| --- | --- | --- | --- |
| apply, refresh, adopt, authoring, unmanage, init, prepared probe | `recoverygate.NewEffectAuthority` | effectful or effect-adjacent | Consume one State Barrier owner and operation demand; no workflow recapture. |
| recover | bounded `recoverygate.CaptureStateDirBounded` plus recovery plans | recovery | Active-journal planning captures StateDir through the State Barrier; cleanup-only still skips capture. The StateDirAuthority type lives in recoverygate. |
| lock mutation | `recoverygate.CaptureStateDir` | lockfile write | Preserve lock compatibility while using canonical StateDir authority. Do not add RecoveryDir journal domains. |
| init, authoring dry-run, lock planning | `recoverygate.RequireFileSetClear` | metadata planning/write | File-set fence only; do not add journal blocking. Capture then identity-bound census; no RecoveryDir inspection. Compatibility exception: lock remains available during an active journal. |
| list/status/diagnose/probe-read paths | `recoverygate.RequireClear` | read-only refusal | Joint journal+file-set refusal without mutation authority. Retained read-only compatibility exception; never grant mutation authority. |
| recoverygate itself | `StateDirAuthority` plus compiled `operationplan.Demand` | joint barrier and reservation | Owns StateDir identity types and capture APIs; generic file-set marker/census/publication mechanics live in `internal/effect/fileset`. Identity-free `observeClearFence` is package-local and is not a production gate. |

## Persisted And Public Compatibility Matrix

| Boundary | Current authority | Migration parity target | Explicit exclusions |
| --- | --- | --- | --- |
| Manifest TOML and authoring bytes | declaration codec/manifest contracts | exact retained syntax, defaults, ordering, and edit behavior | no SurfaceID or compiler IR fields |
| Lockfile versions and order | realization/lockfile | exact schema, IDs, canonical ordering, old-version policy | no profile/catalog reinterpretation |
| Statefile version and facts | assurance/statefile | exact schema and current historical fact meaning | no operation IR or current evidence persistence |
| Recovery journal/retirement | effect/journal | exact schema, ordering, authority identity, and recovery classification | no ordinary apply authority |
| Carrier-claim and ownership registries | exact store codecs and canonical claim owners | exact bytes/identity/transition semantics | no surface or plan ownership |
| SubjectID/topology namespaces | topology | exact text, ordering, and graph meaning | SurfaceID remains internal linkage only |
| Placement/codec/route/adapter IDs | current owner packages | exact values and lookup semantics | no renumbering or alias layer |
| Apply operation and authority fingerprints | `operationplan` exact enclosing projections with workflow-owned normalized inputs | exact digest values for identical inputs | no cross-operation format unification |
| Refresh fingerprints and revision subsets | `operationplan` exact fingerprint projection; workflow refresh inputs | exact digest values and full/persistence subset semantics | no colon/NUL normalization by convenience |
| Recovery fingerprints | `operationplan` exact active/cleanup projections; journal plan semantics remain owner-local | exact active/cleanup identity | no merge with forward operation identity |
| Import/adopt Plan identity | adopt Plan | exact identity bytes and conflict/skip meaning | not automatically absorbed by operation-authority phase |
| CLI JSON schema versions/fields/order | contractversion, cli/present, strict testkit consumers | exact | no compiler debug projection in public output |
| Human output and error precedence | workflow/cli contracts | behavioral or exact where documented/tested | incidental uncontracted prose is not a new operation identity |
| Platform support | platformsupport and docs | exact product admission; capability changes remain separate | no support expansion |

## Transition-Owner Ledger

| Transition | Canonical semantic owner | Coordinator/executor | Persisted projection | Migration consequence |
| --- | --- | --- | --- | --- |
| Desired family -> structural subjects | Topology family lowerer | lock/readiness composition | SubjectID in lock/state/journal as foreign key | Surface compiler selects contracts but never constructs SubjectID. |
| Desired/Topology/Supply + static surface -> locked realization | Realization/refinement | lock workflow | lockfile codec | Host-Surface views replace distributed joins; lock meaning unchanged. |
| Locked/current facts -> decision | Reconciliation | readiness orchestration | none | Readiness emits normalized facts; no I/O in decision owner. |
| Decision -> executable effect intent | Effect form-specific algebra (`effect/execute` current owner) | apply/refresh workflow | journal captures before mutation | effect-envelope phase makes obligations explicit; State Barrier does not reinterpret them. |
| Effect intent -> journal record | Effect/journal protocol | execute/journal | journal codec | Journal-before-mutation and exact authority remain unchanged. |
| Effect outcome -> durable successor | Effect transition algebra with Assurance value owners | execute plus exact stores | statefile/registries | No workflow-local alternate successor model. |
| Interrupted journal -> recovery action | journal/recovery algebra | recover workflow and effect executors | retirement control/journal state | state-barrier phase supplies authority; recovery owner retains classification. |
| Surface descriptors -> compiled views | Host-Surface compiler | static composition root | none | One cross-facet coherence owner; local fact owners remain authoritative. |
| Operation facts -> domains/revisions/fingerprint | Operation-Safety compiler | operation workflow | operation-local digest only | One compiler, several exact compatibility projections. |
| Effect envelope -> physical reservation/capability | State Barrier | retained rooted/mutation mechanisms | none | Demand admitted before effects; capability is non-persisted and single operation. |

## Verification And Golden Fixture Matrix

The existing tests below are the frozen old-model executable oracle. Later
phases add old/new differential assertions around these inputs before
cutover; they do not replace the current oracle with prose.

| Contract | Existing executable fixtures | Covered claim | Known gap assigned forward |
| --- | --- | --- | --- |
| MCP topology cells | `internal/topology/mcp`: `TestProjectionNamespaceCatalogIsCompleteAndUnique`, `TestProjectionSubjectOwnsCanonicalIdentityForEveryPlacement`, `ImplementedProjectionNamespaces` | exact namespaces, identity, completeness | owner catalog remains the fact source; compiled views must match it. |
| Hook topology | `internal/topology/hook`: `TestLowerOwnsTheCompleteDesiredTopology`, `TestAssetSubjectIdentityExcludesContentAndConsumers` | complete target graph and shared HookAsset identity | surface-schema phase proves surface mapping does not create duplicate assets. |
| MCP placements | `internal/realization/aggregate`: `TestImplementedMCPPlacementsExposeCurrentRowsOnly`, `TestMCPPlacementStaticContractAccessorsAreCanonicalAndDefensive`, catalog duplicate tests | exact rows, paths, codecs, policies, cardinality | compiled MCP views must compare every facet and durable id without copying row literals. |
| Target/profile views | `internal/realization/profile`: `TestProfilesPreserveCurrentSupportAndRealizationMatrix`, `TestProfileSeparatesPlacementDiscoveryRuntimeAndRoutes`, `TestSharedPlacementsRemainOnePhysicalIdentity`, `TestProfilePreservesMCPPlacementScopeMatrix` | support, placement, discovery/runtime/route separation and shared placement | surface-cutover phase shadows every consumer before cutover. |
| Probe/order capability | profile probe/order tests | exact independent capability rows and defensive copies | compiled MCP views attach runtime-probe as an optional observation purpose; order capability stays with profile until later families. |
| MCP compiled host-surface views | `internal/hostsurface` identity tests; `internal/hostsurface/catalog`: `TestProductCatalogMatchesOwnerMCPRows`, `TestLookupMCPMatchesOwnerPlacement`, `TestHasMCPTargetMatchesOwnerCatalog`, `TestRuntimeProbeFacetMatchesOwnerCapabilities`, `TestHasMCPProviderAuthoringMatchesOwnerCatalog`, `TestLookupMCPBySubjectMatchesOwnerPlacement`, `TestMCPInOwnerOrderMatchesOwnerPlacementCatalog`; `internal/adopt/mcp`: `TestMCPImportExtractorsCoverNonPiCompiledCells`, `TestCandidatesRejectsUnsupportedSurfacesExplicitly`; `internal/workflow/help`: `TestBuildUsageFactsReturnsStaticTargetInventory` plus duplicate-key, missing-namespace, many-to-one, and unreferenced-probe fixtures | opaque SurfaceID/SurfaceKey, exact nine-cell parity, LookupMCP/HasMCPTarget/HasMCPProviderAuthoring/RuntimeProbe/LookupMCPBySubject/MCPInOwnerOrder owner equality, adopt-local extractor coverage of compiled non-Pi cells, Pi/uncompiled unsupported-surface skips, help/authoring owner-order lists, invalid seeds, many-to-one placement, optional runtime-probe purpose | lock/profile route construction stays owner-internal because realization cannot import catalog. MCPPlacementForSubject remains the realization/lock owner join. |
| Non-MCP host-surface views | `internal/hostsurface/catalog`: managed-path, Hook/HookAsset, and Extension owner-parity, target/scope lookup, default/exact-root and order-class selection, and negative-seed tests; `internal/realization/profile`: managed-path relative-path, delegated-route/order, and HookAsset snapshot tests; `internal/topology/extension`: carrier namespace coverage; list/adopt/diagnose/readiness/apply/CLI parity and behavior tests | exact Instruction/Skill placement, admission/default, discovery/runtime, and route facets; Hook aggregate placement/codec/routes; shared HookAsset physical placement; Extension carrier/source/namespace/delegated-route/order facets; stable owner/placement order and SurfaceID ordering; defensive copies; list/import/diagnosis/order parity | list inventory/selection, managed import, Skill diagnosis, Extension import ordering, and selected readiness/apply/presentation order consumers use compiled views. Authored relative-path interpretation, Skill compatibility, persisted lock contracts, effect adapters, and owner-internal observation validation remain with their canonical owners. |
| Architecture separation | `internal/archguard`: blocking dependency-direction, workflow-boundary, effect-boundary, and synthetic near-neighbor tests plus `TestTopologyGuardBaseline`; report-only `TestCompilerShadowBaseline` plus forbidden/near-neighbor/perturbation fixtures in `shadow_test.go` | current owner/refusal and import law without prose or symbol-name inference; compiler/State Barrier prefix roles never fail the blocking baseline | exact-path `packagePlacementRows` still determine whether blocking affinity checks apply. Final cutover requires equivalent semantic blocking coverage before deleting that classifier. Density stays gone. |
| Apply authority/fingerprint | `internal/operationplan` builder/fingerprint and I/O-free path-step tests; `internal/workflow/apply`: `TestBuildApplyAuthorityEvidenceCoversAuthoritativePaths`, `TestApplyOperationFingerprintBindsPlanAndDelegateMode`, `TestApplyOperationFingerprintIncludesStatefileSemanticsWitness`, relation/order/carrier fingerprint tests | deterministic sensitivity and current authority coverage; byte-identical authority, full apply, provider-stable, and remaining-execution projections; pure domain-step compilation with workflow lowering | exact apply serializers compile through `internal/operationplan`; workflow retains owner-local fact construction, path lowering, observations, and effect sequencing. Sibling mutating-operation compilers remain the completion gap. |
| Refresh authority/fingerprint | `internal/operationplan`; refresh timeout/freshness/replan tests including `TestRefreshTimeoutParticipatesInDisclosureAndFingerprint` and `TestCompiledRefreshFingerprintMatchesLegacyProjection` | exact full operation fingerprint bytes, operation identity sensitivity, authority revalidation, persistence subset behavior, owner-order-independent authority fingerprint, and pure domain-step compilation | refresh full and authority fingerprints compile in `operationplan`; workflow retains path lowering and effect sequencing. Persistence revision-role compilation remains workflow-local. |
| Recovery authority/fingerprint | `internal/operationplan`; recover execution/authority/replacement/cleanup tests including `TestCompiledActiveRecoveryFingerprintMatchesLegacyProjection` and `TestCompiledCleanupRecoveryFingerprintMatchesLegacyProjection` | active and cleanup authority, drift, terminal classification, byte-identical active and cleanup-only operation projections, and pure logical/physical domain-step compilation; recover authority DTO omits Family/Containment | active and cleanup-only operation fingerprints compile in `operationplan`; workflow lowers paths while journal-owned action, statefile, cleanup-obligation, and claim-transition facts remain owner-supplied inputs. |
| Import/adopt operation safety | `internal/operationplan`: adopt admission-order, access/effect, revision-role, authoritative-conflict, MCP external-validation, scan-closure, ordered-step, and defensive-copy tests; `internal/workflow/adopt`: fingerprint, Skill route, MCP source, path-versus-revision error precedence, revalidation, stale-plan, rollback, and publication tests | exact barrier-prefix/domain order, physical deduplication, decimal-effect revision ordering, full versus stable roles, authoritative replacement/conflict behavior, MCP primary no-revision handling, historical compilation-error order, and unchanged `Plan.IdentityBytes` fingerprint | path resolution/lowering, source rereads, Skill-root witnesses, leases, stale/currentness sequencing, and publication remain workflow/effect responsibilities; typed publication obligations remain effect-envelope work. |
| Init operation safety | `internal/operationplan`: init domain/revision order, access/effect, barrier-tail, lazy revision-limit, and defensive-copy tests; `internal/workflow/init`: create/overwrite, cleanup-journal, file-set residue, cancellation, mode preservation, collision, and publication tests | exact manifest entry/referent and metadata domain order followed by barrier domains; exact manifest bounded-file, metadata content, and barrier revision order; unchanged dry-run and stale/currentness sequence | path resolution/lowering, file-set-only BuildPlan gate, leases, barrier validation, manifest re-observation, and storage publication remain workflow/effect responsibilities; no fingerprint is manufactured. |
| Lock operation safety | `internal/operationplan`: lock domain/revision order, local-path order, present/absent StateDir access, lazy revision-limit, and defensive-copy tests; `internal/workflow/lock`: stale manifest, revision coverage, manifest-entry conflict, metadata residue, extension-order, source build, dry-run, outdated, cancellation, and publication tests | exact manifest/lockfile/metadata/local-source/StateDir domain order and access; exact bounded document, metadata, and local-source revision order; unchanged journal-independent dry-run/outdated and StateDir first-incarnation behavior | local-path observation, StateDir capture/fencing/ensure, path lowering, leases, cache preparation, source build, currentness, and publication remain workflow/effect responsibilities; no StateDir generic revisions, RecoveryDir domains, or fingerprint are added. |
| Authoring/unmanage operation safety | `internal/operationplan`: metadata target/marker/local/trailing-domain, authoring revision, unmanage declaration/persistence revision, input-ownership, and order tests; `internal/workflow/authoring`: lease contention, interrupted transaction recovery, candidate reload, local-path drift, barrier/fence, StateDir establishment, lock build, unmanage recovery authority, atomic file-set commit, and host-state-retained tests | exact target, marker, local-source, trailing barrier domain order; exact authoring and unmanage revision capture modes/order; preserved recovery-domain inclusion difference and all-owner atomicity | semantic manifest/lock/state/registry construction, recovery, path lowering, leases, StateDir authority, file-set publication, and result projection remain workflow/effect responsibilities; no fingerprint is manufactured. |
| Forward reservation | `internal/operationplan` envelope/demand tests; recoverygate reservation and high-cardinality tests; apply `TestExecuteReservesCompleteStateDirEnvelopeBeforeProviderInvocation` | pre-effect atomic reservation and exact consumption; apply/refresh pass compiled Demand into recoverygate | physical path/entry/byte/census lowering stays in recoverygate `StateDirAuthority.ReserveOperation`. |
| StateDir/file-set mechanics | `internal/effect/fileset` marker, rollback, recovery, and census tests; recoverygate StateDir identity tests; `TestFirstIncarnationFaultMatrix`, `TestEnsureStateDirForEffectFaultMatrix`, `TestAbandonedResidueFenceSurvivesRetryThenClears` | first-incarnation, budgets, residue, restore, limits, cancel/replace before and after witness acceptance, residue retry | declaration/transaction remains the manifest/lockfile adapter. Mutating workflows validate recoverygate before `CommitFileSet`/`RecoverFileSet`. storage/commit publication `faultPlan` stays the lower visibility oracle. |
| Public/wire contracts | versioned codec tests, `test/testkit/clijson`, contractversion constants, and executable producer/consumer tests | exact schemas and strict consumers | Each phase runs affected consumers; no new public field. Documentation is reviewed directly rather than asserted as prose. |

### Verification commands

```sh
cd daem
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/topology/mcp ./internal/topology/hook \
    ./internal/realization/aggregate ./internal/realization/profile \
    ./internal/hostsurface ./internal/hostsurface/catalog \
    ./internal/adopt/mcp ./internal/workflow/authoring ./internal/workflow/list \
    ./internal/workflow/probe
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/operationplan \
    ./internal/workflow/apply ./internal/workflow/refresh \
    ./internal/workflow/recover ./internal/recoverygate \
    ./internal/effect/fileset ./internal/declaration/transaction
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/archguard ./internal/platformsupport ./test/testkit/clijson
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test -run 'Test(TopologyGuardBaseline|CompilerShadowBaseline|CompilerShadow)' ./internal/archguard
```

Repository-wide lanes remain mandatory for implementation phases; these
focused commands identify the old-model parity oracles.

## Phase Handoffs And Reset Rules

| Phase | Consumes | Must produce | Reset/reopen condition |
| --- | --- | --- | --- |
| surface-schema phase | surface cell, owner, compatibility, and fixture rows above | internal identity/schema; exact MCP parity; and shadow-compiled Instruction, Skill, Hook, HookAsset, and Extension views with stable owner ordering, many-to-one placement, negative-join, and defensive-copy coverage | key axes cannot distinguish a current cell, durable id would change, owner dependency reverses, or an owner facet requires I/O/callbacks |
| surface-cutover phase | surface-schema phase views and consumer census | bounded cutover and one source per migrated facet. MCP waves 1–4 use compiled target/scope, probe/provider, adopt-local extractor, subject, readiness/observe, and owner-order views. Non-MCP consumers use compiled inventory/selection, import, diagnosis, authoring support, payload placement, Extension route/order, readiness, apply, presentation, and host-route views. Importability remains consumer-local; authored path interpretation plus realization/codec/durable validation remain with their owners. The final census finds no fallback surface join in production consumer packages. | any consumer requires different semantics or facade becomes authority |
| operation-authority phase | operation/transition/compatibility rows | complete: `internal/operationplan` fact/domain/revision compiler, pure shared `DomainStep` lowering, exact apply/refresh/recover authority subprojections, adopt/import, init, lock, authoring, and unmanage domain/revision-role compilation, refresh persistence roles, and exact apply/provider/remaining, refresh, active-recovery, and cleanup-only recovery fingerprints. The caller census classifies probe as observation-only, recoverygate as the State Barrier owner, adopt source rereads as source-specific freshness, and adopt write revisions as rollback evidence. | exact parity fails because facts are not actually shared |
| effect-envelope phase | operation-authority phase facts and operation closure | passive typed structure, closed choices/triggers/conditionals, bounded demand frontiers, Apply/Refresh structural State Barrier lowering, and a Refresh-first exact-schedule runtime cursor pilot that reserves the componentwise physical frontier of cursor-reachable alternatives and cursor-couples descendant binding, validation, publication access, and close over branch-exclusive raw physical authority without duplicate legacy semantic counters. Still required: Apply and Recovery runtime consumption, branch-selected physical reservation reduction, rollback/cleanup settlement, and removal of remaining flat demand and scalar runtime counters. | work cannot be bounded before effects or branch algebra is not closed |
| state-barrier phase | demand plus State Barrier/caller ledger | Waves 1–4 consolidated primary ownership in recoverygate and generic mechanics in `internal/effect/fileset`; no old direct capture symbols remain. Fault injection covers first-incarnation cancellation/replacement and residue retry. Named Linux/Darwin/Windows/minimum-Go/race verification is active; Windows remains capability-sliced and no new journal blocking or product-platform guarantee is implied. Broader closeout still depends on the typed effect-envelope handoff. | public recovery semantics or platform guarantees must change |
| readiness phase | active surface views and operation vocabulary | observation shell and pure readiness middle-end. `Assess` and `AssessOutputInventory` remain the observation sequencers; `assembleAssessment` / `planOutputInventory` / `planExtensionOrderDecisions` perform no I/O; Skill support, compiled MCP cells, and Extension order-class ownership use compiled views. Lower relation observation still validates owner-local capability values. `operationplan` stays in apply/refresh/recover/adopt. Observation order and codec `MaximumDocumentBytes` bounds stay exact. | observation order/evidence semantics cannot remain exact |
| guard-shadow phase | implemented owner graph at Host-Surface, Operation-Safety, State Barrier, and readiness cutovers | report-only Shadow channel on `Report` that never feeds `HasFailures`; prefix roles `internal/hostsurface`, `internal/operationplan`, `internal/recoverygate`; S1–S7 compiler/barrier import and OS-file rules; perturbation fixtures for new OS, extra catalog package, new realization, new observe purpose, and recovery hardening; old-rule disposition table below | proposed rule cannot reject forbidden fixtures and admit near-neighbors, or Shadow findings appear on the current tree |
| closeout phase | every prior artifact | provisional: surface compilation and production-consumer cutover, exact operation authority/fingerprint/revision-role compilation, pure workflow lowering, and named State Barrier verification are present. Incomplete Apply/Recovery cursor settlement, remaining flat/scalar effect authority, rollback/cleanup closure, and exact-path blocking classification still prevent closeout. | any duplicate source, bypass, parity drift, or unowned exception remains |

## Plan-Attack Dispositions

| Candidate | Disposition | Reason |
| --- | --- | --- |
| Keep the active compiler contract only in the private workbench | reject | The workbench is historical/evidence-only and is not available as public repository authority. `ARCHITECTURE.md` is the discoverable active contract. |
| Create multiple competing compiler contract files | reject | `ARCHITECTURE.md` is the one active cross-cutting contract. This ledger remains derived evidence; narrower code, tests, codecs, and public docs retain their own contracts. |
| Freeze concrete IR structs, stage enums, package names, or private Surface id spellings now | reject | The semantic axes, owners, compatibility classes, and handoffs are sufficient. Concrete shapes are derived during the implementing phase and may not create a new Guarantee. |
| Assign all static facts to one central surface package | reject | Cross-facet coherence is central, but topology, realization, codec, observation, route, and capability facts retain local owners. |
| Treat HookAsset shared placement as one targetless surface | reject | Logical support is target-relative while physical placement is scope-shared and consumer-derived. Many-to-one placement must remain explicit. |
| Require exactly one observation strategy per surface | reject | MCP and relation surfaces already have distinct effective, provider, inventory, runtime-probe, and postcondition purposes. Cardinality is per purpose. |
| Unify operation fingerprint formats while extracting the compiler | reject | Exact compatibility takes precedence; one compiler can own several versioned projections. |
| Put physical rootedpath cost rules in operation planning | reject | Operation planning owns semantic obligations; State Barrier owns physical lowering under selected paths/platform facts. |
| Give State Barrier decision/effect semantics | reject | Reconciliation and Effect remain transition owners; State Barrier grants and revalidates authority only. |
| Introduce a separate transition program immediately | reject | `ARCHITECTURE.md` assigns decision-to-effect and durable-successor transition semantics to Effect. effect-envelope phase makes the projection explicit; no independent owner is currently missing after that assignment. |
| Make read-only workflows acquire effect authority | reject | Existing read barriers are compatibility behavior to preserve or disposition individually; mutation capability is unnecessary. |
| Restore unclassified-package rejection for hostsurface/operationplan | reject | Exact unplaced-package admission is already gone. Compiler role is a prefix predicate on Shadow, not a blocking catalogue row. |
| Feed Shadow into HasFailures / TestTopologyGuardBaseline | reject | This unit is report-only. Blocking cutover is a later owner with equivalent coverage. |
| Import hostsurface or operationplan from archguard | reject | Archguard analyzes go list JSON only and must not become a compiler consumer. |

## Guard-shadow old-rule dispositions

None of these rows delete a blocking guard in this phase.

| Current mechanism | Disposition | Reason |
| --- | --- | --- |
| Phase `importRules`, affinity/role direction for placed packages | retain | Still the blocking owner/refusal and import law for packages that have placement rows. |
| Forbidden shapes, future-family, retired packages | retain | Independent of compiler prefix roles. |
| Production→test-support, read-only workflow, off-diagonal seams | retain | Behavioral/graph contracts outside compiler I/O. |
| Storage/journal/CLI presentation boundary rules | retain | Effect and presentation leaves stay blocking. |
| Exact unplaced-package rejection | demote-to-review | Already removed. Shadow prefix roles provide report-only evidence; they do not replace blocking semantic owner/role classification. |
| `packagePlacementRows` exact lists | replace before closeout | Still the classifier deciding whether blocking affinity checks apply. Report-only Shadow is not equivalent coverage; a semantic blocking owner must replace this exact-path dependency before deletion. |
| Density / LOC / standalone exact-path presence baselines | already gone | Remain review evidence only (ARCH-G20). The separate temporary `packagePlacementRows` blocking classifier is listed above and still requires replacement. |
| Documentation-prose or source-symbol guards | reject restore | Architecture tests stay executable behavior, typed contracts, import graphs, and produced artifacts. |
| Move packages before parity fixtures | reject | Package shape is derived from proven owners and dependency direction, not the starting mechanism. |
| Preserve documentation prose, declaration-name, API-symbol, exact package/file catalogue, or density assertions as migration guards | reject | These mechanisms create brittle reverse authority and high iteration cost. Executable behavior, typed/serialized contracts, import graphs, and produced artifacts are the valid oracles. |

## Closeout census and change locality

Status: provisional census for the bounded MCP cell join, non-MCP catalog plus
list, managed-import, Skill-diagnosis, and selected-order consumer cutovers,
compiled surface consumers; exact operation authority, revision-role, and
fingerprint ownership; reservation-demand seeds; State Barrier ownership and
named verification; readiness middle-end; and report-only compiler-shadow
guards. This is not final program closeout. Ordered effect settlement and
semantic blocking-guard replacement remain required work.

### Removed dual sources

| Former dual | Disposition | Replacement |
| --- | --- | --- |
| Workflow-local authority, revision-role, fingerprint, and domain grammars | deleted after parity cutover and caller census | `internal/operationplan` owns pure facts, `DomainStep` sequences, revision roles, and exact operation-specific fingerprint serializers; workflows only adapt and lower normalized facts |
| Workflow-constructed `ForwardEffectPlan` | deleted | `operationplan.Demand` consumed by `recoverygate.ReserveForwardEffects` |
| `transaction.CaptureStateDirAuthority`, `RequireClearFileSet`, `transaction.StateDirAuthority` | deleted | `recoverygate.CaptureStateDir` / `CaptureStateDirBounded` / `StateDirAuthority` |
| Identity-free production `ObserveClearFence` | unexported | package-local `observeClearFence`; production census is `ObserveClearFenceAt` after capture |
| Workflow MCP target/scope switches in list, authoring, adopt, probe, readiness, observe, help | deleted | `catalog.Product` lookups; owner catalogs remain the fact source |

### Retained boundaries and known remaining residue

| Surface | Reason |
| --- | --- |
| `aggregate.MCPPlacementForSubject` in `realization/lock` | topology/realization must not import catalog |
| Profile route construction | owner-internal; realization cannot import catalog |
| `operationplan.NewDemand` | direct consumer test reconstruction only; production effectful workflows use `CompileApply` or `CompileRefresh` |
| File-set-only `RequireFileSetClear` | lock planning, init BuildPlan, authoring dry-run; no journal blocking |
| Read-only `RequireClear` | list/status/diagnose/probe and planning without retained `EffectAuthority` |
| Workflow `mutation.NewLogicalPathDomain` / `NewPhysicalPathDomain` calls | boundary lowering of compiler-owned `DomainStep` values immediately before lease acquisition; not semantic compilation |
| `recoverygate` domain/revision construction | canonical State Barrier ownership for RecoveryDir/StateDir facts |
| Adopt MCP required-absence recapture | source-specific physical freshness authority separate from generic revision roles |
| Adopt write/rollback revision capture | post-creation rollback identity evidence owned by publication cleanup |
| Probe barrier domain/revision use | observation-only subprocess authority with no durable mutation or operation fingerprint contract |
| `packagePlacementRows` | temporary exact-path classifier for blocking affinity checks; final closeout requires an equivalent semantic classifier before removal |
| `internal/recoverygate` package path | rename would not shorten the import graph |

### Compiler and barrier paths

```text
MCP owner catalogs
-> internal/hostsurface/catalog.Product
-> list, authoring, adopt, probe, readiness, observe, help

normalized apply/refresh/recover authority facts
-> internal/operationplan pure domain steps, authority, and exact fingerprint projections
-> workflow-owned path lowering before lease acquisition

normalized adopt/import paths and barrier facts
-> internal/operationplan domain requests and full/stable revision roles
-> workflow-owned path lowering, authority capture, leases, and publication

normalized init manifest/metadata paths and barrier facts
-> internal/operationplan ordered domain and revision program
-> workflow-owned file-set gate, lowering, leases, re-observation, and publication

normalized lock manifest/lockfile/metadata/local-source/StateDir facts
-> internal/operationplan ordered domain and revision program
-> workflow-owned StateDir authority, cache preparation, build, and publication

normalized authoring/unmanage metadata targets, marker, local-source, and barrier facts
-> internal/operationplan ordered domain and revision programs
-> workflow-owned semantic changes, recovery, StateDir establishment, and file-set publication

coarse CompileApply / CompileRefresh demand seed
-> recoverygate.ReserveForwardEffects
-> StateDir / RecoveryDir authority

remaining required path
-> typed ordered effect obligations and settlement
-> canonical demand
-> State Barrier reservation and consumption

fileset mechanics
-> declaration/transaction adapter and recoverygate census
```

### Guard-rule locality

| Rule family | Blocking? | Closeout |
| --- | --- | --- |
| Import-direction, effect-boundary, forbidden shapes | yes | retained |
| `Report.Shadow` compiler prefix roles | no | retained report-only; never feeds `HasFailures` |
| `packagePlacementRows` exact lists | yes, for packages present in the catalogue | temporary classifier; replace with equivalent semantic blocking coverage before removal |
| Density / prose / symbol-presence | n/a | remain gone |

### Representative change paths

| Perturbation | Expected locality | Evidence |
| --- | --- | --- |
| New OS adapter | physical adapters and platform admission | `compiler-os-specialization` shadow fixtures |
| Extra catalog package | Host-Surface compiler prefix | shadow near-neighbor fixtures |
| New realization form | Realization plus Reconciliation/Effect variants | forbidden-shape and perturbation fixtures |
| New observe purpose | Assurance fact plus surface observation binding | shadow perturbation fixtures |
| Recovery hardening | `recoverygate` and Effect/recovery mechanisms | first-incarnation fault matrix; named CI deferred |

### Required remaining program work

- Replacement of the coarse count projection with typed ordered effect
  obligations, closed branch groups, settlement, rollback, and cleanup mapping
  consumed by runtime reservation and validation.
- Equivalent blocking semantic package-owner coverage before
  `packagePlacementRows` can be removed.

Broad ARCH-N07 redesigns remain outside this program, but moving an existing
workflow's operation-safety grammar to the canonical compiler is inside it.

Numeric package or line-count deltas are evidence only. Success is one primary
owner per invariant, contained capability surfaces, and exact compatible
contracts.

The remaining work is known rather than an unresolved design Unknown. Material
evidence that changes one of these decisions reopens the contract-freeze phase
and updates `ARCHITECTURE.md` and affected narrower contracts before
implementation continues.
