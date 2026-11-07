package profile

import (
	"errors"
	"fmt"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

var ErrHostConfigReserved = errors.New("host configuration overlaps a reserved area")

type hostConfigFieldReservation struct {
	key   string
	owner string
}

type HostConfigPlacementInput struct {
	Target    target.Target
	Scope     target.Scope
	Path      string
	Ownership desiredhostconfig.Ownership
	Syntax    desiredhostconfig.Syntax
}

// HostConfigPlacement contains static facts only. It is not physical admission
// or evidence that the required observations have completed.
type HostConfigPlacement struct {
	target          target.Target
	scope           target.Scope
	nativePath      string
	ownership       desiredhostconfig.Ownership
	requestedSyntax desiredhostconfig.Syntax
	documentKind    hostConfigDocumentKind
	document        configuration.DocumentSpec
}

func ResolveHostConfigPlacement(input HostConfigPlacementInput) (HostConfigPlacement, error) {
	selected, err := target.ParseTarget(string(input.Target))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if selected != target.TargetPi && selected != target.TargetCodex {
		return HostConfigPlacement{}, fmt.Errorf("host configuration target %q is unsupported", selected)
	}
	scope, err := target.ParseScope(string(input.Scope))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if err := desiredhostconfig.ValidateRelativePath(input.Path); err != nil {
		return HostConfigPlacement{}, err
	}
	ownership, err := desiredhostconfig.ParseOwnership(string(input.Ownership))
	if err != nil {
		return HostConfigPlacement{}, err
	}
	syntax, err := desiredhostconfig.ParseSyntax(string(input.Syntax))
	if err != nil {
		return HostConfigPlacement{}, err
	}

	if owner, reserved := hostConfigPathOwner(selected, scope, input.Path); reserved {
		return HostConfigPlacement{}, fmt.Errorf("%w: %q belongs to %s", ErrHostConfigReserved, input.Path, owner)
	}
	document, err := configuration.NewDocumentSpec(configuration.DocumentInput{
		Target: selected, Scope: scope, Path: input.Path, Syntax: syntax,
	})
	if err != nil {
		if errors.Is(err, configuration.ErrDocumentPath) {
			return HostConfigPlacement{}, fmt.Errorf("%w: %w", ErrHostConfigReserved, err)
		}
		return HostConfigPlacement{}, err
	}
	if ownership == desiredhostconfig.OwnershipDocument && document.Class() == configuration.SharedConfiguration {
		return HostConfigPlacement{}, fmt.Errorf("%w: %q requires key ownership alongside its dedicated resources", ErrHostConfigReserved, input.Path)
	}
	if ownership == desiredhostconfig.OwnershipKeys && document.Grammar() == configuration.Opaque {
		return HostConfigPlacement{}, fmt.Errorf("%w: key ownership requires an explicit supported syntax for %q", configuration.ErrGrammar, input.Path)
	}
	return HostConfigPlacement{
		target: selected, scope: scope, nativePath: input.Path,
		ownership: ownership, requestedSyntax: syntax,
		documentKind: hostConfigReservationKind(selected, document.Class()), document: document,
	}, nil
}

func (placement HostConfigPlacement) Target() target.Target { return placement.target }
func (placement HostConfigPlacement) Scope() target.Scope   { return placement.scope }
func (placement HostConfigPlacement) NativePath() string    { return placement.nativePath }
func (placement HostConfigPlacement) Ownership() desiredhostconfig.Ownership {
	return placement.ownership
}

func (placement HostConfigPlacement) Grammar() configuration.Grammar {
	return placement.document.Grammar()
}

func (placement HostConfigPlacement) Destination() output.Destination {
	return placement.document.Destination()
}

func (placement HostConfigPlacement) Document() configuration.DocumentSpec { return placement.document }

func (placement HostConfigPlacement) RequestedSyntax() desiredhostconfig.Syntax {
	return placement.requestedSyntax
}

func (placement HostConfigPlacement) ID() string {
	if placement.documentKind == 0 {
		return ""
	}
	id, _ := aggregate.HostConfigPlacementID(placement.target, placement.scope)
	return id
}

func (placement HostConfigPlacement) Validate() error {
	canonical, err := ResolveHostConfigPlacement(HostConfigPlacementInput{
		Target: placement.target, Scope: placement.scope, Path: placement.nativePath,
		Ownership: placement.ownership, Syntax: placement.requestedSyntax,
	})
	if err != nil {
		return err
	}
	if canonical != placement {
		return fmt.Errorf("host configuration placement is not canonical")
	}
	return nil
}

// ValidateStaticFootprint checks logical ownership only, never current aliases,
// installed resources or host loader state.
func (placement HostConfigPlacement) ValidateStaticFootprint(footprint output.Footprint) error {
	if placement.documentKind == 0 {
		return fmt.Errorf("host configuration placement is required")
	}
	if err := footprint.Validate(); err != nil {
		return err
	}
	if placement.ownership == desiredhostconfig.OwnershipDocument {
		if footprint.Kind() != output.FootprintPath {
			return fmt.Errorf("document ownership requires a whole-path footprint")
		}
		return nil
	}
	key, keyOwned := footprint.Key()
	if !keyOwned {
		return fmt.Errorf("key ownership requires a native key footprint")
	}
	for _, field := range hostConfigReservedFields(placement.documentKind) {
		reserved, err := output.NewKeyPath(field.key)
		if err != nil {
			return err
		}
		if reserved.Overlaps(key) {
			return fmt.Errorf("%w: %q belongs to %s; use its dedicated resource", ErrHostConfigReserved, key.String(), field.owner)
		}
	}
	return nil
}

func (placement HostConfigPlacement) RequiredObservations() []configuration.Observation {
	if placement.documentKind == 0 {
		return nil
	}
	return placement.document.RequiredObservations()
}

func hostConfigReservedFields(kind hostConfigDocumentKind) []hostConfigFieldReservation {
	if kind == hostConfigPiSettings {
		return []hostConfigFieldReservation{
			{"packages", "extension packages"},
			{"extensions", "extensions"},
			{"skills", "skills"},
		}
	}
	if kind == hostConfigCodexConfiguration {
		return []hostConfigFieldReservation{
			{"mcp_servers", "MCP servers"},
			{"hooks", "hooks"},
			{"plugins", "extensions"},
			{"marketplaces", "extension marketplaces"},
		}
	}
	return nil
}

type hostConfigDocumentKind uint8

const (
	hostConfigOrdinaryDocument hostConfigDocumentKind = iota + 1
	hostConfigPiSettings
	hostConfigCodexConfiguration
)

func hostConfigReservationKind(selected target.Target, class configuration.DocumentClass) hostConfigDocumentKind {
	if class == configuration.SharedConfiguration {
		if selected == target.TargetPi {
			return hostConfigPiSettings
		}
		if selected == target.TargetCodex {
			return hostConfigCodexConfiguration
		}
	}
	return hostConfigOrdinaryDocument
}
