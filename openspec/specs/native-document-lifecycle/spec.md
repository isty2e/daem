# Native document lifecycle

## Purpose

Define how selected settings and dedicated whole configuration files settle, move, or change ownership. [Agent configuration](../agent-configuration/spec.md) defines admitted placements, atomic values and byte editing. [Capture and replacement](../native-value-commands/spec.md) define their distinct CLI approval boundaries.

## Requirements

### Requirement: Compose only the selected contributions to a physical document

Direct work on a shared settings file SHALL combine admitted changes to that physical document, even when reached through different paths. Codex MCP and managed keys in the same `config.toml` use one composed write and recovery. Direct native keys, instructions, skills and independent MCP work can share one recovery operation; compensation before state commit may restore this direct work together. Foreign and unselected contributions remain outside the edit. A conflicting selected key requires explicit replacement, not implicit permission from ordinary apply or adoption.

When removing managed keys, retain the containing file and structural parents, including generated empty objects or tables. Selecting an explicitly empty object as an atomic value is different: removing that selection deletes the value. Preserve unrelated values, empty objects and comments. TOML dotted siblings keep their dotted form; an already implicit parent does not gain a redundant explicit header on add/remove. An atomic child added and removed in an existing newline-terminated ordinary section without a new parent preserves its prior bytes. An EOF-only header can retain the separator newline needed for its first child. Successful state records do not retain parent-construction or formatting history.

#### Scenario: Removing the last managed child leaves an empty table

- **WHEN** the only selected change is removal of a managed child of `features`
- **THEN** daem can leave the empty table and preserve unrelated settings rather than restoring an inferred prior document

### Requirement: Retire or move dedicated documents without writing a drifted old path

Daem SHALL delete a removed dedicated document only when its live content matches the recorded baseline. A removed native key that is already absent, or a removed dedicated file already absent, can retire its management record without writing that setting or file. Dropping a selection does not authorize deletion of a changed value. Host-preserving `unmanage` releases a declaration as a whole; it is not a per-key removal command.

A dedicated file can move with the same ID under a newly locked path, target, or scope, with a distinct admitted destination. When the old file is already absent, a qualified move can create the new file from the locked source and retire the old claim without writing the previous path. A move across targets with `--target` requires both targets for a single apply. An alias of the same physical file is not a move. A conflicting old baseline or occupied unrelated destination is not resolved by the new source.

#### Scenario: The old dedicated file is already absent

- **WHEN** the same declaration moves to an admitted unused destination with verified authority
- **THEN** apply can create the new file and retire the old record without recreating or deleting anything at the old path

### Requirement: Transition key and whole-document ownership explicitly

A whole-to-keys transition SHALL retain the same declaration ID, target, scope and destination. Retain only the keys supplied by the selected source; released content remains in the file. Empty selection releases the whole-file claim without deleting the file. If the whole file has drifted, the values that will remain managed must match the locked key source; an approved capture can first align a writable local source with the live values. It does not automatically retain the prior desired values for undo.

Before state commit, rollback of narrowing can restore exact prior file bytes when the current file still matches the post-write bytes. If released content changed after the write but selected facts still qualify, restore only selected values and preserve external edits to released values and unrelated comments. A third selected value refuses recovery. Keep current permissions. After state commit, finalize without replaying host writes.

The reverse transition requires `apply --manage-existing` and an existing whole file exactly matching the locked dedicated source, including comments and formatting. It replaces exactly that declaration's key records with whole ownership without rewriting bytes or changing mode. A missing file, content mismatch, conflict, or another document participant's authority blocks widening.

#### Scenario: A narrowed file has a new edit outside the retained keys

- **WHEN** pre-commit recovery can still verify the selected values but released content no longer matches the exact post-write file
- **THEN** recovery restores only the selected values and leaves the newly edited released content intact

#### Scenario: Widening would absorb another declaration's keys

- **WHEN** the proposed whole source matches the document but another participant holds key ownership there
- **THEN** exact byte equality does not authorize whole ownership
