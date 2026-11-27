# Daem product specifications

These OpenSpec capabilities describe the product behavior of this source revision.
For a released executable, read the documentation and specifications at its
matching Git tag. [User guides](../docs/README.md) show how to use that behavior;
the [LikeC4 model](../architecture/README.md) describes how the current Go code
implements it.

| To look up | Specifications |
| --- | --- |
| Manifest fields, defaults, source grammar, and the lockfile | [Manifest](specs/manifest/spec.md), [workspace selection](specs/workspace-selection/spec.md), [source acquisition](specs/source-acquisition/spec.md), [lockfile](specs/lockfile/spec.md) |
| Instructions, skills and groups, repair, and hooks | [Instructions](specs/instructions/spec.md), [skills](specs/skills/spec.md), [skill compatibility](specs/skill-compatibility/spec.md), [hooks and hook assets](specs/hooks/spec.md) |
| Host support and configuration | [Host admission](specs/host-admission/spec.md), [MCP configuration](specs/mcp-configuration/spec.md), [Pi MCP](specs/pi-mcp/spec.md), [agent configuration](specs/agent-configuration/spec.md), [carrier declarations](specs/carrier-declarations/spec.md) |
| Commands, confirmation, output, and automation | [CLI protocol](specs/cli-protocol/spec.md), [manifest authoring](specs/manifest-authoring/spec.md), [native capture and replacement](specs/native-value-commands/spec.md), [inspection commands](specs/inspection-cli/spec.md), [effect commands](specs/effect-commands/spec.md) |
| Planning, Apply, host commands, and ownership | [Observation and planning](specs/observation-and-planning/spec.md), [Apply lifecycle](specs/apply-lifecycle/spec.md), [managed output](specs/managed-output/spec.md), [shared ownership](specs/shared-ownership/spec.md), [native document lifecycle](specs/native-document-lifecycle/spec.md), [carrier lifecycle](specs/carrier-lifecycle/spec.md) and [routes](specs/carrier-routes/spec.md) |
| Mutation, interruption, and durable records | [Mutation authority](specs/mutation-authority/spec.md), [host mutation](specs/host-mutation/spec.md), [interrupted operations](specs/interrupted-operations/spec.md), [permission protection](specs/permission-protection/spec.md), [durable formats](specs/durable-management-formats/spec.md), [metadata migration](specs/metadata-migration/spec.md) |
| Operating systems and installation | [Platform and distribution](specs/platform-and-distribution/spec.md) |

For changes, see [contract ownership](../CONTRIBUTING.md#contract-ownership) and
[verification](../CONTRIBUTING.md#verify-specifications-and-architecture).
