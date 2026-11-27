# Source acquisition

## Purpose

Define the supported ways to identify an input artifact, the boundary of the artifact, and what daem may reuse. The [source parser](../../../internal/supply/source), [Git backend](../../../internal/supply/source/backend/gitcli), and [S3 backend](../../../internal/supply/source/backend/s3object) implement these contracts.

## Requirements

### Requirement: Admit only the supported Git locator forms

Daem SHALL validate nonempty UTF-8 locator, path and ref text without surrounding whitespace or Unicode control or format characters, including after decoding a URL path. Locators cannot start with `-` or use `::` helper syntax. URL forms do not admit query or fragment fields. HTTP and `git://` are not admitted supply transports. Git/SSH credentials stay outside the declaration and lock; local `mode` is not a Git field.

| Form | Admission |
| --- | --- |
| `https://host/path` | Host and non-root repository path; no userinfo, including username-only. |
| `ssh://[user@]host[:port]/path` | Host and non-root repository path; an optional nonempty username, no password. |
| `[user@]host:path` | Nonempty host and path; optional nonempty user; no slash before the first colon, whitespace, extra `@`, or option-shaped host. |
| `file:///absolute/path` | No host or userinfo; decoded path must be absolute. |
| Native absolute path | Resolved with the native lexical cleaner; no relative repository locator or `~` expansion. |

#### Scenario: HTTPS includes a username

- **WHEN** a locator resembles `https://user@example.org/repo`
- **THEN** daem rejects it even though it includes no password

### Requirement: Resolve a Git selector to one exact artifact

The artifact `path` SHALL be clean POSIX, slash-separated, relative and non-escaping; `.` selects the root and backslashes are rejected. Daem rejects Git exports containing links or escaping entries. A ref must be an unqualified branch/tag that matches exactly one candidate, a qualified `refs/heads/` or `refs/tags/` name, or a full 40- or 64-hex object ID. Abbreviations, pseudo-refs, revision expressions, refspecs and option-shaped selectors are rejected.

Symbolic names exclude leading or trailing `/`, `//`, `..`, `@{`, whitespace, `~`, `^`, `:`, `?`, `*`, `[`, backslash, trailing `.`, components starting with `.`, and components ending in `.lock`. The stored Git identity is `git:locator=<query-escaped-locator>&path=<query-escaped-path>&ref=<query-escaped-canonical-selector>` in that order; selectors use `name:`, `branch:`, `tag:` or `commit:` with a lower-case full object ID. The resolved commit is separate. Older delimiter-based Git identities require lock regeneration, not an alias.

#### Scenario: A branch and a tag share a name

- **WHEN** an unqualified Git ref resolves to both a branch and a tag
- **THEN** daem does not choose one; the caller can qualify the branch or tag

#### Scenario: A floating declaration advances

- **WHEN** the same admitted branch selector resolves to a new commit on a later lock
- **THEN** the source identity stays the same while `resolved_ref` records the new commit

### Requirement: Observe Git format at the declared source

Daem SHALL contact only the declared locator for network format observation. An enclosing repository's URL rewrite cannot choose another repository for that observation. Source format does not come from `GIT_DEFAULT_HASH`. Daem can still use SHA-1 with Git that lacks explicit format selection; a Git without SHA-256 support cannot resolve a SHA-256 source.

#### Scenario: A surrounding Git repository defines a URL rewrite

- **WHEN** a rewrite would redirect format observation to a different repository
- **THEN** format observation does not contact that substitute

### Requirement: Resolve local and S3 inputs without mixing source kinds

Local sources SHALL resolve by filesystem path. Project-relative paths resolve from the selected manifest's directory; global local sources require absolute paths. The mode describes the source, not permission to install a symlink. [Skills](../skills/spec.md), [instructions](../instructions/spec.md) and [hook assets](../hooks/spec.md) define which source forms they accept.

Current local source support does not include a symlink as the selected artifact or inside it, including for `mode = "link"`. Ordinary lookup can resolve ancestor symlinks. This limitation is separate from the options for importing an existing host installation.

An S3 URI `s3://bucket/key` selects one object, not a prefix or bucket. Reject query strings, fragments, embedded credentials, empty keys and keys ending in `/`. The source can request `version_id` and override `region`; otherwise region and credentials come from the AWS SDK configuration. Formats are `file` (default), `tar`, and `tar.gz` (`tgz` is an alias). Do not mix values from different source kinds; unused empty fields that the decoder treats as absent do not select another kind.

`resolved_ref` records the returned VersionId, or the requested ID if none is returned. Always record the materialized content hash. An exact verified hit for an explicitly versioned source can be resolved without AWS configuration, credentials or network access. Unversioned sources refetch on a later resolution. An ETag alone is not immutable identity.

#### Scenario: A versioned object has a verified exact cache hit

- **WHEN** the record matches the requested source/version and the cached artifact bytes verify
- **THEN** daem can resolve that version from the cache without consulting AWS

#### Scenario: A global local skill names a relative source

- **WHEN** a global skill uses a project-relative local source path
- **THEN** daem rejects it instead of resolving it differently according to the caller's directory

### Requirement: Enforce source budgets on data, not just reported sizes

The [direct-file limit](../../../internal/supply/source/directfile/directfile.go) SHALL apply during source resolution, lock, status and apply, including local files, plain S3 objects, instruction files and hook assets. Known sizes provide early rejection evidence, not a substitute for bounded hash, read or download. Archive budgets are separate, not a larger allowance for plain objects. The current published limits are:

| Quantity | Limit |
| --- | ---: |
| Direct file or individual archive file | 128 MiB |
| Raw tar or gzip input, including trailing data | 256 MiB |
| Decompressed tar stream | 768 MiB |
| Total regular-file bytes extracted | 512 MiB |
| Logical entries and extracted files/directories, separately | 100,000 each |
| Canonical archive path | 4,096 bytes and 64 components |

Implicit parents count toward the extracted-entry budget; cache wrapper and completion metadata use separate capacity. Archive safety checks reject symlinks, hard links, special files, traversal, backslashes and parent segments independently of size. A refused artifact is not truncated into a valid one and does not publish cache completion, a new lock result, or host effects.

#### Scenario: A plain object exceeds the direct-file limit

- **WHEN** a streamed plain object exceeds the regular-file limit although its advertised size did not
- **THEN** daem refuses it without using the larger archive allowance or publishing partial completion

#### Scenario: Trailing tar data runs over the input budget

- **WHEN** all tar entries fit but bytes after the end marker exceed the raw input budget
- **THEN** extraction fails instead of accepting the fitting prefix
