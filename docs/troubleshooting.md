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
host state. `status --check` returns nonzero when the environment is not up to
date; inspect its findings rather than treating that exit alone as a crash.
See the [CLI Reference](cli.md) for JSON output and exact exit-code behavior.

## Find Your Symptom

- Setup: [unsupported platform](#unsupported-platform),
  [missing or stale lockfile](#lockfile-is-missing-or-stale),
  [skipped import](#import-skipped-an-instruction-hook-or-mcp-file).
- Ownership: [`ownership_conflict`](#ownership_conflict),
  [`unmanaged_output_exists`](#unmanaged_output_exists),
  [drift](#a-managed-output-drifted),
  [same-name skills at several paths](#same-skill-name-at-multiple-agent-paths).
- MCP: [missing environment sources](#missing-mcp-environment-sources),
  [Pi provider or config mismatch](#pi-mcp-provider-or-config-is-not-current).
- Extensions: [unclaimed carrier](#external-carrier-is-present-but-unclaimed),
  [order change](#extension-order-changed-after-carrier-updates),
  [refresh failure](#extension-refresh-was-refused-or-failed),
  [Pi removal not converged](#pi-package-removal-did-not-converge).
- Interrupted work: [apply](#apply-was-interrupted),
  [manifest metadata update](#manifest-metadata-update-was-interrupted),
  [journal from an earlier boot](#recovery-journal-from-an-earlier-boot).
- Other limits: [old durable schemas](#pre-10-durable-authority-schemas),
  [skill-group expansion](#lock-or-doctor-exceeded-the-skill-group-expansion-limit),
  [NFS](#nfs-backed-home-or-workspace).
- [Collect a diagnostic report](#collecting-a-diagnostic-report).

For a planned migration rather than a failure, see
[Use An Existing Environment](migration.md). Durable record formats and limits
are described in [State And Recovery](state-and-recovery.md).

## Unsupported Platform

On an unsupported platform, `daem doctor` or `daem doctor --json` reports the
platform error alongside path-resolution errors and any independent checks.
Capability-bound checks are `unsupported`/`skipped`, not `ok`; durable
file-set/recovery inventory is not invoked. Storage-backed commands remain
unavailable; use an admitted [platform](platforms.md).

## Pre-1.0 Durable Authority Schemas

Old statefile v7, ownership/carrier registry v1 and journal v7 recorded path
strings without current filesystem witnesses. Daem cannot infer their case or
Unicode namespace semantics.

Only exact empty artifacts can retire automatically: v7 state with all seven
fact arrays present/empty, or v1 registries with present/empty `claims`. Reads
do not rewrite/delete them; the next state-changing guarded write uses current
schema. Missing, null, populated or unknown fields remain blocked. Fields
require exact ASCII `lower_snake_case`; `CLAIMS` is not an alias.

Every pre-v13 journal is blocked, never rewritten or adopted. These journals
lack current recovery authority: v12 removal-demand coverage, v10 durable Linux
mount identity, v9 transition foreign key/global-root binding, or v8's current
transaction contract. Ordinarily recover with the writer before upgrading, but
**v10 requires the continuity check below**.

Do not unconditionally open a v10 journal with its old writer. That writer can
authorize Linux recovery with a reusable mount witness that lacks the current
boot-bound evidence. Use it only after independently establishing that the
journal was created in the current boot and that no relevant filesystem was
unmounted or remounted since capture. If either fact is unknown, stop: preserve the journal,
its backups, and the affected filesystem state for manual analysis. Do not run
the old writer merely to bypass the current refusal.

No action is needed for a new workspace with none of these old durable files.
If a populated old artifact or any old recovery journal exists, do not hand-edit
it into the new schema: a witness must come from a fresh filesystem observation,
and changing the JSON would forge authority rather than migrate it. A schema
version newer than this daem understands instead requires upgrading daem; do not
apply the old-binary retirement procedure to a future schema.

Do not edit or delete the statefile, shared ownership registry, carrier claim
registry, or recovery journal to bypass the refusal. Use this single
dry-run-first retirement and re-adoption route:

1. Keep an exact copy of the manifest and lockfile in version control, and use
   the daem binary that wrote the old schema against the same manifest path.
2. If that version reports an active v10 journal, first apply the v10
   continuity rule above. For any old journal whose old-writer recovery remains
   authorized, run `daem recover --dry-run`, review it, and complete that
   recovery before changing declarations. If v10 continuity cannot be
   established, stop and preserve the evidence instead of continuing this
   procedure.
3. Capture `daem status --json` and `daem apply --dry-run --diff` as evidence of
   the old managed state.
4. With the old version, remove the manifest's managed resources through
   ordinary `daem remove` operations, review the resulting `daem apply --dry-run
   --diff`, and apply the retirement. This lets the old version remove its own
   host outputs and release state and shared claims through their normal
   protocols. It may leave the exact empty v7/v1 artifacts described above.
5. Confirm no recovery journal remains. Restore the manifest declarations from
   version control. With current daem,
   run `daem lock --dry-run --verbose`, `daem lock`, and `daem apply --dry-run
   --diff`; then apply only after the recreated outputs and any
   `--manage-existing` adoption are acceptable.

Do not mix the old and current daem versions between these steps. If ordinary
old-version recovery or retirement cannot complete, preserve the files and
diagnostics for manual analysis rather than deleting authority evidence.

## Recovery Journal From An Earlier Boot

Linux journals, including state-only journals, bind the selected manifest root
to the capture boot. After reboot, recovery refuses before
host/state/ownership/cleanup effects. Preserve the journal/backups for manual
analysis; never edit/delete them to bypass refusal. This changes neither
manifest nor lock and does not block a clean workspace. Automatic cross-boot
recovery is unsupported.

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

This passive, point-in-time warning does not itself block apply. Daem checks
exact cataloged paths, not host precedence, and neither adopts nor deletes
retained copies; files may change afterward.

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

`daem import` can author source-exact extension declarations for supported
Codex, Claude Code, OpenCode, and Pi rows, but it is not a shortcut around
these steps. Import writes no lock or management claim. Review the generated
declaration, run `daem lock`, and use explicit `apply --manage-existing` only
when the resulting exact relation is eligible and intended.

## Lock Or Doctor Exceeded The Skill-Group Expansion Limit

Skill-group source/selector overflow rejects the whole lock without a partial
lockfile. Doctor reports an error and omits all selector-expanded checks, not
just the overflowing subset.

Use fewer declarations/roots, narrower repository roots or smaller explicit
groups, then retry. Files/links count as entries; exclusions do not refund
selection work, and shared listings do not make extra declarations free. [Skill
Groups](manifest.md#skill-groups) lists the fixed, non-configurable ceilings.

## Import Skipped An Instruction, Hook, Or MCP File

Import reads existing agent files as untrusted input. An instruction file is
skipped when its final path is a symlink, it is not a regular file, it exceeds
128 MiB, or it changes during the read. Replace a final symlink with an owned
regular-file copy only when that is the desired source of truth; do not point
import at a FIFO, socket, device, or directory.

Hook JSON skips unsafe files, documents over 4 MiB, duplicate keys, depth over
64, more than 256 events/4,096 groups/4,096 handlers, event names over 256
bytes, and input other than one complete UTF-8 JSON value. Comments are
rejected. Check/apply/recover use the same limits and reject oversized
rendered/restored output before writing.

MCP import skips the whole document for unsafe final-path shape, changes during
read or the codec's 4 MiB limit; no partial servers are imported. OpenCode also
skips strict `opencode.json` when the cataloged `opencode.jsonc` alternate
exists. Move/remove the alternate only if strict JSON should be authoritative,
then preview again. Daem never edits either host file. An alternate appearing
before publication or a rewritten/replaced primary makes the plan stale—even for
identical bytes or unchanged selected rows. The appeared alternate is not
traversed/read.

For a failed skill-source write, stabilize every contributing route and ensure
an exact regular root `SKILL.md`, only regular files/directories and no nested
symlinks. A resolved top-level symlink is allowed. Merged imports require all
routes to remain stable; the representative route supplies copied bytes.
Planning/staging limits are 100,000 entries, 64 descendant-directory levels and
4 GiB per tree; the containing inventory does not reduce tree depth. Aggregate
freshness allows 400,000 entries/16 GiB, without widening per-tree limits.

Root inventory allows 100,000 immediate entries, 32 MiB total name bytes and
4,096 bytes per name across new distinct resolved roots. Reduce unrelated
entries, shorten names or select fewer roots if exceeded. Reused inventories and
live-root bindings are revalidated on reuse, after revision capture and before
publication; child reads stay under the captured root. Changed roots/aliases or
copied identity prevent manifest/partial-skill publication. Retry `daem import
--target <target> --dry-run` only once stable and within bounds.

Hook import allows 4,096 skipped entries and 256 KiB total skip diagnostics.
Document overflow yields one `hook_import_budget_exceeded` skip, never a
valid-looking subset. Fix the live file and retry `daem import --target <target>
--dry-run`.

Imported hooks must satisfy the desired model: invalid UTF-8,
control/bidirectional-control text and other model violations are skipped. Valid
event-based names stay unchanged so later merge still correlates them.

On macOS/Linux the final file is checked as regular without following symlinks
or blocking on a special file. Cancellation occurs between filesystem
operations, not inside an OS call already entered.

## Extension Order Changed After Carrier Updates

Pi install/OpenCode edits can reveal new order risks. After carrier work,
interactive apply discloses only the newly observed managed/foreign precedence
changes and asks `Proceed with updated apply plan?`. Each risk identifies the
relation, foreign load identity and before-to-after/after-to-before movement.
Unsafe/local identities use stable `redacted:sha256:<digest>` labels, not
credentials, query material or local paths.

Non-interactive `apply --yes` stops instead. Inspect `daem apply --dry-run`,
then rerun interactively if the revised precedence is acceptable. Do not infer
that completed carrier work was rolled back. Daem does not start later
delegates after this stop.

If one OpenCode document converges and a later document fails, the result lists
each physical sequence as `converged`, `failed`, or `not_attempted`. Preserve
the selected files and recovery evidence, repair only the reported external
cause, and rerun apply. Retry reobserves current files and does not trust a
historical success row as convergence evidence.

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

If the plan reports `retained_cleanup_residue`, host, statefile, and ownership
recovery is already complete. The only legal action is
`finalize_journal_cleanup` over the exact correlated retirement artifacts.
Review the dry-run and rerun recovery; do not delete hidden residue or the
visible retirement control manually. A stale, replaced, malformed, or
cross-paired artifact is intentionally refused instead of being guessed safe.

If a cleanup obligation reports `namespace_changed` because a captured
existing parent is absent, daem cannot tell whether that exact directory was
unlinked or moved elsewhere. It retains the journal and performs no cleanup.
If an operator or tool moved the same directory, restore that exact directory
object to its original path and rerun `recover --dry-run`; do not create a
replacement directory. If the directory was actually unlinked, preserve the
journal for manual analysis because current daem has no durable evidence that
can authorize automatic retirement.

If recovery reports a pre-1.0 `.daem-tombstone-<32 lowercase hex>`, stop the
upgrade path. The current binary recognizes that exact old format only to
block; it does not inspect or migrate the old authority schema. Other names in
the `.daem-tombstone-` namespace are blocked as malformed. Use the daem version
that wrote a valid old tombstone to finish recovery, then upgrade. Do not rename
or delete the directory merely from its prefix. Remove it manually only after
independently proving that no interrupted apply or backup data remains.

If a command reports `journal retirement committed; hidden GC cleanup did not
complete successfully; no recovery action remains`, the journal residue was
already removed and semantic retirement completed. A private GC directory may
remain with retirement-control metadata, not journal backups. The command
still exits unsuccessfully so the physical cleanup failure remains visible,
but `daem recover` has no legal plan for GC-only residue and later commands are
not blocked. Daem does not automatically sweep the directory after restart
because the hidden name alone grants no deletion authority. Do not delete it
solely from its name.

## Manifest Metadata Update Was Interrupted

An interrupted `add`, `remove` or `unmanage` can leave a published metadata
marker. Manifest/lock/state/shared-carrier consumers then fail with `interrupted
file-set transaction`. Choose the action by the evidence:

| Evidence | Action |
| --- | --- |
| Active journal (`interrupted_apply`) or retained cleanup (`journal_cleanup_incomplete`) | Start with `daem recover --dry-run`. Finish authorized journal work before metadata retry; joint file-set fences remain. |
| Valid published `metadata-transaction` marker | Retry the exact interrupted write with the same manifest/selectors. It restores/finalizes the recorded files under complete authority, revalidates, then continues. Fully committed after-images may yield only cleanup and an already-absent result. Preview and `daem recover` do not consume this marker. |
| Marker version 1 or 2 | Current code preserves it; retry with its writer before upgrading. Current marker version 3 enforces bounded recovery. |
| Invalid/incomplete evidence, out-of-authority target or unclassifiable before/after image | Preserve diagnostics/state for manual inspection; repair before journal recovery. Never edit recorded files independently or delete the marker. |
| StateDir access/identity cannot be established | Restore it before recovery, even if a journal was observed. An accessible RecoveryDir child is insufficient. Do not retry a write as if a marker existed. |
| Bounded census overflow with accessible StateDir | Journal recovery may proceed if RecoveryDir is readable, but the census fence remains. This is neither a marker nor named residue; do not infer a retry or prefix-deletion remedy. |

Markerless `.daem-tmp-*`, legacy `.metadata-stage-*`, `.daem-tombstone-*` or
`.daem-cleanup-*` residue is a separate fence. Authoring/unmanage retry, refresh
and recover do not remove it. Preserve it; never delete, rename or empty by
prefix. If a recoverable journal exists, resolve it first; if a valid marker
also exists, then retry that write. Leftover siblings still block until
independently resolved. `unmanage` does not inspect/repair metadata transactions
while journal authority remains.

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

Linux amd64 NFSv3 supports ordinary unprivileged single-client use, including
kernel 5.15. Run one writer at a time against the same manifest or destination;
do not rely on daem leases for cross-node mutual exclusion. `daem doctor`
exercises scratch storage publication and artifact access on selected paths,
but cannot certify server durability or concurrent-writer exclusion.

If an NFS-backed operation appears stuck:

1. Confirm no daem process is still using the same manifest or destination on
   any node available to you.
2. If the original operation was interrupted, run `daem recover --dry-run` on
   the same host and manifest, within the same boot and without remounting the
   affected filesystems, before another mutation.
3. A failed NFS rename may already have taken effect. Preserve retained stages,
   journals, and backups if recovery refuses or reports an unknown outcome;
   do not assume nothing changed or delete internal metadata to bypass refusal.
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
