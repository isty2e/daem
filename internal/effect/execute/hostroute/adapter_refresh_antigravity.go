package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

var antigravityCLIPluginCarrierRefreshCommandAdapter = commandAdapter{
	label:     "Antigravity CLI plugin repeat-install refresh",
	operation: lock.OperationRefresh,
	profile: mustCommandAdapterProfile(
		target.TargetAntigravityCLI,
		desiredextension.CarrierAntigravityCLIPlugin,
	),
	build:    buildAntigravityCLIPluginCarrierRefreshCommand,
	disclose: discloseAntigravityCLIPluginCarrierRefresh,
}

func buildAntigravityCLIPluginCarrierRefreshCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Antigravity CLI plugin repeat-install refresh supports explicit global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin repeat-install refresh supports host-source only",
		)
	}
	if err := validateHostRouteSourceArg(input.source.Ref(), subject); err != nil {
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

func discloseAntigravityCLIPluginCarrierRefresh(
	input commandAdapterInput,
) (Disclosure, error) {
	return NewDisclosure(DisclosureInput{
		ExecutionSubject: input.subject.String(),
		InvocationKind:   InvocationKindCommand,
		CWDPolicy:        CWDPolicySelectedRoot,
		EffectClasses: []string{
			"bundled_contribution_changes",
			"host_source_access",
			"import_registry_maintenance",
			"plugin_bundle_replacement",
			"plugin_install_lifecycle",
		},
		RetainedEffectClasses: []string{
			"installed_plugin_bundle",
			"partial_host_source_state",
			"plugin_import_registry",
		},
		NonClaims: []string{
			"antigravity_ide_support",
			"dedicated_host_update",
			"exact_artifact_convergence",
			"host_rollback",
			"plugin_activation",
			"plugin_uninstall",
			"remote_source_freshness",
			"runtime_readiness",
		},
	})
}
