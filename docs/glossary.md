# Glossary

This page is a navigation aid for terms used in daem's user documentation. It
does not define new manifest syntax or widen product support. Follow the linked
reference when exact behavior or schema matters.

## Admission

An admitted platform or route has a reviewed contract shape that daem may
reason about. Admission is not the same as product support: an admitted row may
still be diagnostic, deferred, unsupported, or blocked. See
[Platform Support](platforms.md) and the [Product Feature Matrix](features.md).

## Apply And Reconciliation

Reconciliation compares desired, locked, previously managed, and live state to
produce guarded actions. `daem apply` performs supported actions after review
or confirmation; it does not make every visible host artifact daem-owned. See
the [Safety Model](concepts.md#safety-model) and [`apply`](cli.md#apply).

## Carrier

A host-native plugin or package that can provide capabilities such as MCP
servers, skills, hooks, commands, or apps. A carrier relation can be managed or
diagnosed without daem owning the installed artifact or every contribution it
provides. See [Extension Carriers](concepts.md#extension-carriers).

## Contribution

One capability exposed by a carrier. A provider-scoped contribution remains
associated with its carrier; visibility does not turn it into a standalone
`[[skill]]`, `[[hook]]`, or `[[mcp_server]]` resource.

## Delegated Operation

A host-owned command that daem locks, discloses, and invokes for an admitted
route. Command success is attempt evidence, not proof of package identity,
runtime readiness, cleanup, or future convergence. See the target route
summaries in the [Product Feature Matrix](features.md).

## Desired State

The normalized intent read from `daem.toml`. Desired state says what should be
managed; it is distinct from resolved source identity, current host state, and
past attempt records. See [Manifest](concepts.md#manifest).

## Lockable Source

A source whose identity and content can be resolved into `daem.lock.toml`, such
as a local path, Git source, or supported archive/object source. Hook command
strings and host-native carrier operands are not automatically source payloads.
See [Sources](concepts.md#sources).

## Lockfile

`daem.lock.toml` records exact resolved source and operation identities for the
selected manifest. It does not record current host convergence or authorize
cleanup. See [Lockfile](concepts.md#lockfile).

## Managed Output

A host file, directory, or config projection for which the selected manifest
holds current daem ownership. Equal bytes alone do not establish ownership.
Use `daem list outputs` to inspect outputs and conflicts. See
[Statefile](concepts.md#statefile).

## Manifest

`daem.toml`, the public desired-state input. The
[Manifest Reference](manifest.md) is the authoritative schema users may write.

## Manage Existing

`apply --manage-existing` registers an already present output or supported
config projection only when it exactly matches locked desired content and
required metadata. For supported external carriers, it can also acquire one
state-only claim for an already declared, locked, source-exact relation after
full lifecycle admission. Neither branch overwrites drift, steals another
manifest's claim, invokes a host install command for adoption, or accepts
approximate host state. See [`apply`](cli.md#apply).

## Projection

One managed placement of a resource into host-visible state, such as an
instruction file or one MCP config entry. Several resources may share a
physical aggregate while retaining separate managed contributions.

## Relation

A modeled association between daem intent and host-visible state, such as one
plugin selector being installed for a target and scope. Relation presence is
separate from exact artifact identity, enabled state, runtime readiness, and
destructive cleanup authority.

## Recovery Journal

The durable record written before a mutating apply operation changes covered
host or ownership state. `daem recover` classifies and resolves one interrupted
operation. See [Recovery Journal](concepts.md#recovery-journal).

## Runtime Probe

An explicit active check that may launch a locked MCP command. It is separate
from passive `status` and `doctor` checks and does not change manifest, lock,
state, or host configuration. See [`probe mcp-server`](cli.md#probe-mcp-server).

## Scope

The project or global placement and authority boundary for a resource. Global
effects require an explicit row where the manifest contract requires one. A
project declaration never authorizes deleting global state. See
[Targets and Scopes](concepts.md#targets-and-scopes).

## Statefile

Private authority data recording which live outputs a selected manifest
previously wrote or registered. Historical state is not fresh proof that the
host remains unchanged. See [Statefile](concepts.md#statefile).

## Subject

The stable identity used to report and correlate one lock, projection,
relation, or resource fact. A physical config file can contain several subjects.

## Target

The agent host selected for a resource, such as Codex, Claude Code, OpenCode,
Pi, or Antigravity CLI. Target support is always qualified by surface, scope,
and operation. See [Targets and Scopes](concepts.md#targets-and-scopes).
