# Apply lifecycle

## Purpose

Define how admitted direct writes, delegated commands, and recorded management changes compose. The [apply workflow](../../../internal/workflow/apply) executes the selected work; the [effect command interface](../effect-commands/spec.md) owns disclosure and the result families. [Provider replan tests](../../../internal/workflow/apply/provider_order_authorization_test.go) exercise the approval boundary across provider effects.

## Requirements

### Requirement: Preserve the effect order of the selected apply

Apply SHALL qualify all selected work before beginning any of it. Perform any admitted settings prefix and source-handoff retirements before their dependent work. Where a Pi Adapter route needs a provider install, qualify it, establish any required selected keys, and check the final plan against fresh evidence before and after the prerequisite phase. Unobservable or incompatible installed providers are not missing providers to reinstall. Pending completion backed by a scheduled provider invocation belongs to that phase; otherwise settle it only when the owning core or later phase has the required evidence. Do not publish a predicted claim.

Within the normal continuation, perform core direct work, global claim retirements, managed carrier removals, final host routes, relation order, then delegated executable work; terminal global adoption comes last. In a supported split native apply, finish the settings and remaining direct suffix at its admitted point. Work absent from a plan has no effect. Recheck the [remaining authorization](../mutation-authority/spec.md) at the boundaries; neither a prior no-op relation nor an already completed phase supplies authority for a new reservation.

Selected instruction, skill, direct MCP and native-key changes can share one recovery operation. Where keys and Codex MCP share a document, compose them into one write. Failure before that state's commit can restore its direct changes together. Host commands are not part of that rollback guarantee: an already settled direct segment or verified host claim remains settled if a later segment fails.

#### Scenario: An incompatible Pi Adapter provider is already installed

- **WHEN** the selected binding cannot qualify the installed version
- **THEN** apply refuses instead of installing over it or writing the dependent configuration

### Requirement: Compose only admitted native settings and host routes

Apply SHALL establish desired keys before supported Pi package install, reinstall and removal or Codex global install and removal, and verify them after each command. Key removals and other remaining direct changes follow the applicable commands and settings checks. Pi Adapter installation follows the same settings-first rule, before dependent MCP config and other remaining writes.

Claude Code, OpenCode and Antigravity CLI routes can accompany managed keys if their private write namespaces and those files are physically disjoint. For Claude Code that excludes its private root and, for project commands, the project's `.claude`; for OpenCode it excludes the selected plugin candidate paths; for Antigravity it excludes shared config and plugin-store paths. Aliases and nesting count. Different target names are not evidence of separation. An already-current relation does not by itself block direct work or rerun installation.

Whole-file mutations can accompany carrier changes only when their files are disjoint from the command's settings rewrites and private plugin paths. Create, update or remove those files with the other direct file work after the commands. Check them before that phase; an unexpected change stops it without content repair. Narrowing to retained keys settles before the commands. Empty narrowing (release without editing content) and explicitly adopted exact key-to-whole matches settle afterward. Preview and confirmed apply both refuse unsupported compositions.

#### Scenario: A plugin's private write path aliases managed keys

- **WHEN** a selected combination has no supported settings rewrite route for that overlap
- **THEN** both preview and confirmed apply refuse it before any of those effects

### Requirement: Reconcile order from the current selected relations

Daem SHALL distinguish Pi runtime precedence from OpenCode's per-document config order. Preserve package entry options and unrelated positions under the admitted order rules. Already installed Pi packages and managed keys in the same settings layer can share a journaled write, even when the keys already match. Physically separate order changes retain their ordinary path, including Pi order when only another layer has managed keys.

After applicable carrier changes, compute the remaining order from fresh settings. Newly discovered changes to managed/foreign precedence require the renewed consent defined by the [apply interface](../effect-commands/spec.md#requirement-preserve-apply-outcome-and-diagnostic-distinctions). Do not repeat completed carrier commands to obtain that consent or finish order.

#### Scenario: A completed install exposes a new precedence change

- **WHEN** `apply --yes` encounters a newly discovered managed/foreign precedence change
- **THEN** it retains the completed carrier work without writing that order or starting later delegates

### Requirement: Limit executable delegation during apply to the admitted route

Ordinary apply SHALL not run the explicit [MCP probe](../effect-commands/spec.md#requirement-disclose-and-execute-only-the-selected-mcp-probe). Only an admitted Claude Code project MCP delegate can execute the exact locked server command during confirmed apply, with sanitized attempt results and bounded post-observation. That attempt does not qualify runtime readiness, authentication, tools, trust, package convergence or a future skip.

#### Scenario: An OpenCode project row admits a separate explicit probe

- **WHEN** the operator applies that row instead of probing it
- **THEN** apply does not launch the server on the strength of its probe eligibility
