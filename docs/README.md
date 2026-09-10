# Documentation

## Set Up And Make Changes

- [Install, Upgrade, And Roll Back](install.md) — install the executable, set
  PATH, choose a version, or restore the previous binary.
- [Getting Started](getting-started.md) — create and apply one project
  instruction file from a local source.
- [Use An Existing Environment](migration.md) — import live configuration,
  declare local sources, and decide which matching outputs to register.
- [Use Daem From Your Agent](agent-skill.md) — install the daem skill.
- [Troubleshooting](troubleshooting.md) — respond to conflicts, drift, skipped
  imports, and interrupted operations.

## Understand The Model

- [Concepts](concepts.md) — how manifests, locks, sources, ownership, and apply
  fit together.
- [Glossary](glossary.md) — short definitions with links to the owning reference.

## Look Up A Contract

| Reference | Questions it answers |
| --- | --- |
| [CLI](cli.md) | Which commands and flags exist? What do output and exit codes mean? |
| [Manifest](manifest.md) | Which TOML fields, source forms, and resource settings are accepted? |
| [Feature Support](features.md) | What can daem manage for each host? |
| [Host Integrations](host-integrations.md) | Which native operations run, and what may they leave behind? |
| [Platform Support](platforms.md) | Which OS/architecture pairs are supported, and what runtime limits apply? |
| [Skill Compatibility](compatibility.md) | Which skill formats and loading locations does each host support? |
| [State And Recovery](state-and-recovery.md) | What do durable records authorize, and what can recovery do? |

The manifest, CLI, host, and platform references own their contracts. Feature
Support and the glossary summarize them; they do not add syntax or support.

## Examples

- [Minimal manifest](../examples/daem.toml).
- [Representative project](../examples/representative-project.toml) — local
  instructions, skill, hook asset, hook, and MCP declarations.
- [Skill placement](../examples/skill-placement.toml) — default and supported
  alternative roots.
- [Extension order](../examples/extension-order.toml).
- Pi MCP: [project](../examples/pi-project-mcp-stdio.toml) and
  [global](../examples/pi-global-mcp-stdio.toml) configurations using an explicit
  provider package.

## Contribute

Start with [Contributing](../CONTRIBUTING.md) for setup and verification,
[Architecture](../ARCHITECTURE.md) for implementation ownership, and the
[compiler implementation guide](architecture/compiler-migration.md) for current
boundaries and retained compatibility seams.
