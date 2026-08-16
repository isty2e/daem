package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

var openCodePluginCarrierRefreshCommandAdapter = commandAdapter{
	label:     "OpenCode plugin carrier refresh",
	operation: lock.OperationRefresh,
	profile: mustCommandAdapterProfile(
		target.TargetOpenCode,
		desiredextension.CarrierOpenCodePlugin,
	),
	build:    buildOpenCodePluginCarrierRefreshCommand,
	disclose: discloseOpenCodePluginCarrierRefresh,
}

func buildOpenCodePluginCarrierRefreshCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeProject &&
		input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"OpenCode plugin carrier refresh supports project or global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"OpenCode plugin carrier refresh supports host-source only",
		)
	}
	if err := validateHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	args := []string{"plugin", input.source.Ref(), "--force"}
	if input.scope == target.ScopeGlobal {
		args = append(args, "--global")
	}
	return subprocess.CommandAttemptRequest{
		Command: openCodeCommand,
		Args:    args,
		WorkDir: input.workDir,
	}, nil
}

func discloseOpenCodePluginCarrierRefresh(
	input commandAdapterInput,
) (Disclosure, error) {
	return NewDisclosure(DisclosureInput{
		ExecutionSubject: input.subject.String(),
		InvocationKind:   InvocationKindCommand,
		CWDPolicy:        CWDPolicySelectedRoot,
		EffectClasses: []string{
			"multi_target_config_write",
			"package_resolution",
			"package_store_write",
			"plugin_config_write",
			"same_family_config_replacement",
		},
		RetainedEffectClasses: []string{
			"dependency_state",
			"package_cache",
			"plugin_config_state",
			"resolved_package_artifacts",
		},
		NonClaims: []string{
			"carrier_uninstall",
			"contribution_inventory",
			"exact_artifact_convergence",
			"host_rollback",
			"package_manager_ownership",
			"relation_observation",
			"runtime_readiness",
			"trust_or_approval",
		},
	})
}
