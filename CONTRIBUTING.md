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
  [features](docs/features.md), and [platforms](docs/platforms.md) own their
  respective user-visible contracts.
- [Examples](examples/) are copyable inputs subordinate to the manifest and CLI
  references; examples do not introduce syntax or support.
- CI and repository guards enforce these artifacts but do not create a second
  source of truth.

If implementation and documentation disagree, treat the mismatch as drift.
Determine which side is wrong, then update implementation, tests, and the
responsible public document together.

## Verify

Run focused tests while editing, then use the repository gates appropriate to
the claim:

```bash
go run mvdan.cc/gofumpt@v0.10.0 -w .
go run mvdan.cc/gofumpt@v0.10.0 -w .
test -z "$(go run mvdan.cc/gofumpt@v0.10.0 -l .)"
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go mod tidy -diff
go mod verify
git diff --check
```

When raising the Go toolchain, preview the standard modernizers with
`go fix -diff ./...`, review the proposed source changes, and run `go fix ./...`
to a fixed point before the full verification suite. A nonzero `go fix` result
or a repeated conflict warning requires review; it is not a formatting failure
to suppress.

Native platform and release claims require the lanes in
[Platform Support](docs/platforms.md) and the checked-in GitHub workflows.
Cross-compilation is not native execution evidence, and an unexecuted workflow
does not prove that a lane passed.
