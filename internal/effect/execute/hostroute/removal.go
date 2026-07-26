package hostroute

import (
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// RemovalRequest is the immutable operation identity passed to one
// host-specific removal adapter after generic carrier-absence validation.
type RemovalRequest struct {
	subject      topology.SubjectID
	target       target.Target
	scope        target.Scope
	carrier      desiredextension.Carrier
	source       desiredextension.SourceRef
	operation    lock.OperationContract
	routeRequest realizationdelegate.Request
	workDir      string
}

// RemovalAdapter lowers one admitted removal request to one structured command
// attempt without performing host I/O.
type RemovalAdapter func(RemovalRequest) (subprocess.CommandAttemptRequest, error)

// RemovalBuildInput carries one already-planned removal action into boundary
// request construction. It grants no authority to execute the returned command.
type RemovalBuildInput struct {
	Action  carrierabsence.Action
	WorkDir string
	Adapter RemovalAdapter
}

// BuildRemovalCommand converts one admitted carrier-absence action into one
// structured host command. It performs no host I/O and makes no convergence
// claim.
func BuildRemovalCommand(input RemovalBuildInput) (Command, error) {
	action := input.Action
	subject := action.Subject()
	if err := action.Validate(); err != nil {
		return Command{}, newValidationError(
			ReasonUnsupportedAction,
			subject,
			"carrier removal action is invalid: %v",
			err,
		)
	}
	if !action.InvokesHostRoute() {
		return Command{}, newValidationError(
			ReasonUnsupportedAction,
			subject,
			"carrier absence decision %q cannot invoke a host removal route",
			action.Decision(),
		)
	}
	if strings.TrimSpace(input.WorkDir) == "" {
		return Command{}, newValidationError(
			ReasonMissingWorkDir,
			subject,
			"host removal route requires caller-provided workdir",
		)
	}
	if input.Adapter == nil {
		return Command{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"host removal route has no command adapter",
		)
	}

	route := action.RouteAdmission()
	carrierKey := action.Claim().Identity().Carrier().Key()
	request := RemovalRequest{
		subject:      subject,
		target:       action.Target(),
		scope:        action.Scope(),
		carrier:      carrierKey.Carrier(),
		source:       carrierKey.Source(),
		operation:    route.Operation(),
		routeRequest: route.Request(),
		workDir:      input.WorkDir,
	}
	attempt, err := input.Adapter(request)
	if err != nil {
		return Command{}, err
	}
	if strings.TrimSpace(attempt.Command) == "" ||
		strings.TrimSpace(attempt.Command) != attempt.Command {
		return Command{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"host removal adapter returned an invalid command",
		)
	}
	if attempt.WorkDir != input.WorkDir {
		return Command{}, newValidationError(
			ReasonMissingWorkDir,
			subject,
			"host removal adapter workdir %q does not match selected workdir %q",
			attempt.WorkDir,
			input.WorkDir,
		)
	}
	return Command{
		subject:      subject,
		routeRequest: route.Request(),
		attempt:      attempt,
	}, nil
}

// BuildDelegatedRemovalAttempt lowers one admitted carrier removal through the
// same operation-indexed command-adapter catalog used by locked operations.
// It performs no host I/O and grants no authority to invoke the command.
func BuildDelegatedRemovalAttempt(request RemovalRequest) (subprocess.CommandAttemptRequest, error) {
	subject := request.Subject()
	if strings.TrimSpace(request.WorkDir()) == "" {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonMissingWorkDir,
			subject,
			"carrier removal route requires caller-provided workdir",
		)
	}
	carrierKey, err := desiredextension.NewCarrierKey(
		request.Carrier(),
		request.Target(),
		request.Scope(),
		request.Source(),
	)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"carrier removal identity is invalid: %v",
			err,
		)
	}
	if !extensiontopology.IsCarrierRelation(carrierKey.Carrier(), subject) {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"carrier removal subject is outside carrier family %q",
			carrierKey.Carrier(),
		)
	}
	operation := request.Operation()
	if operation.Operation() != lock.OperationRemove ||
		operation.Actuation() != lock.ActuationDelegatedHostRoute ||
		operation.Authority() != lock.AuthorityRemove ||
		!operation.OrdinaryMutationEligible() {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"carrier removal operation is not eligible for delegated mutation",
		)
	}
	routeRequest := request.RouteRequest()
	if err := routeRequest.Validate(); err != nil {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonRouteRequestMismatch,
			subject,
			"carrier removal route request is invalid: %v",
			err,
		)
	}
	route := operation.Route()
	if routeRequest.RouteID() != route.RouteID ||
		routeRequest.ContractVersion() != route.AdapterContractVersion {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonRouteRequestMismatch,
			subject,
			"carrier removal route request does not match operation contract",
		)
	}
	adapter, ok := commandAdapterForRoute(
		carrierKey.Carrier(),
		lock.OperationRemove,
		route,
	)
	if !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"carrier %q has no admitted remove command adapter",
			carrierKey.Carrier(),
		)
	}
	return adapter.buildAttempt(operation, commandAdapterInput{
		subject: subject,
		scope:   carrierKey.Scope(),
		source:  carrierKey.Source(),
		workDir: request.WorkDir(),
	})
}

// Subject returns the exact managed relation subject.
func (request RemovalRequest) Subject() topology.SubjectID { return request.subject }

// Target returns the host selected by the managed carrier identity.
func (request RemovalRequest) Target() target.Target { return request.target }

// Scope returns the carrier locality selected by the managed claim.
func (request RemovalRequest) Scope() target.Scope { return request.scope }

// Carrier returns the exact host-native carrier family selected by the claim.
func (request RemovalRequest) Carrier() desiredextension.Carrier { return request.carrier }

// Source returns the exact unresolved host-native source selected by the claim.
func (request RemovalRequest) Source() desiredextension.SourceRef { return request.source }

// Operation returns the admitted remove operation contract.
func (request RemovalRequest) Operation() lock.OperationContract { return request.operation }

// RouteRequest returns the exact operation-indexed route identity.
func (request RemovalRequest) RouteRequest() realizationdelegate.Request {
	return request.routeRequest
}

// WorkDir returns the selected lexical working directory. Descriptor-backed
// authority is acquired separately at execution time.
func (request RemovalRequest) WorkDir() string { return request.workDir }
