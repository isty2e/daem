package configuration

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/hostsurface"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

var ErrReserved = errors.New("host configuration overlaps a reserved area")

type PlacementInput struct {
	Target    target.Target
	Scope     target.Scope
	Path      string
	Ownership hostconfig.Ownership
	Syntax    hostconfig.Syntax
}

// Placement retains declaration intent and document identity, not selected key
// sets, completed observations or physical mutation authority.
type Placement struct {
	surface         hostsurface.SurfaceKey
	ownership       hostconfig.Ownership
	requestedSyntax hostconfig.Syntax
	document        DocumentSpec
}

func NewPlacement(input PlacementInput, reservations DedicatedReservations) (Placement, error) {
	surface, err := hostsurface.NewSurfaceKey(input.Target, input.Scope, entity.KindHostConfig, hostsurface.VariantDefault)
	if err != nil {
		return Placement{}, err
	}
	if err := hostconfig.ValidateRelativePath(input.Path); err != nil {
		return Placement{}, err
	}
	ownership, err := hostconfig.ParseOwnership(string(input.Ownership))
	if err != nil {
		return Placement{}, err
	}
	syntax, err := hostconfig.ParseSyntax(string(input.Syntax))
	if err != nil {
		return Placement{}, err
	}

	if err := requireNativePath(surface.Target(), surface.Scope(), input.Path); err != nil {
		return Placement{}, err
	}
	if err := reservations.RequireUnreservedPath(input.Path); err != nil {
		if errors.Is(err, ErrReservedPath) {
			return Placement{}, fmt.Errorf("%w: %w", ErrReserved, err)
		}
		return Placement{}, err
	}
	placement, err := placementFromIntent(surface, input.Path, ownership, syntax)
	if err != nil {
		return Placement{}, err
	}
	if err := reservations.ValidateDocument(placement.document); err != nil {
		return Placement{}, err
	}
	return placement, nil
}

func placementFromIntent(surface hostsurface.SurfaceKey, nativePath string, ownership hostconfig.Ownership, syntax hostconfig.Syntax) (Placement, error) {
	document, err := NewDocumentSpec(DocumentInput{
		Target: surface.Target(), Scope: surface.Scope(), Path: nativePath, Syntax: syntax,
	})
	if err != nil {
		if errors.Is(err, ErrDocumentPath) {
			return Placement{}, fmt.Errorf("%w: %w", ErrReserved, err)
		}
		return Placement{}, err
	}
	if ownership == hostconfig.OwnershipDocument && document.Class() == SharedConfiguration {
		return Placement{}, fmt.Errorf("%w: %q requires key ownership alongside its dedicated resources", ErrReserved, nativePath)
	}
	if ownership == hostconfig.OwnershipKeys && document.Grammar() == Opaque {
		return Placement{}, fmt.Errorf("%w: key ownership requires an explicit supported syntax for %q", ErrGrammar, nativePath)
	}
	return Placement{surface: surface, ownership: ownership, requestedSyntax: syntax, document: document}, nil
}

func (placement Placement) SurfaceKey() hostsurface.SurfaceKey { return placement.surface }
func (placement Placement) Target() target.Target              { return placement.surface.Target() }
func (placement Placement) Scope() target.Scope                { return placement.surface.Scope() }
func (placement Placement) Ownership() hostconfig.Ownership    { return placement.ownership }
func (placement Placement) Grammar() Grammar                   { return placement.document.Grammar() }

func (placement Placement) UsesKeyOwnership() bool {
	return placement.ownership == hostconfig.OwnershipKeys
}

func (placement Placement) Destination() output.Destination { return placement.document.Destination() }
func (placement Placement) Document() DocumentSpec          { return placement.document }

func (placement Placement) NativePath() string {
	nativePath, _ := placement.document.NativePlacementPath()
	return nativePath
}

func (placement Placement) RequestedSyntax() hostconfig.Syntax { return placement.requestedSyntax }

func (placement Placement) ID() string {
	if placement.document.RootReference().Validate() != nil {
		return ""
	}
	id, _ := aggregate.HostConfigPlacementID(placement.Target(), placement.Scope())
	return id
}

func (placement Placement) Validate() error {
	if err := placement.surface.Validate(); err != nil {
		return err
	}
	if placement.surface.Kind() != entity.KindHostConfig || placement.surface.Variant() != hostsurface.VariantDefault {
		return fmt.Errorf("native placement requires the default HostConfig surface")
	}
	ownership, err := hostconfig.ParseOwnership(string(placement.ownership))
	if err != nil {
		return err
	}
	syntax, err := hostconfig.ParseSyntax(string(placement.requestedSyntax))
	if err != nil {
		return err
	}
	nativePath, native := placement.document.NativePlacementPath()
	if !native {
		return fmt.Errorf("native placement requires a document in its native layout")
	}

	canonical, err := placementFromIntent(placement.surface, nativePath, ownership, syntax)
	if err != nil {
		return err
	}
	if canonical != placement {
		return fmt.Errorf("host configuration placement is not canonical")
	}
	return nil
}

func (placement Placement) RequiredObservations() []Observation {
	if placement.document.RootReference().Validate() != nil {
		return nil
	}
	return placement.document.RequiredObservations()
}
