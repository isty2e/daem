// Package skillcompat owns skill compatibility profiles, artifact/frontmatter
// compatibility checks, and target-specific diagnostics.
//
// It may inspect resolved skill artifacts and target facts. It must not own
// canonical skill resource identity, declaration authoring, lock construction,
// output projection, payload execution, workflow orchestration, or CLI
// presentation.
package skillcompat
