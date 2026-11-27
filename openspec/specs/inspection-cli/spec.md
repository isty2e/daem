# Inspection commands

## Purpose

Define which facts a query returns and what its result means. A declared item, a known location, a current managed output, and a live runtime result are different observations. The [CLI presenters](../../../internal/cli/present) implement exact fields and rendering.

## Requirements

### Requirement: Report executable identity offline

`version` SHALL report embedded identity without selecting a workspace, checking the running platform, resolving sources or launching a host CLI. It exposes the module version, full revision, revision time, VCS, source state, Go toolchain and build OS/architecture as applicable to the result. Missing facts remain unknown; a pseudo-version is not promoted to a release. Revision time is source provenance, not build time. Embedded identity does not prove verification, trusted publication or checksum validity.

#### Scenario: Run a development build without a manifest

- **WHEN** `daem version --json` runs outside a workspace
- **THEN** it reports the executable's available build facts rather than requiring an installed project or official release tag

### Requirement: Compare lock identities without confusing them with effects

`lock` and `outdated` SHALL report comparable identity changes using their shared [lock-comparison family](../../../internal/cli/present/lock_json.go). Distinguish `comparable` from `missing` (an empty comparison baseline), `legacy` and `damaged` (new counts without comparison; comparison members are `null`). `outdated` uses only comparable or missing evidence and compares valid schema 6 without rewriting it. `--check` fails if the new exact identities would change. An old or damaged file's replacement is admitted only by the [lockfile rules](../lockfile/spec.md).

Identify named resource and placement without inventing a primary target for shared consumers or a target for exact source-only facts. Reasons distinguish exact source ID, resolved ref, content kind and hash in order, use broader exact Supply change when a facet cannot be decomposed, and retain separate derivation, environment, provider and other contract changes. Report changed order classes without sorting their members by declaration name. Default human output omits unchanged details; verbose adds bounded safely disclosed evidence. Grammar-proven public carrier identities can remain literal; local or opaque ones are deterministically redacted with their marker. An exact mode of zero is not omission. No comparison result promises a host write or predicts one solely from a changed source hash.

#### Scenario: A readable schema 6 lock has no source change

- **WHEN** `outdated --check` compares a valid old snapshot against the same exact facts
- **THEN** the old readable wire version alone is not an outdated source; with no exact identity change, the check exits 0 without writing the lock

### Requirement: Count declarations separately from outputs and paths

`list resources` SHALL use the selected normalized manifest without resolving sources or requiring a lock. It reports declaration keys, installed names, scope, targets and known explicit skill-group membership, not discovered selector matches. The group position is not a persistent ID. Agent-config declarations and their keys are not resource rows. Hook assets are listed once, using the full declared reference-target union; a target filter selects by overlap without shortening the union, and unreferenced assets remain visible with an empty target value. No resource row itself grants an add/remove command or executable route.

`list outputs` reports independent managed, unmanaged and ownership-blocked rows from state, claims and selected live paths/documents. It does not resolve source updates or probe a host, provider or order. A whole shared document has no aggregate row hash. Codec evidence is required for an unmanaged contribution; a document blocker does not invent an existing sibling. Shared paths retain their complete consumer set. A managed copied-skill hash covers shipped content; an unmanaged tree is examined as a whole. Native key names are arrays of literal segments, not aggregate pointers. Return all selected inventory rows, not a truncated convergence report.

`list paths` describes admitted default/selected writes, discovery, runtime, configured and delegated locations for the modeled scopes. It derives selection from catalogs and declarations without checking existence, contents, readiness or writability. Discovery is not write authority and unsupported requests do not imply generic overrides. The flat JSON location row has one applicable payload: path, route with operation, or unsupported reason/details. Its ordering follows the human tree. Actual installed hook files can differ from the portable location, as with global Claude bindings.

#### Scenario: A shared path has a single target filter

- **WHEN** one consumer matches the selected target for a shared managed output
- **THEN** the row remains selected and still reports its complete consumer set

### Requirement: Report status and check cleanliness independently

A valid `status` report SHALL succeed even when its rows report blockers. `--check` succeeds only for a present lock, known-clear owner permission protection, no pending or blocked output, no pending native narrowing, exact extension order, and no blocked relation, adoption or extension-removal action. Merely selected missing relations and observe-only relations do not fail the check. Error-grade diagnostics contribute to `has_errors` but do not independently determine `check_clean`. Human and JSON use the same check predicate. Both status JSON forms carry `check_clean`; apply preview omits it. A clean status is not runtime readiness, permission to apply or a prediction that a process will succeed.

Status does not mutate managed settings or state. It can observe the bounded Pi Native version when required, without invoking package inventory queries, installation or servers. `host_prerequisite` names a separate dimension from Adapter `provider_prerequisite`; non-Native rows report the former as `not_applicable`. The [Native reason codes](../../../internal/assurance/observe/mcp/host_prerequisite.go) distinguish unqualified version, unobserved settings, disabled or unobserved builtin, and configured Adapter intent. A Native blocker is one binding's typed evidence, not a reason to drop other targets. Unsupported effective observation is distinct from a supported observer that could not observe; neither proves no shadow. Attempt history is not current relation or runtime proof. An observation, usage or delivery failure is not a valid assessment.

#### Scenario: A plan is pending but has no errors

- **WHEN** a valid status report has one pending output, with no error-grade diagnostics
- **THEN** ordinary status can succeed, `--check` fails, and JSON distinguishes `check_clean: false` from `has_errors: false`

### Requirement: Present scoped facts without inflating counts

Status and apply presentations SHALL count managed paths once, including shared outputs, and count aggregate subjects independently even in one file. Output actions, routes, order, adoption, removal, and permission facts remain distinct. Human output groups by public resource/target/scope with stable tie ordering; arrays whose ordering expresses execution or precedence retain it. Routine current/no-op rows can be compacted, while blockers, uncertain attempts, changing terms and required work remain visible. Compact output does not omit detailed admitted facts from verbose or JSON.

Owner `permission_protection` is independent of `--target`. An omitted field can mean clear or unchecked. Observed pending records, failed observation and unavailable platform support are different from no records. A pending owner record makes `--check` nonclean without proving that an unrelated change would be blocked. Its `next_step` points to a recovery preview with the same manifest; it [does not authorize recovery](../cli-protocol/spec.md#requirement-preserve-argument-identity-without-disclosing-unsafe-input). Same-name retained skill discovery warnings do not claim which copy the host loads or authorize its removal.

#### Scenario: Several settings share a physical document

- **WHEN** one shared document has two distinct managed config subjects and one blocked subject
- **THEN** the report retains each subject's own status rather than counting the file as one action or labeling every key changed

### Requirement: Run passive diagnostics without invoking the host

`doctor` SHALL check selected effective capabilities with a manifest or general prerequisites without one; `--all-targets` and explicit `--target` are alternatives. It never launches host CLIs, credential helpers, package managers, plugins or runtime/network probes. It can use bounded disposable scratch files or trees on admitted platforms; existing outputs and managed state are unchanged, and this does not prove concurrent exclusion or durability. An admitted active apply interruption blocks before host inspection. Unsupported platforms retain the platform finding and only checks with unchanged meaning.

Typed check status differs from ordinary diagnostic severity. Human default output lists warnings, errors, skipped and unsupported checks and counts successes; verbose expands success evidence. Warnings/skips/unsupported checks alone do not fail the command. Errors fail. JSON retains all typed checks and the separate reported error status.

#### Scenario: A storage scratch check succeeds

- **WHEN** doctor verifies a disposable operation on an admitted root
- **THEN** it can report that operation's result without claiming concurrent-writer or post-crash recovery guarantees
