// Package commit provides descriptor-anchored stable filesystem commits.
//
// The package owns no-follow regular-file and rooted-destination snapshots,
// commit-parent preparation, file publication, rooted-tree
// staging and publication, and logical removal. It admits staged tree entries
// through a descriptor-backed writer and streams existing rooted trees through
// a descriptor-backed snapshot sink. It does not own payload-source traversal,
// serialization, workflow recovery, retries, mutation authority, or user-facing
// diagnostics.
//
// Linux NFS publication follows the single-writer support envelope in
// docs/platforms.md. No-replace fallback checks absence without atomic external
// writer exclusion; an uncertain rename retains evidence instead of cleaning it.
package commit
