package clipresent

import (
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"

	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func PrintDeltaSummaryWithOptions(output io.Writer, delta lock.Delta, options HumanOptions) {
	projection := newLockIdentityProjection(lock.File{}, delta)
	counts := delta.Counts()
	fmt.Fprintf(
		output,
		"lock entry changes: added=%d changed=%d removed=%d unchanged=%d\n",
		counts.Added,
		counts.Changed,
		counts.Removed,
		counts.Unchanged,
	)
	printDeltaEntries(output, "added", delta.EntriesWithStatus(lock.DeltaStatusAdded), projection, options)
	printDeltaEntries(output, "changed", delta.EntriesWithStatus(lock.DeltaStatusChanged), projection, options)
	printDeltaEntries(output, "removed", delta.EntriesWithStatus(lock.DeltaStatusRemoved), projection, options)
	if options.Verbose {
		printDeltaEntries(output, "unchanged", delta.EntriesWithStatus(lock.DeltaStatusUnchanged), projection, options)
	}
	printOrderDeltaSummary(output, delta, projection, options)
}

func printOrderDeltaSummary(
	output io.Writer,
	delta lock.Delta,
	projection lockIdentityProjection,
	options HumanOptions,
) {
	counts := delta.OrderCounts()
	if counts.Added+counts.Changed+counts.Removed+counts.Unchanged == 0 {
		return
	}
	fmt.Fprintf(
		output,
		"lock order constraints: added=%d changed=%d removed=%d unchanged=%d\n",
		counts.Added,
		counts.Changed,
		counts.Removed,
		counts.Unchanged,
	)
	printOrderDeltaEntries(
		output,
		"added",
		delta.OrderEntriesWithStatus(lock.DeltaStatusAdded),
		projection,
		options,
	)
	printOrderDeltaEntries(
		output,
		"changed",
		delta.OrderEntriesWithStatus(lock.DeltaStatusChanged),
		projection,
		options,
	)
	printOrderDeltaEntries(
		output,
		"removed",
		delta.OrderEntriesWithStatus(lock.DeltaStatusRemoved),
		projection,
		options,
	)
	if options.Verbose {
		printOrderDeltaEntries(
			output,
			"unchanged",
			delta.OrderEntriesWithStatus(lock.DeltaStatusUnchanged),
			projection,
			options,
		)
	}
}

func printOrderDeltaEntries(
	output io.Writer,
	label string,
	entries []lock.OrderDeltaEntry,
	projection lockIdentityProjection,
	options HumanOptions,
) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(output, "lockfile.order_constraint.%s:\n", label)
	for _, entry := range entries {
		fmt.Fprintf(output, "  - %s", Escape(string(entry.Key)))
		if !options.Verbose {
			fmt.Fprintln(output)
			continue
		}
		switch entry.Status {
		case lock.DeltaStatusAdded:
			fmt.Fprintf(output, " members=%s\n", orderMemberSummary(entry.After, projection, lockIdentityAfter, options.Verbose))
		case lock.DeltaStatusRemoved:
			fmt.Fprintf(output, " members=%s\n", orderMemberSummary(entry.Before, projection, lockIdentityBefore, options.Verbose))
		case lock.DeltaStatusChanged:
			fmt.Fprintf(
				output,
				" before=%s after=%s\n",
				orderMemberSummary(entry.Before, projection, lockIdentityBefore, options.Verbose),
				orderMemberSummary(entry.After, projection, lockIdentityAfter, options.Verbose),
			)
		default:
			fmt.Fprintln(output)
		}
	}
}

func orderMemberSummary(
	constraint hostrelation.RelationOrderConstraint,
	projection lockIdentityProjection,
	side lockIdentitySide,
	verbose bool,
) string {
	members := constraint.Members()
	summary := make([]string, 0, len(members))
	for _, member := range members {
		identity := lockHostLoadIdentityDisclosureFor(
			constraint.ClassID(),
			string(member.HostLoadIdentity()),
		)
		if verbose {
			identity = verboseIdentityDisclosureFor(string(member.HostLoadIdentity()))
		}
		subject := projection.subject(member.Subject(), side, verbose)
		summary = append(
			summary,
			fmt.Sprintf(
				"%s=%s",
				Escape(lockSubjectString(subject)),
				Escape(identity.Value()),
			),
		)
	}
	return strings.Join(summary, ",")
}

func printDeltaEntries(
	output io.Writer,
	label string,
	entries []lock.DeltaEntry,
	projection lockIdentityProjection,
	options HumanOptions,
) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(output, "%s lock entries:\n", label)
	ordered := sortedHumanRows(entries, func(entry lock.DeltaEntry) humanResourceOrder {
		contract, _ := lockDeltaContract(entry)
		var consumers []target.Target
		var scope target.Scope
		if spec, present := contract.Realization(); present {
			consumers, scope = spec.ConsumerTargets(), spec.Scope()
		} else if use, present := contract.ExactFileUse(); present {
			scope = use.Scope()
		}
		return humanResourceOrderFor(lockHumanReference(entry, projection, options.Verbose).resource, consumers, scope)
	})
	for _, entry := range ordered {
		contract, _ := lockDeltaContract(entry)
		fmt.Fprintf(output, "  - %s", lockHumanReference(entry, projection, options.Verbose).fields(options.Verbose))
		if spec, present := contract.Realization(); present {
			consumers := targetStrings(spec.ConsumerTargets())
			field := "target"
			if len(consumers) != 1 {
				field = "targets"
			}
			fmt.Fprintf(output, " %s=%s scope=%s placement=%s", field, Escape(strings.Join(consumers, ",")), Escape(string(spec.Scope())), Quote(spec.PlacementID()))
		} else if use, present := contract.ExactFileUse(); present {
			fmt.Fprintf(output, " scope=%s", Escape(string(use.Scope())))
		}
		switch {
		case entry.Status == lock.DeltaStatusChanged:
			fmt.Fprintf(output, " changed=%s\n", strings.Join(changedFacetNames(entry.Before, entry.After), ","))
		case options.Verbose && entry.Status == lock.DeltaStatusAdded:
			printSubjectFields(output, entry.After, projection, lockIdentityAfter)
		case options.Verbose && entry.Status == lock.DeltaStatusRemoved:
			printSubjectFields(output, entry.Before, projection, lockIdentityBefore)
		default:
			fmt.Fprintln(output)
		}
	}
}

func printSubjectFields(
	output io.Writer,
	contract lock.LockedSubjectContract,
	projection lockIdentityProjection,
	side lockIdentitySide,
) {
	fmt.Fprintf(
		output,
		" entity=%q ownership=%q on_absent=%q",
		contract.EntityID(),
		contract.Ownership(),
		contract.OnAbsent(),
	)
	if identity, ok := contract.ExactSupply(); ok {
		fmt.Fprintf(
			output,
			" supply_kind=%q source_id=%q resolved_ref=%q content_hash=%q",
			identity.Kind(),
			identity.SourceID(),
			identity.ResolvedRef(),
			identity.ContentHash(),
		)
	}
	if use, ok := contract.ExactFileUse(); ok {
		fmt.Fprintf(output, " file_scope=%q executable=%t", use.Scope(), use.Executable())
	}
	if spec, ok := contract.Realization(); ok {
		printRealizationFields(output, spec, contract.SubjectID(), projection, side)
	}
	if recipe, ok := contract.RepairRecipe(); ok {
		fmt.Fprintf(output, " repair_recipe_hash=%q", recipe.Hash())
	}
	if plan, ok := contract.DelegatePlan(); ok {
		fmt.Fprintf(output, " delegate_plan=%q runner=%q", plan.IdentityKey(), plan.Runner().Kind())
	}
	if correlation, ok := contract.SkillSetMemberCorrelation(); ok {
		fmt.Fprintf(output, " skill_set=%q", correlation.DeclarationIdentity())
	}
	fmt.Fprintln(output)
}

func printRealizationFields(
	output io.Writer,
	spec realization.RealizationSpec,
	subject topology.SubjectID,
	projection lockIdentityProjection,
	side lockIdentitySide,
) {
	fmt.Fprintf(output, " realization=%q", spec.Kind())
	switch spec.Kind() {
	case realization.RealizationManagedPathProjection:
		projection, _ := spec.ManagedPathProjection()
		fmt.Fprintf(
			output,
			" consumers=%q scope=%q destination=%q content_kind=%q placement_mode=%q permission_policy=%q adapter_contract=%q",
			projection.ConsumerTargets(),
			projection.Scope(),
			projection.Destination(),
			projection.ContentKind(),
			projection.PlacementMode(),
			projection.PermissionPolicy(),
			projection.AdapterContractVersion(),
		)
		if exactMode, present := projection.ExactPermissionMode(); present {
			fmt.Fprintf(output, " exact_permission_mode=%04o", exactMode.FileMode())
		}
	case realization.RealizationManagedAggregateContribution:
		contribution, _ := spec.ManagedAggregateContribution()
		fmt.Fprintf(
			output,
			" target=%q scope=%q aggregate_root=%q content_path=%q adapter_contract=%q",
			contribution.Target(),
			contribution.Scope(),
			contribution.AggregateRoot(),
			contribution.ContentPath(),
			string(contribution.CodecContractID()),
		)
	case realization.RealizationDelegatedRelation:
		relation, _ := spec.DelegatedRelation()
		relationSubjectKey := verboseIdentityDisclosureFor(string(relation.ExpectedRelation().SubjectKey()))
		if disclosure, ok := projection.carrierFor(subject, side); ok {
			relationSubjectKey = disclosure.verboseRelationSubjectKey
		}
		fmt.Fprintf(
			output,
			" target=%q scope=%q relation_subject_key=%q route_id=%q route_contract=%q request_hash=%q",
			relation.Target(),
			relation.Scope(),
			relationSubjectKey.Value(),
			relation.RouteID(),
			relation.RouteContractVersion(),
			relation.CanonicalRequestHash(),
		)
	}
}

func lockSubjectString(subject jsonSubjectID) string {
	return subject.Kind + "/" + url.PathEscape(subject.Namespace) + "/" + url.PathEscape(subject.Name)
}

func changedFacetNames(before lock.LockedSubjectContract, after lock.LockedSubjectContract) []string {
	changed := make([]string, 0, 11)
	if before.EntityID() != after.EntityID() {
		changed = append(changed, "entity_id")
	}
	changed = append(changed, exactSupplyChangeNames(optionalExactSupply(before), optionalExactSupply(after))...)
	if !reflect.DeepEqual(optionalExactFileUse(before), optionalExactFileUse(after)) {
		changed = append(changed, "exact_file_use")
	}
	if !reflect.DeepEqual(optionalRealization(before), optionalRealization(after)) {
		changed = append(changed, "realization")
	}
	if !reflect.DeepEqual(optionalDerivation(before), optionalDerivation(after)) {
		changed = append(changed, "derivation")
	}
	if !reflect.DeepEqual(optionalRepairRecipe(before), optionalRepairRecipe(after)) {
		changed = append(changed, "repair_recipe")
	}
	if !reflect.DeepEqual(optionalDelegatePlan(before), optionalDelegatePlan(after)) {
		changed = append(changed, "delegate_plan")
	}
	if !reflect.DeepEqual(optionalSkillSetCorrelation(before), optionalSkillSetCorrelation(after)) {
		changed = append(changed, "skill_set_member")
	}
	if before.Ownership() != after.Ownership() {
		changed = append(changed, "ownership")
	}
	if before.OnAbsent() != after.OnAbsent() {
		changed = append(changed, "on_absent")
	}
	if !reflect.DeepEqual(before.ReplayCoverage(), after.ReplayCoverage()) {
		changed = append(changed, "replay")
	}
	if !operationContractsEqual(before, after) {
		changed = append(changed, "operations")
	}
	return changed
}

func exactSupplyChangeNames(before optional[artifact.ExactIdentity], after optional[artifact.ExactIdentity]) []string {
	if before.present != after.present {
		return []string{"exact_supply"}
	}
	if !before.present {
		return nil
	}
	return exactIdentityChangeNames(before.value.Changes(after.value))
}

func exactIdentityChangeNames(changes artifact.ExactIdentityChanges) []string {
	var names []string
	if changes.SourceID {
		names = append(names, "source_id")
	}
	if changes.ResolvedRef {
		names = append(names, "resolved_ref")
	}
	if changes.Kind {
		names = append(names, "kind")
	}
	if changes.ContentHash {
		names = append(names, "content_hash")
	}
	if changes.Unclassified {
		names = append(names, "exact_supply")
	}
	return names
}

type optional[T any] struct {
	value   T
	present bool
}

func optionalExactSupply(contract lock.LockedSubjectContract) optional[artifact.ExactIdentity] {
	value, ok := contract.ExactSupply()
	return optional[artifact.ExactIdentity]{value: value, present: ok}
}

func optionalExactFileUse(contract lock.LockedSubjectContract) optional[lock.ExactFileUse] {
	value, ok := contract.ExactFileUse()
	return optional[lock.ExactFileUse]{value: value, present: ok}
}

func optionalRealization(contract lock.LockedSubjectContract) optional[realization.RealizationSpec] {
	value, ok := contract.Realization()
	return optional[realization.RealizationSpec]{value: value, present: ok}
}

func optionalDerivation(contract lock.LockedSubjectContract) optional[lock.DerivationContract] {
	value, ok := contract.Derivation()
	return optional[lock.DerivationContract]{value: value, present: ok}
}

func optionalRepairRecipe(contract lock.LockedSubjectContract) optional[skillrepair.Recipe] {
	value, ok := contract.RepairRecipe()
	return optional[skillrepair.Recipe]{value: value, present: ok}
}

func optionalDelegatePlan(contract lock.LockedSubjectContract) optional[string] {
	value, ok := contract.DelegatePlan()
	if !ok {
		return optional[string]{}
	}
	return optional[string]{value: value.IdentityKey(), present: true}
}

func optionalSkillSetCorrelation(contract lock.LockedSubjectContract) optional[lock.SkillSetMemberCorrelation] {
	value, ok := contract.SkillSetMemberCorrelation()
	return optional[lock.SkillSetMemberCorrelation]{value: value, present: ok}
}

func operationContractsEqual(before lock.LockedSubjectContract, after lock.LockedSubjectContract) bool {
	beforeKinds := before.OperationKinds()
	afterKinds := after.OperationKinds()
	if !reflect.DeepEqual(beforeKinds, afterKinds) {
		return false
	}
	for _, kind := range beforeKinds {
		beforeContract, _ := before.OperationContract(kind)
		afterContract, _ := after.OperationContract(kind)
		if !reflect.DeepEqual(beforeContract, afterContract) {
			return false
		}
	}
	return true
}
