# daem

[![CI](https://github.com/isty2e/daem/actions/workflows/ci.yml/badge.svg)](https://github.com/isty2e/daem/actions/workflows/ci.yml)

`daem` is a Declarative Agent Environment Manager for people who use coding
agents across projects or hosts. Declare instructions, skills, hooks, MCP
configuration, and supported extensions in `daem.toml`, lock source-backed
content, and preview changes before applying them. Daem supports Codex,
Claude Code, OpenCode, Pi, and Antigravity CLI. Available resources and operations
differ by host; see [Feature Support](docs/features.md).

## Install

Install the latest stable binary for macOS 26 or newer on Apple silicon or
Linux on x86-64:

```sh
curl -fsSL https://raw.githubusercontent.com/isty2e/daem/main/install.sh -o daem-install.sh &&
  sh daem-install.sh
```

The installer verifies the selected [GitHub Release](https://github.com/isty2e/daem/releases)
and installs it at `~/.local/bin/daem`. It does not edit `PATH` or shell profiles.
See [Install, Upgrade, And Roll Back](docs/install.md) for PATH setup, custom
directories, version selection, and executable rollback.

These docs follow the current source tree. For a released version, use the
[matching Git tag](https://github.com/isty2e/daem/tags); `daem version` identifies
your executable.

## Choose A Starting Point

- **No existing agent configuration:** [Getting Started](docs/getting-started.md)
  creates one project instruction file from a local source.
- **Existing files or agent configuration:** [Use An Existing Environment](docs/migration.md)
  explains importing live configuration, using local files, and registering
  matching outputs. You do not need a hosted Git repository.
- **Let your agent manage the manifest:** install the repository's
  [daem skill](docs/agent-skill.md).

## A Local Project Manifest

This example uses files kept alongside `daem.toml`:

```toml
version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "project"
install_mode = "copy"

[instructions.project]
source = "instructions/project.md"

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
```

Create `instructions/project.md` and a `skills/review/` directory containing a
valid `SKILL.md` before locking this manifest. Local sources do not need a Git
ref. [Sources](docs/manifest.md#sources) describes local, Git, and S3 options.

## Make Changes

You can edit TOML directly or use `daem add` and `daem remove`. Those commands
write the manifest and lockfile together; `--dry-run` previews the change.
After a manual edit or import, run `daem lock --dry-run`, then `daem lock`.
Neither authoring nor locking changes agent files.

Inspect the plan before applying it:

```bash
daem status
daem apply --dry-run --diff
daem apply
```

Bare `apply` asks for confirmation when all three streams are terminals.
Non-interactive execution requires `--yes`. Applying global resources affects
the selected agent's user-level configuration, not just the current project.
Removing a declaration does not by itself erase host package caches,
credentials, or shared plugin data; review the planned removal.

## Documentation

[Browse the documentation](docs/README.md), or go directly to:

- [CLI Reference](docs/cli.md) — commands, flags, output, and exit codes.
- [Manifest Reference](docs/manifest.md) — fields and complete examples.
- [Concepts](docs/concepts.md) — sources, locks, ownership, and reconciliation.
- [Troubleshooting](docs/troubleshooting.md) — conflicts, drift, and recovery.
- [Host Integrations](docs/host-integrations.md) and [Platform Support](docs/platforms.md)
  — supported operations and their limits.

For source builds and development checks, see [Contributing](CONTRIBUTING.md)
and [installation from source](docs/install.md#build-from-source).

## License

Daem is licensed under the [MIT License](LICENSE).
