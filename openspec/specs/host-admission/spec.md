# Host admission

## Purpose

Explain what the support rows mean. The [host-surface catalog](../../../internal/hostsurface/catalog) owns the exact current target, scope, resource, placement and operation combinations. The [feature guide](../../../docs/features.md) reports user-facing coverage; it is not an independent admission rule.

## Requirements

### Requirement: Interpret support at the granularity of the selected operation

Daem SHALL distinguish a supported operation from declaration/lock admission, authoring-only edits, diagnostic observation, explicit invocation, blocked evidence and unsupported/deferred capabilities. A recognized target or admitted profile is not blanket permission to apply, execute, probe, refresh, import, adopt or remove. Product support is separate from the current host's ability to run a route and from operating-system admission.

The host-specific specs define the admitted behaviors and important exclusions. Do not infer runtime readiness from a file, successful process or recorded relation. A newer host version outside a set of observed examples is not automatically unsupported; use an actual qualification floor or constraint only when the owning profile defines one. A failed host operation does not authorize a substitute route.

#### Scenario: A command hook is declared for Pi

- **WHEN** the catalog admits the declaration as a lock-only bridge diagnostic
- **THEN** daem reports that scope of support, not a supported command-hook write or package installation

### Requirement: Bound host control to the named resource and evidence

Daem SHALL scope effects and claims to the exact supported row and selected evidence. Project visibility does not authorize a global write, and a recorded attempt does not prove current convergence. A removal row preserves unrelated host state unless it names a coupled effect. No common operation can infer control over provider-bundled contributions, credentials, trust, runtime execution, stores, or external cleanup merely from relation visibility.

Directory-skill support does not include arbitrary plugin/system/admin directories, slash-command Markdown skills, or the Antigravity IDE. Antigravity CLI's global `agent_config` is not a separate settings or IDE resource. Remote/bundled MCP, active OAuth, general executable provisioning, arbitrary hook directories and implicit command-path installation are outside current support.

#### Scenario: A declared carrier has a visible package cache

- **WHEN** daem can observe the relation for planning
- **THEN** the observation does not confer a general cache-prune or contribution-management capability
