package profile

import (
	"fmt"
	"strings"

	desiredhostconfig "github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
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
	nativePath, ok := hostConfigNativePath(address.Target(), address.Scope(), address.AggregateRoot())
	if !ok {
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

func hostConfigNativePath(selected target.Target, scope target.Scope, destination output.Destination) (string, bool) {
	if selected != target.TargetPi && selected != target.TargetCodex {
		return "", false
	}
	if scope == target.ScopeProject && destination.RootRole() == output.RootProject {
		prefix := ".pi/"
		if selected == target.TargetCodex {
			prefix = ".codex/"
		}
		return strings.CutPrefix(destination.RelativePath(), prefix)
	}
	root := output.RootPiAgent
	if selected == target.TargetCodex {
		root = output.RootCodexHome
	}
	if scope == target.ScopeGlobal && destination.RootRole() == root {
		return destination.RelativePath(), true
	}
	return "", false
}
