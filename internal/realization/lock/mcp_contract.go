package lock

import (
	"fmt"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

type mcpProjectionDependencyPolicy string

const (
	mcpProjectionRejectNonLauncherDependencies mcpProjectionDependencyPolicy = "reject-non-launcher-dependencies"
	mcpProjectionAllowCredentialDependencies   mcpProjectionDependencyPolicy = "allow-credential-dependencies"
)

type mcpProjectionLockSpec struct {
	Placement                aggregate.MCPPlacement
	Label                    string
	LauncherDependencyPolicy mcpProjectionDependencyPolicy
	ReplayExclusions         []ReplayExclusion
	WritePreconditions       []string
	RemovePreconditions      []string
}

// MCPProjectionSubjectInput carries facts already selected by Desired,
// Topology, the target profile, and the aggregate codec boundary.
type MCPProjectionSubjectInput struct {
	Graph                topology.Graph
	EntityID             entity.ID
	PlacementID          aggregate.MCPPlacementID
	ServerID             string
	RequestedOnAbsent    desiredmcp.OnAbsent
	LauncherCommand      string
	LauncherArgs         []string
	CanonicalProjection  string
	DelegatePlan         *delegate.DelegatePlan
	CredentialReferences []string
}

// NewMCPProjectionSubjectContract constructs one standalone MCP aggregate subject.
func NewMCPProjectionSubjectContract(input MCPProjectionSubjectInput) (LockedSubjectContract, error) {
	if err := input.EntityID.Validate(); err != nil {
		return LockedSubjectContract{}, fmt.Errorf("MCP projection entity: %w", err)
	}
	if input.EntityID.Kind() != entity.KindMCPServer || input.EntityID.Name() != input.ServerID {
		return LockedSubjectContract{}, fmt.Errorf(
			"MCP projection entity %q does not match server %q",
			input.EntityID,
			input.ServerID,
		)
	}
	spec, ok := mcpProjectionLockSpecFor(input.PlacementID)
	if !ok {
		return LockedSubjectContract{}, fmt.Errorf("unsupported MCP placement %q", input.PlacementID)
	}
	requestedOnAbsent, err := desiredmcp.ParseOnAbsent(string(input.RequestedOnAbsent))
	if err != nil {
		return LockedSubjectContract{}, err
	}
	if requestedOnAbsent != desiredmcp.OnAbsentRemoveBinding {
		return LockedSubjectContract{}, fmt.Errorf(
			"%s binding %q must use remove-binding absence with managed-binding precondition",
			spec.label(),
			input.ServerID,
		)
	}
	placement, err := spec.placement()
	if err != nil {
		return LockedSubjectContract{}, err
	}
	projectionSubject, err := spec.projectionSubject(input.Graph, placement, input.EntityID)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	if err := spec.validateLauncherDependency(input.Graph, projectionSubject, input.LauncherCommand); err != nil {
		return LockedSubjectContract{}, err
	}
	if err := spec.validateCredentialDependencies(input.Graph, projectionSubject, input.CredentialReferences); err != nil {
		return LockedSubjectContract{}, err
	}
	if err := validateMCPDelegatePlanCorrelation(
		input.DelegatePlan,
		input.LauncherCommand,
		input.LauncherArgs,
		input.CredentialReferences,
	); err != nil {
		return LockedSubjectContract{}, err
	}
	realization, err := mcpAggregateRealization(
		placement,
		input.ServerID,
		input.CanonicalProjection,
	)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	replay, err := spec.replayCoverage()
	if err != nil {
		return LockedSubjectContract{}, err
	}
	contracts, err := spec.operationContracts(placement)
	if err != nil {
		return LockedSubjectContract{}, err
	}
	return NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           input.EntityID,
		SubjectID:          projectionSubject,
		Realization:        &realization,
		DelegatePlan:       input.DelegatePlan,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentRemoveBinding,
		Replay:             replay,
		OperationContracts: contracts,
	})
}

func validateMCPDelegatePlanCorrelation(
	plan *delegate.DelegatePlan,
	command string,
	args []string,
	credentialReferences []string,
) error {
	if plan == nil {
		return nil
	}
	if err := plan.Validate(); err != nil {
		return fmt.Errorf("MCP delegate plan: %w", err)
	}
	if err := validateStringSet(credentialReferences, "MCP credential reference"); err != nil {
		return err
	}
	planCommand := plan.Command()
	if planCommand.Name() != command {
		return fmt.Errorf("MCP delegate command %q does not match launcher command %q", planCommand.Name(), command)
	}
	if !slices.Equal(planCommand.Args(), args) {
		return fmt.Errorf("MCP delegate args do not match launcher args")
	}
	if !slices.Equal(plan.Env().SourceNames(), normalizeStrings(credentialReferences)) {
		return fmt.Errorf("MCP delegate env refs do not match credential references")
	}
	return nil
}

func mcpProjectionLockSpecFor(id aggregate.MCPPlacementID) (mcpProjectionLockSpec, bool) {
	for _, spec := range []mcpProjectionLockSpec{
		claudeProjectMCPProjectionSpec,
		claudeGlobalMCPProjectionSpec,
		antigravityGlobalMCPProjectionSpec,
		openCodeProjectMCPProjectionSpec,
		openCodeGlobalMCPProjectionSpec,
		codexProjectMCPProjectionSpec,
		codexGlobalMCPProjectionSpec,
	} {
		if spec.Placement.ID() == id {
			return spec, true
		}
	}
	return mcpProjectionLockSpec{}, false
}

func (spec mcpProjectionLockSpec) placement() (aggregate.MCPPlacement, error) {
	if err := spec.Placement.Validate(); err != nil {
		return aggregate.MCPPlacement{}, fmt.Errorf("MCP projection lock spec placement: %w", err)
	}
	return spec.Placement, nil
}

func (spec mcpProjectionLockSpec) projectionSubject(
	graph topology.Graph,
	placement aggregate.MCPPlacement,
	id entity.ID,
) (topology.SubjectID, error) {
	subject, err := topologymcp.ProjectionSubject(placement.Target(), placement.Scope(), id.Name())
	if err != nil {
		return topology.SubjectID{}, err
	}
	if !graph.Contains(subject) {
		return topology.SubjectID{}, fmt.Errorf("%s projection subject %q is missing", spec.label(), subject)
	}
	return subject, nil
}

func (spec mcpProjectionLockSpec) replayCoverage() (ReplayCoverage, error) {
	return NewReplayCoverage(ReplayExact, ReplayExact, ReplayNotApplicable, spec.ReplayExclusions)
}

func (spec mcpProjectionLockSpec) operationContracts(placement aggregate.MCPPlacement) ([]OperationContract, error) {
	writeRoute, err := mcpRouteContract(placement, profile.OperationWrite)
	if err != nil {
		return nil, err
	}
	removeRoute, err := mcpRouteContract(placement, profile.OperationRemove)
	if err != nil {
		return nil, err
	}
	observe, err := NewOperationContract(OperationContractInput{
		Operation:       OperationObserve,
		Actuation:       ActuationNoMutation,
		Authority:       AuthorityObserve,
		EffectEnvelope:  EffectEnvelopeNotApplicable,
		Idempotency:     IdempotencyNotApplicable,
		Verification:    VerificationExactProjection,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryNotApplicable,
	})
	if err != nil {
		return nil, err
	}
	write, err := NewOperationContract(OperationContractInput{
		Operation:       OperationWriteProjection,
		Actuation:       ActuationDirectProjection,
		Authority:       AuthorityManage,
		Route:           writeRoute,
		Preconditions:   spec.WritePreconditions,
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     Idempotent,
		Verification:    VerificationExactProjection,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryAtomic,
	})
	if err != nil {
		return nil, err
	}
	remove, err := NewOperationContract(OperationContractInput{
		Operation:       OperationRemoveProjection,
		Actuation:       ActuationDirectProjection,
		Authority:       AuthorityRemove,
		Route:           removeRoute,
		Preconditions:   spec.RemovePreconditions,
		EffectEnvelope:  EffectEnvelopeComplete,
		Idempotency:     Idempotent,
		Verification:    VerificationExactProjection,
		TrustActivation: TrustActivationNotRequired,
		Recovery:        OperationRecoveryAtomic,
	})
	if err != nil {
		return nil, err
	}
	return []OperationContract{observe, write, remove}, nil
}

func mcpRouteContract(placement aggregate.MCPPlacement, operation profile.Operation) (RouteContractRef, error) {
	route, ok := profile.Profile(placement.Target()).OperationRoute(entity.KindMCPServer, string(placement.ID()), operation)
	if !ok {
		return RouteContractRef{}, fmt.Errorf("MCP placement %q has no unique %s route", placement.ID(), operation)
	}
	return RouteContractRef{
		RouteID:                route.RouteID(),
		AdapterContractVersion: route.AdapterContractVersion(),
	}, nil
}

func (spec mcpProjectionLockSpec) label() string {
	if strings.TrimSpace(spec.Label) == "" {
		return "MCP projection"
	}
	return strings.TrimSpace(spec.Label)
}

func (spec mcpProjectionLockSpec) validateLauncherDependency(
	graph topology.Graph,
	projection topology.SubjectID,
	command string,
) error {
	switch spec.LauncherDependencyPolicy {
	case mcpProjectionRejectNonLauncherDependencies, mcpProjectionAllowCredentialDependencies:
	default:
		return fmt.Errorf("%s launcher dependency policy %q is unsupported", spec.label(), spec.LauncherDependencyPolicy)
	}
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("%s launcher command is required", spec.label())
	}
	return validateMCPProjectionCommandDependency(
		spec.label(),
		graph,
		projection,
		command,
		spec.LauncherDependencyPolicy,
	)
}

func (spec mcpProjectionLockSpec) validateCredentialDependencies(
	graph topology.Graph,
	projection topology.SubjectID,
	references []string,
) error {
	if err := validateStringSet(references, "MCP credential reference"); err != nil {
		return err
	}
	references = normalizeStrings(references)
	if spec.LauncherDependencyPolicy != mcpProjectionAllowCredentialDependencies {
		if len(references) != 0 {
			return fmt.Errorf("%s does not admit credential references", spec.label())
		}
		return nil
	}
	expected := make(map[topology.SubjectID]struct{}, len(references))
	for _, reference := range references {
		subject, err := topologymcp.EnvironmentReferenceSubject(reference)
		if err != nil {
			return fmt.Errorf("%s credential reference %q: %w", spec.label(), reference, err)
		}
		expected[subject] = struct{}{}
	}
	actual := make(map[topology.SubjectID]struct{}, len(expected))
	for _, dependency := range graph.DependenciesOf(projection) {
		if dependency.Kind() != topology.SubjectCredentialReference {
			return fmt.Errorf("%s projection %q has unsupported dependency %s", spec.label(), projection, dependency)
		}
		if _, ok := topologymcp.EnvironmentReferenceName(dependency); !ok {
			return fmt.Errorf("%s credential dependency %s is outside the MCP env namespace", spec.label(), dependency)
		}
		actual[dependency] = struct{}{}
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("%s credential dependency count = %d, want %d", spec.label(), len(actual), len(expected))
	}
	for reference := range expected {
		if _, ok := actual[reference]; !ok {
			return fmt.Errorf("%s credential dependency %s is missing", spec.label(), reference)
		}
	}
	return nil
}

func validateMCPProjectionCommandDependency(
	label string,
	graph topology.Graph,
	projection topology.SubjectID,
	command string,
	policy mcpProjectionDependencyPolicy,
) error {
	dependencies := graph.DependenciesOf(projection)
	if len(dependencies) != 0 && policy != mcpProjectionAllowCredentialDependencies {
		return fmt.Errorf("%s projection %q cannot lock credential dependencies", label, projection)
	}
	launchers := graph.LauncherDependenciesOf(projection)
	if len(launchers) != 1 {
		return fmt.Errorf("%s projection %q requires exactly one launcher dependency", label, projection)
	}
	expected, err := topologymcp.ExecutableSubject(command)
	if err != nil {
		return fmt.Errorf("%s launcher command: %w", label, err)
	}
	if launchers[0] != expected {
		return fmt.Errorf("%s launcher %s, want %s", label, launchers[0], expected)
	}
	return nil
}

func mustProfileMCPPlacement(selectedTarget target.Target, id aggregate.MCPPlacementID) aggregate.MCPPlacement {
	placement, ok := profile.Profile(selectedTarget).MCPPlacement(id)
	if !ok {
		panic(fmt.Sprintf("target profile %q is missing MCP placement %q", selectedTarget, id))
	}
	return placement
}

func mcpAggregateRealization(
	placement aggregate.MCPPlacement,
	serverID string,
	canonicalProjection string,
) (realization.RealizationSpec, error) {
	contribution, err := placement.Contribution(serverID, canonicalProjection)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	return realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID:           contribution.PlacementID(),
		Target:                contribution.Target(),
		Scope:                 contribution.Scope(),
		AggregateRoot:         contribution.AggregateRoot(),
		ContentPath:           contribution.ContentPath(),
		MergeUnit:             contribution.MergeUnit(),
		Cardinality:           contribution.Cardinality(),
		SiblingRetention:      contribution.SiblingRetention(),
		SiblingPreservation:   contribution.SiblingPreservation(),
		Equivalence:           contribution.Equivalence(),
		CanonicalContribution: contribution.CanonicalContribution(),
		CodecContractID:       contribution.CodecContractID(),
		ComparedFields:        contribution.ComparedFields(),
	})
}
