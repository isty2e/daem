package nativeconfig

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

// QualifyCaptureOwnership checks selected read facts, not a host mutation or
// complete same-document ownership transition. The caller retains the evidence.
// The result identifies an admitted key/recorded-whole narrowing handoff.
func QualifyCaptureOwnership(ctx context.Context, input readiness.NativeAssessmentInput, config hostconfig.Config, placement configuration.Placement, selected []output.Footprint) (narrowingHandoff bool, err error) {
	if ctx == nil || input.Documents == nil || len(input.Documents.Evidence().Namespaces()) != 1 {
		return false, fmt.Errorf("capture requires one retained native document")
	}
	scope, err := input.Documents.Evidence().RegistryScope()
	if err != nil {
		return false, err
	}
	if err := scope.RequireProtectionClear(input.Registry.PendingProtections()); err != nil {
		return false, err
	}
	namespace := input.Documents.Evidence().Namespaces()[0].Observation()
	comparison, exact := namespace.Exact()
	binding, bound := input.Registry.DocumentBinding(input.Owner.Key(), placement.Destination())
	if !exact && bound {
		missing, _ := namespace.Provisional()
		if !missing.MatchesMissingExact(binding.PathAuthority()) {
			return false, fmt.Errorf("capture binding differs from missing native document")
		}
		comparison = binding.PathAuthority()
	}

	var expected []output.Footprint
	wholeBefore := false
	before := input.Documents.Evidence().BeforeState()
	for _, row := range before.ManagedPaths() {
		id, present := topologyprojection.EntityID(row.Subject())
		if !present || id != config.ID() || !aggregate.IsHostConfigDocumentSubject(row.Subject()) {
			continue
		}
		if err := profile.ValidateHostConfigDocumentOccupancy(row.Subject(), row.ConsumerTargets(), row.Scope(), row.Destination(), row.ContentKind(), row.PermissionPolicy(), row.FileMode()); err != nil {
			return false, err
		}
		if row.Destination() != placement.Destination() || row.Scope() != config.Scope() || row.ConsumerTargets()[0] != config.Target() {
			return false, fmt.Errorf("capture whole history differs from selected placement")
		}
		wholeBefore = true
		expected = append(expected, output.WholePathFootprint())
	}
	for _, row := range before.ManagedAggregates() {
		id, _, present := topologyprojection.HostConfigKeyIdentity(row.Subject())
		contract := row.Contribution().Contract()
		if !present || id != config.ID() || contract.Address().Document().AggregateRoot() != placement.Destination() {
			continue
		}
		grammar, native := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID())
		if !native || grammar != string(placement.Grammar()) || contract.Address().Document().Target() != config.Target() || contract.Address().Document().Scope() != config.Scope() {
			return false, fmt.Errorf("capture key history differs from selected profile")
		}
		expected = append(expected, contract.Address().Footprint())
	}
	if len(expected) != 0 && (!bound || comparison.IsZero() || !binding.PathAuthority().Equal(comparison)) {
		return false, fmt.Errorf("capture history requires its current physical binding")
	}

	var claims []ownership.Claim
	for _, footprint := range selected {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := profile.ValidateHostConfigFootprint(placement, footprint); err != nil {
			return false, err
		}
		if comparison.IsZero() {
			missing, _ := namespace.Provisional()
			if _, conflict := input.Registry.ProvisionalAncestorConflict(missing); conflict {
				return false, fmt.Errorf("capture missing native document overlaps ownership")
			}
			continue
		}
		address, err := ownership.NewManagedAddress(comparison, footprint)
		if err != nil {
			return false, err
		}
		overlaps, err := input.Registry.Overlapping(address)
		if err != nil {
			return false, err
		}
		for _, claim := range overlaps {
			if claim.State() != ownership.ClaimActive || !claim.OwnedBy(input.Owner) || !claim.Address().PathAuthority().Equal(comparison) || !slices.Contains(expected, claim.Address().Footprint()) {
				return false, fmt.Errorf("selected capture overlaps reserved, foreign or unrelated ownership")
			}
			claims = append(claims, claim)
		}
	}

	for _, footprint := range expected {
		if slices.ContainsFunc(selected, footprint.Overlaps) && !slices.ContainsFunc(claims, func(claim ownership.Claim) bool { return claim.Address().Footprint().Equal(footprint) }) {
			return false, fmt.Errorf("%w: capture history differs from its selected active claims", ownership.ErrClaimMissing)
		}
	}
	return wholeBefore && config.Ownership() == hostconfig.OwnershipKeys, nil
}
