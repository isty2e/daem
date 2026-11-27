# Durable management formats

## Purpose

Define how to interpret recorded management and recovery evidence across compatible versions. The [statefile codec](../../../internal/assurance/statefile), [output registry](../../../internal/output/ownership/store), [carrier registry](../../../internal/effect/storage/carrierclaim), [journal codec](../../../internal/effect/journal/record) and [file-set codec](../../../internal/effect/fileset) implement the exact field, ordering and encoding rules. Manifest, lock, CLI and durable data versions are independent. [State image tests](../../../internal/assurance/statefile/image_codec_test.go) cover mixed baselines and version selection; [file-set tests](../../../internal/effect/fileset/reference_test.go) cover reference-image validation and older-reader refusal.

## Requirements

### Requirement: Select the statefile encoding from the facts it must retain

Daem SHALL keep installed baselines, native document commits, pending carrier operations, claims and bounded attempt history distinct. A host-route diagnostic retains the latest request per subject, target, scope and route ID; a different request hash replaces that diagnostic. Process outcome and post-attempt working-directory authority are separate. Diagnostic records do not establish current installation or runtime readiness.

The current writer selects its encoding by the facts recorded, not by the latest version number:

| Statefile contents, in precedence order | Without recorded statefile location | With recorded statefile location |
| --- | --- | --- |
| Copied-skill shipped path membership | v16 | v17 |
| Installed-content digest without membership | v14 | v15 |
| Legacy baselines with selected Claude global hook settings path | v12 | v13 |
| Other state using legacy baselines | v10 | v11 |

One snapshot may include both installed and older digests. Statefile v10 introduced RFC 6901 `key_path` for aggregate contributions. V8 and v9 remain readable without a rewrite; the v8 reader translates the old combined process/working-directory reason. A recorded lookup location is distinct from the filesystem comparison identity. Existing usable legacy files may keep their encoding; files may have different versions, but one registry uses one encoding. After writing new facts, including `pending_pin` or `pin_transition_observed` in an unchanged claim format, use a binary that understands them.

The optional `native_document_commit` advances only on a real native file mutation that would otherwise leave recorded state unchanged. It distinguishes rollback from finalization. A no-op does not advance it.

#### Scenario: A native replacement restores the value already in state

- **WHEN** the host file needs a real mutation but state would otherwise compare equal
- **THEN** the committed state carries a new native document commit distinguishable from the before-state

### Requirement: Keep registry versions and reference roles independent

The output registry SHALL distinguish whole-path and native-key claims from document bindings. Output v4 separates those facts; v5 also records qualified lookup locations. Populated v2 and v3 remain readable without rewriting them; the next state-changing write using the legacy physical format upgrades them to v4. Output v2 key paths represent literal slash-separated segments, not JSON Pointer escapes. Carrier registries use v2 or the located v3; a carrier claim is not a document binding or an output claim. A comparison identity alone is not a filesystem locator.

Accept retired statefile v7 only when all seven known fact arrays are present and empty (`managed_paths`, `managed_aggregate_contributions`, `pending_carrier_installs`, `pending_carrier_removals`, `managed_carrier_claims`, `delegate_attempts`, `host_route_attempts`). Accept a retired output or carrier registry v1 only with present empty `claims`. Reading an empty retired artifact does not rewrite or delete it; an admitted state-changing write may replace it with the applicable supported encoding. Never synthesize missing filesystem witnesses for populated old artifacts by changing their version numbers. When the original writer can safely settle recovery and remove its own managed outputs through ordinary remove/apply, that retirement can precede re-declaring, locking and applying them with a current writer.

#### Scenario: An old registry contains a claim but lacks a current witness

- **WHEN** a v1 registry contains a claim rather than a present empty array
- **THEN** daem refuses authority rather than treating the registry as empty or upgrading its claim by inference

### Requirement: Use the journal format that supports its recovery obligations

Recovery SHALL interpret the recorded evidence with the matching codec, independently of the current physical statefile encoding. Ordinary format 15 uses canonical Snapshot v10 for its recovery fingerprints. Current readers recognize ordinary v15, v18 and v20, and native v16, v17, v19, v21 and v22. Native v16 is an older selected-document variant; v17 binds native host documents and roots.

| Required evidence | Current ordinary encoding | Current native or mixed encoding |
| --- | --- | --- |
| Baseline form without the obligations below | v15 | v17 |
| Recreation of an ordinary projection root deleted by the operation | v18 | v19 |
| Installed-content evidence, including applicable separate tree restoration modes | v20 | v21 |
| Whole-file configuration permissions that require the newer native rule | — | v22 |

Choose the applicable form covering combined obligations, not merely the newest version. The whole-file permission form lets rollback preserve a surviving file's permissions and use the recorded before-mode when it has to recreate the file. When a journal has obligations an older reader cannot honor, use an encoding that reader rejects. Native management release journals use a separate release-only record in v17, or v21 for installed-content evidence. Current daem blocks every pre-v15 journal and neither adopts nor rewrites it.

If using the writer of a v10 Linux journal, independently establish that capture was in the current boot and that no relevant filesystem was unmounted or remounted. That writer can accept a reused legacy mount witness. If continuity cannot be established, preserve the journal, backups and host state rather than using the older binary to bypass refusal. Other historic recovery and state retirement guidance belongs in [Troubleshooting](../../../docs/troubleshooting.md#pre-10-durable-authority-schemas).

#### Scenario: An older reader cannot recreate a deleted settings file

- **WHEN** an interrupted removal records the newer projection-root restoration format
- **THEN** that reader refuses the journal instead of offering an incomplete rollback

### Requirement: Bind interrupted file-set transactions to the recorded variant

A file-set recovery SHALL use the selected transaction format and the owning operation's exact target set. Format 3 retains before-images and after-state evidence of an ordinary transaction. Format 4 supports static sets and, where required, one sized and hashed commit-point after-image bound to its target. Format 5 binds a published marker to a sized and hashed containing intent with before/after target facts for the qualified publication. A generation's marker does not grant another operation's recovery authority. Formats 1 and 2 remain preserved for their corresponding writer; current daem does not settle them by interpreting them as format 3.

Compare each target's recognized before/after content. Permissions alone do not make a manifest/lock pair a third version. Any unrecognized target value, wrong namespace, invalid image or out-of-authority path leaves the files and evidence in place. Verify restore inputs, including type, size and hash, before writing; do not restore a target already at its verified before-state.

#### Scenario: One declaration file contains a third version

- **WHEN** an interrupted file-set contains a manifest or lockfile matching neither recorded version
- **THEN** its owning recovery route refuses rather than replacing the unrecognized edit

### Requirement: Bound storage evidence without turning it into partial authority

Daem SHALL create statefile, journal and registry records with `0600`. For an existing record, changing ordinary permissions alone does not invalidate it or reset those permissions on the next write. The applicable reader requires a stable regular file owned by the invoking user and refuses final links, special files, replacement during reading and oversized content. Keep durable records where only trusted users can write them. Declaration artifacts have their separate [stable referent rules](../workspace-selection/spec.md).

Durable JSON records use the exact ASCII `lower_snake_case` schema fields, not case-insensitive aliases. Reject unknown fields, missing required fields and null where the format does not admit it. An unknown newer statefile, registry, journal or file-set version is refused without rewriting or deleting it; use a reader that supports the version rather than applying the old-format retirement procedure.

| Evidence or document | Current limit |
| --- | --- |
| Manifest or lockfile, including physical read, decode and generated output | 64 MiB each |
| Metadata transaction target image or restored backup, before or after | 64 MiB each; 8 targets; 256 MiB total captured before-images |
| Statefile, carrier registry and output registry content | 16 MiB each |
| Recovery journal | 64 MiB |
| Individual regular-file recovery backup | 128 MiB |
| Managed Hook or MCP host document at observation, editing, recovery or codec output | 4 MiB |
| Hook document structure | 256 events, 4,096 groups, 4,096 handlers; 256 bytes per event name |
| Immediate recovery-root inventory | 4,096 entries |
| Journal directory inventory | 100,000 entries |
| One retirement control | 64 entries, no descendant directories, 1 MiB of regular-file content |
| Managed directory snapshot | 100,000 entries, 64 descendant-directory levels, 4 GiB of regular-file content |

Oversized state is refused before planning; oversized journals and backups are refused before covered mutation. Check generated evidence before publication. These bounds do not turn unfinished discovery, validation or backup into success. [Mutation revision bounds](../mutation-authority/spec.md#requirement-record-freshness-evidence-at-the-granularity-of-the-operation) and [recovery work budgets](../interrupted-operations/spec.md#requirement-qualify-every-removal-intent-before-retirement) cover work across observations and effects, not just serialized byte sizes.

#### Scenario: Serialized successor state exceeds the limit

- **WHEN** new state cannot be encoded within its limit
- **THEN** do not publish a truncated record or present it as a durable successor
