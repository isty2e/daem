# Managed output reconciliation

## Purpose

Define when a file, directory or selected configuration contribution is current, can be registered, can change, or must be left alone. [Shared ownership](../shared-ownership/spec.md) determines who can manage it; neither matching content nor this comparison grants authority over another owner's output.

## Requirements

### Requirement: Compare desired output, installed baseline, and live evidence separately

Daem SHALL create a missing desired output when the route is admitted, but refuse an existing unowned destination or contribution by default. `apply --manage-existing` can record a supported exact match after fresh validation. Compare the rendered output, including required metadata for the route, not merely the raw source file or an approximate match. Registration authorizes future managed changes and removal; it does not rewrite matching content. The same apply can still perform other disclosed work, such as correcting a shared document's mode or running an admitted delegate. Import alone acquires no ownership, and adoption neither takes another owner's output nor bypasses drift.

For an already managed path, match current content to the installed baseline before updating or removing an old output. At the same destination, an independently produced exact desired output can be recorded when the route qualifies it; daem does not rewrite the output to establish that match. A move checks the old path against the recorded baseline and requires an absent new destination. Even a new destination matching the desired bytes cannot be adopted as part of the move.

A missing desired path can be recreated. Under current retirement rules, an absent installed instruction, skill or hook asset instead blocks removal (`missing_output`) or a move (`drifted_output`). A missing managed hook or MCP projection being retired also blocks instead of authorizing absent retirement. [Native document lifecycle](../native-document-lifecycle/spec.md) specifies the absent key and dedicated-file exceptions. For a copied output shared by consumer targets of one declaration, check the live baseline before dropping any selected consumer; retain the output while another consumer remains.

Report `drifted_output` for a conflicting managed baseline and `unmanaged_output_exists` for an occupied unowned destination without qualified adoption. Do not offer a content diff as if a blocked plan could write. No general force, automatic baseline restore, or use of `recover` to rewrite ordinary drift is provided. A hash is evidence, not a backup.

#### Scenario: Matching bytes belong to another manifest

- **WHEN** an existing file matches the locked output but another manifest owns the path
- **THEN** adoption is refused without rewriting either the file or the other owner's claim

#### Scenario: The live file already matches a newly locked output

- **WHEN** a managed file at the same destination exactly matches the newly locked desired output, with the required evidence, but state still records the old output
- **THEN** an admitted apply can record the current baseline without rewriting the file

### Requirement: Compare installed content without undoing permission changes

For installed instructions, hook assets, copied skills and dedicated whole files, daem SHALL use installed content, not the current permission bits, for ordinary drift. An update to an existing regular file preserves its current mode where the operating system and the [platform metadata policy](../host-mutation/spec.md#requirement-preserve-supported-metadata-when-changing-file-contents-or-access) admit replacement, even for `0444` when the parent allows it. A permission-only change does not force content rewrite. Shared settings compare selected values; rollback keeps existing file permissions or restores recorded permissions when recreating a file the operation deleted.

Old executable-aware baselines have distinct verification. For a file, compare its same bytes against both possible executable classes. For a skill, try the installed tree's executable bits or the verified source's bits; recovery can use the exact backup's bits. If none proves the recorded baseline, leave the output in place. An original matching tree can be supplied to metadata conversion, or to recovery for an interrupted operation; it is not authorization to substitute bytes for the journal's backup.

#### Scenario: A user has made an instruction file read-only

- **WHEN** its content still matches and replacing it through the parent is admitted
- **THEN** the update keeps its current permissions rather than restoring the source or initial mode

### Requirement: Check shipped skill membership without claiming the extra files

A managed copied skill with recorded membership SHALL be current when its shipped relative paths, kinds, and contents match. Changing or removing a shipped entry is drift even if the next release no longer contains that entry. Extra files, caches and directories are not drift by themselves and are left in place on a no-op. This matching does not establish which code the host loads. A same-name directory at another cataloged discovery path can produce a nonblocking `skill_discovery_duplicate_retained` warning, not an adoption, deletion, or precedence decision.

Actual replacement, relocation or removal still checks and backs up the whole affected tree. Warn that the old tree will be removed with any extra entries, whether or not any were found; the warning is not an inventory. A tree that cannot safely be captured or revalidated, for example because of an unsupported extra entry or changed contents, can block that change even when the shipped subset was sufficient for a no-op. An update preserves the [current permissions of entries shipped at the same path and kind](../skills/spec.md#requirement-preserve-permissions-for-copied-skill-entries-that-persist); do not inherit the mode of a local extra that becomes newly shipped. On interrupted replacement, restore removed extras as well as shipped content; keep current permissions for matching path/kind members shipped on both sides, and use the recorded prior modes for the rest. Successful update and retirement are not an archive.

An older hash-only skill record can match the complete tree or use a path list from verified locked supply. For an installed-content digest, the live projection at those paths must reproduce the recorded digest. This can work even after relocking if the paths and kinds did not change. For an older source digest, the locked supply must itself match that digest and the projected installed content must match the verified release. Without usable membership or a successful whole-tree match, leave the directory in place; [baseline conversion](../metadata-migration/spec.md) can record the original membership even after removing a declaration. Read-only observation and no-op apply do not rewrite the old record. A newly recorded installation or verified conversion saves shipped membership for later checks without the original source.

#### Scenario: A cache is added and the skill has not changed

- **WHEN** the recorded shipped content still matches and an extra cache is present
- **THEN** that cache does not dirty the skill and ordinary no-op apply does not remove it

#### Scenario: The next release removes a locally modified shipped file

- **WHEN** the last installed membership still includes the changed file
- **THEN** the desired release's omission does not prove the old tree safe to replace

#### Scenario: The authenticated path list proves an older baseline

- **WHEN** a hash-only installed-content record is verified against the live paths and kinds supplied by the locked release, despite extra entries
- **THEN** the old baseline can qualify without a whole-tree match; no-op observation does not rewrite the statefile
