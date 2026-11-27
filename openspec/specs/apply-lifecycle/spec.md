# Apply lifecycle

## Purpose

Define how admitted direct writes, delegated commands, and recorded management changes compose. The [apply workflow](../../../internal/workflow/apply) executes the selected work; the [effect command interface](../effect-commands/spec.md) owns disclosure and the result families. [Provider replan tests](../../../internal/workflow/apply/provider_order_authorization_test.go) exercise the approval boundary across provider effects.

## Requirements

### Requirement: Preserve the effect order of the selected apply

Apply SHALL qualify the selected plan before beginning effects, including the conditions for any later work. Perform admitted settings and source-handoff retirements before their dependent work. Where a Pi Adapter route needs a provider install, qualify it and establish any required selected keys before invocation. Do not treat unobservable or incompatible installed providers as missing packages to reinstall.

After the provider attempt, rebuild and validate the final plan from fresh evidence, including under the continued leases. The prerequisite may update its provider, relation, attempt, adoption and global claim facts, not the rest of the approved plan or its authority. Stop if the remaining plan changes or exceeds the approved reservation, retaining known attempt evidence. Do not settle the same pending install twice or publish predicted management before observing it.

Within the normal continuation, perform core direct work, global claim retirements, managed carrier removals, final host routes, ordinary relation order, then delegated executable work; terminal global adoption comes last. In a split native apply, carry out the remaining native work after final host routes. If an executable delegate is scheduled, establish its ordinary and MCP prerequisites before invocation and defer native retirement, ownership transitions and the coupled Pi order until after it. Recheck the [remaining authorization](../mutation-authority/spec.md) at those boundaries; a completed phase does not grant a new reservation.

[Admitted direct document work](../native-document-lifecycle/spec.md#requirement-compose-only-the-selected-contributions-to-a-physical-document) can share one recovery operation. That rollback guarantee does not cover already settled direct work or verified host claims from an earlier phase.

#### Scenario: An incompatible Pi Adapter provider is already installed

- **WHEN** the selected binding cannot qualify the installed version
- **THEN** apply refuses instead of installing over it or writing the dependent configuration

### Requirement: Compose only admitted native settings and host routes

Apply SHALL establish desired keys before supported Pi package install, reinstall and removal or Codex global install and removal, and verify them after each command. If managed settings change unexpectedly, stop without automatically repairing their contents. Leave tighter permissions after a host rewrite, and use [permission recovery](../permission-protection/spec.md) for interrupted protection rather than replaying the command. Key removals and other remaining direct changes follow the applicable commands and settings checks. Pi Adapter installation follows the same settings-first rule, before dependent MCP config and other remaining writes.

Claude Code, OpenCode and Antigravity CLI routes can accompany managed keys if their private write namespaces and those files are physically disjoint. For Claude Code that excludes its private root and, for project commands, the project's `.claude`; for OpenCode it excludes the selected plugin candidate paths; for Antigravity it excludes shared config and plugin-store paths. Aliases and nesting count. Different target names are not evidence of separation. An already-current relation does not by itself block direct work or rerun installation.

Whole-file mutations can accompany carrier changes only when their files are disjoint from the command's settings rewrites and private plugin paths. Create, update or remove those files with the other direct file work after the commands. Check them before that phase; an unexpected change stops it without content repair. Narrowing to retained keys settles before the commands. Empty narrowing (release without editing content) and explicitly adopted exact key-to-whole matches settle afterward. Preview and confirmed apply both refuse unsupported compositions.

#### Scenario: A plugin's private write path aliases managed keys

- **WHEN** a selected combination has no supported settings rewrite route for that overlap
- **THEN** both preview and confirmed apply refuse it before any of those effects

### Requirement: Reconcile order from the current selected relations

Daem SHALL distinguish Pi runtime precedence from OpenCode's per-document config order. Preserve package entry options and unrelated positions under the admitted order rules. Already installed Pi packages and managed keys in the same settings layer can share a journaled write, even when the keys already match. Physically separate order changes retain their ordinary path, including Pi order when only another layer has managed keys.

After applicable carrier changes, compute the remaining order from fresh settings. Newly discovered changes to managed/foreign precedence require the renewed consent defined by the [apply interface](../effect-commands/spec.md#requirement-preserve-apply-outcome-and-diagnostic-distinctions). If renewed consent is not obtained, do not write the order or any remaining direct changes that would share that write. Retain completed effects, including a delegate already invoked before deferred native order. Do not repeat completed carrier commands to obtain consent or finish order.

#### Scenario: A completed install exposes a new precedence change

- **WHEN** `apply --yes` encounters a newly discovered managed/foreign precedence change
- **THEN** it retains completed work but does not write the order or its coupled direct changes, or start delegates still scheduled after that write

### Requirement: Limit executable delegation during apply to the admitted route

Ordinary apply SHALL not run the explicit [MCP probe](../effect-commands/spec.md#requirement-disclose-and-execute-only-the-selected-mcp-probe). Only an admitted Claude Code project MCP delegate can execute the exact locked server command during confirmed apply, with sanitized attempt results and bounded post-observation. That attempt does not qualify runtime readiness, authentication, tools, trust, package convergence or a future skip.

#### Scenario: An OpenCode project row admits a separate explicit probe

- **WHEN** the operator applies that row instead of probing it
- **THEN** apply does not launch the server on the strength of its probe eligibility
