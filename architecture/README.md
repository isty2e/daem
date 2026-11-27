# Daem architecture

The LikeC4 model shows the Go CLI, the owners of its decisions and data, and its effects on host processes and files. Open the views from the repository root:

```sh
likec4 start architecture
```

Start with **Daem in context** and **Semantic ownership and data flow**. The **Host-Surface catalog** view shows where static host facts come from and who uses them. **Operation-Safety and the State Barrier** shows how typed work gets authority to act. **Host and durable boundaries** separates installed outputs, state, claims and recovery records.

## Domain model

Open **Domain model overview**, then choose the view for the identity or boundary you are tracing. The [LikeC4 sources](domain/) link to the actual Go types, constructors, and tests. A domain arrow describes a relationship between data or decisions, not a package import or the order in which code runs.

| To understand | Open these views |
| --- | --- |
| Why one declaration can produce several lock rows, or be locked without any installed file | **Declarations, identities, and exact supply**; **Source-backed skills, instructions, and hook assets** |
| How two codecs can update disjoint parts of the same Codex `config.toml`, and why that differs from whole-file ownership | **Native documents, ownership modes, and key identity**; **Disjoint projections in a shared settings file** |
| What a Pi MCP binding locks, or what proves that a host package is present and managed | **MCP bindings, provider contributions, and launch identity**; **Extension carriers, host correlation, and claims**; **Declared precedence and observed host sequences** |
| Why a binding or protection record can exist without a claim, and why a copied skill with extra files can still be up to date | **Output ownership, document bindings, and permission fences**; **Installed baselines and reconciliation decisions** |
| How recovery can finish a claim after State is committed without repeating a host write | **Effect authority, journals, and recovery evidence** |

## Execution views

The numbered views show particular routes through the same model: assessment and declaration writes; direct Apply, provider and native continuation; explicit refresh and MCP probe; journal, permission and metadata recovery. Read the view description for the branch it illustrates. The element and view links identify the relevant source and tests in this checkout.

**Direct workflow service imports** shows production Go imports between the named packages. Other arrows show data flow or runtime interaction, which can cross several packages. The [workflow import policy](../internal/archguard/workflow_imports.go) governs permissions; a permission alone does not make an edge live.

Review a change at the owner it affects. New OS support starts with the platform rules and physical adapters. A target that reuses a format starts with the catalog and target adapter. A new realization form belongs with Realization and its matching Reconciliation and Effect variants. New observations belong with Assurance and its adapter; recovery changes belong with the State Barrier and recovery effects.

To check the model or build the views outside the checkout:

```sh
likec4 validate architecture
likec4 build architecture --output "$(mktemp -d)"
```

These commands check the model and render local previews. The build can contain checkout-specific source links; only the source model and views belong in the repository. Compare changed relationships and sequence steps with the linked code and tests at the same revision. The existing Go architecture checks remain under `tools/test.sh repository`.
