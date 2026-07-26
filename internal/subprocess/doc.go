// Package subprocess executes an already-authorized child-process attempt.
//
// It owns argv-preserving launch mechanics, exact child environments,
// descriptor-backed working directories, terminal-safe bounded and redacted
// output, timeout/cancel classification, and complete process-group cleanup.
// It does not own route admission, host or protocol policy, convergence,
// current or durable evidence, root selection, or presentation wording.
package subprocess
