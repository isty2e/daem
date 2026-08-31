# Daem Architecture Contract

Status: authoritative internal architecture contract for semantic ownership,
compiler boundaries, transition ownership, dependency direction, and
behavior-preserving architecture migration.

Current executable behavior remains owned by canonical Go models and
invariant-bearing tests. Current user-visible syntax, operations, support, and
safety guarantees remain owned by `docs/`, versioned codecs, and strict public
consumers. This contract constrains implementation structure; it does not
replace those narrower authorities.

Tests verify behavior and typed or serialized contracts through executable
inputs, calls, imports, compile-time interfaces, and produced artifacts. They
do not infer a contract from documentation prose, declaration names, symbol
presence or absence, exact package/file catalogues, or numeric density.

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

## The Missing Compilation Boundaries

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

## Goals And Non-Goals

Goals:

- establish one primary owner for every surface-selection, operation-safety,
  and barrier invariant;
- replace repeated joins and workflow-local compilers with deterministic
  compiled views and plans;
- derive complete operation demand before effects;
- reduce change amplification while retaining existing semantic cores and
  effect protocols; and
- preserve every classified public, persisted, recovery, security, and
  platform contract during cutover.

Non-goals:

- no product feature, support, platform-admission, CLI, or wire redesign;
- no persisted Surface id or compiler IR;
- no giant profile, surface, resource, adapter, or operation framework;
- no immediate SubjectID, Paths, adopt-family, payload, findings, or DTO
  overhaul;
- no package move before ownership and parity are proven; and
- no package, file, LOC, or density reduction target.

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

Static variation remains split into owner-local typed facets:

```text
surface identity
representation form
Topology namespace contract
physical/logical placement
codec contract
observation purpose
operation and dispatch binding
discovery and runtime location
selection/default policy
product support
runtime/platform capability
```

This is not one optional-field `SurfaceContract`. Topology continues to own
structural identity; Realization owns its three forms and placement contracts;
codecs own syntax and preservation; observation owners own evidence; route and
capability owners own their local facts. The Host-Surface compiler owns only
cross-facet referential integrity, required/forbidden cardinality, and immutable
derived views.

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
typed and ordered effect envelope
semantic reservation demand
```

It performs no filesystem access, path discovery, subprocess execution,
persistence, capability acquisition, host-private parsing, workflow
confirmation, or presentation.

### Authority and compatibility

Apply, refresh, recover, and other operations may retain different exact
fingerprint projections while sharing one fact algebra and compiler. Existing
fingerprint values, revision subsets, ordering, and stale/currentness
precedence remain exact compatibility contracts during this migration.
Unifying those projections requires a separate versioned compatibility
decision.

A field already included in a current fingerprint remains identity-bearing for
that operation, including diagnostic detail where the current implementation
uses it, until a separately authorized change removes it.

### Effect envelopes

An effect envelope is typed and ordered. It preserves:

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

Sequential work composes. A proven exclusive branch reserves the maximum
reachable demand rather than the sum. No callback, replan, or workflow may add
unreserved work after the first external or visibility effect.

Dry-run and no-op operations have explicit no-effect envelopes and do not
create StateDir merely because they were planned.

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

Workflows may:

- select command context and paths;
- collect or request boundary evidence;
- invoke canonical pure compilers;
- acquire leases and retained capabilities;
- sequence already-authorized effects;
- coordinate confirmation and cancellation; and
- assemble operation results for presentation.

Workflows must not define a second surface matrix, authority fact grammar,
fingerprint format, reservation algebra, host syntax model, or StateDir
protocol. Workflows do not import other workflows.

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

The current implementation remains authoritative while a replacement runs in
shadow. A caller moves only after the required exact or behavioral parity is
proved from the same inputs. The superseded row, switch, workflow-local
compiler, raw reservation arithmetic, fallback, or facade is removed after its
last consumer moves.

There is no long-lived dual writable source of truth. A parity failure is
evidence to classify, not permission to normalize the old result away.

Package movement is last. Logical ownership and dependency direction are fixed
before a package name; a package is introduced only when it passes the package
versus file gate and prevents a real reverse edge or change-amplification seam.

The current implementation has completed bounded cutovers for the MCP cell
join, apply/refresh/recover authority subprojections and reservation demand,
exact apply, refresh, and recovery fingerprints, and the State Barrier protocol.
Instruction, Skill, Hook, HookAsset, and Extension surfaces also compile as
I/O-free views with owner parity. List inventory and manifest selection,
Instruction/Skill import and diagnosis, Hook diagnostics, Extension import
ordering, host-route
command selection, and selected readiness/apply/presentation order consumers
use those views. Other production consumers still use owner-local projections.
The program remains open until
those consumers cut over, every mutating workflow uses the
canonical operation compiler, typed effect obligations drive reservation and
settlement, and semantic guard coverage replaces exact-path classification
where it still determines blocking enforcement.

Current source, surface, operation, compatibility, transition, verification,
and closeout locality evidence are recorded in
[Compiler Migration Ledger](docs/architecture/compiler-migration.md).

## Forbidden Shapes

- giant `AgentProfile`, mega `SurfaceContract`, universal Resource, or generic
  HostAdapter;
- `map[string]any`, reflection, callback registries, or service locators as IR;
- a fourth realization variant for delegates, observation, or exact Supply;
- central ownership of all topology, codec, observation, and route facts merely
  because they share a Surface key;
- I/O or capability acquisition inside either compiler;
- operation demand added after effects begin;
- State Barrier reinterpretation of Reconciliation or Effect semantics;
- persisted compiler IR or new unversioned identity;
- resource-by-target, resource-by-operation, target-by-operation, or
  operation-by-phase package matrices;
- package, file, LOC, or density reduction as an acceptance criterion; and
- documentation-prose, declaration-name, symbol-presence, exact-path-catalogue,
  or density assertions presented as architecture tests.

Semantic dependency and effect-boundary guards remain executable evidence.
Report-only compiler and State Barrier shadow findings never fail the
blocking architecture baseline. Removing one requires equivalent or stronger
behavioral or graph-level coverage of the accepted invariant; deleting a
prose or symbol-presence check does not require replacing that check with
another textual proxy.

## Perturbation And Acceptance

The architecture must survive these changes with the expected locality:

- a new OS changes physical adapters and platform admission, not Desired,
  Topology, Realization, surface semantics, or recovery policy;
- a new target using an existing format primarily adds static surface rows,
  required private adapters, tests, and documentation;
- a new realization form changes its canonical variant plus corresponding
  Reconciliation/Effect variants, not unrelated host workflows;
- a new observation purpose adds an Assurance fact and surface observation
  binding without changing placement identity;
- recovery hardening changes State Barrier and Effect/recovery mechanisms, not
  artifact family or target-profile algorithms.

Completion is based on one primary owner per invariant, reduced mechanism
duplication and change amplification, contained capabilities, exact compatible
behavior, and removal of migration residue. Numeric graph changes are evidence,
not success criteria.
