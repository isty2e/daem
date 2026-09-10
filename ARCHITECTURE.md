# Daem Architecture Contract

This contract governs internal ownership, compiler boundaries, transitions and
dependency direction. Public syntax and behavior remain governed by `docs/`,
versioned codecs and their strict consumers; canonical Go models implement them.
Verify contracts through executable inputs, calls, interfaces and artifacts,
not assertions about prose, symbol names, file catalogues or numeric density.

## Product Flow

```text
boundary declaration
-> canonical desired environment
-> structural subjects + supplied content
-> locked realization contracts
-> qualified current evidence
-> pure reconciliation decisions
-> authorized, journaled, verified effects
-> managed state + boundary presentation
```

Topology and Supply are sibling inputs. A family may produce either or both.
Post-effect verification is a newly sequenced current observation, not a
reverse dependency from Effect into observation adapters.

Every flexible boundary input is normalized once before entering a canonical
owner. Raw syntax, filesystem paths, subprocess output, host config, timestamps,
and presentation DTOs do not flow through the core as generic bags.

## Retained Semantic Owners

| Owner | Owns | Refuses |
| --- | --- | --- |
| Desired | normalized authored environment, family identity, and desired policy | syntax, source I/O, placement, current evidence, and effects |
| Topology | stable structural SubjectID, subjects, edges, lowering, and graph validity | placement, current evidence, routes, effects, and presentation |
| Supply | source/provenance, exact content identity, derivation, and repair contracts | desired policy, host placement, current state, and mutation authority |
| Realization | exactly managed path projection, managed aggregate contribution, or delegated relation plus locked operation contracts | current observation, effect outcome, and boundary syntax |
| Assurance | qualified current evidence, durable managed facts, evidence lifetime, and authority inputs | desired state, mutation decisions, and currentness inferred from history |
| Reconciliation | pure decisions over locked contracts, current evidence, durable authority, and explicit policy | I/O, journaling, mutation, and presentation |
| Effect | decision-to-effect transition, journal-before-mutation, verified outcomes, compensation, and durable-successor semantics | desired inference, observation construction, and presentation |
| Boundary mechanisms | codecs, adapters, workflow sequencing, storage, subprocess, CLI, and presentation | canonical semantic reinterpretation |

These owners are conceptual contracts, not instructions to create one package
per row. Package creation still requires an independent invariant, boundary,
lifecycle, volatility seam, caller set, and test surface.

## Compilation Boundaries

Daem retains the owners above but makes two cross-owner compilations explicit.
They are orthogonal and must not be combined into one framework or IR.

```text
owner-local static host facts
-> Host-Surface compiler
-> immutable target/topology/realization/observation/operation views

normalized operation facts + reconciliation + current evidence
-> Operation-Safety compiler
-> authority plan + effect envelope + semantic demand
```

A third component is effectful rather than a compiler:

```text
operation semantic demand + current journal/file-set/StateDir facts
-> State Barrier
-> physical reservation + single-operation authority
```

## Bounded Delivery And Deferred Work

Compiler and State Barrier work does not authorize product, support, CLI or
wire redesign, or an overhaul of SubjectID, Paths, adopt families, payloads,
findings or DTOs. It does not require a universal execution framework or
converting every remaining operation/checkpoint to one cursor, including for
release 0.2.0. Deferral never waives a demonstrated violation of an accepted contract.

The following are explicit **NON-GOALS** for this delivery. They are deferred,
not implemented or proven unnecessary. The repository maintainer owns each
revisit. This section is their canonical disposition; task plans derive from it.

| Deferred work | Rationale and retained boundary | Reopen condition |
| --- | --- | --- |
| Whole-operation fine-grained cursor coverage, including prepared-host settlement, relation order and delegates | Explicit authority, visibility, persistence and recovery boundaries remain required; a generic IR for every observation, no-op and cleanup step is not. Existing cursor segments stay enabled. | A reproduced supported-path ordering/authority defect, or an approved feature with a bounded design showing why a cursor is preferable to owner-local enforcement. |
| Universal scalar removal and exact-frontier admission | Apply retains its structural checks and conservative scalar reservation; Refresh retains its structural frontier and cursor-backed authority. Completing one universal representation is not a product guarantee. | Measured avoidable refusal or maintenance cost, or a reproduced reservation defect, with a lifecycle-local cutover that actually retires the competing representation. |
| Semantic package-classifier replacement | Existing blocking import/effect guards stay enabled. Exact placement classification has limited coverage; shadow diagnostics are not equivalent blocking coverage. No universal classification proof is claimed. | A concrete uncovered forbidden dependency or a separately approved bounded classifier replacement with forbidden and legitimate-neighbor evidence. |
| Universal effect-structure size policy | Existing document, action, repetition and demand-frontier limits remain. A new global structure cap needs a supported workload and its own admission decision. | A supported workload demonstrates excessive structural retention or a new operation materially changes construction bounds. |
| Remaining migration-residue removal and package renaming | A live compatibility path is not dead residue. Do not remove authority, parity evidence or owner-local APIs merely to finish a checklist. | Last-consumer evidence makes a concrete deletion possible without weakening a contract, or a changed dependency graph justifies relocation. |
| New blocked-delegate readiness or persistence modes | Aggregate-blocker rejection does not prove every blocked input unreachable; authority requirements must be resolved before enabling new modes. | A supported public execution failure requiring a maintainer decision on those requirements. |
| Broader provider core rebinding | Current prepared/current and both final-schedule checks remain strict; no general mid-execution provider-version-change guarantee. | A supported reproduction that requires broader rebinding. |

Linux cross-boot recovery admission, public/durable formats, product support,
and fingerprint compatibility are not changed by these deferrals. Reconsidering
one requires a separate product or compatibility decision.

### Platform Maintenance Decisions

**Deferred Darwin admission policy (NON-GOAL):** the maintainer defers blanket
nonzero generation/birth-time admission for mutation roots, durable provenance
and StateDir witnesses. A macOS 26.6.2 probe set `ATTR_CMN_CRTIME` to epoch zero:
root/provenance/StateDir capture accepted the zero tuple, while StateDir
revalidation rejected rename-and-recreate. That tuple alone does not establish
unavailable identity; the probe proves neither inode-reuse safety nor every
filesystem's missing-metadata behavior. Existing root/StateDir identity contracts
remain. Reopen on a supported unavailable-incarnation or missed-replacement
reproduction, or an explicit nonzero-admission decision. This does not change
[artifact-view admission](docs/platforms.md#artifact-paths).

Platform contributors must distinguish capability tests from product admission:
Windows has native retained-root, observation and handle-relative storage
publication/removal jobs; FreeBSD, NetBSD and OpenBSD filesnapshot/Codex
observation jobs are compile-only, not native execution. Neither changes the
public support matrix.

## Host-Surface Compiler

### Identity and axes

One logical surface is selected by:

```text
(target, scope, desired family, variant)
```

`variant` distinguishes independently selectable host contracts, such as
alternate managed-path placements. It does not encode current evidence, an
effect outcome, a platform capability, or a persisted occurrence.

The implementation may use an opaque internal `SurfaceID` as a linkage key.
That id is not persisted or public and does not replace existing SubjectID,
placement, codec, route, adapter-contract, lock, or state identity.

### Normalized facets

Static facets stay typed and owner-local: identity, representation, namespace,
placement, codec, observation purpose, operation/dispatch, discovery/runtime
location, selection/defaults, support and capability. They are not an
optional-field `SurfaceContract`. The compiler owns only cross-facet
referential integrity, required/forbidden cardinality and immutable derived views.

### Cardinality and pressure cases

The compiler must enforce:

- one logical identity for each admitted surface key;
- exactly one primary representation form;
- one Topology identity rule when the surface creates a structural subject;
- placement and codec only when required by the representation;
- zero or more observations distinguished by explicit purpose;
- at most one selected actuator per operation and dispatch class;
- explicit default selection when several variants are admitted;
- explicit unsupported product facts rather than partial surface rows; and
- explicit many-to-one mapping when several logical surfaces share one
  physical placement.

Shared instruction and skill destinations and shared HookAsset paths are
required cases: target-relative support remains distinct even when physical
occupancy is shared. MCP effective-state, provider, inventory, and runtime
probe facts are distinct observation purposes, not duplicate placements.
Delegate or host-route behavior is actuation, not a fourth realization form.

### Compilation behavior

Compilation is deterministic, immutable, and I/O-free. It stores stable
contract references, never adapter objects, callbacks, mutable registrations,
or host behavior. Compiled views expose only the facts required by each
consumer. A target-profile API may exist temporarily as a derived compatibility
view, but it cannot retain another source of static facts.

Product support and runtime/platform capability remain distinct. Support cannot
manufacture capability, and missing optional capability cannot silently make
the product unsupported.

## Operation-Safety Compiler

### Inputs and outputs

Each operation has a narrow typed input built from normalized boundary facts,
pure Reconciliation decisions, qualified current evidence, durable authority
references, and an opaque barrier identity contribution.

The compiler alone owns:

```text
canonical authority fact validity
ordering, deduplication, and conflicts
logical/physical/route mutation domains
lifecycle-named revision sets
exact operation-specific fingerprint projections
typed effect obligations for migrated lifecycles
semantic reservation demand
```

It performs no filesystem access, path discovery, subprocess execution,
persistence, capability acquisition, host-private parsing, workflow
confirmation, or presentation.

### Authority and compatibility

Operations share the fact algebra, not fingerprint projections: full Apply,
provider-stable and remaining Apply, Refresh, and active/cleanup Recovery stay
distinct. [Exact parity](#exact-parity) includes every currently identity-bearing field,
even diagnostic detail. Unifying or removing projections requires a separate
versioned compatibility decision.

### Effect envelopes

Where an effect envelope is used, it is typed and ordered. It preserves the
operation's relevant distinctions:

```text
sequence
closed exclusive branches
bounded repetition
promotion/finalization work
external effect boundaries
post-effect observation
persistence
rollback and compensation
cleanup and retirement
```

All supported work must fit a safe bounded reservation admitted before the
first external or visibility effect. No callback, replan, or workflow may add
unreserved work afterward. A proven exclusive branch may use its maximum
reachable demand instead of summing impossible paths. Exact frontier precision
is a lifecycle-specific implementation choice, not a universal completion
requirement; this amendment does not change any current admission limit or
refusal behavior.

A typed envelope or cursor is not required for every internal checkpoint.
Unmigrated segments retain their owner-local order, validation, persistence and
recovery protocols. Existing envelope/cursor checks must not be bypassed.
Dry-run and no-op operations do not create StateDir merely because they were
planned, whether represented by an envelope or an owner-local no-effect path.

### Transition ownership

The canonical transition direction remains:

```text
Reconciliation decision
-> Effect-owned executable intent
-> journal-before-mutation record
-> verified outcome or compensation
-> durable successor
```

The compiler makes obligations and projections explicit but does not take
Effect's semantic transition authority. Form-specific decision-to-effect,
journal, rollback, and durable-successor semantics remain with Effect.

Each pending carrier completion has one execution-phase owner. A scheduled
provider invocation owns its exact matching completion; the later core and
final promotion must not plan that completion again. Planning may project
remaining pending work, but must not publish predicted state or replace
observed claim settlement. Completions without a scheduled invocation retain
their existing core or final-promotion owner.

Preserve continuation order: core Apply, global retirement, carrier removal,
final routes, relation order, delegates, then terminal-last global adoption.
Both provider replan gates compare final schedules. Planning and execution
pass full relation facts, including NoOp facts needed for pending project
claims. Never rebuild an empty scheduled descendant reservation from pre-core
relations. Core clears exact pending global installs backed by committed
registry claims; final promotion includes only remaining registry/statefile work.

Keep the bounded core/failure-settlement, carrier settlement/removal, final
host-route prefix and recovery cursor segments. The final prefix ends before
prepared host commands; provider prerequisites have a separate entrypoint.
These segments do not replace owner-local exact-baseline CAS, registry-first
split writes, retry or successor preservation. Prepared/current core plans,
full pre-effect reservation and both final-schedule comparisons remain required.
Apply checks structural demand against its conservative scalar reservation;
Refresh couples its structural frontier to StateDir/descendant execution.
Cleanup-only Recovery keeps RecoveryDir-only budgets, independent of StateDir
census; active Recovery retains journal/Effect transition and budget authority.

## Stored Skill Repair Contract

User opt-in and replay behavior are documented in
[Skill Compatibility](docs/compatibility.md). For generated lock codec changes:

- A repaired Skill's `locked.subject.exact_supply` is the exact output for
  apply/status. Original input remains distinct in both
  `derivation.deterministic_transform.input_identity` and `repair_recipe.input`.
- The deterministic transform records `recipe_hash`,
  `algorithm_id = "compat.skill.repair"`, `algorithm_version = "v1"` and
  `execution_domain = "daem:compat/skill/repair"`. Its
  `expected_output_identity` and `repair_recipe.output` must equal `exact_supply`;
  both input identities must agree. Each exact identity includes `source_id`,
  `resolved_ref`, `kind` and `content_hash`.
- `repair_recipe.version = 1`; recompute its `recipe_hash` from the canonical
  ordered `[[locked.subject.repair_recipe.operation]]` records. Paths are
  normalized slash-separated artifact-relative paths; old/new bytes use base64
  and file modes use decimal integers (for example, 420 for 0644).
- Reject recipes on non-Skill subjects, missing/mismatched identities,
  unsupported operations, fields belonging to a different operation, unsafe
  paths, malformed base64 or absent preconditions. Apply never substitutes
  output identity for original input or reruns compatibility inference.

## State Barrier

The logical State Barrier lowers semantic operation demand with selected path
shape and current capability into physical reservation and consumable
authority.

It owns:

- retained StateDir and RecoveryDir identity;
- independent journal and file-set axes;
- first-incarnation creation evidence;
- shared/exclusive StateDir admission;
- physical path, entry, byte, observation, and descendant-work reservation;
- pre/post-effect and pre-persistence validation;
- cancellation precedence; and
- terminal barrier classification.

It does not decide what semantic effect should run, construct a journal record,
perform storage syscalls, parse host syntax, or construct a durable successor.
Generic file-set mechanics, journal serialization, rooted filesystem access,
storage publication, subprocess execution, and form-specific Effect semantics
remain lower or peer mechanisms.

Read-only journal/file-set refusals are not automatically mutation authority.
Each status, list, diagnose, help, probe, lock, or other read path retains its
operation-specific compatibility behavior until explicitly compared and
dispositioned. Shared StateDir ownership must not make every reader acquire an
effect capability or make lock operations inherit an unrelated recovery block.

## Workflow Role

Workflows select context/paths, gather evidence, invoke compilers, acquire
leases/capabilities, sequence authorized effects, coordinate confirmation and
cancellation, and assemble results. They do not import other workflows or
redefine surface matrices, authority grammar, fingerprints, host syntax,
physical reservation or StateDir protocols.

Operation-specific semantic count projections may remain workflow-owned under
the retained compatibility seam. Physical lowering and capability consumption
remain State Barrier-owned.

## Compatibility Classes

### Exact parity

The migration preserves exactly:

- manifest, lockfile, statefile, journal, registry, and CLI schemas and retained
  bytes/ordering;
- SubjectID and current placement, codec, route, adapter-contract, lock/state
  projection ids;
- operation and authority fingerprint values for identical inputs;
- mutation domains, revision membership/roles, and deterministic ordering;
- effect ordering, visibility classifications, cancellation and stale-state
  precedence; and
- current target/scope support and unsupported outcomes.

### Behavioral parity

Private package paths, helper types, lookup data structures, compiled-view
representations, and algorithms may change when every observable and persisted
contract above remains exact.

### Separately authorized changes

Any schema version, persisted/public field, stable id, CLI wording contract,
product support stance, effect visibility outcome, historical artifact
interpretation, or fingerprint projection change stops the current migration
unit and requires an explicit compatibility decision.

## Migration Rule

Keep the current implementation authoritative during shadow evaluation. Move
callers only after same-input parity is proved; remove the superseded mechanism
after its last consumer moves. Never normalize away a parity failure or retain
a second writable surface/operation authority.

Structural checks and scalar physical reservation must both pass; neither is
an alternative permission source. Replace this seam lifecycle by lifecycle.
Move packages only after ownership and parity are established, and create one
only for a real dependency or change-amplification boundary.

### Implementation Map

| Boundary | Location | Keep outside the compiler |
| --- | --- | --- |
| Host-Surface | `internal/hostsurface/catalog` | Topology/Realization validity, codecs and consumer-local importability; owner-internal `aggregate.MCPPlacementForSubject` must not reverse-import the catalog. |
| Operation authority | `internal/operationplan` | Workflow path observation/lowering before leases, source freshness, rollback evidence and Adopt's `Plan.IdentityBytes`; do not invent fingerprints for operations without them. |
| State Barrier | `internal/recoverygate` | Generic `internal/effect/fileset`, journal, storage and subprocess protocols. Preserve file-set-only lock/init/authoring checks, read-only joint refusal without effect authority, and RecoveryDir-only cleanup. |
| Readiness | `internal/workflow/readiness` | Effectful observations remain separate from pure assessment/inventory/order; probe authority proves neither durable readiness nor a new fingerprint. |

## Forbidden Shapes

In addition to the owner boundaries above, do not introduce:

- giant `AgentProfile`/`SurfaceContract`, universal Resource or generic HostAdapter;
- `map[string]any`, reflection, callback registries or service locators as IR;
- persisted compiler IR or new unversioned identity;
- resource-by-target, resource-by-operation, target-by-operation or
  operation-by-phase package matrices;
- package, file, LOC or density reduction as acceptance criteria.

Semantic dependency and effect-boundary guards remain executable evidence.
Report-only compiler and State Barrier shadow findings never fail the
blocking architecture baseline. Removing a blocking rule requires equivalent
or stronger behavioral or graph-level coverage of its accepted invariant; deleting a
prose or symbol-presence check does not require replacing that check with
another textual proxy.

`packagePlacementRows` can leave missing, duplicate or invalid placements
unclassified; affinity/role rules skip those nodes. This is not complete
semantic coverage. `Report.Shadow` findings remain diagnostic, while analysis
or load errors still fail. A guard change must exercise forbidden and
legitimate-neighbor imports without making shadow findings block the baseline.

## Perturbation And Acceptance

| Change | Expected owner/locality |
| --- | --- |
| New OS | Physical adapters and platform admission, not semantic owners, surface semantics or recovery policy. |
| New target using an existing format | Static surface rows, required private adapters, tests and docs. |
| New realization form | Canonical variant and matching Reconciliation/Effect variants, not unrelated host workflows. |
| New observation purpose | Assurance fact and observation binding, not placement identity. |
| Recovery hardening | State Barrier and Effect/recovery, not artifact families or target-profile algorithms. |

Review one owner per invariant, mechanism duplication, change locality,
capability containment, exact compatibility and retained-seam dispositions.
See [Contributing](CONTRIBUTING.md#verify) for verification entrypoints.
