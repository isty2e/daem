package normalize

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
)

func normalizeHookAssets(rawAssets map[string]declaration.HookAsset, defaults desired.Defaults) ([]desiredhookasset.HookAsset, error) {
	if len(rawAssets) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(rawAssets))
	for name := range rawAssets {
		names = append(names, name)
	}
	sort.Strings(names)

	assets := make([]desiredhookasset.HookAsset, 0, len(names))
	for _, name := range names {
		context := "hook_asset." + name
		rawAsset := rawAssets[name]
		sourceSpec, err := normalizeRequiredSource(rawAsset.Source.Source, context+".source")
		if err != nil {
			return nil, err
		}
		if rawAsset.Kind == "" {
			return nil, fmt.Errorf("%s.kind: required", context)
		}
		assetKind, err := desiredhookasset.ParseArtifactKind(rawAsset.Kind)
		if err != nil {
			return nil, fmt.Errorf("%s.kind: %w", context, err)
		}

		scope, err := scopeWithDefault(rawAsset.Scope, defaults.Scope(), context+".scope")
		if err != nil {
			return nil, err
		}

		asset, err := desiredhookasset.New(desiredhookasset.Spec{
			Name:         name,
			Source:       sourceSpec,
			ArtifactKind: assetKind,
			Scope:        scope,
			Executable:   rawAsset.Executable,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", context, err)
		}
		assets = append(assets, asset)
	}

	return assets, nil
}
