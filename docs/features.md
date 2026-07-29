# Feature Support

This page shows what daem can currently manage for each supported agent CLI.
For manifest syntax, see the [Manifest Reference](manifest.md). For exact
native commands and safety limits, see the
[Host Integration Contract](host-integrations.md).

## Reading The Tables

- `Yes`: the feature is available for that target.
- `Project only`: only project-scoped configuration is supported.
- `Global only`: only global configuration is supported.
- `Project + global`: both scopes are supported.
- `Limited`: support has a source or observation restriction explained below.
- `Report only`: daem can inspect or diagnose the feature but cannot manage it.
- `No`: the feature is not currently supported.

## Files And Configuration

| Feature | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Instructions | Yes | Yes | Yes | Yes | Yes |
| Skills | Yes | Yes | Yes | Yes | Yes |
| Skill groups | Yes | Yes | Yes | Yes | Yes |
| Command hooks | Yes | Yes | Report only | Report only | No |
| MCP configuration | Project + global | Project + global | Project + global | Project + global | Global only |
| MCP startup test | No | Project only | Project only | No | No |
| Run MCP on apply | No | Project only | No | No | No |

`Report only` means that `doctor` can explain the available host feature or why
daem cannot manage it. It does not write host configuration.

Skill declarations use each target's default root unless a supported
target-specific `install_to` selects a compatible alternative. Run
`daem list paths` to see every modeled write, discovery, runtime, config,
private-store, and delegated-route location together with the manifest
selection. If the same skill name still exists at another modeled discovery
root, `doctor`, `status`, and `apply` warn without deleting it.

MCP configuration support manages the host's command and argument entry. Pi
support is provider-mediated: the manifest must also declare the admitted
`pi-mcp-adapter` package, and apply installs that package through Pi before
writing `.pi/mcp.json` or the selected Pi agent-root `mcp.json`. `add
mcp-server --target pi` authors both declarations when needed. Claude Code
project and global rows also support structured child-to-source environment
references, while Codex global rows support same-name references rendered as
native `env_vars`. OpenCode global rows support child-to-source aliases
rendered as exact `{env:SOURCE}` references. Antigravity CLI global rows
support same-name ambient requirements; daem locks the names and checks current
presence, but intentionally omits native `env` so the server inherits the
environment of the Antigravity CLI process. Environment values stay
runtime-only. Except for the separately declared Pi provider, daem does not
install the executable or package named by an MCP entry. Claude Code project
MCP is the only current row where confirmed `apply` may run the locked server
command. `probe mcp-server` is a separate, explicit startup check for Claude
Code and OpenCode project MCP entries. Pi provider installation and config
convergence do not prove project trust, provider activation, server
connectivity, authentication, or tool inventory.

## Plugins And Extensions

Daem calls these resources `extension` entries in the manifest. Depending on
the host, the native object may be called a plugin, package, or extension.

| Action | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Declare | Global only | Project + global | Project + global | Project + global | Global only |
| Detect installed | Global only | Project + global | Project + global | Project + global | Limited |
| Install | Global only | Project + global | Project + global | Project + global | Global only |
| Refresh one | Global only | Project + global | Project + global | Project + global | Global only |
| Remove managed | Global only | Project + global | Project + global | Project + global | Limited |
| Adopt existing | Global only | Project + global | Project + global | Project + global | No |
| Import installed | Global only | Project + global | Project + global | Project + global | Diagnostic only |
| List bundled features | Report only | No | No | No | No |
| Delete leftover data | No | No | No | No | No |

Install, refresh, and removal use the target CLI or its native configuration
format. Daem locks the selected operation, shows it in dry-run output, asks for
normal apply confirmation, and then checks the host state that the target makes
available. A successful host command is not treated as stronger proof than the
post-operation check provides.

Removing an extension declaration requests removal of that managed plugin or
package on the next confirmed apply. Daem removes it only when daem created the
installation or the user explicitly adopted an exact existing installation,
and no other daem workspace is a known consumer. `unmanage extension` instead
stops daem management while leaving the host installation in place.

Removal does not delete marketplaces, package caches, dependencies,
credentials, trust decisions, sessions, logs, or unrelated host data. Daem
does not currently offer a general cleanup or prune operation for those files.

`import` authors exact installed extension declarations for Codex global,
Claude Code project/global, OpenCode project/global, and Pi project/global
rows. It preserves the host-native source spelling and observed relative order
without granting ownership or changing the host. Antigravity CLI cannot recover
the exact marketplace/source from its installed inventory, so import reports
those rows as skipped instead of approximating them. A later
`apply --manage-existing` is still required to adopt an eligible exact
relation.

### Host Notes

- **Codex:** plugin management is global because the current Codex plugin CLI
  does not expose project-scoped installation. Codex can report selected
  features declared by configured plugin cache manifests, but it does not
  import them as standalone daem resources.
- **Claude Code:** marketplace plugins are supported for project and global
  scope. Daem's global scope maps to Claude Code's native user scope.
- **OpenCode:** plugin sources and standalone MCP configuration are supported
  for project and global scope.
- **Pi:** package-backed extensions are supported for project and global scope.
  MCP configuration is supported through the explicitly declared
  `pi-mcp-adapter` provider, not as a Pi core-native surface.
- **Antigravity CLI:** only the CLI is covered, not the IDE. Plugin and MCP
  management are global. Exact installed-state detection and managed removal
  require a `PLUGIN@MARKETPLACE`-shaped source; other plugin sources cannot be
  verified precisely enough for those operations.

## Typical Workflow

| Goal | Command |
| --- | --- |
| Create a manifest | `daem init` |
| Add desired state | `daem add ...` |
| Remove desired state | `daem remove ...` |
| Lock source identities | `daem lock` |
| Inspect pending work | `daem status` |
| Preview host changes | `daem apply --dry-run --diff` |
| Apply host changes | `daem apply` |
| Adopt an exact match | `daem apply --manage-existing` |
| Keep an extension | `daem unmanage extension <id>` |
| Refresh one extension | `daem refresh extension <id>` |
| Test MCP startup | `daem probe mcp-server <id>` |
| Diagnose configuration | `daem doctor` |
| Recover interrupted apply | `daem recover` |

`add` and `remove` update the manifest and lockfile together. They do not alter
agent configuration until `apply`. A project declaration never authorizes
deleting global state.

## Detailed References

- [Getting Started](getting-started.md) for a first project.
- [CLI Reference](cli.md) for command and flag behavior.
- [Manifest Reference](manifest.md) for resource schemas and source forms.
- [Host Integration Contract](host-integrations.md) for exact target routes,
  observation evidence, retained effects, and tested host versions.
- [Platform Support](platforms.md) for operating-system and architecture
  coverage.
