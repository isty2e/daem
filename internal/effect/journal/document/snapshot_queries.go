package document

import (
	"context"
	"slices"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

func (index SnapshotIndex) Subjects(ctx context.Context, aliases []aggregate.DocumentAddress) ([]topology.SubjectID, error) {
	addresses, err := AliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}
	seen := make(map[topology.SubjectID]struct{})
	for _, row := range index.aggregates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, selected := addresses[row.Contribution().Contract().Address().Document()]; selected {
			seen[row.Subject()] = struct{}{}
		}
	}
	for _, row := range index.paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, selected := range row.ConsumerTargets() {
			address, err := aggregate.NewDocumentAddress(selected, row.Scope(), row.Destination())
			if err != nil {
				return nil, err
			}
			if _, present := addresses[address]; present {
				seen[row.Subject()] = struct{}{}
			}
		}
	}
	subjects := make([]topology.SubjectID, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	slices.SortFunc(subjects, topology.CompareSubjectID)
	return subjects, ctx.Err()
}

func (index SnapshotIndex) Footprints(ctx context.Context, aliases []aggregate.DocumentAddress) (map[output.Footprint]struct{}, error) {
	addresses, err := AliasSet(ctx, aliases)
	if err != nil {
		return nil, err
	}
	result := make(map[output.Footprint]struct{})
	for address := range addresses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for footprint := range index.claimFootprints[address] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result[footprint] = struct{}{}
		}
	}
	return result, nil
}

func (indexes SnapshotIndexes) HasMembers(ctx context.Context, aliases []aggregate.DocumentAddress) (bool, error) {
	before, err := indexes.before.Subjects(ctx, aliases)
	if err != nil {
		return false, err
	}
	after, err := indexes.after.Subjects(ctx, aliases)
	if err != nil {
		return false, err
	}
	return len(before)+len(after) != 0, nil
}
