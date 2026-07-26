// Package diagnose executes environment, manifest, resource, and compatibility
// checks that emit findings.
//
// Canonical finding facts live in internal/findings. Diagnose does not own
// rendering, JSON DTOs, exit-code policy, or host mutation.
package diagnose
