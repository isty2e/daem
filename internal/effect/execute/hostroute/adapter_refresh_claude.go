package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

var claudePluginCarrierRefreshCommandAdapter = commandAdapter{
	label:     "Claude plugin carrier refresh",
	operation: lock.OperationRefresh,
	profile: mustCommandAdapterProfile(
		target.TargetClaudeCode,
		desiredextension.CarrierClaudeCodePlugin,
	),
	build:    buildClaudePluginCarrierRefreshCommand,
	disclose: discloseClaudePluginCarrierRefresh,
}

func buildClaudePluginCarrierRefreshCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	hostScope, err := claudePluginHostScopeArg(input.scope, subject)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Claude plugin carrier refresh supports marketplace source only",
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
			"update",
			input.source.Ref(),
			"--scope",
			hostScope,
		},
		WorkDir: input.workDir,
	}, nil
}

func discloseClaudePluginCarrierRefresh(
	input commandAdapterInput,
) (Disclosure, error) {
	return NewDisclosure(DisclosureInput{
		ExecutionSubject: input.subject.String(),
		InvocationKind:   InvocationKindCommand,
		CWDPolicy:        CWDPolicySelectedRoot,
		EffectClasses: []string{
			"dependency_resolution",
			"marketplace_source_access",
			"plugin_cache_write",
			"plugin_relation_update",
			"restart_required",
		},
		RetainedEffectClasses: []string{
			"dependency_state",
			"marketplace_state",
			"old_plugin_cache",
			"selected_plugin_version",
		},
		NonClaims: []string{
			"carrier_uninstall",
			"exact_artifact_convergence",
			"host_rollback",
			"plugin_activation",
			"runtime_readiness",
			"trust_or_approval",
			"version_pin",
		},
	})
}
