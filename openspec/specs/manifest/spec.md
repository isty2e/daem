# Manifest

## Purpose

Define how daem reads a declaration and resolves its defaults. The [TOML decoder](../../../internal/declaration/toml_decode.go) and [manifest loading](../../../internal/declaration/manifest) implement those checks. Resource-specific capabilities give the admitted forms; the [Manifest Reference](../../../docs/manifest.md) is the reference for looking up fields and examples.

## Requirements

### Requirement: Read a versioned declaration

Daem SHALL accept integer `version = 1` as the manifest schema, not a daem release version. It SHALL reject unknown fields, invalid TOML types, and declarations that cannot form a valid resource. It reads a [bounded, stable regular-file referent](../../../internal/declarationartifact/artifact.go) and applies the decoder's TOML structure bounds. Recognizing a resource or target name does not imply that every operation on it is supported.

#### Scenario: An editor accepts a non-integer version

- **WHEN** a manifest has `version = 1.0`
- **THEN** daem rejects it even if an editor's schema check accepts the value

#### Scenario: A command refers to an unimplemented declaration family

- **WHEN** a manifest uses an executable-lifecycle declaration such as `[[package_runner]]`
- **THEN** daem rejects the field rather than interpreting it as an installation request

### Requirement: Resolve defaults without broadening a declaration

The manifest SHALL define a nonempty default target set. A resource that accepts `targets` inherits that set when its value is omitted or empty; a nonempty resource set replaces it. The authored target values must be supported and nonduplicated. Hook assets have no target field; selection comes from their consumers.

When omitted or empty, scope defaults to `project` and skill install mode to `copy`, except where inherited or specialized defaults apply. For example, `[instructions.global]` defaults to global even in a project-default manifest. The manifest recognizes `copy`, `symlink`, and `hardlink` for skills. A selected non-copy placement is refused before an executable dry-run plan or host mutation.

#### Scenario: Select one target at execution

- **WHEN** a resource inherits `codex` and `pi` and the caller selects `pi`
- **THEN** the operation considers the Pi placement without changing the declaration or adding targets to other resources

#### Scenario: Decode an unsupported skill install mode

- **WHEN** a skill selects a recognized `symlink` install mode
- **THEN** parsing may succeed, but preview or apply cannot proceed with that placement

### Requirement: Keep editor validation advisory

Daem SHALL determine admission from runtime decoding and the operation's checks, not editor validation. The optional [editor schema](../../../schemas/daem.schema.json) currently describes structural syntax in a portable Draft 4 subset. A matching schema can assist editing; daem does not configure or check the association. A schema pass says nothing about source availability, lock freshness, mutation authority or runtime readiness.

#### Scenario: A resource passes a structural check but has an invalid reference

- **WHEN** editor validation does not catch an invalid target-specific placement or cross-resource reference
- **THEN** the normal daem command still checks that placement or reference before relying on it
