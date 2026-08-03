# Contributing

Daem is pre-release. Open or update a GitHub issue before broad changes so the
intended behavior, dependencies, and verification evidence remain visible.

## Start

1. Read the [README](README.md) and the relevant page under [docs](docs/README.md).
2. Search existing GitHub issues before opening another.
3. Keep the change scoped to one behavior or contract.

Keep changes scoped. Public behavior changes require implementation, tests, and
the responsible user documentation to move together.

## Contract Ownership

- Canonical Go models and invariant-bearing tests own executable semantics.
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
vet, productive exported API reachability, and architecture/documentation
guards. The productive API check requires `jq`. Run focused tests while
editing, then use the remaining repository gates appropriate to the claim:

```bash
tools/test.sh focused ./internal/workflow/apply
tools/test.sh focused ./internal/workflow/apply 'TestName$'
tools/test.sh repository
tools/test.sh full
tools/test.sh race
go mod verify
git diff --check
```

`focused` accepts one exact package and an optional top-level test-name regular
expression. It keeps a stable isolated root address so unchanged runs can use
Go's result cache, but removes the root after every execution. Go build inputs,
the tracked worktree diff, non-ignored untracked files, and environment values
read by tests invalidate the cached result. Ignored or external inputs are not
part of this claim, so `focused` is an iteration aid rather than a repository
correctness claim. `repository` checks
architecture, documentation, and repository contracts. `full` is the fresh
hermetic repository-correctness claim, and `race` runs the same owned package
set under the race detector. The underlying full and race harness gives
every test package private user and XDG roots while reusing the selected Go
toolchain and existing build and module caches. It also ignores host `GOENV`,
`GOFLAGS`, and workspace selection, so local agent and Go configuration cannot
suppress or cross-contaminate mandatory tests. Inspect a lane's package
selectors with `tools/test.sh packages <lane>`.

When raising the Go toolchain, preview the standard modernizers with
`go fix -diff ./...`, review the proposed source changes, and run `go fix ./...`
to a fixed point before the full verification suite. A nonzero `go fix` result
or a repeated conflict warning requires review; it is not a formatting failure
to suppress.

Native platform and release claims require the lanes in
[Platform Support](docs/platforms.md) and the checked-in GitHub workflows.
Cross-compilation is not native execution evidence, and an unexecuted workflow
does not prove that a lane passed.
