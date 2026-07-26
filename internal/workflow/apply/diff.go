package apply

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	payloadbuild "github.com/isty2e/daem/internal/effect/payload/build"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	outputmodel "github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
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
}

func BuildDryRunDiffs(
	ctx context.Context,
	paths daempaths.Paths,
	environment desired.Environment,
	locked lock.File,
	selection targetselection.Selection,
	planResult reconcile.Result,
	projectRoot *rootedpath.CapturedRoot,
) (result []DryRunDiff, resultErr error) {
	decisions := diffableManagedFileDecisions(planResult)
	diffs := make([]DryRunDiff, 0, len(decisions))
	if len(decisions) == 0 {
		return diffs, nil
	}
	if projectRoot == nil && managedFileDiffNeedsProjectRoot(decisions) {
		capturedRoot, captureErr := rootedpath.CaptureRoot(paths.ManifestRoot)
		if captureErr != nil {
			return nil, fmt.Errorf("capture diff project root: %w", captureErr)
		}
		projectRoot = capturedRoot
		defer func() {
			resultErr = errors.Join(resultErr, projectRoot.Close())
		}()
	}
	subjects := make([]topology.SubjectID, 0, len(decisions))
	for _, decision := range decisions {
		subjects = append(subjects, decision.Subject())
	}

	payloads, err := payloadbuild.ManagedPathPayloadSet(ctx, payloadbuild.Input{
		Paths:                      paths,
		Environment:                environment,
		Lockfile:                   locked,
		Selection:                  selection,
		ManagedPathPayloadSubjects: subjects,
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		if cleanupErr := payloads.Cleanup(); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release diff payloads: %w", cleanupErr))
		}
	}()
	resolver := hostpath.NewResolver(paths.ManifestRoot).Resolve

	for _, decision := range decisions {
		entityID, ok := topologyprojection.EntityID(decision.Subject())
		if !ok {
			return nil, fmt.Errorf("managed file diff subject %q has no entity", decision.Subject())
		}
		payload, ok := payloads.LookupSubject(decision.Subject())
		if !ok {
			return nil, fmt.Errorf("missing managed file payload for subject %q", decision.Subject())
		}
		if err := payload.VerifyHash(decision.DesiredHash(), decision.Destination()); err != nil {
			return nil, err
		}
		file, isFile := payload.File()
		if !isFile {
			return nil, fmt.Errorf("managed file diff subject %q has a non-file payload", decision.Subject())
		}
		consumers := decision.ConsumerTargets()
		if len(consumers) == 0 {
			return nil, fmt.Errorf("managed file diff subject %q has no selected consumer", decision.Subject())
		}

		currentContent := []byte(nil)
		currentLabel := "/dev/null"
		if decision.Kind() == reconcile.ManagedPathReplace {
			currentContent, err = readManagedFileForDiff(
				ctx,
				projectRoot,
				decision.Scope(),
				decision.Destination(),
				decision.LiveHash(),
				resolver,
			)
			if err != nil {
				return nil, fmt.Errorf("read current destination %q: %w", decision.Destination(), err)
			}
			currentLabel = "current/" + string(decision.Destination())
		}

		diffs = append(diffs, DryRunDiff{
			EntityID:       entityID,
			Targets:        append([]target.Target(nil), consumers...),
			Scope:          decision.Scope(),
			Destination:    string(decision.Destination()),
			CurrentLabel:   currentLabel,
			CurrentContent: currentContent,
			DesiredLabel:   "desired/" + string(decision.Destination()),
			DesiredContent: file.Bytes(),
		})
	}

	return diffs, nil
}

func readManagedFileForDiff(
	ctx context.Context,
	projectRoot *rootedpath.CapturedRoot,
	scope target.Scope,
	destination outputmodel.Destination,
	expectedHash artifact.ContentHash,
	resolver func(outputmodel.Destination) (string, error),
) (content []byte, resultErr error) {
	var root *rootedpath.CapturedRoot
	var bound rootedpath.Destination
	var err error
	switch scope {
	case target.ScopeProject:
		if projectRoot == nil {
			return nil, fmt.Errorf("project root authority is required")
		}
		authority, authorityErr := projectRoot.Authority()
		if authorityErr != nil {
			return nil, authorityErr
		}
		relative, relativeErr := rootedpath.NewRelativeDestination(string(destination))
		if relativeErr != nil {
			return nil, relativeErr
		}
		bound, err = authority.Bind(relative)
		root = projectRoot
	case target.ScopeGlobal:
		hostPath, resolveErr := resolver(destination)
		if resolveErr != nil {
			return nil, resolveErr
		}
		root, bound, err = rootedpath.CaptureDestination(hostPath)
		if err == nil {
			defer func() {
				resultErr = errors.Join(resultErr, root.Close())
			}()
		}
	default:
		return nil, fmt.Errorf("unsupported scope %q", scope)
	}
	if err != nil {
		return nil, err
	}
	capability, err := root.Acquire(bound)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, capability.Close())
	}()
	var mode os.FileMode
	content, mode, _, err = storagecommit.ReadRootedRegularFile(ctx, capability)
	if err != nil {
		return nil, err
	}
	if observed := artifact.HashFileContentWithExecutable(
		content,
		mode.Perm()&0o111 != 0,
	); observed != expectedHash {
		return nil, fmt.Errorf("content changed after planning")
	}
	return content, nil
}

func diffableManagedFileDecisions(planResult reconcile.Result) []reconcile.ManagedPathDecision {
	managedPaths := planResult.ManagedPaths()
	decisions := make([]reconcile.ManagedPathDecision, 0, len(managedPaths))
	for _, decision := range managedPaths {
		if decision.Kind() != reconcile.ManagedPathCreate && decision.Kind() != reconcile.ManagedPathReplace {
			continue
		}
		if decision.ContentKind() != realization.PathProjectionFile ||
			decision.PlacementMode() != realization.PathProjectionCopy {
			continue
		}

		decisions = append(decisions, decision)
	}

	return decisions
}

func managedFileDiffNeedsProjectRoot(decisions []reconcile.ManagedPathDecision) bool {
	for _, decision := range decisions {
		if decision.Kind() == reconcile.ManagedPathReplace && decision.Scope() == target.ScopeProject {
			return true
		}
	}
	return false
}
