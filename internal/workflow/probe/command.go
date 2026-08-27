package probe

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/isty2e/daem/internal/assurance/runtimeprobe"
	runtimeprobemcp "github.com/isty2e/daem/internal/assurance/runtimeprobe/mcp"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

// Prepare resolves one immutable probe request and retains its selected-root
// authority so later execution consumes exactly the disclosed operation.
func Prepare(ctx context.Context, input CommandInput) (*PreparedCommand, error) {
	if err := validateMode(input.Mode); err != nil {
		return nil, err
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return nil, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return nil, err
	}
	loaded, result, err := loadCommandInputs(ctx, input, paths, barrier)
	if err != nil {
		return nil, err
	}

	result.Subject = loaded.contract.SubjectID()
	request, err := probeRequestFromLockedLaunchIdentity(loaded.contract)
	if err != nil {
		return nil, err
	}
	result.Mode = ModeDryRun
	result.WorkingDirectory = loaded.paths.ManifestRoot
	result.ProbeRequest = cloneProbeRequest(request)
	result.SideEffects = probeSideEffects(request)
	result.Runtime, err = runtimeprobe.FoldFacts(nil)
	if err != nil {
		return nil, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, barrier.RevisionRequests()...)
	if err != nil {
		return nil, err
	}
	if err := barrier.Validate(ctx); err != nil {
		return nil, err
	}
	binding, err := projectWorkingDirectoryBinder(result.WorkingDirectory)()
	if err != nil {
		return nil, err
	}
	return newPreparedCommand(
		result,
		request,
		binding,
		loaded.paths.DataDir,
		barrier,
		revisions,
	), nil
}

type commandInputs struct {
	paths    daempaths.Paths
	contract lock.LockedSubjectContract
}

func loadCommandInputs(
	ctx context.Context,
	input CommandInput,
	paths daempaths.Paths,
	barrier recoverygate.EffectAuthority,
) (commandInputs, CommandResult, error) {
	if strings.TrimSpace(input.ServerName) == "" {
		return commandInputs{}, CommandResult{}, fmt.Errorf("mcp-server name is required")
	}
	selectedTarget, err := parseProbeTarget(input.TargetValue)
	if err != nil {
		return commandInputs{}, CommandResult{}, err
	}
	selectedScope, err := parseProbeScope(input.ScopeValue)
	if err != nil {
		return commandInputs{}, CommandResult{}, err
	}
	result := CommandResult{
		ManifestPath:     paths.ManifestPath,
		LockfilePath:     selectedLockfilePath(paths, input.LockfilePath),
		LockfileExplicit: input.LockfilePath != "",
		ServerName:       input.ServerName,
		Target:           selectedTarget,
		Scope:            selectedScope,
		Mode:             input.Mode,
		Timeout:          input.Timeout,
	}
	if err := barrier.Validate(ctx); err != nil {
		return commandInputs{}, result, err
	}

	environment, err := declarationmanifest.LoadSelected(ctx, paths)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("invalid manifest: %w", err)
	}
	server, binding, err := selectedMCPBinding(environment.MCPServers(), input.ServerName, selectedTarget, selectedScope)
	if err != nil {
		return commandInputs{}, result, err
	}
	selectedTarget = binding.Target()
	selectedScope = binding.Scope()
	result.Target = selectedTarget
	result.Scope = selectedScope
	if selectedScope == target.ScopeProject && !paths.ProjectPlacementAllowed() {
		return commandInputs{}, result, fmt.Errorf("project MCP probe requires an explicit or cwd manifest; pass --manifest for the project to probe")
	}
	locked, err := lockfile.Load(ctx, result.LockfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return commandInputs{}, result, fmt.Errorf("read lockfile: %w; run lock before probing runtime readiness", err)
		}
		return commandInputs{}, result, fmt.Errorf("read lockfile: %w", err)
	}
	if err := lockrefine.ValidateCurrentExtensionOrder(
		environment.Extensions(),
		locked,
		aggregatecodec.ExtensionOrderIdentityResolver(paths),
	); err != nil {
		return commandInputs{}, result, fmt.Errorf("read lockfile: %w", err)
	}
	currentContract, err := currentMCPSubjectContract(server, binding)
	if err != nil {
		return commandInputs{}, result, fmt.Errorf("build current MCP subject: %w", err)
	}
	lockedContract, err := lockedMCPSubject(locked, currentContract.SubjectID())
	if err != nil {
		return commandInputs{}, result, err
	}
	if !lockedContract.Equal(currentContract) {
		return commandInputs{}, result, fmt.Errorf("locked MCP subject %q is stale; run lock before probing runtime readiness", currentContract.SubjectID().Key())
	}
	if err := validateAdmittedLockedSubject(lockedContract, input.ServerName, selectedTarget, selectedScope); err != nil {
		return commandInputs{}, result, err
	}

	return commandInputs{paths: paths, contract: lockedContract}, result, nil
}

func parseProbeTarget(value string) (target.Target, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	parsed, err := target.ParseTarget(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if !runtimeProbeTargetSupported(parsed) {
		return "", fmt.Errorf(
			"MCP runtime probe supports only --target %s",
			joinAlternatives(runtimeProbeTargetValues()),
		)
	}
	return parsed, nil
}

func parseProbeScope(value string) (target.Scope, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	parsed, err := target.ParseScope(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if !runtimeProbeScopeSupported(parsed) {
		return "", fmt.Errorf(
			"MCP runtime probe supports only --scope %s",
			joinAlternatives(runtimeProbeScopeValues()),
		)
	}
	return parsed, nil
}

func selectedLockfilePath(paths daempaths.Paths, lockfilePath string) string {
	if lockfilePath != "" {
		return lockfilePath
	}
	return paths.LockfilePath
}

type selectedBinding struct {
	server  desiredmcp.Server
	binding desiredmcp.Binding
}

func selectedMCPBinding(
	servers []desiredmcp.Server,
	name string,
	selectedTarget target.Target,
	selectedScope target.Scope,
) (desiredmcp.Server, desiredmcp.Binding, error) {
	var matches []selectedBinding
	var rejected []error
	for _, server := range servers {
		if server.ID().Name() != name {
			continue
		}
		for _, binding := range server.Bindings() {
			if selectedTarget != "" && binding.Target() != selectedTarget {
				continue
			}
			if selectedScope != "" && binding.Scope() != selectedScope {
				continue
			}
			if err := validateRuntimeProbeBinding(binding); err != nil {
				rejected = append(rejected, err)
				continue
			}
			matches = append(matches, selectedBinding{server: server, binding: binding})
		}
	}
	if len(matches) == 0 {
		if len(rejected) == 1 {
			return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf(
				"mcp_server %q runtime-probe admission: %w",
				name,
				rejected[0],
			)
		}
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp_server %q has no admitted runtime-probe row matching target=%s scope=%s", name, selectedTarget, selectedScope)
	}
	if len(matches) > 1 {
		return desiredmcp.Server{}, desiredmcp.Binding{}, fmt.Errorf("mcp_server %q is ambiguous across %d runtime-probe rows; narrow with --target and --scope", name, len(matches))
	}
	return matches[0].server, matches[0].binding, nil
}

func currentMCPSubjectContract(server desiredmcp.Server, binding desiredmcp.Binding) (lock.LockedSubjectContract, error) {
	return lockrefine.MCPBindingSubject(
		server,
		binding,
		mcpcodec.CanonicalMCPBindingContribution,
	)
}

func lockedMCPSubject(locked lock.File, subject topology.SubjectID) (lock.LockedSubjectContract, error) {
	contract, ok := locked.Locked.Subject(subject)
	if !ok {
		return lock.LockedSubjectContract{}, fmt.Errorf("locked MCP subject %s/%s %q is missing; run lock before probing runtime readiness", subject.Kind(), subject.Namespace(), subject.Key())
	}
	return contract, nil
}

func validateAdmittedLockedSubject(contract lock.LockedSubjectContract, serverName string, selectedTarget target.Target, selectedScope target.Scope) error {
	subject := contract.SubjectID()
	serverID, ok := topologymcp.ServerID(subject)
	if !ok || serverID != serverName {
		return fmt.Errorf("locked subject %s/%s %q does not match selected mcp-server %q", subject.Kind(), subject.Namespace(), subject.Key(), serverName)
	}
	realization, ok := contract.Realization()
	if !ok {
		return fmt.Errorf("locked MCP subject %q is missing aggregate realization", subject.Key())
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		return fmt.Errorf("locked MCP subject %q is not a managed aggregate contribution", subject.Key())
	}
	if contribution.Target() != selectedTarget || contribution.Scope() != selectedScope {
		return fmt.Errorf("locked MCP subject %q target/scope = %s/%s, want %s/%s", subject.Key(), contribution.Target(), contribution.Scope(), selectedTarget, selectedScope)
	}
	placement, _, ok := runtimeProbePlacementCapability(subject)
	if !ok {
		return fmt.Errorf(
			"locked subject %s/%s %q is not admitted for runtime probe",
			subject.Kind(),
			subject.Namespace(),
			subject.Key(),
		)
	}
	expected, err := placement.Contribution(serverName, contribution.CanonicalContribution())
	if err != nil {
		return fmt.Errorf("locked MCP subject %q projection: %w", subject.Key(), err)
	}
	if !contribution.Equal(expected) {
		return fmt.Errorf(
			"locked MCP subject %q projection identity does not match admitted runtime-probe adapter",
			subject.Key(),
		)
	}
	return nil
}

func probeRequestFromLockedLaunchIdentity(contract lock.LockedSubjectContract) (runtimeprobemcp.ProbeRequest, error) {
	subject := contract.SubjectID()
	realization, ok := contract.Realization()
	if !ok {
		return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q is missing aggregate realization", subject.Key())
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q is not a managed aggregate contribution", subject.Key())
	}
	placement, capability, ok := runtimeProbePlacementCapability(subject)
	if !ok {
		return runtimeprobemcp.ProbeRequest{}, fmt.Errorf(
			"locked subject %s/%s %q is not admitted for runtime probe",
			subject.Kind(),
			subject.Namespace(),
			subject.Key(),
		)
	}
	command, args, env, err := mcpcodec.DecodeRuntimeProbeLaunch(
		placement.ID(),
		contribution.CanonicalContribution(),
	)
	if err != nil {
		return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("decode locked MCP projection for %q: %w", subject.Key(), err)
	}
	if capability.RequiresDelegatePlan() {
		plan, present := contract.DelegatePlan()
		if !present {
			return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q is missing locked launch identity", subject.Key())
		}
		commandSpec, err := delegate.NewCommandSpec(command, args)
		if err != nil {
			return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q has invalid probe command: %w", subject.Key(), err)
		}
		envBindings, err := delegateEnvBindingSet(env)
		if err != nil {
			return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q has invalid probe environment: %w", subject.Key(), err)
		}
		if !plan.CorrelatesInvocation(commandSpec, envBindings) {
			return runtimeprobemcp.ProbeRequest{}, fmt.Errorf("locked MCP subject %q projection does not match locked launch identity", subject.Key())
		}
	}
	return runtimeprobemcp.ProbeRequest{
		Transport: runtimeprobemcp.TransportStdio,
		Command:   command,
		Args:      args,
		Env:       env,
	}, nil
}

func delegateEnvBindingSet(values map[string]string) (delegate.EnvBindingSet, error) {
	result := make([]delegate.EnvBinding, 0, len(values))
	for name, sourceName := range values {
		binding, err := delegate.NewEnvBinding(name, sourceName)
		if err != nil {
			return delegate.EnvBindingSet{}, err
		}
		result = append(result, binding)
	}
	return delegate.NewEnvBindingSet(result)
}

func validateRuntimeProbeBinding(binding desiredmcp.Binding) error {
	placement, err := aggregate.MCPPlacementForBinding(binding)
	if err != nil {
		return err
	}
	if _, ok := profile.Profile(binding.Target()).MCPRuntimeProbeCapability(
		placement.ID(),
	); !ok {
		return fmt.Errorf(
			"target=%s scope=%s is not admitted for runtime probe",
			binding.Target(),
			binding.Scope(),
		)
	}
	return nil
}

func runtimeProbePlacementCapability(
	subject topology.SubjectID,
) (aggregate.MCPPlacement, profile.MCPRuntimeProbeCapability, bool) {
	placement, ok := aggregate.MCPPlacementForSubject(subject)
	if !ok {
		return aggregate.MCPPlacement{}, profile.MCPRuntimeProbeCapability{}, false
	}
	capability, ok := profile.Profile(placement.Target()).MCPRuntimeProbeCapability(
		placement.ID(),
	)
	if !ok {
		return aggregate.MCPPlacement{}, profile.MCPRuntimeProbeCapability{}, false
	}
	return placement, capability, true
}

func runtimeProbeTargetSupported(selectedTarget target.Target) bool {
	for _, capability := range profile.MCPRuntimeProbeCapabilities() {
		if capability.Placement().Target() == selectedTarget {
			return true
		}
	}
	return false
}

func runtimeProbeScopeSupported(selectedScope target.Scope) bool {
	for _, capability := range profile.MCPRuntimeProbeCapabilities() {
		if capability.Placement().Scope() == selectedScope {
			return true
		}
	}
	return false
}

func runtimeProbeTargetValues() []string {
	values := make([]string, 0)
	seen := make(map[target.Target]struct{})
	for _, capability := range profile.MCPRuntimeProbeCapabilities() {
		value := capability.Placement().Target()
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, string(value))
	}
	return values
}

func runtimeProbeScopeValues() []string {
	values := make([]string, 0)
	seen := make(map[target.Scope]struct{})
	for _, capability := range profile.MCPRuntimeProbeCapabilities() {
		value := capability.Placement().Scope()
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, string(value))
	}
	return values
}

func joinAlternatives(values []string) string {
	switch len(values) {
	case 0:
		return "(none)"
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
	}
}

func probeSideEffects(request runtimeprobemcp.ProbeRequest) []string {
	effects := []string{
		"may launch the locked MCP server command as a child process",
		"runs the child process from the selected project root workdir",
		"inherits daem's process environment; ambient values are execution context, not lock identity or readiness evidence",
		"may write one MCP initialize request to child stdin and read MCP messages from child stdout",
		"the launched server may perform network calls or depend on existing auth, OAuth, trust, or session state outside daem ownership",
		"the declared command may perform package/cache/network behavior outside daem ownership",
		"may capture bounded stderr/stdout diagnostics with redaction on failure",
		"the probe is bounded by the selected timeout and cancels or terminates the child process during cleanup",
		"closes child stdin and terminates the child process after the initialize probe",
	}
	if len(request.Env) != 0 {
		effects = append(effects, "may read referenced host environment variables by name and pass their values to the child process")
	}
	return effects
}

func refuseJournalAndFileSet(ctx context.Context, paths daempaths.Paths) error {
	return recoverygate.RequireClear(ctx, paths)
}
