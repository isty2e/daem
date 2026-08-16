# Concepts

`daem` is built around one flow:

```text
manifest -> normalized desired resources -> lockfile -> plan -> host files
```

The manifest is the desired-state boundary. The lockfile captures resolved
source identity. The statefile records which live host outputs `daem` owns.
`status` and `apply --dry-run` compare all three against the current filesystem
before any host mutation happens.

## Manifest

`daem.toml` declares the resources you want managed:

- instruction files
- skills
- skill groups
- command hooks
- hook assets
- MCP server bindings
- extension carriers

The parser is strict. Unknown keys, duplicate resource names, invalid target
values, unsafe paths, and unsupported source shapes are rejected before the
planner runs.

Manifest selection is shared by all resource families. If `--manifest` is
omitted, `daem` checks only `./daem.toml` in the current working directory.
When that file exists, its directory is the selected project root. Otherwise,
on a supported platform, `daem` uses
`${XDG_CONFIG_HOME:-~/.config}/daem/daem.toml` as the user manifest. Platform
admission is documented separately in [Platform Support](platforms.md); a path
that can be calculated for another OS is not a product-support claim.

The CLI does not search parent directories for `daem.toml`. The OS user config
manifest is not a project root; project-scoped target-visible resources from
that manifest are rejected. Use an explicit project manifest such as
`--manifest ./daem.toml`, or declare those resources with `scope = "global"`.

## Targets and Scopes

Targets name agent hosts:

- `codex`
- `claude-code`
- `opencode`
- `pi`
- `antigravity-cli`

Scopes decide whether a target-visible resource is project-local or global:

- `project`: written under the selected project manifest root.
- `global`: written under the target's user-level root.

Current support is summarized in
[Feature Support](features.md). That page is a derived user-facing view of
typed target/resource surface contracts. Future target support must
extend the typed registry and its invariant-bearing tests rather than adding ad
hoc target/product booleans here.

Unsupported instruction target/scope combinations are rejected for `lock`,
`status`, and `apply`; for example, Antigravity CLI project instructions render
to `AGENTS.md` by default and Antigravity CLI global instructions render to
`~/.gemini/GEMINI.md`. Unsupported hook targets are treated as lock-only
diagnostics when command hook rendering is not implemented for those hosts.
Antigravity CLI direct hooks are not supported as a native command-hook surface,
so `add hook` rejects that target even though a manually authored declaration
can still be reported as lock-only.

## Sources

Lockable sources can come from:

- local filesystem paths
- Git repositories
- S3 objects

Local paths are resolved relative to the manifest directory unless absolute.
Git accepts only credential-free HTTPS, SSH/scp-like, absolute file URL, and
native absolute repository locators. A Git ref is one unqualified branch-or-tag
name, qualified branch/tag, or full 40/64-hex commit id; unqualified
branch/tag collisions and revision/refspec syntax are rejected. `lock` records
the canonical declaration identity separately from the resolved immutable
commit. S3 sources use the AWS SDK default configuration chain unless a
source-level region is provided.

Hook commands are not lockable source payloads. They are rendered as host
configuration strings and must already be executable in the host environment.

## Lockfile

`daem.lock.toml` records source identity, content hashes, declaration
provenance, and locked operation identities for lockable resources and
supported delegated relations. It is exact with respect to the manifest: when a
lockable resource is removed from `daem.toml`, the next successful `daem lock`
removes the stale lockfile entry.

The lockfile does not delete host files. Host cleanup is a guarded
reconciliation action planned by `status` and executed by `apply`.

## Statefile

The statefile records which live host outputs one selected manifest previously
wrote or registered, plus project-scoped carrier claims and write-ahead carrier
transitions. For global outputs and carrier relations, durable authority also
uses the matching subject-specific registry in daem's shared data root. The
statefile may record a separate bounded last delegated-attempt record tied to a
locked plan identity. It is separate from the lockfile because resolved source
identity, live host authority, and historical attempt diagnostics answer
different questions:

- the lockfile says what content was resolved from declared sources
- the statefile says which host outputs were previously written or registered,
  and which project carrier claims or pending transitions remain authoritative
- last delegate attempt records say what happened during the last relevant
  bounded attempt, without claiming current package-manager cache convergence,
  runtime server health, tool inventory, credential validity, or trust state

Host-route diagnostics likewise retain only the latest request for each
subject, target, scope, and route id. A changed request hash replaces the prior
diagnostic for that route; the statefile is not an append-only operation log.
Statefile v9 stores a delegate command's mechanical process reason separately
from post-attempt working-directory authority. Current daem reads v8 records
using their legacy combined reason and writes the separated facts only as v9.

The statefile is private authority data. Daem writes and accepts it only as an
invoking-user-owned regular file with exact mode `0600`; final symlinks,
special files, replacement during a read, oversized content, and other
permission modes are rejected.

When a managed output changes outside `daem`, future apply operations report
drift instead of overwriting it. When an output exists but is not state-owned,
`apply` reports `unmanaged_output_exists` unless `--manage-existing` is used and
the live subject exactly matches the desired output, including required file
metadata for mode-sensitive outputs.

Managed-path state records the projection's permission policy. Exact-mode file
projections retain the last verified permission bits; executable-class files
retain no read/write-bit baseline because executable class is already part of
their content identity. The retained exact mode is historical authority, not a
claim about the file's current mode. Recovery therefore keeps it separate from
the freshly captured physical mode used to guard and restore an interrupted
operation.

Visibility, import, state ownership, convergence, removal, and destructive
cleanup are separate claims. `import` records declarations but does not by
itself claim host state. `apply --manage-existing` may adopt an existing output
only when the live subject exactly matches the desired output and the applicable
route supports adoption.
For supported external carriers, explicit manage-existing commits a
source-exact state-only claim after full lifecycle admission and fresh
revalidation, without invoking the host route. The claim records future bounded
managed-relation removal authority, not package/cache ownership, runtime
readiness, or ambient exclusivity.

### Shared Global Ownership

Project statefiles are independent, but explicit-global declarations from
different projects can resolve to the same host path. Daem therefore keeps a
shared managed-output registry under its OS data root. One canonical whole path
or overlapping config projection has one statefile authority. Equal bytes do
not allow co-ownership. Losslessly disjoint projections, such as two different
MCP server entries in one aggregate config, may have different owners.

Global carrier relations use a separate carrier-claim registry. Several daem
manifests may be known consumers of one structural carrier, while each exact
relation claim still retains its own target, scope, source/provenance, route,
and owner identity. Neither registry proves ambient non-daem consumers or
exclusive host ownership.

`status` and `apply --dry-run` report `ownership_conflict` with the owning
manifest path before mutation. Removing the owning declaration and applying
that removal releases the claim only after the host and statefile changes
commit. Interrupted acquisition or release remains reserved or owned until
`daem recover` finishes the journaled transition; neither ordinary apply nor
`--manage-existing` steals it.

### Mutation Revision Evidence

Lock, authoring, refresh, apply, import, init, and unmanage retain filesystem
revisions for inputs that must remain current until their owned effect. Each
request explicitly selects bounded regular-file content, a shallow required-
absence entry, a bounded directory inventory, or bounded complete content.
Required absence never traverses an entry that appears. Directory inventory
observes immediate child names and kinds without opening descendants; selected
child content has its own complete-content evidence. Declaration files use
their 64 MiB regular-file boundary rather than recursive content evidence.
Extension inventory scans retain bounded regular-file content under the same
byte ceiling used by the corresponding host observer.

One revision observation pass admits at most 100,000 descendants and 64
descendant-directory levels in any one complete-content tree, and at most
400,000 descendant entries and 16 GiB of regular-file content across the pass.
Incremental observations that belong to one pass share the same aggregate
budget. Initial capture and every freshness check use the same policy. The
first over-limit probe returns a resource error; it does not produce a partial,
identity-only, or mtime-only revision. Cancellation is checked while
enumerating entries and streaming file bytes. These generic revisions do not
replace recovery's separate rooted physical-work authority.

## Recovery Journal

Mutating `apply` commits a complete recovery journal before it reserves a new
global claim or mutates host files or state. Under ordinary local-filesystem
process failures, `recover` can classify the journal and clean it up, roll back
guarded changes, or finish claim finalization after host and state commit.
The root authority used by an in-process plan and the root provenance persisted
for recovery are distinct facts. On Linux, ordinary planning uses the captured
root object and operation-local mount identity. Recovery provenance additionally
binds the kernel's unique mount identity to the current boot ID. A clean reboot
therefore creates no persisted boot dependency for a manifest, lockfile, or
newly constructed ordinary apply plan. An active journal from an earlier boot
is deliberately refused before recovery effects because its mount authority
can no longer be established.

Journal removal itself is also recoverable. Daem first publishes a correlated
retirement control, renames the exact active journal to a private residue,
advances the control to finalizing, removes the exact residue, and only then
retires the control to inert GC. A restart observes the durable phase rather
than trusting a prior command's success. The active directory identity selected
for execution and the exact published `journal.json` fingerprint form one
immutable execution basis. Daem revalidates both facts after every reload and
immediately before rollback, cleanup, or retirement effects. Identity or
content drift fails closed. As with other guarded filesystem effects, this is
not an atomic compare-and-rename
against a non-daem writer racing after the final validation.

Every journaled rooted removal also publishes one exact removal intent before
the first covered effect. The intent binds the portable scope and destination
to a relation-specific parent authority, two opaque same-parent names, and the
complete before/expected-after whole-path states that execution may remove.
The canonical intent is the composition of that transition-derived removal
demand and its namespace authority; cleanup obligations retain the complete
intent as their immutable basis rather than reconstructing a partial relation
key.

Each entry persists only the before and/or expected-after whole-path facts that
contribute to removal reachability. Every reload independently reconstructs the
canonical demand set from those transition facts and requires exact equality
with the persisted intents. Missing or surplus relations or states, malformed
hashes or modes, and noncanonical state ordering are rejected before cleanup or
retirement authority is constructed.

The residue name is valid only while the complete original state can still be
verified. After that verification, daem atomically promotes the entry to the
separate cleanup-stage name before recursively deleting it. The cleanup-stage
name is durable progress evidence, so a retry can continue after some children
were already removed without pretending that the partial tree still has the
original hash. Both names contain a preselected 128-bit opaque token. A
same-user process that reads an active journal and forges either exact private
name remains outside the local authority threat model; daem does not weaken
the exact-name contract by scanning a prefix. Aggregate contributors share the
document-level intent; the intent is never keyed by a resource name, target,
action ordinal, or content path. A replacement parent cannot inherit the old
authority. An existing parent must remain observable as the exact captured
object at its original path: once that path disappears, daem cannot distinguish
unlink from relocation and retains the journal. Its authority therefore stores
only the exact parent provenance and the two reserved names. An initially
absent parent is admitted only through its retained ancestor, missing suffix,
and reserved names without creating that parent during validation. The two
namespace variants reject each other's provenance fields.

Visible recovery classification and residue reconciliation are separate. A
journal may be `clean_before` or `clean_after` while a removal obligation is
still pending. Immediately before retirement, daem observes every complete
intent rather than only selected recovery entries. A matching residue is
promoted through a no-replace rooted rename and then removed through the exact
cleanup-stage protocol. A cleanup-stage entry resumes bounded recursive
cleanup; both names absent are confirmed only after three
nearest-existing-ancestor synchronization and re-observation attempts plus one
final exact re-observation. Both names
present, a changed namespace, a mismatched or unsupported residue, unavailable
evidence, or failed durability retains the journal and reports a typed blocker
or retry condition. Only after every obligation is discharged may the
retirement gate rename the active journal.

One journal may contain at most 4,096 removal intents and each intent carries
at most its before and expected-after whole-path states. Each journal planning
pass creates one operation-wide budget before observing current host paths,
recovery backups, or cleanup state; exact path and backup traversal work plus
cleanup assessment consume that same budget without reset. Global destination
binding, persisted-root and manifest-root recapture, and state or ownership
path-authority observation are included before their physical I/O; recovery
planning has no unbounded authority fallback. Each planning pass and bound
execution lifecycle is limited to
90,112 namespace/slot
observations, 524,288 root/path component visits, 400,000 recursive entry
visits, and 16 GiB of regular-file content. Alias resolution and physical-root
opening consume that component budget directly; a short alias cannot hide a
deeper authority path. One bound physical path may have at most 256 components.
Confirmed rollback keeps backup lease identity separate from backup content
freshness: it retains the active operation root, reserves every future rooted
read or snapshot before effects, and verifies exact work and content while
copying the backup. Generic mutation revisions never hash backup payloads.
Before the first recovery effect, the remaining traversal authority is split
into disjoint general-effect and removal-cleanup capabilities. Retained project,
global, active-journal, and ownership-registry authorities follow that phase
capability; they cannot fall back to ambient or unbounded path observation.
Every reachable forward-removal state is bounded before the first effect. The
currently visible state is measured through its retained rooted capability;
states that apply or recovery can create require a verified payload, backup, or
codec-bound producer certificate. Each state reserves its fresh pre-removal
observation plus every destination, namespace, and candidate path revalidation;
that capacity is transferred into a forward-only execution budget before the
recovery journal is published. Rooted removal also reserves storage's complete
cleanup envelope: snapshot capture, every pre-effect whole-tree seal,
destructive traversal, one possible overflow-name probe, and every
destination-parent chain revalidation. Storage consumes that same envelope
before touching the selected entry and rejects a tree that grows beyond its
fresh ceiling. Empty-entry proof capacity is a local reader bound and does not
count as positive semantic work or mutation
authority. Capacity for the extra directory name needed only to prove an
entry-count overflow is reserved before enumeration and charged when observed.
It never expands the admitted semantic tree. Storage
receives the exact observed cleanup ceiling, including zero, so content that
appears after re-observation is rejected rather than deleted. An incomplete bounded
observation whose exact work is unavailable conservatively consumes its
admitted maximum. The existing
per-tree limits of 100,000 entries, 64
descendant-directory levels, and 4 GiB remain independent fail-closed guards.
Correlation and coverage use canonical keyed indexes rather than repeated
intent scans.

Once the control-to-GC rename is durable, semantic recovery is complete.
Physical GC removal is best effort: interruption may leave private
retirement-control metadata, but not the retired journal or its backups. That
GC-only residue does not block later commands or recreate recovery authority,
and daem does not infer restart-time deletion authority from its name alone.
Failure after this boundary is reported as non-success without recommending
another recovery action: a new `recover` plan cannot be constructed from
GC-only residue.

Once the active journal has become retained cleanup residue, ordinary
workflows do not finalize it implicitly. `daem recover --dry-run` reports
`retained_cleanup_residue` with one `finalize_journal_cleanup` action.
Confirmed recovery then uses only recovery-root/control/residue authority; it
does not inspect or mutate host outputs, state, ownership, manifest, or
lockfile.

Pre-1.0 `.daem-tombstone-<32 lowercase hex>` directories use an obsolete
recovery-authority schema. Current daem recognizes the exact old name only to
block safely; it does not inspect, migrate, rename, or delete the directory.
Other names in the `.daem-tombstone-` namespace are blocked as malformed. Use
the daem version that wrote a valid old tombstone to finish recovery before
upgrading. Never discard one without independently confirming that no
interrupted apply or backup remains.

Stable-storage publication guarantees are platform-scoped. They preserve
durable evidence across an OS crash or power loss; they do not imply that the
current binary can automatically execute recovery after a reboot. Automatic
recovery is claimed for ordinary local-filesystem process failures within the
same boot. Compile-only and unsupported rows are not promoted into either
guarantee. On Linux, a kernel that cannot provide a unique mount ID may still
satisfy operation-local rooted-path checks, but daem refuses to publish a
recovery journal or begin its covered host effects. Before a provider
prerequisite can publish pending state or invoke the host, daem preflights the
durable manifest-root provenance that the later recovery journal requires.
After provider replan, final journal capture derives and validates it again.
See [Platform Support](platforms.md).
Daem refuses every selected manifest or lockfile larger than 64 MiB at the
physical read boundary, before decoding it, and applies the same limit to
in-memory decode inputs and generated manifest or lockfile output. Final
manifest or lockfile symlinks are admitted only while the selected link and
regular-file referent remain stable for the complete read. Metadata file-set
transactions also refuse any target after-image, captured before-image, or
restored backup larger than 64 MiB. These transaction limits bound physical
evidence; the statefile and carrier-registry codecs retain their stricter 16
MiB semantic limits. Daem refuses input statefiles larger than 16 MiB before
planning and refuses recovery journals larger than 64 MiB. Individual
regular-file recovery backups larger than 128 MiB are refused before the
covered host mutation; produced state and journal documents are also
size-checked before publication. Managed hook and MCP host documents are
limited to 4 MiB at observation, mutation, recovery, and codec-output
boundaries. Hook documents additionally admit at most 256 events, 4,096
groups, and 4,096 handlers, with event names limited to 256 bytes. Recovery
inventory admits at most 4,096
immediate recovery-root entries, 100,000 entries inside one journal directory,
and 64 entries inside one retirement control.
Managed directory snapshots are streamed but stop at 100,000 entries, 64
descendant-directory levels, or 4 GiB of observed regular-file content.
Retirement-control snapshots permit no descendant directory and at most 1 MiB
of regular-file content.

Before advancing a prepared retirement control or deleting the first residue
child, daem performs a complete metadata-only preflight with the same entry and
depth bounds. Every traversed directory must remain on the captured mount;
FIFO, socket, and device children fail closed without deleting an earlier
sibling. Symbolic links are never followed: cleanup revalidates and removes
only the link itself. Directory backup storage remains proportional to the
managed directory within these limits.

Recovery is intentionally narrow. It handles one active operation or the exact
retained cleanup selected for the manifest path set; it is not a snapshot,
profile, or historical restore surface.

## Managed Resource Types

### Instructions

Instruction resources render source files into target instruction files. The
default project outputs are:

- Codex: `AGENTS.md`
- Claude Code: `CLAUDE.md`
- OpenCode: `AGENTS.md`
- Pi: `AGENTS.md`
- Antigravity CLI: `AGENTS.md`

Global instruction outputs go under the target's global config root when the
target admits a global instruction placement row. Antigravity CLI global
instructions render to `~/.gemini/GEMINI.md`. Copy mode is executable today;
symlink mode is parsed but not yet executable.

Each instruction source locks one exact Supply subject and one managed-file
projection per distinct physical placement. Targets selecting the same file,
such as Codex and OpenCode project `AGENTS.md`, share one projection and one
managed-state row with the complete canonical consumer set. No consumer is
promoted to a primary target. Copy publication writes a private,
non-executable file; a source executable bit remains part of source identity
but does not leak into the managed instruction file.

### Skills

Skill resources install a full skill directory containing `SKILL.md`. The
default write roots are target-specific, for example `.agents/skills/<install-name>`
for Codex project skills and `.claude/skills/<install-name>` for Claude Code
project skills. `name` is the agent-visible directory and frontmatter name.
Where the target catalog exposes a compatible alternative, a per-target
`install_to` selects that root before `<install-name>` is appended. The request
does not admit arbitrary directories and does not change the target's
discovery or runtime catalog.
Copy placement is executable today. Skill `symlink` and `hardlink` placement
can be represented in desired state and lockfiles but `apply` rejects them
before host, state, or journal mutation.
When a separate daem resource key is needed, `id` supplies the lockfile/status
identity; otherwise the resource id defaults to `name`.

This distinction matters when two agents expose different skills with the same
directory name. They can use distinct `id` values while both install under the
agent-visible `name` that each host expects.

Targets whose profiles select the same placement share one physical skill
directory and one managed-state row. That row records the complete canonical
consumer set; no consumer is promoted to a primary target. Targets selecting
different placements produce independent paths and state rows, so applying or
removing one projection does not mutate its siblings.

Skill groups keep one source root and shared placement settings for several
child skills. Explicit `names` groups expand into ordinary per-skill resources
before lock, status, or apply. Selector-backed groups use explicit `glob:` or
`regex:` include selectors and optional excludes; they expand during `lock` into
ordinary per-skill lockfile entries with skill-group provenance. `status` and
`apply` use those locked entries instead of rediscovering upstream source roots.

### Hooks

Hook resources manage native Codex and Claude Code command hook configuration
aggregates. They do not fetch or install hook scripts. The configured command
must already be available to the target host.

For supported hook config files, `daem` owns only the `hooks` subtree and
preserves unrelated top-level settings.

### Hook Assets

Hook assets are explicitly declared, source-backed executable files referenced
from supported command hooks through `{hook_file:<name>}` placeholders. A hook
asset locks exact source identity and creates its own managed-file projection;
it does not turn arbitrary command strings into installable executables or infer
files from the host's `PATH`. Hook and asset scope must agree.

### MCP Server Bindings

MCP server declarations manage one supported host config binding for a
stdio launch vector. The locked command and arguments describe how the host
launches the server; they are not an executable provisioning plan. Removing the
declaration and applying the result removes only the managed config relation, not
packages, caches, credentials, trust state, logs, or other runtime residue.

Pi is the one current provider-mediated binding: an explicit `pi-package`
extension supplies the admitted MCP config capability, while the MCP server
subject still owns only its selected config contribution. Provider relation,
installed version, effective merged config, project trust, and runtime
readiness remain separate facts.

`daem probe mcp-server` can perform an explicit bounded runtime check for a
supported locked row. A prior successful probe or delegated attempt is historical
evidence, never authority to skip fresh observation or claim current readiness.
Current target and scope rows are listed in
[Feature Support](features.md).

### Extension Carriers

Extension declarations model a host-native plugin or package carrier relation,
not a cross-host artifact type. `add extension` and `remove extension` update the
manifest and lockfile only. For a supported lifecycle row, a later mutating
`apply` may delegate the locked install/create command to the host and record a
bounded attempt result.

Delegation does not make `daem` the owner of the host's package store and does
not prove exact installed version, enablement, trust, bundled contributions, or
runtime readiness. Explicit refresh is a separate operation. Removing a
declaration requests exact managed relation absence. For a currently supported
removal row, confirmed apply may invoke the host-native or direct-config route
only with durable exact management authority, fresh route evidence, and no
remaining daem-known shared consumer. `unmanage extension` retains host state,
and external-store prune remains a separate unsupported operation. See the
[Host Integration Contract](host-integrations.md) for the currently executable
host routes.

## Safety Model

The normal safe loop is:

```bash
daem lock --manifest daem.toml --dry-run
daem lock --manifest daem.toml
daem status --manifest daem.toml
daem apply --manifest daem.toml --dry-run
daem apply --manifest daem.toml --yes
```

When desired state is changed with `daem add` or `daem remove`, the
manifest and lockfile are updated together, so the loop can continue at
`status` or `apply --dry-run`. When `daem.toml` is edited directly or produced
by import, keep the explicit `lock --dry-run` and `lock` steps.

`lock --dry-run`, `status`, `apply --dry-run`, `doctor`, and `recover --dry-run`
are read-only with respect to host files. Mutating `apply` refuses unsafe plans
before writing, and mutating `recover` refuses blocked recovery plans.

### NFS-Backed Homes

`daem` is expected to work in ordinary single-host use when the home directory,
manifest, or source files are stored on NFS. NFS server, client, mount, locking,
cache, and failure semantics vary, however, so this is a best-effort environment
rather than a guarantee across every NFS deployment.

Do not rely on `daem` leases for cross-node mutual exclusion. A file changed
from another node is treated as a non-`daem` write: revalidation detects changes
visible before an effect starts, but cannot exclude a concurrent write after the
final check. Stable-storage durability and behavior during NFS outages or
reconnects are also outside the current guarantee. See the
[Safety Model](#safety-model), [Platform Support](platforms.md), and
[NFS troubleshooting guidance](troubleshooting.md#nfs-backed-home-or-workspace)
for the public boundaries.
