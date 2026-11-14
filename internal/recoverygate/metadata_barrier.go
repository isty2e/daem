package recoverygate

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	daempaths "github.com/isty2e/daem/internal/paths"
)

type metadataBarrierMode uint8

const (
	metadataBarrierClear metadataBarrierMode = iota + 1
	metadataBarrierOwnedRecovery
)

type metadataParticipantBarrier struct {
	paths    daempaths.Paths
	stateDir StateDirAuthority
}

// MetadataBarrier observes one global predicate and the complete local barrier
// set. It provides no normal effect, reservation or directory-creation authority.
type MetadataBarrier struct {
	paths        daempaths.Paths
	mode         metadataBarrierMode
	revision     string
	participants []metadataParticipantBarrier
	domains      []mutation.Domain
	budget       fileset.PhysicalWorkBudget
}

// CaptureMetadataConversionBarrier requires global clear. The caller separately
// retains the original command's ordinary EffectAuthority and eligibility.
func CaptureMetadataConversionBarrier(ctx context.Context, paths daempaths.Paths, stateDirs []string, budget fileset.PhysicalWorkBudget) (MetadataBarrier, error) {
	return captureMetadataBarrier(ctx, paths, stateDirs, metadataBarrierClear, "", budget)
}

// CaptureMetadataRecoveryBarrier binds this operation's exact gen5 revision
// instead of requiring global clear. FileSet alone compares the expected intent.
// This does not rerun normal loaders against a mixed Before/After metadata set.
func CaptureMetadataRecoveryBarrier(ctx context.Context, paths daempaths.Paths, stateDirs []string, revision string, budget fileset.PhysicalWorkBudget) (MetadataBarrier, error) {
	if revision == "" {
		return MetadataBarrier{}, fmt.Errorf("global metadata recovery revision is required")
	}
	return captureMetadataBarrier(ctx, paths, stateDirs, metadataBarrierOwnedRecovery, revision, budget)
}

func captureMetadataBarrier(ctx context.Context, paths daempaths.Paths, stateDirs []string, mode metadataBarrierMode, revision string, budget fileset.PhysicalWorkBudget) (MetadataBarrier, error) {
	if ctx == nil || budget == nil || len(stateDirs) == 0 {
		return MetadataBarrier{}, fmt.Errorf("metadata barrier requires context, participants and physical work budget")
	}
	barrier := MetadataBarrier{paths: paths, mode: mode, revision: revision, budget: budget}
	if err := barrier.validateGlobal(ctx); err != nil {
		return MetadataBarrier{}, err
	}
	directories := slices.Clone(stateDirs)
	slices.Sort(directories)
	directories = slices.Compact(directories)
	for _, stateDir := range directories {
		if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
			return MetadataBarrier{}, fmt.Errorf("metadata participant requires an absolute clean StateDir")
		}
		captured, err := CaptureStateDirBounded(ctx, stateDir, mutationfs.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return MetadataBarrier{}, err
		}
		participant := metadataParticipantBarrier{paths: paths, stateDir: captured}
		participant.paths.StateDir = stateDir
		participant.paths.RecoveryDir = filepath.Join(stateDir, "recovery")
		barrier.participants = append(barrier.participants, participant)
		for _, path := range []string{stateDir, participant.paths.RecoveryDir} {
			for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
				domain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{Path: path, Access: mutation.AccessShared, Effect: effect})
				if err != nil {
					return MetadataBarrier{}, err
				}
				barrier.domains = append(barrier.domains, domain)
			}
		}
	}
	return barrier, nil
}

func (barrier MetadataBarrier) Domains() []mutation.Domain {
	return slices.Clone(barrier.domains)
}

// Validate retains StateDir identity and ErrStateDirAppeared precedence from
// normal validation, without manufacturing absent participant directories.
func (barrier MetadataBarrier) Validate(ctx context.Context) error {
	if ctx == nil || barrier.budget == nil || len(barrier.participants) == 0 {
		return fmt.Errorf("metadata barrier is not initialized")
	}
	if err := barrier.validateGlobal(ctx); err != nil {
		return err
	}
	for _, participant := range barrier.participants {
		if err := validateLocalBarrierTail(ctx, participant.paths, participant.stateDir, barrier.budget); err != nil {
			if cancellation := ctx.Err(); cancellation != nil {
				return cancellation
			}
			if barrier.mode == metadataBarrierOwnedRecovery {
				return fmt.Errorf("%w: global and local recovery evidence coexist at %q; preserve both evidence sets and the original selection; automatic and ordinary local recovery cannot proceed without deliberate diagnosis: %w", ErrGlobalMetadataFence, participant.paths.StateDir, err)
			}
			return fmt.Errorf("metadata participant %q requires local recovery first: %w", participant.paths.StateDir, err)
		}
	}
	return nil
}

func (barrier MetadataBarrier) validateGlobal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch barrier.mode {
	case metadataBarrierClear:
		return requireGlobalMetadataClear(ctx, barrier.paths, barrier.budget)
	case metadataBarrierOwnedRecovery:
		view, err := fileset.ReadPublicationIntentEvidence5(ctx, barrier.paths.GlobalMetadataDir(), mutationfs.MaximumPhysicalPathDepth, barrier.budget)
		if err != nil {
			return fmt.Errorf("%w: published gen5 global recovery evidence is unavailable: %w", ErrGlobalMetadataFence, err)
		}
		if view.MarkerRevision() != barrier.revision {
			return fmt.Errorf("%w: global metadata marker revision changed: %w", ErrGlobalMetadataFence, fileset.ErrRecoveryIntentMismatch)
		}
		return nil
	default:
		return fmt.Errorf("metadata barrier has no global predicate")
	}
}
