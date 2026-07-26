package hostroute

import (
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func buildClaudePluginCarrierRemoveCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	hostScope, err := claudePluginHostScopeArg(input.scope, subject)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Claude plugin carrier remove supports marketplace source only",
		)
	}
	if _, ok := input.source.MarketplaceSelector(); !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Claude plugin carrier remove requires source selector PLUGIN@MARKETPLACE",
		)
	}
	if err := validateHostRouteSourceArg(input.source.Ref(), subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	return subprocess.CommandAttemptRequest{
		Command: claudeCommand,
		Args: []string{
			"plugin",
			"uninstall",
			input.source.Ref(),
			"--scope",
			hostScope,
			"--keep-data",
		},
		WorkDir: input.workDir,
	}, nil
}

func buildCodexPluginCarrierRemoveCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Codex plugin carrier remove supports global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin carrier remove supports marketplace source only",
		)
	}
	if _, ok := input.source.MarketplaceSelector(); !ok {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Codex plugin carrier remove requires source selector PLUGIN@MARKETPLACE",
		)
	}
	if err := validateHostRouteSourceArg(input.source.Ref(), subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	return subprocess.CommandAttemptRequest{
		Command: codexCommand,
		Args: []string{
			"plugin",
			"remove",
			input.source.Ref(),
			"--json",
		},
		WorkDir: input.workDir,
	}, nil
}

func buildPiPackageCarrierRemoveCommand(input commandAdapterInput) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeProject && input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Pi package carrier remove supports project or global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Pi package carrier remove supports host-source only",
		)
	}
	if err := validateHostRouteSourceArg(input.source.Ref(), subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}

	args := []string{"remove", input.source.Ref()}
	if input.scope == target.ScopeProject {
		args = append(args, "-l")
	}
	return subprocess.CommandAttemptRequest{
		Command: piCommand,
		Args:    args,
		WorkDir: input.workDir,
	}, nil
}

func buildAntigravityCLIPluginCarrierRemoveCommand(
	input commandAdapterInput,
) (subprocess.CommandAttemptRequest, error) {
	subject := input.subject
	if input.scope != target.ScopeGlobal {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedScope,
			subject,
			"Antigravity CLI plugin remove supports explicit global scope only",
		)
	}
	if input.source.Kind() != desiredextension.SourceKindHostSource {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin remove supports host-source only",
		)
	}
	carrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		input.scope,
		input.source,
	)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin remove source is invalid: %v",
			err,
		)
	}
	source, err := extensiontopology.InterpretCarrierSource(carrier)
	if err != nil {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin remove source is not interpretable: %v",
			err,
		)
	}
	if source.Class() != extensiontopology.CarrierSourceMarketplace {
		return subprocess.CommandAttemptRequest{}, newValidationError(
			ReasonUnsupportedSource,
			subject,
			"Antigravity CLI plugin remove requires host source PLUGIN@MARKETPLACE",
		)
	}
	pluginName := source.RelationIdentity()
	if err := validateHostRouteSourceArg(pluginName, subject); err != nil {
		return subprocess.CommandAttemptRequest{}, err
	}
	return subprocess.CommandAttemptRequest{
		Command: agyCommand,
		Args: []string{
			"plugin",
			"uninstall",
			pluginName,
		},
		WorkDir: input.workDir,
	}, nil
}
