# Feature Support

This page shows what daem can currently manage for each supported agent CLI.
For manifest syntax, see the [Manifest Reference](manifest.md). For exact
native commands and safety limits, see the
[Host Integration Contract](host-integrations.md).

## Reading The Tables

`Report only` means diagnostics without host changes. `Limited` means a source
or observation restriction explained below; scope labels apply only to the
named project/global row.

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

Skills use the default root or a cataloged `install_to` alternative. Inspect
locations with `daem list paths`. Same-name skills in other modeled discovery
roots produce warnings in doctor/status/apply, not automatic deletion.

MCP entries configure a server, not install its executable. Pi additionally
requires the admitted `pi-mcp-adapter` package; `add mcp-server --target pi`
authors both declarations when needed, and apply installs the provider before
config. Provider/config convergence does not prove trust, activation or runtime
readiness. [MCP Servers](manifest.md#mcp-servers) lists environment-reference
rules; values stay runtime-only. Only Claude Code project apply may run the
locked server. `probe mcp-server` is a separate explicit startup check for
Claude Code/OpenCode project entries.

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

Daem previews the locked host command/config change, obtains confirmation and
checks the available post-operation evidence. Command success alone is not
convergence.

Removing a declaration requests managed removal on the next confirmed apply;
it requires a daem-created or explicitly adopted installation and no remaining
daem-known shared consumer. `unmanage extension` instead leaves the host
installation in place. Removal is not general cleanup: see each host's
[retained effects](host-integrations.md#managed-carrier-absence).

Import preserves supported exact source spelling and relative order without
ownership or host changes. Antigravity inventory lacks recoverable source
provenance, so those rows are skipped. Eligible imported relations still need
`apply --manage-existing` to acquire management.

Pi package order controls runtime precedence; OpenCode arrays expose config
order only. Apply re-observes order after install/removal. New interactions with
unmanaged rows require renewed interactive confirmation; `--yes` stops.
Multi-sequence results may be partial, with fresh-state retry rather than rollback.

Antigravity covers the CLI, not the IDE. Its detection and managed removal
require a `PLUGIN@MARKETPLACE` source; other forms do not support those operations.

## Detailed References

- [Getting Started](getting-started.md) for a first project.
- [CLI Reference](cli.md) for command and flag behavior.
- [Manifest Reference](manifest.md) for resource schemas and source forms.
- [Host Integration Contract](host-integrations.md) for exact target routes,
  observation evidence, retained effects, and tested host versions.
- [Platform Support](platforms.md) for operating-system and architecture
  coverage.
