package carrierabsence

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

// RouteAdmissionStatus identifies whether one exact managed-carrier removal
// route passed the operation-specific admission floor.
type RouteAdmissionStatus string

const (
	RouteUnavailable RouteAdmissionStatus = "unavailable"
	RouteAdmitted    RouteAdmissionStatus = "admitted"
)

// RouteAdmissionInput contains the already-selected removal contract and its
// exact request identity. Raw route evidence belongs to profile realization,
// not to reconciliation.
type RouteAdmissionInput struct {
	Operation              lock.OperationContract
	Request                realizationdelegate.Request
	PreservesSharedCarrier bool
	RemovedEffects         []string
	RetainedEffects        []string
	NonClaims              []string
}

// RouteAdmission is the immutable RA-05 input consumed by carrier-absence
// reconciliation. It grants no execution authority by itself.
type RouteAdmission struct {
	status                 RouteAdmissionStatus
	operation              lock.OperationContract
	request                realizationdelegate.Request
	preservesSharedCarrier bool
	removedEffects         []string
	retainedEffects        []string
	nonClaims              []string
}

// UnavailableRoute records that no operation-specific removal route was
// admitted. It is a valid planner input, not an execution error.
func UnavailableRoute() RouteAdmission {
	return RouteAdmission{status: RouteUnavailable}
}

// NewRouteAdmission validates a complete operation-specific removal route.
func NewRouteAdmission(input RouteAdmissionInput) (RouteAdmission, error) {
	if input.Operation.Operation() != lock.OperationRemove {
		return RouteAdmission{}, fmt.Errorf(
			"carrier removal route requires %q operation, got %q",
			lock.OperationRemove,
			input.Operation.Operation(),
		)
	}
	if input.Operation.Actuation() != lock.ActuationDelegatedHostRoute &&
		input.Operation.Actuation() != lock.ActuationDirectProjection {
		return RouteAdmission{}, fmt.Errorf(
			"carrier removal route requires delegated-host or direct-projection actuation",
		)
	}
	if input.Operation.Authority() != lock.AuthorityRemove {
		return RouteAdmission{}, fmt.Errorf("carrier removal route requires remove authority")
	}
	if !input.Operation.OrdinaryMutationEligible() {
		return RouteAdmission{}, fmt.Errorf("carrier removal operation contract is not eligible for ordinary mutation")
	}
	if input.Operation.Verification() != lock.VerificationHostRelation {
		return RouteAdmission{}, fmt.Errorf("carrier removal route requires host-relation verification")
	}
	if err := input.Request.Validate(); err != nil {
		return RouteAdmission{}, fmt.Errorf("carrier removal route request: %w", err)
	}
	route := input.Operation.Route()
	if input.Request.RouteID() != route.RouteID ||
		input.Request.ContractVersion() != route.AdapterContractVersion {
		return RouteAdmission{}, fmt.Errorf("carrier removal route request does not match operation contract")
	}
	removed, err := canonicalDisclosureSet("removed effect", input.RemovedEffects, true)
	if err != nil {
		return RouteAdmission{}, err
	}
	retained, err := canonicalDisclosureSet("retained effect", input.RetainedEffects, false)
	if err != nil {
		return RouteAdmission{}, err
	}
	nonClaims, err := canonicalDisclosureSet("removal non-claim", input.NonClaims, false)
	if err != nil {
		return RouteAdmission{}, err
	}
	return RouteAdmission{
		status:                 RouteAdmitted,
		operation:              input.Operation,
		request:                input.Request,
		preservesSharedCarrier: input.PreservesSharedCarrier,
		removedEffects:         removed,
		retainedEffects:        retained,
		nonClaims:              nonClaims,
	}, nil
}

// Validate rejects forged or internally inconsistent route admission.
func (admission RouteAdmission) Validate() error {
	switch admission.status {
	case RouteUnavailable:
		if admission.operation.Operation() != "" ||
			admission.request.RouteID() != "" ||
			admission.preservesSharedCarrier ||
			len(admission.removedEffects) != 0 ||
			len(admission.retainedEffects) != 0 ||
			len(admission.nonClaims) != 0 {
			return fmt.Errorf("unavailable carrier removal route carries admitted route facts")
		}
		return nil
	case RouteAdmitted:
		expected, err := NewRouteAdmission(RouteAdmissionInput{
			Operation:              admission.operation,
			Request:                admission.request,
			PreservesSharedCarrier: admission.preservesSharedCarrier,
			RemovedEffects:         admission.removedEffects,
			RetainedEffects:        admission.retainedEffects,
			NonClaims:              admission.nonClaims,
		})
		if err != nil {
			return err
		}
		if !admission.equal(expected) {
			return fmt.Errorf("carrier removal route admission is not canonical")
		}
		return nil
	default:
		return fmt.Errorf("carrier removal route admission status %q is unsupported", admission.status)
	}
}

// Status returns whether the exact removal route was admitted.
func (admission RouteAdmission) Status() RouteAdmissionStatus { return admission.status }

// Operation returns the admitted removal contract. The zero contract is
// returned when the route is unavailable.
func (admission RouteAdmission) Operation() lock.OperationContract { return admission.operation }

// Request returns the exact removal route request. The zero request is
// returned when the route is unavailable.
func (admission RouteAdmission) Request() realizationdelegate.Request { return admission.request }

// InvokesHostRoute reports whether the admitted removal delegates mutation to
// a host command.
func (admission RouteAdmission) InvokesHostRoute() bool {
	return admission.status == RouteAdmitted &&
		admission.operation.Actuation() == lock.ActuationDelegatedHostRoute
}

// MutatesDirectProjection reports whether the admitted removal directly edits
// one bounded host projection.
func (admission RouteAdmission) MutatesDirectProjection() bool {
	return admission.status == RouteAdmitted &&
		admission.operation.Actuation() == lock.ActuationDirectProjection
}

// PreservesSharedCarrier reports whether the route can remove only the selected
// relation while retaining a carrier used by other daem-known consumers.
func (admission RouteAdmission) PreservesSharedCarrier() bool {
	return admission.preservesSharedCarrier
}

// RemovedEffects returns the complete admitted deletion envelope.
func (admission RouteAdmission) RemovedEffects() []string {
	return append([]string(nil), admission.removedEffects...)
}

// RetainedEffects returns effects explicitly retained by the route.
func (admission RouteAdmission) RetainedEffects() []string {
	return append([]string(nil), admission.retainedEffects...)
}

// NonClaims returns authority deliberately excluded from this route.
func (admission RouteAdmission) NonClaims() []string {
	return append([]string(nil), admission.nonClaims...)
}

func (admission RouteAdmission) equal(other RouteAdmission) bool {
	return admission.status == other.status &&
		equalOperationContracts(admission.operation, other.operation) &&
		admission.request.Equal(other.request) &&
		admission.preservesSharedCarrier == other.preservesSharedCarrier &&
		slices.Equal(admission.removedEffects, other.removedEffects) &&
		slices.Equal(admission.retainedEffects, other.retainedEffects) &&
		slices.Equal(admission.nonClaims, other.nonClaims)
}

func equalOperationContracts(left lock.OperationContract, right lock.OperationContract) bool {
	return left.Operation() == right.Operation() &&
		left.Actuation() == right.Actuation() &&
		left.Authority() == right.Authority() &&
		left.Route() == right.Route() &&
		left.HostCompatibility() == right.HostCompatibility() &&
		slices.Equal(left.Preconditions(), right.Preconditions()) &&
		left.EffectEnvelope() == right.EffectEnvelope() &&
		left.EffectPostconditions().Equal(right.EffectPostconditions()) &&
		left.Idempotency() == right.Idempotency() &&
		left.Verification() == right.Verification() &&
		left.TrustActivation() == right.TrustActivation() &&
		left.Recovery() == right.Recovery()
}

func canonicalDisclosureSet(label string, values []string, requireOne bool) ([]string, error) {
	canonical := append([]string(nil), values...)
	for index, value := range canonical {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("%s[%d] must be a non-empty trimmed token", label, index)
		}
		for _, character := range value {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') ||
				character == '_' ||
				character == '-' ||
				character == '.' {
				continue
			}
			return nil, fmt.Errorf("%s[%d] %q must be a stable token", label, index, value)
		}
	}
	sort.Strings(canonical)
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1] == canonical[index] {
			return nil, fmt.Errorf("%s %q is duplicated", label, canonical[index])
		}
	}
	if requireOne && len(canonical) == 0 {
		return nil, fmt.Errorf("carrier removal route requires at least one removed effect")
	}
	return canonical, nil
}
