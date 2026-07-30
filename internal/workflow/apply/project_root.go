package apply

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

type projectRootFingerprintFacts struct {
	PhysicalRoot      string
	ObjectFingerprint string
	MountFingerprint  string
}

func requiresProjectRootAuthority(planned commandPlan) bool {
	for _, decision := range planned.assessment.Reconciliation.MutatingManagedPaths() {
		if decision.InvolvesScope(target.ScopeProject) {
			return true
		}
	}
	for _, decision := range planned.assessment.Reconciliation.MutatingAggregates() {
		if decision.DocumentAddress().Scope() == target.ScopeProject {
			return true
		}
	}
	for _, action := range planned.assessment.Reconciliation.Relations() {
		if action.InvokesHostRoute() ||
			isGlobalCarrierPromotionCandidate(planned.assessment.CurrentState, action) {
			return true
		}
	}
	for _, prerequisite := range planned.assessment.MCPProviders {
		if prerequisite.State() == readiness.MCPProviderInstallRequired {
			return true
		}
	}
	for _, action := range planned.assessment.Reconciliation.CarrierAbsences() {
		if _, present := action.HostRouteRequest(); present ||
			action.MutatesDirectProjection() ||
			action.VerifiesPendingRemoval() {
			return true
		}
	}
	for _, decision := range planned.assessment.Reconciliation.RelationOrders() {
		if decision.Scope() == target.ScopeProject {
			return true
		}
	}
	for _, action := range planned.result.Reconciliation.Delegates() {
		if action.SchedulesAttempt() && action.Scope() == target.ScopeProject {
			return true
		}
	}
	return false
}

func captureProjectRootAuthorityBeforeLoad(paths daempaths.Paths) (*rootedpath.CapturedRoot, error) {
	root, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		return nil, fmt.Errorf("capture apply project root before load: %w", err)
	}
	return root, nil
}

func retainProjectRootAuthority(
	planned *commandPlan,
	root *rootedpath.CapturedRoot,
	captureErr error,
) error {
	if planned == nil {
		return fmt.Errorf("apply command plan is required")
	}
	if !requiresProjectRootAuthority(*planned) {
		return nil
	}
	if captureErr != nil {
		return captureErr
	}
	if root == nil {
		return rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			planned.context.Paths.ManifestRoot,
			"apply project-root witness was not captured before load",
			nil,
		)
	}
	if err := root.ValidateSelection(planned.context.Paths.ManifestRoot); err != nil {
		return err
	}
	planned.projectRoot = root
	return nil
}

func projectRootFingerprint(planned commandPlan) (*projectRootFingerprintFacts, error) {
	required := requiresProjectRootAuthority(planned)
	if planned.projectRoot == nil {
		if !required {
			return nil, nil
		}
		return nil, rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			planned.context.Paths.ManifestRoot,
			"apply project-root witness is required",
			nil,
		)
	}
	if err := planned.projectRoot.ValidateSelection(planned.context.Paths.ManifestRoot); err != nil {
		return nil, err
	}
	authority, err := planned.projectRoot.Authority()
	if err != nil {
		return nil, err
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return nil, err
	}
	return &projectRootFingerprintFacts{
		PhysicalRoot:      provenance.PhysicalRoot(),
		ObjectFingerprint: provenance.ObjectFingerprint(),
		MountFingerprint:  provenance.MountFingerprint(),
	}, nil
}

func validateHostRouteProjectRoot(options runOptions, selectedRoot string) error {
	if options.projectRoot == nil {
		return rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			selectedRoot,
			"delegated route requires retained project-root authority",
			nil,
		)
	}
	return options.projectRoot.ValidateSelection(selectedRoot)
}

func acquireHostRouteWorkingDirectory(
	options runOptions,
	selectedRoot string,
) (subprocess.WorkingDirectoryBinding, error) {
	if err := validateHostRouteProjectRoot(options, selectedRoot); err != nil {
		return nil, err
	}
	return options.projectRoot.AcquireSelectedWorkingDirectory(selectedRoot)
}

func projectDestinationAuthorityPathFor(
	planned commandPlan,
	scope target.Scope,
	destinationValue output.Destination,
) (string, error) {
	switch scope {
	case target.ScopeGlobal:
		return destinationResolver(planned.context.Paths).Resolve(destinationValue)
	case target.ScopeProject:
		// Continue with the retained project authority below.
	default:
		_, err := target.ParseScope(string(scope))
		return "", fmt.Errorf("resolve apply authority destination %q: %w", destinationValue, err)
	}
	if planned.projectRoot == nil {
		return "", rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			planned.context.Paths.ManifestRoot,
			"project destination requires retained root authority",
			nil,
		)
	}
	authority, err := planned.projectRoot.Authority()
	if err != nil {
		return "", err
	}
	relative, err := rootedpath.NewRelativeDestination(destinationValue.RelativePath())
	if err != nil {
		return "", err
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return "", err
	}
	return destination.LexicalPath()
}

func observeProjectDestinationAuthorityFor(
	ctx context.Context,
	planned commandPlan,
	scope target.Scope,
	destinationValue output.Destination,
) error {
	if scope != target.ScopeProject {
		return nil
	}
	if planned.projectRoot == nil {
		return rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			planned.context.Paths.ManifestRoot,
			"project destination requires retained root authority",
			nil,
		)
	}
	authority, err := planned.projectRoot.Authority()
	if err != nil {
		return err
	}
	relative, err := rootedpath.NewRelativeDestination(destinationValue.RelativePath())
	if err != nil {
		return err
	}
	destination, err := authority.Bind(relative)
	if err != nil {
		return err
	}
	capability, err := planned.projectRoot.Acquire(destination)
	if err != nil {
		return err
	}
	_, inspectErr := storagecommit.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if errors.Is(inspectErr, fs.ErrNotExist) {
		inspectErr = nil
	}
	return errors.Join(inspectErr, closeErr)
}
