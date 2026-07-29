package readiness

import (
	"context"
	"fmt"

	mcpprovider "github.com/isty2e/daem/internal/assurance/observe/mcp/provider"
	mcpproviderhost "github.com/isty2e/daem/internal/assurance/observe/mcp/provider/host"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/topology"
)

// MCPProviderPrerequisiteState classifies one provider contribution's current
// plan dependency without conflating it with the carrier relation.
type MCPProviderPrerequisiteState string

const (
	MCPProviderCurrent         MCPProviderPrerequisiteState = "current"
	MCPProviderInstallRequired MCPProviderPrerequisiteState = "install_required"
	MCPProviderBlocked         MCPProviderPrerequisiteState = "blocked"
)

// MCPProviderPrerequisiteReason explains the provider dependency result.
type MCPProviderPrerequisiteReason string

const (
	MCPProviderReasonNone                MCPProviderPrerequisiteReason = ""
	MCPProviderReasonRelationInstall     MCPProviderPrerequisiteReason = "relation_install_required"
	MCPProviderReasonPackageAbsent       MCPProviderPrerequisiteReason = "package_absent"
	MCPProviderReasonVersionUnobserved   MCPProviderPrerequisiteReason = "version_unobserved"
	MCPProviderReasonVersionIncompatible MCPProviderPrerequisiteReason = "version_incompatible"
	MCPProviderReasonCodecMismatch       MCPProviderPrerequisiteReason = "codec_mismatch"
)

// MCPProviderPrerequisite is one provider-scoped dependency shared by all
// selected MCP projections that consume the contribution.
type MCPProviderPrerequisite struct {
	observation    mcpprovider.Observation
	relationAction reconcile.RelationAction
	installRequest realizationdelegate.Request
	requiredCodec  aggregate.CodecContractID
	state          MCPProviderPrerequisiteState
	reason         MCPProviderPrerequisiteReason
	detail         string
}

func (prerequisite MCPProviderPrerequisite) Observation() mcpprovider.Observation {
	return prerequisite.observation
}

func (prerequisite MCPProviderPrerequisite) State() MCPProviderPrerequisiteState {
	return prerequisite.state
}

func (prerequisite MCPProviderPrerequisite) Reason() MCPProviderPrerequisiteReason {
	return prerequisite.reason
}

func (prerequisite MCPProviderPrerequisite) Detail() string { return prerequisite.detail }

func (prerequisite MCPProviderPrerequisite) RequiredCodec() aggregate.CodecContractID {
	return prerequisite.requiredCodec
}

// InstallAction returns the existing admitted relation install action, or an
// explicit replay of that same route when only the package artifact is absent.
func (prerequisite MCPProviderPrerequisite) InstallAction() (
	reconcile.RelationAction,
	bool,
	error,
) {
	if prerequisite.state != MCPProviderInstallRequired {
		return reconcile.RelationAction{}, false, nil
	}
	if prerequisite.relationAction.InvokesHostRoute() {
		return prerequisite.relationAction, true, nil
	}
	replayed, err := prerequisite.relationAction.ReplayInstallRoute()
	if err != nil {
		return reconcile.RelationAction{}, false, err
	}
	if !replayed.RouteRequest().Equal(prerequisite.installRequest) {
		return reconcile.RelationAction{}, false, fmt.Errorf(
			"MCP provider install replay differs from locked install request",
		)
	}
	return replayed, true, nil
}

func observeMCPProviders(
	ctx context.Context,
	paths daempaths.Paths,
	locked lock.File,
	contracts []lock.LockedSubjectContract,
) ([]mcpprovider.Observation, error) {
	return mcpproviderhost.Observe(ctx, mcpproviderhost.Input{
		Paths: paths, Locked: locked, Contracts: contracts,
	})
}

func planMCPProviderPrerequisites(
	locked lock.File,
	observations []mcpprovider.Observation,
	relationActions []reconcile.RelationAction,
) ([]MCPProviderPrerequisite, error) {
	result := make([]MCPProviderPrerequisite, 0, len(observations))
	for _, observation := range observations {
		requiredCodec, err := providerRequiredCodec(locked.Locked, observation)
		if err != nil {
			return nil, err
		}
		relationAction, err := providerRelationAction(observation, relationActions)
		if err != nil {
			return nil, err
		}
		relationContract, present := locked.Locked.Subject(
			observation.Carrier().RelationSubject(),
		)
		if !present {
			return nil, fmt.Errorf(
				"MCP provider relation %q is absent from the lock",
				observation.Carrier().RelationSubject(),
			)
		}
		installRequest, err := lock.DelegatedOperationRequest(
			relationContract,
			lock.OperationInstall,
		)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP provider install request: %w", err)
		}
		prerequisite := MCPProviderPrerequisite{
			observation: observation, relationAction: relationAction,
			installRequest: installRequest, requiredCodec: requiredCodec,
		}
		switch observation.State() {
		case mcpprovider.StateCurrent:
			if observation.MappedCodec() != requiredCodec {
				prerequisite.state = MCPProviderBlocked
				prerequisite.reason = MCPProviderReasonCodecMismatch
				prerequisite.detail = fmt.Sprintf(
					"current provider version %q maps to codec %q, not locked codec %q",
					observation.Version(),
					observation.MappedCodec(),
					requiredCodec,
				)
			} else if relationAction.InvokesHostRoute() {
				prerequisite.state = MCPProviderInstallRequired
				prerequisite.reason = MCPProviderReasonRelationInstall
			} else {
				prerequisite.state = MCPProviderCurrent
			}
		case mcpprovider.StateAbsent:
			prerequisite.state = MCPProviderInstallRequired
			prerequisite.reason = MCPProviderReasonPackageAbsent
		case mcpprovider.StateUnobservable:
			prerequisite.state = MCPProviderBlocked
			prerequisite.reason = MCPProviderReasonVersionUnobserved
			prerequisite.detail = observation.Detail()
		case mcpprovider.StateIncompatible:
			prerequisite.state = MCPProviderBlocked
			prerequisite.reason = MCPProviderReasonVersionIncompatible
			prerequisite.detail = observation.Detail()
		default:
			return nil, fmt.Errorf(
				"MCP provider observation has unsupported state %q",
				observation.State(),
			)
		}
		result = append(result, prerequisite)
	}
	return result, nil
}

func providerRequiredCodec(
	locked lock.LockedSection,
	observation mcpprovider.Observation,
) (aggregate.CodecContractID, error) {
	var required aggregate.CodecContractID
	for _, subject := range observation.Consumers() {
		contract, present := locked.Subject(subject)
		if !present {
			return "", fmt.Errorf("MCP provider consumer %q is absent from the lock", subject)
		}
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return "", err
		}
		if !present {
			return "", fmt.Errorf("MCP provider consumer %q has no aggregate contribution", subject)
		}
		codec := contribution.Contribution().CodecContractID()
		if required == "" {
			required = codec
			continue
		}
		if required != codec {
			return "", fmt.Errorf(
				"MCP provider contribution is consumed by incompatible codecs %q and %q",
				required,
				codec,
			)
		}
	}
	if required == "" {
		return "", fmt.Errorf("MCP provider contribution has no locked consumers")
	}
	return required, nil
}

func providerRelationAction(
	observation mcpprovider.Observation,
	actions []reconcile.RelationAction,
) (reconcile.RelationAction, error) {
	var matched reconcile.RelationAction
	found := false
	for _, action := range actions {
		if !action.CarrierIdentity().ExactEqual(observation.Carrier()) {
			continue
		}
		if found {
			return reconcile.RelationAction{}, fmt.Errorf(
				"MCP provider carrier %q has duplicate relation actions",
				observation.Carrier().CarrierSubject(),
			)
		}
		matched = action
		found = true
	}
	if !found {
		return reconcile.RelationAction{}, fmt.Errorf(
			"MCP provider carrier %q has no relation action",
			observation.Carrier().CarrierSubject(),
		)
	}
	return matched, nil
}

func providerPrerequisiteConstraints(
	prerequisites []MCPProviderPrerequisite,
) ([]reconcileprojection.AggregateSubjectConstraint, error) {
	result := make([]reconcileprojection.AggregateSubjectConstraint, 0)
	for _, prerequisite := range prerequisites {
		if prerequisite.State() != MCPProviderBlocked {
			continue
		}
		var reason reconcile.ActionReason
		switch prerequisite.Reason() {
		case MCPProviderReasonVersionUnobserved:
			reason = reconcile.ReasonProviderVersionUnobserved
		case MCPProviderReasonVersionIncompatible:
			reason = reconcile.ReasonProviderVersionIncompatible
		case MCPProviderReasonCodecMismatch:
			reason = reconcile.ReasonProviderCodecMismatch
		default:
			return nil, fmt.Errorf(
				"blocked MCP provider has unsupported reason %q",
				prerequisite.Reason(),
			)
		}
		for _, subject := range prerequisite.Observation().Consumers() {
			constraint, err := reconcileprojection.NewAggregateSubjectConstraint(
				subject,
				reason,
				prerequisite.Detail(),
			)
			if err != nil {
				return nil, err
			}
			result = append(result, constraint)
		}
	}
	return result, nil
}

func providerConsumerIndex(
	prerequisites []MCPProviderPrerequisite,
) (map[topology.SubjectID]MCPProviderPrerequisite, error) {
	result := make(map[topology.SubjectID]MCPProviderPrerequisite)
	for _, prerequisite := range prerequisites {
		for _, subject := range prerequisite.Observation().Consumers() {
			if _, duplicate := result[subject]; duplicate {
				return nil, fmt.Errorf("duplicate MCP provider prerequisite for %q", subject)
			}
			result[subject] = prerequisite
		}
	}
	return result, nil
}
