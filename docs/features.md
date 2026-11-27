# Feature Support

This page shows what daem can currently manage for each supported agent CLI.
For manifest syntax, see the [Manifest Reference](manifest.md). For exact
native commands and safety limits, see the
[host integrations guide](host-integrations.md).

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
| Agent configuration | Project + global | Project + global | Project + global | Project + global | Global only |
| MCP startup test | No | Project only | Project only | No | No |
| Run MCP on apply | No | Project only | No | No | No |

Use [`[[agent_config]]`](manifest.md#agent-configuration) to manage selected
keys in a shared settings file or a dedicated file in full. The manifest
reference explains [how to switch between these modes](manifest.md#whole-files-and-ownership-changes).
To start from settings already on disk, use [`daem import agent-config`](cli.md#import-agent-config).

Use `daem list paths` to check the install location before choosing a skill
root. MCP configuration does not install an executable; check the
[Pi backend guide](host-integrations.md#pi-native-and-adapter) before switching
an existing Pi binding.

## Plugins And Extensions

Daem calls these resources `extension` entries in the manifest. Depending on
the host, the native object may be called a plugin, package, or extension.

| Action | Codex | Claude Code | OpenCode | Pi | Antigravity CLI |
| --- | --- | --- | --- | --- | --- |
| Declare | Global only | Project + global | Project + global | Project + global | Global only |
| Detect installed | Global only | Project + global | Project + global | Project + global | Limited |
| Install | Global only | Project + global | Project + global | Project + global | Global only |
| Explicit refresh | Global only | Project + global | Project + global | Project + global | Global only |
| Remove managed | Global only | Project + global | Project + global | Project + global | Limited |
| Adopt existing | Global only | Project + global | Project + global | Project + global | No |
| Import installed | Global only | Project + global | Project + global | Project + global | Diagnostic only |
| List bundled features | Report only | No | No | No | No |
| Delete leftover data | No | No | No | No | No |

Antigravity covers the CLI, not the IDE. Its limited detection and removal
require a `PLUGIN@MARKETPLACE` source. Before confirming native effects, review
[refresh scope](host-integrations.md#explicit-carrier-refresh),
[what removal leaves behind](host-integrations.md#managed-carrier-absence), and
[what happens when a source changes](host-integrations.md#managed-source-handoffs).
Use `unmanage extension` if the installation should stay in place.

## Detailed References

- [Getting Started](getting-started.md) for a first project.
- [CLI Reference](cli.md) for command and flag behavior.
- [Manifest Reference](manifest.md) for resource schemas and source forms.
- [Host integrations](host-integrations.md) for host-specific examples, checked
  versions and links to the route specifications.
- [Platform Support](platforms.md) for operating-system and architecture
  coverage.
