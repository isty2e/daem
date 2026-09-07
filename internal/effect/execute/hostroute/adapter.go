package hostroute

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

const (
	claudeCommand                = "claude"
	claudePluginHostScopeProject = "project"
	claudePluginHostScopeUser    = "user"
	codexCommand                 = "codex"
	openCodeCommand              = "opencode"
	piCommand                    = "pi"
	agyCommand                   = "agy"
)

type commandAdapter struct {
	label     string
	operation lock.OperationKind
	profile   profile.DelegatedRouteProfile
	build     func(commandAdapterInput) (subprocess.CommandAttemptRequest, error)
	disclose  func(commandAdapterInput) (Disclosure, error)
}

type commandAdapterInput struct {
	subject topology.SubjectID
	scope   target.Scope
	source  desiredextension.SourceRef
	workDir string
}

func (adapter commandAdapter) buildAttempt(
	operationContract lock.OperationContract,
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	profileOperation, ok := profileOperationForHostOperation(adapter.operation)
	if !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"%s operation %q has no profile mapping",
			adapter.label,
			adapter.operation,
		)
	}
	operationRoute, ok := adapter.profile.OperationRoute(profileOperation)
	if !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedRoute,
			subject,
			"%s operation %q has no unique profile route",
			adapter.label,
			adapter.operation,
		)
	}
	if operationContract.Operation() != adapter.operation {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonInvalidLockedRecord,
			subject,
			"%s operation contract is %q, want %q",
			adapter.label,
			operationContract.Operation(),
			adapter.operation,
		)
	}
	lockedRoute := operationContract.Route()
	if lockedRoute.AdapterContractVersion != operationRoute.AdapterContractVersion() {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonInvalidLockedRecord,
			subject,
			"%s adapter contract version %q does not match command adapter contract %q",
			adapter.label,
			lockedRoute.AdapterContractVersion,
			operationRoute.AdapterContractVersion(),
		)
	}
	if lockedRoute.RouteID != operationRoute.RouteID() {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonInvalidLockedRecord,
			subject,
			"%s route id %q does not match command adapter route id %q",
			adapter.label,
			lockedRoute.RouteID,
			operationRoute.RouteID(),
		)
	}
	return adapter.build(input)
}

func commandAdapterForRoute(
	carrier desiredextension.Carrier,
	operation lock.OperationKind,
	lockedRoute lock.RouteContractRef,
) (commandAdapter, bool) {
	for _, adapter := range commandAdapters {
		if adapter.operation != operation ||
			adapter.profile.Carrier() != carrier {
			continue
		}
		profileOperation, ok := profileOperationForHostOperation(operation)
		if !ok {
			return commandAdapter{}, false
		}
		route, ok := adapter.profile.OperationRoute(profileOperation)
		if ok &&
			route.RouteID() == lockedRoute.RouteID &&
			route.AdapterContractVersion() == lockedRoute.AdapterContractVersion {
			return adapter, true
		}
	}
	return commandAdapter{}, false
}

func profileOperationForHostOperation(operation lock.OperationKind) (profile.Operation, bool) {
	switch operation {
	case lock.OperationInstall:
		return profile.OperationInstall, true
	case lock.OperationRefresh:
		return profile.OperationRefresh, true
	case lock.OperationRemove:
		return profile.OperationRemove, true
	default:
		return "", false
	}
}

var commandAdapters = []commandAdapter{
	claudePluginCarrierCommandAdapter,
	claudePluginCarrierRemoveCommandAdapter,
	claudePluginCarrierRefreshCommandAdapter,
	codexPluginCarrierCommandAdapter,
	codexPluginCarrierRemoveCommandAdapter,
	codexPluginCarrierRefreshCommandAdapter,
	openCodePluginCarrierCommandAdapter,
	openCodePluginCarrierRefreshCommandAdapter,
	piPackageCarrierCommandAdapter,
	piPackageCarrierRefreshCommandAdapter,
	piPackageCarrierRemoveCommandAdapter,
	antigravityCLIPluginCarrierCommandAdapter,
	antigravityCLIPluginCarrierRemoveCommandAdapter,
	antigravityCLIPluginCarrierRefreshCommandAdapter,
}

func init() {
	if err := validateCommandAdapterCatalog(commandAdapters); err != nil {
		panic(err)
	}
}

func validateCommandAdapterCatalog(adapters []commandAdapter) error {
	supported := desiredextension.SupportedCarriers()
	supportedSet := make(map[desiredextension.Carrier]struct{}, len(supported))
	for _, carrier := range supported {
		supportedSet[carrier] = struct{}{}
	}
	type adapterKey struct {
		carrier   desiredextension.Carrier
		operation lock.OperationKind
	}
	seen := make(map[adapterKey]struct{}, len(adapters))
	for _, adapter := range adapters {
		if strings.TrimSpace(adapter.label) == "" {
			return fmt.Errorf("delegated command adapter label is required")
		}
		if adapter.build == nil {
			return fmt.Errorf("delegated command adapter %q requires a builder", adapter.label)
		}
		if adapter.operation == lock.OperationRefresh && adapter.disclose == nil {
			return fmt.Errorf("delegated refresh command adapter %q requires a disclosure", adapter.label)
		}
		if err := adapter.profile.Validate(); err != nil {
			return fmt.Errorf("delegated command adapter %q profile: %w", adapter.label, err)
		}
		profileOperation, ok := profileOperationForHostOperation(adapter.operation)
		if !ok {
			return fmt.Errorf(
				"delegated command adapter %q uses unsupported operation %q",
				adapter.label,
				adapter.operation,
			)
		}
		if _, ok := adapter.profile.OperationRoute(profileOperation); !ok {
			return fmt.Errorf(
				"delegated command adapter %q has no unique %s profile route",
				adapter.label,
				adapter.operation,
			)
		}
		carrier := adapter.profile.Carrier()
		if _, ok := supportedSet[carrier]; !ok {
			return fmt.Errorf("delegated command adapter %q uses unsupported carrier %q", adapter.label, carrier)
		}
		key := adapterKey{carrier: carrier, operation: adapter.operation}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf(
				"duplicate delegated command adapter for carrier %q operation %q",
				carrier,
				adapter.operation,
			)
		}
		seen[key] = struct{}{}
	}
	for _, carrier := range supported {
		if _, ok := seen[adapterKey{carrier: carrier, operation: lock.OperationInstall}]; !ok {
			return fmt.Errorf(
				"supported extension carrier %q is missing an install command adapter",
				carrier,
			)
		}
	}
	return nil
}

var claudePluginCarrierCommandAdapter = commandAdapter{
	label:     "Claude plugin carrier install",
	operation: lock.OperationInstall,
	profile:   mustCommandAdapterProfile(target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin),
	build:     buildClaudePluginCarrierCommand,
}

var claudePluginCarrierRemoveCommandAdapter = commandAdapter{
	label:     "Claude plugin carrier remove",
	operation: lock.OperationRemove,
	profile:   mustCommandAdapterProfile(target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin),
	build:     buildClaudePluginCarrierRemoveCommand,
}

var codexPluginCarrierCommandAdapter = commandAdapter{
	label:     "Codex plugin carrier install",
	operation: lock.OperationInstall,
	profile:   mustCommandAdapterProfile(target.TargetCodex, desiredextension.CarrierCodexPlugin),
	build:     buildCodexPluginCarrierCommand,
}

var codexPluginCarrierRemoveCommandAdapter = commandAdapter{
	label:     "Codex plugin carrier remove",
	operation: lock.OperationRemove,
	profile:   mustCommandAdapterProfile(target.TargetCodex, desiredextension.CarrierCodexPlugin),
	build:     buildCodexPluginCarrierRemoveCommand,
}

var openCodePluginCarrierCommandAdapter = commandAdapter{
	label:     "OpenCode plugin carrier install",
	operation: lock.OperationInstall,
	profile:   mustCommandAdapterProfile(target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin),
	build:     buildOpenCodePluginCarrierCommand,
}

var piPackageCarrierCommandAdapter = commandAdapter{
	label:     "Pi package carrier install",
	operation: lock.OperationInstall,
	profile:   mustCommandAdapterProfile(target.TargetPi, desiredextension.CarrierPiPackage),
	build:     buildPiPackageCarrierCommand,
}

var piPackageCarrierRemoveCommandAdapter = commandAdapter{
	label:     "Pi package carrier remove",
	operation: lock.OperationRemove,
	profile:   mustCommandAdapterProfile(target.TargetPi, desiredextension.CarrierPiPackage),
	build:     buildPiPackageCarrierRemoveCommand,
}

var antigravityCLIPluginCarrierCommandAdapter = commandAdapter{
	label:     "Antigravity CLI plugin carrier install",
	operation: lock.OperationInstall,
	profile:   mustCommandAdapterProfile(target.TargetAntigravityCLI, desiredextension.CarrierAntigravityCLIPlugin),
	build:     buildAntigravityCLIPluginCarrierCommand,
}

var antigravityCLIPluginCarrierRemoveCommandAdapter = commandAdapter{
	label:     "Antigravity CLI plugin carrier remove",
	operation: lock.OperationRemove,
	profile: mustCommandAdapterProfile(
		target.TargetAntigravityCLI,
		desiredextension.CarrierAntigravityCLIPlugin,
	),
	build: buildAntigravityCLIPluginCarrierRemoveCommand,
}

func mustCommandAdapterProfile(
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
) profile.DelegatedRouteProfile {
	routeProfile, ok := hostsurfacecatalog.Product().ExtensionRouteProfile(selectedTarget, carrier)
	if !ok {
		panic(fmt.Sprintf("target profile %q is missing delegated command route %q", selectedTarget, carrier))
	}
	return routeProfile
}

func buildClaudePluginCarrierCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	hostScope, err := claudePluginHostScopeArg(input.scope, subject)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Claude plugin carrier install currently supports marketplace source only",
		)
	}
	if _, ok := input.source.MarketplaceSelector(); !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Claude plugin carrier marketplace source must be PLUGIN@MARKETPLACE",
		)
	}
	if err := validateHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	return subprocess.CommandAttemptRequest{
		Command: claudeCommand,
		Args: []string{
			"plugin",
			"install",
			input.source.Ref(),
			"--scope",
			hostScope,
		},
		WorkDir: input.workDir,
	}, nil
}

func claudePluginHostScopeArg(scope target.Scope, subject topology.SubjectID) (string, error) {
	switch scope {
	case target.ScopeProject:
		return claudePluginHostScopeProject, nil
	case target.ScopeGlobal:
		return claudePluginHostScopeUser, nil
	default:
		return "", newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Claude plugin carrier routes support daem project or global scope only",
		)
	}
}

func buildCodexPluginCarrierCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Codex plugin carrier install currently supports global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin carrier install currently supports marketplace source only",
		)
	}
	if _, ok := input.source.MarketplaceSelector(); !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin carrier install requires source selector PLUGIN@MARKETPLACE",
		)
	}
	if err := validateHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	return subprocess.CommandAttemptRequest{
		Command: codexCommand,
		Args: []string{
			"plugin",
			"add",
			input.source.Ref(),
			"--json",
		},
		WorkDir: input.workDir,
	}, nil
}

func buildOpenCodePluginCarrierCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeProject && input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"OpenCode plugin carrier install currently supports project or global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"OpenCode plugin carrier install currently supports host-source only",
		)
	}
	if err := validateHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	args := []string{"plugin", input.source.Ref()}
	if input.scope == target.ScopeGlobal {
		args = append(args, "--global")
	}
	return subprocess.CommandAttemptRequest{
		Command: openCodeCommand,
		Args:    args,
		WorkDir: input.workDir,
	}, nil
}

func buildPiPackageCarrierCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeProject && input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Pi package carrier install currently supports project or global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Pi package carrier install currently supports host-source only",
		)
	}
	if err := validatePiHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	args := []string{"install", input.source.Ref()}
	if input.scope == target.ScopeProject {
		args = append(args, "-l")
	}
	return subprocess.CommandAttemptRequest{
		Command: piCommand,
		Args:    args,
		WorkDir: input.workDir,
	}, nil
}

func buildAntigravityCLIPluginCarrierCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Antigravity CLI plugin carrier install currently supports global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin carrier install currently supports host-source only",
		)
	}
	if err := validateHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	return subprocess.CommandAttemptRequest{
		Command: agyCommand,
		Args: []string{
			"plugin",
			"install",
			input.source.Ref(),
		},
		WorkDir: input.workDir,
	}, nil
}

func validateHostRouteSourceArg(
	source desiredextension.SourceRef,
	subject topology.SubjectID,
) error {
	if !source.CredentialFree() || !source.ControlFree() {
		return newValidationError(
			ReasonUnsupportedSource,
			subject,
			"host route source must be inspectable and contain no inline credentials",
		)
	}
	return validateHostRouteArg(source.Ref(), subject)
}

func validatePiHostRouteSourceArg(
	source desiredextension.SourceRef,
	subject topology.SubjectID,
) error {
	if err := validateHostRouteSourceArg(source, subject); err != nil {
		return err
	}
	gitSource, ok := desiredextension.ParseGitSource(source.Ref())
	if !ok {
		return nil
	}
	host, _, _ := strings.Cut(gitSource.Identity(), "/")
	if desiredextension.PathSafeGitHost(host) {
		return nil
	}
	return newValidationError(
		ReasonUnsupportedSource,
		subject,
		"Pi git package host is not path-safe",
	)
}

func validateHostRouteArg(source string, subject topology.SubjectID) error {
	if strings.HasPrefix(source, "-") {
		return newValidationError(
			ReasonUnsupportedSource,
			subject,
			"host route source must not begin with '-' because host CLIs may parse it as an option",
		)
	}
	return nil
}
