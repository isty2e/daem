# Skill Compatibility

`daem` treats agent skills as declarative artifacts that must be valid before
they are locked and applied. Compatibility checks are intentionally stricter
than a best-effort copy: a skill that is likely not loadable by a selected
target should fail during lock/apply instead of being installed silently.

This page is based on official target documentation checked on 2026-06-22 and
local Antigravity CLI runtime evidence checked on 2026-07-02:

- [Codex Agent Skills](https://developers.openai.com/codex/skills)
- [Claude Code Skills](https://code.claude.com/docs/en/skills)
- [OpenCode Agent Skills](https://opencode.ai/docs/skills/)
- [Pi Skills](https://pi.dev/docs/latest/skills)
- [Agent Skills specification](https://agentskills.io/specification)
- Antigravity CLI registered skill catalog under
  `~/.gemini/antigravity-cli/brain/.../registered_skills_list.md`

## Diagnostic Layers

`daem` separates artifact validity from target compatibility.

Artifact validity is target-independent. A skill source must resolve to a
directory, contain an exact uppercase `SKILL.md`, and that `SKILL.md` must be a
regular non-symlink file.

Target compatibility is target-specific. It is checked after artifact validity
and covers YAML frontmatter, target identity rules, selection metadata, control
fields, and known collision behavior.

## Skill Document Resource Boundary

The raw `SKILL.md` compatibility document is limited to 1 MiB. The limit is
measured before BOM or newline normalization and before YAML decoding. An exact
1 MiB document is accepted; one additional byte is rejected with the stable
`skill-document-too-large` compatibility diagnostic.

The same limit applies to lower-case `skill.md` while planning or replaying the
supported casing repair, and to every repaired `SKILL.md` output before recipe
creation or file replacement. Lock, apply, status, doctor, validation, repair
planning, and repair replay therefore do not widen the document accepted by an
earlier phase.

This is not a whole-skill or repository size limit. Other files in a skill
artifact remain streamed through the artifact boundary and may exceed 1 MiB;
only files interpreted as the skill compatibility document use this in-memory
parsing and edit budget. Rejection never truncates the document, emits a partial
recipe, or publishes repaired staging.

## Compatibility Axes

The internal compatibility profile for each target covers these axes:

- `artifact`: directory shape and exact `SKILL.md` casing.
- `discovery`: target-visible skill roots and traversal behavior.
- `frontmatter`: required YAML frontmatter and field-level constraints.
- `identity`: how the target addresses a skill and whether `name` must match
  the installed directory.
- `selection`: metadata used for automatic skill selection.
- `control fields`: target-recognized frontmatter fields and sidecar files.
- `collision`: documented behavior when identities collide.

## Target Matrix

| Target | Discovery | Frontmatter | Identity | Selection | Control fields | Collision |
| --- | --- | --- | --- | --- | --- | --- |
| Codex | `.agents/skills` in repo ancestry, `$HOME/.agents/skills`, `/etc/codex/skills`; daem also models `$HOME/.codex/skills` as a discovered Codex root. | `SKILL.md` must include `name` and `description`; frontmatter is parsed as YAML. | Codex uses the skill name, description, and file path; daem does not currently block when `name` differs from the install directory because the Codex docs do not state that as a load blocker. | `description` drives implicit invocation and is required. | Standard Agent Skills fields are accepted; Codex-specific appearance/dependency/policy metadata belongs in `agents/openai.yaml`. | Codex docs say same-name skills are not merged and can both appear in selectors. |
| Claude Code | `.claude/skills` in project or `$HOME/.claude/skills`. | YAML frontmatter between `---` markers is required; all listed fields are optional; `description` is recommended. | The directory name is the command name; `name` is an optional display label. | `description` is recommended, and Claude can fall back to the first paragraph if absent. | Recognized fields include standard optional fields plus Claude fields such as `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `allowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, and `shell`. | Collision behavior follows Claude Code scope and command-name resolution. |
| OpenCode | `.opencode/skills`, `$HOME/.config/opencode/skills`, plus compatible `.claude/skills` and `.agents/skills` roots. | `name` and `description` are required; `description` must be 1-1024 characters. | `name` must be 1-64 lowercase alphanumeric/hyphen characters and must match the directory containing `SKILL.md`. | `description` is required for correct selection. | Recognized fields are `name`, `description`, `license`, `compatibility`, and `metadata`; unknown fields are ignored by the target and warned by daem. | Skill names must be unique enough for deterministic discovery. |
| Pi | `.pi/skills`, `$HOME/.pi/agent/skills`, and compatible `.agents/skills`; Pi roots also support recursive discovery, and native Pi roots support root `.md` skills outside daem's current directory-skill artifact shape. | `name` and `description` are required; missing `description` is not loaded. | Pi warns about invalid names but remains lenient, and explicitly allows `name` to differ from the parent directory. | `description` determines when the skill loads. | Recognized fields include `name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools`, and `disable-model-invocation`; unknown fields are ignored. | Name collisions warn and keep the first discovered skill. |
| Antigravity CLI | `.agents/skills` for project directory packages and `$HOME/.gemini/config/skills` for global directory packages. Builtin `~/.gemini/antigravity-cli/builtin/skills` and plugin `~/.gemini/config/plugins/<plugin>/skills` roots are target-visible but not daem placement roots. | `name` and `description` are required for registered directory packages. | The registered catalog addresses skills by frontmatter name; the local builtin `antigravity_guide` directory contains `name: antigravity-guide`, so directory/name mismatch is allowed. | `description` appears in the registered catalog and drives skill selection. | Standard Agent Skills fields are accepted by daem; unknown managed-skill fields are warned but non-blocking. | Collision behavior is target-defined by the registered skill catalog; daem manages only declared placement outputs. |

## YAML Parsing Contract

`SKILL.md` frontmatter is parsed with a real YAML parser, not line splitting.
Valid YAML scalars, quoted strings, comments, nested maps, lists, and folded or
literal multiline strings are accepted. `name` and `description` must be YAML
strings when present; empty/null values are treated as missing for required-field
checks.

## Repair Scope

Compatibility repair is manifest-declared. A `[[skill]]` or `[[skill_group]]`
entry may set `compat_repair = true` to permit daem-defined mechanical repairs
while locking that resource. Omitted or false means no repair is permitted:
`lock`, `apply`, `status`, `doctor`, and authoring commands may diagnose
repairable incompatibilities, but they must not mutate the artifact or write a
repair recipe for that resource.

`compat_repair = true` is a boolean policy, not a repair selection language. It
does not mean "make this skill work however possible". It permits only the
registered deterministic operations below when every operation can record exact
old state, exact new state, exact preconditions, and exact postconditions. If a
repair would require semantic judgment, lossy normalization, generated prose, or
changing files outside the skill loader contract, locking must fail with manual
guidance instead.

For `[[skill_group]]`, the policy applies to every selected child skill. The
lockfile records repair recipes on the expanded child lock entries, not on the
group selector itself, because each child has its own resolved source identity,
content hash, install name, target set, and repaired output hash.

### Replayable Repair Contract

The invariant is:

```text
resolved original source + ordered repair recipe with exact preconditions
  => repaired artifact bytes + repaired content hash
```

The lockfile is the authority for that transformation. For every repaired skill
entry, it must record:

- the original source identity and resolved reference used for locking.
- the original content hash before repair.
- the repair recipe version and an ordered list of operation records.
- a repair recipe hash covering the ordered operations and their preconditions.
- the repaired content hash after all operations.

`apply` installs the repaired artifact bytes, not the upstream bytes alone. This
is necessary because the selected agent loads files from the applied artifact,
and a target that requires exact `SKILL.md` casing or a strict frontmatter name
will still fail if daem only remembers that a repair was possible. The upstream
source identity remains auditable through the original content hash and recipe,
but the managed runtime artifact is the repaired byte tree.

Recipes must be lossless and replayable. Each operation records enough old state
to prove that it was applied to the expected input and enough new state to
reproduce the output without consulting mutable upstream data or re-running
compatibility inference. A recipe that cannot be replayed byte-for-byte is not a
valid lock entry.

Lossless and inverse-reconstructible refer to the accepted logical artifact
tree: normalized relative paths, directory/regular-file entry kinds, regular
file bytes, and permission bits explicitly recorded by a repair operation.
Replay staging must apply recorded modes explicitly rather than inherit the
process umask. Symbolic links and special files are rejected. Physical inode or
hard-link identity, timestamps, uid/gid, xattrs, and ACLs are not recipe state
and are not claimed to round-trip. The artifact hash-v1 identity separately
tracks bytes, paths, entry kinds, and each file's executable class; a repair
operation may use a stronger recorded mode precondition for an entry it edits.

### Lockfile Repair Entries

For a repaired Skill subject, `locked.subject.exact_supply` is the repaired
exact output that `apply` and `status` must materialize. The original resolved
input is retained by both the deterministic derivation and the canonical
`repair_recipe`; the lockfile never substitutes the output identity for that
input or reruns compatibility inference during apply:

```toml
[[locked.subject]]
entity_id = "skill:review"
subject_id = "resource/skill/review"
ownership = "manifest"
on_absent = "apply"

[locked.subject.exact_supply]
source_id = "git:locator=https%3A%2F%2Fgithub.com%2Fowner%2Frepo.git&path=skills%2Freview&ref=name%3Amain"
resolved_ref = "0123456789abcdef0123456789abcdef01234567"
kind = "directory"
content_hash = "sha256:repaired"

[locked.subject.derivation.deterministic_transform]
recipe_hash = "sha256:recipe"
algorithm_id = "compat.skill.repair"
algorithm_version = "v1"
execution_domain = "daem:compat/skill/repair"

[locked.subject.derivation.deterministic_transform.input_identity]
source_id = "git:locator=https%3A%2F%2Fgithub.com%2Fowner%2Frepo.git&path=skills%2Freview&ref=name%3Amain"
resolved_ref = "0123456789abcdef0123456789abcdef01234567"
kind = "directory"
content_hash = "sha256:original"

[locked.subject.repair_recipe]
version = 1
recipe_hash = "sha256:recipe"

[[locked.subject.repair_recipe.operation]]
kind = "rename"
from = "skill.md"
to = "SKILL.md"
file_hash = "sha256:file"
mode = 420
```

The omitted `repair_recipe.input`, `repair_recipe.output`, and deterministic
transform `expected_output_identity` tables contain complete exact identities.
The recipe input must equal the transform input, and both output identities
must equal `exact_supply`; the recipe hash is recomputed from the canonical
ordered operations. Repair operations are stored as
`[[locked.subject.repair_recipe.operation]]` rows. Paths are normalized
slash-separated relative paths inside the skill artifact. Byte preconditions
and replacements are base64 strings, and file modes are serialized as decimal
integers. Lockfile validation rejects repair recipes on non-Skill subjects,
missing or mismatched identities, unsupported operation kinds, fields that do
not belong to the selected operation, unsafe relative paths, malformed base64,
and missing operation preconditions.

### Initial Repair Operations

The initial repair registry is deliberately small:

| Operation | Preconditions | Old state | New state | Postconditions |
| --- | --- | --- | --- | --- |
| `rename` | The path pair is exactly `skill.md` and `SKILL.md`, the source is a regular non-symlink file, the destination is absent, and the recorded source file hash and mode match the current input. | source/destination casing, file hash, file mode, and destination absence. | The opposite allowed casing with the same bytes and mode. | The source casing is absent, the destination casing exists with the recorded hash and mode, and no unrelated path changed. |
| `replace_bytes` | The target path is exactly `SKILL.md`, the file is regular, and its recorded input hash matches. The exact `old` byte sequence is present at the recorded byte offset. | `SKILL.md`, input file hash, byte offset, and exact `old` bytes. | Exact `new` bytes and output file hash. | The `SKILL.md` hash equals the recorded output hash and only the recorded byte range changed. |
| `set_frontmatter_string` | `SKILL.md` exists as a regular file, has valid YAML frontmatter, the parsed frontmatter is a mapping, and the target field is absent or exactly equals the recorded old YAML scalar. The field name is a non-empty string key. | file hash, field name, absent-or-existing old scalar value, exact edit byte range, and exact old bytes for that range. | exact string value, exact replacement bytes for that range, and output file hash. | Re-parsing `SKILL.md` yields the recorded string value for the field, the file hash matches, and all bytes outside the recorded range are unchanged. |

Recipe validation is ordered state-transition validation, not a global
path-uniqueness check. A later operation may address a path produced or changed
by an earlier operation only when the earlier postcondition exactly satisfies
the later precondition; lower-case-file rename followed by edits to `SKILL.md`
is the canonical example. Validation rejects operations whose required states
cannot coexist in order, ambiguous overwrite/collision chains, or precondition
hashes that do not connect. Inverse construction reverses this dependency order
and must prove the corresponding inverse pre/postcondition chain.

Supported uses of these operations include renaming a lower-case `skill.md` to
`SKILL.md`, normalizing a trivial byte-level frontmatter delimiter when the
exact old bytes are recorded, and setting a required `name` from the declared
install name when the value is fully determined by daem's manifest model.
OpenCode name/install-directory alignment is only repairable when daem controls
the install name and can record the exact old and new frontmatter values.

The registry intentionally excludes missing frontmatter creation, generated or
rewritten descriptions, optional-field translation, target-specific field
renaming, script changes, reference edits, asset edits, arbitrary YAML
reformatting, and any operation whose inverse cannot be reconstructed from the
recipe record.

### Command Responsibilities

`lock` is the only command that resolves and writes repair recipes. It resolves
the original source, classifies compatibility, applies permitted repairs in a
temporary artifact, validates the repaired artifact against the selected target
profiles, and writes both the original and repaired hashes. A failing lock must
leave the manifest unchanged and must not write a partial repair recipe.

`apply` replays or verifies the locked recipe before writing managed outputs. If
the original content hash, repair recipe hash, repaired content hash, or replay
postconditions do not match, apply must fail as stale or corrupted instead of
falling back to best-effort inference.

`status` uses the same replay and hash checks to report whether managed outputs
match the repaired locked artifact. It must not silently compare a repaired
managed skill to unrepaired upstream bytes.

`doctor` and authoring commands may report when `compat_repair = true` would
make an incompatibility repairable, but they must not invent a lockfile recipe.
Repair remains a lock-time reproducibility contract.

Failure diagnostics classify repairability without mutating source content. A
mechanical candidate reports `repairability=mechanical`, the replayable action
summaries, and the next step to set `compat_repair = true` before rerunning
`lock`. A manual blocker reports `repairability=manual` with the required source
edits, such as adding an author-written description, and must not imply that
auto repair can complete the lock.
