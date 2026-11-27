# Manifest

## Purpose

Define how daem reads a declaration and resolves its defaults. The [TOML decoder](../../../internal/declaration/toml_decode.go) owns exact fields and types; [manifest loading](../../../internal/declaration/manifest) owns normalization and placement validation. The [Manifest Reference](../../../docs/manifest.md) provides examples and field lookup.

## Requirements

### Requirement: Read a versioned declaration

Daem SHALL accept the supported integer manifest version defined in [contractversion](../../../internal/contractversion/current.go). It SHALL reject unknown fields, invalid TOML types, and declarations that cannot form a valid resource. Recognizing a resource or target name does not imply that every operation on it is supported.

#### Scenario: An editor accepts a non-integer version

- **WHEN** a manifest has `version = 1.0`
- **THEN** daem rejects it even if an editor's schema check accepts the value

#### Scenario: A command refers to an unimplemented declaration family

- **WHEN** a manifest uses an executable-lifecycle declaration such as `[[package_runner]]`
- **THEN** daem rejects the field rather than interpreting it as an installation request

### Requirement: Resolve defaults without broadening a declaration

The manifest SHALL define a nonempty default target set. A resource without `targets` inherits it; an explicit set replaces it. Each declared set must use supported target identifiers without duplicates. Scope defaults to `project`, and skill install mode defaults to `copy`, unless changed at a level admitting an override. The manifest recognizes `copy`, `symlink`, and `hardlink`; recognizing a mode is not permission to apply it. Copy is the supported mutating skill placement.

#### Scenario: Select one target at execution

- **WHEN** a resource inherits `codex` and `pi` and the caller selects `pi`
- **THEN** the operation considers the Pi placement without changing the declaration or adding targets to other resources

#### Scenario: Decode an unsupported skill install mode

- **WHEN** a skill selects a recognized `symlink` install mode
- **THEN** daem does not treat that mode as an approved mutating placement

### Requirement: Keep editor validation advisory

The optional [editor schema](../../../schemas/daem.schema.json) SHALL describe the manifest's structural syntax using its portable Draft 4 subset. The runtime decoder and the operation's validation, not the editor, determine admission. A schema pass does not establish source availability, lock freshness, permission to mutate, or host readiness. The schema can be associated with the matching release or source revision; daem does not configure or check the editor's association.

#### Scenario: A resource passes a structural check but has an invalid reference

- **WHEN** editor validation does not catch an invalid target-specific placement or cross-resource reference
- **THEN** the normal daem command still checks that placement or reference before relying on it
