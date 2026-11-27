# Source acquisition

## Purpose

Define the supported ways to identify an input artifact, the boundary of the artifact, and what daem may reuse. The executable [source grammar](../../../internal/supply/source), [Git backend](../../../internal/supply/source/backend/gitcli), and [S3 backend](../../../internal/supply/source/backend/s3object) own exact parsing, canonical encodings, and acquisition policy.

## Requirements

### Requirement: Keep Git locators, artifact paths, and selectors distinct

Daem SHALL admit credential-free HTTPS, SSH URLs with at most username userinfo, Git scp-like locators, hostless absolute `file:///` URLs, and native absolute repository paths. It SHALL reject unsafe or unmodeled forms including HTTP, `git://`, remote helpers, URL passwords, and query or fragment fields. Locator, path and ref text must satisfy the [grammar](../../../internal/supply/source/git.go), including the checks on decoded URL paths. Git/SSH credentials stay outside the declaration and the lock. A Git source cannot set local `mode`.

The repository-relative POSIX `path` selects only the artifact to export, at the commit resolved from `ref`. It must be clean, use slash separators, and remain inside the repository; `.` selects the root. Archive links and escaping entries do not become artifacts. A ref is an unambiguous branch/tag, `refs/heads/` or `refs/tags/` name, or full 40- or 64-hex object ID. Abbreviations, pseudo-refs, revision expressions, refspecs and unsafe symbols are not equivalent selectors. The canonical source identity includes locator, path and selector, not the resolved commit. Older delimiter-based Git identities require lock regeneration; there is no alias.

Daem uses system Git with the required object-format support: SHA-1 can use older Git without `--object-format`, SHA-256 requires it, and a capable Git receives the explicitly selected format. The caller's `GIT_DEFAULT_HASH` does not choose it. Remote format observation runs in a rooted repository context rather than allowing an enclosing repository's URL rewrite to redirect that observation.

#### Scenario: A branch and a tag share a name

- **WHEN** an unqualified Git ref resolves to both a branch and a tag
- **THEN** daem does not choose one; the caller can qualify the branch or tag

#### Scenario: A floating declaration advances

- **WHEN** the same admitted branch selector resolves to a new commit on a later lock
- **THEN** the source identity stays the same while `resolved_ref` records the new commit

### Requirement: Resolve local and S3 inputs without mixing source kinds

Local sources SHALL resolve by filesystem path. Project-relative paths resolve from the selected manifest's directory; global local sources require absolute paths. An admitted local source declares `vendor` or `link`; a project-scoped link skill or skill group requires `portable = false`. This describes the source, not permission to install a symlink. Resource-specific forms still apply: for example, an instruction can specify a local file directly, a skill uses a structured source, and an opaque hook command is not a source declaration.

An S3 source selects one object, not a prefix or bucket. It may request one `version_id` and supply a `region` override; otherwise region and credentials come from the AWS SDK configuration. Object formats are `file` (default), `tar`, and `tar.gz` (`tgz` is an alias). Reject invalid locators and fields for another source kind. Record a returned VersionId as `resolved_ref` and the materialized content hash even when no VersionId is returned. An explicitly versioned source may reuse a verified cache entry without creating an AWS client; unversioned sources refetch on a later resolution. An ETag alone is not immutable identity.

#### Scenario: A versioned object has a verified exact cache hit

- **WHEN** the record matches the requested source/version and the cached artifact bytes verify
- **THEN** daem can resolve that version from the cache without consulting AWS

#### Scenario: A global local skill names a relative source

- **WHEN** a global skill uses a project-relative local source path
- **THEN** daem rejects it instead of resolving it differently according to the caller's directory

### Requirement: Enforce source budgets on data, not just reported sizes

Daem SHALL enforce the [direct-file limit](../../../internal/supply/source/directfile/directfile.go) during hash, exact read and download, including plain S3 file objects. Archive input, decompressed stream, expansion, single-file, logical entry, materialized tree and path limits are separate and owned by the [archive extractor](../../../internal/supply/source/archive/budget.go). Included in the archive accounting are implicit parent directories and raw tar bytes after the end marker; cache wrappers and completion metadata have separate capacity. Archive safety checks reject links, special files, traversal, backslashes and parent segments independently of size.

Declared sizes are useful for early rejection, but the limits also apply to what arrives. A refused input is not truncated into a valid artifact and does not publish a cache completion, new lock result, or host mutation.

#### Scenario: A plain object exceeds the direct-file limit

- **WHEN** a streamed plain object exceeds the regular-file limit although its advertised size did not
- **THEN** daem refuses it without using the larger archive allowance or publishing partial completion

#### Scenario: Trailing tar data runs over the input budget

- **WHEN** all tar entries fit but bytes after the end marker exceed the raw input budget
- **THEN** extraction fails instead of accepting the fitting prefix
