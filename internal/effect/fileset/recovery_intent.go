package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/mutation"
)

var ErrRecoveryIntentMismatch = errors.New("file-set evidence differs from the containing intent")

// RecoveryTarget is an independently selected target and its expected marker
// facts. A persisted path is not a substitute for the caller's leased selection.
type RecoveryTarget struct {
	Path         string
	BeforeExists bool
	BeforeHash   string
	BeforeMode   os.FileMode
	AfterHash    string
	Write        bool
	CommitPoint  bool
}

// CommitFreshFileSetForIntent refuses pending evidence and captures matching
// before/after facts before publishing a marker or writing any target. Callers
// hold the target and evidence-root leases through publication.
func CommitFreshFileSetForIntent(ctx context.Context, input FileSetInput, expected []RecoveryTarget) error {
	if ctx == nil {
		return fmt.Errorf("file-set intent publication context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	targets, err := canonicalRecoveryTargets(expected)
	if err != nil {
		return err
	}
	return commitWithOperationsIntentVersion(ctx, input, operations{writeFile: commitFile}, contractversion.MetadataTransaction, false, targets)
}

// InspectFileSetRecoveryForIntent previews the existing recovery decision only
// when every target and image matches the containing durable intent.
func InspectFileSetRecoveryForIntent(ctx context.Context, stateDir string, expected []RecoveryTarget) (RecoveryDisposition, error) {
	canonical, marker, err := loadFileSetIntent(ctx, stateDir, expected)
	if err != nil {
		return "", err
	}
	return inspectLoadedFileSetRecovery(ctx, canonical, marker)
}

// RecoverFileSetForIntent restores or finalizes only matching marker evidence.
// With no marker, residue must be clear. Callers hold target and evidence-root leases.
func RecoverFileSetForIntent(ctx context.Context, stateDir string, expected []RecoveryTarget) error {
	canonical, marker, err := loadFileSetIntent(ctx, stateDir, expected)
	if errors.Is(err, errFileSetMarkerAbsent) {
		return rejectAbandonedFileSetResidue(ctx, canonical)
	}
	if err != nil {
		return err
	}
	return recoverLoadedFileSet(ctx, canonical, marker, operations{writeFile: commitFile})
}

func loadFileSetIntent(ctx context.Context, stateDir string, expected []RecoveryTarget) (string, transactionMarker, error) {
	if ctx == nil {
		return "", transactionMarker{}, fmt.Errorf("file-set intent recovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", transactionMarker{}, err
	}
	targets, err := canonicalRecoveryTargets(expected)
	if err != nil {
		return "", transactionMarker{}, err
	}
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return "", transactionMarker{}, err
	}
	marker, err := loadMarker(ctx, markerPath(canonical))
	if err != nil {
		return canonical, transactionMarker{}, err
	}
	if err := matchFileSetIntent(marker, targets); err != nil {
		return canonical, transactionMarker{}, err
	}
	return canonical, marker, nil
}

func canonicalRecoveryTargets(expected []RecoveryTarget) (map[string]RecoveryTarget, error) {
	if len(expected) == 0 {
		return nil, fmt.Errorf("%w: an exact target set is required", ErrRecoveryIntentMismatch)
	}
	if err := admitFileSetTargetCount(len(expected)); err != nil {
		return nil, err
	}
	targets := make(map[string]RecoveryTarget, len(expected))
	for _, target := range expected {
		if target.BeforeExists {
			if !isHash(target.BeforeHash) || target.BeforeMode == 0 || target.BeforeMode&^os.ModePerm != 0 {
				return nil, fmt.Errorf("%w: invalid target before-image", ErrRecoveryIntentMismatch)
			}
		} else if target.BeforeHash != "" || target.BeforeMode != 0 {
			return nil, fmt.Errorf("%w: absent before-image has file metadata", ErrRecoveryIntentMismatch)
		}
		if target.Write {
			if !isHash(target.AfterHash) {
				return nil, fmt.Errorf("%w: invalid target after-image", ErrRecoveryIntentMismatch)
			}
		} else if target.AfterHash != "" || target.CommitPoint {
			return nil, fmt.Errorf("%w: retained target has publication metadata", ErrRecoveryIntentMismatch)
		}
		path, err := mutation.CanonicalDirectoryEntryPath(target.Path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := targets[path]; duplicate {
			return nil, fmt.Errorf("%w: duplicate selected target", ErrRecoveryIntentMismatch)
		}
		target.Path = path
		targets[path] = target
	}
	return targets, nil
}

func matchFileSetIntent(marker transactionMarker, targets map[string]RecoveryTarget) error {
	if len(marker.Targets) != len(targets) {
		return fmt.Errorf("%w: target set differs", ErrRecoveryIntentMismatch)
	}
	for _, target := range marker.Targets {
		value, selected := targets[target.Path]
		if !selected || value.BeforeExists != target.Before.Exists || value.BeforeHash != target.Before.Hash ||
			value.BeforeMode != os.FileMode(target.Before.Mode) || value.AfterHash != target.AfterHash ||
			value.Write != target.Write || value.CommitPoint != target.CommitPoint {
			return fmt.Errorf("%w: target %q", ErrRecoveryIntentMismatch, target.Path)
		}
	}
	return nil
}
