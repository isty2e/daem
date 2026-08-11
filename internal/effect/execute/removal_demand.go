package execute

import (
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

// removalDemandSetForExecution is the only production reachability matrix for
// journaled rooted removals. It is derived from the executable transition
// schedule before journal capture; journal capture only validates and binds
// this typed result to fresh physical evidence.
func removalDemandSetForExecution(
	managed []ManagedPathEffect,
	aggregates []AggregateEffect,
	evidence []observe.ManagedPathEvidence,
) (recovery.RemovalDemandSet, error) {
	evidenceByDestination := make(map[output.Destination][]observe.ManagedPathEvidence, len(evidence))
	for _, item := range evidence {
		evidenceByDestination[item.Destination()] = append(
			evidenceByDestination[item.Destination()],
			item,
		)
	}
	statesByRelation := make(map[removalRelationKey][]recovery.RemovalState)

	add := func(scope target.Scope, destination output.Destination, state recovery.RemovalState) error {
		key := removalRelationKey{scope: scope, destination: destination}
		for _, prior := range statesByRelation[key] {
			if prior.Equal(state) {
				return nil
			}
		}
		statesByRelation[key] = append(statesByRelation[key], state)
		return nil
	}

	for index, effect := range managed {
		if err := addManagedPathRemovalDemands(
			effect,
			evidenceByDestination,
			add,
		); err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf(
				"managed path effect[%d] removal demand: %w",
				index,
				err,
			)
		}
	}
	for index, effect := range aggregates {
		if err := addAggregateRemovalDemands(effect, add); err != nil {
			return recovery.RemovalDemandSet{}, fmt.Errorf(
				"aggregate effect[%d] removal demand: %w",
				index,
				err,
			)
		}
	}

	demands := make([]recovery.RemovalDemand, 0, len(statesByRelation))
	for relation, states := range statesByRelation {
		demand, err := recovery.NewRemovalDemand(
			relation.scope,
			relation.destination,
			states,
		)
		if err != nil {
			return recovery.RemovalDemandSet{}, err
		}
		demands = append(demands, demand)
	}
	return recovery.NewRemovalDemandSet(demands)
}

func addManagedPathRemovalDemands(
	effect ManagedPathEffect,
	evidenceByDestination map[output.Destination][]observe.ManagedPathEvidence,
	add func(target.Scope, output.Destination, recovery.RemovalState) error,
) error {
	switch effect.Kind() {
	case ManagedPathEffectCreate:
		expected, err := expectedManagedPathRemovalState(effect)
		if err != nil {
			return err
		}
		return add(effect.Scope(), effect.Destination(), expected)
	case ManagedPathEffectRemove:
		before, err := beforeManagedPathRemovalState(effect, effect.Destination(), evidenceByDestination)
		if err != nil {
			return err
		}
		return add(effect.Scope(), effect.Destination(), before)
	case ManagedPathEffectReplace:
		expected, err := expectedManagedPathRemovalState(effect)
		if err != nil {
			return err
		}
		previous, present := effect.PreviousState()
		if !present {
			return fmt.Errorf("replace effect lacks previous state")
		}
		if previous.Scope() != effect.Scope() || previous.Destination() != effect.Destination() {
			before, err := beforeManagedPathRemovalState(
				effect,
				previous.Destination(),
				evidenceByDestination,
			)
			if err != nil {
				return err
			}
			if err := add(previous.Scope(), previous.Destination(), before); err != nil {
				return err
			}
			return add(effect.Scope(), effect.Destination(), expected)
		}
		if effect.ContentKind() != realization.PathProjectionDirectory {
			return nil
		}
		before, err := beforeManagedPathRemovalState(
			effect,
			effect.Destination(),
			evidenceByDestination,
		)
		if err != nil {
			return err
		}
		if err := add(effect.Scope(), effect.Destination(), before); err != nil {
			return err
		}
		return add(effect.Scope(), effect.Destination(), expected)
	case ManagedPathEffectRecord:
		return nil
	default:
		return fmt.Errorf("unsupported managed path effect kind %q", effect.Kind())
	}
}

func beforeManagedPathRemovalState(
	effect ManagedPathEffect,
	destination output.Destination,
	evidenceByDestination map[output.Destination][]observe.ManagedPathEvidence,
) (recovery.RemovalState, error) {
	previous, present := effect.PreviousState()
	if !present {
		return recovery.RemovalState{}, fmt.Errorf(
			"removal destination %q lacks previous state",
			destination,
		)
	}
	kind, err := recoveryPathKind(previous.ContentKind())
	if err != nil {
		return recovery.RemovalState{}, err
	}
	state := recovery.BeforePathState{
		Existed:     true,
		Kind:        kind,
		ContentHash: string(previous.ContentHash()),
	}
	if kind == recovery.PathKindFile {
		mode, err := managedRemovalFileMode(effect, destination, previous, evidenceByDestination)
		if err != nil {
			return recovery.RemovalState{}, err
		}
		state.PathMode = recovery.NewPermissionMode(mode)
	}
	return recovery.NewBeforeRemovalState(state)
}

func managedRemovalFileMode(
	effect ManagedPathEffect,
	destination output.Destination,
	previous durable.ManagedPathState,
	evidenceByDestination map[output.Destination][]observe.ManagedPathEvidence,
) (os.FileMode, error) {
	// The live effect mode is exact for an in-place transition. A relocation
	// needs the separately observed old destination because its persisted state
	// may intentionally use executable-class rather than exact permissions.
	if effect.Destination() == destination && effect.LiveHash() == previous.ContentHash() {
		return effect.LiveFileMode(), nil
	}
	items := evidenceByDestination[destination]
	for _, item := range items {
		if item.Exists() && item.ContentHash() == previous.ContentHash() {
			return item.FileMode(), nil
		}
	}
	return 0, fmt.Errorf(
		"relocated file removal destination %q lacks matching live mode evidence",
		destination,
	)
}

func expectedManagedPathRemovalState(effect ManagedPathEffect) (recovery.RemovalState, error) {
	kind, err := recoveryPathKind(effect.ContentKind())
	if err != nil {
		return recovery.RemovalState{}, err
	}
	state := recovery.ExpectedPathState{
		Existed:     true,
		Kind:        kind,
		ContentHash: string(effect.DesiredHash()),
	}
	if kind == recovery.PathKindFile {
		state.PathMode = recovery.NewPermissionMode(effect.DesiredFileMode())
	}
	return recovery.NewExpectedRemovalState(state)
}

func addAggregateRemovalDemands(
	effect AggregateEffect,
	add func(target.Scope, output.Destination, recovery.RemovalState) error,
) error {
	if effect.journaledProjectionCount() == 0 {
		return nil
	}
	switch effect.Kind() {
	case AggregateEffectCreate:
		state, err := aggregateDocumentExpectedRemovalState(effect)
		if err != nil {
			return err
		}
		return add(effect.Scope(), effect.Destination(), state)
	case AggregateEffectRemove:
		// Removing one contribution usually rewrites a still-present shared
		// document. Only a document transition to absent can invoke the
		// journaled rooted-removal operation and therefore needs a residue
		// intent.
		if effect.Rendered().Document().Exists() {
			return nil
		}
		state, err := aggregateDocumentBeforeRemovalState(effect)
		if err != nil {
			return err
		}
		return add(effect.Scope(), effect.Destination(), state)
	case AggregateEffectReplace, AggregateEffectRecord:
		return nil
	default:
		return fmt.Errorf("unsupported aggregate effect kind %q", effect.Kind())
	}
}

func aggregateDocumentBeforeRemovalState(effect AggregateEffect) (recovery.RemovalState, error) {
	return aggregateDocumentRemovalState(effect.BeforeDocument(), aggregate.DocumentFileMode, false)
}

func aggregateDocumentExpectedRemovalState(effect AggregateEffect) (recovery.RemovalState, error) {
	return aggregateDocumentRemovalState(effect.Rendered().Document(), aggregate.DocumentFileMode, true)
}

func aggregateDocumentRemovalState(
	document aggregate.Document,
	mode os.FileMode,
	expected bool,
) (recovery.RemovalState, error) {
	if !document.Exists() {
		if expected {
			return recovery.NewExpectedRemovalState(recovery.ExpectedPathState{})
		}
		return recovery.NewBeforeRemovalState(recovery.BeforePathState{})
	}
	stateHash := artifact.HashFileContentWithExecutable(
		document.Content(),
		mode.Perm()&0o111 != 0,
	)
	if expected {
		return recovery.NewExpectedRemovalState(recovery.ExpectedPathState{
			Existed:     true,
			PathExisted: true,
			Kind:        recovery.PathKindFile,
			ContentHash: string(stateHash),
			PathMode:    recovery.NewPermissionMode(mode),
		})
	}
	return recovery.NewBeforeRemovalState(recovery.BeforePathState{
		Existed:       true,
		PathExisted:   true,
		ParentExisted: true,
		Kind:          recovery.PathKindFile,
		ContentHash:   string(stateHash),
		PathMode:      recovery.NewPermissionMode(mode),
	})
}

func recoveryPathKind(kind realization.PathProjectionContentKind) (string, error) {
	switch kind {
	case realization.PathProjectionFile:
		return recovery.PathKindFile, nil
	case realization.PathProjectionDirectory:
		return recovery.PathKindDirectory, nil
	default:
		return "", fmt.Errorf("unsupported managed removal content kind %q", kind)
	}
}
