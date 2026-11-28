# Interrupted operations

## Purpose

Define which interrupted effects daem can settle and which evidence must remain. [Durable formats](../durable-management-formats/spec.md) define the records; the [recovery workflow](../../../internal/workflow/recover), [journal execution](../../../internal/effect/journal) and [file-set transactions](../../../internal/effect/fileset) interpret and settle them. The [recovery execution tests](../../../internal/workflow/recover/execute_test.go) exercise revalidation and the distinction between active recovery and cleanup-only authority.

## Requirements

### Requirement: Recover an active journal from recorded authority rather than desired state

Journal-covered apply SHALL write complete recovery evidence before reserving new global output claims or changing covered host files or state. The journal describes one interrupted operation, not an archive or a request to replay the current manifest. Select it under its original workspace and verify the recorded scope, leases, guarded host/state/ownership facts, provenance and backups. Normal desired source content need not be available. An eligible `--baseline-tree` proof verifies the recorded older skill digest; recovery restores bytes from the verified backups, not from that proof tree.

| Active classification | Admitted result |
| --- | --- |
| `clean_before` | Recognized pre-operation host, state and ownership; only eligible cleanup remains. |
| `clean_after` | Recognized committed host, state and ownership; only eligible cleanup remains. |
| `needs_rollback` | Restore guarded changes to the recorded before-state. |
| `needs_finalize` | Host and state committed; finalize the prepared claims without replaying host writes. |
| `blocked` | Current evidence cannot be reconciled with a legal state; retain evidence without writing. |

Recover rechecks the current candidate and its required observations before effects. An unrecognized selected value, changed namespace, corrupt or missing required backup, or unqualified mount/boot witness is not permission to overwrite. If an interrupted first Claude global hook install has not yet saved its settings location, qualify the original `CLAUDE_CONFIG_DIR` or original default; another profile cannot redirect recovery. Prepare and authorize the current recovery kind according to the [effect interface](../effect-commands/spec.md#requirement-select-only-the-admitted-recovery-authority).

Pending permission protection fences the selected recovery effects according to the [permission recovery selection rule](../permission-protection/spec.md#requirement-recover-access-without-recovering-file-contents-or-replaying-the-command).

#### Scenario: Host writes and state committed but a prepared claim remains

- **WHEN** the current evidence qualifies finalization of that operation
- **THEN** settle the prepared claim without restoring old host content or running the host write again

### Requirement: Qualify every removal intent before retirement

Before retiring an active journal, recovery SHALL reconcile every persisted removal intent, including work outside the selected recovery subset. A clean host/state classification does not establish cleanup. Before promoting full residue into cleanup, prove its complete recorded state. Once promoted, resume qualified partial deletion without demanding an untouched original tree.

Validate exact private locations, parent namespace and identity, durability and complete intent coverage. Missing, surplus, malformed or conflicting evidence, or a missing/replaced parent, blocks this work. Validation does not create a replacement parent. Do not infer deletion authority from a reserved name or prefix; exact private-name evidence does not protect against deliberate same-user forgery.

| Work scope | Current limit |
| --- | --- |
| Removal intents per journal | 4,096, with at most before and expected-after whole-path states per intent |
| Each planning pass and bound execution lifecycle | 90,112 namespace/slot observations; 67,108,864 root/path component visits; 400,000 recursive entry visits; 16 GiB regular-file content |
| One bound physical path | 256 components |
| One recovery tree | 100,000 entries, 64 descendant-directory levels, 4 GiB |

One planning budget includes guarded host paths, backups, cleanup and authority observations, including alias resolution. Separate maxima need not compose. Reserve reachable work before effects, charge unknown exact work at its admitted maximum, verify backup content during restoration, and refuse growth beyond the fresh cleanup ceiling, even if that ceiling is zero.

#### Scenario: A clean-before journal has cleanup in a lost parent namespace

- **WHEN** a persisted removal intent no longer has its qualified parent
- **THEN** retain the journal and backups rather than creating a new parent or treating the visible clean state as completed recovery

### Requirement: Separate retained journal cleanup from terminal garbage collection

Daem SHALL revalidate retirement identity, content and durability. Unavailable or changed evidence stops retirement and retains the relevant recovery artifacts. Revalidation is not atomic exclusion against an external writer racing afterward.

An already-retired but incomplete journal can leave `journal_cleanup_incomplete` with `retained_cleanup_residue` and `finalize_journal_cleanup`. This action touches only the exact correlated recovery root, retirement control, residue and final collection name; it acquires no host, state, ownership, manifest or lock authority. Preflight entry/depth and mount continuity before advancing the control or deleting the first residue child. FIFO, socket and device children block before deleting earlier siblings. Do not follow symlinks; revalidate and remove the link itself where admitted.

After semantic retirement, best-effort garbage collection can fail and leave private control metadata, not retired journals or backups. Report that failure without recreating a recovery action or fencing later commands. A name alone grants no restart-time deletion authority. Pre-1.0 `.daem-tombstone-<32 lowercase hex>` entries are recognized only to block; malformed entries in that reserved namespace also block. Current daem does not inspect, rename, migrate or delete them. Resolve a valid old tombstone with its writer when safe, and do not discard it without independent evidence that no interrupted apply or backup remains.

#### Scenario: Physical garbage collection fails after semantic retirement

- **WHEN** journal and backups are already retired and only control metadata remains
- **THEN** report failure without authorizing another recovery of that operation

### Requirement: Keep declaration transactions and journal authority separate

Daem SHALL distinguish a published file-set transaction, invalid evidence, unprovable StateDir access, bounded census overflow and markerless transaction residue. An active journal can be recovered beside a published declaration transaction, census overflow or markerless residue without clearing that continuing fence. Invalid file-set evidence or unprovable StateDir authority blocks active recovery even when RecoveryDir is accessible. Cleanup-only recovery uses its own recovery-root authority and does not require an unrelated StateDir census. Preserve known evidence on one axis even when the other is unknown; honor cancellation during replan.

Native capture has its own newly reviewed [recovery plan](../native-value-commands/spec.md). Legacy user relocation and shared metadata conversion each have their own [migration recovery](../metadata-migration/spec.md). An unmanage retry retains its operation-specific file-set authority and does not inspect or repair its file-set while conflicting journal authority remains. Local markerless `.daem-tmp-*`, `.metadata-stage-*`, `.daem-tombstone-*` and `.daem-cleanup-*` residue does not have a supported automatic cleanup route. A name is not evidence from which to infer retry or disposal.

Confirmed apply may clear an exactly verified retired journal or completed selected manifest and default lockfile transaction. If both must clear, verify both before clearing either, retire journal evidence first and recheck the approved plan after cleanup. Retain each completed cleanup if later work stops. Apply dry-run reports it without changing it. A lockfile-only write can clear the same verified selected completed transaction and a verified retired journal blocking that cleanup. Lock resolves sources normally after cleanup, so floating inputs can advance. These routes cannot finish an interrupted declaration write or restore its files. [Lock preview](../lockfile/spec.md#requirement-preview-the-lock-from-the-selected-files-without-settling-recovery) does not select cleanup.

#### Scenario: Both retired journal cleanup and completed declaration cleanup are selected

- **WHEN** the declaration record no longer qualifies at the joint preflight
- **THEN** apply does not clear either record on the strength of the retired journal alone

### Requirement: Settle a declaration pair without replaying the edit

`recover --declarations` SHALL select the ordinary local file-set record for the selected manifest and adjacent default lockfile. Resolve the two referents independently of the record. Admit only the exact pair, without State, registry or other targets. Verify the recorded before and after states and restorable backups; execution revalidates the selected entries, referents and record under the target and recovery leases. An absent record, a different pair, unrecognized contents, or evidence that cannot be verified gives no write authority. A pending journal or shared metadata fence must be handled separately.

Preview reports whether an authorized call would restore the recorded before-state or clear a completed record, without doing either. Execution settles that one record and stops. It does not interpret the interrupted edit, resolve declared sources, or generate a lock. An already completed pair is left in place without rewriting either file. Apply-journal recovery remains separate.

#### Scenario: Lock encounters a partially published authoring transaction

- **WHEN** a lockfile-only write cannot settle a partial declaration pair in its own scope
- **THEN** it leaves both files and the record alone and points to explicit declaration recovery; the authorized recovery restores the pair without rerunning either edit or lock generation

### Requirement: Recover native management release without replaying host changes

For an interrupted `unmanage agent-config` management release, `recover` SHALL settle the journal's exact file-set for the manifest, default lockfile and State before deciding whether to keep or finish releasing management. This exception covers the published transaction belonging to that release, not a separate declaration fence. Require the recorded State and original control-file locations, including selected symlink referents. Pending protection for the selected release scope or a control file the transaction may restore blocks the release recovery.

After the local transaction is settled, leave later manifest and default-lockfile edits alone, including a re-added declaration. Do not write host content or take a new owner's claims. If the operation removed only an unapplied declaration, retry the original `unmanage agent-config` to settle its file-set, which also records State. A committed edit can leave the ID absent after that retry clears the evidence.

#### Scenario: The declaration was added again after the release's file-set settled

- **WHEN** recovery qualifies the remaining release of only the old management
- **THEN** it leaves the new declaration, lock and host content unchanged and does not take a new owner's claim
