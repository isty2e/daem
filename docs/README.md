# User Documentation

This directory contains user-facing documentation for configuring and running
`daem` (Declarative Agent Environment Manager).

Start here:

- [Getting Started](getting-started.md): executable first-project and import
  paths through authoring, lock, status, apply, diagnosis, and recovery.
- [Feature Support](features.md): what users can currently manage for each
  agent CLI.
- [Host Integration Contract](host-integrations.md): exact native commands,
  evidence, retained effects, and safety limits for target integrations.
- [Platform Support](platforms.md): supported operating-system/architecture
  rows, verification lanes, and unsupported-build behavior.
- [Concepts](concepts.md): manifest, lockfile, statefile, targets, scopes,
  source types, managed ownership, and recovery model.
- [Glossary](glossary.md): short definitions and links for daem-specific terms.
- [CLI Reference](cli.md): command and flag contract.
- [Manifest Reference](manifest.md): `daem.toml` schema and examples.
- [Troubleshooting](troubleshooting.md): safe responses to common conflicts,
  drift, interrupted operations, and NFS caveats.
- [Skill Compatibility](compatibility.md): supported agent skill loading rules
  and diagnostics for Codex, Claude Code, OpenCode, Pi, and Antigravity CLI.
- [Example Manifest](../examples/daem.toml): minimal Codex and Claude Code
  project setup.
- [Representative Project](../examples/representative-project.toml):
  instruction, local skill, managed hook asset, hook, and MCP declarations in
  one lock-validated manifest.

These documents are self-contained for public commands, schema, product support,
and safety guarantees. Implementation and invariant-bearing tests enforce those
contracts; contributors should start with
[Contributing](../CONTRIBUTING.md).
