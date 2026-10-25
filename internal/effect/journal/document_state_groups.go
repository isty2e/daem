package journal

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type documentStateGroupInput struct {
	aliases          []aggregate.DocumentAddress
	selectedSubjects []topology.SubjectID
	comparison       DocumentComparison
	bindingIntents   []ownership.DocumentBindingIntent
}

// Resolved state groups are reconstruction facts, not complete journal authority.
// Physical, prepared-action, image, claim and phase joins remain mandatory.
type documentStateGroup struct {
	aliases        []aggregate.DocumentAddress
	selections     []aggregate.Selection
	comparison     DocumentComparison
	bindingIntents []ownership.DocumentBindingIntent
	frontier       map[documentSelectionKey]map[output.KeyPath]aggregate.ProjectionContract
}

type documentWholeAddress struct {
	target      target.Target
	scope       target.Scope
	destination output.Destination
}

func reconstructDocumentStateGroups(ctx context.Context, owner stateauthority.Authority, before, after durable.Snapshot, inputs []documentStateGroupInput) ([]documentStateGroup, error) {
	indexes, err := indexDocumentSnapshots(ctx, before, after)
	if err != nil {
		return nil, err
	}
	groups := make([]documentStateGroup, 0, len(inputs))
	aliases := make(map[aggregate.DocumentAddress]int)
	wholeAliases := make(map[documentWholeAddress]int)
	var allAliases []aggregate.DocumentAddress
	var intents []ownership.DocumentBindingIntent
	for index, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := input.comparison.Validate(); err != nil {
			return nil, fmt.Errorf("document group[%d]: %w", index, err)
		}
		selections, err := indexes.selectedContracts(ctx, input.selectedSubjects, input.aliases)
		if err != nil {
			return nil, fmt.Errorf("document group[%d]: %w", index, err)
		}
		group := documentStateGroup{
			aliases: slices.Clone(input.aliases), selections: selections,
			comparison: input.comparison, bindingIntents: slices.Clone(input.bindingIntents),
			frontier: make(map[documentSelectionKey]map[output.KeyPath]aggregate.ProjectionContract),
		}
		for _, selection := range selections {
			key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
			contracts := make(map[output.KeyPath]aggregate.ProjectionContract)
			for _, contract := range selection.Contracts() {
				contracts[contract.Address().KeyPath()] = contract
			}
			group.frontier[key] = contracts
		}
		if err := group.validateComparisonSelection(ctx); err != nil {
			return nil, fmt.Errorf("document group[%d]: %w", index, err)
		}
		destinations := make(map[output.Destination]struct{}, len(input.aliases))
		for _, alias := range input.aliases {
			if _, duplicate := aliases[alias]; duplicate {
				return nil, fmt.Errorf("document alias belongs to more than one group")
			}
			aliases[alias] = index
			wholeAliases[documentWholeAddress{alias.Target(), alias.Scope(), alias.AggregateRoot()}] = index
			destinations[alias.AggregateRoot()] = struct{}{}
		}
		for _, intent := range input.bindingIntents {
			if _, declared := destinations[intent.Destination()]; !declared {
				return nil, fmt.Errorf("document binding intent belongs to another group's alias")
			}
		}
		allAliases = append(allAliases, input.aliases...)
		intents = append(intents, input.bindingIntents...)
		groups = append(groups, group)
	}
	if err := indexes.validateNativeBindingMembership(ctx, owner, allAliases, intents); err != nil {
		return nil, err
	}
	if err := indexes.validateNativeGroupCoverage(ctx, groups, aliases, wholeAliases); err != nil {
		return nil, err
	}
	return groups, nil
}

func (group documentStateGroup) validateComparisonSelection(ctx context.Context) error {
	remaining := 0
	for _, contracts := range group.frontier {
		remaining += len(contracts)
	}
	if group.comparison.whole && remaining != 0 {
		return fmt.Errorf("whole document comparison cannot contain selected subjects")
	}
	seen := make(map[documentSelectionKey]map[output.KeyPath]struct{})
	for _, participant := range group.comparison.participants {
		selection := participant.Selection()
		key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
		for _, contract := range selection.Contracts() {
			if err := ctx.Err(); err != nil {
				return err
			}
			path := contract.Address().KeyPath()
			resolved, found := group.frontier[key][path]
			if !found || !resolved.Equal(contract) {
				return fmt.Errorf("document comparison selects a contract outside its resolved frontier")
			}
			if seen[key] == nil {
				seen[key] = make(map[output.KeyPath]struct{})
			}
			if _, duplicate := seen[key][path]; duplicate {
				return fmt.Errorf("document comparison repeats a selected contract")
			}
			seen[key][path] = struct{}{}
			remaining--
		}
	}
	if remaining != 0 {
		return fmt.Errorf("document comparison omits a resolved frontier contract")
	}
	return ctx.Err()
}

func (indexes documentSnapshotIndexes) validateNativeGroupCoverage(ctx context.Context, groups []documentStateGroup, aliases map[aggregate.DocumentAddress]int, wholeAliases map[documentWholeAddress]int) error {
	wholeGroups := make(map[int]struct{})
	for side, index := range []documentSnapshotIndex{indexes.before, indexes.after} {
		other := indexes.after
		if side == 1 {
			other = indexes.before
		}
		for subject, row := range index.paths {
			if err := ctx.Err(); err != nil {
				return err
			}
			id, entityBacked := topologyprojection.EntityID(subject)
			if !entityBacked || id.Kind() != entity.KindHostConfig {
				continue
			}
			targets := row.ConsumerTargets()
			if err := profile.ValidateHostConfigDocumentOccupancy(subject, targets, row.Scope(), row.Destination(), row.ContentKind(), row.PermissionPolicy(), row.FileMode()); err != nil {
				return err
			}
			groupIndex, declared := wholeAliases[documentWholeAddress{targets[0], row.Scope(), row.Destination()}]
			previous, present := other.paths[subject]
			if !declared {
				if !present || !row.Equal(previous) {
					return fmt.Errorf("changed native whole-file state has no document group")
				}
				continue
			}
			wholeGroups[groupIndex] = struct{}{}
			if side == 1 && !groups[groupIndex].comparison.whole {
				return fmt.Errorf("native whole-file after state requires whole comparison")
			}
		}
		for subject, row := range index.aggregates {
			if err := ctx.Err(); err != nil {
				return err
			}
			contract := row.Contribution().Contract()
			if _, native := aggregate.HostConfigSyntaxForCodec(contract.CodecContractID()); !native {
				continue
			}
			groupIndex, declared := aliases[contract.Address().Document()]
			previous, present := other.aggregates[subject]
			changed := !present || !row.Equal(previous)
			if !declared {
				if changed {
					return fmt.Errorf("changed native key state has no document group")
				}
				continue
			}
			if side == 1 && groups[groupIndex].comparison.whole {
				return fmt.Errorf("native keyed after state cannot use whole comparison")
			}
			if side == 1 && changed && !groups[groupIndex].covers(contract) {
				return fmt.Errorf("changed native key after state is outside its document frontier")
			}
			// An unselected before-only row may be released, not deleted. The
			// prepared-action/image join must still forbid edits outside the frontier.
		}
	}
	for index, group := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, present := wholeGroups[index]; group.comparison.whole && !present {
			return fmt.Errorf("whole document comparison has no native whole-file state")
		}
	}
	return ctx.Err()
}

func (group documentStateGroup) covers(contract aggregate.ProjectionContract) bool {
	key := documentSelectionKey{document: contract.Address().Document(), codec: contract.CodecContractID()}
	contracts := group.frontier[key]
	for path := contract.Address().KeyPath(); ; {
		if _, present := contracts[path]; present {
			return true
		}
		parent, present := path.Parent()
		if !present {
			return false
		}
		path = parent
	}
}
