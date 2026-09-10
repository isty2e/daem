# Getting Started

Create a project with one Codex instruction file, using a local source. This
example needs neither a remote repository nor an existing agent configuration.
If you already have files to keep, start with
[Use An Existing Environment](migration.md) instead.

## Install Daem

Follow [Install, Upgrade, And Roll Back](install.md), including its PATH step.
Then check that the executable is available:

```bash
daem version
daem --help
```

## Create A Project

Use a new directory so the example does not overwrite existing files:

```bash
mkdir ~/daem-example && cd ~/daem-example
```

If that directory already exists, choose another name. Preview and create
`daem.toml`:

```bash
daem init --dry-run
daem init
```

`init` writes only the starter manifest. It does not create a lockfile or touch
agent files. Run the remaining commands from this directory; daem does not
search parent directories for a manifest.

## Add Instructions

Create the source file:

```bash
mkdir instructions
printf '%s\n' '# Project instructions' 'Use concise, direct answers.' > instructions/project.md
```

Preview the declaration, then write it:

```bash
daem add instruction project ./instructions/project.md --target codex --dry-run --diff
daem add instruction project ./instructions/project.md --target codex
```

The second command updates `daem.toml` and creates the adjacent
`daem.lock.toml`. Your source stays in `instructions/project.md`; the Codex
output, `AGENTS.md`, does not exist yet. There is no separate lock step after a
successful `add`.

## Apply And Check

Inspect the pending change:

```bash
daem status
daem apply --dry-run --diff
```

The plan should create the project's `AGENTS.md` from your instruction source.
If it reports a conflict or a different destination, stop and check the
[troubleshooting guide](troubleshooting.md) before continuing.

Apply the change:

```bash
daem apply
daem status --check
```

Bare `apply` displays the effects and asks for confirmation. Answer `yes` only
if they match your intent. All three streams must be terminals; in a script,
use `daem apply --yes` after reviewing the preview. `status --check` returns
zero when the selected environment is up to date.

Keep these roles separate:

| File | Your next use |
| --- | --- |
| `instructions/project.md` | Edit the instruction content here. |
| `daem.toml` | Change which resources and targets daem manages. |
| `AGENTS.md` | Let daem update this output; direct edits are reported as drift. |

## Continue From Here

- After changing a source or editing TOML directly, preview `daem lock --dry-run`,
  write with `daem lock`, then preview and apply again.
- [Use local skills or import existing configuration](migration.md).
- [Choose resources for another host](features.md). Repeat `--target` to select
  multiple hosts; do not use comma-separated values.
- [Read the manifest fields](manifest.md) or [browse complete examples](README.md#examples).
- [Diagnose a problem or recover an interrupted apply](troubleshooting.md).
