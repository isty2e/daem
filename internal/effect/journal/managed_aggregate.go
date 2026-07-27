package journal

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// AggregateMutationKind identifies one physical aggregate journal transition.
type AggregateMutationKind string

const (
	AggregateMutationCreate  AggregateMutationKind = "create"
	AggregateMutationReplace AggregateMutationKind = "replace"
	AggregateMutationRemove  AggregateMutationKind = "remove"
	AggregateMutationRecord  AggregateMutationKind = "record"
)

// ManagedAggregateMutation is a journal-capture request for one physical
// projection transition. Subject state remains correlated by the full
// statefile before/after snapshots rather than this group-level path row.
type ManagedAggregateMutation struct {
	kind     AggregateMutationKind
	subject  topology.SubjectID
	contract aggregate.ProjectionContract
	before   aggregate.Document
	snapshot aggregate.Snapshot
	expected aggregate.RenderedDocument
	fileMode os.FileMode
}

func validateAggregateProjectionCorrelation(
	subject topology.SubjectID,
	selectedTarget target.Target,
	scope target.Scope,
	destination output.Destination,
	contentPath output.ContentPath,
	contract aggregate.ProjectionContract,
) error {
	if err := aggregate.ValidateSubjectContract(subject, contract); err != nil {
		return fmt.Errorf("aggregate projection subject: %w", err)
	}
	document := contract.Address().Document()
	if selectedTarget != document.Target() {
		return fmt.Errorf(
			"aggregate projection target %q does not match contract target %q",
			selectedTarget,
			document.Target(),
		)
	}
	if scope != document.Scope() {
		return fmt.Errorf(
			"aggregate projection scope %q does not match contract scope %q",
			scope,
			document.Scope(),
		)
	}
	if destination != document.AggregateRoot() ||
		string(contentPath) != string(contract.Address().ContentPath()) {
		return fmt.Errorf(
			"aggregate projection address %q%q does not match contract address %q%q",
			destination,
			contentPath,
			document.AggregateRoot(),
			contract.Address().ContentPath(),
		)
	}
	return nil
}

// NewManagedAggregateMutation constructs one validated capture request.
func NewManagedAggregateMutation(
	kind AggregateMutationKind,
	subject topology.SubjectID,
	contract aggregate.ProjectionContract,
	before aggregate.Document,
	snapshot aggregate.Snapshot,
	expected aggregate.RenderedDocument,
	fileMode os.FileMode,
) (ManagedAggregateMutation, error) {
	mutation := ManagedAggregateMutation{
		kind: kind, subject: subject, contract: contract.Clone(), before: before,
		snapshot: snapshot, expected: expected, fileMode: fileMode.Perm(),
	}
	if err := mutation.validate(); err != nil {
		return ManagedAggregateMutation{}, err
	}
	return mutation, nil
}

func (mutation ManagedAggregateMutation) validate() error {
	switch mutation.kind {
	case AggregateMutationCreate, AggregateMutationReplace, AggregateMutationRemove, AggregateMutationRecord:
	default:
		return fmt.Errorf("managed aggregate journal mutation kind %q is unsupported", mutation.kind)
	}
	if err := aggregate.ValidateSubjectContract(mutation.subject, mutation.contract); err != nil {
		return fmt.Errorf("managed aggregate subject contract: %w", err)
	}
	if err := mutation.before.Validate(); err != nil {
		return err
	}
	if mutation.before.Exists() != mutation.snapshot.DocumentExisted() {
		return fmt.Errorf("managed aggregate before document differs from snapshot")
	}
	beforeState, present := mutation.snapshot.State(mutation.contract)
	if !present {
		return fmt.Errorf("managed aggregate snapshot does not cover its contract")
	}
	expectedState, present := mutation.expected.Expected().State(mutation.contract)
	if !present || mutation.expected.Document().Exists() != mutation.expected.Expected().DocumentExisted() {
		return fmt.Errorf("managed aggregate expected document does not cover its contract")
	}
	if err := validateAggregateMutationTransition(
		mutation.kind,
		beforeState.Present(),
		expectedState.Present(),
	); err != nil {
		return err
	}
	if !mutation.before.Exists() && mutation.fileMode != 0 {
		return fmt.Errorf("managed aggregate absent before document cannot carry file mode")
	}
	return nil
}

func validateAggregateMutationTransition(kind AggregateMutationKind, beforePresent bool, expectedPresent bool) error {
	valid := false
	switch kind {
	case AggregateMutationCreate:
		valid = !beforePresent && expectedPresent
	case AggregateMutationReplace:
		valid = beforePresent && expectedPresent
	case AggregateMutationRemove:
		valid = beforePresent && !expectedPresent
	case AggregateMutationRecord:
		valid = beforePresent && expectedPresent
	default:
		return fmt.Errorf("managed aggregate journal mutation kind %q is unsupported", kind)
	}
	if !valid {
		return fmt.Errorf(
			"managed aggregate %s mutation has invalid projection transition %t -> %t",
			kind,
			beforePresent,
			expectedPresent,
		)
	}
	return nil
}

func pathMutationFromAggregate(mutation ManagedAggregateMutation) pathMutation {
	beforeState, _ := mutation.snapshot.State(mutation.contract)
	expectedState, _ := mutation.expected.Expected().State(mutation.contract)
	expectedDocument := mutation.expected.Document()
	expectedPathMode := os.FileMode(0)
	if expectedDocument.Exists() {
		expectedPathMode = aggregate.DocumentFileMode
	}
	result := pathMutation{
		Kind: pathMutationKind(mutation.kind), Subject: mutation.subject,
		Target: mutation.contract.Address().Document().Target(), Scope: mutation.contract.Address().Document().Scope(),
		Destination: mutation.contract.Address().Document().AggregateRoot(),
		ContentPath: output.ContentPath(mutation.contract.Address().ContentPath()),
		LiveExists:  beforeState.Present(), LiveHash: projectionStateHash(beforeState),
		DesiredHash: projectionStateHash(expectedState), ExpectedExists: expectedState.Present(),
		ExpectedPathExists: expectedDocument.Exists(), ExpectedPathMode: expectedPathMode,
		LivePathExists: mutation.before.Exists(), LivePathHash: documentHash(mutation.before, mutation.fileMode),
		AggregateContract: pointerToAggregateContract(mutation.contract),
		StateIndependent:  true,
	}
	return result
}

func projectionStateHash(state aggregate.ProjectionState) artifact.ContentHash {
	if !state.Present() {
		return ""
	}
	return artifact.HashFileContent([]byte(state.CanonicalProjection()))
}

func documentHash(document aggregate.Document, fileMode os.FileMode) artifact.ContentHash {
	if !document.Exists() {
		return ""
	}
	return artifact.HashFileContentWithExecutable(document.Content(), fileMode.Perm()&0o111 != 0)
}

func pointerToAggregateContract(contract aggregate.ProjectionContract) *aggregate.ProjectionContract {
	copy := contract.Clone()
	return &copy
}

func persistedAggregateContractFromMutation(mutation pathMutation) *recoveryAggregateContract {
	if mutation.AggregateContract == nil {
		return nil
	}
	return persistedAggregateContract(*mutation.AggregateContract)
}

type recoveryAggregateContract struct {
	PlacementID         string   `json:"placement_id"`
	Target              string   `json:"target"`
	Scope               string   `json:"scope"`
	AggregateRoot       string   `json:"aggregate_root"`
	ContentPath         string   `json:"content_path"`
	MergeUnit           string   `json:"merge_unit"`
	Cardinality         string   `json:"cardinality"`
	SiblingRetention    string   `json:"sibling_retention"`
	SiblingPreservation string   `json:"sibling_preservation"`
	Equivalence         string   `json:"equivalence"`
	CodecContractID     string   `json:"codec_contract_id"`
	ComparedFields      []string `json:"compared_fields,omitempty"`
}

func persistedAggregateContract(contract aggregate.ProjectionContract) *recoveryAggregateContract {
	address := contract.Address()
	document := address.Document()
	return &recoveryAggregateContract{
		PlacementID: address.PlacementID(), Target: string(document.Target()), Scope: string(document.Scope()),
		AggregateRoot: document.AggregateRoot().String(), ContentPath: string(address.ContentPath()), MergeUnit: string(address.MergeUnit()),
		Cardinality: string(contract.Cardinality()), SiblingRetention: string(contract.SiblingRetention()),
		SiblingPreservation: string(contract.SiblingPreservation()), Equivalence: string(contract.Equivalence()),
		CodecContractID: string(contract.CodecContractID()), ComparedFields: contract.ComparedFields(),
	}
}

func (persisted recoveryAggregateContract) canonical() (aggregate.ProjectionContract, error) {
	aggregateRoot, err := output.Parse(persisted.AggregateRoot)
	if err != nil {
		return aggregate.ProjectionContract{}, err
	}
	contribution, err := aggregate.NewManagedContribution(aggregate.ManagedContributionInput{
		PlacementID: persisted.PlacementID, Target: target.Target(persisted.Target), Scope: target.Scope(persisted.Scope),
		AggregateRoot: aggregateRoot, ContentPath: persisted.ContentPath, MergeUnit: aggregate.MergeUnit(persisted.MergeUnit),
		Cardinality:         aggregate.ContributionCardinality(persisted.Cardinality),
		SiblingRetention:    aggregate.SiblingRetention(persisted.SiblingRetention),
		SiblingPreservation: aggregate.SiblingPreservation(persisted.SiblingPreservation),
		Equivalence:         aggregate.Equivalence(persisted.Equivalence), CanonicalContribution: "recovery-contract",
		CodecContractID: aggregate.CodecContractID(persisted.CodecContractID), ComparedFields: persisted.ComparedFields,
	})
	if err != nil {
		return aggregate.ProjectionContract{}, err
	}
	return contribution.Contract(), nil
}
