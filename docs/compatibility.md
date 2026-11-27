# Skill compatibility

Use this guide to identify why a skill failed to lock and whether daem can
repair it. The [skill compatibility spec](../openspec/specs/skill-compatibility/spec.md)
contains the limits, target rules, and behavior of stored repairs.

## Diagnostic Layers

First check that the skill directory contains a regular file named exactly
`SKILL.md`, not a symlink. Then use the target named in the diagnostic to check its
metadata. A skill that locks for one host can fail for another. In particular,
check the installed directory name when OpenCode rejects an otherwise valid
document.

## Skill Document Resource Boundary

For `skill-document-too-large`, reduce the size of `SKILL.md` itself. Renaming
it or enabling repair does not change the
[raw document limit](../openspec/specs/skill-compatibility/spec.md#requirement-check-the-artifact-before-interpreting-its-metadata).

## Target Matrix

Check [skill placement roots](../openspec/specs/skills/spec.md#requirement-choose-a-cataloged-write-root)
and run `daem list paths` to see where daem expects to install. Hosts may scan
other locations. Host-side discovery is not a daem installation option. Unknown
frontmatter fields produce warnings on all five modeled targets; see
[target checks](../openspec/specs/skill-compatibility/spec.md#requirement-check-selected-target-policy-without-turning-warnings-into-refusals).

Profile references checked on 2026-06-22: [Codex](https://developers.openai.com/codex/skills),
[Claude Code](https://code.claude.com/docs/en/skills),
[OpenCode](https://opencode.ai/docs/skills/), [Pi](https://pi.dev/docs/latest/skills),
and [Agent Skills](https://agentskills.io/specification). Antigravity's
registered-directory behavior was checked on 2026-07-02. These dates record
what informed the profiles, not a statement about all newer versions.

## YAML Parsing Contract

If parsing fails, inspect the `---` delimiters and the top-level YAML mapping.
Keep required `name` and `description` values as strings. For exact handling,
see the [frontmatter reader](../internal/supply/compat/skill/frontmatter.go).

## Repair Scope

If the diagnostic reports `repairability=mechanical`, set `compat_repair = true`
on the affected `[[skill]]` or `[[skill_group]]` to allow the proposed repair.
Run `daem lock --dry-run` to review the result before writing the lock. If it
reports `repairability=manual`, fix the source first; for example, write a
missing description yourself.

### Replayable Repair Contract

If Apply reports a stale or damaged repair, check the recorded source and the
lock. The [replay rules](../openspec/specs/skill-compatibility/spec.md#requirement-replay-the-stored-transformation-and-verify-its-identities)
explain why daem cannot infer a replacement repair during Apply.

### Lockfile Repair Entries

Do not add a recipe by hand. Use the manifest opt-in and inspect the lock
preview. The [recipe codec](../internal/realization/lockfile/repair_conversion.go)
defines the generated fields.

### Initial Repair Operations

Examples of mechanical fixes are changing `skill.md` to `SKILL.md`, correcting
a recorded delimiter, and setting a name that is fully determined by the
declared install name. See [repair admission](../openspec/specs/skill-compatibility/spec.md#requirement-generate-repair-only-for-an-opted-in-lock)
for which changes can be recorded.

### Command Responsibilities

Use `lock` to generate an opted-in repair, `apply` to install the exact repaired
artifact, and `doctor` to diagnose a blocker. See the
[CLI guide](cli.md#doctor) for diagnostic commands.
