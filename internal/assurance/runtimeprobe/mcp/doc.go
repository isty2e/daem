// Package mcp executes explicit MCP runtime readiness probes.
//
// It owns subprocess effects, stdio initialize, timeout, env resolution,
// cleanup, raw bounded capture, and probe-fact construction. Generic launch,
// child-environment, working-directory, process-group, and captured-text
// mechanics live in subprocess. This package does not select manifest
// subjects, persist evidence, render output, or infer runtime health.
package mcp
