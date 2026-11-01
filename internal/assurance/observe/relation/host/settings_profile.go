package relationhost

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	"github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	managedhostpath "github.com/isty2e/daem/internal/output/hostpath/managed"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type NativeSettingsProfile struct {
	key       desiredextension.CarrierKey
	operation lock.OperationKind
}

func NativeSettingsProfileFor(key desiredextension.CarrierKey, operation lock.OperationKind) (NativeSettingsProfile, error) {
	if err := key.Validate(); err != nil {
		return NativeSettingsProfile{}, err
	}
	if operation != lock.OperationInstall && operation != lock.OperationRemove && operation != lock.OperationRefresh {
		return NativeSettingsProfile{}, fmt.Errorf("native settings operation is unsupported")
	}
	admitted := false
	switch key.Carrier() {
	case desiredextension.CarrierPiPackage:
		admitted = key.Target() == target.TargetPi
	case desiredextension.CarrierCodexPlugin:
		admitted = key.Target() == target.TargetCodex && key.Scope() == target.ScopeGlobal
	case desiredextension.CarrierClaudeCodePlugin:
		admitted = key.Target() == target.TargetClaudeCode
	case desiredextension.CarrierAntigravityCLIPlugin:
		admitted = key.Target() == target.TargetAntigravityCLI && key.Scope() == target.ScopeGlobal
	case desiredextension.CarrierOpenCodePlugin:
		admitted = key.Target() == target.TargetOpenCode
	}
	if !admitted {
		return NativeSettingsProfile{}, fmt.Errorf("native settings profile is unsupported")
	}
	return NativeSettingsProfile{key: key, operation: operation}, nil
}

func (profile NativeSettingsProfile) validate() error {
	_, err := NativeSettingsProfileFor(profile.key, profile.operation)
	return err
}

func (profile NativeSettingsProfile) RequireActuation(actual lock.ActuationKind) error {
	if err := profile.validate(); err != nil {
		return err
	}
	wanted := lock.ActuationDelegatedHostRoute
	if profile.key.Carrier() == desiredextension.CarrierOpenCodePlugin && profile.operation == lock.OperationRemove {
		wanted = lock.ActuationDirectProjection
	}
	if wanted != actual {
		return fmt.Errorf("native settings profile does not admit the actual operation actuation")
	}
	return nil
}

func (profile NativeSettingsProfile) ExclusionPaths(paths daempaths.Paths) ([]relationobserve.AuthorityPath, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	values, _, err := opaquePluginNamespaces(paths, profile.key.Target(), profile.key.Scope())
	return values, err
}

func (profile NativeSettingsProfile) FencePaths(paths daempaths.Paths) ([]relationobserve.AuthorityPath, error) {
	if profile.operation != lock.OperationRefresh {
		return nil, fmt.Errorf("native refresh fence requires its refresh profile")
	}
	result, err := profile.ExclusionPaths(paths)
	if err != nil {
		return nil, err
	}
	rewrite, err := profile.RewritePaths(paths)
	if err != nil {
		return nil, err
	}
	return append(result, rewrite...), nil
}

func (profile NativeSettingsProfile) RewritePaths(paths daempaths.Paths) ([]relationobserve.AuthorityPath, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	var result []relationobserve.AuthorityPath
	var addresses []aggregate.DocumentAddress
	switch profile.key.Carrier() {
	case desiredextension.CarrierPiPackage:
		scopes := []target.Scope{profile.key.Scope()}
		if profile.operation == lock.OperationRefresh {
			scopes = []target.Scope{target.ScopeProject, target.ScopeGlobal}
		}
		for _, scope := range scopes {
			address, err := pipackage.SettingsDocumentAddress(scope)
			if err != nil {
				return nil, err
			}
			addresses = append(addresses, address)
		}
	case desiredextension.CarrierCodexPlugin:
		address, err := codexplugin.ConfigDocumentAddress()
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, address)
	}
	for _, address := range addresses {
		path, err := managedhostpath.Resolver(paths).Resolve(address.AggregateRoot())
		if err != nil {
			return nil, err
		}
		value, err := relationobserve.NewAuthorityPath(path, address.Target(), address.Scope())
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

type settingsRewrite struct {
	aliases  []aggregate.DocumentAddress
	preserve func(context.Context, configdocument.Document, configdocument.Document) (bool, error)
}

// NativeSettingsRewrites carries only host-selected exceptions. Its zero value
// preserves the complete document; it does not admit an invocation.
type NativeSettingsRewrites struct{ rows []settingsRewrite }

func (rewrites NativeSettingsRewrites) PreservesOutside(ctx context.Context, address aggregate.DocumentAddress, before, after configdocument.Document) (bool, error) {
	for _, row := range rewrites.rows {
		if slices.Contains(row.aliases, address) {
			return row.preserve(ctx, before, after)
		}
	}
	return before.PreservesOutside(ctx, after, nil, nil)
}

func (profile NativeSettingsProfile) BindInvocationRewrites(paths daempaths.Paths, groups [][]aggregate.DocumentAddress) (NativeSettingsRewrites, error) {
	if err := profile.RequireActuation(lock.ActuationDelegatedHostRoute); err != nil {
		return NativeSettingsRewrites{}, err
	}
	key := profile.key
	if key.Carrier() == desiredextension.CarrierCodexPlugin {
		wanted, err := codexplugin.ConfigDocumentAddress()
		if err != nil {
			return NativeSettingsRewrites{}, err
		}
		for _, aliases := range groups {
			if !slices.Contains(aliases, wanted) {
				continue
			}
			preserve := func(ctx context.Context, before, after configdocument.Document) (bool, error) {
				if profile.operation == lock.OperationRefresh {
					return codexplugin.PreservesOutsideMarketplaceRewrite(ctx, before, after, key.Source())
				}
				return codexplugin.PreservesOutsidePluginRewrite(ctx, before, after, key.Source())
			}
			return NativeSettingsRewrites{rows: []settingsRewrite{{aliases: append([]aggregate.DocumentAddress(nil), aliases...), preserve: preserve}}}, nil
		}
		return NativeSettingsRewrites{}, nil
	}
	if key.Carrier() != desiredextension.CarrierPiPackage {
		return NativeSettingsRewrites{}, nil
	}
	home, err := paths.UserHomeDir()
	if err != nil {
		return NativeSettingsRewrites{}, err
	}
	selectedSource := key.Source().Ref()
	interpreted, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return NativeSettingsRewrites{}, err
	}
	if interpreted.Class() == extensiontopology.CarrierSourceLocal {
		commandContext, err := extensiontopology.NewLocalSourceContext(paths.ManifestRoot, home)
		if err != nil {
			return NativeSettingsRewrites{}, err
		}
		absolute, err := interpreted.ResolveLocal(commandContext)
		if err != nil {
			return NativeSettingsRewrites{}, err
		}
		selectedSource = absolute.Path()
	}
	bind := func(address aggregate.DocumentAddress, aliases []aggregate.DocumentAddress) (settingsRewrite, error) {
		path, err := managedhostpath.Resolver(paths).Resolve(address.AggregateRoot())
		if err != nil {
			return settingsRewrite{}, err
		}
		sourceContext, err := extensiontopology.NewLocalSourceContext(filepath.Dir(path), home)
		if err != nil {
			return settingsRewrite{}, err
		}
		return settingsRewrite{aliases: append([]aggregate.DocumentAddress(nil), aliases...), preserve: func(ctx context.Context, before, after configdocument.Document) (bool, error) {
			return pipackage.PreservesOutsidePackageRewrite(ctx, before, after, selectedSource, address.Scope(), sourceContext)
		}}, nil
	}
	if profile.operation != lock.OperationRefresh {
		wanted, err := pipackage.SettingsDocumentAddress(key.Scope())
		if err != nil {
			return NativeSettingsRewrites{}, err
		}
		row, err := bind(wanted, nil)
		if err != nil {
			return NativeSettingsRewrites{}, err
		}
		for _, aliases := range groups {
			if slices.Contains(aliases, wanted) {
				row.aliases = append([]aggregate.DocumentAddress(nil), aliases...)
				return NativeSettingsRewrites{rows: []settingsRewrite{row}}, nil
			}
		}
		return NativeSettingsRewrites{}, nil
	}
	var result NativeSettingsRewrites
	for _, aliases := range groups {
		for _, address := range aliases {
			if address.Target() != target.TargetPi {
				continue
			}
			wanted, err := pipackage.SettingsDocumentAddress(address.Scope())
			if err != nil {
				return NativeSettingsRewrites{}, err
			}
			if address != wanted {
				continue
			}
			row, err := bind(address, []aggregate.DocumentAddress{address})
			if err != nil {
				return NativeSettingsRewrites{}, err
			}
			result.rows = append(result.rows, row)
		}
	}
	return result, nil
}
