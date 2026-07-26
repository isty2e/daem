package execute

import (
	"context"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/topology"
)

type statefileCommitStatus uint8

const (
	statefileCommitInvalid statefileCommitStatus = iota
	statefileCommitted
	statefileUncommitted
	statefileCommitIndeterminate
)

type statefileCommitOutcome struct {
	status statefileCommitStatus
	err    error
}

type statefileCommitter func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome

func commitCarrierState(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	next durable.Snapshot,
	stateEncoder durable.SnapshotEncoder,
	label string,
) error {
	if stateEncoder == nil {
		return fmt.Errorf("%s state codec is required", label)
	}
	content, err := stateEncoder.Encode(next)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	if err := rootedSnapshotCommitter(filesystem, authority)(ctx, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}

func rootedSnapshotCommitter(
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
) snapshotCommitter {
	return func(ctx context.Context, content []byte, mode os.FileMode) error {
		return commitRootedControlFile(ctx, filesystem, authority, content, mode)
	}
}

func (outcome statefileCommitOutcome) committed() bool {
	return outcome.valid() && outcome.status == statefileCommitted
}

func (outcome statefileCommitOutcome) requiresRecovery() bool {
	if !outcome.valid() {
		return true
	}
	return outcome.status == statefileCommitIndeterminate
}

func (outcome statefileCommitOutcome) failure() error {
	if outcome.err != nil {
		return outcome.err
	}
	return fmt.Errorf("invalid statefile commit outcome %d", outcome.status)
}

func (outcome statefileCommitOutcome) valid() bool {
	switch outcome.status {
	case statefileCommitted:
		return outcome.err == nil
	case statefileUncommitted, statefileCommitIndeterminate:
		return outcome.err != nil
	default:
		return false
	}
}

func snapshotAfterManagedPathEffects(
	snapshot durable.Snapshot,
	effects []ManagedPathEffect,
) (durable.Snapshot, error) {
	replacements := make(map[topology.SubjectID]durable.ManagedPathState, len(effects))
	removals := make(map[topology.SubjectID]struct{}, len(effects))
	for index, effect := range effects {
		if err := effect.validate(); err != nil {
			return durable.Snapshot{}, fmt.Errorf("managed path effect[%d]: %w", index, err)
		}
		if previous, present := effect.PreviousState(); present {
			removals[previous.Subject()] = struct{}{}
		}
		if effect.Kind() == ManagedPathEffectRemove {
			continue
		}
		if _, duplicate := replacements[effect.Subject()]; duplicate {
			return durable.Snapshot{}, fmt.Errorf(
				"duplicate managed path state replacement for subject %q",
				effect.Subject(),
			)
		}
		state, err := durable.NewManagedPathState(
			effect.Subject(),
			effect.ConsumerTargets(),
			effect.Scope(),
			effect.Destination(),
			effect.DesiredHash(),
			effect.ContentKind(),
			effect.PermissionPolicy(),
			effect.StateFileMode(),
		)
		if err != nil {
			return durable.Snapshot{}, fmt.Errorf(
				"managed path effect[%d] state: %w",
				index,
				err,
			)
		}
		replacements[effect.Subject()] = state
		delete(removals, effect.Subject())
	}

	current := snapshot.ManagedPaths()
	next := make([]durable.ManagedPathState, 0, len(current)+len(replacements))
	for _, existing := range current {
		subject := existing.Subject()
		if replacement, replace := replacements[subject]; replace {
			next = append(next, replacement)
			delete(replacements, subject)
			continue
		}
		if _, remove := removals[subject]; remove {
			continue
		}
		next = append(next, existing)
	}
	for _, replacement := range replacements {
		next = append(next, replacement)
	}
	return snapshot.WithManagedPaths(next)
}

func snapshotAfterAggregateEffects(
	snapshot durable.Snapshot,
	effects []AggregateEffect,
) (durable.Snapshot, error) {
	removeSubjects := make(map[topology.SubjectID]struct{})
	replacements := make(map[topology.SubjectID]durable.ManagedAggregateState)
	for index, effect := range effects {
		if err := effect.validate(); err != nil {
			return durable.Snapshot{}, fmt.Errorf("aggregate effect[%d]: %w", index, err)
		}
		for _, previous := range effect.PreviousStates() {
			removeSubjects[previous.Subject()] = struct{}{}
		}
		for _, item := range effect.DesiredContributions() {
			if _, duplicate := replacements[item.SubjectID()]; duplicate {
				return durable.Snapshot{}, fmt.Errorf(
					"duplicate aggregate state replacement for subject %q",
					item.SubjectID(),
				)
			}
			state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
			if err != nil {
				return durable.Snapshot{}, fmt.Errorf(
					"aggregate effect[%d] state subject %q: %w",
					index,
					item.SubjectID(),
					err,
				)
			}
			replacements[item.SubjectID()] = state
			delete(removeSubjects, item.SubjectID())
		}
	}

	current := snapshot.ManagedAggregates()
	next := make([]durable.ManagedAggregateState, 0, len(current)+len(replacements))
	for _, existing := range current {
		subject := existing.Subject()
		if replacement, replace := replacements[subject]; replace {
			next = append(next, replacement)
			delete(replacements, subject)
			continue
		}
		if _, remove := removeSubjects[subject]; remove {
			continue
		}
		next = append(next, existing)
	}
	for _, replacement := range replacements {
		next = append(next, replacement)
	}
	return snapshot.WithManagedAggregates(next)
}

func snapshotAfterRetiredProjectCarrierClaims(
	snapshot durable.Snapshot,
	claims []durablecarrier.ManagedCarrierClaim,
) (durable.Snapshot, int, error) {
	next := snapshot
	for index, claim := range claims {
		for previous := range index {
			if claims[previous].ExactEqual(claim) {
				return durable.Snapshot{}, 0, fmt.Errorf(
					"retired project carrier claim[%d] duplicates claim[%d]",
					index,
					previous,
				)
			}
		}
		updated, changed, err := next.WithoutManagedCarrierClaim(claim)
		if err != nil {
			return durable.Snapshot{}, 0, fmt.Errorf(
				"retire project carrier claim[%d]: %w",
				index,
				err,
			)
		}
		if !changed {
			return durable.Snapshot{}, 0, fmt.Errorf(
				"retire project carrier claim[%d]: exact claim is absent",
				index,
			)
		}
		next = updated
	}
	return next, len(claims), nil
}
