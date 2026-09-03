package operationplan

import "github.com/isty2e/daem/internal/effect/mutation"

type pathDomainKind uint8

const (
	pathDomainLogical pathDomainKind = iota + 1
	pathDomainPhysical
)

// PathDomainRequest is one pure logical-or-physical mutation-domain request.
// Workflow code lowers it through mutation so path canonicalization and
// filesystem authority remain outside operationplan.
type PathDomainRequest struct {
	kind     pathDomainKind
	logical  mutation.LogicalPathRequest
	physical mutation.PhysicalPathRequest
}

// Logical returns the logical request when this value represents one.
func (request PathDomainRequest) Logical() (mutation.LogicalPathRequest, bool) {
	if request.kind != pathDomainLogical {
		return mutation.LogicalPathRequest{}, false
	}
	return request.logical, true
}

// Physical returns the physical request when this value represents one.
func (request PathDomainRequest) Physical() (mutation.PhysicalPathRequest, bool) {
	if request.kind != pathDomainPhysical {
		return mutation.PhysicalPathRequest{}, false
	}
	return request.physical, true
}

func newLogicalPathDomainRequest(
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
) PathDomainRequest {
	return PathDomainRequest{
		kind: pathDomainLogical,
		logical: mutation.LogicalPathRequest{
			Path: path, Access: access, Effect: effect,
		},
	}
}

func newPhysicalPathDomainRequest(
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
	target string,
	scope string,
) PathDomainRequest {
	return PathDomainRequest{
		kind: pathDomainPhysical,
		physical: mutation.PhysicalPathRequest{
			Path: path, Access: access, Effect: effect,
			Target: target, Scope: scope,
		},
	}
}

type domainStepKind uint8

const (
	domainStepPath domainStepKind = iota + 1
	domainStepCompiled
)

// DomainStep is one ordered pure path request or owner-compiled mutation domain.
type DomainStep struct {
	kind     domainStepKind
	path     PathDomainRequest
	compiled mutation.Domain
}

// Path returns the pure path request when this step requires boundary lowering.
func (step DomainStep) Path() (PathDomainRequest, bool) {
	if step.kind != domainStepPath {
		return PathDomainRequest{}, false
	}
	return step.path, true
}

// Compiled returns an already owner-compiled mutation domain.
func (step DomainStep) Compiled() (mutation.Domain, bool) {
	if step.kind != domainStepCompiled {
		return mutation.Domain{}, false
	}
	return step.compiled, true
}

func (step DomainStep) valid() bool {
	return step.kind == domainStepPath || step.kind == domainStepCompiled
}

func newPathDomainStep(request PathDomainRequest) DomainStep {
	return DomainStep{kind: domainStepPath, path: request}
}

func newCompiledDomainStep(domain mutation.Domain) DomainStep {
	return DomainStep{kind: domainStepCompiled, compiled: domain}
}
