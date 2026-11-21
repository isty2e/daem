package recoverygate

import (
	"errors"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

// LowerDomainStep resolves one reservation at its position in an operation.
func LowerDomainStep(step operationplan.DomainStep) (mutation.Domain, error) {
	return lowerDomainStep(step, true)
}

// LowerDomainSteps resolves reservations in admission order, stopping at the first error.
func LowerDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	return lowerDomainSteps(steps, true)
}

// LowerLogicalDomainSteps accepts compiled domains unchanged but resolves only logical path requests.
func LowerLogicalDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	return lowerDomainSteps(steps, false)
}

func lowerDomainSteps(steps []operationplan.DomainStep, allowPhysical bool) ([]mutation.Domain, error) {
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		domain, err := lowerDomainStep(step, allowPhysical)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func lowerDomainStep(step operationplan.DomainStep, allowPhysical bool) (mutation.Domain, error) {
	if domain, ok := step.Compiled(); ok {
		return domain, nil
	}
	request, ok := step.Path()
	if !ok {
		return mutation.Domain{}, errors.New("invalid reservation domain step")
	}
	if logical, ok := request.Logical(); ok {
		return mutation.NewLogicalPathDomain(logical)
	}
	if !allowPhysical {
		return mutation.Domain{}, errors.New("reservation path request is not logical")
	}
	physical, ok := request.Physical()
	if !ok {
		return mutation.Domain{}, errors.New("invalid reservation path request")
	}
	return mutation.NewPhysicalPathDomain(physical)
}
