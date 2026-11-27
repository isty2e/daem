# State and recovery

When a command fails, use [Troubleshooting](troubleshooting.md#find-your-symptom)
to identify the operation and choose a recovery command. Keep the statefile,
shared registries, and any journal and backups until the refusal is understood.

## Statefile

The statefile records what daem last installed or registered. The lockfile
records the resolution of the manifest at lock time. Keep the statefile when
relocking or removing a declaration; daem needs the earlier baseline to tell
whether it can safely replace or remove an output. See
[durable formats](../openspec/specs/durable-management-formats/spec.md) for
version compatibility, creation modes, and reader requirements.

### Copied skill baselines

An extra file in an installed skill is not drift by itself. Replacing or
removing the skill can still remove the entire directory, including extra
files. Make a separate copy of anything you need to keep. For an old hash-only
installation, see [managed output drift](troubleshooting.md#a-managed-output-drifted)
and the [rules for verifying a baseline](../openspec/specs/managed-output/spec.md#requirement-check-shipped-skill-membership-without-claiming-the-extra-files).
Use `migrate metadata --baseline-tree` for a verified copy of the old release;
use `recover --baseline-tree` when that proof is needed for interrupted Apply.
The proof tree does not replace journal backups.

### Missing State File

If the registry still records claims for a missing statefile, restore a
matching backup. If that is impossible, use the explicit
[release procedure](troubleshooting.md#state-file-is-missing). Releasing a
claim leaves the host output in place and forgets its baseline. The manifest,
lockfile, and live output cannot reconstruct the prior installed state.

### Key-Only Configuration Containers

After removing a managed key, daem can leave its empty parent table or object.
That parent can still matter: an empty TOML profile table can still define a
profile. Check the host's interpretation before deleting it manually. If you
want to replace an implicit TOML parent with an explicit header, remove its
managed dotted child first. See [native document lifecycle](../openspec/specs/native-document-lifecycle/spec.md)
and [editing selected values](../openspec/specs/agent-configuration/spec.md#requirement-limit-structured-editing-to-the-selected-values).

### Pending Pi Pin Changes

If a Pi package pin change leaves a pending claim, keep the recorded target.
Follow [A Pi Pin Change Is Pending](troubleshooting.md#a-pi-pin-change-is-pending)
rather than switching pins or trying to undo the package manager's writes by
editing state. [Carrier lifecycle](../openspec/specs/carrier-lifecycle/spec.md)
defines how pending and completed claims are distinguished.

### User-State Authority Migration

If the default user manifest was managed from its old manifest-local `.daem`
directory, follow [User-State Selection Changed](troubleshooting.md#user-state-selection-changed).
The [`migrate state` procedure](cli.md#migrate-state) moves the authority, not
the installed files or the cache. Finish any older apply journal or authoring
transaction first, using its documented route. Keep the receipt left at the
old location; do not copy the old snapshot back over it. See the
[migration spec](../openspec/specs/metadata-migration/spec.md) for scope and recovery.

### Shared Global Ownership

Use `daem status` or `daem apply --dry-run` to find the manifest reported for an
ownership conflict. For ordinary removal, work through that manifest; another
manifest cannot take the output just because its intended bytes match. See
[ownership conflicts](troubleshooting.md#ownership_conflict) and the
[shared ownership spec](../openspec/specs/shared-ownership/spec.md).

### Mutation Revision Evidence

If a plan is stale, review a fresh preview rather than retrying the old
approval. See the [freshness rules and observation limits](../openspec/specs/mutation-authority/spec.md#requirement-record-freshness-evidence-at-the-granularity-of-the-operation).

## Permission-Only Recovery

Use `daem recover --dry-run` to inspect pending access restrictions. If the
manifest was removed, select its original location with `--manifest`. For a
protected custom Claude Code profile, use the `CLAUDE_CONFIG_DIR` that selects
the recorded file. This recovery can narrow file access without restoring any
content or running the host command again. For example, it can clear a `0600`
protection after the owner has changed the file to `0400` without granting
write access back. See [permission protection](../openspec/specs/permission-protection/spec.md)
for blockers and partial results.

## Recovery Journal

If Apply was interrupted, [preview recovery](troubleshooting.md#apply-was-interrupted)
using the same workspace selection. If a first Claude Code global hook install
was interrupted before it saved the settings path, use the original
`CLAUDE_CONFIG_DIR` (or the original default). Keep the journal and its backups
if a version or platform mismatch prevents recovery. On Linux, finish pending
recovery before rebooting or remounting. See [interrupted operations](../openspec/specs/interrupted-operations/spec.md)
and [durable formats](../openspec/specs/durable-management-formats/spec.md#requirement-use-the-journal-format-that-supports-its-recovery-obligations).

### Journal Retirement

Keep recovery evidence until retirement succeeds, even when installed output
already matches. Use the [cleanup instructions](#retained-cleanup) if reported.

### Removal Intents

If recovery reports missing or conflicting removal evidence, keep the affected
parent, staging entries, and backups in place for diagnosis. Do not create a
replacement parent or remove files by a shared name prefix. See
[removal evidence](../openspec/specs/interrupted-operations/spec.md#requirement-qualify-every-removal-intent-before-retirement).

### Recovery Work Budgets

See the [recovery budget table](../openspec/specs/interrupted-operations/spec.md#requirement-qualify-every-removal-intent-before-retirement)
for physical work limits and how they apply to aliases, validation, and cleanup.

### Retained Cleanup

For `journal_cleanup_incomplete`, preview and confirm `daem recover` to finish
the retained cleanup. A failure deleting only terminal control metadata is
different: do not expect another recovery plan. See
[retained cleanup and garbage collection](../openspec/specs/interrupted-operations/spec.md#requirement-separate-retained-journal-cleanup-from-terminal-garbage-collection).

### Forward-Operation Barriers

An outstanding declaration transaction may require the command that wrote it,
even when an Apply journal has already been settled. Start with
[Manifest Metadata Update Was Interrupted](troubleshooting.md#manifest-metadata-update-was-interrupted).
See [declaration transactions and journal authority](../openspec/specs/interrupted-operations/spec.md#requirement-keep-declaration-transactions-and-journal-authority-separate)
for what `apply`, `lock`, and dry-run can clear.

### Older Tombstones

If daem reports a pre-1.0 tombstone, use the [interrupted-apply guidance](troubleshooting.md#apply-was-interrupted).
Do not discard the entry without independently establishing that no
interrupted apply or backup remains.

### Storage And Document Limits

See the [storage limits](../openspec/specs/durable-management-formats/spec.md#requirement-bound-storage-evidence-without-turning-it-into-partial-authority)
for manifests, lockfiles, statefiles, journals, backups, and managed documents.
