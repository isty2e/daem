# Instruction files

## Purpose

Define the source and placement of a managed instruction file, not which instructions the host loads or follows.

## Requirements

### Requirement: Select a regular instruction file

An instruction declaration SHALL use a local regular file, an explicit Git ref and repository-relative regular file, or an S3 file object. A plain string selects a local source; a structured local source uses `vendor`. A directory, symlink entry at the locked Git path, or S3 archive format is not an instruction file. When scope is omitted, `[instructions.project]` selects project, `[instructions.global]` selects global, and another name uses `defaults.scope`.

#### Scenario: Declare global instructions with project defaults

- **WHEN** an `[instructions.global]` resource omits scope and the workspace default is project
- **THEN** it still selects a global instruction placement and requires a valid global source

### Requirement: Render only a supported placement

Daem SHALL resolve each selected target through the [instruction placement rows](../../../internal/realization/profile/instruction_profile_rows.go). A target override must name a target in the declaration. `render_to` chooses a supported canonical relative output at that scope, not an arbitrary path or a discovery-only location. Even at global scope it is relative to the host's instruction root. Copy is the supported render action; `symlink` is recognized syntax but cannot proceed to an executable apply plan or host mutation.

The locked instruction is non-executable and uses the selected deterministic file materialization. Initial write and update follow the existing managed-file rules: content determines drift and an update preserves current permissions. No host reload or effective-memory guarantee follows from placing the file.

#### Scenario: Select an alternate Antigravity project name

- **WHEN** a project declares Antigravity CLI instructions with `render_to = "GEMINI.md"`
- **THEN** it selects the admitted alternate instead of the default `AGENTS.md`
