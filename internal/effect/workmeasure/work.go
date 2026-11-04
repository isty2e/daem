// Package workmeasure owns non-negative artifact work measurements without
// allocation, filesystem identity or mutation authority.
package workmeasure

import "fmt"

// ArtifactWork is the content work observed for one regular file or directory.
type ArtifactWork struct {
	entries int
	bytes   int64
}

// NewArtifactWork constructs one non-negative work measurement.
func NewArtifactWork(entries int, bytes int64) (ArtifactWork, error) {
	if entries < 0 || bytes < 0 {
		return ArtifactWork{}, fmt.Errorf("artifact work must not be negative")
	}
	return ArtifactWork{entries: entries, bytes: bytes}, nil
}

// Entries returns the observed descendant-entry count.
func (work ArtifactWork) Entries() int { return work.entries }

// Bytes returns the observed regular-file byte count.
func (work ArtifactWork) Bytes() int64 { return work.bytes }

// Equal reports exact agreement of descendant and byte work.
func (work ArtifactWork) Equal(other ArtifactWork) bool { return work == other }
