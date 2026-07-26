package journal

import (
	"fmt"
	"os"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

type pathMutationKind string

const (
	pathMutationCreate  pathMutationKind = "create"
	pathMutationReplace pathMutationKind = "replace"
	pathMutationRemove  pathMutationKind = "remove"
	pathMutationRecord  pathMutationKind = "record"
)

type previousPathState struct {
	Subject         topology.SubjectID
	Target          target.Target
	ConsumerTargets []target.Target
	Scope           target.Scope
	Destination     output.Destination
	ContentPath     output.ContentPath
	ContentHash     artifact.ContentHash
	ContentKind     realization.PathProjectionContentKind
}

// pathMutation is journal-private normalized capture input. It is not a
// planner or execution authority and cannot escape this package.
type pathMutation struct {
	Kind               pathMutationKind
	Subject            topology.SubjectID
	Target             target.Target
	ConsumerTargets    []target.Target
	Scope              target.Scope
	Destination        output.Destination
	ContentPath        output.ContentPath
	DesiredHash        artifact.ContentHash
	ExpectedExists     bool
	ExpectedPathExists bool
	ExpectedPathMode   os.FileMode
	LiveExists         bool
	LiveHash           artifact.ContentHash
	LivePathExists     bool
	LivePathHash       artifact.ContentHash
	ContentKind        realization.PathProjectionContentKind
	PreviousState      *previousPathState
	AggregateContract  *aggregate.ProjectionContract
	StateIndependent   bool
}

type managedPathMutationFacts struct {
	subject         topology.SubjectID
	consumerTargets []target.Target
	scope           target.Scope
	destination     output.Destination
	desiredHash     artifact.ContentHash
	liveHash        artifact.ContentHash
	contentKind     realization.PathProjectionContentKind
	expectedMode    os.FileMode
	previous        *durable.ManagedPathState
}

type (
	managedPathCreateMutation  struct{ facts managedPathMutationFacts }
	managedPathReplaceMutation struct{ facts managedPathMutationFacts }
	managedPathRemoveMutation  struct{ facts managedPathMutationFacts }
	managedPathRecordMutation  struct{ facts managedPathMutationFacts }
)

// ManagedPathMutation is a closed journal-capture request. It describes
// before/expected-after facts; it grants no host mutation authority.
type ManagedPathMutation struct {
	create  *managedPathCreateMutation
	replace *managedPathReplaceMutation
	remove  *managedPathRemoveMutation
	record  *managedPathRecordMutation
}

func NewManagedPathCreateMutation(
	subject topology.SubjectID,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	desiredHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedMode os.FileMode,
	previous *durable.ManagedPathState,
) (ManagedPathMutation, error) {
	facts := newManagedPathMutationFacts(subject, consumers, scope, destination, desiredHash, "", contentKind, expectedMode, previous)
	mutation := ManagedPathMutation{create: &managedPathCreateMutation{facts: facts}}
	return mutation, mutation.validate()
}

func NewManagedPathReplaceMutation(
	subject topology.SubjectID,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	desiredHash artifact.ContentHash,
	liveHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedMode os.FileMode,
	previous durable.ManagedPathState,
) (ManagedPathMutation, error) {
	facts := newManagedPathMutationFacts(subject, consumers, scope, destination, desiredHash, liveHash, contentKind, expectedMode, &previous)
	mutation := ManagedPathMutation{replace: &managedPathReplaceMutation{facts: facts}}
	return mutation, mutation.validate()
}

func NewManagedPathRemoveMutation(
	previous durable.ManagedPathState,
	liveHash artifact.ContentHash,
) (ManagedPathMutation, error) {
	facts := newManagedPathMutationFacts(
		previous.Subject(),
		previous.ConsumerTargets(),
		previous.Scope(),
		previous.Destination(),
		previous.ContentHash(),
		liveHash,
		previous.ContentKind(),
		0,
		&previous,
	)
	mutation := ManagedPathMutation{remove: &managedPathRemoveMutation{facts: facts}}
	return mutation, mutation.validate()
}

func NewManagedPathRecordMutation(
	subject topology.SubjectID,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	desiredHash artifact.ContentHash,
	liveHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedMode os.FileMode,
	previous *durable.ManagedPathState,
) (ManagedPathMutation, error) {
	facts := newManagedPathMutationFacts(subject, consumers, scope, destination, desiredHash, liveHash, contentKind, expectedMode, previous)
	mutation := ManagedPathMutation{record: &managedPathRecordMutation{facts: facts}}
	return mutation, mutation.validate()
}

func newManagedPathMutationFacts(
	subject topology.SubjectID,
	consumers []target.Target,
	scope target.Scope,
	destination output.Destination,
	desiredHash artifact.ContentHash,
	liveHash artifact.ContentHash,
	contentKind realization.PathProjectionContentKind,
	expectedMode os.FileMode,
	previous *durable.ManagedPathState,
) managedPathMutationFacts {
	canonical := append([]target.Target(nil), consumers...)
	slices.Sort(canonical)
	var previousCopy *durable.ManagedPathState
	if previous != nil {
		copy := *previous
		previousCopy = &copy
	}
	return managedPathMutationFacts{
		subject: subject, consumerTargets: canonical, scope: scope, destination: destination,
		desiredHash: desiredHash, liveHash: liveHash, contentKind: contentKind,
		expectedMode: expectedMode.Perm(), previous: previousCopy,
	}
}

func (mutation ManagedPathMutation) facts() managedPathMutationFacts {
	switch {
	case mutation.create != nil:
		return mutation.create.facts
	case mutation.replace != nil:
		return mutation.replace.facts
	case mutation.remove != nil:
		return mutation.remove.facts
	case mutation.record != nil:
		return mutation.record.facts
	default:
		return managedPathMutationFacts{}
	}
}

func (mutation ManagedPathMutation) kind() pathMutationKind {
	switch {
	case mutation.create != nil:
		return pathMutationCreate
	case mutation.replace != nil:
		return pathMutationReplace
	case mutation.remove != nil:
		return pathMutationRemove
	case mutation.record != nil:
		return pathMutationRecord
	default:
		return ""
	}
}

func (mutation ManagedPathMutation) validate() error {
	facts := mutation.facts()
	if err := facts.subject.Validate(); err != nil {
		return err
	}
	if facts.subject.Kind() != topology.SubjectProjection {
		return fmt.Errorf("managed path journal mutation requires projection subject")
	}
	if _, err := target.ParseScope(string(facts.scope)); err != nil {
		return err
	}
	if err := facts.destination.ValidateScope(facts.scope); err != nil {
		return fmt.Errorf("managed path journal mutation destination: %w", err)
	}
	if err := facts.desiredHash.Validate(); err != nil {
		return fmt.Errorf("managed path journal mutation desired hash: %w", err)
	}
	if facts.contentKind != realization.PathProjectionFile && facts.contentKind != realization.PathProjectionDirectory {
		return fmt.Errorf("managed path journal mutation content kind %q is not implemented", facts.contentKind)
	}
	if facts.contentKind == realization.PathProjectionDirectory && facts.expectedMode != 0 {
		return fmt.Errorf("managed directory journal mutation must not carry expected file mode")
	}
	if facts.contentKind == realization.PathProjectionFile {
		if mutation.kind() == pathMutationRemove && facts.expectedMode != 0 {
			return fmt.Errorf("managed file removal must not carry expected file mode")
		}
		if (mutation.kind() == pathMutationCreate || mutation.kind() == pathMutationReplace) && facts.expectedMode == 0 {
			return fmt.Errorf("managed file journal mutation requires expected file mode")
		}
	}
	if len(facts.consumerTargets) == 0 {
		return fmt.Errorf("managed path journal mutation requires consumer targets")
	}
	previous := target.Target("")
	for index, consumer := range facts.consumerTargets {
		if _, err := target.ParseTarget(string(consumer)); err != nil {
			return err
		}
		if index > 0 && consumer <= previous {
			return fmt.Errorf("managed path journal mutation consumers must be sorted and duplicate-free")
		}
		previous = consumer
	}
	switch mutation.kind() {
	case pathMutationCreate:
		return nil
	case pathMutationReplace:
		if facts.previous == nil {
			return fmt.Errorf("managed path replace journal mutation requires previous state and live hash")
		}
		if facts.liveHash == "" {
			return fmt.Errorf("managed path replace journal mutation requires previous state and live hash")
		}
		if err := facts.liveHash.Validate(); err != nil {
			return fmt.Errorf("managed path replace journal mutation live hash: %w", err)
		}
	case pathMutationRemove:
		if facts.previous == nil {
			return fmt.Errorf("managed path remove journal mutation requires previous state and live hash")
		}
		if facts.liveHash == "" {
			return fmt.Errorf("managed path remove journal mutation requires previous state and live hash")
		}
		if err := facts.liveHash.Validate(); err != nil {
			return fmt.Errorf("managed path remove journal mutation live hash: %w", err)
		}
	case pathMutationRecord:
		if facts.liveHash == "" {
			return fmt.Errorf("managed path record journal mutation requires live hash")
		}
		if err := facts.liveHash.Validate(); err != nil {
			return fmt.Errorf("managed path record journal mutation live hash: %w", err)
		}
	default:
		return fmt.Errorf("managed path journal mutation is invalid")
	}
	return nil
}

func pathMutationFromManaged(
	mutation ManagedPathMutation,
	evidence observe.ManagedPathEvidence,
) pathMutation {
	facts := mutation.facts()
	result := pathMutation{
		Kind: mutation.kind(), Subject: facts.subject, ConsumerTargets: append([]target.Target(nil), facts.consumerTargets...),
		Scope: facts.scope, Destination: facts.destination, DesiredHash: facts.desiredHash,
		ExpectedExists: mutation.kind() != pathMutationRemove, ExpectedPathMode: facts.expectedMode,
		LiveExists: evidence.Exists(), LiveHash: evidence.ContentHash(),
		LivePathExists: evidence.Exists(), LivePathHash: evidence.ContentHash(),
		ContentKind: facts.contentKind,
	}
	if facts.previous != nil {
		result.PreviousState = previousPathStateFromManaged(*facts.previous)
	}
	return result
}

func previousPathStateFromManaged(previous durable.ManagedPathState) *previousPathState {
	return &previousPathState{
		Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(), Scope: previous.Scope(),
		Destination: previous.Destination(), ContentHash: previous.ContentHash(), ContentKind: previous.ContentKind(),
	}
}

func pathMutations(
	managed []ManagedPathMutation,
	aggregates []ManagedAggregateMutation,
	evidence []observe.ManagedPathEvidence,
) ([]pathMutation, error) {
	indexedEvidence, err := managedPathEvidenceByKey(managed, evidence)
	if err != nil {
		return nil, err
	}
	result := make([]pathMutation, 0, len(managed)+len(aggregates))
	for _, mutation := range managed {
		facts := mutation.facts()
		result = append(result, pathMutationFromManaged(
			mutation,
			indexedEvidence[managedPathEvidenceKey{subject: facts.subject, destination: facts.destination}],
		))
	}
	for index, mutation := range aggregates {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed aggregate journal mutation[%d]: %w", index, err)
		}
		result = append(result, pathMutationFromAggregate(mutation))
	}
	return result, nil
}

type managedPathEvidenceKey struct {
	subject     topology.SubjectID
	destination output.Destination
}

func managedPathEvidenceByKey(
	mutations []ManagedPathMutation,
	evidence []observe.ManagedPathEvidence,
) (map[managedPathEvidenceKey]observe.ManagedPathEvidence, error) {
	indexed := make(map[managedPathEvidenceKey]observe.ManagedPathEvidence, len(evidence))
	for index, item := range evidence {
		key := managedPathEvidenceKey{subject: item.Subject(), destination: item.Destination()}
		if _, duplicate := indexed[key]; duplicate {
			return nil, fmt.Errorf("managed path evidence[%d] duplicates subject/address %q", index, item.Destination())
		}
		indexed[key] = item
	}
	for index, mutation := range mutations {
		if err := mutation.validate(); err != nil {
			return nil, fmt.Errorf("managed path journal mutation[%d]: %w", index, err)
		}
		facts := mutation.facts()
		item, ok := indexed[managedPathEvidenceKey{subject: facts.subject, destination: facts.destination}]
		if !ok {
			return nil, fmt.Errorf("managed path mutation[%d] lacks exact subject/address evidence for %q", index, facts.destination)
		}
		switch mutation.kind() {
		case pathMutationCreate:
			if item.Exists() {
				return nil, fmt.Errorf("managed path create mutation[%d] expected absent evidence for %q", index, facts.destination)
			}
		case pathMutationReplace, pathMutationRemove, pathMutationRecord:
			if !item.Exists() || item.ContentHash() != facts.liveHash {
				return nil, fmt.Errorf(
					"managed path mutation[%d] evidence for %q does not match live hash %q",
					index,
					facts.destination,
					facts.liveHash,
				)
			}
		default:
			return nil, fmt.Errorf("managed path mutation[%d] has invalid kind %q", index, mutation.kind())
		}
	}
	return indexed, nil
}

func targetStrings(values []target.Target) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func captureRecoveryExpectedAfter(
	action pathMutation,
) (recovery.ExpectedPathState, recoveryManagedMembership, error) {
	if action.StateIndependent {
		expected, err := expectedPathStateFromMutation(action)
		return expected, recoveryManagedMembership{Managed: false}, err
	}
	switch action.Kind {
	case pathMutationCreate, pathMutationReplace, pathMutationRecord:
		if action.DesiredHash == "" {
			return recovery.ExpectedPathState{}, recoveryManagedMembership{}, fmt.Errorf("action %q desired hash is required", action.Destination)
		}
		expected, err := expectedPathStateFromMutation(action)
		if err != nil {
			return recovery.ExpectedPathState{}, recoveryManagedMembership{}, err
		}
		return expected, recoveryManagedMembership{
			Managed:     true,
			ContentHash: string(action.DesiredHash),
		}, nil
	case pathMutationRemove:
		expected, err := expectedPathStateFromMutation(action)
		if err != nil {
			return recovery.ExpectedPathState{}, recoveryManagedMembership{}, err
		}
		return expected, recoveryManagedMembership{Managed: false}, nil
	default:
		return recovery.ExpectedPathState{}, recoveryManagedMembership{}, fmt.Errorf("action %q kind %q is not recoverable", action.Destination, action.Kind)
	}
}

func expectedPathStateFromMutation(action pathMutation) (recovery.ExpectedPathState, error) {
	if !action.ExpectedExists {
		return recovery.ExpectedPathState{
			Existed:     false,
			PathExisted: action.ExpectedPathExists,
			PathMode:    expectedPermissionMode(action.ExpectedPathExists, action.ExpectedPathMode),
		}, nil
	}
	pathKind, err := expectedPathKindForAction(action)
	if err != nil {
		return recovery.ExpectedPathState{}, err
	}

	return recovery.ExpectedPathState{
		Existed:     true,
		PathExisted: action.ExpectedPathExists,
		PathMode:    expectedPermissionMode(pathKind == recovery.PathKindFile, action.ExpectedPathMode),
		Kind:        pathKind,
		ContentHash: string(action.DesiredHash),
	}, nil
}

func expectedPermissionMode(applies bool, mode os.FileMode) *recovery.PermissionMode {
	if !applies {
		return nil
	}
	return recovery.NewPermissionMode(mode)
}

func expectedPathKindForAction(action pathMutation) (string, error) {
	if action.ContentKind != "" {
		if !action.Subject.IsZero() && len(action.ConsumerTargets) != 0 {
			switch action.ContentKind {
			case realization.PathProjectionFile:
				return recovery.PathKindFile, nil
			case realization.PathProjectionDirectory:
				return recovery.PathKindDirectory, nil
			}
		}
		return "", fmt.Errorf("action %q managed content kind %q is not recoverable", action.Destination, action.ContentKind)
	}
	if !action.Subject.IsZero() {
		return recovery.PathKindFile, nil
	}
	return "", fmt.Errorf("action %q subject is required", action.Destination)
}
