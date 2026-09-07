# Compiler Migration Ledger

Status: implementation evidence for the **bounded PR #91 delivery**. This
ledger derives from [`ARCHITECTURE.md`](../../ARCHITECTURE.md), especially its
[delivery boundary and deferred-work policy](../../ARCHITECTURE.md#bounded-delivery-and-deferred-work).
It is not a runtime registry, product support matrix, or independent authority.

The earlier all-or-nothing migration roadmap is superseded. Universal cursor
coverage, scalar-free execution and semantic package-classifier replacement
are not prerequisites for this PR or 0.2.0. They remain unfinished follow-ups;
their disposition does not waive a reproduced product or safety defect.

Baseline: `main` at `c0fd11f937c27936e175fb955168fdef3e539e37`.
Implemented runtime checkpoint: `8c67407a4df9294e3dd4e414ddeb003799f4fb7b`.
The bounded closeout changes architecture documentation and shadow reporting,
not production runtime behavior or persisted formats. Earlier inventory and
phase snapshots remain available in Git history rather than competing with
current owner catalogs.

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
gates. This ledger does not claim that every intermediate operation is
interpreted by a single cursor or that all reservation representations agree
by construction.

## Property Traceability

These identifiers retain continuity with earlier reviews. Their authority is
the linked architecture contract and narrower product/compatibility contracts,
not the existence of a test or mechanism. The delivery reset re-derives
ARCH-G12–G14 and the completion-related constraints in ARCH-G17–G18; it does not
silently alter public behavior.

| IDs | Retained contract and owner | Evidence surface |
| --- | --- | --- |
| ARCH-G01, ARCH-G04, ARCH-G21 | Semantic cores and three realization forms retain ownership; Reconciliation decides, Effect executes/transitions, State Barrier authorizes. | Owner APIs, import/effect-boundary fixtures, journal and recovery tests. |
| ARCH-G02, ARCH-G03, ARCH-G06, ARCH-G07, ARCH-G09 | Host-Surface owns logical-key and cross-facet coherence; facts stay local; purpose/dispatch cardinality and physical sharing stay distinct; compilation is immutable and I/O-free. | `internal/hostsurface`, `internal/hostsurface/catalog`, topology and profile owner-parity/negative-seed fixtures. |
| ARCH-G05 | Product support does not manufacture runtime capability, nor does missing capability imply unsupported product. | Profile, platform-support and readiness contracts. |
| ARCH-G08, ARCH-G22 | Stable IDs and independently versioned public/durable artifacts remain exact. | Versioned codecs, owner identity tests and strict CLI consumers. |
| ARCH-G10, ARCH-G11 | Pure operation authority/domain/revision/fingerprint compilation; effectful path lowering remains outside it. | `internal/operationplan` plus workflow parity and freshness fixtures. |
| ARCH-G12 | Supported work must be bounded and admitted before effects; existing typed/cursor segments remain enforced, without requiring a universal cursor. | Apply/Refresh reservation and execution tests; owner-local continuation and Recovery tests. |
| ARCH-G13 | No runtime branch adds unreserved work. Exact frontier precision is lifecycle-specific; current admission behavior is unchanged. | Effect frontier/terminal tests; Apply dominance checks and Refresh structural reservation tests. |
| ARCH-G14, ARCH-G15 | Physical lowering, retained identity, capacity and revalidation stay State Barrier-owned; semantic obligations grant no filesystem authority. | `internal/recoverygate` and named State Barrier CI. |
| ARCH-G16 | File-set mechanics remain below barrier policy; storage/journal/subprocess retain their mechanisms. | `internal/effect/fileset`, declaration transaction and storage tests. |
| ARCH-G17 | Workflows orchestrate without duplicating canonical static/authority/identity grammars; retained semantic count projections do not own physical lowering. | Compiled consumers and current workflow/State Barrier handoffs. |
| ARCH-G18 | A replacement moves consumers only after required parity. Live compatibility seams stay until their replacement is verified; universal migration is not a release gate. | Differential fixtures, caller-specific cutovers and explicit retained seams above. |
| ARCH-G19 | OS specialization stays at physical/platform boundaries, not compiler semantics. | Platform adapters and compiler-shadow perturbation fixtures. |
| ARCH-G20 | Policy is external to archguard; blocking violations and report-only shadow findings remain distinct. | `Report.HasFailures`, `TestHasFailuresIgnoresShadow`, blocking baseline and shadow fixtures. |

Earlier NON-GOAL identifiers remain traceable: ARCH-N01–N03 prohibit persisted
Surface IDs, mega-contract/registry IR and a fourth actuation realization;
ARCH-N06–N08 exclude fingerprint unification, unrelated broad model redesign
and numeric reduction targets. These follow the architecture's identity,
compatibility and forbidden-shape rules. ARCH-N04 private naming and ARCH-N05
`recoverygate` renaming follow its deferred-work policy. ARCH-N09 records named
State Barrier verification, not a runtime support expansion.

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

## Guard Coverage And Limitations

| Mechanism | Disposition |
| --- | --- |
| Blocking dependency direction, workflow/effect/presentation boundaries, forbidden shapes and production/test-support checks | Retain existing executable enforcement and forbidden/near-neighbor fixtures. |
| Compiler/State Barrier `Report.Shadow` | Diagnostic only. `TestCompilerShadowBaseline` prints findings; it must not fail because findings exist. Analysis/load errors still fail. |
| `packagePlacementRows` | Retained classifier for some blocking affinity/role rules. Missing, duplicate or invalid placement can leave a package unclassified; those rules skip unplaced nodes. This is a coverage limitation, not a complete semantic proof. |
| Exact unclassified-package admission, prose, symbol-presence and density gates | Do not restore as substitutes for behavioral/import evidence. |

Semantic classifier replacement is deferred under the canonical policy, not
accomplished by demoting shadow output. Removing a real blocking rule still
requires equivalent or stronger coverage of its accepted invariant. The
shadow baseline correction removes an accidental gate, not a blocking import
rule or its test.

## Follow-Up Triage

All follow-ups use the maintainer and reopen conditions in the
[canonical disposition](../../ARCHITECTURE.md#bounded-delivery-and-deferred-work).
Private issue plans remain open rather than being marked implemented.

- Delegate persistence: `delegateActionsRequireAttemptPersistence` includes
  blocked actions, whereas planning's project-root retention currently tests
  scheduled project actions. Public `CommandInput` has no passive runner
  readiness input, and missing readiness defaults to `RunnerUnknown`. A
  supported public PlanWrite/Execute reproduction is still needed; this is
  neither a verified fix nor proof that the mismatch is harmless. Resolve it
  before enabling a new blocked-delegate planning path.
- Provider replanning: continuation rebinding and both final-schedule checks
  are present; core `ApplyEffectPlan` rejects prepared/current structural
  disagreement. A proposed core-plan rebinding change needs a supported
  reproduction distinguishing required rejection from avoidable refusal.
- Structural-size policy, whole-continuation coverage, scalar removal and
  classifier replacement require independently justified scopes. Their old
  task dependency chains do not create release requirements.
- Linux pre-reboot journal refusal remains the documented product contract.
  It is not weakened as an implementation simplification.

The prior delegated architecture/value review did not complete its aggregate
gate. Its parent-only assessment was advisory; it is not final PR correctness
certification. Newly reproduced contract violations must be addressed before
merge, regardless of the deferred architecture work.

## Verification And Handoff

Use [CONTRIBUTING.md](../../CONTRIBUTING.md#verify) for scoped checks.
Relevant executable evidence includes catalog owner-parity/negative-seed tests,
operation fingerprint and revision tests, Apply/Refresh/Recovery reservation
and settlement regressions, file-set/StateDir replacement and retry tests,
strict CLI consumers and archguard forbidden/near-neighbor fixtures.

For this documentation and test-reporting closeout:

```sh
tools/test-go.sh -run 'Test(HasFailuresIgnoresShadow|CompilerShadow|TopologyGuardBaseline)' -count=1 -v ./internal/archguard
tools/test.sh repository
git diff --check
```

Exercise the shadow baseline with a temporary forbidden compiler import to
verify that it reports a finding without a nonzero exit; remove the fixture
before committing. Existing blocking-violation tests must still pass. Review
document links directly rather than asserting prose in tests. Commit hooks
remain installed and enabled.

The published runtime checkpoint's 24 GitHub checks were observed passing,
including full native, minimum-Go, race, State Barrier, vulnerability and
compile-only cells. That observation is revision-specific, not evidence for
a later head or an instruction to rerun every platform locally for a Markdown
edit. Final PR-head checks and independent PR review remain separate merge
requirements. No release or exhaustive architecture-completion claim is made.
