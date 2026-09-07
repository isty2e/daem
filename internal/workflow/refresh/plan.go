package refresh

import (
	"context"
	"errors"
	"fmt"

	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	daempaths "github.com/isty2e/daem/internal/paths"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/recoverygate"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// PlanDryRun builds one capability-free immutable refresh plan.
func PlanDryRun(
	ctx context.Context,
	input CommandInput,
	options PlanOptions,
) (result CommandResult, returnErr error) {
	timeout, err := normalizeHostCommandTimeout(input.Timeout)
	if err != nil {
		return refusedResult(
			CommandResult{Mode: ModeDryRun},
			ReasonInvalidTimeout,
			err,
			"select a whole-second timeout between 1s and 1h",
		)
	}
	input.Timeout = timeout.Duration()
	if ctx == nil {
		return refusedResult(CommandResult{Mode: ModeDryRun}, ReasonCancelled, fmt.Errorf("refresh context is required"), "retry the command")
	}
	if err := ctx.Err(); err != nil {
		return refusedResult(CommandResult{Mode: ModeDryRun}, ReasonCancelled, err, "retry the command")
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return refusedResult(CommandResult{Mode: ModeDryRun}, ReasonManifestUnavailable, err, "check the selected manifest path")
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		planned, refusal := journalAndFileSetRefusal(baseResult(paths, ModeDryRun), err)
		return planned.result, refusal
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
	planned, err := planAtPathsWithBarrier(
		ctx,
		input,
		timeout,
		withPlanDefaults(options),
		paths,
		ModeDryRun,
		&barrier,
	)
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
		if result, mapped, ok := mapPreservedReplanCause(result, err); ok {
			return result, mapped
		}
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
	timeout, err := normalizeHostCommandTimeout(input.Timeout)
	if err != nil {
		result, refusal := refusedResult(
			CommandResult{Mode: ModeExecute},
			ReasonInvalidTimeout,
			err,
			"select a whole-second timeout between 1s and 1h",
		)
		return unavailablePreparedCommand(result), refusal
	}
	input.Timeout = timeout.Duration()
	if ctx == nil {
		result, refusal := refusedResult(CommandResult{Mode: ModeExecute}, ReasonCancelled, fmt.Errorf("refresh context is required"), "retry the command")
		return unavailablePreparedCommand(result), refusal
	}
	if err := ctx.Err(); err != nil {
		result, refusal := refusedResult(CommandResult{Mode: ModeExecute}, ReasonCancelled, err, "retry the command")
		return unavailablePreparedCommand(result), refusal
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		result, refusal := refusedResult(CommandResult{Mode: ModeExecute}, ReasonManifestUnavailable, err, "check the selected manifest path")
		return unavailablePreparedCommand(result), refusal
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		planned, refusal := journalAndFileSetRefusal(baseResult(paths, ModeExecute), err)
		return unavailablePreparedCommand(planned.result), refusal
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
	planned, planErr := planAtPathsWithBarrier(
		ctx,
		input,
		timeout,
		withPlanDefaults(options),
		paths,
		ModeExecute,
		&barrier,
	)
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
		if result, mapped, ok := mapPreservedReplanCause(planned.result, err); ok {
			return unavailablePreparedCommand(result), errors.Join(mapped, closeErr)
		}
		result, refusal := refusedResult(
			planned.result,
			ReasonStalePlan,
			err,
			"review a new plan from the current workspace state",
		)
		return unavailablePreparedCommand(result), errors.Join(refusal, closeErr)
	}
	return newPreparedCommand(
		planned,
		input,
		withPlanDefaults(options),
		root,
	), nil
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
	current, err := planAtPathsWithBarrier(
		ctx,
		input,
		initial.timeout,
		options,
		initial.paths,
		initial.result.Mode,
		&initial.barrier,
	)
	if err != nil {
		if isPreservedReplanCause(err) {
			return current, err
		}
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
	timeout HostCommandTimeout,
	options PlanOptions,
	paths daempaths.Paths,
	mode Mode,
) (plan, error) {
	return planAtPathsWithBarrier(ctx, input, timeout, options, paths, mode, nil)
}

func planAtPathsWithBarrier(
	ctx context.Context,
	input CommandInput,
	timeout HostCommandTimeout,
	options PlanOptions,
	paths daempaths.Paths,
	mode Mode,
	barrier *recoverygate.EffectAuthority,
) (plan, error) {
	result := baseResult(paths, mode)
	if ctx == nil {
		return refusedPlan(result, ReasonCancelled, fmt.Errorf("refresh context is required"), "retry the command")
	}
	if err := ctx.Err(); err != nil {
		return refusedPlan(result, ReasonCancelled, err, "retry the command")
	}
	if barrier != nil {
		if err := barrier.Validate(ctx); err != nil {
			return journalAndFileSetRefusal(result, err)
		}
	} else if err := recoverygate.RequireClear(ctx, paths); err != nil {
		return journalAndFileSetRefusal(result, err)
	}
	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		return refusedPlan(result, ReasonManifestUnavailable, err, "fix the selected manifest and retry")
	}
	locked, err := lockfile.Load(ctx, paths.LockfilePath)
	if err != nil {
		return refusedPlan(result, ReasonLockUnavailable, err, "run daem lock for the selected manifest")
	}
	if err := refine.ValidateCurrentExtensionOrder(
		environment.Extensions(),
		locked,
		aggregatecodec.ExtensionOrderIdentityResolver(paths),
	); err != nil {
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
	result.Disclosure = commandResultDisclosure(command, timeout)
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
		timeout:           timeout,
	}
	if barrier != nil {
		planned.barrier = *barrier
	}
	fingerprint, err := refreshFingerprint(planned)
	if err != nil {
		return refusedPlan(result, ReasonRefreshUnsupported, err, "report the invalid refresh plan")
	}
	planned.fingerprint = fingerprint
	return planned, nil
}

func journalAndFileSetRefusal(result CommandResult, err error) (plan, error) {
	if recoverygate.IsCancellation(err) {
		return refusedPlan(result, ReasonCancelled, err, "retry the command")
	}
	state := recoverygate.StateOf(err)
	switch state.FileSet() {
	case fileset.FileSetFenceAccessUnprovable:
		return refusedPlan(
			result,
			ReasonFileSetAccessUnprovable,
			err,
			"restore StateDir access and identity before retrying or running daem recover",
		)
	case fileset.FileSetFenceInvalidEvidence:
		return refusedPlan(
			result,
			ReasonFileSetEvidenceInvalid,
			err,
			"preserve and repair the invalid file-set evidence before retrying or running daem recover",
		)
	}
	if state.JournalKnown() && state.Journal() == journal.InterruptionActiveApply && state.HasContinuingFileSetFence() {
		return refusedPlan(
			result,
			ReasonInterruptedApplyFileSetFence,
			err,
			"run daem recover --dry-run first; the file-set fence remains after recover and is not cleared by it",
		)
	}
	if state.JournalKnown() && state.Journal() == journal.InterruptionCleanupOnly && state.HasContinuingFileSetFence() {
		return refusedPlan(
			result,
			ReasonJournalCleanupFileSetFence,
			err,
			"run daem recover --dry-run to finish journal cleanup; the file-set fence remains afterward",
		)
	}
	if state.JournalKnown() {
		switch state.Journal() {
		case journal.InterruptionActiveApply:
			return refusedPlan(result, ReasonInterruptedApply, err, "run daem recover before refreshing a carrier")
		case journal.InterruptionCleanupOnly:
			return refusedPlan(result, ReasonJournalCleanupIncomplete, err, "run daem recover to finish journal cleanup")
		}
	}
	if state.FileSetKnown() {
		switch state.FileSet() {
		case fileset.FileSetFencePublishedTransaction:
			return refusedPlan(
				result,
				ReasonInterruptedFileSetTransaction,
				err,
				"retry the interrupted authoring or unmanage operation before refreshing a carrier",
			)
		case fileset.FileSetFenceAbandonedResidue:
			return refusedPlan(
				result,
				ReasonAbandonedFileSetResidue,
				err,
				"preserve the reported residue for analysis; do not delete it from its name prefix; current daem cannot recover markerless file-set residue",
			)
		case fileset.FileSetFenceCensusLimit:
			return refusedPlan(
				result,
				ReasonFileSetFenceCensusLimit,
				err,
				"reduce or inspect StateDir entries so the bounded file-set census can prove the fence clean",
			)
		}
	}
	return refusedPlan(
		result,
		ReasonMutationAuthority,
		err,
		"restore the reported workspace mutation authority before refreshing a carrier",
	)
}

func isPreservedReplanCause(err error) bool {
	if recoverygate.IsCancellation(err) {
		return true
	}
	state := recoverygate.StateOf(err)
	return state.Observed()
}

func mapPreservedReplanCause(result CommandResult, err error) (CommandResult, error, bool) {
	if !isPreservedReplanCause(err) {
		return CommandResult{}, nil, false
	}
	planned, mapped := journalAndFileSetRefusal(result, err)
	return cloneCommandResult(planned.result), mapped, true
}
