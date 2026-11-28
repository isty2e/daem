package transaction

import (
	"context"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

// PairRecovery binds the file-set proof to the selected declaration entries
// and their referents, including the original link resolution.
type PairRecovery struct {
	selection FinalizationPair
	evidence  *fileset.PairRecovery
}

func (pair FinalizationPair) ObserveRecovery(ctx context.Context, stateDir string, controls []string, budget rootedpath.PhysicalTraversalBudget) (*PairRecovery, error) {
	if err := pair.rejectControlOverlap(controls, budget); err != nil {
		return nil, err
	}
	evidence, err := fileset.ObservePairRecovery(ctx, stateDir, pair.referents[0], pair.referents[1])
	if err != nil || evidence == nil {
		return nil, err
	}
	if err := pair.validate(ctx); err != nil {
		return nil, err
	}
	return &PairRecovery{selection: pair, evidence: evidence}, nil
}

func (recovery *PairRecovery) Disposition() fileset.RecoveryDisposition {
	return recovery.evidence.Disposition()
}

func (recovery *PairRecovery) Referents() (manifest, lockfile string) {
	return recovery.selection.referents[0], recovery.selection.referents[1]
}

func (recovery *PairRecovery) RemovesLockfile() bool { return recovery.evidence.RemovesCompanion() }

func (recovery *PairRecovery) Settle(ctx context.Context, controls []string, budget rootedpath.PhysicalTraversalBudget, revalidate func(context.Context) error) (bool, error) {
	return recovery.evidence.Settle(ctx, func(ctx context.Context) error {
		if err := revalidate(ctx); err != nil {
			return err
		}
		if err := recovery.selection.rejectControlOverlap(controls, budget); err != nil {
			return err
		}
		return recovery.selection.validate(ctx)
	})
}
