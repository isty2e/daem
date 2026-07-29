# daem

[![CI](https://github.com/isty2e/daem/actions/workflows/ci.yml/badge.svg)](https://github.com/isty2e/daem/actions/workflows/ci.yml)

`daem` is a Declarative Agent Environment Manager. It reads one manifest,
resolves and locks source-backed resources, and reconciles selected agent hosts.

The current product manages:

- project and global instruction files for Codex, Claude Code, OpenCode, Pi,
  and Antigravity CLI;
- Agent Skills directories where the target exposes a known skill root;
- supported Codex and Claude Code command-hook configuration;
- supported MCP configuration projections;
- supported host-native extension relations through delegated host commands;
  and
- the lockfile, managed-state file, cache, and recovery journal needed for
  guarded reconciliation.

Support varies by target and resource. See
[Feature Support](docs/features.md) for the current user-facing overview and
the [Host Integration Contract](docs/host-integrations.md) for exact native
routes and safety limits. Product operating-system and architecture support is
independent; see [Platform Support](docs/platforms.md).

Pi MCP configuration is supported through an explicit `pi-mcp-adapter`
package declaration rather than a Pi core-native surface. See the
[project](examples/pi-project-mcp-stdio.toml) and
[global](examples/pi-global-mcp-stdio.toml) examples.

## How It Works

```text
daem.toml -> daem lock -> daem.lock.toml -> daem status/apply -> host files
```

`add`, `remove`, and `import` author desired state. They write by default and
accept `--dry-run` for preview. `add` and `remove` refresh the lockfile in the
same transaction. Host effects remain behind `apply`: preview with
`apply --dry-run`, confirm interactively, or use `apply --yes` in
non-interactive environments.

## Manifest At A Glance

`daem.toml` can be edited directly or through `daem add` and `daem remove`.
This example manages global instructions and a Git-backed skill for Codex and
Claude Code, plus one global Claude Code MCP entry:

```toml
version = 1
targets = ["codex", "claude-code"]

[defaults]
scope = "global"
install_mode = "copy"

[instructions.personal]
source = "instructions/personal.md"

[[skill]]
name = "humanizer"
source = { git = "https://github.com/blader/humanizer.git", ref = "main" }
targets = ["codex", "claude-code"]

[[mcp_server]]
name = "context7"
targets = ["claude-code"]
scope = "global"
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@1.2.3"]
env = { API_TOKEN = { from_env = "CONTEXT7_API_TOKEN" } }
```

Global declarations affect every project using the selected agent profile.
Review `daem apply --dry-run --diff` before applying them. See the
[Manifest Reference](docs/manifest.md) for all fields and the
[Representative Project](examples/representative-project.toml) for hooks,
managed hook assets, and project-scoped resources.

## Install

Published binaries come from
[GitHub Releases](https://github.com/isty2e/daem/releases). Release `v0.1.0`
provides checksum-verified native artifacts for macOS 26 or newer on Apple
silicon and Linux on x86-64. Follow
[Install, Upgrade, And Roll Back](docs/install.md) for exact download,
verification, PATH, upgrade, rollback, and diagnostic steps.

Source builds remain available for contributor and unreleased development
testing, but they do not substitute for a supported native release lane.

## First Project

In the project you want to manage, create a manifest and one instruction source:

```bash
mkdir -p ~/daem-example
cd ~/daem-example
daem init
mkdir -p instructions
printf '%s\n' '# Project instructions' 'Use concise, direct answers.' > instructions/project.md
daem add instruction project ./instructions/project.md --target codex
```

Add `--dry-run` to preview an authoring command. The write updates `daem.toml`
and `daem.lock.toml` together. Inspect and reconcile the selected host:

```bash
daem status
daem apply --dry-run --diff
daem apply --yes
daem status --check
```

Use `daem import --target <target> --dry-run` instead of `init` when starting
from existing host configuration. The complete first-run and recovery flow is
in [Getting Started](docs/getting-started.md).

## Workspace Selection

`--manifest <path>` explicitly selects a workspace. When omitted, commands use
an existing `./daem.toml`, then the OS user manifest. Parent directories are
not searched. `init` and non-merge `import` are the exception: without an
explicit path they create `./daem.toml` rather than falling back to a user
manifest.

On supported platforms, the user manifest is
`${XDG_CONFIG_HOME:-~/.config}/daem/daem.toml`.

The lockfile is always `daem.lock.toml` beside the selected manifest. Project
metadata lives under `.daem/`; user-workspace state and cache follow the OS
state/cache directories. Imported durable source material defaults to
`daem.d/`, which is user-owned and distinct from `.daem/`.

## Command Lifecycle

| Stage | Commands | Purpose |
| --- | --- | --- |
| Start | `init`, `import` | Create desired state |
| Author | `add`, `remove` | Edit desired state and refresh the lock |
| Resolve | `lock`, `outdated` | Resolve or inspect source identity |
| Inspect | `list`, `status`, `version` | Enumerate resources, convergence, and executable identity |
| Diagnose | `doctor`, `probe` | Check passive prerequisites or explicit runtime evidence |
| Operate | `refresh extension` | Refresh one explicitly selected supported host extension |
| Reconcile | `apply`, `recover` | Apply desired state or resolve an interrupted operation |

Run `daem help <command>` for scoped usage. Advanced resource fields belong in
the manifest rather than an expanding flag surface.

## Agent Skill

This repository includes a portable [`daem` agent skill](skills/daem/SKILL.md)
that routes instruction, skill, hook, MCP, and extension changes through the
selected manifest, lockfile, and apply workflow. Bootstrap it into a global
Codex environment with:

```bash
daem add skill https://github.com/isty2e/daem.git \
  --path skills/daem --ref v0.1.0 --name daem \
  --target codex --scope global --dry-run --diff
# Review the preview, then repeat without --dry-run --diff.
daem apply --dry-run --diff
daem apply
```

Choose or repeat `--target` for other agent hosts supported by the installed
daem.

## Safety Boundaries

- `--dry-run` uses the same planning and validation path without persistent
  writes or runtime effects.
- Default human output is concise. `--verbose` adds bounded evidence;
  `--json` emits one schema-versioned document.
- `--verbose` and `--json` are mutually exclusive. `--diff` requires
  `--dry-run` and cannot be combined with `--json`.
- `doctor` is passive. `probe` is an explicit, non-persistent runtime check and
  does not establish future apply authority.
- Removing desired state does not by itself erase shared plugin data, package
  caches, or credentials. Exact behavior is reported by the apply plan and
  host integration contract.

## Documentation

- [Getting Started](docs/getting-started.md)
- [Install, Upgrade, And Roll Back](docs/install.md)
- [CLI Reference](docs/cli.md)
- [Manifest Reference](docs/manifest.md)
- [Feature Support](docs/features.md)
- [Host Integration Contract](docs/host-integrations.md)
- [Platform Support](docs/platforms.md)
- [Concepts](docs/concepts.md)
- [Glossary](docs/glossary.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Skill Compatibility](docs/compatibility.md)
- [Minimal Example](examples/daem.toml)
- [Representative Project Example](examples/representative-project.toml)
- [Skill Placement Example](examples/skill-placement.toml)

For development and verification guidance, see
[Contributing](CONTRIBUTING.md).

## License

Daem is licensed under the [MIT License](LICENSE).
