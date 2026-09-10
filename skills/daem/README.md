# Use Daem From Your Agent

The portable [daem skill](SKILL.md) guides an agent through manifest edits,
locking, previews, and apply. Install it after
[installing daem](https://github.com/isty2e/daem/blob/main/docs/install.md).

This example installs the skill globally for Codex. It affects that agent's
user-level skill directory, so it is available across projects.

## Select The User Manifest

```bash
DAEM_USER_MANIFEST="${XDG_CONFIG_HOME:-$HOME/.config}/daem/daem.toml"
```

If this manifest does not exist, preview and create it. Skip both commands when
it already exists:

```bash
daem init --manifest "$DAEM_USER_MANIFEST" --dry-run
daem init --manifest "$DAEM_USER_MANIFEST"
```

## Add And Apply The Skill

Preview the declaration at the pinned release ref, then write it:

```bash
daem add skill https://github.com/isty2e/daem.git \
  --path skills/daem --ref v0.1.0 --name daem \
  --target codex --scope global --manifest "$DAEM_USER_MANIFEST" \
  --dry-run --diff
daem add skill https://github.com/isty2e/daem.git \
  --path skills/daem --ref v0.1.0 --name daem \
  --target codex --scope global --manifest "$DAEM_USER_MANIFEST"
daem apply --manifest "$DAEM_USER_MANIFEST" --dry-run --diff
daem apply --manifest "$DAEM_USER_MANIFEST"
```

`add` writes the manifest and lockfile; `apply` changes the agent's files after
confirmation. Review the whole plan, including any other declarations already
in that manifest. Non-interactive apply requires `--yes`.

Choose or repeat `--target` for other hosts supported by the installed daem.
The ref selects the skill content; it does not upgrade the daem executable.
