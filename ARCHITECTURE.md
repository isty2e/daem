# Daem architecture

The [LikeC4 model and views](architecture/README.md) describe the current Go
architecture: domain identities, responsibility boundaries, and execution paths.

From the repository root:

```sh
likec4 start architecture
```

Start with **Daem in context** or **Domain model overview**. For product
behavior, see [OpenSpec](openspec/README.md); for checks, see
[Contributing](CONTRIBUTING.md#verify-specifications-and-architecture).

## Operation-Safety Compiler

Open **Operation-Safety and the State Barrier**. The
[model](architecture/model.c4) describes the operation identities, reservations,
and retained ownership boundaries; the [Apply views](architecture/apply.c4) show
where they are used.

## Platform Maintenance Decisions

See **Platform admission and provenance** in the [model](architecture/model.c4)
for the retained Darwin decision and the distinction between platform tests
and admission. For supported use, see [Platform and distribution](openspec/specs/platform-and-distribution/spec.md).

## Stored Skill Repair Contract

See [Skill compatibility](openspec/specs/skill-compatibility/spec.md#requirement-replay-the-stored-transformation-and-verify-its-identities)
for the input, output, and replay rules, and the
[lockfile codec](internal/realization/lockfile/repair_conversion.go) for their encoding.
