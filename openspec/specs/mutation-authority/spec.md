# Mutation authority

## Purpose

Define the evidence that makes a planned effect legal and keeps it within the selected operation. The [operation plan](../../../internal/operationplan), [recovery gate](../../../internal/recoverygate), and [mutation revision checks](../../../internal/effect/mutation) implement those boundaries.

## Requirements

### Requirement: Bind effects to the selected work and current authority

Before its first external or visible effect, an operation SHALL qualify the selected paths and routes, acquire the applicable leases, and reserve sufficient physical work. Compare physical conflicts as well as logical owners; declared path spelling, an unrelated lease, or a current observation alone does not authorize a write. A proven exclusive branch can reserve the greatest reachable demand without summing mutually exclusive work.

Revalidate the applicable declaration, lock, destination, project root, permission and storage evidence at the operation's boundaries. This includes post-effect or pre-publication checks where a change could invalidate a continued effect or durable successor. Do not broaden approved work, use an exhausted reservation, or reconstruct a spent reservation from an earlier plan. Compensation for an already attempted effect uses the owning operation's evidence.

Replanning may account for the effects of a completed, approved phase. It must still match the authorized remaining work, fit the reservation and preserve the applicable effect order. A provider installation, for example, does not authorize changes to the rest of the plan. A cleanup-only recovery requires only recovery-root authority and does not borrow the active operation's host, state, or lock authority. See the Operation-Safety compiler in the [LikeC4 model](../../../architecture/model.c4) for the internal plan identities.

Cancellation or lost authority prevents later forward effects. Preserve a completed effect, a known process attempt or an uncertain publication as such; a later barrier failure does not establish that no effect happened or make a retry safe.

#### Scenario: A provider installation changes work still to be done

- **WHEN** a fresh assessment after the provider attempt no longer matches the admitted remainder or cannot fit the existing reservation
- **THEN** daem stops before the newly proposed effects and retains the known provider-attempt evidence

### Requirement: Keep the state-directory and recovery barriers independent

Authoring, unmanage, import/init publication, prepared MCP probe execution, apply and refresh SHALL recheck the applicable journal and file-set barriers before their effects. An unexpected first appearance of a previously absent StateDir is `stale_snapshot`; loss or replacement of bound directory or mount evidence is `file_set_access_unprovable`. File-set and journal outcomes remain independently reportable. An applicable shared-data conversion fence is separate from either local barrier. A selected operation can compensate its own work even when forward effects stop.

Read-only paths keep their command-specific admission rules and do not acquire a writer's authority. A lockfile-only write with a clear file-set barrier does not require recovery of an unrelated journal. [Interrupted operations](../interrupted-operations/spec.md#requirement-keep-declaration-transactions-and-journal-authority-separate) specifies what apply and lock may clear and what still needs the owning recovery command.

#### Scenario: An unrelated active journal exists during lock generation

- **WHEN** only the selected lockfile is to be generated and the applicable file-set barrier is clear
- **THEN** that journal does not require lock generation to perform recovery

### Requirement: Record freshness evidence at the granularity of the operation

An operation SHALL observe the facts needed to detect stale authority. Required-absence checks do not traverse a newly appearing entry. Directory inventories that require only names and kinds do not read descendant contents. When complete directory content is needed, one tree allows 100,000 descendants, 64 descendant-directory levels and 4 GiB of regular-file bytes. Each observation pass, including incremental observations, allows 400,000 descendant entries and 16 GiB of regular-file content.

Initial capture and freshness checks use the same limits and honor cancellation in enumeration and streaming. Overflow is a resource failure, not a partial, identity-only or timestamp-only proof. A single complete file, a required absence or an immediate listing does not inherit the per-tree byte limit. Declaration artifacts, host inventories and physical recovery work have their own limits.

#### Scenario: Complete-content revalidation exceeds its bound

- **WHEN** a replacement requires the old directory's full content and revalidation cannot capture that content within its limits
- **THEN** the visit does not establish a matching revision or permit that replacement
