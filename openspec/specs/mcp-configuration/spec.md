# MCP configuration

## Purpose

Declare and reconcile an admitted stdio server binding. This is not a package installer or a statement that an agent has started or can use a server. [Pi MCP](../pi-mcp/spec.md) defines Pi's two distinct contracts; explicit runtime probes are a separate operation.

## Requirements

### Requirement: Select a single admitted stdio binding

A declaration SHALL identify one server, one effective target and an admitted scope, using `transport = "stdio"`. Mutation of global MCP config requires `scope = "global"` on that declaration, not inherited global defaults. Scope and target determine the supported [MCP placement and codec](../../../internal/realization/aggregate/codec/mcp). Remote, bundled, HTTP/OAuth and arbitrary custom file authority do not follow from declaring a server.

A [portable command](../../../internal/declaration/mcp_command.go) is one executable token; an explicit absolute-path object preserves a canonical literal `argv[0]` without shell interpretation or symlink resolution at lock. Args preserve order and duplicates. The add helper admits the portable form only; exact path declarations and supported import can use the object. Neither form provisions or claims the executable.

#### Scenario: Inherit a global default

- **WHEN** the manifest defaults to global but an MCP declaration does not explicitly select global
- **THEN** it cannot authorize a user-level MCP configuration write

### Requirement: Preserve the selected host binding and environment grammar

Daem SHALL write only the exact selected config contribution, preserving unrelated content. The codec owns the accepted shapes. Codex project uses command and args; Codex global admits same-name `env_vars` with canonical sorted names. Claude Code project and global admit aliases, with exact `${SOURCE}` in the global file. OpenCode project admits no env; global strict JSON uses exact `{env:SOURCE}` aliases. Antigravity CLI admits global same-name ambient requirements, records those names only, and writes no native `env`. Pi uses its recorded backend's accepted aliases and file shapes.

Unrelated settings and same-name definitions outside the selected authority are not managed by matching a name. Codex TOML preserves unmanaged values and comments, including comments within a removed entry; a comments-only file can remain after the last server is removed. JSON-family edits use the shared [document rules](../agent-configuration/spec.md#requirement-limit-structured-editing-to-the-selected-values).

#### Scenario: A global OpenCode binding has an environment alias

- **WHEN** a declaration maps `CHILD` to environment source `SOURCE`
- **THEN** the selected global entry contains `{env:SOURCE}`, not the value from daem's process

### Requirement: Keep inspection and import inside admitted configuration sources

Import SHALL preserve accepted absolute command paths and environment references for admitted non-Pi core-native rows. Unsupported and credential-bearing entries are skipped without creating an executable source or mutating host config. Antigravity import does not infer ambient names; Pi import is unsupported. Import does not execute a server, verify readiness, or flatten provider contributions.

For OpenCode MCP, the selected primary is strict JSON. An existing cataloged JSONC alternate can block importing that primary; an alternate's absence is observed and its later appearance invalidates that source read without inspecting the new file. This evidence and the selected primary's identity/content remain checked up to publication, including merge no-ops. An env-selected custom file is not inferred as another admission row.

#### Scenario: A JSONC alternate appears after the preview

- **WHEN** an OpenCode MCP import relied on the absence of that alternate and it appears before publication
- **THEN** the import no longer uses the preview's source evidence, even if the manifest would otherwise be unchanged

### Requirement: Check selected environment names without storing values

Lock and dry-run SHALL validate symbolic references and admitted mappings without requiring live environment values. A normal apply checks that every required source name of each selected desired binding is present, before mutation authority and again after its under-lease plan rebuild. An empty value is present. This check includes already-current selected bindings, not declarations that have been removed. Diagnostics list bounded, sorted, deduplicated source names without values.

Environment values are runtime-only, not lock, state, recovery, rendered config, fingerprint or output data. Claude project values are resolved for delegated launch; a host launching from a direct config resolves later. Antigravity inherits the invoking host's environment; observing daem's environment does not establish that of a future host process. Doctor can inspect executable and variable prerequisites without invoking the binding.

#### Scenario: A current selected binding lacks its named source

- **WHEN** an already-current binding requires an environment source that is absent during normal apply
- **THEN** apply reports the missing source even though it would not otherwise rewrite the binding

### Requirement: Distinguish direct package evidence from reproducibility

For recognized launch argv, daem SHALL classify direct package inputs through the supported ecosystem grammar: complete npm SemVer, validated exact PEP 440 versions, or canonical container SHA-256 digests can establish a pin. Tags and ranges do not. The lock records only what can be derived; opaque or external requirements may leave a partial set and cannot establish pinned assurance. All modeled auxiliary package inputs count. Unknown or floating inputs do not rewrite or refuse otherwise valid argv. A recognized option missing its required argument is malformed, not an opaque selector.

Pin evidence is about the direct input only, not transitives, installed versions, package/cache ownership or runtime readiness. This classification does not install or probe packages. Explicit runtime probing is admitted for the cataloged locked project routes (Claude Code and OpenCode); config support alone does not add a probe route or persistence effects.

#### Scenario: One of several direct runner inputs is floating

- **WHEN** the executable's main package names an exact version but a recognized auxiliary package uses a range
- **THEN** the recorded classification does not describe the launch as pinned
