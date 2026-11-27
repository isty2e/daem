# Lockfile

## Purpose

Record resolved input and realization contracts for later operations. The [lockfile codec](../../../internal/realization/lockfile) owns exact wire grammar, field names and correlations; [contractversion](../../../internal/contractversion/current.go) owns the current version. A lock is not installed-state or runtime evidence.

## Requirements

### Requirement: Resolve a complete lock against the selected declaration

`lock` SHALL normalize the whole manifest, resolve and validate lockable sources, expand groups, and compute authorized repairs. It writes the selected workspace's adjacent lock by default. `add` and `remove` use the same prospective generation and commit with the declaration as one authoring transaction. Direct TOML edits and ordinary import do not automatically regenerate a lock. A failed build preserves the prior lock. `--dry-run` and `outdated` use the same source resolution rules without writing a lock or persistent cache and remove temporary resolution data.

#### Scenario: A declaration is removed and the lock regenerated

- **WHEN** a successful lock no longer includes a previously declared skill
- **THEN** the stale lock entry is removed without inspecting or removing the installed output

### Requirement: Keep supply, output projections, and order facts distinct

A lock SHALL order subjects deterministically by entity and subject identities. Exact source facts, including resolved revision and content kind/hash, remain distinct from consumer-specific projections or aggregated host relations. Source paths from the cache are not serialized as artifact identities. Command-only hooks lock canonical contributions on admitted targets, not a source file for the command. Aggregate `key_path` is the canonical non-root JSON Pointer in the current schema; it is not ownership or permission. Host package recognition can produce a `pin_policy`; a version-like tag, range or unknown input does not by itself prove an immutable pin.

Instructions and skills retain an exact source and the manifest/profile-derived projection set. Physical placements with multiple consumers use the complete canonical set rather than one primary target; destination facts are portable, not machine-expanded paths. Exact permission mode, including mode `0000`, is distinct from omission. Instructions have a non-executable file-use contract and deterministic materialization. A supplied artifact does not waive structural or contextual projection checks. Selector-backed skills retain their declaration provenance; direct entries do not.

#### Scenario: Two targets share one physical skill directory

- **WHEN** two target routes admit the same placement
- **THEN** the locked projection identifies that output and both consumers without choosing a primary target

### Requirement: Lock declared extension order, not current host order

For admitted order classes, daem SHALL lock the class's host-load identity contract, its runtime meaning, and the exact members in manifest order. Classes with fewer than two members are omitted. Class IDs sort independently of member order. Duplicate host identities, dangling or cross-class members, incomplete coverage and changed profile meanings are rejected. Pi runtime precedence and OpenCode config order are distinct contracts. They neither report current sequence nor grant authority to reorder unmanaged content.

Current-lock consumers rederive the context-sensitive identities. If a relocation makes one stale, `status`, `apply`, `refresh` and `probe` refuse until relocking. `lock` and `outdated` can use the previous snapshot for comparison without treating its old path context as current authority.

#### Scenario: Move a workspace with context-sensitive member identities

- **WHEN** a persisted identity no longer matches the current selected workspace
- **THEN** a current-lock consumer refuses and lock comparison can still help regenerate the snapshot

### Requirement: Preserve strict reading and bounded regeneration across schemas

Daem SHALL write schema 7 and read valid schema 6 plans without rewriting them, translating their released aggregate paths in memory and applying current validation. Invalid UTF-8, unknown keys or facets, invalid identities, missing required correlations or permission data, and unknown versions are not usable as plans. The current grammar does not accept the legacy `content_path` or `generated_at` fields. New incompatible wire grammar requires a new version.

Lock generation and admitted transactional authoring can replace schemas 3–6 without relying on their old body; schemas 3–5 are not plans. Unknown earlier or future schemas are preserved. Only standalone `lock` can rebuild a current-version lock with an intact version header and a malformed TOML body. It revalidates the checked destination and ownership of the write; unreadable files and valid TOML with unrecognized contents are not damaged-body rebuilds. `apply`, `status`, `outdated` and paired authoring do not rebuild such a lock.

#### Scenario: A current header survives a malformed body

- **WHEN** `lock` validates the intact current header and identifies a malformed TOML body at a safe selected destination
- **THEN** it may preview or write a complete rebuild; ordinary plan consumers still reject the damaged input

#### Scenario: Read a valid old plan and preview replacement

- **WHEN** schema 6 contains a valid locked plan
- **THEN** a current-lock consumer can read it without writing, and standalone lock can generate a replacement without comparing the old body

### Requirement: Do not convert source identity into output authority

The lock SHALL describe resolved inputs and admitted projections, not current output, cache ownership or execution readiness. Apply refuses host mutations whose physical path equals, contains or is contained by a local source consumed by the manifest. A lock write is also refused if its destination would overwrite the manifest or a source. The content compared by exact Supply can include executable class; managed content may still be current after only that source fact changes.

#### Scenario: A local source overlaps an output

- **WHEN** an apply mutation would edit a consumed local source or its containing tree
- **THEN** daem refuses it instead of relying on the already captured source identity
