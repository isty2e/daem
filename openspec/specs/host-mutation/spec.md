# Host filesystem mutation

## Purpose

Define native path, metadata and storage guarantees for supported filesystem effects. The [platform matrix](../platform-and-distribution/spec.md) governs command admission; [rooted path authority](../../../internal/assurance/pathauthority) and [storage commit](../../../internal/effect/storage/commit) implement the native checks.

## Requirements

### Requirement: Revalidate native namespace and mutation identity

Daem SHALL preserve the distinction between entry authority (including the final symlink itself) and referent authority. On Darwin, matching and exclusion follow each parent namespace's case behavior, including mixed mounts. A missing suffix inherits the deepest existing parent's behavior. Unavailable or contradictory evidence is an error, not a fallback to case-insensitive comparison.

Daem uses the OS-reported stored spelling, not a user-space Unicode normalization. An absent normalization-sensitive destination confers provisional comparison and exclusion evidence, not exact authority. Observe it again after creation in the same namespace, at the same depth and under the same filesystem semantics. Recovery refuses root replacement or a different descendant mount before effects.

Filesystem identity and metadata observations detect observable interference, not every possible concurrent change. Timestamp collisions and inode reuse can conceal competing writes, even on local filesystems. Keep observed inputs and namespaces stable while daem operates.

#### Scenario: Darwin first publishes an absent normalization-sensitive name

- **WHEN** the previously missing destination becomes visible
- **THEN** daem observes its stored spelling and namespace again before relying on exact authority

### Requirement: Preserve supported metadata when changing file contents or access

Replacement of a regular file SHALL preserve its ordinary permissions and supported backup-exclusion flag. On macOS the new file keeps its kernel-supplied attributes; daem does not copy Finder tags and other opaque attributes. An ACL daem cannot preserve, or an unsupported flag such as immutable, prevents replacement. Finder tags and `com.apple.provenance` do not invalidate content staging; a change to `com.apple.macl` does.

On Linux a regular-file replacement preserves supported existing `user.*` names and values; an unrelated attribute already on the new file does not block publication. The kernel manages signatures such as `security.ima` and `security.evm`; daem does not copy old signatures onto new bytes. Replacement refuses loss of a POSIX ACL or transfer of file capabilities to new code. Under SELinux the entire old context must be in effect and verified before writing content. If the login cannot set it, the original stays in place. Other access labels must match without relabeling. A new file or missing parent uses the context the kernel assigns to its temporary name; daem does not relabel it after rename for a different final-name rule.

Permission-only restriction changes access on the existing file, not content. On macOS, Finder tags and `UF_NODUMP` alone do not prevent that chmod; an ACL or a flag that prevents restriction still blocks it. Linux likewise refuses the restriction for a POSIX ACL, file capabilities or a blocking file flag such as immutable or append-only. A dry run is not a filesystem write test. Final verification can report failure even when publication has become visible.

#### Scenario: Linux cannot establish the old SELinux context on replacement

- **WHEN** the existing regular file has a different label and daem cannot apply or verify that label on the replacement
- **THEN** daem does not write the new payload or replace the original

### Requirement: Bind Linux journal work to boot and mount provenance

Linux journal-bearing mutation, including a state-only journal, SHALL require mount identity and the canonical boot UUID from verified procfs. Use `STATX_MNT_ID_UNIQUE` where available. The separately tagged `STATX_MNT_ID` fallback, including on kernel 5.15, supports same-boot recovery without unmount or remount; it does not guarantee detection of a reused legacy ID after remount. Recheck provenance before journal-covered host writes. Refuse mismatched witness schemes, missing evidence or an active journal from another boot rather than converting that evidence.

A clean new plan is not invalid merely because of reboot. Operation-local rooted checks do not necessarily require a durable recovery witness. On admitted storage, durable publication preserves platform-scoped recovery evidence across OS crash or power loss, subject to filesystem limits; preserved evidence does not authorize post-reboot Linux recovery.

Linux cleanup of restrictive private residue can use retained descriptors and verified procfs. If this evidence is needed and missing, report the failure and retain residue rather than using unchecked pathname chmod.

#### Scenario: An active journal survives a reboot

- **WHEN** recovery sees intact artifacts but their recorded boot differs from the current Linux boot
- **THEN** it refuses before recovery effects; an ordinary clean operation has no such historical-boot check

### Requirement: Preserve the supported NFSv3 single-client boundary

On Linux amd64, daem SHALL support unprivileged single-client `lock`, `apply` and same-boot interruption/recovery on NFSv3, including kernel 5.15, on trusted user-controlled paths with one writer to a manifest or destination. Concurrent cache workers within one command may share ancestors. Where atomic no-replace rename is unavailable, daem checks absence before ordinary rename; it does not thereby provide atomic exclusion against other clients or external writers.

A rename error that can follow server-side completion is indeterminate. Retain candidate staging and recovery artifacts rather than treating it as an uncommitted write and cleaning them automatically. The support envelope does not include multi-client exclusion or server-outage, reconnect or power-loss guarantees for those operations, including journal retirement and residue cleanup.

Preserve supported metadata exposed by the client. Unsupported NFS file-flag ioctls do not block ordinary operations; other inspection failures still do. Server-only ACLs and other metadata invisible to the client are not inspected or reproduced.

#### Scenario: An NFS rename returns an ambiguous error

- **WHEN** the server could have completed the rename despite the client's error
- **THEN** daem preserves the candidate evidence and reports uncertainty, without promising automatic rollback
