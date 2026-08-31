package operationplan

import "github.com/isty2e/daem/internal/effect/mutation"

// LockInput contains normalized lock mutation paths and StateDir presence.
type LockInput struct {
	ManifestPath            string
	LockfilePath            string
	MetadataTransactionPath string
	LocalPaths              []string
	StateDirPath            string
	StateDirPresent         bool
	DocumentMaximumBytes    int64
}

// LockProgram is the immutable lock mutation-domain and revision-role program.
type LockProgram struct {
	domains                 []DomainStep
	manifestPath            string
	lockfilePath            string
	metadataTransactionPath string
	localPaths              []string
	documentMaximumBytes    int64
}

// CompileLock compiles normalized lock evidence without filesystem I/O or
// authority acquisition.
func CompileLock(input LockInput) LockProgram {
	domains := []DomainStep{
		newPathDomainStep(newLogicalPathDomainRequest(
			input.ManifestPath,
			mutation.AccessShared,
			mutation.PathEffectDirectoryEntry,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.ManifestPath,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.LockfilePath,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.LockfilePath,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.MetadataTransactionPath,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
		)),
	}
	for _, path := range input.LocalPaths {
		domains = append(domains, newPathDomainStep(newLogicalPathDomainRequest(
			path,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		)))
	}
	stateDirAccess := mutation.AccessShared
	if !input.StateDirPresent {
		stateDirAccess = mutation.AccessExclusive
	}
	for _, effect := range []mutation.PathEffect{
		mutation.PathEffectDirectoryEntry,
		mutation.PathEffectReferent,
	} {
		domains = append(domains, newPathDomainStep(newLogicalPathDomainRequest(
			input.StateDirPath,
			stateDirAccess,
			effect,
		)))
	}
	return LockProgram{
		domains:                 domains,
		manifestPath:            input.ManifestPath,
		lockfilePath:            input.LockfilePath,
		metadataTransactionPath: input.MetadataTransactionPath,
		localPaths:              append([]string(nil), input.LocalPaths...),
		documentMaximumBytes:    input.DocumentMaximumBytes,
	}
}

// DomainSteps returns the exact domain admission sequence.
func (program LockProgram) DomainSteps() []DomainStep {
	return append([]DomainStep(nil), program.domains...)
}

// RevisionRequests returns the exact post-lease revision capture sequence.
func (program LockProgram) RevisionRequests() ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		program.documentMaximumBytes,
		program.manifestPath,
		program.lockfilePath,
	)
	if err != nil {
		return nil, err
	}
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(
			program.metadataTransactionPath,
			mutation.PathEffectDirectoryEntry,
		),
	)
	for _, path := range program.localPaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(
				path,
				mutation.PathEffectReferent,
			),
		)
	}
	return requests, nil
}
