package ownership

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
)

var (
	ErrMissingStateWithOutputs = errors.New("the selected state file is missing but output ownership remains; daem cannot tell what it last applied")
	ErrAmbiguousStateOwner     = errors.New("the selected state file is missing and daem cannot match the recorded owner to its current location")
)

type MissingStateRefusal struct {
	manifest string
	cause    error
}

func (refusal *MissingStateRefusal) ManifestPath() string { return refusal.manifest }
func (refusal *MissingStateRefusal) Unwrap() error        { return refusal.cause }

func (refusal *MissingStateRefusal) Error() string {
	var protection *outputownership.ProtectionPendingError
	switch {
	case errors.As(refusal.cause, &protection):
		return "the selected state file is missing and permission recovery is pending for its output ownership"
	case errors.Is(refusal.cause, outputownership.ErrOwnerOutputsReserved):
		return "the selected state file is missing and output claims from an interrupted operation remain reserved"
	case errors.Is(refusal.cause, ErrAmbiguousStateOwner):
		return ErrAmbiguousStateOwner.Error()
	case errors.Is(refusal.cause, ErrMissingStateWithOutputs):
		return ErrMissingStateWithOutputs.Error()
	default:
		return "the selected state file is missing and daem could not verify the output ownership registry"
	}
}

// MatchMissingStateOutputs compares recorded keys without reopening the outputs
// or treating a historical alias as authority to release a recorded owner.
func MatchMissingStateOutputs(ctx context.Context, missing statefile.MissingState, manifest string, registry outputownership.Registry) (outputownership.OwnerOutputs, error) {
	return matchMissingStateOutputs(ctx, missing, manifest, registry, false)
}

func matchMissingStateOutputs(ctx context.Context, missing statefile.MissingState, manifest string, registry outputownership.Registry, requireClaim bool) (outputownership.OwnerOutputs, error) {
	if err := ctx.Err(); err != nil {
		return outputownership.OwnerOutputs{}, err
	}
	observed := missing.OwnerPath()
	if err := observed.Validate(); err != nil {
		return outputownership.OwnerOutputs{}, err
	}
	owners, err := registry.RecordedOwners()
	if err != nil {
		return outputownership.OwnerOutputs{}, err
	}
	var selected stateauthority.Key
	var relevantClaim, ambiguous bool
	for _, recorded := range owners {
		if err := ctx.Err(); err != nil {
			return outputownership.OwnerOutputs{}, err
		}
		owner := recorded.Authority
		retained := owner.StatefileAuthority()
		if observed.MatchesRetainedWitness(retained) {
			relevantClaim = relevantClaim || recorded.HasClaim
			selected = owner.Key()
			continue
		}

		var legacy bool
		if exact, known := observed.Exact(); known {
			legacy = exact.MatchesLegacyComparison(retained) || retained.MatchesLegacyComparison(exact)
		} else {
			provisional, _ := observed.Provisional()
			legacy = provisional.MatchesMissingExact(retained)
		}
		if legacy || observed.NamespaceKey() == retained.Key() || owner.ManifestPath() == manifest {
			ambiguous = true
			relevantClaim = relevantClaim || recorded.HasClaim
		}
	}
	if err := ctx.Err(); err != nil {
		return outputownership.OwnerOutputs{}, err
	}
	if requireClaim && !relevantClaim {
		return outputownership.OwnerOutputs{}, nil
	}
	if ambiguous {
		return outputownership.OwnerOutputs{}, &MissingStateRefusal{manifest: manifest, cause: ErrAmbiguousStateOwner}
	}
	if selected == (stateauthority.Key{}) {
		return outputownership.OwnerOutputs{}, nil
	}
	outputs, err := registry.SettledOwnerOutputs(selected)
	if cancellation := ctx.Err(); cancellation != nil {
		return outputownership.OwnerOutputs{}, cancellation
	}
	if err != nil {
		return outputownership.OwnerOutputs{}, &MissingStateRefusal{manifest: manifest, cause: err}
	}
	return outputs, nil
}

func RequireMissingStateClear(ctx context.Context, paths daempaths.Paths, missing statefile.MissingState, budget rootedpath.PhysicalTraversalBudget) error {
	if missing.Path() == "" {
		return ctx.Err()
	}
	if missing.Path() != paths.StatefilePath {
		return fmt.Errorf("observed Statefile absence does not belong to the selected workspace")
	}
	registry, err := ownershipstore.ReadProtectionRegistryBounded(ctx, paths.OwnershipRegistryPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &MissingStateRefusal{manifest: paths.ManifestPath, cause: err}
	}
	return RequireMissingStateRegistryClear(ctx, missing, paths.ManifestPath, registry)
}

func RequireMissingStateRegistryClear(ctx context.Context, missing statefile.MissingState, manifest string, registry outputownership.Registry) error {
	outputs, err := matchMissingStateOutputs(ctx, missing, manifest, registry, true)
	if err != nil {
		return err
	}
	if !outputs.Empty() {
		return &MissingStateRefusal{manifest: manifest, cause: ErrMissingStateWithOutputs}
	}
	return nil
}

// StateAdmission binds execution checks to the State presence observed by a plan.
type StateAdmission struct {
	missing statefile.MissingState
}

func ForPlannedState(missing statefile.MissingState) StateAdmission {
	return StateAdmission{missing: missing}
}

// NeedsRegistryRead reports whether the execution lease must cover the registry
// even when the plan has no document ownership work of its own.
func (admission StateAdmission) NeedsRegistryRead() bool { return admission.missing.Path() != "" }

// Revalidate checks the approved State presence before an
// early maintenance phase, without decoding a second State snapshot.
func (admission StateAdmission) Revalidate(ctx context.Context, paths daempaths.Paths) error {
	budget := physicalwork.NewCaptureBudget(nil)
	current, err := statefile.ObserveMissingState(ctx, paths.StatefilePath, budget)
	if cancellation := ctx.Err(); cancellation != nil {
		return cancellation
	}
	switch {
	case errors.Is(err, mutation.StaleSnapshotError{}):
		return err
	case errors.Is(err, statefile.ErrStatefilePresent):
		if admission.NeedsRegistryRead() {
			return mutation.StaleSnapshotError{}
		}
		return ctx.Err()
	case err != nil:
		return err
	}
	admissionErr := RequireMissingStateClear(ctx, paths, current, budget)
	if !admission.missing.Equal(current) {
		return errors.Join(mutation.StaleSnapshotError{}, admissionErr)
	}
	return admissionErr
}
