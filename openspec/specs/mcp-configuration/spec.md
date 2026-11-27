# MCP configuration

## Purpose

Declare and reconcile an admitted stdio server binding. This is not a package installer or a statement that an agent has started or can use a server. [Pi MCP](../pi-mcp/spec.md) defines Pi's two distinct contracts; explicit runtime probes are a separate operation.

## Requirements

### Requirement: Select a single admitted stdio binding

A declaration SHALL identify one server, one effective target and an admitted scope, using `transport = "stdio"`. Mutation of global MCP config requires `scope = "global"` on that declaration, not inherited global defaults. Scope and target determine the supported [MCP placement and codec](../../../internal/realization/aggregate/codec/mcp). Remote, bundled, HTTP/OAuth and arbitrary custom file authority do not follow from declaring a server.

A [portable command](../../../internal/declaration/mcp_command.go) is one executable token. The exact object form is `command = { path = "/absolute/path" }`; it preserves a lexically canonical native `argv[0]` without shell interpretation or symlink resolution at lock. Relative paths, traversal, surrounding whitespace, control or bidi formatting characters, `local_parameter`, extra keys and a redundant `portable` flag are not part of this form. Args preserve order and duplicates. The add helper admits the portable form only; declarations and supported import can use the object. Neither provisions or claims the executable.

#### Scenario: Inherit a global default

- **WHEN** the manifest defaults to global but an MCP declaration does not explicitly select global
- **THEN** it cannot authorize a user-level MCP configuration write

### Requirement: Preserve the selected host binding and environment grammar

Daem SHALL write only the exact selected config contribution, preserving unrelated content. Manifest aliases use `{ from_env = "SOURCE" }`, not literal values. The codec implements the following shape contracts:

| Host | Selected configuration | Environment form |
| --- | --- | --- |
| Codex | Project `.codex/config.toml` or admitted user config, `mcp_servers.<name>` | No project env. Global same-name `env_vars`, sorted and deduplicated; the declared native string and supported `{ name, source = "local" }` forms import equivalently. |
| Claude Code | Project `.mcp.json` or global top-level `~/.claude.json` `mcpServers` | Aliases; global values must be exact `${SOURCE}`, not literals, default expansion or compound interpolation. |
| OpenCode | Project or default user `opencode.json`, strict JSON `/mcp/<name>` with local type | No project env. Global `environment` values must be exact `{env:SOURCE}`, not shell, file or compound interpolation. |
| Pi | Project `.pi/mcp.json` or user `mcp.json` at the Pi agent root | The recorded Native or Adapter codec and its aliases. |
| Antigravity CLI | Global `~/.gemini/config/mcp_config.json` only | Same-name ambient names recorded in the lock. No native `env`, including interpolated forms, and no aliases. |

Known or unowned sibling auth/session/trust fields are not owned by naming a server. In a managed entry the host-specific accepted shapes do not admit arbitrary remote URLs, OAuth, headers, `cwd`, timeout, enablement, or tool-policy keys. Pi's supported enablement/default fields are defined separately.

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

For recognized launch argv, daem SHALL classify direct package inputs through the supported ecosystem grammar: complete npm SemVer, validated exact PEP 440 versions, or canonical lower-case `sha256:` with 64 hex digits can establish a pin. Tags, partial versions, ranges, arbitrary equality and wildcards are not exact pins. Lock and JSON plan preserve only derivable inputs; `add mcp-server` warns on floating resolution. All modeled auxiliary inputs count, including repeated npm `--package` and uvx `--with`. Opaque syntax, requirements files, unrepresentable uvx extras and direct URLs do not establish pinned assurance. They neither rewrite nor refuse otherwise valid argv. An option that needs a separate value rejects absence, an empty argument or `--` as that value.

Pin evidence is about the direct input only, not transitives, installed versions, package/cache ownership or runtime readiness. This classification does not install or probe packages. Explicit runtime probing is admitted for the cataloged locked project routes (Claude Code and OpenCode); config support alone does not add a probe route or persistence effects.

#### Scenario: One of several direct runner inputs is floating

- **WHEN** the executable's main package names an exact version but a recognized auxiliary package uses a range
- **THEN** the recorded classification does not describe the launch as pinned
