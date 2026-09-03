package operationplan

import "github.com/isty2e/daem/internal/effect/mutation"

// MetadataDomainInput describes one metadata file-set mutation authority.
type MetadataDomainInput struct {
	TargetPaths     []string
	MarkerPath      string
	LocalPaths      []string
	TrailingDomains []mutation.Domain
}

// CompileMetadataDomains returns the canonical metadata target, marker, local
// source, and trailing-domain admission order.
func CompileMetadataDomains(input MetadataDomainInput) []DomainStep {
	domains := make([]DomainStep, 0, len(input.TargetPaths)*2+1+len(input.LocalPaths)+len(input.TrailingDomains))
	for _, path := range input.TargetPaths {
		domains = append(
			domains,
			newPathDomainStep(newLogicalPathDomainRequest(
				path,
				mutation.AccessExclusive,
				mutation.PathEffectDirectoryEntry,
			)),
			newPathDomainStep(newLogicalPathDomainRequest(
				path,
				mutation.AccessShared,
				mutation.PathEffectReferent,
			)),
		)
	}
	domains = append(domains, newPathDomainStep(newLogicalPathDomainRequest(
		input.MarkerPath,
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
	)))
	for _, path := range input.LocalPaths {
		domains = append(domains, newPathDomainStep(newLogicalPathDomainRequest(
			path,
			mutation.AccessShared,
			mutation.PathEffectReferent,
		)))
	}
	for _, domain := range input.TrailingDomains {
		domains = append(domains, newCompiledDomainStep(domain))
	}
	return domains
}

// AuthoringInput contains normalized manifest-authoring mutation facts.
type AuthoringInput struct {
	ManifestPath         string
	LockfilePath         string
	MarkerPath           string
	LocalPaths           []string
	BarrierDomains       []mutation.Domain
	BarrierRevisions     []mutation.RevisionRequest
	DocumentMaximumBytes int64
}

// AuthoringProgram is the immutable manifest-authoring domain and revision program.
type AuthoringProgram struct {
	domains              []DomainStep
	manifestPath         string
	lockfilePath         string
	markerPath           string
	localPaths           []string
	barrierRevisions     []mutation.RevisionRequest
	documentMaximumBytes int64
}

// CompileAuthoring compiles normalized authoring evidence without filesystem
// I/O or authority acquisition.
func CompileAuthoring(input AuthoringInput) AuthoringProgram {
	return AuthoringProgram{
		domains: CompileMetadataDomains(MetadataDomainInput{
			TargetPaths:     []string{input.ManifestPath, input.LockfilePath},
			MarkerPath:      input.MarkerPath,
			LocalPaths:      input.LocalPaths,
			TrailingDomains: input.BarrierDomains,
		}),
		manifestPath:         input.ManifestPath,
		lockfilePath:         input.LockfilePath,
		markerPath:           input.MarkerPath,
		localPaths:           append([]string(nil), input.LocalPaths...),
		barrierRevisions:     append([]mutation.RevisionRequest(nil), input.BarrierRevisions...),
		documentMaximumBytes: input.DocumentMaximumBytes,
	}
}

// DomainSteps returns the exact authoring domain admission sequence.
func (program AuthoringProgram) DomainSteps() []DomainStep {
	return append([]DomainStep(nil), program.domains...)
}

// RevisionRequests returns the exact post-recovery revision capture sequence.
func (program AuthoringProgram) RevisionRequests() ([]mutation.RevisionRequest, error) {
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
			program.markerPath,
			mutation.PathEffectDirectoryEntry,
		),
	)
	for _, path := range program.localPaths {
		requests = append(requests, mutation.NewBoundedContentRevisionRequest(
			path,
			mutation.PathEffectReferent,
		))
	}
	requests = append(requests, program.barrierRevisions...)
	return requests, nil
}

// UnmanageInput contains normalized unmanage mutation facts.
type UnmanageInput struct {
	DeclarationPaths     []string
	PersistencePaths     []string
	MarkerPath           string
	LocalPaths           []string
	BarrierDomains       []mutation.Domain
	BarrierRevisions     []mutation.RevisionRequest
	DocumentMaximumBytes int64
}

// UnmanageProgram is the immutable unmanage domain and revision program.
type UnmanageProgram struct {
	domains              []DomainStep
	declarationPaths     []string
	persistencePaths     []string
	markerPath           string
	localPaths           []string
	barrierRevisions     []mutation.RevisionRequest
	documentMaximumBytes int64
}

// CompileUnmanage compiles normalized unmanage evidence without filesystem I/O
// or authority acquisition.
func CompileUnmanage(input UnmanageInput) UnmanageProgram {
	targets := make([]string, 0, len(input.DeclarationPaths)+len(input.PersistencePaths))
	targets = append(targets, input.DeclarationPaths...)
	targets = append(targets, input.PersistencePaths...)
	return UnmanageProgram{
		domains: CompileMetadataDomains(MetadataDomainInput{
			TargetPaths:     targets,
			MarkerPath:      input.MarkerPath,
			LocalPaths:      input.LocalPaths,
			TrailingDomains: input.BarrierDomains,
		}),
		declarationPaths:     append([]string(nil), input.DeclarationPaths...),
		persistencePaths:     append([]string(nil), input.PersistencePaths...),
		markerPath:           input.MarkerPath,
		localPaths:           append([]string(nil), input.LocalPaths...),
		barrierRevisions:     append([]mutation.RevisionRequest(nil), input.BarrierRevisions...),
		documentMaximumBytes: input.DocumentMaximumBytes,
	}
}

// DomainSteps returns the exact unmanage domain admission sequence.
func (program UnmanageProgram) DomainSteps() []DomainStep {
	return append([]DomainStep(nil), program.domains...)
}

// RevisionRequests returns the exact post-recovery revision capture sequence.
func (program UnmanageProgram) RevisionRequests() ([]mutation.RevisionRequest, error) {
	requests, err := mutation.BoundedFileRevisionRequests(
		program.documentMaximumBytes,
		program.declarationPaths...,
	)
	if err != nil {
		return nil, err
	}
	for _, path := range program.persistencePaths {
		requests = append(
			requests,
			mutation.NewBoundedContentRevisionRequest(
				path,
				mutation.PathEffectDirectoryEntry,
			),
			mutation.NewBoundedContentRevisionRequest(
				path,
				mutation.PathEffectReferent,
			),
		)
	}
	requests = append(
		requests,
		mutation.NewBoundedContentRevisionRequest(
			program.markerPath,
			mutation.PathEffectDirectoryEntry,
		),
	)
	for _, path := range program.localPaths {
		requests = append(requests, mutation.NewBoundedContentRevisionRequest(
			path,
			mutation.PathEffectReferent,
		))
	}
	requests = append(requests, program.barrierRevisions...)
	return requests, nil
}
