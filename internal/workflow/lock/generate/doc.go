// Package generate builds prospective canonical lock snapshots from desired
// environments and source acquisition boundaries.
//
// The package owns cache selection, local input enumeration, canonical lock
// construction, and lockfile serialization. It does not select manifest or
// output paths, compare snapshots, commit files, or interpret command progress
// for presentation.
package generate
