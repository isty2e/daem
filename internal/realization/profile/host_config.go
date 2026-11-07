package profile

import (
	"errors"
	"fmt"
	"sync"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

var ErrHostConfigReserved = errors.New("host configuration overlaps a reserved area")

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
	facts, err := nativeConfigurationKeysFor(selected, scope)
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if err := facts.ValidateDocument(document); err != nil {
		return HostConfigPlacement{}, err
	}
	return HostConfigPlacement{
		target: selected, scope: scope, nativePath: input.Path,
		ownership: ownership, requestedSyntax: syntax,
		document: document,
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
	if placement.document.RootReference().Validate() != nil {
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
	if placement.document.RootReference().Validate() != nil {
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
	facts, err := nativeConfigurationKeysFor(placement.target, placement.scope)
	if err != nil {
		return err
	}
	err = facts.RequireUnreservedKey(placement.document, key)
	if errors.Is(err, configuration.ErrReservedKey) {
		return fmt.Errorf("%w: %w; use its dedicated resource", ErrHostConfigReserved, err)
	}
	return err
}

func (placement HostConfigPlacement) RequiredObservations() []configuration.Observation {
	if placement.document.RootReference().Validate() != nil {
		return nil
	}
	return placement.document.RequiredObservations()
}

type nativeKeyContext struct {
	target target.Target
	scope  target.Scope
}

// Catalog definitions are immutable for the process version. Each selected
// context compiles once, with compilation errors confined to that context.
var nativeConfigurationKeyFacts = func() map[nativeKeyContext]func() (configuration.DedicatedKeys, error) {
	result := make(map[nativeKeyContext]func() (configuration.DedicatedKeys, error))
	for _, selected := range target.SupportedTargets() {
		for _, scope := range target.SupportedScopes() {
			result[nativeKeyContext{selected, scope}] = sync.OnceValues(func() (configuration.DedicatedKeys, error) {
				return composeNativeConfigurationKeys(selected, scope)
			})
		}
	}
	return result
}()

func nativeConfigurationKeysFor(selected target.Target, scope target.Scope) (configuration.DedicatedKeys, error) {
	compile, known := nativeConfigurationKeyFacts[nativeKeyContext{selected, scope}]
	if !known {
		return configuration.DedicatedKeys{}, fmt.Errorf("native key coverage is missing for %s/%s", selected, scope)
	}
	return compile()
}

func composeNativeConfigurationKeys(selected target.Target, scope target.Scope) (configuration.DedicatedKeys, error) {
	input := configuration.DedicatedKeysInput{Target: selected, Scope: scope}
	for _, placement := range aggregate.MCPPlacementsForTarget(selected) {
		if placement.Scope() != scope {
			continue
		}
		key, err := placement.NativeKeyPrefix()
		if err != nil {
			return configuration.DedicatedKeys{}, err
		}
		input.MCP = append(input.MCP, key)
	}
	if placement, present := aggregate.HookPlacementFor(selected, scope); present {
		key, err := placement.NativeKeyPrefix()
		if err != nil {
			return configuration.DedicatedKeys{}, err
		}
		input.Hooks = append(input.Hooks, key)
	}
	for _, route := range profileDelegatedRoutes(selected) {
		keys, err := route.NativeMembershipKeys()
		if err != nil {
			return configuration.DedicatedKeys{}, err
		}
		input.Extensions = append(input.Extensions, keys...)
	}
	return configuration.NewDedicatedKeys(input)
}
