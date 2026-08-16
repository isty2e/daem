package apply

import (
	"context"
	"errors"
	"fmt"
	"os"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	outputmodel "github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	artifactaccess "github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const (
	// MaximumDryRunDiffInputBytes bounds the retained current and desired bytes
	// for one managed-file diff.
	MaximumDryRunDiffInputBytes     int64 = 4 << 20
	maximumDryRunDiffOperationBytes int64 = 16 << 20
	maximumDryRunDiffDecisions            = 4_096
)

// DryRunDiffOmissionReason identifies why one planned file has no retained
// inline-diff payload.
type DryRunDiffOmissionReason string

const (
	DryRunDiffOmittedInputLimit     DryRunDiffOmissionReason = "input_limit_exceeded"
	DryRunDiffOmittedOperationLimit DryRunDiffOmissionReason = "operation_limit_exceeded"
)

type DryRunDiff struct {
	EntityID       entity.ID
	Targets        []target.Target
	Scope          target.Scope
	Destination    string
	CurrentLabel   string
	CurrentContent []byte
	DesiredLabel   string
	DesiredContent []byte
	OmissionReason DryRunDiffOmissionReason
}

// DryRunDiffCollection is the bounded collection result for one optional dry-run
// diff request. UninspectedManagedPathCount records canonical decisions that
// were not visited after the operation item budget was exhausted.
type DryRunDiffCollection struct {
	Diffs                       []DryRunDiff
	UninspectedManagedPathCount int
}

func BuildDryRunDiffs(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	sourceEpoch lockobserve.SourceEpoch,
	planResult reconcile.Result,
	projectRoot *rootedpath.CapturedRoot,
) (result DryRunDiffCollection, resultErr error) {
	managedPaths, uninspected, err := planResult.ManagedPathsUpTo(
		maximumDryRunDiffDecisions,
	)
	if err != nil {
		return DryRunDiffCollection{}, err
	}
	decisions, err := diffableManagedFileDecisions(
		ctx,
		managedPaths,
	)
	if err != nil {
		return DryRunDiffCollection{}, err
	}
	diffs := make([]DryRunDiff, 0, len(decisions))
	result = DryRunDiffCollection{
		Diffs:                       diffs,
		UninspectedManagedPathCount: uninspected,
	}
	if len(decisions) == 0 {
		return result, nil
	}
	if projectRoot == nil && managedFileDiffNeedsProjectRoot(decisions) {
		capturedRoot, captureErr := rootedpath.CaptureRoot(paths.ManifestRoot)
		if captureErr != nil {
			return DryRunDiffCollection{}, fmt.Errorf("capture diff project root: %w", captureErr)
		}
		projectRoot = capturedRoot
		defer func() {
			resultErr = errors.Join(resultErr, projectRoot.Close())
		}()
	}
	resolver := destinationResolver(paths).Resolve
	budget := dryRunDiffCollectionBudget{remainingBytes: maximumDryRunDiffOperationBytes}

	for _, decision := range decisions {
		if err := ctx.Err(); err != nil {
			return DryRunDiffCollection{}, err
		}
		entityID, ok := topologyprojection.EntityID(decision.Subject())
		if !ok {
			return DryRunDiffCollection{}, fmt.Errorf("managed file diff subject %q has no entity", decision.Subject())
		}
		consumers := decision.ConsumerTargets()
		if len(consumers) == 0 {
			return DryRunDiffCollection{}, fmt.Errorf("managed file diff subject %q has no selected consumer", decision.Subject())
		}

		diff := DryRunDiff{
			EntityID:     entityID,
			Targets:      append([]target.Target(nil), consumers...),
			Scope:        decision.Scope(),
			Destination:  decision.Destination().String(),
			CurrentLabel: "/dev/null",
			DesiredLabel: "desired/" + decision.Destination().String(),
		}
		if budget.remainingBytes == 0 {
			diff.OmissionReason = DryRunDiffOmittedOperationLimit
			diffs = append(diffs, diff)
			continue
		}

		currentContent := []byte(nil)
		if decision.Kind() == reconcile.ManagedPathReplace {
			currentLimit := min(MaximumDryRunDiffInputBytes, budget.remainingBytes)
			var omitted bool
			var readErr error
			currentContent, omitted, readErr = readManagedFileForDiff(
				ctx,
				projectRoot,
				decision.Scope(),
				decision.Destination(),
				decision.LiveHash(),
				resolver,
				currentLimit,
			)
			if readErr != nil {
				return DryRunDiffCollection{}, fmt.Errorf("read current destination %q: %w", decision.Destination(), readErr)
			}
			if omitted {
				budget.consume(currentLimit)
				diff.OmissionReason = dryRunDiffLimitReason(
					currentLimit,
					MaximumDryRunDiffInputBytes,
				)
				diffs = append(diffs, diff)
				continue
			}
			diff.CurrentLabel = "current/" + decision.Destination().String()
			budget.consume(int64(len(currentContent)))
		}

		pairRemaining := MaximumDryRunDiffInputBytes - int64(len(currentContent))
		if budget.remainingBytes == 0 {
			diff.OmissionReason = DryRunDiffOmittedOperationLimit
			diffs = append(diffs, diff)
			continue
		}
		if pairRemaining == 0 {
			diff.OmissionReason = DryRunDiffOmittedInputLimit
			diffs = append(diffs, diff)
			continue
		}
		desiredLimit := min(pairRemaining, budget.remainingBytes)
		desiredContent, omitted, err := readDesiredFileForDiff(
			ctx,
			locked,
			sourceEpoch,
			entityID,
			decision,
			desiredLimit,
		)
		if err != nil {
			return DryRunDiffCollection{}, fmt.Errorf("read desired destination %q: %w", decision.Destination(), err)
		}
		if omitted {
			budget.consume(desiredLimit)
			diff.OmissionReason = dryRunDiffLimitReason(desiredLimit, pairRemaining)
			diffs = append(diffs, diff)
			continue
		}
		budget.consume(int64(len(desiredContent)))
		diff.CurrentContent = currentContent
		diff.DesiredContent = desiredContent
		diffs = append(diffs, diff)
	}

	result.Diffs = diffs
	return result, nil
}

func readManagedFileForDiff(
	ctx context.Context,
	projectRoot *rootedpath.CapturedRoot,
	scope target.Scope,
	destination outputmodel.Destination,
	expectedHash artifact.ContentHash,
	resolver func(outputmodel.Destination) (string, error),
	maximumBytes int64,
) (content []byte, omitted bool, resultErr error) {
	if maximumBytes <= 0 {
		return nil, true, nil
	}
	var root *rootedpath.CapturedRoot
	var bound rootedpath.Destination
	var err error
	switch scope {
	case target.ScopeProject:
		if projectRoot == nil {
			return nil, false, fmt.Errorf("project root authority is required")
		}
		authority, authorityErr := projectRoot.Authority()
		if authorityErr != nil {
			return nil, false, authorityErr
		}
		relative, relativeErr := rootedpath.NewRelativeDestination(destination.RelativePath())
		if relativeErr != nil {
			return nil, false, relativeErr
		}
		bound, err = authority.Bind(relative)
		root = projectRoot
	case target.ScopeGlobal:
		hostPath, resolveErr := resolver(destination)
		if resolveErr != nil {
			return nil, false, resolveErr
		}
		root, bound, err = rootedpath.CaptureDestination(hostPath)
		if err == nil {
			defer func() {
				resultErr = errors.Join(resultErr, root.Close())
			}()
		}
	default:
		return nil, false, fmt.Errorf("unsupported scope %q", scope)
	}
	if err != nil {
		return nil, false, err
	}
	capability, err := root.Acquire(bound)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, capability.Close())
	}()
	var mode os.FileMode
	content, mode, _, err = storagecommit.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumBytes,
	)
	if err != nil {
		if errors.Is(err, storagecommit.ErrRegularFileReadLimitExceeded) {
			return nil, true, nil
		}
		return nil, false, err
	}
	if observed := artifact.HashFileContentWithExecutable(
		content,
		mode.Perm()&0o111 != 0,
	); observed != expectedHash {
		return nil, false, fmt.Errorf("content changed after planning")
	}
	return content, false, nil
}

func readDesiredFileForDiff(
	ctx context.Context,
	locked lock.File,
	sourceEpoch lockobserve.SourceEpoch,
	entityID entity.ID,
	decision reconcile.ManagedPathDecision,
	maximumBytes int64,
) ([]byte, bool, error) {
	resolution, err := sourceEpoch.FileResolution(entityID)
	if err != nil {
		return nil, false, err
	}
	lockedContract, ok := locked.Locked.ExactSupplySubject(entityID)
	if !ok {
		return nil, false, fmt.Errorf("managed file diff entity %q has no exact Supply", entityID)
	}
	lockedIdentity, ok := lockedContract.ExactSupply()
	if !ok || !lockedIdentity.Equal(resolution.Identity()) {
		return nil, false, fmt.Errorf("managed file diff source identity does not match lockfile entry")
	}
	readLimit := maximumBytes
	if readLimit < 1 {
		readLimit = 1
	}
	content, err := resolution.View().ReadRootFileVerified(
		ctx,
		resolution.Identity(),
		readLimit,
	)
	if err != nil {
		var limitErr *artifactaccess.LimitError
		if errors.As(err, &limitErr) {
			return nil, true, nil
		}
		return nil, false, err
	}
	bytes := content.Bytes()
	if int64(len(bytes)) > maximumBytes {
		return nil, true, nil
	}
	observedHash := artifact.HashFileContentWithExecutable(
		bytes,
		decision.DesiredFileMode().Perm()&0o111 != 0,
	)
	if observedHash != decision.DesiredHash() {
		return nil, false, fmt.Errorf("desired content changed after planning")
	}
	return bytes, false, nil
}

type dryRunDiffCollectionBudget struct {
	remainingBytes int64
}

func (budget *dryRunDiffCollectionBudget) consume(bytes int64) {
	budget.remainingBytes -= bytes
}

func dryRunDiffLimitReason(
	admittedBytes int64,
	perDiffRemaining int64,
) DryRunDiffOmissionReason {
	if admittedBytes < perDiffRemaining {
		return DryRunDiffOmittedOperationLimit
	}
	return DryRunDiffOmittedInputLimit
}

func diffableManagedFileDecisions(
	ctx context.Context,
	managedPaths []reconcile.ManagedPathDecision,
) ([]reconcile.ManagedPathDecision, error) {
	decisions := make(
		[]reconcile.ManagedPathDecision,
		0,
		len(managedPaths),
	)
	for _, decision := range managedPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if decision.Kind() != reconcile.ManagedPathCreate && decision.Kind() != reconcile.ManagedPathReplace {
			continue
		}
		if decision.ContentKind() != realization.PathProjectionFile ||
			decision.PlacementMode() != realization.PathProjectionCopy {
			continue
		}

		decisions = append(decisions, decision)
	}

	return decisions, nil
}

func managedFileDiffNeedsProjectRoot(decisions []reconcile.ManagedPathDecision) bool {
	for _, decision := range decisions {
		if decision.Kind() == reconcile.ManagedPathReplace && decision.Scope() == target.ScopeProject {
			return true
		}
	}
	return false
}
