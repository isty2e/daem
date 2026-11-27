# Workspace selection

## Purpose

Define which workspace a command sees and which files and management roots follow from that selection. The [paths package](../../../internal/paths) owns address resolution. Host-specific paths belong to their placement contracts.

## Requirements

### Requirement: Select one manifest and its adjacent lockfile

`--manifest` SHALL be the only public workspace-path selector. A relative argument is relative to the working directory. Without it, an existing-workspace command checks `./daem.toml`, then the default user manifest, and never searches parents. `init` and non-merge `import` instead default to creating `./daem.toml`; `import --merge` uses existing-workspace selection. The lockfile is `daem.lock.toml` beside the selected manifest, with no independent selector.

Valid path bytes, including whitespace and control characters supported by the filesystem, are part of identity; display escaping does not change the selected path. NUL is not a path or argv byte. Existing manifest and lockfile links and directory aliases may be used where admitted. A permitted write preserves links and updates their regular-file referents, without switching the workspace for relative sources or default storage. A write does not automatically relocate or make a read-only target writable.

#### Scenario: A parent directory contains a manifest

- **WHEN** an existing-workspace command runs without `--manifest` in a subdirectory with no `daem.toml`
- **THEN** it looks for the default user manifest rather than selecting the parent workspace

#### Scenario: A manifest links to a regular file elsewhere

- **WHEN** daem writes an admitted manifest link and resolves project-relative inputs for that workspace
- **THEN** the link is kept and its file is updated, while project-relative lookup still uses the selected workspace

### Requirement: Separate implicit user scope from storage identity

Implicit fallback to the user manifest SHALL refuse project-scoped placements. An explicit or current-directory selection uses its normal placement rules even when it identifies the same user manifest. Resolving to shared user storage is not itself permission to manage a project destination.

#### Scenario: Use the user manifest by fallback rather than explicitly

- **WHEN** an implicit user selection contains a project-scoped skill
- **THEN** daem reports the placement problem and directs the user to select a project manifest or use global scope

### Requirement: Locate management storage without inventing a second authority

For supported Unix targets, applicable nonempty XDG root values SHALL be absolute. Daem appends `daem` to the selected root. `XDG_CONFIG_HOME` selects the default user manifest and its adjacent lock; `XDG_STATE_HOME` and `XDG_CACHE_HOME` select state/recovery and source reuse for that manifest, regardless of selection mode. `XDG_DATA_HOME` selects the shared ownership and carrier registries for all workspaces. With unset or empty variables, the admitted Unix defaults are `~/.config`, `~/.local/state`, `~/.cache`, and `~/.local/share`, respectively.

Other workspaces use local `.daem` state and recovery, and `.daem/cache` for sources. Changing `XDG_STATE_HOME` or `XDG_CACHE_HOME` does not move their files. Directory aliases of the default manifest entry share its management roots; distinct hardlink or final symlink entries do not become that entry. User-address identity lookup is relevant for the default name or a spelling that could identify it under case folding. When that address cannot be formed, explicit, cwd and creation selection can use project-local storage, but fallback fails. When a valid address exists but the required identity check fails, daem refuses rather than guessing which storage owns the workspace.

Changing a root selects a namespace; it does not transfer the old state, registries, or authority. Use [state migration](../../../docs/state-and-recovery.md#user-state-authority-migration) for the supported legacy user-state transfer. The cache supplies no ownership evidence. Host-specific environment variables affect only their admitted host paths, not daem's own roots; OpenCode's `OPENCODE_CONFIG` is not a daem file selector.

#### Scenario: Select the same default user entry explicitly

- **WHEN** an explicit selection names the same default user manifest entry as cwd or fallback selection with the same HOME and XDG roots
- **THEN** daem selects the same state, recovery and source cache locations

#### Scenario: User address is valid but its identity cannot be resolved

- **WHEN** an explicit default-named manifest could be an alias of the default user entry and an inaccessible directory prevents the required observation
- **THEN** daem does not silently create a parallel local state namespace
