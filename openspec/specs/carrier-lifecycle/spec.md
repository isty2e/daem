# Managed host carrier lifecycle

## Purpose

Define how a declared extension can be observed, installed, adopted, removed, handed off, or refreshed. [Carrier declarations](../carrier-declarations/spec.md) define what can be requested; [host routes](../carrier-routes/spec.md) define admitted effects and evidence for each host. The [managed carrier executor](../../../internal/workflow/apply) and [refresh workflow](../../../internal/workflow/refresh) implement these transitions.

## Requirements

### Requirement: Establish claims from qualified selected-scope evidence

For an observed carrier, confirmed apply SHALL invoke only the locked supported install route after fresh exact absence. Malformed, ambiguous, stale, wrong-scope or unavailable selected evidence is not absence. A command result is attempt evidence only. Claim creation requires its exact fresh postcondition under the selected claim store: project statefile or shared global registry. For an affected global install, commit the verified registry claim before retiring its matching pending state. A pending install alone never grants removal authority.

When passive host evidence lacks source provenance but a specific transition can establish it, require that route's exact pending identity as well as its fresh postcondition. For an admitted route with no observer, ordinary apply instead retries the locked install/create command on every run. `attempted_unverified` records the attempt without supplying a skip, ownership or removal claim. Neither path proves an exact installed artifact, enablement, trust, activation, or inventory of current contributions.

#### Scenario: An observed route exits zero but selected evidence is ambiguous

- **WHEN** the post-observation cannot establish the exact required relation
- **THEN** successful exit alone does not create a managed claim

### Requirement: Adopt only an exact external relation and release it explicitly

`apply --manage-existing` SHALL record an external carrier without a host command only when the declared locked relation is source-exact, current and qualified for both installation and removal, with an available scope-selected claim store and no conflicting claim. The admitted rows are Claude Code project/global, Codex global, OpenCode project/global and exact stored-source Pi project/global. Antigravity CLI observation has no recoverable marketplace provenance; installation-created removal claims do not authorize external adoption there.

`present_unclaimed` suggests an adoption preview for an eligible relation. Use `present_unclaimed_ineligible` with a blocker when lifecycle evidence is missing. Name-only, merely equivalent, shadowed, ambiguous, stale or source-inexact relations are not adoptable. An existing install recovery can complete a matching claim from its evidence; it is not ordinary adoption.

Adding or removing a declaration edits the manifest and lock, not the host. Omission requests absence at apply time. `unmanage extension` instead releases exact management and retains the host relation, including when the declaration was already removed. Refuse other owners' claims or pending operations that cannot be released; do not use unmanage for host recovery or blind pending settlement. There is no carrier `on_absent` override.

#### Scenario: An external Antigravity relation matches by plugin name

- **WHEN** `--manage-existing` cannot prove the marketplace source of that installed relation
- **THEN** it does not create a claim or invoke the host

### Requirement: Remove only exact managed relations without remaining known consumers

Managed removal SHALL require the selected route, exact managed authority, fresh selected evidence, and no remaining daem-known shared consumer. The route's fresh absence condition, not exit status, settles it. Where freshly absent evidence satisfies the route without invocation, retire only the claim. Partial or uncertain outcomes retain the pending record and claim until fresh evidence settles or qualifies retry. Undiscoverable ambient host consumers aren't counted as daem-owned consumers.

Coupled deletion for one managed removal does not permit broad prune, unrelated cache/dependency/state deletion, credential/trust changes or disablement of contributions. Scoped evidence and the host route define what gets retained.

#### Scenario: A previous uninstall failed but fresh required evidence now proves absence

- **WHEN** the pending removal still matches the selected claim and its route allows settlement from that absence
- **THEN** settle it without blindly invoking uninstall again

### Requirement: Retire the affected handoff set before acquiring its replacements

An admitted source handoff SHALL keep the owner, carrier, target and scope and apply from the final manifest. Verify removal and conditional acquisition eligibility before effects, including pending and shared-consumer blockers. An admitted Pi pin change uses its in-place install transition instead. Otherwise retire the whole affected old set, including a swap or transfer to another extension ID, and settle matching pending removals. Read fresh state and qualify the remaining work before any of the new acquisitions. Unrelated deletions stay in their ordinary phase.

Completed retirement survives a failed acquisition; a retry can use the same final manifest. A handoff is not an atomic swap, claim reassignment, or rollback. Retention follows each host route. Reinstalling does not preserve old package entry options or copy a desired local source out of an old checkout. Pi and OpenCode order still use fresh evidence and the applicable extra consent.

If retaining exact external host state is intended instead, use explicit release then eligible adoption. Antigravity replacement with a source that has no observer can finish without a new claim; a prior unobserved installation did not supply old removal authority.

#### Scenario: Two managed declarations swap sources

- **WHEN** both old relations and conditional acquisitions qualify for a handoff
- **THEN** apply settles both retirements and requalifies the remainder before either acquisition, without promising that a failed install restores the old relation

### Requirement: Keep a managed Pi Git pin transition exact and resumable

An admitted pin change SHALL keep the extension ID, locator spelling, target and scope, with full lowercase hexadecimal old and new commits. Require an exact selected-scope settings relation and refuse ambiguous rows or another declared or known managed consumer sharing the checkout, even via a different pin or transport alias.

Before invoking the scoped install, record the exact old claim, provenance and requested new pin. Update the claim only on successful execution plus fresh observation of the exact new relation, with `pin_transition_observed` provenance. Native Pi may already reset/clean the checkout or run dependency scripts before publishing settings. On failure or uncertainty preserve the old claim and pending request until verified settlement. A history persistence failure after a proven commit does not restore the old claim.

Retry requires the same desired target and unambiguous exact old or new settings. Even if settings already show the new pin, retry invokes Pi; observation alone cannot settle the transition. Missing, third-pin or ambiguous relations need operator repair first. Pending pin state blocks removal, unmanage, adoption and refresh of the affected relation; Pi refresh also checks known pending pins in the other scope because its route is not scope-local. Generic recovery does not replay or cancel the host pin change.

#### Scenario: The child left the new pin in settings before interruption

- **WHEN** daem still has the old claim and exact pending transition
- **THEN** it keeps that claim until a newly authorized retry succeeds and verifies the exact target

### Requirement: Treat refresh as a distinct one-attempt route

Refresh SHALL run only the one explicitly selected locked route under the [refresh interface](../effect-commands/spec.md#requirement-refresh-one-selected-locked-extension-without-broadening-authority). Require fresh exact presence when the selected route models it and reobserve afterward. Otherwise report an unverified attempt, not package convergence. Do not retry, substitute install, or automatically roll back after start.

Refresh accepts already-applied native keys and dedicated files under the applicable settings-rewrite and physical-separation rules. It cannot apply pending settings or ownership changes. For protected settings, limit access to what was observed just before launch, even on a failed attempt, and retain any tighter mode. Unexpected content changes stop the operation without automatic repair. Do not claim permission containment if it could not be verified; settle an interrupted protection record separately from the host route.

#### Scenario: A refresh rewrites Codex settings with tighter permissions

- **WHEN** post-observation proves required settings content still matches
- **THEN** daem leaves the tighter mode in place instead of restoring the earlier access
