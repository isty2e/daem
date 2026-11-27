# Native value capture and replacement

## Purpose

Define the narrower interactive and noninteractive CLI boundaries for capturing settings into a local declaration source and expressly replacing selected live settings. Neither operation inherits the authority of a full apply.

## Requirements

### Requirement: Capture one selected physical source of host values

`import agent-config` SHALL capture values from one explicitly selected native file. Initial capture selects target, scope, host-relative `--config-path`, and RFC 6901 selectors or explicit whole-document ownership. A selected object means its current atomic leaves, not runtime defaults or effective layered settings. Missing initial values refuse the capture. Whole-file capture preserves admitted raw bytes. Initial capture adds a new ID and new file under `<source-dir>/agent-config/`; `--source-dir` defaults to the manifest basename with `.d` replacing its extension. Merging into a nonempty inline agent-config array requires the block form first.

`--update-source` selects the existing declaration's writable local vendor source and its declared membership. It cannot combine with initial-only placement options. A selector can narrow the set, not acquire siblings. Missing managed values remove those selected source entries and newly empty parents. A missing whole file can retire its declaration without removing the original source. Unselected values and comments remain. Selecting a new unused `--source` switches the declaration to that source; Git, S3 and cache material are not edit destinations.

#### Scenario: Narrow an update and a selected live setting is absent

- **WHEN** an already declared key is absent in the host file and other source keys were not selected
- **THEN** capture updates the selected absence without replacing unrelated source values

### Requirement: Disclose and confirm writes to named files using the real values

Capture SHALL disclose the selected source path, its referent and known affected references in the selected manifest. It does not certify a file is private or enumerate every reader. Masked preview values can still be written as plaintext; approval and staleness use actual values, not display masks. Noninteractive writes require `--yes --expect-plan` from the same reviewed `--dry-run --json`; interactive writes use the three-stream contract. `--diff` only discloses selected values in a human preview. Capture does not update the lock, host or ownership. Capturing during a whole-to-keys handoff replaces desired source values, without saving the old desired values for automatic restoration.

#### Scenario: Two distinct secrets have the same masked display

- **WHEN** a selected underlying value changes after review
- **THEN** an unchanged-looking preview mask does not make the approval fingerprint valid

### Requirement: Recover a selected capture transaction independently of new capture

A capture retry SHALL distinguish `operation: capture` from `operation: recovery`, with their preview and terminal modes. For recovery, qualify the original selection and source independently of the published marker. A reviewed recovery plan settles only that transaction and stops without reading new host values. A new capture plan, an unusable result, or a common error does not prove the earlier write committed. Preserve unresolved evidence; do not turn a lost response into automatic re-execution. The compatible selected source-switch or retirement keeps trailing standalone comments and blank lines; admitted version-4 interrupted markers may predate that edit behavior.

The [capture and recovery presenters](../../../internal/cli/present) own exact output. Capture has literal declaration/source/referent, known references, and selected key presence independent of masked display. Whole-file capture has no key rows or arbitrary file contents. Recovery adds the recorded direction and evidence instead of treating the prior values as a new capture. With JSON selected, identifiers that cannot be safely disclosed exactly cause [refusal](../cli-protocol/spec.md#requirement-distinguish-public-references-from-stored-identities) before effects.

#### Scenario: The prior write result was lost

- **WHEN** a fresh preview selects rollback or finalize for a matching interrupted capture
- **THEN** authorization of that recovery fingerprint does not also authorize another capture

### Requirement: Replace only expressly approved locked values

`replace agent-config` SHALL select only declared atomic keys at the existing admitted placement. An omitted selector selects the current declared set; a selector can narrow to existing declared descendants. No whole-document, ownership-mode or placement change, unrelated resource or delegated host command is implied. Ordinary apply and exact adoption still refuse a conflicting value. Confirmed replacement binds the selection, declaration, locked source, actual values, management claims, and the file's owner, group, kind and special mode bits. Ordinary read, write, or execute permission changes are retained; they and an inode replacement alone do not invalidate otherwise matching approval.

Execution rereads unrelated values/comments before composing the edit. If compensation is needed before state commit, restore the actual immediately prior selected value, not an older stored baseline; a third selected value refuses recovery. Finalization after commit does not replay host writes. [Replacement output](../../../internal/cli/present/native_replacement.go) reports selected key presence separately from displayed values and counts only selected host or management changes. A record-only action can update stale management without rewriting a matching value. No undo archive is retained after successful replacement.

#### Scenario: An unrelated key is edited after review

- **WHEN** the approved selected facts still match and only an unselected value changed
- **THEN** confirmed replacement preserves the unselected edit rather than applying an old whole-file preview
