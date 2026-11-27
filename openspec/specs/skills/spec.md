# Skills and skill groups

## Purpose

Define directory-skill declarations, placement selection and the membership of a locked group. [Skill compatibility](../skill-compatibility/spec.md) governs the content and optional repair. [Managed output](../managed-output/spec.md) defines installation reconciliation and rollback for these trees.

## Requirements

### Requirement: Distinguish a skill's identity from its installed name

A skill SHALL have a `name` that is one safe directory component: not empty, `.`, `..`, `~`-prefixed, or containing `/` or `\`. An optional `id` is a safe resource identifier; otherwise `name` is the ID. IDs must be unique across normalized skills, including group children. Equal install names are allowed when IDs and target/scope destinations do not conflict. The [skill model](../../../internal/desired/skill) enforces the identifier grammar.

Each skill requires a structured local, Git, or S3 archive source (`tar` or `tar.gz`, not an S3 regular-file object). Lock covers the entire selected directory, not just `SKILL.md`. For a Git source with `path = "."`, that directory is the repository root. `portable` defaults to true; project-scoped local link sources require an explicit false. `compat_repair` defaults to false.

#### Scenario: The same name is used by separate targets

- **WHEN** two declarations use the same installed name, distinct IDs, and different, nonoverlapping destinations
- **THEN** they remain separately addressable by ID

### Requirement: Choose a cataloged write root

Daem SHALL place a skill as one direct child of the selected write root, named by `name`. The [skill profile](../../../internal/realization/profile/skill_profile_rows.go) defines default and optional roots for each target and scope. An `install_to` override must name one of those roots for a target in the declaration. Project values are canonical project-relative slash paths; global values are home-relative `~/` paths, not arbitrary absolute or escaping destinations. Discovery and runtime locations do not grant placement permission.

The `install_mode` declaration recognizes `copy`, `symlink`, and `hardlink`; only the copy form is executable, as specified in [Manifest](../manifest/spec.md#requirement-resolve-defaults-without-broadening-a-declaration).

| Target | Project default | Project alternatives | Global default | Global alternatives |
| --- | --- | --- | --- | --- |
| `codex` | `.agents/skills` | — | `~/.agents/skills` | `~/.codex/skills` |
| `claude-code` | `.claude/skills` | — | `~/.claude/skills` | — |
| `opencode` | `.opencode/skills` | `.agents/skills`, `.claude/skills` | `~/.config/opencode/skills` | `~/.agents/skills`, `~/.claude/skills` |
| `pi` | `.pi/skills` | `.agents/skills` | `~/.pi/agent/skills` | `~/.agents/skills` |
| `antigravity-cli` | `.agents/skills` | — | `~/.gemini/config/skills` | — |

#### Scenario: Select a compatible root for one target

- **WHEN** a Pi declaration chooses its admitted project `.agents/skills` root
- **THEN** the Pi projection uses that root and appends the skill name, without changing other target selections

### Requirement: Preserve permissions for copied skill entries that persist

For a newly copied skill, daem SHALL create regular files with mode `0600` or `0700` when executable in the locked source; new directories use source directory permissions. An update preserves current permissions on entries shipped at the same path and kind in both versions. A newly shipped entry uses its installation mode even if a local extra used the same path. A change only to source executable class does not chmod an existing installation.

This is distinct from the [managed-output rules](../managed-output/spec.md#requirement-check-shipped-skill-membership-without-claiming-the-extra-files) for baseline comparison, destructive replacement and rollback.

#### Scenario: A replacement ships a file that had been local only

- **WHEN** a new skill version ships a regular file at an extra file's path
- **THEN** daem does not inherit that extra file's permissions for the newly shipped entry

### Requirement: Expand explicit groups as ordinary skills

A group SHALL use a Git or local directory root and exactly one of nonempty `names` or nonempty `include`; `exclude` is available only with `include`. S3 is not a group source. Explicit names identify direct children and normalize to ordinary skills, with IDs equal to the names. Children inherit group targets, scope, placement, portability, and repair policy. Exact duplicate IDs or overlapping installations are rejected rather than chosen by declaration order. `add skill-group` creates explicit groups from `--member`; selector groups are hand-authored.

#### Scenario: An explicit group and a direct skill claim the same ID

- **WHEN** both normalize to the same skill ID
- **THEN** daem refuses the conflict rather than locking one of them

### Requirement: Resolve selector-backed membership at lock comparison

`lock` and `outdated` SHALL list direct child directories of the selected roots and apply the [selector grammar](../../../internal/desired/skill/selector.go). `glob:` uses Go's `path.Match` including character classes and brackets for literal metacharacters; `/` and `\` are not admitted in glob patterns. `regex:` uses Go regular expressions, with anchoring available for full-name matches. Selectors address names, not paths; `name:` and `[[skill_set]]` are not alternate public forms. During lock generation, every include must match and the final set after exclusion must be nonempty; excludes may match nothing. Each selected child is validated as a skill. Results are deterministic by resource name and source identity.

Each child's locked provenance is correlated with the group declaration, not its array position. `apply` and `status` use that recorded membership and require the declaration identity to still match. They do not relist roots for new children. Changing a selector that owns locked membership can make the lock stale; normal removal follows relocking and state-owned reconciliation. `doctor` shares one local listing view for its checks and does not expand remote groups.

#### Scenario: An upstream child starts matching after the lock

- **WHEN** a new skill directory appears below a selector root after lock generation
- **THEN** ordinary status and apply do not add it, while a new lock comparison can see it

### Requirement: Bound group expansion without accepting a partial selection

Daem SHALL enforce the [listing](../../../internal/supply/source/root_listing_budget.go) and [expansion](../../../internal/desired/skill/expansion_budget.go) budgets for one complete phase across Git and local groups. Canonical roots count once; all declarations and observed direct entries count, including files and links that cannot be selected. An earlier selection is not refunded by later exclusion. Matching observes cancellation. The bounds are policy, not manifest fields:

| Resource | Limit |
| --- | ---: |
| Group declarations and distinct listed roots, separately | 1,024 each |
| Direct entries | 100,000 |
| Direct-entry names in aggregate; one name | 32 MiB; 4 KiB |
| Selectors; pattern data per group | 128; 64 KiB |
| Match evaluations | 1,000,000 |
| Matcher work, `pattern bytes × (name bytes + 1)` per evaluation | 134,217,728 units |
| Newly selected skills | 4,096 |

Exact limits pass. Exceeding a bound fails the lock without a partial selection. A local doctor overflow reports the omitted selector-expanded checks and can still check direct skills.

#### Scenario: Two individually small groups exceed their combined budget

- **WHEN** their combined phase exceeds an admitted limit
- **THEN** lock generation fails as a whole; a diagnostic run does not present unchecked children as checked

### Requirement: Import only admitted skill evidence

Skill import SHALL limit its search to modeled importable discovery and placement roots. It does not claim supplied, system, plugin, admin, or runtime roots. An admitted symlinked skill can be vendored as a local source without making its live destination managed. Same-name declarations from different targets merge only with identical content and retained source freshness for each contributor; otherwise they retain distinct IDs. Compatible names with no explicit IDs may share an explicit group when their normalized target and scope facts agree.

#### Scenario: Two targets expose different contents under one name

- **WHEN** an import observes different skill trees with the same install name
- **THEN** it does not merge them as one multi-target declaration
