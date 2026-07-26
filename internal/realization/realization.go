package realization

import (
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/realization/aggregate"
	delegaterealization "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

// RealizationKind identifies one closed structural host realization form.
type RealizationKind string

const (
	RealizationManagedPathProjection        RealizationKind = "managed_path_projection"
	RealizationManagedAggregateContribution RealizationKind = "managed_aggregate_contribution"
	RealizationDelegatedRelation            RealizationKind = "delegated_relation"
)

// PathProjectionContentKind identifies the shape occupying a managed path.
type PathProjectionContentKind string

const (
	PathProjectionFile      PathProjectionContentKind = "file"
	PathProjectionDirectory PathProjectionContentKind = "directory"
)

// PathProjectionMode identifies how exact content is materialized at a managed path.
type PathProjectionMode string

const (
	PathProjectionCopy     PathProjectionMode = "copy"
	PathProjectionSymlink  PathProjectionMode = "symlink"
	PathProjectionHardlink PathProjectionMode = "hardlink"
)

// DelegatedRelationInput carries one exact desired host-managed relation request.
type DelegatedRelationInput struct {
	PlacementID            string
	Target                 target.Target
	Scope                  target.Scope
	SourceNamespace        string
	ExpectedRelation       hostrelation.ExpectedRelation
	RouteID                string
	RouteContractVersion   string
	CanonicalRequestHash   string
	VerifiedRelationFields []string
}

// DelegatedRelation is one desired relation whose effects are delegated to a host route.
type DelegatedRelation struct {
	placementID            string
	target                 target.Target
	scope                  target.Scope
	sourceNamespace        string
	expectedRelation       hostrelation.ExpectedRelation
	routeRequest           delegaterealization.Request
	verifiedRelationFields []string
}

// RealizationSpec is a closed union over the admitted structural realization forms.
type RealizationSpec struct {
	kind                  RealizationKind
	pathProjection        *ManagedPathProjection
	aggregateContribution *aggregate.ManagedContribution
	delegatedRelation     *DelegatedRelation
}

// NewManagedAggregateContribution constructs a sibling-preserving aggregate realization.
func NewManagedAggregateContribution(input aggregate.ManagedContributionInput) (RealizationSpec, error) {
	contribution, err := aggregate.NewManagedContribution(input)
	if err != nil {
		return RealizationSpec{}, err
	}
	return RealizationSpec{kind: RealizationManagedAggregateContribution, aggregateContribution: &contribution}, nil
}

// NewDelegatedRelation constructs a delegated relation realization without attempt evidence.
func NewDelegatedRelation(input DelegatedRelationInput) (RealizationSpec, error) {
	routeRequest, err := delegaterealization.NewRequest(
		input.RouteID,
		input.RouteContractVersion,
		input.CanonicalRequestHash,
	)
	if err != nil {
		return RealizationSpec{}, err
	}
	relation := DelegatedRelation{
		placementID:            strings.TrimSpace(input.PlacementID),
		target:                 input.Target,
		scope:                  input.Scope,
		sourceNamespace:        strings.TrimSpace(input.SourceNamespace),
		expectedRelation:       input.ExpectedRelation,
		routeRequest:           routeRequest,
		verifiedRelationFields: canonicalStringSet(input.VerifiedRelationFields),
	}
	if err := relation.validate(); err != nil {
		return RealizationSpec{}, err
	}
	return RealizationSpec{kind: RealizationDelegatedRelation, delegatedRelation: &relation}, nil
}

// Kind returns the exact closed realization variant.
func (spec RealizationSpec) Kind() RealizationKind { return spec.kind }

// ManagedPathProjection returns the path body when this is a path realization.
func (spec RealizationSpec) ManagedPathProjection() (ManagedPathProjection, bool) {
	if spec.kind != RealizationManagedPathProjection || spec.pathProjection == nil {
		return ManagedPathProjection{}, false
	}
	return cloneManagedPathProjection(*spec.pathProjection), true
}

// ManagedAggregateContribution returns the aggregate body when this is an aggregate realization.
func (spec RealizationSpec) ManagedAggregateContribution() (aggregate.ManagedContribution, bool) {
	if spec.kind != RealizationManagedAggregateContribution || spec.aggregateContribution == nil {
		return aggregate.ManagedContribution{}, false
	}
	return spec.aggregateContribution.Clone(), true
}

// DelegatedRelation returns the relation body when this is a delegated realization.
func (spec RealizationSpec) DelegatedRelation() (DelegatedRelation, bool) {
	if spec.kind != RealizationDelegatedRelation || spec.delegatedRelation == nil {
		return DelegatedRelation{}, false
	}
	return cloneDelegatedRelation(*spec.delegatedRelation), true
}

// PlacementID returns the static host placement selected by a valid realization.
func (spec RealizationSpec) PlacementID() string {
	switch spec.kind {
	case RealizationManagedPathProjection:
		if spec.pathProjection != nil {
			return spec.pathProjection.placementID
		}
	case RealizationManagedAggregateContribution:
		if spec.aggregateContribution != nil {
			return spec.aggregateContribution.PlacementID()
		}
	case RealizationDelegatedRelation:
		if spec.delegatedRelation != nil {
			return spec.delegatedRelation.placementID
		}
	}
	return ""
}

// Target returns the singular host target selected by aggregate and delegated
// realizations. Managed paths expose ConsumerTargets instead of inventing a
// primary target for shared physical occupancy.
func (spec RealizationSpec) Target() target.Target {
	switch spec.kind {
	case RealizationManagedAggregateContribution:
		if spec.aggregateContribution != nil {
			return spec.aggregateContribution.Target()
		}
	case RealizationDelegatedRelation:
		if spec.delegatedRelation != nil {
			return spec.delegatedRelation.target
		}
	}
	return ""
}

// ConsumerTargets returns every target consuming this realization.
func (spec RealizationSpec) ConsumerTargets() []target.Target {
	switch spec.kind {
	case RealizationManagedPathProjection:
		if spec.pathProjection != nil {
			return append([]target.Target(nil), spec.pathProjection.consumerTargets...)
		}
	case RealizationManagedAggregateContribution:
		if spec.aggregateContribution != nil {
			return []target.Target{spec.aggregateContribution.Target()}
		}
	case RealizationDelegatedRelation:
		if spec.delegatedRelation != nil {
			return []target.Target{spec.delegatedRelation.target}
		}
	}
	return nil
}

// Scope returns the host scope selected by a valid realization.
func (spec RealizationSpec) Scope() target.Scope {
	switch spec.kind {
	case RealizationManagedPathProjection:
		if spec.pathProjection != nil {
			return spec.pathProjection.scope
		}
	case RealizationManagedAggregateContribution:
		if spec.aggregateContribution != nil {
			return spec.aggregateContribution.Scope()
		}
	case RealizationDelegatedRelation:
		if spec.delegatedRelation != nil {
			return spec.delegatedRelation.scope
		}
	}
	return ""
}

// Equal reports whether two valid realization specs contain identical structural facts.
func (spec RealizationSpec) Equal(other RealizationSpec) bool {
	if spec.Validate() != nil || other.Validate() != nil || spec.kind != other.kind {
		return false
	}
	switch spec.kind {
	case RealizationManagedPathProjection:
		return managedPathProjectionsEqual(*spec.pathProjection, *other.pathProjection)
	case RealizationManagedAggregateContribution:
		return spec.aggregateContribution.Equal(*other.aggregateContribution)
	case RealizationDelegatedRelation:
		return delegatedRelationsEqual(*spec.delegatedRelation, *other.delegatedRelation)
	default:
		return false
	}
}

func delegatedRelationsEqual(left DelegatedRelation, right DelegatedRelation) bool {
	return left.placementID == right.placementID &&
		left.target == right.target &&
		left.scope == right.scope &&
		left.sourceNamespace == right.sourceNamespace &&
		left.expectedRelation.Equal(right.expectedRelation) &&
		left.routeRequest.Equal(right.routeRequest) &&
		slices.Equal(left.verifiedRelationFields, right.verifiedRelationFields)
}

func cloneDelegatedRelation(value DelegatedRelation) DelegatedRelation {
	value.verifiedRelationFields = append([]string(nil), value.verifiedRelationFields...)
	return value
}

func (relation DelegatedRelation) PlacementID() string     { return relation.placementID }
func (relation DelegatedRelation) Target() target.Target   { return relation.target }
func (relation DelegatedRelation) Scope() target.Scope     { return relation.scope }
func (relation DelegatedRelation) SourceNamespace() string { return relation.sourceNamespace }
func (relation DelegatedRelation) ExpectedRelation() hostrelation.ExpectedRelation {
	return relation.expectedRelation
}
func (relation DelegatedRelation) RouteID() string { return relation.routeRequest.RouteID() }
func (relation DelegatedRelation) RouteContractVersion() string {
	return relation.routeRequest.ContractVersion()
}

func (relation DelegatedRelation) CanonicalRequestHash() string {
	return relation.routeRequest.CanonicalRequestHash()
}

// RouteRequest returns the exact delegated route identity without granting execution authority.
func (relation DelegatedRelation) RouteRequest() delegaterealization.Request {
	return relation.routeRequest
}

func (relation DelegatedRelation) VerifiedRelationFields() []string {
	return append([]string(nil), relation.verifiedRelationFields...)
}
