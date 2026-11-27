# Command hooks and hook assets

## Purpose

Manage a command's configuration and explicitly declared file inputs separately. The agent decides whether to trust or run a configured hook.

## Requirements

### Requirement: Lock a supported command contribution without claiming its executable

A `[[hook]]` SHALL select a unique name, event and command, with the admitted `command` handler type. The manifest parser owns the field grammar, scope and target selection, and target-override validation. An override belongs to the containing declaration, names a declared target once, and can set the supported target condition and matcher; it is not a separate CLI mini-language. A timeout of zero omits a manifest-level timeout.

On admitted Codex and Claude Code rows, daem locks a canonical `/hooks` contribution for each selected subject, not a source artifact for the command. It does not resolve a command string into a managed script, interpreter or tool. Warning-only diagnostics about timeout, shell syntax, broad interpreters, PATH and trust do not block the admitted row. OpenCode and Pi are lock-only bridge diagnostics; Antigravity direct hooks are not reconciled and are refused by `add hook`. Manual lock-only declarations and their removal are not permission to render hooks.

#### Scenario: A command uses a relative script path

- **WHEN** a supported hook's command is `python3 hooks/check.py` without a hook-file reference
- **THEN** the contribution is lockable but daem does not read, ship, or claim `hooks/check.py` on that basis

### Requirement: Keep each managed hook distinct in a shared settings file

Daem SHALL track each hook as its own `hook/<name>` subject even when several hooks use one physical `/hooks` aggregate. Observation uses the current [hook codec](../../../internal/realization/aggregate/codec/hook), not a whole-document hash. Unrelated top-level values are preserved. Existing unmanaged hooks block unless the selected aggregate exactly matches and the caller requests adoption. Removal addresses the owned contribution; after the last one it removes `/hooks` and removes the containing file only when no unrelated settings remain. A same-layer inline Codex `[hooks]` in `config.toml` blocks reconciliation of `hooks.json`.

The first admitted global Claude Code installation selects `CLAUDE_CONFIG_DIR` or `~/.claude` and records the selected settings file, including the directory target of a profile symlink. Preview shows the file; output inventory identifies the resolved path. Later changes follow the recorded profile, including legacy installations at the default location, until the last hook is removed. Combined global native-key and hook changes require the same physical settings file and refuse with both paths when they differ.

#### Scenario: A different shell selects another Claude profile

- **WHEN** a manifest with already-managed global hooks runs under a different `CLAUDE_CONFIG_DIR`
- **THEN** ordinary hook changes keep the recorded file, and daem does not silently combine the hook transaction with keys in the other file

### Requirement: Manage only selected explicit regular-file assets

A `[hook_asset.<name>]` SHALL describe one regular `kind = "file"` source using an admitted string or structured form. A local asset may use `vendor` or `link`; the source still has to resolve to an admissible regular file. A supported hook can refer to a selected asset in the same effective scope with `{hook_file:<name>}`. A missing, cross-scope, malformed or unsupported reference fails before mutation. Directory assets, invented entrypoints, implicit path rewriting, and bundled hook installation are outside this form. Import does not infer an asset from a host command.

The [hook asset profile](../../../internal/realization/profile/hook_asset_placement.go) determines placement and creation mode. `executable` defaults to false; initial modes are `0600` or `0700` when true. Changing that flag selects a different asset path; an existing file keeps its current permissions. Status and apply select only assets referenced by selected supported hooks. Removing the last reference leaves normal managed-output removal authority; a remaining reference to a deleted declaration is not allowed. Exact-byte adoption needs `--manage-existing` and keeps permissions.

#### Scenario: A declaration is locked but no selected hook refers to it

- **WHEN** an asset remains declared but none of the selected supported hook commands reference it
- **THEN** its lock entry does not authorize a new host file
