# Observation and planning

## Purpose

Define what evidence daem gathers before proposing work. The [inspection commands](../inspection-cli/spec.md) and [effect command interface](../effect-commands/spec.md) define how those facts are reported. The [readiness workflow](../../../internal/workflow/readiness) and [observers](../../../internal/assurance/observe) implement the assessment.

## Requirements

### Requirement: Distinguish evidence about intent, ownership, and the live host

Daem SHALL assess the selected declaration, its exact lock, recorded management and current observations as separate inputs. A lock identifies desired source and realization, not an applied baseline. A recorded claim does not establish that its setting or file still exists or matches. A command attempt does not establish a current relation, configuration, or runtime readiness.

Queries collect the evidence for their purpose. `list resources` can work from declarations alone; `list paths` is a catalog and selection query; `list outputs` uses recorded and live ownership evidence without resolving source updates, checking extension order, or launching a host. Status compares the selected known state, even with a missing lock or a blocked item when it can still form a valid report. It does not acquire authority to make its reported changes.

#### Scenario: A delegated command succeeded earlier

- **WHEN** status has a historical attempt receipt but no current evidence for the claimed relation
- **THEN** it does not turn the receipt into a successful live observation

### Requirement: Keep passive checks separate from explicitly authorized runtime work

Doctor SHALL make passive prerequisite checks rather than invoke host CLIs, servers, package managers, credential helpers or network probes. Bounded disposable checks in selected storage paths may exercise admission and publication without changing managed outputs. For Native Pi MCP, status and apply planning can instead run the isolated, bounded version observation and read modeled settings and source metadata. They do not query package-manager installation locations or start a server to fill missing evidence.

A modeled prerequisite that cannot be observed remains unqualified or unobserved. A route for which daem has no observer is a different case. One missing or unsupported observation does not make unrelated known facts unavailable, nor prove that an unobserved competitor is absent. [Explicit MCP probing](../effect-commands/spec.md#requirement-disclose-and-execute-only-the-selected-mcp-probe) is a separate opt-in operation.

#### Scenario: One Native Pi binding is unqualified in a multi-target workspace

- **WHEN** status cannot establish its host prerequisite but can assess another target
- **THEN** status reports the binding's blocker and the other target's evidence separately, and the selected check is not clean

### Requirement: Treat a preview as evidence, not consent or a mutation

A preview SHALL disclose work from the selected evidence without performing that work. This includes any admitted cleanup, provider prerequisites, conditional handoffs, destructive file replacement and uncertainty about delegated results. In particular, dry-run and a no-op do not create a state directory solely to plan the operation. Query and preview routes retain their own [journal and file-set admission](../mutation-authority/spec.md#requirement-keep-the-state-directory-and-recovery-barriers-independent); a refusal by a read-only command does not grant a write capability. A later execution has to qualify the current inputs and the applicable consent again, rather than treating the prior preview as approval.

#### Scenario: The workspace has no state directory and no planned effects

- **WHEN** the operator previews the work or confirms a plan that needs no effects
- **THEN** daem does not create managed state merely to represent the plan
