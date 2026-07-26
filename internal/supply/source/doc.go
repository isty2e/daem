// Package source defines the boundary between manifest provenance and lockable content.
//
// Resolvers may read Git repositories or local filesystems, but they must not render host
// outputs, inspect target adapters, or decide resource-specific semantics such as whether
// a directory is a valid skill. Callers perform resource validation after resolution.
package source
