# Contributing

Daem is pre-release. Open or update a GitHub issue before broad changes so the
intended behavior, dependencies, and verification evidence remain visible.

## Start

1. Read the [README](README.md) and the relevant [documentation](docs/README.md).
   Read [Architecture](ARCHITECTURE.md) when changing implementation structure.
2. Search existing GitHub issues before opening another.
3. Keep the change scoped to one behavior or contract.

Public behavior changes require implementation, tests, and the responsible
user documentation to move together.

## Contract Ownership

| Change | Owning contract |
| --- | --- |
| Executable semantics | Canonical Go models and invariant-bearing tests |
| Internal owners, compilers, transitions and dependencies | [Architecture](ARCHITECTURE.md), without overriding narrower contracts |
| Public syntax, commands, host effects or platforms | [Manifest](docs/manifest.md), [CLI](docs/cli.md), [Host Integrations](docs/host-integrations.md), [Platforms](docs/platforms.md) |
| Summaries and examples | Derived from those contracts; do not introduce syntax or support |

CI and guards enforce contracts; they do not create a second authority.
If implementation and documentation disagree, treat the mismatch as drift.
Determine which side is wrong, then update implementation, tests, and the
responsible public document together.

## Build From Source

Install the Go toolchain specified in `go.mod`, then build from the repository
root:

```bash
go build -mod=readonly -o ./bin/daem ./cmd/daem
./bin/daem version
```

See [installation from source](docs/install.md#build-from-source) for user setup.

## Verify

Install pre-commit 4.6.0 or newer once per development environment, then enable
the repository hook:

```bash
pre-commit install
pre-commit run --all-files
```

Hooks check hygiene, canonical Go formatting, module tidiness, vet, architecture
and changed test-harness files. Select test lanes by the change and claim:

| Command | Coverage |
| --- | --- |
| `tools/test.sh focused ./internal/workflow/apply 'TestName$'` | One exact package and optional top-level test regex; omit the regex for the package. |
| `tools/test.sh core` | Normal multi-package feedback, excluding real Git backend and black-box CLI journeys. |
| `tools/test.sh full` | Fresh hermetic product/CLI checks, including those integration surfaces. |
| `tools/test.sh race` | Detector proof followed by the same product/CLI packages. |
| `tools/test.sh repository` | Semantic dependency and architecture contracts. |
| `tools/test.sh tooling` | Test-runner checks when its scripts or tests change. |
| `tools/test.sh scale` | Allocation and maximum-size cases; separate from ordinary feedback, run in CI. |
| `go mod verify` | Module-cache integrity. |
| `git diff --check` | Whitespace errors. |

Documentation-only changes do not require Go tests. Review the rendered
document, links, and diff directly; do not add tests that assert prose contains
or omits particular wording.

`focused` caches unchanged runs using Go inputs, tracked diff, non-ignored
untracked files and test-read environment values, not ignored/external inputs.
It is an iteration aid, not full correctness evidence. Full/race isolate each
package's user/XDG roots and ignore ambient `GOENV`, `GOFLAGS` and workspace
selection. Repository, tooling and scale checks remain separate. Inspect selected
packages with `tools/test.sh packages <lane>`.

When raising the Go toolchain, preview the standard modernizers with
`go fix -diff ./...`, review the proposed source changes, and run `go fix ./...`
to a fixed point before the full verification suite. A nonzero `go fix` result
or a repeated conflict warning requires review; it is not a formatting failure
to suppress.

Native platform and release claims require the lanes in
[Platform Support](docs/platforms.md) and the checked-in GitHub workflows.
Cross-compilation is not native execution evidence, and an unexecuted workflow
does not prove that a lane passed.
