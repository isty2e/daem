# CLI protocol

## Purpose

Define command selection, interaction, output, and compatibility across daem CLI operations. The [command dispatcher](../../../internal/cli/run.go) and leaf parsers own exact accepted invocations. The [CLI Reference](../../../docs/cli.md) shows the forms and help topics. Command-specific result meanings are defined at their respective operations.

## Requirements

### Requirement: Resolve the named command before selecting its output mode

Daem SHALL support scoped help from the root, group, or leaf without selecting a workspace. Help is human output and takes precedence in its admitted forms. `--version` is an exact root alias with no additional arguments and no `-v` alias. Bare groups are not successful queries. Unknown routes do not gain JSON output, and `update` is not an alias for a host refresh.

Selectors use one value per token, not comma lists. Where a selector is repeatable, duplicates collapse after validation. Ordered MCP `--arg` values are data, not a selection set. Execution filters the effective declared targets; it does not add authorization or broaden the declaration. A `--` separator allows an admitted literal operand without treating it as an option.

#### Scenario: Seek help without a workspace

- **WHEN** the caller requests `daem help apply` without a manifest
- **THEN** scoped human help succeeds without resolving workspace state

### Requirement: Require the mode appropriate to the effect

Read-only query leaves SHALL reject effect flags they do not admit. Ordinary authoring and lock leaves default to writing, allow preview, and do not require `--yes`. Host/runtime and selected management effects use explicit preview, authorized terminal execution or admitted `--yes`. `import agent-config` and `replace agent-config` require the reviewed `--expect-plan` for noninteractive writes; an ordinary `--yes` alone is not enough for those leaves. Mode permission remains leaf-local.

For interactive authorization, stdin, stdout and stderr must all be terminals. Finish the disclosure to stdout before printing the prompt to stderr. Accept only `y` or `yes`; EOF, interruption and output failure cannot approve. JSON does not share an interactive prompt. Some no-effect plans skip prompting, but do not bypass command admission or authorize recovery work that was not disclosed.

#### Scenario: Redirect stdout on a bare effect command

- **WHEN** an effect command has no noninteractive authorization and its stdout is redirected
- **THEN** daem does not approve by moving the disclosure to another stream or by skipping confirmation

### Requirement: Keep presentation and executable authority separate

Presentation flags SHALL change disclosure, not selection, planning, authority or effects. JSON excludes `--verbose` and `--diff`; an admitted diff flag requires `--dry-run` and may accompany `--verbose`. Each supporting leaf owns its choices, including selected native-value previews and metadata-only releases. Only `status` and `outdated` use `--check`; it affects the exit decision, not the ordinary result.

The human presentation retains relevant blockers, effects, destructive implications, uncertainty and actionable guidance. Relation no-ops do not describe every secondary host effect. Verbose adds bounded typed evidence and omits unprojectable or incomplete error detail rather than emitting arbitrary raw errors. The [presenter](../../../internal/cli/present) owns quoting, evidence limits, redaction and disclosure rules. Human syntax error locations use one-based line and column when available; unknown keys identify the authored expression using rune columns. Source or placement advice does not perform a repair. An environment failure during path/host-load identity resolution is not automatically invalid declaration input.

#### Scenario: Request a diff in a JSON result

- **WHEN** a supported leaf receives both `--json` and `--diff`
- **THEN** it refuses the conflicting presentations rather than merging a text diff into JSON

### Requirement: Preserve argument identity without disclosing unsafe input

Concrete next commands SHALL preserve the selected intent and reconstruct admitted POSIX argument vectors as single arguments, even for escaped controls. An unrepresentable or unsafe operand yields a template or instruction instead. Equivalent cwd selection can elide `--manifest`; explicit selection cannot be switched to implicit user fallback. Creation hints use creation selection. A reminder to rerun is not an exact replay of the entire command.

Dynamic text does not own line breaks or output structure. Bound and escape path and error text and omit raw subprocess/protocol output and protected values as defined by the owning result. Rejected S3 URI and MCP transport values are not repeated; S3 parsing does not leak the underlying URL parser's error. Redact explicitly supplied secrets and supported credential syntax. Do not promise recognition of every unknown secret or encoding. A scanner no-match is not evidence that a message can safely disclose an otherwise protected value.

#### Scenario: Manifest syntax contains a rejected credential-bearing S3 source

- **WHEN** the source URI fails admission
- **THEN** the error can identify the source field and cause without printing the URI or underlying URL parse text

### Requirement: Distinguish public references from stored identities

Public resource kinds and names SHALL retain the command-specific authoring/lookup vocabulary and literal resource name. `skill-group` is an authoring selector, its child inventory rows are skills, and `hook_asset` has no direct add/remove leaf. A public `kind/name` reference is not a guarantee every command accepts that resource. Stored entity, subject and inventory identities are opaque and are not CLI operands.

Native `key_path` arrays contain literal property names, unlike aggregate `content_path` strings. A host property containing `/` or `.` remains one segment when it is one key. Human grouping by declaration, target, scope and destination does not change JSON keys, projected action counts, or independent outcomes. Selected capture/replacement IDs that are not safe to disclose exactly fail before effects rather than being issued with altered names. Masked value displays are not used to compare or approve facts.

#### Scenario: A key contains a literal slash

- **WHEN** output addresses the key `a/b` under `ext`
- **THEN** its native address is `["ext", "a/b"]` as two literal segments, not a decomposition at the slash

### Requirement: Decode a complete result by its independent family version

JSON mode SHALL emit one ordinary result on stdout or the common pre-result error family when applicable. Check the exit code and read a complete result. Inspect `result_type: "cli_error"` first; otherwise use the selected command's result family and check `schema_version`. [Version constants](../../../internal/contractversion/current.go) and [result presenters](../../../internal/cli/present) own the exact versions and fields. They are independent across families. An ordinary result can have valid nonzero status and need not identify itself with `result_type` or `command`.

Each binary emits one current version of a family. A consumer-visible field, type, nullability or semantic change requires a new version before release; a same-version correction restores the documented contract. Diagnostic codes can be added without a version bump when structure and existing meanings are unchanged; consume an unknown code using severity without inferring cleanliness. Unsupported versions, unfamiliar operation variants or partial delivery do not authorize a subsequent write.

#### Scenario: A dirty but otherwise valid status plan is checked

- **WHEN** `status --check --json` has a pending change without diagnostic errors
- **THEN** exit 1 can accompany an ordinary result with `check_clean: false` and `has_errors: false`

### Requirement: Keep known-leaf pre-result failures typed

A known leaf with JSON selected SHALL use [the common failure envelope](../../../internal/cli/present/cli_error.go) before choosing an ordinary family. The full leaf name excludes operands; `error.kind` classifies usage (2) or operation (1); `message` is explanatory, not a stable code. Optional permission protection is observed only if that stage ran. `guidance` is a possibly empty array of notes, instructions and complete `daem` argv. When context is unsafe or unavailable, give manual guidance. A common failure says nothing about whether effects already happened.

Malformed arguments do not erase a later valid `--json`, but declared value slots remain data and an unconsumed `--` terminates recognition. For boolean flags, use the last valid binding; `version` uses its exact syntax. Neither unknown routes nor startup/signal failures are common CLI results. Once an ordinary family is selected, a failure or output fault does not swap it for `cli_error` or attempt to repair partial output.

#### Scenario: The manifest operand resembles an output flag

- **WHEN** `--manifest --json` binds `--json` as the manifest's value
- **THEN** the operand does not select a JSON failure result

### Requirement: Preserve stream and process outcomes

Daem SHALL keep stable ordinary results on stdout and human misuse/pre-result diagnostics on stderr with empty stdout; exit 2 denotes misuse, exit 1 operation failure or a failed check, and successful or warning-only results exit 0. A long-running human command can show one transient line on terminal stderr; JSON, help, misuse, non-terminal stderr, and confirmation do not use it. Clear the line before stable presentation; duplicate completions do not advance counts twice. Progress write failure disables progress, not the operation. Stable delivery failure returns a failure without replacing an already nonzero command status or dumping a writer's raw error. A declined confirmation leaves its disclosure intact and fails before authorization.

The explicit `refresh extension --yes --json` exception writes complete authorization JSON to stderr before launching the host child and one final JSON result to stdout without repeated prose. First observed `SIGINT`, `SIGTERM` or `SIGHUP` supplies exit 130, 143 or 129 respectively and starts cancellation. Later signals preserve that identity while bounding emergency exit; context cancellation alone is not an OS signal.

#### Scenario: Render explicit help instead of a missing-subcommand error

- **WHEN** the caller supplies `list --help`
- **THEN** help is written to stdout and exits 0; invoking a group without its required subcommand instead uses the misuse contract
