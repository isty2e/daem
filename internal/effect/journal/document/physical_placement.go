package document

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// CarrierTransfer assigns one logical MCP carrier to different physical groups.
// Group numbers are operation-local; this fact grants no root or ownership rights.
type CarrierTransfer struct {
	address        aggregate.DocumentAddress
	source, target int
	initialized    bool
}

func NewCarrierTransfer(address aggregate.DocumentAddress, sourceGroup, targetGroup int) (CarrierTransfer, error) {
	if err := address.Validate(); err != nil {
		return CarrierTransfer{}, err
	}
	if address.Target() != target.TargetClaudeCode || address.Scope() != target.ScopeGlobal || address.AggregateRoot().String() != aggregate.ClaudeGlobalMCPConfigPath {
		return CarrierTransfer{}, fmt.Errorf("carrier transfer requires the Claude global MCP document")
	}
	if sourceGroup < 0 || targetGroup < 0 || sourceGroup == targetGroup {
		return CarrierTransfer{}, fmt.Errorf("carrier transfer requires distinct source and target groups")
	}
	return CarrierTransfer{address: address, source: sourceGroup, target: targetGroup, initialized: true}, nil
}

// PhysicalPlacement resolves portable aliases independently for each State phase.
// Root selection, claim qualification and persisted-format admission stay outside.
type PhysicalPlacement struct {
	groups [][]aggregate.DocumentAddress
	before map[aggregate.DocumentAddress]int
	after  map[aggregate.DocumentAddress]int
}

func ResolvePhysicalPlacement(ctx context.Context, groups [][]aggregate.DocumentAddress, transfers []CarrierTransfer) (PhysicalPlacement, error) {
	if ctx == nil {
		return PhysicalPlacement{}, fmt.Errorf("physical placement context is required")
	}
	result := PhysicalPlacement{
		groups: make([][]aggregate.DocumentAddress, len(groups)),
		before: make(map[aggregate.DocumentAddress]int), after: make(map[aggregate.DocumentAddress]int),
	}
	locations := make(map[aggregate.DocumentAddress][]int)
	for index, aliases := range groups {
		if _, err := AliasSet(ctx, aliases); err != nil {
			return PhysicalPlacement{}, err
		}
		result.groups[index] = slices.Clone(aliases)
		for _, alias := range aliases {
			locations[alias] = append(locations[alias], index)
		}
	}
	moved := make(map[aggregate.DocumentAddress]CarrierTransfer, len(transfers))
	for _, transfer := range transfers {
		if err := ctx.Err(); err != nil {
			return PhysicalPlacement{}, err
		}
		if !transfer.initialized {
			return PhysicalPlacement{}, fmt.Errorf("carrier transfer is required")
		}
		if _, duplicate := moved[transfer.address]; duplicate {
			return PhysicalPlacement{}, fmt.Errorf("physical placement repeats a carrier transfer")
		}
		indices := locations[transfer.address]
		if len(indices) != 2 || !slices.Contains(indices, transfer.source) || !slices.Contains(indices, transfer.target) {
			return PhysicalPlacement{}, fmt.Errorf("carrier transfer requires exactly its declared source and target aliases")
		}
		moved[transfer.address] = transfer
	}
	for alias, indices := range locations {
		if err := ctx.Err(); err != nil {
			return PhysicalPlacement{}, err
		}
		if transfer, present := moved[alias]; present {
			result.before[alias], result.after[alias] = transfer.source, transfer.target
			continue
		}
		if len(indices) != 1 {
			return PhysicalPlacement{}, fmt.Errorf("document alias belongs to more than one group without a carrier transfer")
		}
		result.before[alias], result.after[alias] = indices[0], indices[0]
	}
	if err := ctx.Err(); err != nil {
		return PhysicalPlacement{}, err
	}
	return result, nil
}

func (placement PhysicalPlacement) BeforeGroup(address aggregate.DocumentAddress) (int, bool) {
	index, present := placement.before[address]
	return index, present
}

func (placement PhysicalPlacement) AfterGroup(address aggregate.DocumentAddress) (int, bool) {
	index, present := placement.after[address]
	return index, present
}

func (placement PhysicalPlacement) BeforeAliases(index int) []aggregate.DocumentAddress {
	return placement.aliasesFor(index, placement.before)
}

func (placement PhysicalPlacement) AfterAliases(index int) []aggregate.DocumentAddress {
	return placement.aliasesFor(index, placement.after)
}

func (placement PhysicalPlacement) aliasesFor(index int, positions map[aggregate.DocumentAddress]int) []aggregate.DocumentAddress {
	if index < 0 || index >= len(placement.groups) {
		return nil
	}
	var result []aggregate.DocumentAddress
	for _, alias := range placement.groups[index] {
		if group, present := positions[alias]; present && group == index {
			result = append(result, alias)
		}
	}
	return result
}

func (indexes SnapshotIndexes) validateCarrierTransfers(ctx context.Context, transfers []CarrierTransfer) error {
	for _, transfer := range transfers {
		beforePresent, afterPresent := false, false
		for side, index := range []SnapshotIndex{indexes.before, indexes.after} {
			for _, row := range index.paths {
				if err := ctx.Err(); err != nil {
					return err
				}
				if row.Scope() == transfer.address.Scope() && row.Destination() == transfer.address.AggregateRoot() && slices.Contains(row.ConsumerTargets(), transfer.address.Target()) {
					return fmt.Errorf("carrier transfer cannot move whole-file state")
				}
			}
			for _, row := range index.aggregates {
				if err := ctx.Err(); err != nil {
					return err
				}
				contract := row.Contribution().Contract()
				if contract.Address().Document() != transfer.address {
					continue
				}
				if _, mcp := aggregate.MCPPlacementForCodec(aggregate.MCPPlacementID(contract.Address().PlacementID()), contract.CodecContractID()); !mcp {
					return fmt.Errorf("carrier transfer cannot move non-MCP state")
				}
				beforePresent = beforePresent || side == 0
				afterPresent = afterPresent || side == 1
			}
		}
		if !beforePresent {
			return fmt.Errorf("carrier transfer requires before MCP state")
		}
		if !afterPresent {
			return fmt.Errorf("carrier transfer requires after MCP state; use retirement for removal")
		}
	}
	return ctx.Err()
}

func (indexes SnapshotIndexes) validateCarrierFrontiers(ctx context.Context, groups []StateGroup, transfers []CarrierTransfer) error {
	for index, group := range groups {
		source := slices.ContainsFunc(group.routes, func(route AliasRoute) bool { return route.OwnedSource() }) || slices.ContainsFunc(transfers, func(transfer CarrierTransfer) bool { return transfer.source == index })
		if source {
			if err := indexes.after.validateSourceRootChange(ctx, group); err != nil {
				return err
			}
		}
	}
	for _, transfer := range transfers {
		source := groups[transfer.source]
		for _, row := range indexes.before.aggregates {
			if err := ctx.Err(); err != nil {
				return err
			}
			contract := row.Contribution().Contract()
			if contract.Address().Document() != transfer.address {
				continue
			}
			projection, present := source.comparison.ExpectedState().Projection(contract)
			if !source.Covers(contract) || !present || projection.Present() {
				return fmt.Errorf("carrier transfer source must retire every before MCP projection")
			}
		}
		for _, row := range indexes.after.aggregates {
			if err := ctx.Err(); err != nil {
				return err
			}
			contract := row.Contribution().Contract()
			if contract.Address().Document() == transfer.address && !groups[transfer.target].Covers(contract) {
				return fmt.Errorf("carrier transfer target must cover every after MCP projection")
			}
		}
	}
	return ctx.Err()
}

func (after SnapshotIndex) validateSourceRootChange(ctx context.Context, source StateGroup) error {
	beforeExists, afterExists := source.comparison.BeforeDocument().Exists(), source.comparison.ExpectedDocument().Exists()
	if beforeExists && !afterExists {
		return fmt.Errorf("MCP carrier retirement cannot delete its source document")
	}
	if beforeExists || !afterExists {
		return ctx.Err()
	}
	for _, row := range after.aggregates {
		if err := ctx.Err(); err != nil {
			return err
		}
		contract := row.Contribution().Contract()
		if slices.Contains(source.afterAliases, contract.Address().Document()) && source.Covers(contract) {
			projection, present := source.comparison.ExpectedState().Projection(contract)
			if present && projection.Present() {
				return nil
			}
		}
	}
	return fmt.Errorf("MCP retirement cannot create its source without another selected after projection")
}
