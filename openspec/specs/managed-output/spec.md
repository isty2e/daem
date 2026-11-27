# Managed output reconciliation

## Purpose

Define when a file, directory or selected configuration contribution is current, can be registered, can change, or must be left alone. [Shared ownership](../shared-ownership/spec.md) determines who can manage it; neither matching content nor this comparison grants authority over another owner's output.

## Requirements

### Requirement: Compare desired output, installed baseline, and live evidence separately

Daem SHALL create a missing desired output when the route is admitted, but refuse an existing unowned destination or contribution by default. `apply --manage-existing` can record a supported exact match after fresh validation. Compare the rendered output, including required metadata for the selected route, not merely the raw source file or an approximate match. Adopting a matching path or selected value does not rewrite that content; separately planned work can still restrict a shared document's permissions or make other admitted changes. Import alone acquires no ownership and a matching output owned by someone else cannot be adopted.

For an already managed path, match current content to the installed baseline before updating or removing an old output. At the same destination, an independently produced exact desired output can be recorded when the route qualifies it; do not overwrite a live value to make that exception true. A move or removal cannot rely on a match to new source bytes at the old destination. Verify the old managed baseline and the new destination as applicable. A missing desired output can be recreated; a missing output being retired is not uniform across resource families. [Native document lifecycle](../native-document-lifecycle/spec.md) specifies the absent key and dedicated-file retirement cases.

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

Actual replacement, relocation or removal still checks and backs up the whole affected tree and discloses that the removed tree includes extra entries. A tree that cannot safely be captured or revalidated, for example because of an unsupported extra entry or changed contents, can block that change even when the shipped subset was sufficient for a no-op. The new installed tree does not inherit the mode of a local extra that becomes newly shipped. On interrupted replacement, restore removed extras as well as shipped content; keep current permissions for matching path/kind members shipped on both sides, and use the recorded prior modes for the rest. Successful update and retirement are not an archive.

An older hash-only skill record can match the complete tree or the installed bytes at authenticated locked-release paths when that projection reproduces the original digest. If neither proves the recorded digest, do not replace or remove the directory. Read-only observation and no-op apply do not rewrite the old record. A newly recorded installation or verified conversion saves shipped membership; it can then verify removal even without the original source.

#### Scenario: A cache is added and the skill has not changed

- **WHEN** the recorded shipped content still matches and an extra cache is present
- **THEN** that cache does not dirty the skill and ordinary no-op apply does not remove it

#### Scenario: The next release removes a locally modified shipped file

- **WHEN** the last installed membership still includes the changed file
- **THEN** the desired release's omission does not prove the old tree safe to replace

#### Scenario: The authenticated path list proves an older baseline

- **WHEN** installed content at locked release paths reproduces a hash-only baseline even with extra live entries
- **THEN** the old baseline can qualify without a whole-tree match; no-op observation does not rewrite the statefile
