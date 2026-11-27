# Skills and skill groups

## Purpose

Define directory-skill declarations, placement selection and the membership of a locked group. [Skill compatibility](../skill-compatibility/spec.md) governs the content and optional repair. [Managed output and recovery](../../../docs/state-and-recovery.md) govern ownership of installed trees.

## Requirements

### Requirement: Distinguish a skill's identity from its installed name

A skill SHALL have a `name` that is one safe directory component. An optional `id` is a safe resource identifier; otherwise `name` is the ID. IDs must be unique across normalized skills, including group children. Equal install names are allowed when IDs and target/scope destinations do not conflict. The [skill model](../../../internal/desired/skill) owns the exact name and ID grammar.

Each skill requires a structured local, Git, or S3 archive source. A Git root is a valid source if the root itself is the skill artifact; all of that root, not just its document, is locked. `portable` defaults to true; project-scoped local link sources require an explicit false. `compat_repair` defaults to false.

#### Scenario: The same name is used by separate targets

- **WHEN** two declarations use the same installed name, distinct IDs, and different, nonoverlapping destinations
- **THEN** they remain separately addressable by ID

### Requirement: Choose a cataloged write root

Daem SHALL place a skill as one direct child of the selected write root, named by `name`. The [skill profile](../../../internal/realization/profile/skill_profile_rows.go) defines default and optional roots for each target and scope. An `install_to` override must name one of those roots for a target in the declaration. Project values are canonical project-relative slash paths; global values are home-relative `~/` paths, not arbitrary absolute or escaping destinations. Discovery and runtime locations do not grant placement permission.

#### Scenario: Select a compatible root for one target

- **WHEN** a Pi declaration chooses its admitted project `.agents/skills` root
- **THEN** the Pi projection uses that root and appends the skill name, without changing other target selections

### Requirement: Expand explicit groups as ordinary skills

A group SHALL use a Git or local directory root and exactly one of nonempty `names` or nonempty `include`; `exclude` is available only with `include`. S3 is not a group source. Explicit names identify direct children and normalize to ordinary skills, with IDs equal to the names. Children inherit group targets, scope, placement, portability, and repair policy. Exact duplicate IDs or overlapping installations are rejected rather than chosen by declaration order. `add skill-group` creates explicit groups from `--member`; selector groups are hand-authored.

#### Scenario: An explicit group and a direct skill claim the same ID

- **WHEN** both normalize to the same skill ID
- **THEN** daem refuses the conflict rather than locking one of them

### Requirement: Resolve selector-backed membership at lock comparison

`lock` and `outdated` SHALL list direct child directories of the selected roots and apply the [selector grammar](../../../internal/desired/skill/selector.go). `glob:` uses Go's `path.Match`; `regex:` uses Go regular expressions. Selectors address names, not paths; `name:` and `[[skill_set]]` are not alternate public forms. During lock generation, every include must match and the final set after exclusion must be nonempty; excludes may match nothing. Each selected child is validated as a skill. Results are deterministic by resource name and source identity.

Each child's locked provenance is correlated with the group declaration, not its array position. `apply` and `status` use that recorded membership and require the declaration identity to still match. They do not relist roots for new children. Changing a selector that owns locked membership can make the lock stale; normal removal follows relocking and state-owned reconciliation. `doctor` shares one local listing view for its checks and does not expand remote groups.

#### Scenario: An upstream child starts matching after the lock

- **WHEN** a new skill directory appears below a selector root after lock generation
- **THEN** ordinary status and apply do not add it, while a new lock comparison can see it

### Requirement: Bound group expansion without accepting a partial selection

Daem SHALL enforce the [listing](../../../internal/supply/source/root_listing_budget.go) and [expansion](../../../internal/desired/skill/expansion_budget.go) budgets across one phase, not separately for each group. Canonical roots count once; all declarations and observed direct entries count, including files and links that cannot be selected. Include/exclude work and selected children are charged before exceeding their limits; exclusion does not refund an earlier selection. Matching observes cancellation. Exceeding a bound fails the lock rather than publishing partial membership. A local doctor overflow reports the omitted selector-expanded checks and can still check direct skills.

#### Scenario: Two individually small groups exceed their combined budget

- **WHEN** their combined phase exceeds an admitted limit
- **THEN** lock generation fails as a whole; a diagnostic run does not present unchecked children as checked

### Requirement: Import only admitted skill evidence

Skill import SHALL limit its search to modeled importable discovery and placement roots. It does not claim supplied, system, plugin, admin, or runtime roots. An admitted symlinked skill can be vendored as a local source without making its live destination managed. Same-name declarations from different targets merge only with identical content and retained source freshness for each contributor; otherwise they retain distinct IDs. Compatible names with no explicit IDs may share an explicit group when their normalized target and scope facts agree.

#### Scenario: Two targets expose different contents under one name

- **WHEN** an import observes different skill trees with the same install name
- **THEN** it does not merge them as one multi-target declaration
