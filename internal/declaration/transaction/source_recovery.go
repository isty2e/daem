package transaction

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

// SourceRecovery binds read-only recovery evidence to independently selected
// declaration/source targets. It does not grant approval or acquire leases.
type SourceRecovery struct {
	stateDir    string
	targets     []declarationartifact.MutationTarget
	reference   fileset.ReferenceEvidenceView
	disposition fileset.RecoveryDisposition
}

func InspectSourceTransactionRecovery(ctx context.Context, stateDir string, manifest, source declarationartifact.MutationTarget) (SourceRecovery, error) {
	return inspectSourceRecovery(ctx, stateDir, manifest, []declarationartifact.MutationTarget{manifest, source})
}

func InspectSourceRetirementRecovery(ctx context.Context, stateDir string, manifest declarationartifact.MutationTarget) (SourceRecovery, error) {
	return inspectSourceRecovery(ctx, stateDir, manifest, []declarationartifact.MutationTarget{manifest})
}

func inspectSourceRecovery(ctx context.Context, stateDir string, manifest declarationartifact.MutationTarget, targets []declarationartifact.MutationTarget) (SourceRecovery, error) {
	if ctx == nil {
		return SourceRecovery{}, fmt.Errorf("source recovery context is required")
	}

	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		if err := target.Validate(ctx); err != nil {
			return SourceRecovery{}, err
		}
		paths = append(paths, target.Path())
	}

	reference, err := fileset.ReadFileSetReferenceEvidence4(ctx, stateDir, manifest.Path())
	if err != nil {
		return SourceRecovery{}, err
	}
	disposition, err := fileset.InspectFileSetRecovery4(ctx, stateDir, paths)
	if err != nil {
		return SourceRecovery{}, err
	}
	result := SourceRecovery{stateDir: stateDir, targets: targets, reference: reference, disposition: disposition}
	if err := result.validate(ctx); err != nil {
		return SourceRecovery{}, err
	}
	return result, nil
}

func (recovery SourceRecovery) Reference() fileset.ReferenceEvidenceView { return recovery.reference }

func (recovery SourceRecovery) Disposition() fileset.RecoveryDisposition { return recovery.disposition }

func (recovery SourceRecovery) paths() []string {
	paths := make([]string, 0, len(recovery.targets))
	for _, target := range recovery.targets {
		paths = append(paths, target.Path())
	}
	return paths
}

func (recovery SourceRecovery) validate(ctx context.Context) error {
	if recovery.stateDir == "" || len(recovery.targets) == 0 || recovery.reference.MarkerRevision() == "" {
		return fmt.Errorf("source recovery is uninitialized")
	}

	for _, target := range recovery.targets {
		if err := target.Validate(ctx); err != nil {
			return err
		}
	}

	reference, err := fileset.ReadFileSetReferenceEvidence4(ctx, recovery.stateDir, recovery.reference.ReferenceNamespace())
	if err != nil {
		return err
	}
	if reference.MarkerRevision() != recovery.reference.MarkerRevision() {
		return mutation.StaleSnapshotError{}
	}

	disposition, err := fileset.InspectFileSetRecovery4(ctx, recovery.stateDir, recovery.paths())
	if err != nil {
		return err
	}
	if disposition != recovery.disposition {
		return mutation.StaleSnapshotError{}
	}
	return nil
}

// Recover terminates this recovery operation without starting a new capture.
// The caller must revalidate StateDir authority and hold all selected leases.
func (recovery SourceRecovery) Recover(ctx context.Context) (fileset.RecoveryDisposition, error) {
	if err := recovery.validate(ctx); err != nil {
		return "", err
	}
	if err := fileset.RecoverFileSet4(ctx, recovery.stateDir, recovery.paths()); err != nil {
		return "", err
	}
	return recovery.disposition, nil
}
