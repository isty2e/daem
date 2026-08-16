package clipresent

import (
	"encoding/json"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/topology"
)

type jsonOutput struct {
	SchemaVersion          int                         `json:"schema_version"`
	Command                string                      `json:"command"`
	Mode                   string                      `json:"mode"`
	ManifestPath           string                      `json:"manifest_path"`
	LockfilePath           string                      `json:"lockfile_path"`
	PreviousFound          bool                        `json:"previous_found"`
	EntryCounts            jsonEntryCounts             `json:"entry_counts"`
	ChangeCounts           jsonChangeCounts            `json:"change_counts"`
	OrderChangeCounts      jsonChangeCounts            `json:"order_change_counts"`
	HasChanges             bool                        `json:"has_changes"`
	SubjectChanges         []jsonSubjectChange         `json:"subject_changes"`
	OrderConstraintChanges []jsonOrderConstraintChange `json:"order_constraint_changes"`
}

type jsonEntryCounts struct {
	Subjects         int `json:"subjects"`
	OrderConstraints int `json:"order_constraints"`
}

type jsonChangeCounts struct {
	Added     int `json:"added"`
	Changed   int `json:"changed"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
}

type jsonSubjectChange struct {
	Status  lock.DeltaStatus   `json:"status"`
	Subject jsonSubjectID      `json:"subject"`
	Before  *jsonLockedSubject `json:"before,omitempty"`
	After   *jsonLockedSubject `json:"after,omitempty"`
}

type jsonOrderConstraintChange struct {
	Status  lock.DeltaStatus     `json:"status"`
	ClassID string               `json:"class_id"`
	Before  *jsonOrderConstraint `json:"before,omitempty"`
	After   *jsonOrderConstraint `json:"after,omitempty"`
}

type jsonOrderConstraint struct {
	ClassID         string            `json:"class_id"`
	ContractVersion string            `json:"contract_version"`
	RuntimeMeaning  string            `json:"runtime_meaning"`
	Members         []jsonOrderMember `json:"members"`
}

type jsonOrderMember struct {
	Subject                  jsonSubjectID `json:"subject"`
	HostLoadIdentity         string        `json:"host_load_identity"`
	HostLoadIdentityRedacted bool          `json:"host_load_identity_redacted,omitempty"`
}

type jsonSubjectID struct {
	Kind         string `json:"kind"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	NameRedacted bool   `json:"name_redacted,omitempty"`
}

type jsonLockedSubject struct {
	EntityID       string                   `json:"entity_id"`
	Subject        jsonSubjectID            `json:"subject"`
	Ownership      string                   `json:"ownership"`
	OnAbsent       string                   `json:"on_absent"`
	ExactSupply    *jsonExactSupply         `json:"exact_supply,omitempty"`
	ExactFileUse   *jsonExactFileUse        `json:"exact_file_use,omitempty"`
	Realization    *jsonRealization         `json:"realization,omitempty"`
	RepairHash     string                   `json:"repair_recipe_hash,omitempty"`
	DelegatePlan   *jsonDelegatePlan        `json:"delegate_plan,omitempty"`
	SkillSetMember *jsonSkillSetCorrelation `json:"skill_set_member,omitempty"`
	Operations     []string                 `json:"operations"`
}

type jsonExactSupply struct {
	SourceID    string `json:"source_id"`
	ResolvedRef string `json:"resolved_ref,omitempty"`
	Kind        string `json:"kind"`
	ContentHash string `json:"content_hash"`
}

type jsonExactFileUse struct {
	Scope      string `json:"scope"`
	Executable bool   `json:"executable"`
}

type jsonRealization struct {
	Kind                       string   `json:"kind"`
	PlacementID                string   `json:"placement_id"`
	Target                     string   `json:"target,omitempty"`
	ConsumerTargets            []string `json:"consumer_targets,omitempty"`
	Scope                      string   `json:"scope"`
	Destination                string   `json:"destination,omitempty"`
	ContentKind                string   `json:"content_kind,omitempty"`
	PlacementMode              string   `json:"placement_mode,omitempty"`
	PermissionPolicy           string   `json:"permission_policy,omitempty"`
	ExactPermissionMode        *uint32  `json:"exact_permission_mode,omitempty"`
	AggregateRoot              string   `json:"aggregate_root,omitempty"`
	ContentPath                string   `json:"content_path,omitempty"`
	MergeUnit                  string   `json:"merge_unit,omitempty"`
	SiblingRetention           string   `json:"sibling_retention,omitempty"`
	Equivalence                string   `json:"equivalence,omitempty"`
	SourceNamespace            string   `json:"source_namespace,omitempty"`
	SourceNamespaceRedacted    bool     `json:"source_namespace_redacted,omitempty"`
	RelationSubjectKey         string   `json:"relation_subject_key,omitempty"`
	RelationSubjectKeyRedacted bool     `json:"relation_subject_key_redacted,omitempty"`
	ManagedInstanceKey         string   `json:"managed_instance_key,omitempty"`
	ManagedInstanceKeyRedacted bool     `json:"managed_instance_key_redacted,omitempty"`
	RouteID                    string   `json:"route_id,omitempty"`
	CanonicalRequestHash       string   `json:"canonical_request_hash,omitempty"`
	RouteContractVersion       string   `json:"route_contract_version,omitempty"`
	AdapterContractVersion     string   `json:"adapter_contract_version,omitempty"`
	ComparedFields             []string `json:"compared_fields,omitempty"`
	VerifiedRelationFields     []string `json:"verified_relation_fields,omitempty"`
}

type jsonDelegatePlan struct {
	IdentityKey string `json:"identity_key"`
	RunnerKind  string `json:"runner_kind"`
}

type jsonSkillSetCorrelation struct {
	DeclarationIdentity string `json:"declaration_identity"`
}

// JSONInput is the stable command-output contract for lock and outdated JSON.
type JSONInput struct {
	Command       string
	Mode          string
	ManifestPath  string
	LockfilePath  string
	PreviousFound bool
	Lockfile      lock.File
	Delta         lock.Delta
}

// PrintJSON writes the stable structured lock output payload.
func PrintJSON(output io.Writer, input JSONInput) error {
	projection := newLockIdentityProjection(input.Lockfile, input.Delta)
	payload := jsonOutput{
		SchemaVersion:          contractversion.LockComparisonJSON,
		Command:                input.Command,
		Mode:                   input.Mode,
		ManifestPath:           input.ManifestPath,
		LockfilePath:           input.LockfilePath,
		PreviousFound:          input.PreviousFound,
		EntryCounts:            jsonEntryCountsForFile(input.Lockfile),
		ChangeCounts:           jsonChangeCountsForDelta(input.Delta),
		OrderChangeCounts:      jsonChangeCountsForOrderDelta(input.Delta),
		HasChanges:             input.Delta.HasChanges(),
		SubjectChanges:         jsonSubjectChanges(input.Delta, projection),
		OrderConstraintChanges: jsonOrderConstraintChanges(input.Delta, projection),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func jsonEntryCountsForFile(file lock.File) jsonEntryCounts {
	return jsonEntryCounts{
		Subjects:         file.Locked.Len(),
		OrderConstraints: file.Locked.OrderLen(),
	}
}

func jsonChangeCountsForDelta(delta lock.Delta) jsonChangeCounts {
	counts := delta.Counts()
	return jsonChangeCounts{
		Added:     counts.Added,
		Changed:   counts.Changed,
		Removed:   counts.Removed,
		Unchanged: counts.Unchanged,
	}
}

func jsonChangeCountsForOrderDelta(delta lock.Delta) jsonChangeCounts {
	counts := delta.OrderCounts()
	return jsonChangeCounts{
		Added:     counts.Added,
		Changed:   counts.Changed,
		Removed:   counts.Removed,
		Unchanged: counts.Unchanged,
	}
}

func jsonSubjectChanges(delta lock.Delta, projection lockIdentityProjection) []jsonSubjectChange {
	entries := delta.Entries()
	changes := make([]jsonSubjectChange, 0, len(entries))
	for _, entry := range entries {
		side := lockIdentityAfter
		if entry.Status == lock.DeltaStatusRemoved {
			side = lockIdentityBefore
		}
		change := jsonSubjectChange{
			Status:  entry.Status,
			Subject: projection.subject(entry.Key, side, false),
		}
		if entry.Status != lock.DeltaStatusAdded {
			before := jsonLockedSubjectFor(entry.Before, projection, lockIdentityBefore)
			change.Before = &before
		}
		if entry.Status != lock.DeltaStatusRemoved {
			after := jsonLockedSubjectFor(entry.After, projection, lockIdentityAfter)
			change.After = &after
		}
		changes = append(changes, change)
	}
	return changes
}

func jsonOrderConstraintChanges(delta lock.Delta, projection lockIdentityProjection) []jsonOrderConstraintChange {
	entries := delta.OrderEntries()
	changes := make([]jsonOrderConstraintChange, 0, len(entries))
	for _, entry := range entries {
		change := jsonOrderConstraintChange{
			Status:  entry.Status,
			ClassID: string(entry.Key),
		}
		if entry.Status != lock.DeltaStatusAdded {
			before := jsonOrderConstraintFor(entry.Before, projection, lockIdentityBefore)
			change.Before = &before
		}
		if entry.Status != lock.DeltaStatusRemoved {
			after := jsonOrderConstraintFor(entry.After, projection, lockIdentityAfter)
			change.After = &after
		}
		changes = append(changes, change)
	}
	return changes
}

func jsonOrderConstraintFor(
	constraint hostrelation.RelationOrderConstraint,
	projection lockIdentityProjection,
	side lockIdentitySide,
) jsonOrderConstraint {
	members := constraint.Members()
	projectedMembers := make([]jsonOrderMember, 0, len(members))
	for _, member := range members {
		loadIdentity := lockHostLoadIdentityDisclosureFor(
			constraint.ClassID(),
			string(member.HostLoadIdentity()),
		)
		projectedMembers = append(projectedMembers, jsonOrderMember{
			Subject:                  projection.subject(member.Subject(), side, false),
			HostLoadIdentity:         loadIdentity.Value(),
			HostLoadIdentityRedacted: loadIdentity.Redacted(),
		})
	}
	return jsonOrderConstraint{
		ClassID:         string(constraint.ClassID()),
		ContractVersion: constraint.MemberIdentityContract(),
		RuntimeMeaning:  string(constraint.RuntimeMeaning()),
		Members:         projectedMembers,
	}
}

func jsonSubjectIDFor(id topology.SubjectID) jsonSubjectID {
	return lockIdentityProjection{}.subject(id, lockIdentityAfter, false)
}

func jsonLockedSubjectFor(
	contract lock.LockedSubjectContract,
	projection lockIdentityProjection,
	side lockIdentitySide,
) jsonLockedSubject {
	result := jsonLockedSubject{
		EntityID:   contract.EntityID().String(),
		Subject:    projection.subject(contract.SubjectID(), side, false),
		Ownership:  string(contract.Ownership()),
		OnAbsent:   string(contract.OnAbsent()),
		Operations: operationKindStrings(contract.OperationKinds()),
	}
	if identity, ok := contract.ExactSupply(); ok {
		result.ExactSupply = &jsonExactSupply{
			SourceID:    string(identity.SourceID()),
			ResolvedRef: string(identity.ResolvedRef()),
			Kind:        string(identity.Kind()),
			ContentHash: string(identity.ContentHash()),
		}
	}
	if use, ok := contract.ExactFileUse(); ok {
		result.ExactFileUse = &jsonExactFileUse{Scope: string(use.Scope()), Executable: use.Executable()}
	}
	if spec, ok := contract.Realization(); ok {
		value := jsonRealizationForWithDisclosure(spec, projection, contract.SubjectID(), side)
		result.Realization = &value
	}
	if recipe, ok := contract.RepairRecipe(); ok {
		result.RepairHash = recipe.Hash()
	}
	if plan, ok := contract.DelegatePlan(); ok {
		result.DelegatePlan = &jsonDelegatePlan{IdentityKey: plan.IdentityKey(), RunnerKind: string(plan.Runner().Kind())}
	}
	if correlation, ok := contract.SkillSetMemberCorrelation(); ok {
		result.SkillSetMember = &jsonSkillSetCorrelation{
			DeclarationIdentity: correlation.DeclarationIdentity().String(),
		}
	}
	return result
}

func jsonRealizationFor(spec realization.RealizationSpec) jsonRealization {
	return jsonRealizationForWithDisclosure(
		spec,
		lockIdentityProjection{},
		topology.SubjectID{},
		lockIdentityAfter,
	)
}

func jsonRealizationForWithDisclosure(
	spec realization.RealizationSpec,
	projection lockIdentityProjection,
	subject topology.SubjectID,
	side lockIdentitySide,
) jsonRealization {
	result := jsonRealization{Kind: string(spec.Kind())}
	switch spec.Kind() {
	case realization.RealizationManagedPathProjection:
		projection, _ := spec.ManagedPathProjection()
		result.PlacementID = projection.PlacementID()
		result.ConsumerTargets = targetStrings(projection.ConsumerTargets())
		result.Scope = string(projection.Scope())
		result.Destination = projection.Destination().String()
		result.ContentKind = string(projection.ContentKind())
		result.PlacementMode = string(projection.PlacementMode())
		result.PermissionPolicy = string(projection.PermissionPolicy())
		if exactMode, present := projection.ExactPermissionMode(); present {
			mode := uint32(exactMode.FileMode())
			result.ExactPermissionMode = &mode
		}
		result.AdapterContractVersion = projection.AdapterContractVersion()
	case realization.RealizationManagedAggregateContribution:
		contribution, _ := spec.ManagedAggregateContribution()
		result.PlacementID = contribution.PlacementID()
		result.Target = string(contribution.Target())
		result.Scope = string(contribution.Scope())
		result.AggregateRoot = contribution.AggregateRoot().String()
		result.ContentPath = contribution.ContentPath()
		result.MergeUnit = string(contribution.MergeUnit())
		result.SiblingRetention = string(contribution.SiblingRetention())
		result.Equivalence = string(contribution.Equivalence())
		result.ComparedFields = contribution.ComparedFields()
		result.AdapterContractVersion = string(contribution.CodecContractID())
	case realization.RealizationDelegatedRelation:
		relation, _ := spec.DelegatedRelation()
		expected := relation.ExpectedRelation()
		result.PlacementID = relation.PlacementID()
		result.Target = string(relation.Target())
		result.Scope = string(relation.Scope())
		if disclosure, ok := projection.carrierFor(subject, side); ok {
			result.SourceNamespace = disclosure.sourceNamespace.Value()
			result.SourceNamespaceRedacted = disclosure.sourceNamespace.Redacted()
			result.RelationSubjectKey = disclosure.relationSubjectKey.Value()
			result.RelationSubjectKeyRedacted = disclosure.relationSubjectKey.Redacted()
			result.ManagedInstanceKey = disclosure.managedInstanceKey.Value()
			result.ManagedInstanceKeyRedacted = disclosure.managedInstanceKey.Redacted()
		} else {
			sourceNamespace := redactedIdentityDisclosure(relation.SourceNamespace())
			relationSubjectKey := redactedIdentityDisclosure(string(expected.SubjectKey()))
			managedInstanceKey := redactedIdentityDisclosure(string(expected.ManagedInstanceKey()))
			result.SourceNamespace = sourceNamespace.Value()
			result.SourceNamespaceRedacted = sourceNamespace.Redacted()
			result.RelationSubjectKey = relationSubjectKey.Value()
			result.RelationSubjectKeyRedacted = relationSubjectKey.Redacted()
			result.ManagedInstanceKey = managedInstanceKey.Value()
			result.ManagedInstanceKeyRedacted = managedInstanceKey.Redacted()
		}
		result.RouteID = relation.RouteID()
		result.CanonicalRequestHash = relation.CanonicalRequestHash()
		result.RouteContractVersion = relation.RouteContractVersion()
		result.VerifiedRelationFields = relation.VerifiedRelationFields()
	}
	return result
}

func operationKindStrings(kinds []lock.OperationKind) []string {
	values := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		values = append(values, string(kind))
	}
	return values
}
