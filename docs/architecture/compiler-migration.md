# Compiler Implementation

This contributor reference maps the implemented compiler and State Barrier
boundaries to their consumers and retained compatibility seams. Policy belongs
to [Architecture](../../ARCHITECTURE.md), especially its
[deferred-work decisions](../../ARCHITECTURE.md#bounded-delivery-and-deferred-work).
This page is not a runtime registry or product support matrix.

## Delivered Boundaries

| Boundary | Current implementation and consumers | Responsibilities retained elsewhere |
| --- | --- | --- |
| Host-Surface | `internal/hostsurface/catalog.Product` compiles immutable MCP, Instruction, Skill, Hook, HookAsset and Extension views. Consumers include list/selection, import, diagnosis, authoring support, payload placement, host-route selection and selected readiness/apply/presentation order paths. | Topology identity, realization/codec validity, route behavior, observation evidence and owner-local importability remain in their existing owners. |
| Operation authority | `internal/operationplan` compiles authority/domain/revision facts and exact Apply, provider-stable, remaining-execution, Refresh and Recovery fingerprint projections. Adopt/import, init, lock, authoring and unmanage use compiled domain/revision programs. | Workflows observe, normalize and lower paths before leases. Adopt retains `Plan.IdentityBytes`; operations without an existing fingerprint do not manufacture one. |
| State Barrier | `internal/recoverygate` owns StateDir/RecoveryDir identity, journal/file-set axes, first-incarnation evidence, physical reservation and authority validation. | `internal/effect/fileset` owns generic file-set mechanics; declaration transactions are adapters. Journal, storage, rooted filesystem authority and subprocess retain their protocols. |
| Readiness | Observation sequencing is separate from the pure assessment/output-inventory/order middle-end. Compiled views supply selected static facts. | Qualified current evidence, support/capability distinctions, observation bounds and ordering stay owner-local. |
| Architecture evidence | Existing blocking import/effect rules and diagnostic `Report.Shadow` are separate channels. | Policy stays in `ARCHITECTURE.md`; shadow output does not authorize a blocking cutover. |

Surface identity is `(target, scope, desired family, variant)`; internal
`SurfaceID` is not persisted. Shared Instruction/Skill destinations and
HookAsset storage retain logical target-relative support without duplicating
physical objects. Observation purpose is separate from placement; delegates
and host routes are actuation, not a fourth realization form. Owner catalogs
and their executable parity tests, not copied Markdown cell literals, are the
current inventory.

## Effect And Continuation Status

| Lifecycle | Implemented enforcement | Retained limitation |
| --- | --- | --- |
| Apply core | Prepared/current Effect-owned plans are compared before core execution. Forward journal, ownership, observation, mutation and state publication consume the structural cursor. A separate failure-settlement structure constrains rollback/compensation handoffs. | Existing Effect, journal, visibility and recovery owners still define outcomes; the cursor is not physical authority. |
| Apply carrier settlement | Global settlement and carrier removal have bounded continuation segments. Exact-baseline CAS, registry-first split writes, retry and successor preservation remain owner-local. | This does not prove that every post-Apply continuation uses one cursor. |
| Final host-route prefix | Shared immutable route facts/nodes bind prepared/current pre-host plans before authority setup. The prefix covers rejection-attempt persistence, declaration/project-root checks and interrupted global promotions, and finishes before prepared commands. | Prepared-host execution/post-observation/settlement are not prefix-cursor-owned. Provider prerequisites retain their separate entrypoint. |
| Relation order and delegates | Existing workflow/Effect ordering, retained authority, validation and attempt persistence remain active. | Whole-continuation cursor integration is deferred; no new delegate-readiness contract is claimed. |
| Apply reservation | Structural demand is checked against the conservative scalar plan before State Barrier reservation. Existing statefile and forward counters remain enforced. | This is deliberately retained dual representation, not scalar-free or exact-frontier completion. |
| Refresh | Structural physical-frontier reservation and cursor-coupled StateDir/descendant execution are implemented. | Do not roll this back merely because universal adoption is deferred. |
| Cleanup-only Recovery | A cursor constrains retirement and terminal handling while RecoveryDir-only physical budgets remain authoritative. | Cleanup-only recovery stays independent of StateDir census. |
| Active Recovery | Removal cleanup, retirement tail and caller-specific outer settlement have bounded structures and cursor enforcement. | Journal/Effect retain transition meaning; existing semantic and physical budgets remain authoritative. |

The continuation's existing order remains core Apply, global retirement,
carrier removal, final routes, relation order, delegates and terminal-last
global adoption. Provider final-schedule comparison remains at both replan
gates. Not every intermediate operation uses a single cursor, and reservation
representations do not all agree by construction.

## Retained Compatibility Boundaries

| Boundary | Required parity / retained authority |
| --- | --- |
| Manifest, lockfile, statefile, journal and registries | Exact schemas, retained bytes/order, stable IDs, historical meaning and version policy. No compiler IR is persisted. |
| Operation/authority fingerprints | Exact values for identical inputs; Apply/provider/remaining, Refresh and active/cleanup Recovery projections remain distinct. |
| Mutation domains and revision roles | Exact membership, conflict meaning, lifecycle subsets and deterministic ordering. |
| Effects and results | Existing ordering, visibility, cancellation/stale precedence, split-write partial results and recovery semantics. |
| CLI JSON and supported human contracts | Existing strict producer/consumer behavior; no compiler debug fields. |
| Support and platforms | Existing target/scope and capability admission only; cross-compilation is not native execution evidence. |

| Retained path | Why it is not a compiler or authority bypass |
| --- | --- |
| `aggregate.MCPPlacementForSubject` in lock and owner-internal profile routes | Realization/Topology must not reverse-import the catalog; local validity remains theirs. |
| Consumer-local importability, authored paths, codec and durable validation | These interpret their owner's contract, not a fallback static surface join. |
| Workflow logical/physical domain constructors | Lower compiler-owned path requests at the I/O boundary before leases. |
| Adopt source rereads and post-creation rollback witnesses | Source freshness and rollback identity are not generic revision-role compilation. |
| File-set-only `RequireFileSetClear` | Lock planning, init and authoring preserve their file-set-only semantics; an active journal does not acquire a new lock prohibition. |
| Read-only `RequireClear` | Existing joint refusal without mutation authority; readers do not gain effect capabilities. |
| Cleanup-only Recovery | Uses retained RecoveryDir authority without StateDir census. |
| Probe barrier use | Observation-only subprocess authority, not durable readiness or a new operation fingerprint. |

## Provider Completion Ownership

Pending installation, existing relation/settings, and provider artifact
availability are separate facts. When readiness schedules provider replay, a
shared planning-only snapshot removes that exact completion from both core and
final-promotion demand. Actual provider execution owns claim observation and
durable settlement; no-replay pending facts remain with their existing owners.
The snapshot must not become persistent predicted state. See
[Transition Ownership](../../ARCHITECTURE.md#transition-ownership).

Planning and execution pass the same full relation facts to the state-transition
owner, including NoOp facts needed to settle a pending project claim. An empty
scheduled descendant reservation does not trigger reconstruction from pre-core
relations. The core clears exact pending global installs backed by committed
registry claims; the shared final-action projection retains only promotions
still needing registry/statefile work.

Regressions cover first installs and reinstalls at both scopes, cancellation
during replay, mixed replay/no-replay completions in both directions, and retry
after settlement. They check provider invocation count, exact claims, remaining
config projection, and subsequent no-op execution. Full pre-effect reservation,
registry-first CAS, both final-schedule comparisons, and prepared/current core
checks remain required. These cases do not establish a general mid-execution
provider-version-change guarantee.

## Guard Coverage And Limitations

| Mechanism | Disposition |
| --- | --- |
| Blocking dependency direction, workflow/effect/presentation boundaries, forbidden shapes and production/test-support checks | Retain existing executable enforcement and forbidden/near-neighbor fixtures. |
| Compiler/State Barrier `Report.Shadow` | Diagnostic only. `TestCompilerShadowBaseline` prints findings; it must not fail because findings exist. Analysis/load errors still fail. |
| `packagePlacementRows` | Retained classifier for some blocking affinity/role rules. Missing, duplicate or invalid placement can leave a package unclassified; those rules skip unplaced nodes. This is a coverage limitation, not a complete semantic proof. |
| Exact unclassified-package admission, prose, symbol-presence and density gates | Do not restore as substitutes for behavioral/import evidence. |

Removing a blocking rule requires equivalent or stronger coverage of its
accepted invariant. Report-only shadow output does not prove complete semantic
classification. Do not turn prose, package-name, or symbol-presence checks into
substitutes for behavioral or import-graph evidence.

## Deferred Work

The maintainer owns the reopen conditions in the
[canonical architecture disposition](../../ARCHITECTURE.md#bounded-delivery-and-deferred-work).
Universal cursor coverage, scalar-free execution, classifier replacement,
structural-size policy, and remaining renames are not completed merely because
the implemented boundaries work. Nor does their deferral waive a reproduced
violation of an accepted contract.

Narrower decisions stay at their owning surfaces:

- [Import](../cli.md#import) owns the explicit NON-GOAL for freshness of excluded
  discovery roots and ineligible children. Imported-source, inventory, alias,
  and merge evidence keep their existing freshness requirements.
- [Platform Support](../platforms.md) owns the deferred blanket Darwin
  nonzero-admission proposal. Its rationale and reopen conditions are not
  replaced by artifact-view admission rules.
- New blocked-delegate readiness or persistence modes remain deferred. The
  maintainer must resolve their authority requirements before enabling them;
  reopen on a supported public execution failure. Existing aggregate-blocker
  rejection does not prove every blocked input is unreachable.
- Broader provider core rebinding remains deferred pending a supported
  reproduction that needs it; current prepared/current and final-schedule
  checks remain strict.
- Linux pre-reboot journal refusal remains the documented product contract,
  not a simplification to remove during compiler work.

## Verification

Use [Contributing](../../CONTRIBUTING.md#verify) to select checks. Relevant
surfaces include catalog owner-parity and negative-seed tests, operation
fingerprint and revision tests, Apply/Refresh/Recovery reservation and
settlement regressions, file-set and StateDir retry tests, strict CLI
consumers, and archguard forbidden/near-neighbor fixtures.

A shadow-guard change needs evidence that a forbidden compiler dependency is
reported without turning the diagnostic channel into a failing gate; existing
blocking-violation tests must still fail for their forbidden cases. Review
public links and document changes directly, not through prose assertions.
Native platform claims still require their named native lanes; documentation
edits do not inherit an earlier revision's CI result.
