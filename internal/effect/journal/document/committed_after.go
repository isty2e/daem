package document

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

func (comparison Comparison) HasSelectedSequences() bool { return len(comparison.sequences) != 0 }

// MatchCommittedAfter does not revalidate retired Before-only projections. The
// committed State and separate exact metadata CAS own their completion proof.
// It returns no restoration candidate or write permission.
func (after SnapshotIndex) MatchCommittedAfter(ctx context.Context, group StateGroup, current aggregate.Document, mode *recovery.PermissionMode, codecs aggregate.CodecCatalog) (bool, error) {
	comparison := group.Comparison()
	if comparison.WholeDocument() {
		match, err := comparison.Match(ctx, current, mode)
		return match.MatchesExpected(), err
	}
	var participants []compose.Participant
	for _, selection := range group.Selections() {
		var contracts []aggregate.ProjectionContract
		for _, contract := range selection.Contracts() {
			if !slices.Contains(group.AfterAliases(), contract.Address().Document()) {
				continue
			}
			for _, row := range after.aggregates {
				if row.Contribution().Contract().Equal(contract) {
					contracts = append(contracts, contract)
					break
				}
			}
		}
		if len(contracts) == 0 {
			continue
		}
		selected, err := aggregate.NewSelection(contracts)
		if err != nil {
			return false, err
		}
		participant, err := Participant(selected, codecs)
		if err != nil {
			return false, err
		}
		participants = append(participants, participant)
	}
	var expectedMode *recovery.PermissionMode
	if permission, present := comparison.ExpectedState().RegularFileMode(); present {
		expectedMode = &permission
	}
	selected, err := NewSelectedComparison(ctx, ComparisonInput{
		Before: comparison.BeforeDocument(), Expected: comparison.ExpectedDocument(), BeforeMode: comparison.BeforeMode(),
		ExpectedMode: expectedMode, Limits: comparison.limits,
	}, participants, comparison.sequences)
	if err != nil {
		return false, err
	}
	match, err := selected.Match(ctx, current, mode)
	return match.MatchesExpected(), err
}

func (group StateGroup) validateUnpairedSourceRetention(ctx context.Context, indexes SnapshotIndexes, allRoutes []AliasRoute) error {
	for _, route := range group.Routes() {
		if !route.OwnedSource() || slices.ContainsFunc(allRoutes, func(other AliasRoute) bool { return other.Address() == route.Address() && other.CurrentCarrier() }) {
			continue
		}
		for _, after := range indexes.after.aggregates {
			if err := ctx.Err(); err != nil {
				return err
			}
			contract := after.Contribution().Contract()
			if contract.Address().Document() != route.Address() {
				continue
			}
			if group.Covers(contract) {
				return fmt.Errorf("retained MCP source rows require an unselected frontier without a current carrier")
			}
			if before, present := indexes.before.aggregates[after.Subject()]; !present || !before.Equal(after) {
				return fmt.Errorf("retained MCP source rows cannot change without a current carrier")
			}
			for _, intent := range group.BindingIntents() {
				if intent.Destination() != route.Address().AggregateRoot() {
					continue
				}
				changed, err := intent.ChangesBinding()
				if err != nil {
					return err
				}
				if changed {
					return fmt.Errorf("retained MCP source membership cannot bootstrap or transfer its binding")
				}
			}
		}
	}
	return nil
}
