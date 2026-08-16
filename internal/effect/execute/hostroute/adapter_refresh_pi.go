package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

var piPackageCarrierRefreshCommandAdapter = commandAdapter{
	label:     "Pi package carrier refresh",
	operation: lock.OperationRefresh,
	profile: mustCommandAdapterProfile(
		target.TargetPi,
		desiredextension.CarrierPiPackage,
	),
	build:    buildPiPackageCarrierRefreshCommand,
	disclose: disclosePiPackageCarrierRefresh,
}

func buildPiPackageCarrierRefreshCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeProject &&
		input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Pi package carrier refresh supports project or global selection only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Pi package carrier refresh supports host-source only",
		)
	}
	if err := validatePiHostRouteSourceArg(input.source, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	return subprocess.CommandAttemptRequest{
		Command: piCommand,
		Args: []string{
			"update",
			"--extension",
			input.source.Ref(),
		},
		WorkDir: input.workDir,
	}, nil
}

func disclosePiPackageCarrierRefresh(
	input commandAdapterInput,
) (Disclosure, error) {
	return NewDisclosure(DisclosureInput{
		ExecutionSubject: input.subject.String(),
		InvocationKind:   InvocationKindCommand,
		CWDPolicy:        CWDPolicySelectedRoot,
		EffectClasses: []string{
			"cross_scope_identity_update",
			"dependency_install",
			"git_checkout_reset_and_clean",
			"npm_package_update",
			"package_settings_scan",
			"project_trust_filtering",
		},
		RetainedEffectClasses: []string{
			"dependency_state",
			"git_checkout_changes",
			"package_cache",
			"package_settings_state",
			"partial_scope_updates",
		},
		NonClaims: []string{
			"carrier_uninstall",
			"exact_artifact_convergence",
			"host_rollback",
			"local_path_artifact_update",
			"newer_artifact",
			"pinned_npm_update",
			"runtime_readiness",
			"scope_locality",
			"trust_or_approval",
		},
	})
}
