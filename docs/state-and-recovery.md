# State And Recovery Reference

Use this page to interpret stored ownership, recovery results and size limits.
[Concepts](concepts.md) explains manifest, lock and state roles;
[Troubleshooting](troubleshooting.md#apply-was-interrupted) gives recovery commands.

- [Statefile](#statefile)
- [Shared global ownership](#shared-global-ownership)
- [Mutation revision evidence](#mutation-revision-evidence)
- [Recovery journal](#recovery-journal)
- [Retained cleanup](#retained-cleanup)
- [Forward-operation barriers](#forward-operation-barriers)
- [Storage and document limits](#storage-and-document-limits)

## Statefile

The statefile records outputs written or registered by the selected manifest,
project carrier claims and pending carrier transitions. Global authority also
uses shared registries. Delegated-attempt records are bounded historical
diagnostics tied to a locked plan, not evidence of current installation,
package/cache convergence, runtime health, tool inventory, credentials or trust.

Host-route diagnostics retain only the latest request per subject, target,
scope and route id; a changed request hash replaces the prior diagnostic.
Statefile v9 separates mechanical process reason from post-attempt working-
directory authority. Daem reads v8's legacy combined reason and writes v9.

The statefile is private authority data. Daem writes and accepts it only as an
invoking-user-owned regular file with exact mode `0600`; final symlinks,
special files, replacement during a read, oversized content, and other
permission modes are rejected.

Exact-mode projections retain last-verified permission bits. Executable-class
files retain executable class through content identity, not a read/write-bit
baseline. Neither is a current-mode observation: interrupted-operation recovery
separately captures the physical mode it must guard and restore.

Externally changed managed outputs report drift rather than being overwritten.
Unowned existing outputs report `unmanaged_output_exists`; exact adoption must
also match required file metadata for mode-sensitive outputs.

Import does not claim host state. Supported `apply --manage-existing` adoption
requires an exact live match and fresh validation. Carrier adoption records
future bounded relation-removal authority without invoking the host route; it
does not grant package/cache ownership, runtime readiness or ambient exclusivity.

### Shared Global Ownership

Different manifests can select the same global destination. The shared
managed-output registry permits one owner per whole path or overlapping config
projection; equal bytes do not permit co-ownership. Losslessly disjoint
projections, such as separate MCP entries, may have different owners.

The separate carrier registry permits multiple known manifest consumers of one
carrier while retaining exact target, scope, source, route and owner identity
for each relation. Neither registry proves exclusive host ownership or
accounts for non-daem consumers. Global install claims commit to the registry
before retiring the project-state pending fact; interruption can leave pending
work that needs fresh route-supported evidence, not assumed completion.

`status` and `apply --dry-run` report `ownership_conflict` with the owning
manifest. Removing its declaration releases authority only after host and
state changes commit. Interrupted acquisition/release stays reserved or owned
until `daem recover` finishes; another apply cannot steal it.

### Mutation Revision Evidence

Commands retain the inputs needed to detect stale plans. Required-absence
checks do not traverse an entry that appears; directory inventories list only
immediate names and kinds. Complete-content observations use these limits:

| Scope | Limit |
| --- | --- |
| One complete-content directory tree | 100,000 descendants, 64 descendant-directory levels, 4 GiB of regular-file bytes |
| One observation pass, including incremental observations | 400,000 descendant entries, 16 GiB of regular-file content |

Initial capture and freshness checks use the same limits. Overflow fails with
a resource error, never a partial, identity-only or mtime-only revision.
Enumeration and byte streaming honor cancellation. Complete-content files,
required absence and immediate listings do not inherit the per-tree byte cap.
Declaration reads use the 64 MiB limit below; extension inventories use their
host observer's byte limit. Recovery has separate physical-work limits.

## Recovery Journal

Mutating `apply` writes its complete journal before reserving a new global
claim or changing host files/state. For ordinary local-filesystem process
failures, `recover` can clean up, roll back guarded changes, or finish claim
finalization after host and state commit. Recovery handles one interrupted
operation or its retained cleanup, not historical snapshots.

An active Linux journal from a previous boot is refused before recovery effects.
A clean reboot does not invalidate manifests, lockfiles or a newly planned
ordinary apply. Stable-storage durability and executable post-reboot recovery
are different guarantees; see [Platform Support](platforms.md).

### Journal Retirement

Journal retirement is itself recoverable. Changed journal identity/content,
unavailable evidence or failed durability retains recovery evidence and reports
a blocker or retry condition. Validation does not provide atomic compare-and-
rename against a non-daem writer racing afterward.

### Removal Intents

Visible `clean_before` or `clean_after` does not mean cleanup is finished.
Recovery checks every pending removal obligation before retiring the journal,
even those outside the selected recovery subset. A partial cleanup may resume
without treating the remaining tree as an unchanged original. Before promotion
to cleanup stage, residue must match the complete original state; the stage
then records progress through partial deletion.

Missing, surplus or malformed removal evidence blocks cleanup. So do a changed
or vanished parent namespace, conflicting private entries, unsupported residue,
unavailable identity or failed durability. Keep the journal and backups; do not
rename residue or delete entries by prefix. A replacement parent cannot inherit
removal authority. Validation never creates a missing parent. Private cleanup
names are exact, not permission to scan for similarly named files; deliberate
same-user forgery of those names is outside the guarantee.

### Recovery Work Budgets

| Scope | Limit |
| --- | --- |
| Removal intents per journal | 4,096; at most before and expected-after whole-path states per intent |
| Each planning pass and bound execution lifecycle | 90,112 namespace/slot observations; 524,288 root/path component visits; 400,000 recursive entry visits; 16 GiB of regular-file content |
| One bound physical path | 256 components |
| One tree | 100,000 entries, 64 descendant-directory levels, 4 GiB |

Host paths, backups, cleanup and authority observations share the planning
budget without resets; alias resolution counts too. These limits are independent
and their maxima need not compose. Daem reserves reachable work before effects,
verifies backup content during restoration and refuses growth beyond a freshly
observed cleanup ceiling, including an empty ceiling. An incomplete observation
with unknown exact work consumes its admitted maximum, not a cheaper fallback.

### Retained Cleanup

| Result | Meaning and action |
| --- | --- |
| `journal_cleanup_incomplete` | The journal has become retained cleanup residue. Ordinary commands do not finalize it. `daem recover --dry-run` reports `retained_cleanup_residue` and `finalize_journal_cleanup`; confirmed recovery touches only recovery metadata, not host outputs, state, ownership, manifest or lockfile. |
| GC-only residue after semantic retirement | Recovery is complete; best-effort deletion may leave private control metadata, but not the retired journal or backups. Deletion failure remains a command failure, yet no new recover plan exists and later commands are not blocked. The name alone grants no restart-time deletion authority. |

### Forward-Operation Barriers

Authoring, unmanage, import/init publication, prepared MCP probe execution,
apply and refresh recheck recovery/file-set barriers before effects. An
unexpected first StateDir appearance is `stale_snapshot`; loss or replacement
of its bound identity/mount is `file_set_access_unprovable`. Operation-owned
rollback/cleanup remains compensation.

Lockfile-only generation stays available during active recovery, but its
source-build/publication paths still require stable StateDir identity and a
clear file-set fence. For metadata markers and old tombstones, follow
[Troubleshooting](troubleshooting.md), not generic journal recovery advice.

### Older Tombstones

Current daem blocks but does not inspect, migrate, rename or delete pre-1.0
`.daem-tombstone-<32 lowercase hex>` evidence; malformed reserved names also
block. Use the writer to finish a valid old tombstone before upgrading. Never
discard it without independently proving no interrupted apply or backup remains.
See [interrupted-apply guidance](troubleshooting.md#apply-was-interrupted).

### Storage And Document Limits

| Document or traversal | Limit |
| --- | --- |
| Manifest or lockfile: physical read, in-memory decode and generated output | 64 MiB each |
| Metadata transaction target after-image, before-image or restored backup | 64 MiB each; 8 targets; 256 MiB total captured before-images |
| Statefile and carrier-registry semantic content | 16 MiB each |
| Recovery journal | 64 MiB |
| Individual regular-file recovery backup | 128 MiB |
| Managed Hook or MCP host document | 4 MiB at observation, mutation, recovery and codec output |
| Hook document structure | 256 events; 4,096 groups; 4,096 handlers; 256 bytes per event name |
| Immediate recovery-root inventory | 4,096 entries |
| One journal directory inventory | 100,000 entries |
| One retirement control | 64 entries, no descendant directory, 1 MiB of regular-file content |
| Managed directory snapshot | 100,000 entries, 64 descendant-directory levels, 4 GiB of regular-file content |

Oversized state is refused before planning; oversized journals and backups are
refused before covered mutation, and generated state/journals before publication.
Manifest/lock symlinks are accepted only while both the selected link and regular
referent stay stable throughout the read. Metadata-transaction recovery uses the
same bounds and checks every backup's type, size and hash before any restoration.

Cleanup preflights entry/depth bounds and mount continuity before advancing a
retirement control or deleting the first residue child. FIFO, socket and device
children block deletion before an earlier sibling is removed. Symlinks are not
followed; only the revalidated link is removed. Directory backup storage remains
proportional to the managed directory within these limits.
