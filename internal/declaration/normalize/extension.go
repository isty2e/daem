package normalize

import (
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func normalizeExtensions(
	rawExtensions []declaration.Extension,
	defaultTargets []target.Target,
	defaults desired.Defaults,
) ([]desiredextension.Extension, error) {
	extensions := make([]desiredextension.Extension, 0, len(rawExtensions))
	for index, raw := range rawExtensions {
		context := fmt.Sprintf("extension[%d]", index)
		extension, err := normalizeExtension(raw, defaultTargets, defaults, context)
		if err != nil {
			return nil, err
		}
		extensions = append(extensions, extension)
	}
	return extensions, nil
}

func normalizeExtension(
	raw declaration.Extension,
	defaultTargets []target.Target,
	defaults desired.Defaults,
	context string,
) (desiredextension.Extension, error) {
	id, err := requiredExactString(raw.ID, context+".id")
	if err != nil {
		return desiredextension.Extension{}, err
	}
	carrierValue, err := requiredExactString(raw.Carrier, context+".carrier")
	if err != nil {
		return desiredextension.Extension{}, err
	}
	carrier, err := desiredextension.ParseCarrier(carrierValue)
	if err != nil {
		return desiredextension.Extension{}, fmt.Errorf("%s.carrier: %w", context, err)
	}

	targets, err := targetsWithDefault(raw.Targets, defaultTargets, context+".targets")
	if err != nil {
		return desiredextension.Extension{}, err
	}
	if len(targets) != 1 {
		return desiredextension.Extension{}, fmt.Errorf("%s.targets: extension declaration supports exactly one target, got [%s]", context, targetList(targets))
	}

	scope, err := scopeWithDefault(raw.Scope, defaults.Scope(), context+".scope")
	if err != nil {
		return desiredextension.Extension{}, err
	}
	if scope == target.ScopeGlobal && raw.Scope == "" {
		return desiredextension.Extension{}, fmt.Errorf("%s.scope: extension carrier %q requires explicit scope = %q; defaults.scope does not authorize this host mutation", context, carrier, target.ScopeGlobal)
	}

	sourceRef, err := normalizeExtensionSource(raw.Source, context+".source")
	if err != nil {
		return desiredextension.Extension{}, err
	}

	extension, err := desiredextension.New(desiredextension.Spec{
		Name: id, Carrier: carrier, Target: targets[0], Scope: scope,
		Source: sourceRef,
	})
	if err != nil {
		return desiredextension.Extension{}, fmt.Errorf("%s: %w", context, err)
	}
	return extension, nil
}

func normalizeExtensionSource(raw declaration.ExtensionSource, context string) (desiredextension.SourceRef, error) {
	hasMarketplace := raw.Marketplace != ""
	hasHostSource := raw.HostSource != ""
	if hasMarketplace == hasHostSource {
		return desiredextension.SourceRef{}, fmt.Errorf("%s: set exactly one of marketplace or host_source", context)
	}

	kind := desiredextension.SourceKindMarketplace
	value := raw.Marketplace
	field := "marketplace"
	if hasHostSource {
		kind = desiredextension.SourceKindHostSource
		value = raw.HostSource
		field = "host_source"
	}
	sourceRef, err := desiredextension.NewAuthoredSourceRef(kind, value)
	if err != nil {
		return desiredextension.SourceRef{}, fmt.Errorf("%s.%s: %w", context, field, err)
	}
	return sourceRef, nil
}
