package profile

import (
	"fmt"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// ProjectionContract admits one native key through the placement's logical
// reservations. RequiredObservations still govern physical qualification.
func (placement HostConfigPlacement) ProjectionContract(footprint output.Footprint) (aggregate.ProjectionContract, error) {
	if err := placement.Validate(); err != nil {
		return aggregate.ProjectionContract{}, err
	}
	if placement.Ownership() != desiredhostconfig.OwnershipKeys {
		return aggregate.ProjectionContract{}, fmt.Errorf("native projection requires key ownership")
	}
	if err := placement.ValidateStaticFootprint(footprint); err != nil {
		return aggregate.ProjectionContract{}, err
	}
	codec, ok := aggregate.HostConfigCodecForSyntax(string(placement.Grammar()))
	if !ok {
		return aggregate.ProjectionContract{}, fmt.Errorf("native projection grammar is unsupported")
	}
	return aggregate.NewHostConfigProjectionContract(placement.Target(), placement.Scope(), placement.Destination(), footprint, codec)
}

// HostConfigPlacementForContract revalidates persisted or decoded static facts
// through the same profile as authoring. It grants no mutation authority.
func HostConfigPlacementForContract(contract aggregate.ProjectionContract) (HostConfigPlacement, error) {
	if err := contract.Validate(); err != nil {
		return HostConfigPlacement{}, err
	}
	syntax, ok := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID())
	if !ok {
		return HostConfigPlacement{}, fmt.Errorf("native projection codec is unsupported")
	}
	address := contract.Address().Document()
	document, err := configuration.DocumentSpecForAlias(configuration.AliasInput{
		Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot(), Syntax: desiredhostconfig.Syntax(syntax),
	})
	if err != nil {
		return HostConfigPlacement{}, err
	}
	nativePath, native := document.NativePlacementPath()
	if !native {
		return HostConfigPlacement{}, fmt.Errorf("native projection destination is outside its host root")
	}
	placement, err := ResolveHostConfigPlacement(HostConfigPlacementInput{
		Target: address.Target(), Scope: address.Scope(), Path: nativePath,
		Ownership: desiredhostconfig.OwnershipKeys, Syntax: desiredhostconfig.Syntax(syntax),
	})
	if err != nil {
		return HostConfigPlacement{}, err
	}
	expected, err := placement.ProjectionContract(contract.Address().Footprint())
	if err != nil {
		return HostConfigPlacement{}, err
	}
	if !expected.Equal(contract) {
		return HostConfigPlacement{}, fmt.Errorf("native projection differs from its host profile")
	}
	return placement, nil
}
