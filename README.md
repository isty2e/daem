# daem

[![CI](https://github.com/isty2e/daem/actions/workflows/ci.yml/badge.svg)](https://github.com/isty2e/daem/actions/workflows/ci.yml)

`daem` is a Declarative Agent Environment Manager. It reads one manifest,
resolves and locks source-backed resources, and reconciles selected agent hosts.

The current product manages:

- project and global instruction files for Codex, Claude Code, OpenCode, Pi,
  and Antigravity CLI;
- Agent Skills directories where the target exposes a known skill root;
- supported Codex and Claude Code command-hook configuration;
- supported standalone MCP configuration projections;
- supported host-native extension relations through delegated host commands;
  and
- the lockfile, managed-state file, cache, and recovery journal needed for
  guarded reconciliation.

Support varies by target and resource. See
[Feature Support](docs/features.md) for the current user-facing overview and
the [Host Integration Contract](docs/host-integrations.md) for exact native
routes and safety limits. Product operating-system and architecture support is
independent; see [Platform Support](docs/platforms.md).

## How It Works

```text
daem.toml -> daem lock -> daem.lock.toml -> daem status/apply -> host files
```

`add`, `remove`, and `import` author desired state. They write by default and
accept `--dry-run` for preview. `add` and `remove` refresh the lockfile in the
same transaction. Host effects remain behind `apply`: preview with
`apply --dry-run`, confirm interactively, or use `apply --yes` in
non-interactive environments.

## Install From Source

No public release artifacts are published yet. Install from a source checkout
with a current security patch of Go 1.25 or later:

```bash
git clone https://github.com/isty2e/daem.git
cd daem
go install ./cmd/daem
export PATH="$(go env GOPATH)/bin:$PATH"
daem version
daem --help
```

If `go env GOBIN` is set, add that directory to `PATH` instead.
Run a source installation only on a currently supported platform. Released builds
must also have passed the row's required native lane. See
[Platform Support](docs/platforms.md) for the authoritative rows and evidence
distinction.

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

For development and verification guidance, see
[Contributing](CONTRIBUTING.md).

## License

Daem is licensed under the [MIT License](LICENSE).
