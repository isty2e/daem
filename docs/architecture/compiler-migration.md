# Compiler Migration Ledger

Status: active implementation-evidence ledger derived from
[`ARCHITECTURE.md`](../../ARCHITECTURE.md). It records the current source
layout, compatibility fixtures, and finite migration handoffs. It is not a
runtime registry, public support matrix, persisted schema, or independent
architecture authority.

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
| ARCH-G10 | guarantee | Operation authority facts, canonical ordering, deduplication/conflicts, mutation domains, lifecycle-named revision sets, and operation fingerprints have one pure compiler owner. | User decision; `ARCHITECTURE.md`. | Workflow-local apply/refresh/recover compilers. | Exact domains, revisions, ordering, and fingerprint values. | operation-authority phase. | A parity failure proves two apparently shared facts have different canonical semantics. |
| ARCH-G11 | guarantee | Operation compilation performs no filesystem, subprocess, persistence, capability acquisition, or presentation work; all flexible boundary facts are normalized before lowering. | User decision; `ARCHITECTURE.md`. | Current workflows mix some observation with compilation. | Behavioral parity. | operation-authority phase/readiness phase. | A required semantic input cannot be observed before compilation without changing the operation contract. |
| ARCH-G12 | guarantee | Every effectful operation has a typed, ordered effect envelope whose complete demand is admitted before its first external or visibility effect. | User decision; `ARCHITECTURE.md` transaction/recovery contract. | `ForwardEffectPlan`, workflow count arithmetic, effect tests. | Exact refusal timing and effect ordering. | effect-envelope phase. | Supported dynamic work cannot be bounded before effects and the product contract must be reduced or separately changed. |
| ARCH-G13 | guarantee | Exclusive branches reserve the maximum reachable demand rather than the sum; sequential and repeated obligations preserve exact bounded demand and cannot add work after effects begin. | User decision; `ARCHITECTURE.md`. | Current branch-specific arithmetic. | Behavioral parity and pre-effect refusal parity. | effect-envelope phase. | Runtime evidence proves branches are not closed/exclusive or promotion can create unplanned work. |
| ARCH-G14 | guarantee | The operation plan describes semantic obligations; the State Barrier alone lowers them with selected paths and platform facts into physical StateDir/RecoveryDir capacity and consumable authority. | User decision; `ARCHITECTURE.md` and current lifecycle contracts. | `ForwardEffectPlan` plus `StateDirAuthority.ReserveOperation`. | Behavioral parity. | effect-envelope phase/state-barrier phase. | Physical resource cost cannot be derived without moving operation semantics into the barrier owner. |
| ARCH-G15 | guarantee | One logical State Barrier owns StateDir/RecoveryDir authority, journal and file-set axes, first-incarnation evidence, reservation consumption, phase revalidation, cancellation precedence, and terminal barrier classification. | User decision; `ARCHITECTURE.md` and recovery contracts. | `declaration/transaction`, `recoverygate`, and workflow-local sequencing. | Exact failure precedence and visibility classification. | state-barrier phase. | A fact has a different lifecycle/authority and must remain in a lower mechanics owner. |
| ARCH-G16 | guarantee | Generic file-set marker, snapshot, publication, and recovery mechanics remain below State Barrier policy; storage, journal, execute, and subprocess remain effect mechanisms rather than semantic planners. | User decision; `ARCHITECTURE.md`. | `declaration/transaction`, storage/commit, journal, execute. | Exact marker/journal/persistence behavior. | state-barrier phase. | Dependency evidence shows the proposed lower mechanics boundary creates a reverse import or public facade. |
| ARCH-G17 | guarantee | Workflows gather boundary evidence, invoke pure owners, acquire capabilities, sequence effects, and project results; they do not retain competing surface, authority, fingerprint, or capacity grammars. | User decision; `ARCHITECTURE.md`. | Current workflow-local compilers. | Behavioral parity. | surface-cutover phase through readiness phase. | A use case owns a genuinely operation-specific semantic contract not representable by the shared compiler. |
| ARCH-G18 | guarantee | Old implementation remains authoritative during shadow comparison. A caller flips only after exact required parity, and the old source is deleted after its last consumer moves. | User decision; architecture change rules. | Existing old paths; future parity fixtures. | Exact or behavioral as classified below. | surface-schema phase through closeout phase. | A separately authorized migration requires staged dual-version support. |
| ARCH-G19 | guarantee | OS specialization remains at platform admission, physical authority, storage/filesystem, and subprocess boundaries; surface and operation compiler semantics are OS-independent. | `ARCHITECTURE.md`. | Current platform-specialized file census and archguard. | Behavioral parity. | surface-schema phase through guard-shadow phase. | A supported semantic distinction is proved to depend on OS rather than capability or physical adapter. |
| ARCH-G20 | guarantee | Architecture policy remains in active contracts; archguard provides executable evidence and never becomes runtime or normative authority. | `ARCHITECTURE.md`. | Current archguard and design contracts. | Repository behavior only. | guard-shadow phase. | Governance deliberately assigns policy authority to another canonical document. |
| ARCH-G21 | guarantee | Form-specific transition ownership remains explicit: Reconciliation owns decisions; Effect owns decision-to-effect, journal-before-mutation, outcome, rollback, and durable-successor semantics; State Barrier grants authority but does not reinterpret the transition. | `ARCHITECTURE.md` and current lifecycle contracts. | `reconcile`, `effect/execute`, journal, workflows. | Exact effect/recovery behavior. | effect-envelope phase/state-barrier phase, with existing Effect owner retained. | Evidence shows a transition is currently owned by another canonical model or needs a new form-specific algebra. |
| ARCH-G22 | guarantee | Manifest, lockfile, statefile, journal, registries, and CLI DTOs retain independent compatibility lifecycles and are never unified as compiler IR. | `ARCHITECTURE.md` and user decision. | Versioned codecs and strict consumers. | Exact parity unless separately versioned. | all migration phases. | Separately authorized boundary migration. |

### Retained non-goals

| ID | Disposition | Statement and rationale | Decision authority / owner | Reopen condition |
| --- | --- | --- | --- | --- |
| ARCH-N01 | reject | Persist or publicly expose `SurfaceID`. Existing durable identities remain authoritative; a new id would add an unnecessary compatibility axis. | User decision; `ARCHITECTURE.md`. | A durable query cannot be answered by existing placement/topology/route ids and requires a versioned identity. |
| ARCH-N02 | reject | One mega SurfaceContract, AgentProfile interface, generic plugin registry, callback catalog, or `map[string]any` IR. These collapse independent identity, lifecycle, invalid-state, and failure semantics. | User decision; `ARCHITECTURE.md`. | No reopen under the current ownership contract. |
| ARCH-N03 | reject | Treat delegate/host route as a fourth realization form. It is actuation over a locked representation or relation. | `ARCHITECTURE.md` and current lifecycle contracts. | A new representation has independent identity and state, not merely a new operation mechanism. |
| ARCH-N04 | defer | Exact Go package names and the private spelling of internal Surface ids. Logical owner and dependency direction are fixed; physical placement follows package/cycle evidence. | `ARCHITECTURE.md`; implementing phase. | Each phase reaches implementation design. |
| ARCH-N05 | defer | Rename `recoverygate` to `statebarrier`. Ownership consolidation matters first; a rename is allowed only at final closeout if it shortens the public/import graph without a facade. | `ARCHITECTURE.md`; closeout phase. | state-barrier phase complete and final package graph available. |
| ARCH-N06 | reject | Unify apply, refresh, and recover fingerprint wire projections during this migration. One compiler may own several exact compatibility projections. | Compatibility authority. | Separately authorized fingerprint-version migration. |
| ARCH-N07 | reject | Immediate Paths, adopt-family, payload, findings, SubjectKind, or persisted DTO overhaul. These require independent owners and compatibility decisions. | User decision; `ARCHITECTURE.md`. | Search finds a distinct active owner or architecture closeout creates one from new evidence. |
| ARCH-N08 | reject | Use package, file, LOC, or density reduction as acceptance. Numeric changes are evidence only. | `ARCHITECTURE.md` and user decision. | No reopen under current architecture policy. |

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
| MCP aggregate placement | `realization/aggregate` | topology namespace rows, codec operation registry, profile, adopt/readiness/status | Realization/topology/operation/observation views | Exact placement and codec contract ids |
| Support | `realization/profile` | readiness/help/docs | Target support view | Public product wording and platform capability |
| Discovery/runtime | `realization/profile` | adopt/list/diagnose | Observation/discovery views | Filesystem observation and import authority |
| Operation routes | `realization/profile` | effect adapters, apply/refresh, lock | Operation view | Adapter behavior and effect outcomes |
| Runtime probe | `realization/profile` + runtime-probe adapter | help/probe/readiness | Observation-purpose view | Probe evidence and execution |
| Provider/effective policy | profile/provider and Assurance/readiness owners | authoring, readiness, status | Purpose-specific views | Current evidence and provider lifecycle semantics |
| Extension order | `realization/profile` + relation evidence | readiness/apply | Observation and actuation views | Current physical sequence and revision evidence |

## Operation And Transition Ledger

| Operation / mode | Current planning and identity owner | Current barrier/freshness | Effects and transition owner | Required migration disposition |
| --- | --- | --- | --- | --- |
| Apply dry-run | `workflow/apply` readiness, operation fingerprint, authority facts | EffectAuthority captured; no effect authority consumed | no host mutation; disclosure only | operation-authority phase compiles authority/fingerprint; effect-envelope phase compiles no-effect envelope; preserve disclosure exactness. |
| Apply write | `workflow/apply` plus `workflow/readiness` | project root, leases, revisions, EffectAuthority, provider and final replans | `effect/execute`, host-route/delegate adapters, journal, state/claim stores | operation-authority, effect-envelope, and state-barrier phases migrate the complete operation and every subpath. |
| Apply provider phase | workflow-local provider action preparation and post-provider fingerprints | reserved pre-effect StateDir envelope, barrier replans, renewed consent | host route then replanning | One operation envelope spans provider and final phases; no later fresh reservation. |
| Managed path/aggregate mutation | Reconciliation decisions plus apply-local authority facts | physical/logical leases, revisions, retained roots | `effect/execute`, journal, storage/commit | Effect remains form-specific transition owner; compiler supplies typed obligations and facts. |
| Host relation install | Reconciliation relation action plus apply route facts | pre/post command authority and post-observation | hostroute adapter, attempt/pending/claim persistence | Every command and durable write remains in the same envelope and authority. |
| Carrier removal | carrier-absence plan plus apply-local fingerprints/counts | claim/pending baselines, relation/effect observations, retained StateDir | host route or direct relation mutation, attempt/retirement persistence | Preserve current write-ahead, verification, retirement, and retained-pending semantics. |
| Relation order | relation-order decisions and workflow class-count logic | physical sequence revisions and retained roots | bounded config sequence mutation | Reserve by runtime mutation class and preserve foreign rows; Effect owns transition. |
| Delegate attempt | delegate decisions and remaining-execution fingerprint | pre-invocation and post-attempt authority | delegate adapter and bounded attempt persistence | One obligation per invocation and persistence result; history never becomes currentness. |
| Statefile/claim persistence | workflow-local statefile plans and exact stores | descendant reservation, StateDir and peer revisions | Effect/store commit | All descendant writes are planned and reserved before first effect. |
| Refresh dry-run | `workflow/refresh` fingerprint and authority compiler | EffectAuthority plus project root/revisions | disclosure only | operation-authority phase exact parity; effect-envelope phase no-effect envelope. |
| Refresh execute | refresh plan/authority, host timeout, observation | leases/revisions, EffectAuthority before/after host, persistence subset | host adapter, post-observation, attempt-history persistence | Preserve partial result and post-host persistence-revision subset. |
| Recover active journal | `workflow/recover` recovery/authority fingerprints | retained journal, StateDir and project/root authority, recovery leases | journal recovery plan, execute/storage, guarded retirement | operation-authority phase owns pure authority projection; state-barrier phase owns barrier capability, not recovery semantics. |
| Recover cleanup-only | recovery cleanup fingerprint/authority | retained RecoveryDir authority; StateDir-independent | journal retirement | Preserve cleanup-only independence and continuing file-set fence disclosure. |
| Import/adopt dry-run | adopt CandidateSet/Plan identity and workflow plan | physical source authorities, revisions, recovery barrier | no publication | Surface/readiness views may change inputs; exact import plan identity stays owned by adopt until explicitly migrated. |
| Import/adopt write | workflow/adopt plus `Plan.IdentityBytes` | barrier, manifest/source freshness, final pre-publication validation | source/skill publication then manifest transaction | state-barrier phase migrates barrier usage; operation-authority phase expansion requires explicit later scope, not inference. |
| Authoring metadata write | workflow/authoring | barrier, manifest/lock revisions, project/root authority | recoverable manifest/lock/state/registry transaction | state-barrier phase migrates barrier only; declaration semantics remain owner-local. |
| Unmanage | workflow/authoring unmanage | barrier and durable owner revisions | journaled manifest/lock/state/registry release, no host effect | Preserve host state and all-owner atomicity. |
| Init | workflow/init plus declaration transaction | direct file-set gate then EffectAuthority | manifest publication | Remove duplicate gate only when State Barrier preserves existing refusal timing. |
| Lock dry-run/outdated | workflow/lock | file-set fence and source/declaration freshness as currently required | no lockfile publication | Preserve existing compatibility; do not impose journal recovery blocking by generalization. |
| Lock write | workflow/lock mutation | direct StateDirAuthority/file-set fence, source/build revisions | lockfile publication | Migrate StateDir authority without changing lock availability or selected-surface semantics. |
| Prepared MCP probe | workflow/probe | EffectAuthority prevents concurrent recovery/file-set ambiguity | explicit subprocess probe, no durable readiness persistence | Keep runtime evidence operation-local; barrier does not make probe a mutation realization. |
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
| recover | bounded `transaction.CaptureStateDirAuthorityBounded` plus recovery plans | recovery | Retain recovery-specific authority while moving canonical StateDir custody to State Barrier. |
| lock mutation | `transaction.CaptureStateDirAuthority` | lockfile write | Preserve lock compatibility while using canonical StateDir authority. |
| init, authoring, lock | direct `transaction.RequireClearFileSet` | metadata planning/write | Remove duplicate mutating gate only after parity; no new journal block for lock operations. |
| list/status/diagnose/probe-read paths | `recoverygate.RequireClear` | read-only refusal | Preserve or explicitly disposition each read-only compatibility gate; never grant mutation authority. |
| recoverygate itself | `transaction.StateDirAuthority` and raw `ForwardEffectPlan` | joint barrier and reservation | Becomes the logical State Barrier owner; lower mechanics stay separate. |

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
| Apply operation and authority fingerprints | workflow/apply current projection | exact digest values for identical inputs | no cross-operation format unification |
| Refresh fingerprints and revision subsets | workflow/refresh | exact digest values and full/persistence subset semantics | no colon/NUL normalization by convenience |
| Recovery fingerprints | workflow/recover and journal plan | exact active/cleanup identity | no merge with forward operation identity |
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
| MCP topology cells | `internal/topology/mcp`: `TestProjectionNamespaceCatalogIsCompleteAndUnique`, `TestProjectionSubjectOwnsCanonicalIdentityForEveryPlacement` | exact namespaces, identity, completeness | surface-schema phase adds compiled-view parity for the same rows. |
| Hook topology | `internal/topology/hook`: `TestLowerOwnsTheCompleteDesiredTopology`, `TestAssetSubjectIdentityExcludesContentAndConsumers` | complete target graph and shared HookAsset identity | surface-schema phase proves surface mapping does not create duplicate assets. |
| MCP placements | `internal/realization/aggregate`: `TestImplementedMCPPlacementsExposeCurrentRowsOnly`, `TestMCPPlacementStaticContractAccessorsAreCanonicalAndDefensive`, catalog duplicate tests | exact rows, paths, codecs, policies, cardinality | surface-schema phase compares every facet and durable id. |
| Target/profile views | `internal/realization/profile`: `TestProfilesPreserveCurrentSupportAndRealizationMatrix`, `TestProfileSeparatesPlacementDiscoveryRuntimeAndRoutes`, `TestSharedPlacementsRemainOnePhysicalIdentity`, `TestProfilePreservesMCPPlacementScopeMatrix` | support, placement, discovery/runtime/route separation and shared placement | surface-cutover phase shadows every consumer before cutover. |
| Probe/order capability | profile probe/order tests | exact independent capability rows and defensive copies | surface-schema phase models purpose/cardinality explicitly. |
| Architecture separation | `internal/archguard`: MCP topology authority, profile, host-branch, Pi placement, and dependency-direction tests | current owner/refusal and import law | guard-shadow phase adds report-only semantic replacement after implementation. |
| Apply authority/fingerprint | `internal/workflow/apply`: `TestBuildApplyAuthorityEvidenceCoversAuthoritativePaths`, `TestApplyOperationFingerprintBindsPlanAndDelegateMode`, `TestApplyOperationFingerprintIncludesStatefileSemanticsWitness`, relation/order/carrier fingerprint tests | deterministic sensitivity and current authority coverage | operation-authority phase adds old/new exact equality; no fixed literal digest is required before a second compiler exists. |
| Refresh authority/fingerprint | refresh timeout/freshness/replan tests including `TestRefreshTimeoutParticipatesInDisclosureAndFingerprint` | operation identity, authority revalidation, persistence subset behavior | operation-authority phase adds shared compiler differential fixtures. |
| Recovery authority/fingerprint | recover execution/authority/replacement/cleanup tests | active and cleanup authority, drift, terminal classification | operation-authority and state-barrier differential and fault tests. |
| Forward reservation | recoverygate reservation and high-cardinality tests; apply `TestExecuteReservesCompleteStateDirEnvelopeBeforeProviderInvocation` | pre-effect atomic reservation and exact consumption | effect-envelope phase shadows current raw counts with typed envelope demand. |
| StateDir/file-set mechanics | declaration/transaction StateDir, descendant, marker, rollback, recovery, cancellation tests | first-incarnation, budgets, residue, restore, limits | state-barrier phase moves ownership without changing mechanics. |
| Public/wire contracts | versioned codec tests, `test/testkit/clijson`, contractversion and documentation guards | exact schemas and strict consumers | Each phase runs affected consumers; no new public field. |

### Verification commands

```sh
cd daem
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/topology/mcp ./internal/topology/hook \
    ./internal/realization/aggregate ./internal/realization/profile
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/workflow/apply ./internal/workflow/refresh \
    ./internal/workflow/recover ./internal/recoverygate \
    ./internal/declaration/transaction
GOENV=off GOFLAGS= GOWORK=off HOME="$(mktemp -d)" \
  go test ./internal/archguard ./internal/platformsupport ./test/testkit/clijson
```

Repository-wide lanes remain mandatory for implementation phases; these
focused commands identify the old-model parity oracles.

## Phase Handoffs And Reset Rules

| Phase | Consumes | Must produce | Reset/reopen condition |
| --- | --- | --- | --- |
| surface-schema phase | surface cell, owner, compatibility, and fixture rows above | internal identity/schema candidate plus MCP shadow views and exact parity | key axes cannot distinguish a current cell, durable id would change, or owner dependency reverses |
| surface-cutover phase | surface-schema phase views and consumer census | bounded cutover, deletion map, one source per migrated facet | any consumer requires different semantics or facade becomes authority |
| operation-authority phase | operation/transition/compatibility rows | pure fact/domain/revision/fingerprint compiler | exact parity fails because facts are not actually shared |
| effect-envelope phase | operation-authority phase facts and operation closure | typed effect envelope and derived semantic demand | work cannot be bounded before effects or branch algebra is not closed |
| state-barrier phase | demand plus State Barrier/caller ledger | canonical authority owner and lower file-set mechanics | public recovery semantics or platform guarantees must change |
| readiness phase | active surface views and operation vocabulary | observation shell and pure readiness middle-end | observation order/evidence semantics cannot remain exact |
| guard-shadow phase | implemented owner graph | report-only semantic guards and old/new rule dispositions | proposed rule cannot reject forbidden fixtures and admit near-neighbors |
| closeout phase | every prior artifact | no duplicate compiler/facade/residue plus final locality report | any duplicate source, bypass, parity drift, or unowned exception remains |

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
| Move packages before parity fixtures | reject | Package shape is derived from proven owners and dependency direction, not the starting mechanism. |

No blocking Unknown remains after these dispositions. Material evidence that
changes one of these decisions reopens the contract-freeze phase and updates
`ARCHITECTURE.md` and affected narrower contracts before implementation
continues.
