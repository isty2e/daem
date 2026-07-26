package hostroute

import (
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	reconciliation "github.com/isty2e/daem/internal/reconcile"
)

// BuildCommand converts one admitted locked host-route action into a structured
// command attempt. It performs no host I/O and makes no convergence claims.
func BuildCommand(input BuildInput) (Command, error) {
	action := input.Action
	subjectID := action.Subject()
	if (action.Kind() != reconciliation.ActionCreate && action.Kind() != reconciliation.ActionAttempt) ||
		!action.InvokesHostRoute() {
		return Command{}, newValidationError(
			ReasonUnsupportedAction,
			subjectID,
			"action kind=%q execution=%q reason=%q selected_outcome=%q cannot invoke a host route",
			action.Kind(),
			action.Execution(),
			action.Reason(),
			action.RouteAdmission().SelectedOutcome(),
		)
	}
	if strings.TrimSpace(input.WorkDir) == "" {
		return Command{}, newValidationError(
			ReasonMissingWorkDir,
			subjectID,
			"host route requires caller-provided workdir",
		)
	}
	contract, found := input.Lockfile.Locked.Subject(subjectID)
	if !found {
		return Command{}, newValidationError(
			ReasonLockedSubjectMissing,
			subjectID,
			"locked subject for route action is absent",
		)
	}
	command, err := BuildOperationCommand(OperationBuildInput{
		Contract:  contract,
		Operation: lock.OperationInstall,
		WorkDir:   input.WorkDir,
	})
	if err != nil {
		return Command{}, err
	}
	realization, ok := contract.Realization()
	if !ok {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked subject does not carry a realization",
		)
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			contract.SubjectID(),
			"locked subject does not carry a delegated relation realization",
		)
	}
	routeRequest := command.RouteRequest()
	if !routeRequest.Equal(action.RouteRequest()) {
		return Command{}, newValidationError(
			ReasonRouteRequestMismatch,
			contract.SubjectID(),
			"action route request does not match locked route request",
		)
	}
	if relation.Target() != action.Target() {
		return Command{}, newValidationError(
			ReasonTargetMismatch,
			contract.SubjectID(),
			"action target %q does not match locked target %q",
			action.Target(),
			relation.Target(),
		)
	}
	if relation.Scope() != action.Scope() {
		return Command{}, newValidationError(
			ReasonScopeMismatch,
			contract.SubjectID(),
			"action scope %q does not match locked scope %q",
			action.Scope(),
			relation.Scope(),
		)
	}
	if string(relation.ExpectedRelation().SubjectKey()) != action.RelationSubjectKey() {
		return Command{}, newValidationError(
			ReasonRelationKeyMismatch,
			contract.SubjectID(),
			"action relation subject key does not match locked relation subject key",
		)
	}
	return command, nil
}

// BuildOperationCommand converts one exact locked delegated carrier operation
// into a structured command attempt. It performs no host I/O and grants no
// execution authority.
func BuildOperationCommand(input OperationBuildInput) (Command, error) {
	contract := input.Contract
	subjectID := contract.SubjectID()
	if strings.TrimSpace(input.WorkDir) == "" {
		return Command{}, newValidationError(
			ReasonMissingWorkDir,
			subjectID,
			"host route requires caller-provided workdir",
		)
	}
	realization, ok := contract.Realization()
	if !ok {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked subject does not carry a realization",
		)
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked subject does not carry a delegated relation realization",
		)
	}
	carrier, admitted, err := lock.DelegatedRelationCarrier(contract)
	if err != nil {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked delegated relation does not match its admitted carrier contract: %v",
			err,
		)
	}
	if !admitted {
		return Command{}, newValidationError(
			ReasonUnsupportedRoute,
			subjectID,
			"locked subject is not an admitted delegated carrier contract",
		)
	}
	routeRequest, err := lock.DelegatedOperationRequest(contract, input.Operation)
	if err != nil {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked delegated operation request is invalid: %v",
			err,
		)
	}
	operationContract, ok := contract.OperationContract(input.Operation)
	if !ok {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked delegated operation %q is missing",
			input.Operation,
		)
	}
	adapter, ok := commandAdapterForRoute(carrier, input.Operation, operationContract.Route())
	if !ok {
		return Command{}, newValidationError(
			ReasonUnsupportedRoute,
			subjectID,
			"locked subject has no admitted %q host route command adapter",
			input.Operation,
		)
	}
	source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
	if err != nil {
		return Command{}, newValidationError(
			ReasonInvalidLockedRecord,
			subjectID,
			"locked delegated relation source is invalid: %v",
			err,
		)
	}
	adapterInput := commandAdapterInput{
		subject: subjectID,
		scope:   relation.Scope(),
		source:  source,
		workDir: input.WorkDir,
	}
	attempt, err := adapter.buildAttempt(operationContract, adapterInput)
	if err != nil {
		return Command{}, err
	}
	var disclosure Disclosure
	hasDisclosure := adapter.disclose != nil
	if hasDisclosure {
		disclosure, err = adapter.disclose(adapterInput)
		if err != nil {
			return Command{}, err
		}
	}
	return Command{
		subject:       subjectID,
		routeRequest:  routeRequest,
		attempt:       attempt,
		disclosure:    disclosure,
		hasDisclosure: hasDisclosure,
	}, nil
}
