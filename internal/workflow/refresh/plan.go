package refresh

import (
	"context"
	"errors"
	"fmt"

	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// PlanDryRun builds one capability-free immutable refresh plan.
func PlanDryRun(
	ctx context.Context,
	input CommandInput,
	options PlanOptions,
) (result CommandResult, returnErr error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return refusedResult(CommandResult{Mode: ModeDryRun}, ReasonManifestUnavailable, err, "check the selected manifest path")
	}
	root, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		return refusedResult(
			baseResult(paths, ModeDryRun),
			ReasonMutationAuthority,
			err,
			"restore access to the selected manifest directory and retry",
		)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			result.ResultClass = ResultRefused
			result.ReasonCode = ReasonMutationAuthority
			result.Remediation = []string{
				"restore access to the selected manifest directory and retry",
			}
			returnErr = errors.Join(
				returnErr,
				fmt.Errorf("close refresh project-root witness: %w", closeErr),
			)
		}
	}()
	planned, err := planAtPaths(ctx, input, withPlanDefaults(options), paths, ModeDryRun)
	result = cloneCommandResult(planned.result)
	if err != nil {
		return result, err
	}
	planned, err = stabilizePlan(
		ctx,
		input,
		withPlanDefaults(options),
		planned,
		root,
	)
	if err != nil {
		return refusedResult(
			result,
			ReasonStalePlan,
			err,
			"rerun dry-run from the current workspace state",
		)
	}
	planned.result.ResultClass = ResultPlanned
	return cloneCommandResult(planned.result), nil
}

// PlanWrite builds one single-use executable refresh plan and retains its
// selected physical project-root witness.
func PlanWrite(
	ctx context.Context,
	input CommandInput,
	options PlanOptions,
) (*PreparedCommand, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		result, refusal := refusedResult(CommandResult{Mode: ModeExecute}, ReasonManifestUnavailable, err, "check the selected manifest path")
		return unavailablePreparedCommand(result), refusal
	}
	root, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		result, refusal := refusedResult(
			baseResult(paths, ModeExecute),
			ReasonMutationAuthority,
			err,
			"restore access to the selected manifest directory and retry",
		)
		return unavailablePreparedCommand(result), refusal
	}
	planned, planErr := planAtPaths(ctx, input, withPlanDefaults(options), paths, ModeExecute)
	if planErr != nil {
		closeErr := root.Close()
		return unavailablePreparedCommand(planned.result), errors.Join(planErr, closeErr)
	}
	planned, err = stabilizePlan(
		ctx,
		input,
		withPlanDefaults(options),
		planned,
		root,
	)
	if err != nil {
		closeErr := root.Close()
		result, refusal := refusedResult(
			planned.result,
			ReasonStalePlan,
			err,
			"review a new plan from the current workspace state",
		)
		return unavailablePreparedCommand(result), errors.Join(refusal, closeErr)
	}
	return newPreparedCommand(planned, input, withPlanDefaults(options), root), nil
}

func stabilizePlan(
	ctx context.Context,
	input CommandInput,
	options PlanOptions,
	initial plan,
	root *rootedpath.CapturedRoot,
) (plan, error) {
	authority, err := buildAuthorityEvidence(initial, root)
	if err != nil {
		return initial, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, authority.revisions...)
	if err != nil {
		return initial, err
	}
	current, err := planAtPaths(
		ctx,
		input,
		options,
		initial.paths,
		initial.result.Mode,
	)
	if err != nil {
		return current, errors.Join(mutation.StalePlanError{}, err)
	}
	currentAuthority, err := buildAuthorityEvidence(current, root)
	if err != nil {
		return current, err
	}
	if !initial.fingerprint.Equal(current.fingerprint) ||
		!authority.authorityFingerprint.Equal(
			currentAuthority.authorityFingerprint,
		) {
		return current, mutation.StalePlanError{}
	}
	if matches, err := revisions.MatchesCurrent(ctx); err != nil {
		return current, err
	} else if !matches {
		return current, mutation.StalePlanError{}
	}
	current.authority = currentAuthority
	current.revisions = revisions
	return current, nil
}

func planAtPaths(
	ctx context.Context,
	input CommandInput,
	options PlanOptions,
	paths daempaths.Paths,
	mode Mode,
) (plan, error) {
	result := baseResult(paths, mode)
	if ctx == nil {
		return refusedPlan(result, ReasonCancelled, fmt.Errorf("refresh context is required"), "retry the command")
	}
	if err := ctx.Err(); err != nil {
		return refusedPlan(result, ReasonCancelled, err, "retry the command")
	}
	if err := transaction.RequireClearFileSet(ctx, paths.StateDir); err != nil {
		return refusedPlan(
			result,
			ReasonMutationAuthority,
			err,
			"retry the interrupted authoring or unmanage operation before refreshing a carrier",
		)
	}
	if err := journal.EnsureNoActive(paths.RecoveryDir); err != nil {
		return refusedPlan(result, ReasonMutationAuthority, err, "run daem recover before refreshing a carrier")
	}
	environment, err := declarationmanifest.LoadSelected(paths)
	if err != nil {
		return refusedPlan(result, ReasonManifestUnavailable, err, "fix the selected manifest and retry")
	}
	locked, err := lockfile.Load(paths.LockfilePath)
	if err != nil {
		return refusedPlan(result, ReasonLockUnavailable, err, "run daem lock for the selected manifest")
	}
	currentState, err := statefile.LoadOptional(ctx, paths.StatefilePath)
	if err != nil {
		return refusedPlan(result, ReasonMutationAuthority, err, "repair or remove the invalid statefile before retrying")
	}

	selected, err := selectExtension(environment.Extensions(), input)
	if err != nil {
		return refusedPlan(result, ReasonInvalidSelection, err, "run daem list and select one declared extension id")
	}
	result.Selection = Selection{
		ID:      selected.ID().Name(),
		Target:  selected.Target(),
		Scope:   selected.Scope(),
		Carrier: string(selected.Carrier()),
	}
	subject, err := extensiontopology.Relation(selected)
	if err != nil {
		return refusedPlan(result, ReasonInvalidSelection, err, "fix the selected extension declaration")
	}
	contract, found := locked.Locked.Subject(subject)
	if !found {
		return refusedPlan(result, ReasonLockMismatch, fmt.Errorf("selected extension is absent from the lockfile"), "run daem lock for the selected manifest")
	}
	expectedContracts, err := refine.Extensions([]desiredextension.Extension{selected})
	if err != nil || len(expectedContracts) != 1 {
		if err == nil {
			err = fmt.Errorf("selected extension produced %d locked contracts", len(expectedContracts))
		}
		return refusedPlan(result, ReasonLockMismatch, err, "run daem lock for the selected manifest")
	}
	if !contract.Equal(expectedContracts[0]) {
		return refusedPlan(result, ReasonLockMismatch, fmt.Errorf("selected extension lock contract is stale"), "run daem lock and review the refreshed lockfile")
	}
	operationContract, ok := contract.OperationContract(lock.OperationRefresh)
	if !ok {
		return refusedPlan(result, ReasonRefreshUnsupported, fmt.Errorf("selected carrier has no locked refresh operation"), "upgrade daem or use the host directly for this carrier")
	}
	posture, err := refreshPosture(operationContract)
	if err != nil {
		return refusedPlan(result, ReasonRefreshUnsupported, err, "run daem lock with a version that supports this refresh route")
	}
	routeRequest, err := lock.DelegatedOperationRequest(contract, lock.OperationRefresh)
	if err != nil {
		return refusedPlan(result, ReasonLockMismatch, err, "run daem lock and review the refreshed lockfile")
	}
	result.Route = Route{
		Operation:              lock.OperationRefresh,
		RouteID:                routeRequest.RouteID(),
		AdapterContractVersion: routeRequest.ContractVersion(),
		RequestHash:            routeRequest.CanonicalRequestHash(),
		ObservationPosture:     posture,
	}

	var preObservation *observerelation.CorrelationResult
	var authorityPaths []observerelation.AuthorityPath
	if posture == PostureRequireCurrent {
		observation, observeErr := options.Observer(ctx, ObservationRequest{
			Paths:        paths,
			Lockfile:     locked,
			CurrentState: currentState,
			Subject:      subject,
			Target:       selected.Target(),
			Scope:        selected.Scope(),
		})
		if observeErr != nil {
			return refusedPlan(result, ReasonObservationUnavailable, observeErr, "restore passive host inventory access and retry")
		}
		if !observation.Present {
			return refusedPlan(result, ReasonObservationUnavailable, fmt.Errorf("required current relation evidence is unavailable"), "run daem status and restore passive host inventory access")
		}
		observed := observation.Result
		preObservation = &observed
		authorityPaths, err = canonicalObservationAuthorityPaths(observation.AuthorityPaths)
		if err != nil {
			return refusedPlan(result, ReasonObservationUnavailable, err, "report the invalid passive observer authority")
		}
		if len(authorityPaths) == 0 {
			return refusedPlan(result, ReasonObservationUnavailable, fmt.Errorf("required current observation has no authority paths"), "report the incomplete passive observer")
		}
		result.Observation = observationSummary(observed)
		if !assurancehostroute.RelationPostconditionPresent.Accepts(
			observed.State(),
		) {
			reason, remediation := observationRefusal(observed.State())
			return refusedPlan(result, reason, fmt.Errorf("current relation state is %q", observed.State()), remediation)
		}
	}

	command, err := options.CommandBuilder(CommandBuildInput{
		Contract:  contract,
		Operation: lock.OperationRefresh,
		WorkDir:   paths.ManifestRoot,
	})
	if err != nil {
		return refusedPlan(result, ReasonRefreshUnsupported, err, "upgrade daem or use the exact host refresh command directly")
	}
	if command.attempt.WorkDir != paths.ManifestRoot {
		return refusedPlan(result, ReasonRefreshUnsupported, fmt.Errorf("refresh adapter changed the selected working directory"), "report the incompatible refresh adapter")
	}
	result.Disclosure = commandResultDisclosure(command)
	result.Route.ExecutionSubject = command.disclosure.ExecutionSubject()
	result.ResultClass = ResultPlanned
	planned := plan{
		result:            result,
		paths:             paths,
		lockfile:          locked,
		subject:           subject,
		contract:          contract,
		routeRequest:      routeRequest,
		operationContract: operationContract,
		command:           command,
		preObservation:    preObservation,
		authorityPaths:    authorityPaths,
		currentState:      currentState,
	}
	fingerprint, err := refreshFingerprint(planned)
	if err != nil {
		return refusedPlan(result, ReasonRefreshUnsupported, err, "report the invalid refresh plan")
	}
	planned.fingerprint = fingerprint
	return planned, nil
}
