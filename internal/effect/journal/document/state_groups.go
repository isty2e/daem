package document

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

type StateGroupInput struct {
	aliases          []aggregate.DocumentAddress
	selectedSubjects []topology.SubjectID
	comparison       Comparison
	bindingIntents   []ownership.DocumentBindingIntent
}

// NewStateGroupInput retains proposed reconstruction facts. Only
// ReconstructStateGroups validates their operation-wide correspondence.
func NewStateGroupInput(aliases []aggregate.DocumentAddress, subjects []topology.SubjectID, comparison Comparison, bindings []ownership.DocumentBindingIntent) StateGroupInput {
	return StateGroupInput{aliases: slices.Clone(aliases), selectedSubjects: slices.Clone(subjects), comparison: comparison, bindingIntents: slices.Clone(bindings)}
}

// Resolved state groups are reconstruction facts, not complete journal authority.
// Physical, prepared-action, image, claim and phase joins remain mandatory.
type StateGroup struct {
	aliases        []aggregate.DocumentAddress
	beforeAliases  []aggregate.DocumentAddress
	afterAliases   []aggregate.DocumentAddress
	selections     []aggregate.Selection
	comparison     Comparison
	bindingIntents []ownership.DocumentBindingIntent
	frontier       map[documentSelectionKey]map[output.KeyPath]aggregate.ProjectionContract
}

func (group StateGroup) Comparison() Comparison               { return group.comparison }
func (group StateGroup) Aliases() []aggregate.DocumentAddress { return slices.Clone(group.aliases) }
func (group StateGroup) BeforeAliases() []aggregate.DocumentAddress {
	return slices.Clone(group.beforeAliases)
}

func (group StateGroup) AfterAliases() []aggregate.DocumentAddress {
	return slices.Clone(group.afterAliases)
}

func (group StateGroup) BindingIntents() []ownership.DocumentBindingIntent {
	return slices.Clone(group.bindingIntents)
}
func (group StateGroup) Selections() []aggregate.Selection { return slices.Clone(group.selections) }

// CanonicalRecovery retains the validated frontier while replacing caller codecs
// with the operation's reconstructed recovery policy over the same witnesses.
func (group StateGroup) CanonicalRecovery(ctx context.Context, canonical Comparison) (StateGroup, error) {
	if err := group.comparison.RequireCanonicalRecoveryPolicy(ctx, canonical); err != nil {
		return StateGroup{}, err
	}
	if !group.comparison.before.Equal(canonical.before) || !group.comparison.expected.Equal(canonical.expected) ||
		!group.comparison.beforeState.SameRootMetadata(canonical.beforeState) ||
		!group.comparison.expectedState.SameRootMetadata(canonical.expectedState) ||
		group.comparison.limits != canonical.limits || group.comparison.whole != canonical.whole {
		return StateGroup{}, fmt.Errorf("canonical recovery comparison differs from the validated witnesses")
	}
	group.comparison = canonical
	if err := group.validateComparisonSelection(ctx); err != nil {
		return StateGroup{}, err
	}
	return group, nil
}

type documentStateAddress struct {
	target      target.Target
	scope       target.Scope
	destination output.Destination
}

func ReconstructStateGroups(ctx context.Context, owner stateauthority.Authority, before, after durable.Snapshot, inputs []StateGroupInput, transfers ...CarrierTransfer) ([]StateGroup, error) {
	indexes, err := IndexSnapshots(ctx, before, after)
	if err != nil {
		return nil, err
	}
	aliasGroups := make([][]aggregate.DocumentAddress, len(inputs))
	for index, input := range inputs {
		aliasGroups[index] = input.aliases
	}
	placement, err := ResolvePhysicalPlacement(ctx, aliasGroups, transfers)
	if err != nil {
		return nil, err
	}
	if err := indexes.validateCarrierTransfers(ctx, transfers); err != nil {
		return nil, err
	}
	groups := make([]StateGroup, 0, len(inputs))
	aliases := make(map[aggregate.DocumentAddress]int)
	stateAliases := make(map[documentStateAddress]int)
	var allAliases []aggregate.DocumentAddress
	var intents []ownership.DocumentBindingIntent
	for index, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := input.comparison.Validate(); err != nil {
			return nil, fmt.Errorf("document group[%d]: %w", index, err)
		}
		beforeAliases, afterAliases := placement.BeforeAliases(index), placement.AfterAliases(index)
		beforeAddresses := make(map[aggregate.DocumentAddress]struct{}, len(beforeAliases))
		for _, alias := range beforeAliases {
			beforeAddresses[alias] = struct{}{}
		}
		afterAddresses := make(map[aggregate.DocumentAddress]struct{}, len(afterAliases))
		for _, alias := range afterAliases {
			afterAddresses[alias] = struct{}{}
		}
		selections, err := indexes.selectedContractsAt(ctx, input.selectedSubjects, beforeAddresses, afterAddresses)
		if err != nil {
			return nil, fmt.Errorf("document group[%d]: %w", index, err)
		}
		group := StateGroup{
			aliases: slices.Clone(input.aliases), selections: selections,
			beforeAliases: beforeAliases, afterAliases: afterAliases,
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
			if _, present := aliases[alias]; !present {
				allAliases = append(allAliases, alias)
			}
			// Transfers are MCP-only; native coverage still sees one group in both phases.
			afterGroup, _ := placement.AfterGroup(alias)
			aliases[alias] = afterGroup
			stateAliases[documentStateAddress{alias.Target(), alias.Scope(), alias.AggregateRoot()}] = afterGroup
			destinations[alias.AggregateRoot()] = struct{}{}
		}
		for _, intent := range input.bindingIntents {
			if _, declared := destinations[intent.Destination()]; !declared {
				return nil, fmt.Errorf("document binding intent belongs to another group's alias")
			}
		}
		intents = append(intents, input.bindingIntents...)
		groups = append(groups, group)
	}
	if err := indexes.validateCarrierFrontiers(ctx, groups, transfers); err != nil {
		return nil, err
	}
	if err := indexes.validateNativeBindingMembership(ctx, owner, allAliases, intents); err != nil {
		return nil, err
	}
	if err := indexes.validateNativeGroupCoverage(ctx, groups, aliases, stateAliases); err != nil {
		return nil, err
	}
	return groups, nil
}

func (group StateGroup) validateComparisonSelection(ctx context.Context) error {
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

func (indexes SnapshotIndexes) validateNativeGroupCoverage(ctx context.Context, groups []StateGroup, aliases map[aggregate.DocumentAddress]int, stateAliases map[documentStateAddress]int) error {
	wholeGroups := make(map[int]struct{})
	for side, index := range []SnapshotIndex{indexes.before, indexes.after} {
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
			address := documentStateAddress{targets[0], row.Scope(), row.Destination()}
			groupIndex, declared := stateAliases[address]
			previous, present := other.paths[subject]
			if side == 1 && present {
				before := documentStateAddress{previous.ConsumerTargets()[0], previous.Scope(), previous.Destination()}
				if err := validateDocumentRetargetGroups(before, address, stateAliases); err != nil {
					return err
				}
			}
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
			if side == 1 && present {
				before, after := previous.Contribution().Contract().Address().Document(), contract.Address().Document()
				if err := validateDocumentRetargetGroups(
					documentStateAddress{before.Target(), before.Scope(), before.AggregateRoot()},
					documentStateAddress{after.Target(), after.Scope(), after.AggregateRoot()}, stateAliases,
				); err != nil {
					return err
				}
			}
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
			if side == 1 && changed && !groups[groupIndex].Covers(contract) {
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

func validateDocumentRetargetGroups(before, after documentStateAddress, groups map[documentStateAddress]int) error {
	if before == after {
		return nil
	}
	beforeGroup, beforeDeclared := groups[before]
	afterGroup, afterDeclared := groups[after]
	if !beforeDeclared || !afterDeclared {
		return fmt.Errorf("native document retarget requires both retirement and creation groups")
	}
	if beforeGroup == afterGroup {
		return fmt.Errorf("native document retarget requires distinct retirement and creation groups")
	}
	return nil
}

func (group StateGroup) Covers(contract aggregate.ProjectionContract) bool {
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
