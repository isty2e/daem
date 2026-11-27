# Manifest authoring

## Purpose

Define declaration-side commands and their results. Applying a declaration, removing a host resource, and releasing ownership are different operations, even when they start from the same name. The [authoring workflow](../../../internal/workflow/authoring) and [presenter](../../../internal/cli/present/authoring_json.go) implement edits and result shapes.

## Requirements

### Requirement: Initialize only the selected manifest

`init` SHALL create a minimal manifest by default or preview it with `--dry-run`. An existing file requires `--force` and a revalidated regular referent. Its old contents need not parse; a selected symlink is preserved. No lock, managed state, recovery journal, cache, or host output is initialized. Human output reports the action and next step, with the starter content available in verbose; init JSON includes the selected destination and content.

#### Scenario: Force initialization through an existing manifest symlink

- **WHEN** the selected manifest links to a regular file that contains invalid TOML
- **THEN** `init --force` may recreate the starter in the referent without removing the link or applying host changes

### Requirement: Build the prospective declaration and lock together

`add` and `remove` SHALL validate the prospective manifest and lock before changing either. The write is transactional. Preview makes no persistent declaration, lock, cache or host change. A repeated matching add retains declaration bytes, including fields not settable by the CLI, but still validates all sources and can create or refresh the lock. A different source, identity or setting does not overwrite the old declaration. An explicit target set can gain a target where supported; an inherited set is not broadened by add.

`remove` takes an actual declaration key, including a skill's `id` if present. Omitted target/scope selects one unambiguous resource; explicit selectors narrow or assert matching placement, not authoring defaults. An unmatched or repeated removal fails. Removal of the last target drops the declaration. A partial-target removal from one member of a multi-member group fails; split the declaration on purpose rather than changing other members. Agent-config removal selects a complete declaration even if its original source is gone; remaining sources still have to resolve.

#### Scenario: The declaration is unchanged but the lock is stale

- **WHEN** a matching add runs and the unchanged declaration's current source no longer meets prospective validation
- **THEN** the command refuses rather than reporting an unconditional no-op or replacing either file

### Requirement: Respect each authoring leaf's selected syntax

An authoring command SHALL edit only forms supported by its leaf parser. Instructions allow local and Git file sources, including admitted repo shorthand with explicit ref; S3 instruction authors edit the manifest. Do not combine an embedded Git path with `--path`. Skill add can use a Git root; group add takes exact `--member` names, not selectors. Its membership identity is the set rather than the input ordering. It cannot split overlapping groups, regroup existing direct skills, or assign custom IDs to skills. Hook add takes one opaque command and a positive whole-second duration when specified; target override fields stay in TOML.

Agent-config add selects one target, one scope, and `--config-path` under the host root with a prepared independent local, Git, or S3 file source. A CLI-relative source operand resolves from cwd; project sources under the manifest can be stored relatively and global local sources absolutely. `--path` selects a Git source member, not a host destination. Leading-option IDs can follow `--` where the leaf admits it. Inline arrays can be added to or removed from where that leaf supports safe editing; this is not automatic permission for every other edit surface.

MCP add takes a portable command, ordered `--arg` values and supported `--env-from CHILD=SOURCE` names. The CLI requires uppercase ASCII source-variable names (`[A-Z_][A-Z0-9_]*`) without changing case or looking up values; this is not a manifest/import restriction or a secret detector. Repeating add compares the whole environment map independent of flag order rather than merging it. Binding and name-shape errors omit submitted values; admissible names in later diagnostics and literal argv/path data have their own disclosure rules. Pi Adapter add ensures an eligible explicit provider, using the profile's default selector for a new one; removing a server never implicitly removes that provider.

#### Scenario: Re-add an MCP server with one mapping omitted

- **WHEN** the existing declaration requires two environment mappings and the new request supplies one
- **THEN** add reports a mismatch rather than deleting or merging the missing mapping

### Requirement: Import supported live evidence without adopting it

Ordinary `import` SHALL require at least one target and select only modeled instruction, skill, hook, standalone MCP or source-exact extension evidence. It writes declaration and copied sources, not lock, state, host outputs, claims, cache or an apply journal. `--source-dir` controls the source destination, not the discovery root. The default is beside the manifest under its basename with `.d`; relative destinations stay within the workspace and cannot overlap its manifest or management metadata. Non-merge import requires absence; merge requires an existing valid manifest before discovery. An active apply interruption blocks both preview and write before discovery.

Conflicts refuse publication; unsupported or lossy observations are skipped with a typed reason rather than imported approximately. Exact extension declarations preserve matching IDs and relative order, and reject conflicting IDs, duplicate host-load identities, and contradictory native/manifest order. Inspection does not claim the host resource; a later exact `apply --manage-existing` is a separate decision following a lock.

#### Scenario: Merge would encounter a conflicting live extension placement

- **WHEN** selected host and existing declaration evidence disagree on exact identity or contradict order
- **THEN** import does not alter the manifest or host to force them to match

### Requirement: Revalidate import evidence within bounded discovery

Import SHALL use stable regular instruction files (up to 128 MiB) and hook/MCP documents (up to 4 MiB in the admitted codecs). Instruction import follows parent directories, not a final link. Hook JSON uses one UTF-8 value, unique keys and at most 64 levels. A refused file does not yield a partial resource. A skill may resolve its admitted top-level link but rejects nested link entries and needs a regular `SKILL.md`.

Skill import admits at most 100,000 entries, 64 descendant directory levels and 4 GiB of regular-file bytes per tree. Root-listing totals are 100,000 names and 32 MiB of name bytes, up to 4,096 bytes per name. Source identity and final freshness each have separate aggregate limits of 400,000 entries and 16 GiB; they do not widen per-tree limits. Listed names still count when a later check rejects a tree. Shared roots/routes can reuse work, but contributors, aliases, retained inventories and exact selected identities remain freshness evidence through publication, including merge no-ops. Representative bytes do not replace identity checks on other contributing routes. Unexpected changes to those retained facts invalidate the import.

Discovery does not promise continued absence or ineligibility of a root or child excluded in that pass. An exclusion grants no publication authority. A later run can reconsider it; this does not waive freshness of imported, merge-relevant or observed root/alias evidence. See the [import checks](../../../internal/workflow/adopt) and [supported input guidance](../../../docs/cli.md#import).

#### Scenario: A merged skill had two contributing routes

- **WHEN** import chooses bytes from one route and the other route changes before publication
- **THEN** the unchanged relationship of the first route is not enough to publish a merged declaration

### Requirement: Distinguish removal from host-preserving management release

`unmanage` SHALL release only the selected declaration's exact own management and preserve existing host content. It does not imply removal, provider execution or forced adoption. Agent-config release removes the whole declaration and its default-lock/managed rows when present; shared bindings and foreign claims remain. It can use retained authority without the declaration, lock or original source, and without matching host values. Inline array declarations require a safely editable form or prior manual removal. An ambiguous placement or unsafe declaration edit is refused before releasing claims.

Extension release can use an exact retained claim/pending record after manual omission. Neither a declaration nor claim means not found. A state/claim write can use admitted format maintenance, but manifest/lock-only release does not start conversion. Active, unavailable or incompletely retired apply evidence can block release. Recover the interrupted metadata or management operation through its owning route; a retry does not take another owner's claim or replay host content. See the existing [interruption and release rules](../../../docs/cli.md#unmanage-agent-config).

#### Scenario: Drop key ownership while leaving drifted settings in place

- **WHEN** a selected native declaration can be safely released but its host value changed outside daem
- **THEN** `unmanage agent-config` can release the exact claim without replacing that value

### Requirement: Report declaration-side results without implying host effects

The manifest-authoring JSON family SHALL keep operation/mode, selected destinations, resource and change counts, and separate lock, management and host facts. A matched unchanged add is one resource, zero declaration changes, `changes: []` and an `unchanged` reference, not a duplicate warning; a newly added provider is still a change. An unmanage receipt distinguishes previewed release, confirmed release and claim absence, and reports retained host state. Global retention is not ambient-consumer knowledge. An import conflict can be an ordinary error result; misuse uses the common family. Current field shapes and version come from the authoring presenter and `contractversion`.

Admitted import skips include typed reasons, target/scope, optional bounded detail and an action hint for action-required rows. Unknown or untyped reason information does not hide a potentially actionable row. Compact benign skips in human output; retain admitted actionable guidance, and detailed rows in verbose and JSON. One pass admits at most 4,096 skip rows and 256 KiB of aggregate diagnostic data; individual details beyond 4,096 bytes become whole-value digests and counts. Producer failure discards that producer's rows; aggregate budget exhaustion returns a common failure with a bounded explanation, not a partial plan or write. Projection-specific merge rows carry the subject as well as the resource reference. Extension source disclosure is exact only where grammar proves it public; otherwise use a deterministic redaction. The [presenter](../../../internal/cli/present/authoring_json.go) owns the wire representation.

#### Scenario: A declaration remains unchanged while a lock is written

- **WHEN** an already satisfied add still needs to refresh the adjacent lockfile
- **THEN** the result can have zero declaration changes and a `written` lock outcome at the same time
