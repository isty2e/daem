# Troubleshooting

Start with read-only inspection. Substitute `--manifest <path>` when the
workspace is not selected by the normal current-directory or user-manifest
rules.

```bash
daem status --check
daem list outputs --verbose
daem doctor
daem apply --dry-run --diff
```

`doctor` checks passive prerequisites. It does not run MCP servers or mutate
host state. See the [CLI Reference](cli.md) for JSON output and exact exit-code
behavior.

## `ownership_conflict`

Another manifest owns the same whole output or an overlapping config
projection. Matching content does not permit co-ownership.

1. Run `daem list outputs --verbose` and identify the owning manifest and
   destination.
2. If the existing owner is correct, edit or remove the declaration from that
   manifest, refresh its lock, and apply that removal.
3. If an operation was interrupted, run `daem recover --dry-run` for the owning
   manifest before retrying.
4. Run `daem status` for the new manifest after the old claim is released.

`--manage-existing` does not steal a claim. Do not delete daem state or shared
ownership files to bypass this check.

## Same Skill Name At Multiple Agent Paths

`doctor`, `status`, or `apply` may report
`skill_discovery_duplicate_retained` when the selected skill destination and
another same-name directory both exist in modeled discovery roots for the same
target and scope.

1. Run `daem list paths --target <target>` to compare the selected write root
   with the target's other discovery roots.
2. Check the reported directories and the target's own loading behavior.
3. If the selected root is wrong, set a supported target-specific `install_to`
   in the manifest, then run `daem lock` and preview apply again.
4. If the retained copy is obsolete, remove it manually only after confirming
   that no other workspace or target needs it.

The warning is passive and does not block apply by itself. Daem does not infer
host precedence, adopt the retained copy, or delete it. The check examines
only exact cataloged same-name paths and is a point-in-time preflight:
filesystem state can still change after the warning is produced.

## `unmanaged_output_exists`

The desired destination exists but the selected manifest does not own it.
Inspect the dry-run diff before choosing a remedy:

```bash
daem apply --dry-run --diff
```

If the live output is exactly the desired output and should become managed,
preview and confirm registration:

```bash
daem apply --manage-existing --dry-run
daem apply --manage-existing --yes
```

Daem refuses registration when content or required file metadata differs. In
that case, preserve or move the existing material yourself, or import supported
host configuration into a manifest before applying. `--manage-existing` is not
an overwrite flag.

## Missing MCP Environment Sources

A normal apply fails before host, state, or recovery mutation when a selected
MCP declaration references a `from_env` source that is absent from the daem
process environment. The error lists source names, never their values.

1. Export each reported source name in the same shell or service environment
   that launches daem. An explicitly empty value is accepted as present.
2. Retry `daem apply --dry-run` to review the plan; dry-run does not require the
   runtime values.
3. Run the normal apply again after the sources are present.
4. If the binding is no longer wanted, remove it from the manifest, refresh the
   lockfile, and apply the removal. A removed binding does not require its old
   source.

Do not put secret values directly in the manifest. `status` and `doctor` remain
point-in-time diagnostics and do not replace the apply-time gate.

## Pi MCP Provider Or Config Is Not Current

Pi MCP support is provided by the explicit `pi-mcp-adapter` extension in the
same manifest. Start with:

```bash
daem status --target pi --verbose
daem apply --target pi --dry-run --diff
```

- `provider_prerequisite` reports package presence and the freshly observed
  exact version separately from config projection. Supported stable versions
  are `>=2.13.0` and `<3.0.0`; `2.15.0` is the deeply inspected artifact, not a
  permanently pinned version.
- A project provider may require Pi project trust before it loads. A global
  provider can read project MCP layers even under `pi --no-approve`; an
  unowned eager entry may execute before trust. Daem authors lazy entries but
  does not sanitize other project MCP files. Review `.mcp.json` and
  `.pi/mcp.json` before using a global provider.
- `effective_shadowing` names a higher-layer same-name definition. Review the
  six provider layers in the Host Integration Contract; daem mutates only the
  selected `.pi/mcp.json` or agent-root `mcp.json`.
- After removing a managed binding, a lower unowned definition may become
  effective. That is reported as fallback, not deleted.
- If the package was manually removed or disabled, rerun the reviewed apply.
  Historical install evidence never substitutes for fresh package and config
  observation.

Use `daem remove extension <provider-id>` only when the provider package itself
is also undesired. Removing only the MCP row intentionally keeps the provider.

## External Carrier Is Present But Unclaimed

An extension installed outside daem can be claimed only after its exact
declaration and full future lifecycle are known:

```bash
daem add extension <id> <source> --target <target> [--scope <scope>]
daem status
daem apply --manage-existing --dry-run
```

If the manifest was edited directly, run `daem lock` before status. Continue
with confirmed apply only when the dry-run says `would record external carrier
claim` and its source, target, scope, future removal effects, and non-claims are
acceptable.

- `carrier adoption available` receives the dry-run hint.
- `carrier adoption unavailable` includes the first lifecycle blocker and
  receives no success hint.
- Source-inexact, same-name, normalized-equivalent, shadowed, stale, ambiguous,
  or conflicting evidence is refused rather than approximated.
- Current Antigravity CLI external rows are source-inexact and cannot be
  claimed through manage-existing.
- Adoption invokes no host install command. Later manifest omission may still
  invoke the bounded managed-removal route disclosed by dry-run.
- If apply fails after execution was attempted, the claim result may be
  unknown. Run `daem status`; do not infer absence from the error or delete
  state/registry files.

The current `daem import` does not yet author extension declarations and is not
a shortcut around these steps. Use `add extension` until exact extension import
is implemented; imported desired state will still require lock and explicit
manage-existing authority.

## Apply Was Interrupted

While a recovery journal is active, ordinary operations that could conflict
with it are refused. Inspect the recovery plan first:

```bash
daem recover --dry-run
```

Then run interactive `daem recover`, or use `daem recover --yes` in a reviewed
non-interactive environment. Recovery may clean up a completed journal, roll
back guarded changes, or finish ownership finalization. Do not edit host files,
the statefile, shared ownership data, or the journal while recovery is pending.

## Manifest Metadata Update Was Interrupted

An interrupted `add`, `remove`, or `unmanage` write may leave a recoverable
metadata transaction marker. While that marker exists, commands that read or
mutate the selected manifest, lockfile, project state, or shared carrier claims
fail closed with an `interrupted file-set transaction` diagnostic.

Retry the exact interrupted write against the same manifest and selectors. The
write reacquires the complete authority set, restores or finalizes the recorded
manifest/lock/state/registry file set, revalidates current input, and then
continues from the recovered state. If every after-image was already committed,
the retry may only remove completed evidence and then report that the selected
declaration or management fact is already absent. A preview command does not
recover persisted evidence.

This marker is separate from the apply recovery journal: `daem recover` does
not consume it. If the exact retry reports that a target is outside its current
recovery authority or cannot be classified as a recorded before/after image,
do not delete the reported `metadata-transaction` directory or edit the
recorded files independently. Preserve the diagnostic and the selected project
state for manual inspection.

## Lockfile Is Missing Or Stale

Direct manifest edits and imports require an explicit lock refresh:

```bash
daem lock --dry-run --verbose
daem lock
```

`add` and `remove` update the manifest and lockfile together. A floating source
can resolve differently on a later lock; use `daem outdated` when you only want
to inspect whether locked sources can advance.

## Extension Refresh Was Refused Or Failed

Preview the exact selected host route and its broader effects first:

```bash
daem refresh extension <id> --dry-run --verbose
```

- A missing or stale lock requires `daem lock`; refresh never repairs or
  rewrites the lock itself.
- A wrong `--target` or `--scope` is a failed safety filter, not an alternate
  destination.
- Claude Code requires fresh passive evidence that the exact selected relation
  is present. Observed absence is an install/apply concern and never falls back
  to install from refresh.
- `attempted_unverified` is the expected successful refresh result for Codex,
  OpenCode, Pi, and Antigravity CLI rows without a supported refresh-specific
  postcondition. It is history, not convergence or authority to skip a later
  explicit request.
- A missing executable fails before host mutation. A started host failure or
  `partial` result may retain host changes; daem does not claim rollback.
  Inspect the named host state and retry only after reviewing the same dry-run
  disclosure.

Refresh never removes, disables, uninstalls, prunes, or repairs plugin-bundled
contributions. See the
[Host Integration Contract](host-integrations.md#explicit-carrier-refresh) for
each host's native command and verification strength.

## Pi Package Removal Did Not Converge

Run `daem status --verbose` and preview the same desired absence again:

```bash
daem apply --dry-run --verbose
```

- `effect_postcondition_unsatisfied` means the selected settings row may be
  absent while the scoped npm package or Git checkout remains. Inspect only
  the disclosed Pi scope; after repairing the host-owned partial state, rerun
  apply so fresh evidence can settle the retained pending removal without
  blindly invoking Pi again.
- Unreadable, malformed, symlinked, or changing settings and artifact paths are
  unavailable evidence, never proof of absence.
- Local-path removal must leave the referenced source content unchanged. A
  changed or deleted source blocks claim retirement even when the settings row
  disappeared.
- Global removal proves only that no other daem manifest claims the same
  carrier. It cannot detect arbitrary external projects that still use it.

Use `unmanage extension` instead when the intended outcome is to retain the Pi
relation and release only daem's management authority.

## A Managed Output Drifted

Daem reports drift instead of overwriting an output that changed outside daem.
Use `daem status --verbose` and `daem apply --dry-run --diff` to compare the
locked desired output with the live destination. Either restore the managed
content through a reviewed apply or intentionally change the manifest and lock.
Do not use `--manage-existing` to convert mismatched content into ownership.

## NFS-Backed Home Or Workspace

Ordinary single-host use is expected to work, but NFS locking, caching, outage,
and durability behavior varies by deployment. Do not run daem concurrently on
multiple nodes against the same manifest or shared destination, and do not rely
on daem leases for cross-node mutual exclusion.

If an NFS-backed operation appears stuck:

1. Confirm no daem process is still using the same manifest or destination on
   any node available to you.
2. If the original operation was interrupted, run `daem recover --dry-run` on
   the same host and manifest before another mutation.
3. Do not remove internal lease, journal, state, or ownership files manually.
4. Use a single local-filesystem host and workspace for daem mutations when
   cross-node exclusion or crash-durability guarantees are required.

See [NFS-Backed Homes](concepts.md#nfs-backed-homes) for the exact boundary.

## Collecting A Diagnostic Report

These commands provide bounded machine-readable evidence without applying
changes:

```bash
daem version --json
daem status --json
daem doctor --json
daem apply --dry-run --json
daem recover --dry-run --json
```

Review paths, source identifiers, and host details before sharing the output.
Secret values are not intended to appear, but local environment information can
still be sensitive.
