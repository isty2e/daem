# Skill Compatibility

Daem checks a skill's directory structure and target-specific metadata before
locking or applying it. A compatibility failure is reported rather than
silently installing content that the selected target is unlikely to load.

Profile references (checked 2026-06-22): [Codex](https://developers.openai.com/codex/skills),
[Claude Code](https://code.claude.com/docs/en/skills), [OpenCode](https://opencode.ai/docs/skills/),
[Pi](https://pi.dev/docs/latest/skills), [Agent Skills](https://agentskills.io/specification).
Antigravity's registered-directory behavior was checked 2026-07-02. These are
profile evidence dates, not continuous verification of newer hosts.

## Contents

- [Diagnostic layers](#diagnostic-layers)
- [Skill document size limit](#skill-document-resource-boundary)
- [Target matrix](#target-matrix)
- [YAML parsing](#yaml-parsing-contract)
- [Repair scope](#repair-scope), [operations](#initial-repair-operations) and
  [command responsibilities](#command-responsibilities)

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

The same limit covers lower-case `skill.md` casing repair and repaired output
across all commands, planning and replay. It is not a whole-artifact limit;
other files may exceed 1 MiB. Overflow never truncates the document, creates a
partial recipe or publishes repaired staging.

## Target Matrix

| Target | Discovery | Frontmatter | Identity | Selection | Control fields | Collision |
| --- | --- | --- | --- | --- | --- | --- |
| Codex | `.agents/skills` in repo ancestry, `$HOME/.agents/skills`, `/etc/codex/skills`; daem also models `$HOME/.codex/skills` as a discovered Codex root. | `SKILL.md` must include `name` and `description`; frontmatter is parsed as YAML. | Codex uses the skill name, description, and file path; daem does not currently block when `name` differs from the install directory because the Codex docs do not state that as a load blocker. | `description` drives implicit invocation and is required. | Standard Agent Skills fields are accepted; Codex-specific appearance/dependency/policy metadata belongs in `agents/openai.yaml`. | Codex docs say same-name skills are not merged and can both appear in selectors. |
| Claude Code | `.claude/skills` in project or `$HOME/.claude/skills`. | YAML frontmatter between `---` markers is required; all listed fields are optional; `description` is recommended. | The directory name is the command name; `name` is an optional display label. | `description` is recommended, and Claude can fall back to the first paragraph if absent. | Recognized fields include standard optional fields plus Claude fields such as `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `allowed-tools`, `model`, `effort`, `context`, `agent`, `hooks`, `paths`, and `shell`. | Collision behavior follows Claude Code scope and command-name resolution. |
| OpenCode | `.opencode/skills`, `$HOME/.config/opencode/skills`, plus compatible `.claude/skills` and `.agents/skills` roots. | `name` and `description` are required; `description` must be 1-1024 characters. | `name` must be 1-64 lowercase alphanumeric/hyphen characters and must match the directory containing `SKILL.md`. | `description` is required for correct selection. | Recognized fields are `name`, `description`, `license`, `compatibility`, and `metadata`; unknown fields are ignored by the target and warned by daem. | Skill names must be unique enough for deterministic discovery. |
| Pi | `.pi/skills`, `$HOME/.pi/agent/skills`, and compatible `.agents/skills`; Pi roots also support recursive discovery, and native Pi roots support root `.md` skills outside daem's current directory-skill artifact shape. | `name` and `description` are required; missing `description` is not loaded. | Pi warns about invalid names but remains lenient, and explicitly allows `name` to differ from the parent directory. | `description` determines when the skill loads. | Recognized fields include `name`, `description`, `license`, `compatibility`, `metadata`, `allowed-tools`, and `disable-model-invocation`; unknown fields are ignored. | Name collisions warn and keep the first discovered skill. |
| Antigravity CLI | `.agents/skills` for project directory packages and `$HOME/.gemini/config/skills` for global directory packages. Builtin `~/.gemini/antigravity-cli/builtin/skills` and plugin `~/.gemini/config/plugins/<plugin>/skills` roots are target-visible but not daem placement roots. | `name` and `description` are required for registered directory packages. | The registered catalog addresses skills by frontmatter name; directory/name mismatch is allowed. | `description` appears in the registered catalog and drives skill selection. | Standard Agent Skills fields are accepted by daem; unknown managed-skill fields are warned but non-blocking. | Collision behavior is target-defined by the registered skill catalog; daem manages only declared placement outputs. |

## YAML Parsing Contract

Frontmatter accepts YAML scalars, quoted/multiline strings, comments, maps and
lists. `name` and `description` must be strings when present; empty/null values
count as missing for required-field checks.

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

The lock records the original source/ref/hash, versioned ordered recipe and
its hash, and repaired output hash. Replay must reproduce those exact bytes
from the recorded input and preconditions, without mutable-upstream lookup or
new compatibility inference.

`apply` installs the repaired artifact bytes, not the upstream bytes alone. This
is necessary because the selected agent loads files from the applied artifact,
and a target that requires exact `SKILL.md` casing or a strict frontmatter name
will still fail if daem only remembers that a repair was possible. The upstream
source identity remains auditable through the original content hash and recipe,
but the managed runtime artifact is the repaired byte tree.

Lossless replay and inverse reconstruction cover normalized paths, directory/
regular-file kinds, bytes and recorded permission bits, independent of umask.
Symlinks and special files are rejected. Inodes/hard links, timestamps, uid/gid,
xattrs and ACLs do not round-trip. Artifact hash-v1 covers bytes, paths, kinds
and executable class; an operation may require stronger exact-mode matching.

### Lockfile Repair Entries

Recipes are generated lock data, not manifest input. Codec maintainers should
use the [stored repair contract](../ARCHITECTURE.md#stored-skill-repair-contract);
users select only `compat_repair` and inspect lock diagnostics.

### Initial Repair Operations

| Operation | Permitted change and required evidence |
| --- | --- |
| `rename` | Only `skill.md` ↔ `SKILL.md`; regular non-symlink source, absent destination, exact recorded hash/mode. Preserve bytes/mode and leave unrelated paths unchanged. |
| `replace_bytes` | Only regular `SKILL.md`; matching input hash and exact old bytes at the recorded offset. Change only that range and verify the output hash. |
| `set_frontmatter_string` | Only regular `SKILL.md` with a valid YAML mapping and nonempty field key. Field must be absent or match the recorded scalar; record exact old/new edit bytes and hashes, verify the resulting string, and preserve bytes outside the range. |

Operations run in recorded order: an earlier output must exactly satisfy the
next precondition, including rename followed by edits. Impossible states,
ambiguous collisions or disconnected hashes are rejected. Inverse replay must
satisfy the same chain in reverse order.

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

| Command | Responsibility |
| --- | --- |
| `lock` | Resolve original input, create allowed repairs, validate against selected targets and write original/repaired hashes. Failure leaves the manifest unchanged and writes no partial recipe. |
| `apply` | Replay/verify before host writes. Input, recipe, output-hash or postcondition mismatch fails as stale/corrupted; no best-effort inference. |
| `status` | Compare managed output to the repaired locked artifact using the same checks, not unrepaired upstream bytes. |
| `doctor` and authoring | May suggest opt-in, never invent a recipe. Recipe generation remains lock-time work. |

Failure diagnostics classify repairability without mutating source content. A
mechanical candidate reports `repairability=mechanical`, the replayable action
summaries, and the next step to set `compat_repair = true` before rerunning
`lock`. A manual blocker reports `repairability=manual` with the required source
edits, such as adding an author-written description, and must not imply that
auto repair can complete the lock.
