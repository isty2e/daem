# Contributing

Daem is pre-release. Open or update a GitHub issue before broad changes so the
intended behavior, dependencies, and verification evidence remain visible.

## Start

1. Read the [README](README.md), the [architecture contract](ARCHITECTURE.md),
   and the relevant page under [docs](docs/README.md).
2. Search existing GitHub issues before opening another.
3. Keep the change scoped to one behavior or contract.

Keep changes scoped. Public behavior changes require implementation, tests, and
the responsible user documentation to move together.

## Contract Ownership

- Canonical Go models and invariant-bearing tests own executable semantics.
- [Architecture](ARCHITECTURE.md) owns internal semantic-owner, compiler,
  transition, dependency-direction, and architecture-migration rules. It does
  not override narrower executable, persisted, or user-visible contracts.
- [Manifest](docs/manifest.md), [CLI](docs/cli.md),
  [host integrations](docs/host-integrations.md), and
  [platforms](docs/platforms.md) own their respective user-visible contracts.
  [Feature support](docs/features.md) is the user-facing summary of those
  contracts.
- [Examples](examples/) are copyable inputs subordinate to the manifest and CLI
  references; examples do not introduce syntax or support.
- CI and repository guards enforce these artifacts but do not create a second
  source of truth.

If implementation and documentation disagree, treat the mismatch as drift.
Determine which side is wrong, then update implementation, tests, and the
responsible public document together.

## Verify

Install pre-commit 4.6.0 or newer once per development environment, then enable
the repository hook:

```bash
pre-commit install
pre-commit run --all-files
```

Pre-commit owns repository hygiene, canonical Go formatting, module tidiness,
vet, semantic architecture checks, and test-harness checks when their owning
files change. Run focused tests while editing, then use only the lanes that
match the claim:

```bash
tools/test.sh focused ./internal/workflow/apply
tools/test.sh focused ./internal/workflow/apply 'TestName$'
tools/test.sh core
tools/test.sh repository
tools/test.sh tooling
tools/test.sh scale
tools/test.sh full
tools/test.sh race
go mod verify
git diff --check
```

Documentation-only changes do not require Go tests. Review the rendered
document, links, and diff directly; do not add tests that assert prose contains
or omits particular wording.

`focused` accepts one exact package and an optional top-level test-name regular
expression. It keeps a stable isolated root address so unchanged runs can use
Go's result cache, but removes the root after every execution. Go build inputs,
the tracked worktree diff, non-ignored untracked files, and environment values
read by tests invalidate the cached result. Ignored or external inputs are not
part of this claim, so `focused` is an iteration aid rather than a repository
correctness claim. `repository` checks semantic dependency and architecture
contracts. `tooling` checks the test runner itself and is required only when
its scripts or tests change. `scale` runs allocation and maximum-size resource
evidence; it is intentionally outside ordinary development feedback and runs
in CI. `core` is the normal multi-package development lane; it omits the real
Git backend and black-box CLI journeys. `full` adds those integration surfaces
and is the fresh hermetic product-and-CLI correctness claim;
repository, tooling, and scale evidence are intentionally separate.
`race` first proves the detector with an intentional race and then runs the
same product and CLI packages. The underlying full and race harness gives every
test package private user and XDG roots while reusing the
selected Go toolchain and existing build and module caches. It also ignores
host `GOENV`, `GOFLAGS`, and workspace selection, so local agent and Go
configuration cannot suppress or cross-contaminate mandatory tests. Inspect a
lane's package selectors with `tools/test.sh packages <lane>`.

When raising the Go toolchain, preview the standard modernizers with
`go fix -diff ./...`, review the proposed source changes, and run `go fix ./...`
to a fixed point before the full verification suite. A nonzero `go fix` result
or a repeated conflict warning requires review; it is not a formatting failure
to suppress.

Native platform and release claims require the lanes in
[Platform Support](docs/platforms.md) and the checked-in GitHub workflows.
Cross-compilation is not native execution evidence, and an unexecuted workflow
does not prove that a lane passed.
