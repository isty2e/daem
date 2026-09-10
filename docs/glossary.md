# Glossary

Look up unfamiliar output terms here; linked references give the full rules.

## Apply And Reconciliation

Reconciliation compares desired, locked, managed and live state;
[`apply`](cli.md#apply) performs the supported guarded actions.

## Carrier

A host-native plugin/package, distinct from ownership of its store or bundled
features; see [Extension Carriers](concepts.md#extension-carriers).

## Contribution

A carrier-provided capability, not automatically a standalone daem resource;
see [provider diagnostics](host-integrations.md#provider-scoped-contribution-diagnostics).

## Delegated Operation

A locked, disclosed host command whose successful attempt need not prove
convergence; see [Host Integrations](host-integrations.md).

## Desired State

What the [manifest](concepts.md#manifest) asks daem to manage, not current host state.

## Lockable Source

Content whose identity can be resolved into the lockfile; a command or carrier
operand is not automatically a payload. See [Sources](concepts.md#sources).

## Lockfile

[`daem.lock.toml`](concepts.md#lockfile) records resolved source/operation identity,
not live ownership or convergence.

## Managed Output

A file, directory or config projection owned by the selected manifest;
[`list outputs`](cli.md#list) shows it, and equal bytes alone do not confer ownership.

## Manifest

[`daem.toml`](manifest.md) is the public desired-state input.

## Manage Existing

[`apply --manage-existing`](cli.md#apply) registers eligible exact-match outputs
or source-exact carrier relations without overwriting mismatches or stealing claims.

## Projection

One host-visible placement or config contribution; several can share one physical
aggregate. See [Statefile](concepts.md#statefile).

## Relation

A selected host association, such as an installed plugin selector; presence is
not ownership, exact artifact identity or readiness. See [Host Integrations](host-integrations.md).

## Recovery Journal

The durable record for resolving one interrupted journaled apply;
see [Recovery Journal](concepts.md#recovery-journal).

## Runtime Probe

An explicit active MCP check, separate from passive status/doctor;
see [`probe mcp-server`](cli.md#probe-mcp-server).

## Scope

The [project/global boundary](concepts.md#targets-and-scopes); project declarations
do not authorize global deletion.

## Statefile

[Private authority data](concepts.md#statefile) recording previously managed
outputs, not a fresh observation of them.

## Subject

A stable identity for one lock, projection, relation or resource fact; a physical
config may contain several. See [Lock Behavior](manifest.md#lock-behavior).

## Target

The agent host selected for a resource; support depends on surface, scope and
operation. See [Feature Support](features.md).
