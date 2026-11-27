# Agent configuration

## Purpose

Define native configuration declaration, projection, and editing semantics. Consent, ownership transitions, settlement with delegated host commands, and recovery follow the native document lifecycle. Config convergence is not proof that the agent has loaded the settings.

## Requirements

### Requirement: Identify a native document and its grammar

A declaration SHALL identify one effective target, one scope, a unique ID, a canonical slash-separated `path` relative to the selected configuration root, and a supported regular-file source. Absolute paths, `..`, `.`, empty components, and backslashes are rejected. Local sources use vendor mode; Git and S3 sources use file forms. The [profile](../../../internal/realization/profile/agent_config.go) and [native paths](../../../internal/output/hostpath/native) implement these roots:

| Target | Project root | Global root |
| --- | --- | --- |
| `pi` | `.pi` | `PI_CODING_AGENT_DIR`, default `~/.pi/agent` |
| `codex` | `.codex` | `CODEX_HOME`, default `~/.codex` |
| `claude-code` | `.claude` | `CLAUDE_CONFIG_DIR`, default `~/.claude` |
| `opencode` | Manifest directory | `opencode` under `XDG_CONFIG_HOME`, default `~/.config/opencode` |
| `antigravity-cli` | Unsupported | `~/.gemini/antigravity-cli` |

OpenCode project paths must be under `.opencode/` or name a root `opencode.json`, `opencode.jsonc`, `tui.json`, or `tui.jsonc`. The selected root itself cannot be changed by `path`.

Codex `config.toml` and global `<name>.config.toml` use TOML. Pi `settings.json` is strict JSON; global `models.json` permits line comments. Other admitted paths infer grammar from `.json`, `.jsonc` or `.toml`, or use explicit `syntax` of `json`, `jsonc`, `json-line-comments` or `toml`. A selector cannot override a known file's required grammar or promise validity under the agent's own settings schema. Preview does not create missing directories; confirmed apply can create them, and empty scaffolding can remain after removal or rollback.

#### Scenario: An explicit syntax disagrees with the known file

- **WHEN** a declaration tries to interpret known Pi `settings.json` using a permissive JSONC reader
- **THEN** daem refuses the override rather than writing a shape the known file does not admit

### Requirement: Own values without claiming their surrounding document

The default `ownership = "keys"` SHALL select each leaf of a nonempty table/object and keep scalars, nulls, arrays and explicit empty objects atomic. Disjoint declarations may share a document. Physical aliases do not bypass the nonoverlap check, including against an out-of-selection owner. Shared setting files cannot be declared as whole-document projections. Dedicated `ownership = "document"` selects the entire admitted file; it is not a way to acquire shared config.

Reserved native configuration belongs to its dedicated surface. This includes Codex `mcp_servers`, `hooks`, `plugins` and `marketplaces`; Claude Code `hooks` and `enabledPlugins`; OpenCode `plugin`; and Pi `packages`, `extensions` and `skills`. Host-owned authentication, runtime/cache, package code and loading controls are outside this ownership. Absence from the reserved sets does not make a key safe for credentials in source or lock files.

#### Scenario: An array is selected as a setting

- **WHEN** the source supplies an array under a managed key
- **THEN** the array is one owned value; declaration selection cannot acquire one positional element as an independent key

### Requirement: Limit structured editing to the selected values

Daem SHALL preserve unselected content and skip unchanged assignments, even when another assignment changes. The [document codec](../../../internal/encoding/configdocument) determines local JSON-family layout and TOML representation. New JSON objects use the containing style when no local evidence exists, with a two-space multiline root default. Nonempty objects retain ordering/layout of unedited keys and their trailing-comma preference. Empty scope can lose the layout evidence of removed children; a later edit need not recreate their original whitespace or remove generated parent structure.

Reserializing a changed whole value is not a byte-preservation promise for that value. Same-path object layout can carry forward, whereas array content is compact. Comments are retained but may move; retained CRLF line-comment endings survive, lone CR can normalize on edit, and an edited document can gain a final LF. Recovery does not itself imply formatting undo.

A new shared or dedicated config file uses `0600`. Editing or removing a managed hook, MCP binding or key in an existing shared file preserves that file's current permissions. A dedicated whole-file update likewise preserves the current file mode; a chmod alone does not make its contents drift.

#### Scenario: Edit a key beside an unchanged key

- **WHEN** one managed value changes and a sibling assignment already matches
- **THEN** the sibling is not rewritten merely because the containing object needs an edit
