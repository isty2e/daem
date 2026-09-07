package operationplan

import "github.com/isty2e/daem/internal/effect/mutation"

// InitInput contains normalized init paths plus owner-compiled barrier facts.
type InitInput struct {
	ManifestPath            string
	ManifestMaximumBytes    int64
	MetadataTransactionPath string
	BarrierDomains          []mutation.Domain
	BarrierRevisions        []mutation.RevisionRequest
}

// InitProgram is the immutable init mutation-domain and revision-role program.
type InitProgram struct {
	domains                 []DomainStep
	manifestPath            string
	manifestMaximumBytes    int64
	metadataTransactionPath string
	barrierRevisions        []mutation.RevisionRequest
}

// CompileInit compiles normalized init evidence without filesystem I/O or
// authority acquisition.
func CompileInit(input InitInput) InitProgram {
	domains := []DomainStep{
		newPathDomainStep(newLogicalPathDomainRequest(
			input.ManifestPath,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.ManifestPath,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		)),
		newPathDomainStep(newLogicalPathDomainRequest(
			input.MetadataTransactionPath,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
		)),
	}
	for _, domain := range input.BarrierDomains {
		domains = append(domains, newCompiledDomainStep(domain))
	}
	return InitProgram{
		domains:                 domains,
		manifestPath:            input.ManifestPath,
		manifestMaximumBytes:    input.ManifestMaximumBytes,
		metadataTransactionPath: input.MetadataTransactionPath,
		barrierRevisions:        append([]mutation.RevisionRequest(nil), input.BarrierRevisions...),
	}
}

// DomainSteps returns the exact domain admission sequence.
func (program InitProgram) DomainSteps() []DomainStep {
	return append([]DomainStep(nil), program.domains...)
}

// RevisionRequests returns the exact post-lease revision capture sequence.
func (program InitProgram) RevisionRequests() ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		program.manifestMaximumBytes,
		program.manifestPath,
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
	requests = append(requests, program.barrierRevisions...)
	return requests, nil
}
