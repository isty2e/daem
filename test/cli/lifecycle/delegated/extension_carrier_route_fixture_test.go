package cli_test

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func mustCLIDelegatedRoute(
	t *testing.T,
	selectedTarget target.Target,
	carrier desiredextension.Carrier,
) profile.DelegatedRouteProfile {
	t.Helper()
	profile, ok := profile.Profile(selectedTarget).DelegatedRoute(carrier)
	if !ok {
		t.Fatalf("target profile %q is missing delegated route %q", selectedTarget, carrier)
	}
	return profile
}

func claudePluginRoute(t *testing.T) profile.OperationRoute {
	t.Helper()
	return mustCLIDelegatedOperationRoute(
		t,
		mustCLIDelegatedRoute(t, target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin),
		profile.OperationInstall,
	)
}

func codexPluginRoute(t *testing.T) profile.OperationRoute {
	t.Helper()
	return mustCLIDelegatedOperationRoute(
		t,
		mustCLIDelegatedRoute(t, target.TargetCodex, desiredextension.CarrierCodexPlugin),
		profile.OperationInstall,
	)
}

func openCodePluginRoute(t *testing.T) profile.OperationRoute {
	t.Helper()
	return mustCLIDelegatedOperationRoute(
		t,
		mustCLIDelegatedRoute(t, target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin),
		profile.OperationInstall,
	)
}

func piPackageRoute(t *testing.T) profile.OperationRoute {
	t.Helper()
	return mustCLIDelegatedOperationRoute(
		t,
		mustCLIDelegatedRoute(t, target.TargetPi, desiredextension.CarrierPiPackage),
		profile.OperationInstall,
	)
}

func antigravityCLIPluginRoute(t *testing.T) profile.OperationRoute {
	t.Helper()
	return mustCLIDelegatedOperationRoute(
		t,
		mustCLIDelegatedRoute(t, target.TargetAntigravityCLI, desiredextension.CarrierAntigravityCLIPlugin),
		profile.OperationInstall,
	)
}

func mustCLIDelegatedOperationRoute(
	t *testing.T,
	delegated profile.DelegatedRouteProfile,
	operation profile.Operation,
) profile.OperationRoute {
	t.Helper()
	route, ok := delegated.OperationRoute(operation)
	if !ok {
		t.Fatalf(
			"target profile %q carrier %q is missing operation %q",
			delegated.Target(),
			delegated.Carrier(),
			operation,
		)
	}
	return route
}
