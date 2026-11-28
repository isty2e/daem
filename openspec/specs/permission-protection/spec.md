# Permission protection and recovery

## Purpose

Define how admitted delegated commands protect managed documents against expanded access, and how daem settles a protection left by an interrupted command. The [protected invocation](../../../internal/workflow/protection/invocation) and [permission recovery](../../../internal/workflow/protection/recovery) implement these operations separately from the apply journal. [Permission recovery tests](../../../internal/workflow/recover/permission_test.go) cover partial containment, retained protection and tighter modes on retry.

## Requirements

### Requirement: Bind permission protection to the admitted invocation and its documents

For a route requiring protection, daem SHALL observe the access ceiling immediately before launch, arm a record for the exact selected documents and bind it to the gated invocation before the protected child runs. Preserve route identity and the selected logical and physical document bindings. Missing authority to publish or bind the record is not permission to start an unprotected attempt.

Contain only access added during the attempt, even if the command fails. Leave a tighter permission mode in place. After the attempt, revalidate each applicable binding and supported metadata before restricting access or claiming clearance. Unverified containment or uncertain record publication cannot be presented as safe completion. Do not turn permission containment into automatic repair of changed document content or replay of a failed delegate.

#### Scenario: A host leaves a protected settings file more restrictive

- **WHEN** the file can be verified under its recorded binding and is already within the ceiling
- **THEN** daem can settle the protection without widening those permissions

### Requirement: Observe protection without assuming a global interruption

Apply, status and doctor SHALL discover permission-protection records for the selected state authority without requiring valid manifest, lockfile or state payloads. Select its original path explicitly if the manifest is gone. Results report what was observed during the command; they do not prove those records are still pending after presentation.

Discovery does not itself block unrelated work or a no-op Apply. Actual writes observe their selected document fences. Status keeps a protection observation separate from selected target rows, applied baselines and [journal or file-set evidence](../mutation-authority/spec.md#requirement-keep-the-state-directory-and-recovery-barriers-independent).

#### Scenario: A pending protection is outside an otherwise eligible write

- **WHEN** the selected effect does not conflict with the protected document authority
- **THEN** that unrelated write is not refused solely because diagnostics observed the record

### Requirement: Recover access without recovering file contents or replaying the command

Permission-only `recover` SHALL select the exact state authority's protection without needing manifest or lockfile content. Verified retired-journal cleanup takes precedence. If an active journal is eligible, recover it first only when its selected physical writes and ownership claim or binding changes are disjoint from pending protection. Recheck that boundary under the recovery leases before effects. If the effects conflict or their authority cannot be established, retain both records and report the blocker.

Joint native recovery includes the journal's retained document write intents in this comparison, even when an individual member already has its recorded value.

One invocation settles only the selected recovery. After journal recovery, a separate `recover` can select the remaining permission protection. A continuing or unclassified file-set fence or invalid journal evidence blocks permission-only settlement.

Check the current document bindings, namespace and supported metadata. A bound invocation needs fresh process quiescence; an armed record is not evidence of a running child. If a Claude profile supplied the recorded settings file, a different `CLAUDE_CONFIG_DIR` cannot satisfy that binding.

Preview changes no permissions. Confirmed recovery can restrict safe peers while another selected document remains blocked, but cannot report complete settlement until all selected protections are confirmed cleared. If access has tightened, do not widen it to the old limit. Verified absence does not authorize recreating a deleted file. Do not parse or rewrite protected content, signal or replay the prior command, or finish its installation. Containment cannot make another unsupported Apply or Refresh combination admissible.

The [recovery command interface](../effect-commands/spec.md#requirement-report-recovery-phases-without-claiming-unavailable-retry-authority) defines reporting, including an uncertain write or a later command failure after records have cleared.

#### Scenario: A journal can be recovered without touching a protected document

- **WHEN** the active journal and the protection share a state authority but the selected effects do not change the protected document or its claims or bindings
- **THEN** journal recovery leaves the protection pending for a separate invocation

#### Scenario: One protected document is inaccessible and another qualifies

- **WHEN** access can be safely restricted on the qualified peer but the other binding is unproved
- **THEN** daem may contain the qualified peer without claiming that every protection has been cleared
