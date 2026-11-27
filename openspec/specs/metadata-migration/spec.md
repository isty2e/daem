# Management metadata migration

## Purpose

Distinguish transferring the default user's old management authority from converting the format of existing records. The [migration workflow](../../../internal/workflow/migrate) and [metadata conversion](../../../internal/effect/metadataconversion) own selection and qualification. The [effect command interface](../effect-commands/spec.md#requirement-distinguish-state-transfer-from-metadata-format-conversion) owns disclosure and results. [Transfer recovery tests](../../../internal/workflow/migrate/recovery_test.go) cover interruption after each visible write and refusal after evidence changes.

## Requirements

### Requirement: Transfer only the default user manifest's legacy state authority

`migrate state` SHALL transfer the default user manifest's former local `.daem` authority to its selected XDG state location. It preserves installed baselines, pending carrier facts and pins, acquisition provenance, recorded manifest paths, diagnostics and unrelated claims. A qualifying global carrier claim can exist without a snapshot; that case is eligible. Missing state with output claims cannot be reconstructed. No legacy management to transfer is a refusal, not a new empty state. A verified completed transfer can report `already_migrated`.

Revalidate under leases before publication. Refuse an existing conflicting destination, source reservations, conflicting owners, malformed authority or unresolved recovery. The exact transaction covers destination state, output registry, carrier registry and then the old statefile, which is written last as a v1 relocation receipt. The receipt is neither a live snapshot nor a redirect. Do not transfer host files, permissions, symlinks, source/lock bytes or cache. Arbitrary state relocation, changed HOME/XDG, and authority merging are not this migration. Old local caches remain untouched; future commands can require acquisition into the selected XDG cache.

#### Scenario: A registry-only global claim was adopted in the old namespace

- **WHEN** the legacy user manifest has verified carrier management but no snapshot
- **THEN** daem can transfer its selected claim without deriving an applied baseline from the host

### Requirement: Settle only the recorded user-state transfer or selected legacy apply

An interrupted authority transfer SHALL retain its fence and exact target set for `migrate state --recover`. Revalidate the recorded before/after images under the selected leases. Restore the before-set or finalize a complete after-set; do not replace an unrecognized value or bypass missing, changed or unsupported evidence. Edits or another global metadata write can invalidate eligibility. This is metadata settlement, not recovery of an independent declaration write or host journal.

Before a transfer, `recover --legacy-user-state` can select an old apply journal only in the known former local root of the default user manifest. Check the canonical recovery boundary first and refuse a retired relocation receipt. Old journals do not move. Interrupted legacy authoring metadata remains with the matching previous writer and original authoring command; the old apply selector cannot settle it.

#### Scenario: The transfer stopped before writing its relocation receipt

- **WHEN** fresh recovery evidence classifies the exact partially written target set for rollback
- **THEN** `migrate state --recover` restores that recorded set without relocking sources or replaying any host command

### Requirement: Convert the selected management cohort without changing its meaning

`migrate metadata` SHALL qualify the selected state and management records reachable from its shared registries. Record qualified lookup locations alongside, not in place of, comparison identities. Preserve owners, baselines, bindings, reservations, pending carrier facts and unrelated management. The preview covers checked targets even when a file is already converted or absent. It does not write; execution revalidates the plan before publication. Unchanged targets can remain unchanged and absent targets remain absent. Conversion does not move authority, resolve an existing conflict, relock, or change installed host state.

Repeatable `--reference` assertions supply a lookup path for an unresolved legacy identity. Check the currently observed spelling and identity; the result does not prove that was the historical location or replace a recorded locator. An unresolved participating record can refuse conversion. `--baseline-tree` instead proves missing shipped membership from a matching copy of the earlier installed release, even if its declaration is gone. Check the recorded digest and derive its shipped paths; neither the proof tree nor installed files are changed. It cannot be combined with `--reference` or `--recover`, and does not substitute for `recover --baseline-tree` on an interrupted apply.

#### Scenario: A former skill has no declaration or recorded shipped path list

- **WHEN** the operator supplies a copy of its old tree that verifies the recorded baseline
- **THEN** metadata conversion can record that membership without reinstalling the skill or changing its installed tree

### Requirement: Recover published metadata conversion before collecting inactive artifacts

`migrate metadata --recover` SHALL settle the original recorded selection and exact participating cohort, even when invoked from another workspace sharing that data directory. A published active record fences ordinary work; invalid or incomplete active evidence is not inactive cleanup. Confirm the terminal target set before retiring the exact transaction directory. An interrupted cleanup cannot reactivate a retired transaction. Unavailable storage or limits can prevent qualification without proving any conversion was interrupted.

With no active record, the command can disclose a distinct `cleanup` action for qualified inactive preparations and retired directories. Check identity, ownership and expected shape. Do not read or change conversion targets for that cleanup, inspect/delete arbitrary entries by prefix, or claim a bounded pass covered all inactive work. Unknown or changed artifacts stay intact. Deferred collection is not a global transaction fence. A completed repeat can report `none` without writing; platform admission still applies.

#### Scenario: A different workspace sees a shared metadata conversion fence

- **WHEN** both workspaces share the data directory and the recorded evidence qualifies
- **THEN** the approved recovery operates on the recorded original selection, not the caller's selected statefiles by default

### Requirement: Keep opportunistic conversion separate from the requested effects

Apply, refresh and unmanage SHALL retain their existing effect and consent boundaries when format conversion is eligible. Apply may convert before a non-no-op write, but not during read-only planning, a no-op, or an invocation that first clears qualifying recovery records. Unmanage can convert when writing State or shared claims, not for a declaration/lock-only release. Refresh can convert before the route and rebuild its physical plan; verify the same authorized operation, logical state, declaration, lock and applicable host evidence before execution.

Failure to qualify optional conversion can leave the operation using its eligible existing format. An active conversion fence still requires recovery. A completed conversion can remain if the requested operation later refuses; conversion does not establish a host effect or authorize a changed plan.

#### Scenario: Format preparation succeeds but a refreshed observation has changed

- **WHEN** conversion committed but the authorized operation no longer qualifies
- **THEN** daem retains the conversion and stops rather than treating it as permission to run the changed operation
