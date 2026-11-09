package document

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// These indexes retain complete state facts, not validated group authority.
type SnapshotIndex struct {
	paths           map[topology.SubjectID]durable.ManagedPathState
	aggregates      map[topology.SubjectID]durable.ManagedAggregateState
	claimFootprints map[aggregate.DocumentAddress]map[output.Footprint]struct{}
}

type ClaimSelection uint8

const (
	LegacyGlobalClaims ClaimSelection = iota + 1
	NativeClaims
)

type SnapshotIndexes struct {
	before SnapshotIndex
	after  SnapshotIndex
}

func CompareAliases(left, right aggregate.DocumentAddress) int {
	for _, order := range []int{
		cmp.Compare(left.Target(), right.Target()),
		cmp.Compare(left.Scope(), right.Scope()),
		cmp.Compare(left.AggregateRoot().String(), right.AggregateRoot().String()),
	} {
		if order != 0 {
			return order
		}
	}
	return 0
}

func (indexes SnapshotIndexes) Before() SnapshotIndex { return indexes.before }
func (indexes SnapshotIndexes) After() SnapshotIndex  { return indexes.after }

func IndexSnapshots(ctx context.Context, before, after durable.Snapshot) (SnapshotIndexes, error) {
	return IndexSnapshotsForClaims(ctx, before, after, LegacyGlobalClaims)
}

func IndexSnapshotsForClaims(ctx context.Context, before, after durable.Snapshot, claims ClaimSelection) (SnapshotIndexes, error) {
	if ctx == nil {
		return SnapshotIndexes{}, fmt.Errorf("document state context is required")
	}
	beforeIndex, err := IndexSnapshotForClaims(ctx, before, claims)
	if err != nil {
		return SnapshotIndexes{}, err
	}
	afterIndex, err := IndexSnapshotForClaims(ctx, after, claims)
	if err != nil {
		return SnapshotIndexes{}, err
	}
	return SnapshotIndexes{before: beforeIndex, after: afterIndex}, nil
}

func IndexSnapshot(ctx context.Context, snapshot durable.Snapshot) (SnapshotIndex, error) {
	return IndexSnapshotForClaims(ctx, snapshot, LegacyGlobalClaims)
}

func IndexSnapshotForClaims(ctx context.Context, snapshot durable.Snapshot, claims ClaimSelection) (SnapshotIndex, error) {
	if ctx == nil {
		return SnapshotIndex{}, fmt.Errorf("document state context is required")
	}
	if err := ctx.Err(); err != nil {
		return SnapshotIndex{}, err
	}
	if claims != LegacyGlobalClaims && claims != NativeClaims {
		return SnapshotIndex{}, fmt.Errorf("document claim membership requires its admitted selection rule")
	}
	paths, rows := snapshot.ManagedPaths(), snapshot.ManagedAggregates()
	index := SnapshotIndex{
		paths:           make(map[topology.SubjectID]durable.ManagedPathState, len(paths)),
		aggregates:      make(map[topology.SubjectID]durable.ManagedAggregateState, len(rows)),
		claimFootprints: make(map[aggregate.DocumentAddress]map[output.Footprint]struct{}),
	}
	for _, row := range paths {
		if err := ctx.Err(); err != nil {
			return SnapshotIndex{}, err
		}
		index.paths[row.Subject()] = row
		id, native := topologyprojection.EntityID(row.Subject())
		native = native && id.Kind() == entity.KindHostConfig
		selected := native && (claims == NativeClaims || row.Scope() == target.ScopeGlobal) || claims == NativeClaims && row.Scope() == target.ScopeGlobal
		if !selected {
			continue
		}
		for _, selected := range row.ConsumerTargets() {
			if err := ctx.Err(); err != nil {
				return SnapshotIndex{}, err
			}
			address, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return SnapshotIndex{}, err
			}
			index.addClaimFootprint(address, output.WholePathFootprint())
		}
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return SnapshotIndex{}, err
		}
		index.aggregates[row.Subject()] = row
		contract := row.Contribution().Contract()
		_, native := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID())
		if contract.Address().Document().Scope() == target.ScopeGlobal || claims == NativeClaims && native {
			index.addClaimFootprint(contract.Address().Document(), contract.Address().Footprint())
		}
	}
	if err := ctx.Err(); err != nil {
		return SnapshotIndex{}, err
	}
	return index, nil
}

func (index SnapshotIndex) addClaimFootprint(address aggregate.DocumentAddress, footprint output.Footprint) {
	if index.claimFootprints[address] == nil {
		index.claimFootprints[address] = make(map[output.Footprint]struct{})
	}
	index.claimFootprints[address][footprint] = struct{}{}
}

type documentSelectionKey struct {
	document aggregate.DocumentAddress
	codec    aggregate.CodecContractID
}

// SelectedContracts resolves only the declared frontier. Unselected state still
// belongs to the operation's separate membership and changed-row coverage join.
func (indexes SnapshotIndexes) SelectedContracts(ctx context.Context, subjects []topology.SubjectID, aliases []aggregate.DocumentAddress) ([]aggregate.Selection, error) {
	addresses, err := AliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}

	groups := make(map[documentSelectionKey]map[output.KeyPath]aggregate.ProjectionContract)
	seen := make(map[topology.SubjectID]struct{}, len(subjects))
	for index, subject := range subjects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := subject.Validate(); err != nil {
			return nil, fmt.Errorf("selected subject[%d]: %w", index, err)
		}
		if _, duplicate := seen[subject]; duplicate {
			return nil, fmt.Errorf("selected document subjects must be unique")
		}
		seen[subject] = struct{}{}
		_, beforePath := indexes.before.paths[subject]
		_, afterPath := indexes.after.paths[subject]
		if beforePath || afterPath {
			return nil, fmt.Errorf("selected subject[%d] is whole-path state, not a selected projection", index)
		}
		before, beforePresent := selectedContractAt(indexes.before, subject, addresses)
		after, afterPresent := selectedContractAt(indexes.after, subject, addresses)
		if !beforePresent && !afterPresent {
			return nil, fmt.Errorf("selected subject[%d] does not resolve within the document aliases", index)
		}
		if beforePresent && afterPresent && !before.Equal(after) {
			return nil, fmt.Errorf("selected subject[%d] changes its in-group contract", index)
		}
		contract := before
		if afterPresent {
			contract = after
		}
		key := documentSelectionKey{document: contract.Address().Document(), codec: contract.CodecContractID()}
		if groups[key] == nil {
			groups[key] = make(map[output.KeyPath]aggregate.ProjectionContract)
		}
		path := contract.Address().KeyPath()
		if previous, exists := groups[key][path]; exists && !previous.Equal(contract) {
			return nil, fmt.Errorf("selected subject[%d] conflicts at its projection address", index)
		}
		groups[key][path] = contract
	}

	keys := make([]documentSelectionKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareDocumentSelectionKeys)
	var selections []aggregate.Selection
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		contracts := groups[key]
		paths := make([]output.KeyPath, 0, len(contracts))
		for path := range contracts {
			paths = append(paths, path)
		}
		frontier := output.TopmostKeyPaths(paths)
		selected := make([]aggregate.ProjectionContract, 0, len(frontier))
		for _, path := range frontier {
			selected = append(selected, contracts[path])
		}
		selection, err := aggregate.NewSelection(selected)
		if err != nil {
			return nil, err
		}
		selections = append(selections, selection)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return selections, nil
}

func AliasSet(ctx context.Context, aliases []aggregate.DocumentAddress) (map[aggregate.DocumentAddress]struct{}, error) {
	if ctx == nil {
		return nil, fmt.Errorf("document selection context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, fmt.Errorf("document selection aliases are required")
	}
	addresses := make(map[aggregate.DocumentAddress]struct{}, len(aliases))
	for index, alias := range aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := alias.Validate(); err != nil {
			return nil, fmt.Errorf("document alias[%d]: %w", index, err)
		}
		if _, duplicate := addresses[alias]; duplicate {
			return nil, fmt.Errorf("document aliases must be unique")
		}
		addresses[alias] = struct{}{}
	}
	return addresses, nil
}

func selectedContractAt(index SnapshotIndex, subject topology.SubjectID, aliases map[aggregate.DocumentAddress]struct{}) (aggregate.ProjectionContract, bool) {
	row, present := index.aggregates[subject]
	if !present {
		return aggregate.ProjectionContract{}, false
	}
	contract := row.Contribution().Contract()
	_, selected := aliases[contract.Address().Document()]
	return contract, selected
}

func compareDocumentSelectionKeys(left, right documentSelectionKey) int {
	for _, comparison := range []int{
		cmp.Compare(left.document.Target(), right.document.Target()),
		cmp.Compare(left.document.Scope(), right.document.Scope()),
		cmp.Compare(left.document.AggregateRoot().String(), right.document.AggregateRoot().String()),
		cmp.Compare(left.codec, right.codec),
	} {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}
