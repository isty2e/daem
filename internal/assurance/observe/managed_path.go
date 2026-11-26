package observe

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
)

// ManagedPathEvidence is one fresh observation of a locked physical path. It
// contains no desired policy, managed authority, or previous-attempt fact.
type ManagedPathEvidence struct {
	subject        topology.SubjectID
	destination    output.Destination
	exists         bool
	contentHash    artifact.ContentHash
	hashes         artifact.PathHashes
	selected       artifact.ContentHash
	members        artifact.TreeMembers
	verifiedSource artifact.ContentHash
	selectedSource artifact.ContentHash
	fileMode       os.FileMode
}

// NewManagedPathEvidence constructs current path evidence already observed by
// a filesystem boundary adapter.
func NewManagedPathEvidence(
	subject topology.SubjectID,
	destination output.Destination,
	exists bool,
	contentHash artifact.ContentHash,
	fileMode os.FileMode,
) (ManagedPathEvidence, error) {
	evidence := ManagedPathEvidence{
		subject:     subject,
		destination: destination,
		exists:      exists,
		contentHash: contentHash,
		fileMode:    fileMode.Perm(),
	}
	if err := evidence.validate(); err != nil {
		return ManagedPathEvidence{}, err
	}
	return evidence, nil
}

func (evidence ManagedPathEvidence) validate() error {
	if err := evidence.subject.Validate(); err != nil {
		return fmt.Errorf("managed path evidence subject: %w", err)
	}
	if evidence.subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("managed path evidence requires projection subject")
	}
	if err := evidence.destination.Validate(); err != nil {
		return fmt.Errorf("managed path evidence destination: %w", err)
	}
	if evidence.exists && (!evidence.members.Known() || evidence.contentHash != "") {
		if evidence.contentHash == "" {
			return fmt.Errorf("managed path evidence content hash is required when present")
		}
		if err := evidence.contentHash.Validate(); err != nil {
			return fmt.Errorf("managed path evidence content hash: %w", err)
		}
	}
	if !evidence.exists && (evidence.contentHash != "" || evidence.fileMode != 0) {
		return fmt.Errorf("absent managed path evidence cannot carry content or mode")
	}
	return nil
}

func (evidence ManagedPathEvidence) Subject() topology.SubjectID       { return evidence.subject }
func (evidence ManagedPathEvidence) Destination() output.Destination   { return evidence.destination }
func (evidence ManagedPathEvidence) Exists() bool                      { return evidence.exists }
func (evidence ManagedPathEvidence) ContentHash() artifact.ContentHash { return evidence.contentHash }
func (evidence ManagedPathEvidence) FileMode() os.FileMode             { return evidence.fileMode }

func NewObservedPath(subject topology.SubjectID, destination output.Destination, hashes artifact.PathHashes, mode os.FileMode) (ManagedPathEvidence, error) {
	evidence, err := NewManagedPathEvidence(subject, destination, true, hashes.Source, mode)
	if err != nil {
		return ManagedPathEvidence{}, err
	}
	if !hashes.Installed.IsInstalled() || hashes.Installed.ValidateManaged() != nil {
		return ManagedPathEvidence{}, fmt.Errorf("observed path requires an installed content hash")
	}
	evidence.hashes = hashes
	return evidence, nil
}

func (evidence ManagedPathEvidence) Matches(hash artifact.ContentHash) bool {
	return evidence.exists && hash != "" && (hash == evidence.contentHash || hash == evidence.verifiedSource || evidence.hashes.Matches(hash))
}

// NewObservedTree separates the physical tree, when observed, from the bytes
// at the requested inventory's paths. Only the former authorizes whole-tree I/O.
func NewObservedTree(subject topology.SubjectID, destination output.Destination, whole artifact.PathHashes, selected artifact.ContentHash, members artifact.TreeMembers) (ManagedPathEvidence, error) {
	if !members.Known() || (selected != "" && (!selected.IsInstalled() || selected.ValidateManaged() != nil)) {
		return ManagedPathEvidence{}, fmt.Errorf("invalid tree membership evidence")
	}
	e := ManagedPathEvidence{subject: subject, destination: destination, exists: true, selected: selected, members: members}
	if whole != (artifact.PathHashes{}) {
		var err error
		e, err = NewObservedPath(subject, destination, whole, 0)
		if err != nil {
			return ManagedPathEvidence{}, err
		}
		e.selected, e.members = selected, members
	}
	return e, e.validate()
}

func (evidence ManagedPathEvidence) MatchesBaseline(hash artifact.ContentHash) bool {
	return evidence.Matches(hash) || (hash != "" && (hash == evidence.selected || hash == evidence.selectedSource))
}

func (evidence ManagedPathEvidence) VerifiedMembers(hash artifact.ContentHash) artifact.TreeMembers {
	if hash != "" && (hash == evidence.selected || hash == evidence.selectedSource) {
		return evidence.members
	}
	return artifact.TreeMembers{}
}

func (evidence ManagedPathEvidence) InstalledBaseline(hash artifact.ContentHash) artifact.ContentHash {
	if evidence.VerifiedMembers(hash).Known() {
		return evidence.selected
	}
	if evidence.Matches(hash) {
		return evidence.InstalledHash()
	}
	return ""
}

// WithVerifiedSource admits an old source baseline only when its verified
// installed bytes and tree structure match this physical observation.
func (evidence ManagedPathEvidence) WithVerifiedSource(source, installed artifact.ContentHash) ManagedPathEvidence {
	if installed.IsInstalled() && evidence.InstalledHash() == installed {
		evidence.verifiedSource = source
	}
	if installed.IsInstalled() && evidence.selected == installed {
		evidence.selectedSource = source
	}
	return evidence
}

func (evidence ManagedPathEvidence) InstalledHash() artifact.ContentHash {
	return evidence.hashes.Installed
}
