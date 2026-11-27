# Daem architecture

The LikeC4 model shows the Go CLI, the owners of its decisions and data, and its effects on host processes and files. Open the views from the repository root:

```sh
likec4 start architecture
```

Start with **Daem in context** and **Semantic ownership and data flow**. The **Host-Surface catalog** view shows where static host facts come from and who uses them. **Operation-Safety and the State Barrier** shows how typed work gets authority to act. **Host and durable boundaries** separates installed outputs, state, claims and recovery records.

The numbered views show particular routes through the same model: assessment and declaration writes; direct Apply, provider and native continuation; explicit refresh and MCP probe; journal, permission and metadata recovery. Read the view description for the branch it illustrates. The element and view links identify the relevant source and tests in this checkout.

**Direct workflow service imports** shows production Go imports between the named packages. Other arrows show data flow or runtime interaction, which can cross several packages. The [workflow import policy](../internal/archguard/workflow_imports.go) governs permissions; a permission alone does not make an edge live.

Review a change at the owner it affects. New OS support starts with the platform rules and physical adapters. A target that reuses a format starts with the catalog and target adapter. A new realization form belongs with Realization and its matching Reconciliation and Effect variants. New observations belong with Assurance and its adapter; recovery changes belong with the State Barrier and recovery effects.

To check the model or build the views outside the checkout:

```sh
likec4 validate architecture
likec4 build architecture --output "$(mktemp -d)"
```

These commands check the model and render local previews. The build can contain checkout-specific source links; only the source model and views belong in the repository. Compare changed relationships and sequence steps with the linked code and tests at the same revision. The existing Go architecture checks remain under `tools/test.sh repository`.
