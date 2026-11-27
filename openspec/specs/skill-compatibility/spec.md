# Skill compatibility

## Purpose

Keep target-specific loader compatibility separate from the validity of a skill artifact. Permit small, replayable transformations only when the declaration opts in.

## Requirements

### Requirement: Check the artifact before interpreting its metadata

A normal skill artifact SHALL be a directory with an exact uppercase `SKILL.md` that is a regular, non-symlink file. The [compatibility loader](../../../internal/supply/compat/skill) accepts up to 1 MiB before newline, BOM or YAML processing. The limit also applies to casing candidates and repaired bytes, not to every file in the artifact. Oversized input produces `skill-document-too-large`, not truncated metadata or a partial repair.

#### Scenario: A lower-case document exceeds the raw budget

- **WHEN** an opt-in skill has a repairable filename spelling but that document is over the compatibility limit
- **THEN** the casing option does not bypass the budget

### Requirement: Check selected target policy without turning warnings into refusals

Daem SHALL use the selected [compatibility profiles](../../../internal/supply/compat/skill/profile.go) to check YAML frontmatter. `name` and `description` are strings when present; empty and null values count as missing. Codex, OpenCode, Pi and Antigravity CLI require both; Claude Code requires frontmatter but a missing description is a warning. Missing required fields and other selected-profile errors block a lock unless an admitted repair resolves them. They do not permit a partial authoring write or a host mutation. Fields not recognized by the selected profile produce compatibility warnings, not lock refusals.

OpenCode requires the name to match the installed directory, match `^[a-z0-9]+(-[a-z0-9]+)*$`, and not exceed 64 characters; the description is limited to 1,024 characters. For Pi the name format and those lengths are warnings, not blockers. Pi does not require the name to match the installed directory.

#### Scenario: Claude Code has frontmatter without a description

- **WHEN** the Claude Code profile receives otherwise valid skill metadata without a description
- **THEN** diagnosis can recommend a description without refusing the lock for its absence

#### Scenario: OpenCode has a nonmatching installed name

- **WHEN** an OpenCode skill's frontmatter name and installed directory name do not match
- **THEN** daem checks the selected OpenCode policy rather than applying a more permissive profile from another target

### Requirement: Generate repair only for an opted-in lock

Daem SHALL generate repair only during lock generation, including prospective authoring locks, for a declaration with `compat_repair = true`. Preview can disclose a repair without publishing it. A group passes the policy to each child; the recipe belongs to that child's locked input/output. Doctor and other non-generating paths can diagnose mechanical versus manual repair but do not infer a new recipe for replay.

The [repair registry](../../../internal/supply/compat/skill/repair) owns the exact operations: the admitted `skill.md`/`SKILL.md` rename, exact recorded byte replacement, and exact edit of a string in existing frontmatter. Every operation needs identifiable before and after states. Missing descriptions cannot be generated; semantic, lossy, script, asset and reference changes remain manual. Name repair cannot guess an OpenCode install name that daem does not control. Diagnostics distinguish `repairability=mechanical`, with the opt-in next step, from `repairability=manual`, with the needed source edit.

#### Scenario: An opt-in has no fully determined repair

- **WHEN** the source needs an author-written description or other semantic edit to meet a selected profile
- **THEN** lock reports the manual blocker rather than writing generated prose or inventing a new recipe operation

### Requirement: Replay the stored transformation and verify its identities

The lockfile SHALL retain original exact source identity, ordered versioned recipe and digest, and exact repaired identity as separate facts. The [recipe codec](../../../internal/realization/lockfile/repair_conversion.go) owns wire fields, version, domain, accepted operations and byte encodings. Unknown or inconsistent records are rejected rather than repaired in memory. The transform and recipe refer to the same input and output, and the locked exact Supply is the output. Replaying or reversing an operation must check the recorded state at every step; operation order matters.

Repair replay checks exact normalized paths, file/directory kinds, bytes and recorded modes. It does not promise to preserve inode identity, links, ownership, timestamps, ACLs or other xattrs. `apply` installs the verified repaired artifact, and `status` evaluates that artifact rather than the unrepaired upstream bytes. Replay uses recorded source facts and no fresh inference from floating upstream content. Source integrity and the installed-content baseline are separate checks; an installed mode change does not by itself change its installed digest.

#### Scenario: Original bytes no longer match a stored recipe

- **WHEN** a locked repair's source identity or operation precondition fails verification
- **THEN** daem reports stale or corrupted evidence instead of trying a new repair or installing unverified bytes
