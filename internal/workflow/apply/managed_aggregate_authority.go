package apply

import (
	"sort"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/topology"
)

type aggregateFingerprintFacts struct {
	Kind          reconcile.AggregateDecisionKind
	Reason        reconcile.ActionReason
	Detail        string
	Subjects      []topology.SubjectID
	Projections   []aggregateProjectionFingerprintFacts
	BeforeExists  bool
	BeforeMode    uint32
	AfterExists   bool
	Preconditions []aggregatePreconditionFingerprintFacts
}

type aggregatePreconditionFingerprintFacts struct {
	Kind          string
	Target        string
	Scope         string
	AggregateRoot string
}

type aggregateContractFingerprintFacts struct {
	PlacementID         string
	Target              string
	Scope               string
	AggregateRoot       string
	ContentPath         string
	MergeUnit           string
	Cardinality         string
	SiblingRetention    string
	SiblingPreservation string
	Equivalence         string
	CodecContractID     string
	ComparedFields      []string
}

type aggregateProjectionFingerprintFacts struct {
	Kind     reconcile.AggregateDecisionKind
	Reason   reconcile.ActionReason
	Detail   string
	Contract aggregateContractFingerprintFacts
	Before   aggregateProjectionStateFingerprintFacts
	After    aggregateProjectionStateFingerprintFacts
	Desired  []aggregateContributionFingerprintFacts
	Previous []aggregateContributionFingerprintFacts
}

type aggregateProjectionStateFingerprintFacts struct {
	ParentPresent bool
	Present       bool
	CanonicalHash string
}

type aggregateContributionFingerprintFacts struct {
	Subject       topology.SubjectID
	CanonicalHash string
}

func aggregateFingerprintRows(decisions []reconcile.AggregateDecision) []aggregateFingerprintFacts {
	rows := make([]aggregateFingerprintFacts, 0, len(decisions))
	for _, decision := range decisions {
		before := decision.BeforeDocument()
		after := decision.Rendered().Document()
		row := aggregateFingerprintFacts{
			Kind: decision.Kind(), Reason: decision.Reason(), Detail: decision.Detail(), Subjects: decision.Subjects(),
			BeforeExists: before.Exists(), BeforeMode: uint32(decision.Evidence().FileMode().Perm()),
			AfterExists: after.Exists(),
		}
		for _, projection := range decision.Projections() {
			projectionRow := aggregateProjectionFingerprintFacts{
				Kind: projection.Kind(), Reason: projection.Reason(), Detail: projection.Detail(),
				Contract: aggregateContractFingerprint(projection.Contract()),
				Before:   aggregateProjectionStateFingerprint(projection.Before()),
				After:    aggregateProjectionStateFingerprint(projection.Expected()),
			}
			if desired, present := projection.Desired(); present {
				projectionRow.Desired = aggregateContributionFingerprints(desired.Contributions())
			}
			for _, state := range projection.PreviousStates() {
				projectionRow.Previous = append(projectionRow.Previous, aggregateContributionFingerprintFacts{
					Subject:       state.Subject(),
					CanonicalHash: string(artifact.HashFileContent([]byte(state.Contribution().CanonicalContribution()))),
				})
			}
			sort.Slice(projectionRow.Previous, func(left int, right int) bool {
				return topology.CompareSubjectID(
					projectionRow.Previous[left].Subject,
					projectionRow.Previous[right].Subject,
				) < 0
			})
			row.Projections = append(row.Projections, projectionRow)
		}
		for _, precondition := range decision.OperationPreconditions() {
			document := precondition.DocumentAddress()
			row.Preconditions = append(row.Preconditions, aggregatePreconditionFingerprintFacts{
				Kind: string(precondition.Kind()), Target: string(document.Target()),
				Scope: string(document.Scope()), AggregateRoot: document.AggregateRoot(),
			})
		}
		rows = append(rows, row)
	}
	return rows
}

func aggregateContractFingerprint(contract aggregate.ProjectionContract) aggregateContractFingerprintFacts {
	address := contract.Address()
	document := address.Document()
	return aggregateContractFingerprintFacts{
		PlacementID: address.PlacementID(), Target: string(document.Target()), Scope: string(document.Scope()),
		AggregateRoot: document.AggregateRoot(), ContentPath: string(address.ContentPath()), MergeUnit: string(address.MergeUnit()),
		Cardinality: string(contract.Cardinality()), SiblingRetention: string(contract.SiblingRetention()),
		SiblingPreservation: string(contract.SiblingPreservation()), Equivalence: string(contract.Equivalence()),
		CodecContractID: string(contract.CodecContractID()), ComparedFields: contract.ComparedFields(),
	}
}

func aggregateProjectionStateFingerprint(
	state aggregate.ProjectionState,
) aggregateProjectionStateFingerprintFacts {
	hash := ""
	if state.Present() {
		hash = string(artifact.HashFileContent([]byte(state.CanonicalProjection())))
	}
	return aggregateProjectionStateFingerprintFacts{
		ParentPresent: state.ParentPresent(), Present: state.Present(), CanonicalHash: hash,
	}
}

func aggregateContributionFingerprints(values []aggregate.SubjectContribution) []aggregateContributionFingerprintFacts {
	result := make([]aggregateContributionFingerprintFacts, 0, len(values))
	for _, item := range values {
		result = append(result, aggregateContributionFingerprintFacts{
			Subject:       item.SubjectID(),
			CanonicalHash: string(artifact.HashFileContent([]byte(item.Contribution().CanonicalContribution()))),
		})
	}
	return result
}
