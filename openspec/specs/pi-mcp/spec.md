# Pi MCP backends

## Purpose

Select and qualify Pi's Native and Adapter MCP contracts without silently reinterpreting a declaration. The [Pi profiles](../../../internal/realization/profile) determine supported versions, providers, selectors and host evidence. The [host guide](../../../docs/host-integrations.md#pi-native-and-adapter) explains these distinctions and a safe backend transition.

## Requirements

### Requirement: Retain the declared backend

Each Pi binding SHALL record the Native or Adapter contract; an omitted backend means Adapter. Other targets reject the `backend` field. A declared/loaded-lock cohort in one Pi context is homogeneous. Previous and retiring contracts keep their codec; replacing a managed binding's backend is not an in-place rename. The supported transition retires the old correlation and provider intent before authoring the new form.

For the first automatic add without a Pi binding or explicit Adapter provider, a bounded isolated version observation can select Native. An absent, malformed or unqualified observation retains Adapter authoring with a diagnostic; cancellation refuses the operation. An existing homogeneous cohort or explicit provider intent determines selection without another default version probe. Lock and apply do not make this choice again.

#### Scenario: Upgrade Pi without changing a declaration

- **WHEN** an existing omitted-backend declaration is read under a new Pi version
- **THEN** it remains Adapter and is not silently rewritten as Native

### Requirement: Qualify a Native binding from observed host prerequisites

Native publication SHALL require observable stable Pi `>=1.0.2,<2.0.0`, admitted builtin selection, and no identified Adapter replacement intent in the selected settings. Status reports qualification per binding, preserving other rows and the causal reason. `status --check` and dry-run reflect a blocker; confirmed apply refuses a blocked selected plan rather than applying only the healthy targets. No settings repair, provider removal or fallback to Adapter is implied.

Recognized Adapter intent includes direct npm, an alias to the registry package, the official Git source and local metadata naming the package. Package filters, `extensions: []`, autoload settings and installation presence do not prove intent absent. Do not inspect package-manager installation inventories to claim disabled-Adapter coexistence. Use Pi's modeled settings-source dispatch, not generic Git identity from extension declarations. For unmodeled syntax or unreadable/malformed present required metadata, do not claim qualification by default.

The selected builtin semantics distinguish forced inclusion/exclusion from plain entries and respect user precedence and the last matching project override. The [selectors](../../../internal/realization/profile/pi_mcp_builtin_selection.go) define exact aliases and supported slash-free patterns. A decisive supported override can resolve uncertainty. Local metadata checks for package and direct-extension entries are bounded to the selected directory or a file's configured and resolved target parents, including chained links, not recursive searches or extension execution. Settings pattern classification precedes local path whitespace handling. These checks do not certify arbitrary custom runtime replacement or full Pi matcher parity.

#### Scenario: A configured Adapter is filtered out in project settings

- **WHEN** selected settings contain an Adapter package, even with no installed copy or with an exclusion filter
- **THEN** the Native binding remains unqualified rather than treating that filter as proof of coexistence

### Requirement: Reconcile Native files without inferring project trust

Native SHALL use the strict JSON project `.pi/mcp.json` or selected Pi agent-root `mcp.json`, with the supported command, ordered args, exact `${SOURCE}` environment aliases, `enabled = true`, and `exposure = "codemode"`. The [codec](../../../internal/realization/aggregate/codec/mcp/pi_native_config.go) accepts omitted enablement and exposure where they have those defaults, optional `type = "stdio"`, and the `codemode-deferred` exposure alias. It does not accept Adapter fields, JSONC, HTTP/OAuth, `cwd`, timeouts, per-tool exposure, or command interpolation. Names use letters, digits, underscores and hyphens; colliding normalized tool namespaces are refused, not renamed. Native activation can connect at startup; codemode is not lazy-server execution.

Project and user config entries must be physically distinct even through aliases. The trusted project full entry replaces a same-name user entry; a transport-free project entry can override flags only while a user definition survives. Equivalent or replaced lower definitions do not alone block. Same-name managed peers still require desired or recorded contribution evidence and ordinary adoption/ownership checks. Removal reports surviving fallback excluding peers retired by that operation, and does not delete unowned override bytes. Scope selection does not change when the host root environment changes or trust is granted.

#### Scenario: A project override relies on a removed user server

- **WHEN** the project record changes only enablement or exposure and its user definition is removed
- **THEN** the project record does not independently define a surviving server and its unowned bytes are not changed

### Requirement: Correlate and qualify the Adapter provider separately

An Adapter binding SHALL correlate with a unique eligible explicit `pi-mcp-adapter` package relation. Project uses its selected project package or an unambiguous explicit-global provider; global requires global. The [provider profile](../../../internal/realization/profile/pi_mcp_provider_profile.go) admits canonical exact or caret-bounded stable npm selectors `>=2.13.0,<3.0.0`. It checks observed installed version separately from requested source. Unobservable or incompatible installed evidence is not a missing package that may be reinstalled. The inspected `2.15.0` is not a version ceiling.

Apply establishes and observes the eligible provider before writing the selected Pi config contribution. The Adapter codec admits canonical command/args, exact environment aliases, lazy lifecycle and enabled state (including modeled defaults), JSONC and unambiguous aliases; malformed, conflicting or unmodeled managed input refuses.

The provider's modeled config layers in increasing precedence are `~/.config/mcp/mcp.json`, `~/.agents/mcp.json`, `~/.agents/mcp/mcp.json`, the Pi agent root's `mcp.json`, project `.mcp.json`, and project `.pi/mcp.json`. Daem observes active layers and eligible imports for equivalence, shadowing and fallback, but writes only the selected Pi-owned file: the agent-root file globally or `.pi/mcp.json` for a project. It observes host-config discovery when enabled and does not enable it silently. Removing an MCP declaration leaves its provider declaration intact.

Global providers may read unowned project MCP files before project trust, including eager entries. Daem authors only lazy entries, warns on shared global use and does not sanitize other files. Neither provider qualification nor written entries prove trust, activation, connectivity or tools. Native and Adapter are not admitted import routes.

#### Scenario: The selected Adapter version cannot be observed

- **WHEN** the package is present but its installed version cannot qualify for the binding
- **THEN** apply reports a prerequisite refusal instead of reinstalling and treating the config as verified
