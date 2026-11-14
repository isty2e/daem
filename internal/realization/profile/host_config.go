package profile

import (
	"errors"
	"fmt"
	"sync"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func ResolveHostConfigPlacement(input configuration.PlacementInput) (configuration.Placement, error) {
	facts, err := nativeConfigurationReservationsFor(input.Target, input.Scope)
	if err != nil {
		return configuration.Placement{}, err
	}
	return configuration.NewPlacement(input, facts)
}

// ValidateHostConfigFootprint joins declaration intent to current static owner
// namespaces, not current aliases, claims or host loader state.
func ValidateHostConfigFootprint(placement configuration.Placement, footprint output.Footprint) error {
	if placement.Document().RootReference().Validate() != nil {
		return fmt.Errorf("host configuration placement is required")
	}
	if err := footprint.Validate(); err != nil {
		return err
	}
	if placement.Ownership() == desiredhostconfig.OwnershipDocument {
		if footprint.Kind() != output.FootprintPath {
			return fmt.Errorf("document ownership requires a whole-path footprint")
		}
		return ValidatePhysicalHostConfigFootprint(placement.Document(), footprint)
	}
	_, keyOwned := footprint.Key()
	if !keyOwned {
		return fmt.Errorf("key ownership requires a native key footprint")
	}
	return ValidatePhysicalHostConfigFootprint(placement.Document(), footprint)
}

// ValidatePhysicalHostConfigFootprint applies the same dedicated owners to a
// document's observed role, rather than trusting a declaration's path spelling.
func ValidatePhysicalHostConfigFootprint(document configuration.DocumentSpec, footprint output.Footprint) error {
	if err := document.RootReference().Validate(); err != nil {
		return err
	}
	if err := footprint.Validate(); err != nil {
		return err
	}
	if footprint.Kind() == output.FootprintPath {
		if document.Class() == configuration.SharedConfiguration {
			return fmt.Errorf("%w: shared native configuration cannot be owned as a whole document", configuration.ErrReserved)
		}
		return nil
	}
	key, keyOwned := footprint.Key()
	if !keyOwned {
		return fmt.Errorf("native configuration requires a whole-path or key footprint")
	}
	facts, err := nativeConfigurationReservationsFor(document.Target(), document.Scope())
	if err != nil {
		return err
	}
	err = facts.RequireUnreservedKey(document, key)
	if errors.Is(err, configuration.ErrReservedKey) {
		return fmt.Errorf("%w: %w; use its dedicated resource", configuration.ErrReserved, err)
	}
	return err
}

func HostConfigProjectionContract(placement configuration.Placement, footprint output.Footprint) (aggregate.ProjectionContract, error) {
	if err := placement.Validate(); err != nil {
		return aggregate.ProjectionContract{}, err
	}
	if placement.Ownership() != desiredhostconfig.OwnershipKeys {
		return aggregate.ProjectionContract{}, fmt.Errorf("native projection requires key ownership")
	}
	if err := ValidateHostConfigFootprint(placement, footprint); err != nil {
		return aggregate.ProjectionContract{}, err
	}
	codec, ok := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	if !ok {
		return aggregate.ProjectionContract{}, fmt.Errorf("native projection grammar is unsupported")
	}
	return aggregate.NewHostConfigProjectionContract(placement.Target(), placement.Scope(), placement.Destination(), footprint, codec)
}

// HostConfigPlacementForContract re-enters current static admission, never
// treating decoded native shape as mutation authority.
func HostConfigPlacementForContract(contract aggregate.ProjectionContract) (configuration.Placement, error) {
	if err := contract.Validate(); err != nil {
		return configuration.Placement{}, err
	}
	syntax, ok := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID())
	if !ok {
		return configuration.Placement{}, fmt.Errorf("native projection codec is unsupported")
	}
	address := contract.Address().Document()
	document, err := configuration.DocumentSpecForAlias(configuration.AliasInput{
		Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot(), Syntax: desiredhostconfig.Syntax(syntax),
	})
	if err != nil {
		return configuration.Placement{}, err
	}
	nativePath, native := document.NativePlacementPath()
	if !native {
		return configuration.Placement{}, fmt.Errorf("native projection destination is outside its host root")
	}
	placement, err := ResolveHostConfigPlacement(configuration.PlacementInput{
		Target: address.Target(), Scope: address.Scope(), Path: nativePath,
		Ownership: desiredhostconfig.OwnershipKeys, Syntax: desiredhostconfig.Syntax(syntax),
	})
	if err != nil {
		return configuration.Placement{}, err
	}
	expected, err := HostConfigProjectionContract(placement, contract.Address().Footprint())
	if err != nil {
		return configuration.Placement{}, err
	}
	if !expected.Equal(contract) {
		return configuration.Placement{}, fmt.Errorf("native projection differs from its host profile")
	}
	return placement, nil
}

func HostConfigOperationRoute(placement configuration.Placement, operation Operation) (OperationRoute, error) {
	if err := placement.Validate(); err != nil {
		return OperationRoute{}, err
	}
	var suffix string
	switch operation {
	case OperationWrite:
		suffix = ".write"
	case OperationRemove:
		suffix = ".remove"
	default:
		return OperationRoute{}, fmt.Errorf("unsupported native operation %q", operation)
	}

	if placement.Ownership() == desiredhostconfig.OwnershipDocument {
		return NewOperationRoute(entity.KindHostConfig, operation, placement.ID(), "managed-host-config-document"+suffix, "native-document.v1")
	}
	codec, supported := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	if !supported {
		return OperationRoute{}, fmt.Errorf("native key route has no codec")
	}
	return NewOperationRoute(entity.KindHostConfig, operation, placement.ID(), "managed-host-config-key"+suffix, string(codec))
}

func HostConfigDocumentProjection(placement configuration.Placement) (realization.RealizationSpec, error) {
	if placement.Ownership() != desiredhostconfig.OwnershipDocument {
		return realization.RealizationSpec{}, fmt.Errorf("whole-document projection requires document ownership")
	}
	route, err := HostConfigOperationRoute(placement, OperationWrite)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	mode, err := realization.NewExactPathPermissionMode(0o600)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	return realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: placement.ID(), ConsumerTargets: []target.Target{placement.Target()}, Scope: placement.Scope(),
		Destination: placement.Destination(), ContentKind: realization.PathProjectionFile,
		PlacementMode: realization.PathProjectionCopy, PermissionPolicy: realization.PathPermissionsExact,
		ExactPermissionMode: mode, AdapterContractVersion: route.AdapterContractVersion(),
	})
}

type nativeConfigurationContext struct {
	target target.Target
	scope  target.Scope
}

// Catalog definitions are immutable for the process version. Each selected
// context compiles once, with compilation errors confined to that context.
var nativeConfigurationReservationFacts = func() map[nativeConfigurationContext]func() (configuration.DedicatedReservations, error) {
	result := make(map[nativeConfigurationContext]func() (configuration.DedicatedReservations, error))
	for _, selected := range target.SupportedTargets() {
		for _, scope := range target.SupportedScopes() {
			result[nativeConfigurationContext{selected, scope}] = sync.OnceValues(func() (configuration.DedicatedReservations, error) {
				return composeNativeConfigurationReservations(selected, scope)
			})
		}
	}
	return result
}()

func nativeConfigurationReservationsFor(selected target.Target, scope target.Scope) (configuration.DedicatedReservations, error) {
	compile, known := nativeConfigurationReservationFacts[nativeConfigurationContext{selected, scope}]
	if !known {
		if _, err := target.ParseTarget(string(selected)); err != nil {
			return configuration.DedicatedReservations{}, err
		}
		if _, err := target.ParseScope(string(scope)); err != nil {
			return configuration.DedicatedReservations{}, err
		}
		return configuration.DedicatedReservations{}, fmt.Errorf("native reservation coverage is missing for %s/%s", selected, scope)
	}
	return compile()
}

func composeNativeConfigurationReservations(selected target.Target, scope target.Scope) (configuration.DedicatedReservations, error) {
	input := configuration.DedicatedReservationsInput{Target: selected, Scope: scope}
	for _, placement := range aggregate.MCPPlacementsForTarget(selected) {
		if placement.Scope() != scope {
			continue
		}
		key, err := placement.NativeKeyPrefix()
		if err != nil {
			return configuration.DedicatedReservations{}, err
		}
		input.MCP = append(input.MCP, key)
		input.MCPDocuments = append(input.MCPDocuments, placement.ConfigPath())
	}
	if placement, present := aggregate.HookPlacementFor(selected, scope); present {
		key, err := placement.NativeKeyPrefix()
		if err != nil {
			return configuration.DedicatedReservations{}, err
		}
		input.Hooks = append(input.Hooks, key)
		input.HookDocuments = append(input.HookDocuments, placement.AggregateRoot())
	}
	for _, route := range profileDelegatedRoutes(selected) {
		keys, err := route.NativeMembershipKeys()
		if err != nil {
			return configuration.DedicatedReservations{}, err
		}
		input.Extensions = append(input.Extensions, keys...)
	}

	for _, placement := range profilePlacements(profilePlacementAdmissions(selected)) {
		if placement.Scope() == scope {
			input.Resources = append(input.Resources, placement.Root())
		}
	}
	for _, location := range profileDiscoveries(selected) {
		if location.Scope() != scope {
			continue
		}
		alias, err := output.Parse(location.Path())
		if err != nil {
			return configuration.DedicatedReservations{}, err
		}
		input.Resources = append(input.Resources, alias)
	}
	return configuration.NewDedicatedReservations(input)
}
