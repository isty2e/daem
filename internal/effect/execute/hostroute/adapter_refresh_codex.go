package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

const codexPluginMarketplaceRefreshTimeoutSeconds = 30

var codexPluginCarrierRefreshCommandAdapter = commandAdapter{
	label:     "Codex plugin marketplace refresh",
	operation: lock.OperationRefresh,
	profile: mustCommandAdapterProfile(
		target.TargetCodex,
		desiredextension.CarrierCodexPlugin,
	),
	build:    buildCodexPluginCarrierRefreshCommand,
	disclose: discloseCodexPluginCarrierRefresh,
}

func buildCodexPluginCarrierRefreshCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Codex plugin marketplace refresh supports explicit global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin marketplace refresh supports marketplace source only",
		)
	}
	selector, ok := input.source.MarketplaceSelector()
	if !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin marketplace refresh requires source selector PLUGIN@MARKETPLACE",
		)
	}
	if err := validateHostRouteSourceArg(selector.Marketplace(), subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	return subprocess.CommandAttemptRequest{
		Command: codexCommand,
		Args: []string{
			"plugin",
			"marketplace",
			"upgrade",
			selector.Marketplace(),
			"--json",
		},
		WorkDir: input.workDir,
	}, nil
}

func discloseCodexPluginCarrierRefresh(
	input commandAdapterInput,
) (Disclosure, error) {
	selector, ok := input.source.MarketplaceSelector()
	if !ok {
		return Disclosure{}, newValidationError(
			ReasonUnsupportedSource,
			input.subject,
			"Codex plugin marketplace refresh requires source selector PLUGIN@MARKETPLACE",
		)
	}
	return NewDisclosure(DisclosureInput{
		ExecutionSubject: "codex-plugin-marketplace:" + selector.Marketplace(),
		InvocationKind:   InvocationKindCommand,
		CWDPolicy:        CWDPolicySelectedRoot,
		TimeoutSeconds:   codexPluginMarketplaceRefreshTimeoutSeconds,
		EffectClasses: []string{
			"installed_sibling_cache_refresh",
			"marketplace_network_access",
			"marketplace_snapshot_upgrade",
			"shared_marketplace_update",
		},
		RetainedEffectClasses: []string{
			"installed_sibling_cache_state",
			"marketplace_snapshot_state",
			"partial_plugin_cache_updates",
		},
		NonClaims: []string{
			"atomic_marketplace_and_cache_update",
			"exact_plugin_artifact_convergence",
			"host_rollback",
			"marketplace_fallback",
			"new_marketplace_revision",
			"plugin_install_fallback",
			"plugin_only_mutation",
			"runtime_readiness",
		},
	})
}
