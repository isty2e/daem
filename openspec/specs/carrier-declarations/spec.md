# Extension carrier declarations

## Purpose

Declare exact host plugin or package relations. Declaration, lock, observation, and authority to act on a host store are separate.

## Requirements

### Requirement: Declare one host relation and admit scope explicitly

An extension SHALL have a stable ID and exactly one effective target matching its carrier. Claude Code and Codex use a marketplace selector; OpenCode, Pi and Antigravity CLI use the host-source form. Exact rows and source-kind pairings are owned by the [extension catalog](../../../internal/hostsurface/catalog/extension.go) and [lock profiles](../../../internal/realization/lock/delegated_relation_profiles.go). Claude Code, OpenCode and Pi have admitted project and explicit-global rows; Codex and Antigravity CLI require explicit global. Defaults do not authorize global carrier mutation. Claude Code public `global` lowers to host `user`, not a public alias; `local` is deferred. Pi direct extension resources are not the Pi package relation.

#### Scenario: Inherit a set of several targets

- **WHEN** an extension's effective target list contains several hosts
- **THEN** none of them is implicitly selected to give a single carrier broad authority

### Requirement: Preserve the selected source grammar and prevent credential-bearing arguments

A marketplace selector SHALL contain the admitted plugin and marketplace identity. A host-source operand is one structured argv element, not a command line. The [carrier grammar](../../../internal/desired/extension) admits one source kind, checks raw and bounded canonical forms, and rejects option-shaped arguments, passwords, HTTP userinfo, URL query fields, assignment-style fragments, inline credentials and raw host config. Punctuation such as `:` and `@` has the meaning of its selected grammar; an inert fragment such as `#v1` is not itself a credential.

Pi local identities are normalized lexically in the selected context (project-relative or global absolute), without requiring existence or resolving symlinks. Alias spelling does not prove exact external relation evidence. Registry package identities and npm aliases retain their outer name identity. Generic Git classification supports checkout and removal identity checks; it does not rewrite the locked argument into Pi package syntax. Ambiguous same-scope identities are refused.

#### Scenario: A Pi source is recognized as Git but uses a bare spelling

- **WHEN** a declared Pi source uses an advisory bare Git form such as `github:owner/repo`
- **THEN** add, status and apply give the corresponding nonblocking spelling diagnostic without echoing the source or testing Pi's parser
- **AND** the locked host argument is not silently converted and `lock`/`outdated` do not acquire a new warning or admission gate

### Requirement: Keep exact declaration separate from host invocation and authority

Manual and CLI-authored declarations SHALL use the same locked relation and route facts. `add extension` interprets one opaque source operand using the selected row, and may omit the target only if the selection is unambiguous. `remove extension` selects the declaration ID; an optional target or scope is a safety filter rather than an add default. These commands update declaration and lock, not a running host. Removing a declaration expresses desired absence. `unmanage` instead drops selected management while retaining host state.

Import can declare exact supported Codex, Claude Code, OpenCode and Pi source rows, preserving their source spelling and relative order without installing, locking or adopting. Antigravity CLI evidence without exact source yields a provenance skip. Provider-owned hooks, skills, MCP and other contributions do not become standalone declarations. [Carrier lifecycle](../../../docs/host-integrations.md) owns the exact evidence and authority for adoption, invocation, removal, refresh and deferred operations.

#### Scenario: Host inventory shows a plugin without its source

- **WHEN** Antigravity CLI lists an installed relation but supplies no exact source suitable for import
- **THEN** daem reports an import skip rather than declaring or claiming a source it cannot identify
